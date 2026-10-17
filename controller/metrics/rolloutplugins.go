package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutlister "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

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

// Determines the phase of a RolloutPlugin from conditions
func calculateRolloutPluginPhase(rp *v1alpha1.RolloutPlugin) RolloutPluginPhase {
	phase := RolloutPluginProgressing

	progressing := conditions.GetRolloutPluginCondition(rp.Status, v1alpha1.RolloutPluginConditionProgressing)
	if progressing != nil {
		if progressing.Reason == conditions.RolloutPluginPausedReason {
			phase = RolloutPluginPaused
		}
		if progressing.Reason == conditions.RolloutPluginReconciliationErrorReason || progressing.Reason == conditions.RolloutPluginAnalysisRunFailedReason {
			phase = RolloutPluginError
		}
		if progressing.Reason == conditions.RolloutPluginTimedOutReason {
			phase = RolloutPluginTimeout
		}
		if progressing.Reason == conditions.RolloutPluginAbortedReason {
			phase = RolloutPluginAborted
		}
	}

	invalidSpec := conditions.GetRolloutPluginCondition(rp.Status, v1alpha1.RolloutPluginConditionInvalidSpec)
	if invalidSpec != nil && invalidSpec.Status == corev1.ConditionTrue {
		phase = RolloutPluginError
	}

	completedCond := conditions.GetRolloutPluginCondition(rp.Status, v1alpha1.RolloutPluginConditionCompleted)
	if completedCond != nil && completedCond.Status == corev1.ConditionTrue {
		phase = RolloutPluginHealthy
	}

	return phase
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

	addGauge(MetricRolloutPluginInfo, 1, strategyType, pluginName, workloadKind, string(calculatedPhase))

	addGauge(MetricRolloutPluginWorkloadReplicasDesired, float64(rp.Status.Replicas))
	addGauge(MetricRolloutPluginWorkloadReplicasUpdated, float64(rp.Status.UpdatedReplicas))
	addGauge(MetricRolloutPluginWorkloadReplicasReady, float64(rp.Status.ReadyReplicas))

}
