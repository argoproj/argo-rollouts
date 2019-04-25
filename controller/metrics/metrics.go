package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"

	"time"

	v1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutlister "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

type MetricsServer struct {
	*http.Server
	reconcileHistogram    *prometheus.HistogramVec
	reconcilePhaseCounter *prometheus.CounterVec
	errorCounter          *prometheus.CounterVec
}

const (
	// MetricsPath is the endpoint to collect rollout metrics
	MetricsPath = "/metrics"
)

// Follow Prometheus naming practices
// https://prometheus.io/docs/practices/naming/
var (
	descRolloutDefaultLabels = []string{"namespace", "name"}

	descRolloutWithStrategyLabels = append(descRolloutDefaultLabels, "strategy")

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
)

// ReconcilePhase the phases of a reconcile can have
type ReconcilePhase string

const (

	// InvalidSpec means the rollout had an InvalidSpec during reconciliation
	InvalidSpec ReconcilePhase = "InvalidSpec"
	// Completed means the rollout finished the reconciliation with no remaining work
	Completed ReconcilePhase = "Completed"
	// Progressing means the rollout finished the reconciliation with remaining work
	Progressing ReconcilePhase = "Progressing"
	// Paused means the rollout finished the reconciliation with a paused status
	Paused ReconcilePhase = "Progressing"
	// Timeout means the rollout finished the reconciliation with an timeout message
	Timeout ReconcilePhase = "Timeout"
	// Error means the rollout finished the reconciliation with an error
	Error ReconcilePhase = "Error"
)

// NewMetricsServer returns a new prometheus server which collects rollout metrics
func NewMetricsServer(addr string, rolloutLister rolloutlister.RolloutLister) *MetricsServer {
	mux := http.NewServeMux()
	rolloutRegistry := NewRolloutRegistry(rolloutLister)
	mux.Handle(MetricsPath, promhttp.HandlerFor(rolloutRegistry, promhttp.HandlerOpts{}))

	reconcileHistogram := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rollout_reconcile",
			Help:    "Rollout reconciliation performance.",
			Buckets: []float64{0.01, 0.15, .25, .5, 1},
		},
		append(descRolloutWithStrategyLabels),
	)

	rolloutRegistry.MustRegister(reconcileHistogram)

	reconcilePhaseCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rollout_reconcile_phases",
			Help: "Phase the rollout has",
		},
		append(descRolloutReconcilePhaseLabels),
	)
	rolloutRegistry.MustRegister(reconcilePhaseCounter)

	errorCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rollout_reconcile_error",
			Help: "Error occuring during the rollout",
		},
		append(descRolloutDefaultLabels),
	)

	rolloutRegistry.MustRegister(errorCounter)

	return &MetricsServer{
		Server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		reconcileHistogram:    reconcileHistogram,
		reconcilePhaseCounter: reconcilePhaseCounter,
		errorCounter:          errorCounter,
	}
}

// IncReconcile increments the reconcile counter for an rollout
func (m *MetricsServer) IncReconcile(rollout *v1alpha1.Rollout, duration time.Duration) {
	m.reconcileHistogram.WithLabelValues(rollout.Namespace, rollout.Name, defaults.GetStrategyType(rollout)).Observe(duration.Seconds())
}

// IncError increments the reconcile counter for an rollout
func (m *MetricsServer) IncError(namespace, name string) {
	m.errorCounter.WithLabelValues(namespace, name).Inc()
}

// IncError increments the error counter for an rollout
func (m *MetricsServer) IncPhase(rollout *v1alpha1.Rollout, newStatus *v1alpha1.RolloutStatus) {
	phase := Progressing
	progressing := conditions.GetRolloutCondition(*newStatus, v1alpha1.RolloutProgressing)
	if progressing != nil {
		if progressing.Reason == conditions.NewRSAvailableReason {
			phase = Completed
		}
		if progressing.Reason == conditions.PausedRolloutReason {
			phase = Paused
		}
		if progressing.Reason == conditions.ServiceNotFoundReason || progressing.Reason == conditions.FailedRSCreateReason {
			phase = Error
		}
		if progressing.Reason == conditions.TimedOutReason {
			phase = Timeout
		}
	}
	invalidSpec := conditions.GetRolloutCondition(*newStatus, v1alpha1.InvalidSpec)
	if invalidSpec != nil {
		phase = InvalidSpec
	}
	m.reconcilePhaseCounter.WithLabelValues(rollout.Namespace, rollout.Name, defaults.GetStrategyType(rollout), string(phase)).Inc()
}

type rolloutCollector struct {
	store rolloutlister.RolloutLister
}

// NewRolloutCollector returns a prometheus collector for rollout metrics
func NewRolloutCollector(rolloutLister rolloutlister.RolloutLister) prometheus.Collector {
	return &rolloutCollector{
		store: rolloutLister,
	}
}

// NewRolloutRegistry creates a new prometheus registry that collects rollouts
func NewRolloutRegistry(rolloutLister rolloutlister.RolloutLister) *prometheus.Registry {
	registry := prometheus.NewRegistry()
	registry.MustRegister(NewRolloutCollector(rolloutLister))
	registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	registry.MustRegister(prometheus.NewGoCollector())
	return registry
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
}
