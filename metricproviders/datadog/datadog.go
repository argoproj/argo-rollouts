package datadog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var unixNow = func() int64 { return time.Now().Unix() }

const (
	//ProviderType indicates the provider is datadog
	ProviderType            = "Datadog"
	DatadogTokensSecretName = "datadog"
)

// Provider contains all the required components to run a Datadog query
// Implements the Provider Interface
type Provider struct {
	logCtx log.Entry
	config datadogConfig
}

type datadogResponse struct {
	Series []struct {
		Pointlist [][]float64 `json:"pointlist"`
	}
}

type datadogConfig struct {
	Address string `yaml:"address,omitempty"`
	ApiKey  string `yaml:"api-key,omitempty"`
	AppKey  string `yaml:"app-key,omitempty"`
}

// Type incidates provider is a Datadog provider
func (p *Provider) Type() string {
	return ProviderType
}

func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := metav1.Now()

	// Measurement to pass back
	measurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	endpoint := "https://api.datadoghq.com/api/v1/query"
	if p.config.Address != "" {
		endpoint = p.config.Address + "/api/v1/query"
	}

	url, _ := url.Parse(endpoint)

	now := unixNow()
	var interval int64 = 300
	if metric.Provider.Datadog.Interval != "" {
		expDuration, err := metric.Provider.Datadog.Interval.Duration()
		if err != nil {
			return metricutil.MarkMeasurementError(measurement, err)
		}
		// Convert to seconds as DataDog expects unix timestamp
		interval = int64(expDuration.Seconds())
	}

	q := url.Query()
	q.Set("query", metric.Provider.Datadog.Query)
	q.Set("from", strconv.FormatInt(now-interval, 10))
	q.Set("to", strconv.FormatInt(now, 10))
	url.RawQuery = q.Encode()

	request := &http.Request{Method: "GET"}
	request.URL = url
	request.Header = make(http.Header)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("DD-API-KEY", p.config.ApiKey)
	request.Header.Set("DD-APPLICATION-KEY", p.config.AppKey)

	// Send Request
	httpClient := &http.Client{
		Timeout: time.Duration(10) * time.Second,
	}
	response, err := httpClient.Do(request)

	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
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

	bodyBytes, err := ioutil.ReadAll(response.Body)

	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Received no bytes in response: %v", err)
	}

	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("received authentication error response code: %v %s", response.StatusCode, string(bodyBytes))
	} else if response.StatusCode != http.StatusOK {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("received non 2xx response code: %v %s", response.StatusCode, string(bodyBytes))
	}

	var res datadogResponse
	err = json.Unmarshal(bodyBytes, &res)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Could not parse JSON body: %v", err)
	}

	if len(res.Series) < 1 || len(res.Series[0].Pointlist) < 1 {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Datadog returned no value: %s", string(bodyBytes))
	}

	series := res.Series[0]
	datapoint := series.Pointlist[len(series.Pointlist)-1]
	if len(datapoint) < 1 {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Datadog returned no value: %s", string(bodyBytes))
	}

	status := evaluate.EvaluateResult(datapoint[1], metric, p.logCtx)
	return strconv.FormatFloat(datapoint[1], 'f', -1, 64), status, nil
}

// Resume should not be used the Datadog provider since all the work should occur in the Run method
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Datadog provider should not execute the Resume method")
	return measurement
}

// Terminate should not be used the Datadog provider since all the work should occur in the Run method
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Datadog provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect is a no-op for the Datadog provider
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	return nil
}

func NewDatadogProvider(logCtx log.Entry, kubeclientset kubernetes.Interface) (*Provider, error) {
	ns := Namespace()
	secret, err := kubeclientset.CoreV1().Secrets(ns).Get(context.TODO(), DatadogTokensSecretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	config := datadogConfig{}

	if e := yaml.Unmarshal(secret.Data["default"], &config); e != nil {
		return nil, e
	}

	if config.ApiKey != "" && config.AppKey != "" {
		return &Provider{
			logCtx: logCtx,
			config: config,
		}, nil
	} else {
		return nil, errors.New("API or App token not found")
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
