package datadog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"

	log "github.com/sirupsen/logrus"
)

var unixNow = func() int64 { return timeutil.Now().Unix() }

const (
	//ProviderType indicates the provider is datadog
	ProviderType                     = "Datadog"
	EnvVarArgoRolloutsDatadogApiKey  = "ARGO_ROLLOUTS_DD_API_KEY"
	EnvVarArgoRolloutsDatadogAppKey  = "ARGO_ROLLOUTS_DD_APP_KEY"
	EnvVarArgoRolloutsDatadogAddress = "ARGO_ROLLOUTS_DD_ADDRESS"
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

// GetMetadata returns any additional metadata which needs to be stored & displayed as part of the metrics result.
func (p *Provider) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return nil
}

func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := timeutil.MetaNow()

	// Measurement to pass back
	measurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	endpoint := "https://api.datadoghq.com/api/v1/query"
	if p.config.Address != "" {
		endpoint = p.config.Address + "/api/v1/query"
	}

	url, err := url.Parse(endpoint)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

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
	finishedTime := timeutil.MetaNow()
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

	// Handle an empty query result
	if len(res.Series) == 0 || len(res.Series[0].Pointlist) == 0 {
		var nilFloat64 *float64
		status, err := evaluate.EvaluateResult(nilFloat64, metric, p.logCtx)
		seriesBytes, jsonErr := json.Marshal(res.Series)
		if jsonErr != nil {
			return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Failed to marshall JSON empty series: %v", jsonErr)
		}

		return string(seriesBytes), status, err
	}

	// Handle a populated query result
	series := res.Series[0]
	datapoint := series.Pointlist[len(series.Pointlist)-1]
	if len(datapoint) != 2 {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Datapoint does not have 2 values")
	}

	value := datapoint[1]
	status, err := evaluate.EvaluateResult(value, metric, p.logCtx)
	return strconv.FormatFloat(value, 'f', -1, 64), status, err
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

func NewDatadogProvider(logCtx log.Entry) (*Provider, error) {
	envValuesByKey := make(map[string]string)
	if apiKey, ok := os.LookupEnv(fmt.Sprintf("%s", EnvVarArgoRolloutsDatadogApiKey)); ok {
		envValuesByKey[EnvVarArgoRolloutsDatadogApiKey] = apiKey
		log.Debugf("ARGO_ROLLOUTS_DD_API_KEY: %v", envValuesByKey[EnvVarArgoRolloutsDatadogApiKey])
	}
	if appKey, ok := os.LookupEnv(fmt.Sprintf("%s", EnvVarArgoRolloutsDatadogAppKey)); ok {
		envValuesByKey[EnvVarArgoRolloutsDatadogAppKey] = appKey
		log.Debugf("ARGO_ROLLOUTS_DD_APP_KEY: %v", envValuesByKey[EnvVarArgoRolloutsDatadogAppKey])
	}
	if address, ok := os.LookupEnv(fmt.Sprintf("%s", EnvVarArgoRolloutsDatadogAddress)); ok {
		envValuesByKey[EnvVarArgoRolloutsDatadogAddress] = address
		log.Debugf("ARGO_ROLLOUTS_DD_ADDRESS: %v", envValuesByKey[EnvVarArgoRolloutsDatadogAddress])
	}

	if envValuesByKey[EnvVarArgoRolloutsDatadogApiKey] != "" && envValuesByKey[EnvVarArgoRolloutsDatadogAppKey] != "" {
		return &Provider{
			logCtx: logCtx,
			config: datadogConfig{
				Address: envValuesByKey[EnvVarArgoRolloutsDatadogAddress],
				ApiKey:  envValuesByKey[EnvVarArgoRolloutsDatadogApiKey],
				AppKey:  envValuesByKey[EnvVarArgoRolloutsDatadogAppKey],
			},
		}, nil
	} else {
		return nil, errors.New("API or App token not found")
	}

}
