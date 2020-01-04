package webmetric

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/jsonpath"
)

const (
	//ProviderType indicates the provider is prometheus
	ProviderType = "WebMetric"
)

// Provider contains all the required components to run a WebMetric query
// Implements the Provider Interface
type Provider struct {
	logCtx     log.Entry
	client     *http.Client
	jsonParser *jsonpath.JSONPath
}

// Type incidates provider is a WebMetric provider
func (p *Provider) Type() string {
	return ProviderType
}

func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := metav1.Now()

	// Measurement to pass back
	measurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	// Create request
	request := &http.Request{
		Method: "GET", // TODO maybe make this configurable....also implies we will need body templates
	}

	url, err := url.Parse(metric.Provider.Web.URL)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}
	request.URL = url

	request.Header = make(http.Header)
	for _, header := range metric.Provider.Web.Headers {
		request.Header.Set(header.Key, header.Value)
	}

	// Send Request
	response, err := p.client.Do(request)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	} else if response.StatusCode < 200 || response.StatusCode >= 300 {
		return metricutil.MarkMeasurementError(measurement, fmt.Errorf("received non 2xx response code: %v", response.StatusCode))
	}

	value, status, err := p.parseResponse(metric, response)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	measurement.Value = value
	measurement.Phase = status
	finishedTime := metav1.Now()
	measurement.FinishedAt = &finishedTime

	return measurement
}

func (p *Provider) parseResponse(metric v1alpha1.Metric, response *http.Response) (string, v1alpha1.AnalysisPhase, error) {
	var data interface{}

	bodyBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Received no bytes in response: %v", err)
	}

	err = json.Unmarshal(bodyBytes, &data)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Could not parse JSON body: %v", err)
	}

	buf := new(bytes.Buffer)
	err = p.jsonParser.Execute(buf, data)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Could not find JSONPath in body: %s", err)
	}
	out := buf.String()

	status := evaluate.EvaluateResult(out, metric, p.logCtx)
	return out, status, nil
}

// Resume should not be used the WebMetric provider since all the work should occur in the Run method
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("WebMetric provider should not execute the Resume method")
	return measurement
}

// Terminate should not be used the WebMetric provider since all the work should occur in the Run method
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("WebMetric provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect is a no-op for the WebMetric provider
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	return nil
}

func NewWebMetricHttpClient(metric v1alpha1.Metric) *http.Client {
	var timeout time.Duration

	// Using a default timeout of 10 seconds
	if metric.Provider.Web.TimeoutSeconds <= 0 {
		timeout = time.Duration(10) * time.Second
	} else {
		timeout = time.Duration(metric.Provider.Web.TimeoutSeconds) * time.Second
	}

	c := &http.Client{
		Timeout: timeout,
	}
	return c
}

func NewWebMetricJsonParser(metric v1alpha1.Metric) (*jsonpath.JSONPath, error) {
	jsonParser := jsonpath.New("metrics")

	err := jsonParser.Parse(metric.Provider.Web.JSONPath)

	return jsonParser, err
}

func NewWebMetricProvider(logCtx log.Entry, client *http.Client, jsonParser *jsonpath.JSONPath) *Provider {
	return &Provider{
		logCtx:     logCtx,
		client:     client,
		jsonParser: jsonParser,
	}
}
