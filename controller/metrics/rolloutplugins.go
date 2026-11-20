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
	// RolloutPluginError means the rolloutplugin has encountered an error
	RolloutPluginError RolloutPluginPhase = "Error"
	// RolloutPluginTimeout means the rolloutplugin has timed out
	RolloutPluginTimeout RolloutPluginPhase = "Timeout"
	// RolloutPluginAborted means the rolloutplugin was aborted
	RolloutPluginAborted RolloutPluginPhase = "Aborted"
	// RolloutPluginInvalidSpec means the rolloutplugin has an invalid spec
	RolloutPluginInvalidSpec RolloutPluginPhase = "InvalidSpec"
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
	ch <- MetricRolloutPluginWorkloadReplicasDesired
	ch <- MetricRolloutPluginWorkloadReplicasUpdated
	ch <- MetricRolloutPluginWorkloadReplicasReady
	ch <- MetricRolloutPluginWorkloadReplicasAvailable
	ch <- MetricRolloutPluginWorkloadReplicasUnavailable
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

// calculateRolloutPluginPhase returns a more detailed phase than status.phase, in the same way as
// the Rollout metrics. Later checks take priority: Healthy follows status.phase, and InvalidSpec wins.
func calculateRolloutPluginPhase(rp *v1alpha1.RolloutPlugin) RolloutPluginPhase {
	phase := RolloutPluginProgressing

	progressing := conditions.GetRolloutPluginCondition(rp.Status, v1alpha1.RolloutPluginConditionProgressing)
	if progressing != nil {
		switch progressing.Reason {
		case conditions.RolloutPluginPausedReason:
			phase = RolloutPluginPaused
		case conditions.RolloutPluginReconciliationErrorReason, conditions.RolloutPluginAnalysisRunFailedReason:
			phase = RolloutPluginError
		case conditions.RolloutPluginTimedOutReason:
			phase = RolloutPluginTimeout
		case conditions.RolloutPluginAbortedReason:
			phase = RolloutPluginAborted
		}
	}

	if rp.Status.Phase == v1alpha1.RolloutPluginPhaseHealthy {
		phase = RolloutPluginHealthy
	}

	invalidSpec := conditions.GetRolloutPluginCondition(rp.Status, v1alpha1.RolloutPluginConditionInvalidSpec)
	if invalidSpec != nil && invalidSpec.Status == corev1.ConditionTrue {
		phase = RolloutPluginInvalidSpec
	}

	return phase
}

// getStrategyType extracts the strategy type from RolloutPlugin spec
func getRolloutPluginStrategyType(rp *v1alpha1.RolloutPlugin) string {
	// Only Canary strategy is supported for RolloutPlugin
	if rp.Spec.Strategy.Canary != nil {
		return "canary"
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

	addGauge(MetricRolloutPluginWorkloadReplicasDesired, float64(rp.Status.DesiredReplicas))
	addGauge(MetricRolloutPluginWorkloadReplicasUpdated, float64(rp.Status.UpdatedReplicas))
	addGauge(MetricRolloutPluginWorkloadReplicasReady, float64(rp.Status.ReadyReplicas))
	addGauge(MetricRolloutPluginWorkloadReplicasAvailable, float64(rp.Status.AvailableReplicas))
	addGauge(MetricRolloutPluginWorkloadReplicasUnavailable, float64(max(rp.Status.DesiredReplicas-rp.Status.AvailableReplicas, 0)))
}
