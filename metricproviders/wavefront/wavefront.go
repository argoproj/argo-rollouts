package wavefront

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	wavefront_api "github.com/spaceapegames/go-wavefront"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	templateutil "github.com/argoproj/argo-rollouts/utils/template"
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
	NewQuery(params *wavefront_api.QueryParams) WavefrontQueryAPI
}

type WavefrontClient struct {
	*wavefront_api.Client
}

func (wc *WavefrontClient) NewQuery(params *wavefront_api.QueryParams) WavefrontQueryAPI {
	return &WavefrontQuery{Query: wc.Client.NewQuery(params)}
}

type WavefrontQueryAPI interface {
	Execute() (*wavefront_api.QueryResponse, error)
}

type WavefrontQuery struct {
	*wavefront_api.Query
}

func (wq *WavefrontQuery) Execute() (*wavefront_api.QueryResponse, error) {
	return wq.Query.Execute()
}

// Run queries with wavefront provider for the metric
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := metav1.Now()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	query, err := templateutil.ResolveArgs(metric.Provider.Wavefront.Query, run.Spec.Args)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	queryParams := &wavefront_api.QueryParams{
		QueryString:             query,
		StartTime:               strconv.FormatInt(time.Now().Unix()*1000, 10),
		MaxPoints:               "1",
		Granularity:             "s",
		SeriesOutsideTimeWindow: false,
	}

	response, err := p.api.NewQuery(queryParams).Execute()
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}
	newValue, newStatus, err := p.processResponse(metric, response)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)

	}
	newMeasurement.Value = newValue
	newMeasurement.Phase = newStatus
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

func (p *Provider) processResponse(metric v1alpha1.Metric, response *wavefront_api.QueryResponse) (string, v1alpha1.AnalysisPhase, error) {

	if len(response.TimeSeries) == 1 {
		series := response.TimeSeries[0]
		result := series.DataPoints[0][1] // Wavefront DataPoint struct is of type []float{<timestamp>, <value>}
		if math.IsNaN(result) {
			return fmt.Sprintf("%.2f", result), v1alpha1.AnalysisPhaseInconclusive, nil
		}
		newStatus := evaluate.EvaluateResult(result, metric, p.logCtx)
		return fmt.Sprintf("%.2f", result), newStatus, nil

	} else if len(response.TimeSeries) > 1 {
		results := make([]float64, 0, len(response.TimeSeries))
		valueStr := "["
		for _, series := range response.TimeSeries {
			value := series.DataPoints[0][1]
			valueStr = valueStr + fmt.Sprintf("%.2f", value) + ","
			results = append(results, value)
		}
		if len(valueStr) > 1 {
			valueStr = valueStr[:len(valueStr)-1]
		}
		valueStr = valueStr + "]"
		for _, result := range results {
			if math.IsNaN(result) {
				return valueStr, v1alpha1.AnalysisPhaseInconclusive, nil
			}
		}
		newStatus := evaluate.EvaluateResult(results, metric, p.logCtx)
		return valueStr, newStatus, nil

	} else {
		return "", v1alpha1.AnalysisPhaseFailed, fmt.Errorf("No TimeSeries found in response from Wavefront")
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
	secret, err := kubeclientset.CoreV1().Secrets(ns).Get(WavefrontTokensSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	var wf_client *wavefront_api.Client
	for source, token := range secret.Data {
		if source == metric.Provider.Wavefront.Address {
			wf_client, _ = wavefront_api.NewClient(&wavefront_api.Config{
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
