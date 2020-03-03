package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"

	// make sure to register workqueue prometheus metrics
	_ "k8s.io/kubernetes/pkg/util/workqueue/prometheus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutlister "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

type MetricsServer struct {
	*http.Server
	reconcileHistogram *prometheus.HistogramVec
	errorCounter       *prometheus.CounterVec
	k8sRequestsCounter *K8sRequestsCountProvider
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

	// InvalidSpec means the rollout had an InvalidSpec during reconciliation
	InvalidSpec RolloutPhase = "InvalidSpec"
	// Completed means the rollout finished the reconciliation with no remaining work
	Completed RolloutPhase = "Completed"
	// Progressing means the rollout finished the reconciliation with remaining work
	Progressing RolloutPhase = "Progressing"
	// Paused means the rollout finished the reconciliation with a paused status
	Paused RolloutPhase = "Paused"
	// Timeout means the rollout finished the reconciliation with an timeout message
	Timeout RolloutPhase = "Timeout"
	// Error means the rollout finished the reconciliation with an error
	Error RolloutPhase = "Error"
)

// NewMetricsServer returns a new prometheus server which collects rollout metrics
func NewMetricsServer(addr string, rolloutLister rolloutlister.RolloutLister, k8sRequestProvider *K8sRequestsCountProvider) *MetricsServer {
	mux := http.NewServeMux()
	rolloutRegistry := NewRolloutRegistry(rolloutLister)
	mux.Handle(MetricsPath, promhttp.HandlerFor(prometheus.Gatherers{
		// contains app controller specific metrics
		rolloutRegistry,
		// contains process, golang and controller workqueues metrics
		prometheus.DefaultGatherer,
	}, promhttp.HandlerOpts{}))

	reconcileHistogram := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rollout_reconcile",
			Help:    "Rollout reconciliation performance.",
			Buckets: []float64{0.01, 0.15, .25, .5, 1},
		},
		append(descRolloutWithStrategyLabels),
	)
	k8sRequestProvider.Register(rolloutRegistry)
	rolloutRegistry.MustRegister(reconcileHistogram)

	errorCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rollout_reconcile_error",
			Help: "Error occurring during the rollout",
		},
		append(descRolloutDefaultLabels),
	)

	rolloutRegistry.MustRegister(errorCounter)

	return &MetricsServer{
		Server: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		reconcileHistogram: reconcileHistogram,
		errorCounter:       errorCounter,
		k8sRequestsCounter: k8sRequestProvider,
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

// calculatePhase calculates where a Rollout is in a Completed, Paused, Error, Timeout, or InvalidSpec phase
func calculatePhase(rollout *v1alpha1.Rollout) RolloutPhase {
	phase := Progressing
	progressing := conditions.GetRolloutCondition(rollout.Status, v1alpha1.RolloutProgressing)
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
	invalidSpec := conditions.GetRolloutCondition(rollout.Status, v1alpha1.InvalidSpec)
	if invalidSpec != nil {
		phase = InvalidSpec
	}
	return phase
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

func boolFloat64(b bool) float64 {
	if b {
		return 1
	}
	return 0
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
	addGauge(descRolloutPhaseLabels, boolFloat64(calculatedPhase == Completed), string(Completed))
	addGauge(descRolloutPhaseLabels, boolFloat64(calculatedPhase == Progressing), string(Progressing))
	addGauge(descRolloutPhaseLabels, boolFloat64(calculatedPhase == Paused), string(Paused))
	addGauge(descRolloutPhaseLabels, boolFloat64(calculatedPhase == Timeout), string(Timeout))
	addGauge(descRolloutPhaseLabels, boolFloat64(calculatedPhase == Error), string(Error))
	addGauge(descRolloutPhaseLabels, boolFloat64(calculatedPhase == InvalidSpec), string(InvalidSpec))
}
