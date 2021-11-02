package appinsights

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	//ProviderType indicates the provider is Applicaion Insights
	ProviderType                = "AppInsights"
	AppInsightsTokensSecretName = "appinsights"
	AppInsightsApiId            = "api-id"
	AppInsightsApiKey           = "api-key"
	AppInsightsAddress          = "address"
)

// Provider contains all the required components to run a Applicaion Insights query
// Implements the Provider Interface
type Provider struct {
	logCtx log.Entry
	config appinsightsConfig
}

type appinsightsResponse struct {
	Tables []struct {
		Columns []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}

		Rows [][]interface{} `json:"rows"`
	}
}

type appinsightsConfig struct {
	Address string `yaml:"address,omitempty"`
	ApiId   string `yaml:"api-id,omitempty"`
	ApiKey  string `yaml:"api-key,omitempty"`
}

// Type incidates provider is a Application Insights provider
func (p *Provider) Type() string {
	return ProviderType
}

func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := metav1.Now()

	// Measurement to pass back
	measurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	endpoint := fmt.Sprintf("https://api.applicationinsights.io/v1/apps/%s/query", p.config.ApiId)
	if p.config.Address != "" {
		endpoint = fmt.Sprintf(p.config.Address+"/v1/apps/%s/query", p.config.ApiId)
	}

	url, err := url.Parse(endpoint)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	q := url.Query()
	q.Set("query", metric.Provider.AppInsights.Query)
	url.RawQuery = q.Encode()

	request := &http.Request{Method: "GET"}
	request.URL = url
	request.Header = make(http.Header)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("x-api-key", p.config.ApiKey)

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

func toJSONString(v map[string][]interface{}) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (p *Provider) parseResponse(metric v1alpha1.Metric, response *http.Response) (string, v1alpha1.AnalysisPhase, error) {

	bodyBytes, err := ioutil.ReadAll(response.Body)

	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("received no bytes in response: %v", err)
	}

	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("received authentication error response code: %v %s", response.StatusCode, string(bodyBytes))
	} else if response.StatusCode != http.StatusOK {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("received non 2xx response code: %v %s", response.StatusCode, string(bodyBytes))
	}

	var res appinsightsResponse
	err = json.Unmarshal(bodyBytes, &res)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("could not parse JSON body: %v", err)
	}

	// Handle an empty query result
	if len(res.Tables) == 0 || len(res.Tables[0].Columns) == 0 || len(res.Tables[0].Rows[0]) == 0 {
		var nilFloat64 *float64
		status, err := evaluate.EvaluateResult(nilFloat64, metric, p.logCtx)
		seriesBytes, jsonErr := json.Marshal(res.Tables)
		if jsonErr != nil {
			return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("failed to marshal JSON empty series: %v", jsonErr)
		}

		return string(seriesBytes), status, err
	}

	// Handle a populated query result
	tables := res.Tables[0]
	columns := tables.Columns
	rows := tables.Rows

	result := make(map[string][]interface{}, len(columns))

	// Combine columns with rows
	for _, value := range rows {
		for i := 0; i < len(columns); i++ {
			result[columns[i].Name] = append(result[columns[i].Name], value[i])
		}
	}

	valueStr, err := toJSONString(result)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("could not marshal results: %w", err)
	}
	newStatus, err := evaluate.EvaluateResult(result, metric, p.logCtx)

	return valueStr, newStatus, err
}

// Resume should not be used the Application Insights provider since all the work should occur in the Run method
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Application Insights provider should not execute the Resume method")
	return measurement
}

// Terminate should not be used the Application Insightsdog provider since all the work should occur in the Run method
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Application Insights provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect is a no-op for the Application Insights provider
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	return nil
}

func NewAppInsightsProvider(logCtx log.Entry, kubeclientset kubernetes.Interface, metric v1alpha1.Metric) (*Provider, error) {
	ns := defaults.Namespace()
	profileSecret := AppInsightsTokensSecretName
	if metric.Provider.AppInsights.Profile != "" {
		profileSecret = metric.Provider.AppInsights.Profile
	}

	secret, err := kubeclientset.CoreV1().Secrets(ns).Get(context.TODO(), profileSecret, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	address := ""
	apiId := string(secret.Data[AppInsightsApiId])
	apiKey := string(secret.Data[AppInsightsApiKey])
	if _, hasAddress := secret.Data[AppInsightsAddress]; hasAddress {
		address = string(secret.Data[AppInsightsAddress])
	}

	if apiKey != "" && apiId != "" {
		return &Provider{
			logCtx: logCtx,
			config: appinsightsConfig{
				Address: address,
				ApiId:   apiId,
				ApiKey:  apiKey,
			},
		}, nil
	} else {
		return nil, errors.New("API Id or API Key not found")
	}

}
