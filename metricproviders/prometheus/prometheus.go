package prometheus

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/sigv4"
	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

const (
	// ProviderType indicates the provider is prometheus
	ProviderType = "Prometheus"
	// ResolvedPrometheusQuery is used as the key for storing the resolved prometheus query in the metrics result
	// metadata object.
	ResolvedPrometheusQuery             = "ResolvedPrometheusQuery"
	EnvVarArgoRolloutsPrometheusAddress = "ARGO_ROLLOUTS_PROMETHEUS_ADDRESS"
)

// Provider contains all the required components to run a prometheus query
type Provider struct {
	api     v1.API
	logCtx  log.Entry
	timeout time.Duration
}

// Type indicates provider is a prometheus provider
func (p *Provider) Type() string {
	return ProviderType
}

// GetMetadata returns any additional metadata which needs to be stored & displayed as part of the metrics result.
func (p *Provider) GetMetadata(metric v1alpha1.Metric) map[string]string {
	metricsMetadata := make(map[string]string)
	if metric.Provider.Prometheus.Query != "" {
		metricsMetadata[ResolvedPrometheusQuery] = metric.Provider.Prometheus.Query
	}
	return metricsMetadata
}

// Run queries prometheus for the metric
func (p *Provider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := timeutil.MetaNow()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
	defer cancel()

	response, warnings, err := p.api.Query(ctx, metric.Provider.Prometheus.Query, time.Now())
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	newValue, newStatus, err := p.processResponse(metric, response)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)

	}
	newMeasurement.Value = newValue
	if len(warnings) > 0 {
		warningMetadata := ""
		for _, warning := range warnings {
			warningMetadata = fmt.Sprintf(`%s"%s", `, warningMetadata, warning)
		}
		warningMetadata = warningMetadata[:len(warningMetadata)-2]
		if warningMetadata != "" {
			newMeasurement.Metadata = map[string]string{"warnings": warningMetadata}
			p.logCtx.Warnf("Prometheus returned the following warnings: %s", warningMetadata)
		}
	}

	newMeasurement.Phase = newStatus
	finishedTime := timeutil.MetaNow()
	newMeasurement.FinishedAt = &finishedTime
	return newMeasurement
}

// Resume should not be used the prometheus provider since all the work should occur in the Run method
func (p *Provider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Prometheus provider should not execute the Resume method")
	return measurement
}

// Terminate should not be used the prometheus provider since all the work should occur in the Run method
func (p *Provider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	p.logCtx.Warn("Prometheus provider should not execute the Terminate method")
	return measurement
}

// GarbageCollect is a no-op for the prometheus provider
func (p *Provider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	return nil
}

func (p *Provider) processResponse(metric v1alpha1.Metric, response model.Value) (string, v1alpha1.AnalysisPhase, error) {
	switch value := response.(type) {
	case *model.Scalar:
		valueStr := value.Value.String()
		result := float64(value.Value)
		newStatus, err := evaluate.EvaluateResult(result, metric, p.logCtx)
		return valueStr, newStatus, err
	case model.Vector:
		results := make([]float64, 0, len(value))
		valueStr := "["
		for _, s := range value {
			if s != nil {
				valueStr = valueStr + s.Value.String() + ","
				results = append(results, float64(s.Value))
			}
		}
		// if we appended to the string, we should remove the last comma on the string
		if len(valueStr) > 1 {
			valueStr = valueStr[:len(valueStr)-1]
		}
		valueStr = valueStr + "]"
		newStatus, err := evaluate.EvaluateResult(results, metric, p.logCtx)
		return valueStr, newStatus, err
	//TODO(dthomson) add other response types
	default:
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Prometheus metric type not supported")
	}
}

// NewPrometheusProvider Creates a new Prometheus client
func NewPrometheusProvider(api v1.API, logCtx log.Entry, metric v1alpha1.Metric) (*Provider, error) {
	provider := &Provider{
		logCtx: logCtx,
		api:    api,
	}

	if metric.Provider.Prometheus == nil || metric.Provider.Prometheus.Timeout == nil {
		provider.timeout = 30 * time.Second
		return provider, nil
	}

	metricTimeout := metric.Provider.Prometheus.Timeout

	if *metricTimeout < 0 {
		return nil, errors.New("prometheus timeout should not be negative")
	}

	provider.timeout = time.Duration(*metricTimeout * int64(time.Second))
	return provider, nil
}

// NewPrometheusAPI generates a prometheus API from the metric configuration
func NewPrometheusAPI(metric v1alpha1.Metric) (v1.API, error) {
	envValuesByKey := make(map[string]string)
	if value, ok := os.LookupEnv(fmt.Sprintf("%s", EnvVarArgoRolloutsPrometheusAddress)); ok {
		envValuesByKey[EnvVarArgoRolloutsPrometheusAddress] = value
		log.Debugf("ARGO_ROLLOUTS_PROMETHEUS_ADDRESS: %v", envValuesByKey[EnvVarArgoRolloutsPrometheusAddress])
	}
	if len(metric.Provider.Prometheus.Address) != 0 {
		if !IsUrl(metric.Provider.Prometheus.Address) {
			return nil, errors.New("prometheus address is not is url format")
		}
	} else if envValuesByKey[EnvVarArgoRolloutsPrometheusAddress] != "" {
		if IsUrl(envValuesByKey[EnvVarArgoRolloutsPrometheusAddress]) {
			metric.Provider.Prometheus.Address = envValuesByKey[EnvVarArgoRolloutsPrometheusAddress]
		} else {
			return nil, errors.New("prometheus address is not is url format")
		}
	} else {
		return nil, errors.New("prometheus address is not configured")
	}

	var roundTripper http.RoundTripper

	roundTripper = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: metric.Provider.Prometheus.Insecure},
	}

	// attach custom headers to api requests, if specified
	customHeaders := metric.Provider.Prometheus.Headers
	if len(customHeaders) > 0 {
		roundTripper = httpHeadersRoundTripper{
			headers:      customHeaders,
			roundTripper: roundTripper,
		}
	}

	prometheusApiConfig := api.Config{
		Address:      metric.Provider.Prometheus.Address,
		RoundTripper: roundTripper,
	}

	//Check if using Amazon Managed Prometheus if true build sigv4 client
	if strings.Contains(metric.Provider.Prometheus.Address, "aps-workspaces") {
		cfg := sigv4.SigV4Config{
			Region:  metric.Provider.Prometheus.Authentication.Sigv4.Region,
			Profile: metric.Provider.Prometheus.Authentication.Sigv4.Profile,
			RoleARN: metric.Provider.Prometheus.Authentication.Sigv4.RoleARN,
		}
		var next http.RoundTripper
		sigv4RoundTripper, err := sigv4.NewSigV4RoundTripper(&cfg, next)
		if err != nil {
			log.Errorf("Error creating SigV4 RoundTripper: %v", err)
			return nil, err
		}
		prometheusApiConfig.RoundTripper = sigv4RoundTripper
	}

	client, err := api.NewClient(prometheusApiConfig)
	if err != nil {
		log.Errorf("Error in getting prometheus client: %v", err)
		return nil, err
	}
	return v1.NewAPI(client), nil
}

func IsUrl(str string) bool {
	u, err := url.Parse(str)
	if err != nil {
		log.Errorf("Error in parsing url: %v", err)
	}
	log.Debugf("Parsed url: %v", u)
	return err == nil && u.Scheme != "" && u.Host != ""
}
