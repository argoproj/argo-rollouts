package wavefront

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	wavefrontapi "github.com/spaceapegames/go-wavefront"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
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

// GetMetadata returns any additional metadata which needs to be stored & displayed as part of the metrics result.
func (p *Provider) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return nil
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
	drift      string
}

// Run queries with wavefront provider for the metric
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := timeutil.MetaNow()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
		Metadata:  map[string]string{},
	}

	// NOTE: Currently our queries to wavefront set StartTime and EndTime to be the same value.
	// By doing so,  we should also set SeriesOutsideTimeWindow to true, otherwise there is a high
	// likelihood of returning no datapoints from wavefront.
	// If in the future, we adjust StartTime such that there is a larger query window, we
	// will also need to remove MaxPoints, since Wavefront will pick data points closer to StartTime
	// which will result in a larger drift.
	queryParams := &wavefrontapi.QueryParams{
		QueryString:             metric.Provider.Wavefront.Query,
		StartTime:               strconv.FormatInt(startTime.Unix()*1000, 10),
		EndTime:                 strconv.FormatInt(startTime.Unix()*1000, 10),
		MaxPoints:               "1",
		Granularity:             "s",
		SeriesOutsideTimeWindow: true,
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
	newMeasurement.Metadata["drift"] = result.drift
	finishedTime := timeutil.MetaNow()
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

// findDataPointValue returns the value of the closest data point to the measurement time.
// Returns the value, timestamp in epoch seconds, and drift in seconds of the chosen datapoint
// Wavefront DataPoint struct is of type []float{<timestamp>, <value>}
func (p *Provider) findDataPointValue(datapoints []wavefrontapi.DataPoint, mTime metav1.Time) (float64, int64, int64) {
	currentValue := float64(0)
	currentTime := float64(0)
	delta := math.Inf(1)
	startTimeEpoch := float64(mTime.Unix())
	for _, dp := range datapoints {
		newDelta := dp[0] - startTimeEpoch
		if math.Abs(newDelta) < math.Abs(delta) {
			currentValue = dp[1]
			currentTime = dp[0]
			delta = newDelta
		}
	}
	return currentValue, int64(currentTime), int64(delta)
}

func (p *Provider) processResponse(metric v1alpha1.Metric, response *wavefrontapi.QueryResponse, startTime metav1.Time) (wavefrontResponse, error) {
	wavefrontResponse := wavefrontResponse{}
	var err error
	if len(response.TimeSeries) == 1 {
		series := response.TimeSeries[0]
		value, epoch, drift := p.findDataPointValue(series.DataPoints, startTime)
		wavefrontResponse.newValue = fmt.Sprintf("%.2f", value)
		wavefrontResponse.epochsUsed = strconv.Itoa(int(epoch))
		wavefrontResponse.newStatus, err = evaluate.EvaluateResult(value, metric, p.logCtx)
		wavefrontResponse.drift = strconv.Itoa(int(drift))
		return wavefrontResponse, err

	} else if len(response.TimeSeries) > 1 {
		results := make([]float64, 0, len(response.TimeSeries))
		resultStrs := []string{}
		epochStrs := []string{}
		driftStrs := []string{}
		for _, series := range response.TimeSeries {
			value, epoch, drift := p.findDataPointValue(series.DataPoints, startTime)
			results = append(results, value)
			resultStrs = append(resultStrs, fmt.Sprintf("%.2f", value))
			epochStrs = append(epochStrs, strconv.Itoa(int(epoch)))
			driftStrs = append(driftStrs, strconv.Itoa(int(drift)))
		}
		wavefrontResponse.newValue = fmt.Sprintf("[%s]", strings.Join(resultStrs, ","))
		wavefrontResponse.epochsUsed = fmt.Sprintf("[%s]", strings.Join(epochStrs, ","))
		wavefrontResponse.drift = fmt.Sprintf("[%s]", strings.Join(driftStrs, ","))
		wavefrontResponse.newStatus, err = evaluate.EvaluateResult(results, metric, p.logCtx)
		return wavefrontResponse, err

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
	ns := defaults.Namespace()
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
