package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutlister "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
)

// RolloutPluginPhase the phases of a rolloutplugin reconcile can have
type RolloutPluginPhase string

const (
	// RolloutPluginProgressing means the rolloutplugin is progressing through steps
	RolloutPluginProgressing RolloutPluginPhase = "Progressing"
	// RolloutPluginPaused means the rolloutplugin is paused
	RolloutPluginPaused RolloutPluginPhase = "Paused"
	// RolloutPluginHealthy means the rolloutplugin has successfully completed
	RolloutPluginHealthy RolloutPluginPhase = "Healthy"
	// RolloutPluginDegraded means the rolloutplugin has encountered issues
	RolloutPluginDegraded RolloutPluginPhase = "Degraded"
	// RolloutPluginError means the rolloutplugin has encountered an error
	RolloutPluginError RolloutPluginPhase = "Error"
	// RolloutPluginTimeout means the rolloutplugin has timed out
	RolloutPluginTimeout RolloutPluginPhase = "Timeout"
	// RolloutPluginAborted means the rolloutplugin was aborted
	RolloutPluginAborted RolloutPluginPhase = "Aborted"
)

type rolloutPluginCollector struct {
	store rolloutlister.RolloutPluginLister
}

// NewRolloutPluginCollector returns a prometheus collector for rolloutplugin metrics
func NewRolloutPluginCollector(rolloutPluginLister rolloutlister.RolloutPluginLister) prometheus.Collector {
	return &rolloutPluginCollector{
		store: rolloutPluginLister,
	}
}

// Describe implements the prometheus.Collector interface
func (c *rolloutPluginCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- MetricRolloutPluginInfo
}

// Collect implements the prometheus.Collector interface
func (c *rolloutPluginCollector) Collect(ch chan<- prometheus.Metric) {
	rolloutPlugins, err := c.store.List(labels.NewSelector())
	if err != nil {
		log.Warnf("Failed to collect rolloutplugins: %v", err)
		return
	}
	for _, rp := range rolloutPlugins {
		collectRolloutPlugins(ch, rp)
	}
}

// calculatePhase determines the phase of a RolloutPlugin based on its status
func calculateRolloutPluginPhase(rp *v1alpha1.RolloutPlugin) RolloutPluginPhase {
	// Use the phase from status if available
	if rp.Status.Phase != "" {
		// Map the status phase to our metric phase constants
		switch rp.Status.Phase {
		case "Progressing":
			return RolloutPluginProgressing
		case "Paused":
			return RolloutPluginPaused
		case "Healthy", "Successful", "Completed":
			return RolloutPluginHealthy
		case "Degraded":
			return RolloutPluginDegraded
		case "Error", "Failed":
			return RolloutPluginError
		case "Timeout", "TimedOut":
			return RolloutPluginTimeout
		case "Aborted":
			return RolloutPluginAborted
		default:
			return RolloutPluginProgressing
		}
	}

	// Default to Progressing if no phase is set
	return RolloutPluginProgressing
}

// getStrategyType extracts the strategy type from RolloutPlugin spec
func getRolloutPluginStrategyType(rp *v1alpha1.RolloutPlugin) string {
	// Only Canary strategy is supported for RolloutPlugin
	if rp.Spec.Strategy.Canary != nil {
		return "Canary"
	}
	return "none"
}

func collectRolloutPlugins(ch chan<- prometheus.Metric, rp *v1alpha1.RolloutPlugin) {
	strategyType := getRolloutPluginStrategyType(rp)
	pluginName := rp.Spec.Plugin.Name
	workloadKind := rp.Spec.WorkloadRef.Kind
	calculatedPhase := calculateRolloutPluginPhase(rp)

	addGauge := func(desc *prometheus.Desc, v float64, lv ...string) {
		lv = append([]string{rp.Namespace, rp.Name}, lv...)
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v, lv...)
	}

	// Main info metric with labels
	addGauge(MetricRolloutPluginInfo, 1, strategyType, pluginName, workloadKind, string(calculatedPhase))

	// Workload replica metrics
	addGauge(MetricRolloutPluginWorkloadReplicasDesired, float64(rp.Status.Replicas))
	addGauge(MetricRolloutPluginWorkloadReplicasUpdated, float64(rp.Status.UpdatedReplicas))
	addGauge(MetricRolloutPluginWorkloadReplicasReady, float64(rp.Status.ReadyReplicas))

}
