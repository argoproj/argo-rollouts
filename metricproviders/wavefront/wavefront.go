package wavefront

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	wavefrontapi "github.com/spaceapegames/go-wavefront"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
)

const (
	//ProviderType indicates the provider is wavefront
	ProviderType = "Wavefront"
	//k8s secret that has wavefront api tokens
	WavefrontTokensSecretName = "wavefront-api-tokens"
)

type Provider struct {
	api    WavefrontClientAPI
	logCtx log.Entry
}

func (p *Provider) Type() string {
	return ProviderType
}

type WavefrontClientAPI interface {
	NewQuery(params *wavefrontapi.QueryParams) WavefrontQueryAPI
}

type WavefrontClient struct {
	*wavefrontapi.Client
}

func (wc *WavefrontClient) NewQuery(params *wavefrontapi.QueryParams) WavefrontQueryAPI {
	return &WavefrontQuery{Query: wc.Client.NewQuery(params)}
}

type WavefrontQueryAPI interface {
	Execute() (*wavefrontapi.QueryResponse, error)
}

type WavefrontQuery struct {
	*wavefrontapi.Query
}

func (wq *WavefrontQuery) Execute() (*wavefrontapi.QueryResponse, error) {
	return wq.Query.Execute()
}

type wavefrontResponse struct {
	newValue   string
	newStatus  v1alpha1.AnalysisPhase
	epochsUsed string
}

// Run queries with wavefront provider for the metric
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := metav1.Now()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
		Metadata:  map[string]string{},
	}

	queryParams := &wavefrontapi.QueryParams{
		QueryString:             metric.Provider.Wavefront.Query,
		StartTime:               strconv.FormatInt(startTime.Unix()*1000, 10),
		EndTime:                 strconv.FormatInt(startTime.Unix()*1000, 10),
		MaxPoints:               "1",
		Granularity:             "s",
		SeriesOutsideTimeWindow: false,
	}

	response, err := p.api.NewQuery(queryParams).Execute()
	if response != nil && response.Warnings != "" {
		newMeasurement.Metadata["warnings"] = response.Warnings
	}
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}
	result, err := p.processResponse(metric, response, startTime)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)

	}
	newMeasurement.Value = result.newValue
	newMeasurement.Phase = result.newStatus
	newMeasurement.Metadata["timestamps"] = result.epochsUsed
	finishedTime := metav1.Now()
	newMeasurement.FinishedAt = &finishedTime
	return newMeasurement
}

// Resume should not be used the Wavefront provider since all the work should occur in the Run method
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Wavefront provider should not execute the Resume method")
	return measurement
}

// Terminate should not be used the Wavefront provider since all the work should occur in the Run method
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Wavefront provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect is a no-op for the Wavefront provider
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	return nil
}

func (p *Provider) findDataPointValue(datapoints []wavefrontapi.DataPoint, startTime metav1.Time) (float64, string) {
	currentValue := float64(0)
	currentTime := float64(0)
	delta := math.Inf(1)
	startTimeEpoch := float64(startTime.Unix())
	for _, dp := range datapoints {
		newDelta := math.Abs(startTimeEpoch - dp[0])
		if newDelta < delta {
			currentValue = dp[1]
			currentTime = dp[0]
			delta = newDelta
		}
	}
	p.logCtx.Infof("Selected Timestamp has drift of %.0f seconds", delta)
	return currentValue, fmt.Sprintf("%.0f", currentTime)
}

func (p *Provider) processResponse(metric v1alpha1.Metric, response *wavefrontapi.QueryResponse, startTime metav1.Time) (wavefrontResponse, error) {
	wavefrontResponse := wavefrontResponse{}
	if len(response.TimeSeries) == 1 {
		series := response.TimeSeries[0]
		value, time := p.findDataPointValue(series.DataPoints, startTime) // Wavefront DataPoint struct is of type []float{<timestamp>, <value>}
		wavefrontResponse.newValue = fmt.Sprintf("%.2f", value)
		wavefrontResponse.epochsUsed = time
		wavefrontResponse.newStatus = evaluate.EvaluateResult(value, metric, p.logCtx)
		return wavefrontResponse, nil

	} else if len(response.TimeSeries) > 1 {
		results := make([]float64, 0, len(response.TimeSeries))
		valueStr := "["
		epochsStr := "["
		for _, series := range response.TimeSeries {
			value, epoch := p.findDataPointValue(series.DataPoints, startTime) // Wavefront DataPoint struct is of type []float{<timestamp>, <value>}
			valueStr = valueStr + fmt.Sprintf("%.2f", value) + ","
			epochsStr = epochsStr + epoch + ","
			results = append(results, value)
		}
		if len(valueStr) > 1 {
			valueStr = valueStr[:len(valueStr)-1]
		}
		valueStr = valueStr + "]"
		if len(epochsStr) > 1 {
			epochsStr = epochsStr[:len(epochsStr)-1]
		}
		epochsStr = epochsStr + "]"
		wavefrontResponse.newValue = valueStr
		wavefrontResponse.epochsUsed = epochsStr
		wavefrontResponse.newStatus = evaluate.EvaluateResult(results, metric, p.logCtx)
		return wavefrontResponse, nil

	} else {
		wavefrontResponse.newStatus = v1alpha1.AnalysisPhaseFailed
		return wavefrontResponse, fmt.Errorf("No TimeSeries found in response from Wavefront")
	}
}

// NewWavefrontProvider Creates a new Wavefront client
func NewWavefrontProvider(api WavefrontClientAPI, logCtx log.Entry) *Provider {
	return &Provider{
		logCtx: logCtx,
		api:    api,
	}
}

// NewWavefrontAPI generates a Wavefront API client from the metric configuration
func NewWavefrontAPI(metric v1alpha1.Metric, kubeclientset kubernetes.Interface) (WavefrontClientAPI, error) {
	ns := Namespace()
	secret, err := kubeclientset.CoreV1().Secrets(ns).Get(context.TODO(), WavefrontTokensSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	var wf_client *wavefrontapi.Client
	for source, token := range secret.Data {
		if source == metric.Provider.Wavefront.Address {
			wf_client, _ = wavefrontapi.NewClient(&wavefrontapi.Config{
				Address: source,
				Token:   string(token),
			})
		}
	}
	if wf_client != nil {
		return &WavefrontClient{Client: wf_client}, nil
	} else {
		return nil, errors.New("API token not found")
	}
}

func Namespace() string {
	// This way assumes you've set the POD_NAMESPACE environment variable using the downward API.
	// This check has to be done first for backwards compatibility with the way InClusterConfig was originally set up
	if ns, ok := os.LookupEnv("POD_NAMESPACE"); ok {
		return ns
	}
	// Fall back to the namespace associated with the service account token, if available
	if data, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if ns := strings.TrimSpace(string(data)); len(ns) > 0 {
			return ns
		}
	}
	return "argo-rollouts"
}
