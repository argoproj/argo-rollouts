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

var (
	descRolloutWithStrategyLabels = append(descDefaultLabels, "strategy")

	descRolloutReconcilePhaseLabels = append(descRolloutWithStrategyLabels, "phase")

	descRolloutInfo = prometheus.NewDesc(
		"rollout_info",
		"Information about rollout.",
		descRolloutWithStrategyLabels,
		nil,
	)

	descRolloutCreated = prometheus.NewDesc(
		"rollout_created_time",
		"Creation time in unix timestamp for an rollout.",
		descRolloutWithStrategyLabels,
		nil,
	)

	descRolloutPhaseLabels = prometheus.NewDesc(
		"rollout_phase",
		"Information on the state of the rollout",
		descRolloutReconcilePhaseLabels,
		nil,
	)
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
	ch <- descRolloutInfo
	ch <- descRolloutCreated
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
		if progressing.Reason == conditions.PausedRolloutReason {
			phase = RolloutPaused
		}
		if progressing.Reason == conditions.ServiceNotFoundReason || progressing.Reason == conditions.FailedRSCreateReason {
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

func collectRollouts(ch chan<- prometheus.Metric, rollout *v1alpha1.Rollout) {

	addConstMetric := func(desc *prometheus.Desc, t prometheus.ValueType, v float64, lv ...string) {
		lv = append([]string{rollout.Namespace, rollout.Name, defaults.GetStrategyType(rollout)}, lv...)
		ch <- prometheus.MustNewConstMetric(desc, t, v, lv...)
	}
	addGauge := func(desc *prometheus.Desc, v float64, lv ...string) {
		addConstMetric(desc, prometheus.GaugeValue, v, lv...)
	}

	addGauge(descRolloutInfo, 1)

	addGauge(descRolloutCreated, float64(rollout.CreationTimestamp.Unix()))

	calculatedPhase := calculatePhase(rollout)
	addGauge(descRolloutPhaseLabels, boolFloat64(calculatedPhase == RolloutCompleted), string(RolloutCompleted))
	addGauge(descRolloutPhaseLabels, boolFloat64(calculatedPhase == RolloutProgressing), string(RolloutProgressing))
	addGauge(descRolloutPhaseLabels, boolFloat64(calculatedPhase == RolloutPaused), string(RolloutPaused))
	addGauge(descRolloutPhaseLabels, boolFloat64(calculatedPhase == RolloutTimeout), string(RolloutTimeout))
	addGauge(descRolloutPhaseLabels, boolFloat64(calculatedPhase == RolloutError), string(RolloutError))
	addGauge(descRolloutPhaseLabels, boolFloat64(calculatedPhase == RolloutAbort), string(RolloutAbort))
}
