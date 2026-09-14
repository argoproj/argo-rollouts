package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutlister "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

// RolloutPhase the phases of a reconcile can have
type RolloutPhase string

const (

	// RolloutInvalidSpec means the rollout had an invalid spec during reconciliation
	RolloutInvalidSpec RolloutPhase = "InvalidSpec"
	// RolloutCompleted means the rollout finished the reconciliation with no remaining work
	RolloutCompleted RolloutPhase = "Completed"
	// RolloutProgressing means the rollout finished the reconciliation with remaining work
	RolloutProgressing RolloutPhase = "Progressing"
	// RolloutPaused means the rollout finished the reconciliation with a paused status
	RolloutPaused RolloutPhase = "Paused"
	// RolloutTimeout means the rollout finished the reconciliation with an timeout message
	RolloutTimeout RolloutPhase = "Timeout"
	// RolloutError means the rollout finished the reconciliation with an error
	RolloutError RolloutPhase = "Error"
	// RolloutAbort means the rollout finished the reconciliation in an aborted state
	RolloutAbort RolloutPhase = "Abort"
)

type rolloutCollector struct {
	store rolloutlister.RolloutLister
}

// NewRolloutCollector returns a prometheus collector for rollout metrics
func NewRolloutCollector(rolloutLister rolloutlister.RolloutLister) prometheus.Collector {
	return &rolloutCollector{
		store: rolloutLister,
	}
}

// Describe implements the prometheus.Collector interface
func (c *rolloutCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- MetricRolloutInfo
}

// Collect implements the prometheus.Collector interface
func (c *rolloutCollector) Collect(ch chan<- prometheus.Metric) {
	rollouts, err := c.store.List(labels.NewSelector())
	if err != nil {
		log.Warnf("Failed to collect rollouts: %v", err)
		return
	}
	for _, rollout := range rollouts {
		collectRollouts(ch, rollout)
	}
}

// calculatePhase calculates where a Rollout is in a RolloutCompleted, Paused, Error, Timeout, InvalidSpec, or ABorted phase
func calculatePhase(rollout *v1alpha1.Rollout) RolloutPhase {
	phase := RolloutProgressing
	progressing := conditions.GetRolloutCondition(rollout.Status, v1alpha1.RolloutProgressing)
	if progressing != nil {
		if progressing.Reason == conditions.NewRSAvailableReason {
			phase = RolloutCompleted
		}
		if progressing.Reason == conditions.RolloutPausedReason {
			phase = RolloutPaused
		}
		if progressing.Reason == conditions.FailedRSCreateReason {
			phase = RolloutError
		}
		if progressing.Reason == conditions.TimedOutReason {
			phase = RolloutTimeout
		}
		if progressing.Reason == conditions.RolloutAbortedReason {
			phase = RolloutAbort
		}
	}
	invalidSpec := conditions.GetRolloutCondition(rollout.Status, v1alpha1.InvalidSpec)
	if invalidSpec != nil {
		phase = RolloutInvalidSpec
	}
	return phase
}

func getStrategyAndTrafficRouter(rollout *v1alpha1.Rollout) (string, string) {
	strategy := "none"
	trafficRouter := ""
	if rollout.Spec.Strategy.BlueGreen != nil {
		strategy = "blueGreen"
	} else if rollout.Spec.Strategy.Canary != nil {
		strategy = "canary"
		if rollout.Spec.Strategy.Canary.TrafficRouting != nil {
			if rollout.Spec.Strategy.Canary.TrafficRouting.ALB != nil {
				trafficRouter = "ALB"
			}
			if rollout.Spec.Strategy.Canary.TrafficRouting.Ambassador != nil {
				trafficRouter = "Ambassador"
			}
			if rollout.Spec.Strategy.Canary.TrafficRouting.Istio != nil {
				trafficRouter = "Istio"
			}
			if rollout.Spec.Strategy.Canary.TrafficRouting.Nginx != nil {
				trafficRouter = "Nginx"
			}
			if rollout.Spec.Strategy.Canary.TrafficRouting.SMI != nil {
				trafficRouter = "SMI"
			}
			if rollout.Spec.Strategy.Canary.TrafficRouting.AppMesh != nil {
				trafficRouter = "AppMesh"
			}
		}
	}
	return strategy, trafficRouter
}

func collectRollouts(ch chan<- prometheus.Metric, rollout *v1alpha1.Rollout) {
	strategyType, trafficRouter := getStrategyAndTrafficRouter(rollout)
	calculatedPhase := calculatePhase(rollout)

	addGauge := func(desc *prometheus.Desc, v float64, lv ...string) {
		lv = append([]string{rollout.Namespace, rollout.Name}, lv...)
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v, lv...)
	}
	addGauge(MetricRolloutInfo, 1, strategyType, trafficRouter, string(calculatedPhase))
	addGauge(MetricRolloutInfoReplicasAvailable, float64(rollout.Status.AvailableReplicas))
	addGauge(MetricRolloutInfoReplicasUnavailable, float64(rollout.Status.Replicas-rollout.Status.AvailableReplicas))
	addGauge(MetricRolloutInfoReplicasDesired, float64(defaults.GetReplicasOrDefault(rollout.Spec.Replicas)))
	addGauge(MetricRolloutInfoReplicasUpdated, float64(rollout.Status.UpdatedReplicas))

	// DEPRECATED
	addGauge(MetricRolloutPhase, boolFloat64(calculatedPhase == RolloutCompleted), strategyType, string(RolloutCompleted))
	addGauge(MetricRolloutPhase, boolFloat64(calculatedPhase == RolloutProgressing), strategyType, string(RolloutProgressing))
	addGauge(MetricRolloutPhase, boolFloat64(calculatedPhase == RolloutPaused), strategyType, string(RolloutPaused))
	addGauge(MetricRolloutPhase, boolFloat64(calculatedPhase == RolloutTimeout), strategyType, string(RolloutTimeout))
	addGauge(MetricRolloutPhase, boolFloat64(calculatedPhase == RolloutError), strategyType, string(RolloutError))
	addGauge(MetricRolloutPhase, boolFloat64(calculatedPhase == RolloutAbort), strategyType, string(RolloutAbort))
}
