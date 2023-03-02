package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/argoproj/argo-rollouts/metricproviders/plugin/rpc"

	"github.com/argoproj/argo-rollouts/utils/plugin/types"

	"github.com/argoproj/argo-rollouts/metricproviders/plugin"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/evaluate"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
)

const EnvVarArgoRolloutsPrometheusAddress string = "ARGO_ROLLOUTS_PROMETHEUS_ADDRESS"

var _ rpc.MetricProviderPlugin = &RpcPlugin{}

// Here is a real implementation of MetricProviderPlugin
type RpcPlugin struct {
	LogCtx log.Entry
}

type Config struct {
	// Address is the HTTP address and port of the prometheus server
	Address string `json:"address,omitempty" protobuf:"bytes,1,opt,name=address"`
	// Query is a raw prometheus query to perform
	Query string `json:"query,omitempty" protobuf:"bytes,2,opt,name=query"`
}

func (g *RpcPlugin) InitPlugin() types.RpcError {
	return types.RpcError{}
}

func (g *RpcPlugin) Run(anaysisRun *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	startTime := timeutil.MetaNow()
	newMeasurement := v1alpha1.Measurement{
		StartedAt: &startTime,
	}

	config := Config{}
	err := json.Unmarshal(metric.Provider.Plugin["argoproj/sample-prometheus"], &config)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	api, err := newPrometheusAPI(config.Address)
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	response, warnings, err := api.Query(ctx, config.Query, time.Now())
	if err != nil {
		return metricutil.MarkMeasurementError(newMeasurement, err)
	}

	newValue, newStatus, err := g.processResponse(metric, response)
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
			g.LogCtx.Warnf("Prometheus returned the following warnings: %s", warningMetadata)
		}
	}

	newMeasurement.Phase = newStatus
	finishedTime := timeutil.MetaNow()
	newMeasurement.FinishedAt = &finishedTime
	return newMeasurement
}

func (g *RpcPlugin) Resume(analysisRun *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	return measurement
}

func (g *RpcPlugin) Terminate(analysisRun *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	return measurement
}

func (g *RpcPlugin) GarbageCollect(*v1alpha1.AnalysisRun, v1alpha1.Metric, int) types.RpcError {
	return types.RpcError{}
}

func (g *RpcPlugin) Type() string {
	return plugin.ProviderType
}

func (g *RpcPlugin) GetMetadata(metric v1alpha1.Metric) map[string]string {
	metricsMetadata := make(map[string]string)

	config := Config{}
	json.Unmarshal(metric.Provider.Plugin["argoproj/sample-prometheus"], &config)
	if config.Query != "" {
		metricsMetadata["ResolvedPrometheusQuery"] = config.Query
	}
	return metricsMetadata
}

func (g *RpcPlugin) processResponse(metric v1alpha1.Metric, response model.Value) (string, v1alpha1.AnalysisPhase, error) {
	switch value := response.(type) {
	case *model.Scalar:
		valueStr := value.Value.String()
		result := float64(value.Value)
		newStatus, err := evaluate.EvaluateResult(result, metric, g.LogCtx)
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
		newStatus, err := evaluate.EvaluateResult(results, metric, g.LogCtx)
		return valueStr, newStatus, err
	default:
		return "", v1alpha1.AnalysisPhaseError, fmt.Errorf("Prometheus metric type not supported")
	}
}

func newPrometheusAPI(address string) (v1.API, error) {
	envValuesByKey := make(map[string]string)
	if value, ok := os.LookupEnv(fmt.Sprintf("%s", EnvVarArgoRolloutsPrometheusAddress)); ok {
		envValuesByKey[EnvVarArgoRolloutsPrometheusAddress] = value
		log.Debugf("ARGO_ROLLOUTS_PROMETHEUS_ADDRESS: %v", envValuesByKey[EnvVarArgoRolloutsPrometheusAddress])
	}
	if len(address) != 0 {
		if !isUrl(address) {
			return nil, errors.New("prometheus address is not is url format")
		}
	} else if envValuesByKey[EnvVarArgoRolloutsPrometheusAddress] != "" {
		if isUrl(envValuesByKey[EnvVarArgoRolloutsPrometheusAddress]) {
			address = envValuesByKey[EnvVarArgoRolloutsPrometheusAddress]
		} else {
			return nil, errors.New("prometheus address is not is url format")
		}
	} else {
		return nil, errors.New("prometheus address is not configured")
	}
	client, err := api.NewClient(api.Config{
		Address: address,
	})
	if err != nil {
		log.Errorf("Error in getting prometheus client: %v", err)
		return nil, err
	}
	return v1.NewAPI(client), nil
}

func isUrl(str string) bool {
	u, err := url.Parse(str)
	if err != nil {
		log.Errorf("Error in parsing url: %v", err)
	}
	log.Debugf("Parsed url: %v", u)
	return err == nil && u.Scheme != "" && u.Host != ""
}
