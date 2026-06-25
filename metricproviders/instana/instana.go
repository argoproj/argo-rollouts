package instana

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

const (
	// ProviderType indicates the provider is Instana
	ProviderType = "Instana"

	// InstanaTokensSecretName is the default secret name for Instana credentials
	InstanaTokensSecretName = "instana"

	// InstanaAddress is the key for the Instana base URL in the secret
	InstanaAddress = "endpoint"

	// InstanaAPIToken is the key for the Instana API token in the secret
	InstanaAPIToken = "api-token"

	// MetricTypeApplication queries application monitoring metrics (call throughput, latency, errors)
	MetricTypeApplication = "application"

	// MetricTypeInfrastructure queries infrastructure monitoring metrics (host, JVM, process, etc.)
	MetricTypeInfrastructure = "infrastructure"

	// defaultRollupInterval is the default aggregation window in seconds
	defaultRollupInterval int32 = 60

	// defaultWindowSize is the time window (in milliseconds) we look back for metrics
	defaultWindowSize int64 = 60_000
)

// Provider implements the metric.Provider interface for IBM Instana
type Provider struct {
	logCtx log.Entry
	config instanaConfig
}

type instanaConfig struct {
	Endpoint string
	APIToken string
}

// ---------------------------------------------------------------------------
// Instana REST API request / response types
// ---------------------------------------------------------------------------

// applicationMetricRequest is the body for POST /api/application-monitoring/metrics/applications
type applicationMetricRequest struct {
	TimeFrame  timeFrame  `json:"timeFrame"`
	Metrics    []metric   `json:"metrics"`
	TagFilters []tagFilter `json:"tagFilters,omitempty"`
	GroupBy    []string   `json:"groupBy,omitempty"`
}

// infrastructureMetricRequest is the body for POST /api/infrastructure-monitoring/metrics
type infrastructureMetricRequest struct {
	TimeFrame  timeFrame  `json:"timeFrame"`
	Metrics    []metric   `json:"metrics"`
	Query      string     `json:"query,omitempty"`
}

type timeFrame struct {
	To       int64 `json:"to"`
	Duration int64 `json:"duration"`
}

type metric struct {
	Metric      string `json:"metric"`
	Aggregation string `json:"aggregation"`
	Granularity int32  `json:"granularity"`
}

type tagFilter struct {
	Name     string `json:"name"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// applicationMetricResponse is the response from the application monitoring API
type applicationMetricResponse struct {
	Items []struct {
		Metrics map[string][][2]*float64 `json:"metrics"`
	} `json:"items"`
}

// infrastructureMetricResponse is the response from the infrastructure monitoring API
type infrastructureMetricResponse struct {
	Items []struct {
		Metrics map[string][][2]*float64 `json:"metrics"`
	} `json:"items"`
}

// ---------------------------------------------------------------------------
// metric.Provider interface
// ---------------------------------------------------------------------------

// Type returns the provider type string
func (p *Provider) Type() string {
	return ProviderType
}

// GetMetadata returns any additional metadata to store with a measurement
func (p *Provider) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return nil
}

// Run executes the Instana metric query and returns a measurement
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := timeutil.MetaNow()
	measurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	instanaMetric := metric.Provider.Instana

	// Validate the metric configuration
	if err := validateMetric(instanaMetric); err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	// Determine aggregation, defaulting to "mean"
	aggregation := instanaMetric.Aggregation
	if aggregation == "" {
		aggregation = "mean"
	}

	// Determine rollup interval, defaulting to 60s
	rollupInterval := instanaMetric.RollupInterval
	if rollupInterval <= 0 {
		rollupInterval = defaultRollupInterval
	}

	// Build time frame: look back (rollupInterval) seconds from now
	nowMs := timeutil.Now().UnixNano() / int64(time.Millisecond)
	windowMs := int64(rollupInterval) * 1000

	var (
		value  string
		status v1alpha1.AnalysisPhase
		err    error
	)

	switch instanaMetric.MetricType {
	case MetricTypeApplication:
		value, status, err = p.queryApplicationMetrics(instanaMetric, nowMs, windowMs, aggregation, rollupInterval, metric)
	case MetricTypeInfrastructure:
		value, status, err = p.queryInfrastructureMetrics(instanaMetric, nowMs, windowMs, aggregation, rollupInterval, metric)
	default:
		err = fmt.Errorf("unsupported metricType %q: must be %q or %q", instanaMetric.MetricType, MetricTypeApplication, MetricTypeInfrastructure)
	}

	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}

	measurement.Value = value
	measurement.Phase = status
	finishedTime := timeutil.MetaNow()
	measurement.FinishedAt = &finishedTime
	return measurement
}

// queryApplicationMetrics calls the Instana application monitoring metrics API
func (p *Provider) queryApplicationMetrics(
	instanaMetric *v1alpha1.InstanaMetric,
	nowMs, windowMs int64,
	aggregation string,
	rollupInterval int32,
	m v1alpha1.Metric,
) (string, v1alpha1.AnalysisPhase, error) {
	reqBody := applicationMetricRequest{
		TimeFrame: timeFrame{
			To:       nowMs,
			Duration: windowMs,
		},
		Metrics: []metric{
			{
				Metric:      instanaMetric.MetricID,
				Aggregation: aggregation,
				Granularity: rollupInterval,
			},
		},
	}

	// Use the Dynamic Focus query as a tag filter if set
	if instanaMetric.Query != "" {
		reqBody.TagFilters = []tagFilter{
			{
				Name:     "dynamic.focus.query",
				Operator: "EQUALS",
				Value:    instanaMetric.Query,
			},
		}
	}

	url := p.config.Endpoint + "/api/application-monitoring/metrics/applications"
	bodyBytes, err := p.doPost(url, reqBody)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, err
	}

	var resp applicationMetricResponse
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("could not parse Instana application metrics response: %v", err)
	}

	return extractValue(resp.Items, instanaMetric.MetricID, m, p.logCtx)
}

// queryInfrastructureMetrics calls the Instana infrastructure monitoring metrics API
func (p *Provider) queryInfrastructureMetrics(
	instanaMetric *v1alpha1.InstanaMetric,
	nowMs, windowMs int64,
	aggregation string,
	rollupInterval int32,
	m v1alpha1.Metric,
) (string, v1alpha1.AnalysisPhase, error) {
	reqBody := infrastructureMetricRequest{
		TimeFrame: timeFrame{
			To:       nowMs,
			Duration: windowMs,
		},
		Metrics: []metric{
			{
				Metric:      instanaMetric.MetricID,
				Aggregation: aggregation,
				Granularity: rollupInterval,
			},
		},
		Query: instanaMetric.Query,
	}

	url := p.config.Endpoint + "/api/infrastructure-monitoring/metrics"
	bodyBytes, err := p.doPost(url, reqBody)
	if err != nil {
		return "", v1alpha1.AnalysisPhaseError, err
	}

	// Infrastructure response has the same shape as application response
	var resp infrastructureMetricResponse
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("could not parse Instana infrastructure metrics response: %v", err)
	}

	// Convert to common item shape for extraction
	items := make([]struct {
		Metrics map[string][][2]*float64 `json:"metrics"`
	}, len(resp.Items))
	copy(items, resp.Items)

	return extractValue(items, instanaMetric.MetricID, m, p.logCtx)
}

// doPost sends an authenticated POST request to the Instana API and returns the response body
func (p *Provider) doPost(url string, payload any) ([]byte, error) {
	bodyJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("could not marshal request body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("could not build HTTP request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "apiToken "+p.config.APIToken)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request to Instana failed: %v", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Instana response body: %v", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("received authentication error response code %d from Instana: %s", resp.StatusCode, string(respBytes))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("received non-2xx response code %d from Instana: %s", resp.StatusCode, string(respBytes))
	}

	return respBytes, nil
}

// extractValue extracts the most recent data point from the Instana metric response items
func extractValue(
	items []struct {
		Metrics map[string][][2]*float64 `json:"metrics"`
	},
	metricID string,
	m v1alpha1.Metric,
	logCtx log.Entry,
) (string, v1alpha1.AnalysisPhase, error) {
	// Handle empty result
	if len(items) == 0 {
		var nilFloat64 *float64
		status, err := evaluate.EvaluateResult(nilFloat64, m, logCtx)
		return "[]", status, err
	}

	// Locate the series for our metricID in the first (aggregated) item
	series, ok := items[0].Metrics[metricID]
	if !ok || len(series) == 0 {
		var nilFloat64 *float64
		status, err := evaluate.EvaluateResult(nilFloat64, m, logCtx)
		return "[]", status, err
	}

	// Use the last data point; each point is [timestamp, value]
	lastPoint := series[len(series)-1]
	if lastPoint[1] == nil {
		var nilFloat64 *float64
		status, err := evaluate.EvaluateResult(nilFloat64, m, logCtx)
		return "null", status, err
	}

	val := *lastPoint[1]
	status, err := evaluate.EvaluateResult(val, m, logCtx)
	return strconv.FormatFloat(val, 'f', -1, 64), status, err
}

// Resume is a no-op — Instana provider does all work in Run
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Instana provider should not execute the Resume method")
	return measurement
}

// Terminate is a no-op — Instana provider does all work in Run
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Instana provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect is a no-op for the Instana provider
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	return nil
}

// validateMetric validates the required fields of an InstanaMetric
func validateMetric(instanaMetric *v1alpha1.InstanaMetric) error {
	if instanaMetric.MetricID == "" {
		return errors.New("instana: metricId is required")
	}
	if instanaMetric.MetricType == "" {
		return errors.New("instana: metricType is required (\"application\" or \"infrastructure\")")
	}
	if instanaMetric.MetricType != MetricTypeApplication && instanaMetric.MetricType != MetricTypeInfrastructure {
		return fmt.Errorf("instana: metricType %q is invalid, must be %q or %q", instanaMetric.MetricType, MetricTypeApplication, MetricTypeInfrastructure)
	}
	return nil
}

// NewInstanaProvider creates a new Instana metric provider
func NewInstanaProvider(logCtx log.Entry, kubeclientset kubernetes.Interface, namespace string, metric v1alpha1.Metric) (*Provider, error) {
	endpoint, apiToken, err := findCredentials(logCtx, kubeclientset, namespace, metric)
	if err != nil {
		return nil, err
	}

	if endpoint == "" || apiToken == "" {
		return nil, errors.New("instana: endpoint and api-token must be provided")
	}

	if err := validateMetric(metric.Provider.Instana); err != nil {
		return nil, err
	}

	return &Provider{
		logCtx: logCtx,
		config: instanaConfig{
			Endpoint: endpoint,
			APIToken: apiToken,
		},
	}, nil
}

// findCredentials resolves Instana credentials using the following precedence:
//  1. Kubernetes secret referenced by name in the metric spec
//  2. Environment variables (INSTANA_ENDPOINT, INSTANA_API_TOKEN)
//  3. Default Kubernetes secret named "instana" in the argo-rollouts namespace
func findCredentials(logCtx log.Entry, kubeclientset kubernetes.Interface, namespace string, metric v1alpha1.Metric) (string, string, error) {
	secretName := metric.Provider.Instana.SecretRef.Name
	namespaced := metric.Provider.Instana.SecretRef.Namespaced
	credentialsNs := defaults.Namespace()

	if namespaced {
		credentialsNs = namespace
		if secretName == "" {
			return "", "", errors.New("instana: secret name is required when namespaced is true")
		}
	}

	finders := []credentialsFinder{}
	if secretName != "" {
		finders = append(finders, newSecretFinder(kubeclientset, secretName, credentialsNs))
	} else {
		finders = append(finders, newEnvVarFinder(), newSecretFinder(kubeclientset, InstanaTokensSecretName, defaults.Namespace()))
	}

	for _, finder := range finders {
		endpoint, apiToken := finder.findCredentials(logCtx)
		if endpoint != "" && apiToken != "" {
			return endpoint, apiToken, nil
		}
	}

	return "", "", errors.New("instana: failed to find credentials; provide a Kubernetes secret or environment variables INSTANA_ENDPOINT and INSTANA_API_TOKEN")
}
