package prometheus

import (
	"context"
	"errors"
	"fmt"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
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
	ResolvedPrometheusQuery = "ResolvedPrometheusQuery"
	TokensSecretName = "prometheus"
	Address          = "PROMETHEUS_ADDRESS"
)

// Provider contains all the required components to run a prometheus query
type Provider struct {
	api    v1.API
	logCtx log.Entry
	config prometheusConfig
}

type prometheusConfig struct {
	Address string `yaml:"address,omitempty"`
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

	//TODO(dthomson) make timeout configurable
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

//NewPrometheusProvider Creates a new Prometheus client
func NewPrometheusProvider(api v1.API, logCtx log.Entry) *Provider {
	return &Provider{
		logCtx: logCtx,
		api:    api,
	}
}

// NewPrometheusAPI generates a prometheus API from the metric configuration
func NewPrometheusAPI(metric v1alpha1.Metric, kubeclientset kubernetes.Interface) (v1.API, error) {
	ns := defaults.Namespace()
	secretKeys := []string{Address}
	envValuesByKey := lookupKeysInEnv(secretKeys)
	log.Infof("envValuesByKey: %v, len: %v", envValuesByKey, len(envValuesByKey))
	if envValuesByKey[Address] != "" {
		if IsUrl(envValuesByKey[Address]) {
			metric.Provider.Prometheus.Address = envValuesByKey[Address]
		} else {
			log.Errorf("prometheus address (envvar)  is not is url format: %v", envValuesByKey[Address])
			return nil, errors.New("prometheus address is not is url format")
		}
	} else {
		secret, err := kubeclientset.CoreV1().Secrets(ns).Get(context.TODO(), TokensSecretName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if _, hasAddress := secret.Data[Address]; hasAddress {
			if IsUrl(string(secret.Data[Address])) {
				metric.Provider.Prometheus.Address = string(secret.Data[Address])
			} else {
				log.Errorf("prometheus address (secret) is not is url format: %v", string(secret.Data[Address]))
				return nil, errors.New("prometheus address is not is url format")
			}
		}
	}
	log.Infof("metric.Provider.Prometheus.Address: %v", metric.Provider.Prometheus.Address)
	client, err := api.NewClient(api.Config{
		Address: metric.Provider.Prometheus.Address,
	})
	if err != nil {
		log.Errorf("Error in getting prometheus client: %v", err)
		return nil, err
	}
	return v1.NewAPI(client), nil
}

func IsUrl(str string) bool {
	u, err := url.Parse(str)
	if err != nil{
		log.Errorf("Error in parsing url: %v", err)
	}
	log.Infof("Parsed url: %v", u)
	return err == nil && u.Scheme != "" && u.Host != ""
}

func lookupKeysInEnv(keys []string) map[string]string {
	valuesByKey := make(map[string]string)
	for i := range keys {
		key := keys[i]
		formattedKey := strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
		log.Infof("formattedKey: %v", formattedKey)
		if value, ok := os.LookupEnv(fmt.Sprintf("%s", formattedKey)); ok {
			valuesByKey[key] = value
			log.Infof("PROMETHEUS_ADDRESS: %v", value)
		}
	}
	return valuesByKey
}
