package metrics

import (
	"net/http"
	"time"

	"github.com/argoproj/argo-rollouts/utils/log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	// make sure to register workqueue prometheus metrics
	_ "k8s.io/component-base/metrics/prometheus/workqueue"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutlister "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

type MetricsServer struct {
	*http.Server
	reconcileRolloutHistogram *prometheus.HistogramVec
	errorRolloutCounter       *prometheus.CounterVec

	reconcileExperimentHistogram *prometheus.HistogramVec
	errorExperimentCounter       *prometheus.CounterVec

	reconcileAnalysisRunHistogram *prometheus.HistogramVec
	errorAnalysisRunCounter       *prometheus.CounterVec

	k8sRequestsCounter *K8sRequestsCountProvider
}

const (
	// MetricsPath is the endpoint to collect rollout metrics
	MetricsPath = "/metrics"
)

// Follow Prometheus naming practices
// https://prometheus.io/docs/practices/naming/
var (
	descDefaultLabels = []string{"namespace", "name"}
)

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

type ServerConfig struct {
	Addr               string
	RolloutLister      rolloutlister.RolloutLister
	AnalysisRunLister  rolloutlister.AnalysisRunLister
	ExperimentLister   rolloutlister.ExperimentLister
	K8SRequestProvider *K8sRequestsCountProvider
}

// NewMetricsServer returns a new prometheus server which collects rollout metrics
func NewMetricsServer(cfg ServerConfig) *MetricsServer {
	mux := http.NewServeMux()

	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	reg.MustRegister(prometheus.NewGoCollector())
	reg.MustRegister(NewRolloutCollector(cfg.RolloutLister))
	reg.MustRegister(NewAnalysisRunCollector(cfg.AnalysisRunLister))
	reg.MustRegister(NewExperimentCollector(cfg.ExperimentLister))
	cfg.K8SRequestProvider.MustRegister(reg)

	reconcileRolloutHistogram := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rollout_reconcile",
			Help:    "Rollout reconciliation performance.",
			Buckets: []float64{0.01, 0.15, .25, .5, 1},
		},
		append(descRolloutWithStrategyLabels),
	)
	reg.MustRegister(reconcileRolloutHistogram)

	errorRolloutCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rollout_reconcile_error",
			Help: "Error occurring during the rollout",
		},
		append(descDefaultLabels),
	)
	reg.MustRegister(errorRolloutCounter)

	reconcileExperimentHistogram := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "experiment_reconcile",
			Help:    "Experiments reconciliation performance.",
			Buckets: []float64{0.01, 0.15, .25, .5, 1},
		},
		append(descDefaultLabels),
	)
	reg.MustRegister(reconcileExperimentHistogram)

	errorExperimentCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "experiment_reconcile_error",
			Help: "Error occurring during the experiment",
		},
		append(descDefaultLabels),
	)
	reg.MustRegister(errorExperimentCounter)

	reconcileAnalysisRunHistogram := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "analysis_run_reconcile",
			Help:    "Analysis Run reconciliation performance.",
			Buckets: []float64{0.01, 0.15, .25, .5, 1},
		},
		append(descDefaultLabels),
	)
	reg.MustRegister(reconcileAnalysisRunHistogram)

	errorAnalysisRunCounter := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "analysis_run_reconcile_error",
			Help: "Error occurring during the analysis run",
		},
		append(descDefaultLabels),
	)
	reg.MustRegister(errorAnalysisRunCounter)

	mux.Handle(MetricsPath, promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	return &MetricsServer{
		Server: &http.Server{
			Addr:    cfg.Addr,
			Handler: mux,
		},
		reconcileRolloutHistogram: reconcileRolloutHistogram,
		errorRolloutCounter:       errorRolloutCounter,

		reconcileExperimentHistogram: reconcileExperimentHistogram,
		errorExperimentCounter:       errorExperimentCounter,

		reconcileAnalysisRunHistogram: reconcileAnalysisRunHistogram,
		errorAnalysisRunCounter:       errorAnalysisRunCounter,

		k8sRequestsCounter: cfg.K8SRequestProvider,
	}
}

// IncRolloutReconcile increments the reconcile counter for a Rollout
func (m *MetricsServer) IncRolloutReconcile(rollout *v1alpha1.Rollout, duration time.Duration) {
	m.reconcileRolloutHistogram.WithLabelValues(rollout.Namespace, rollout.Name, defaults.GetStrategyType(rollout)).Observe(duration.Seconds())
}

// IncExperimentReconcile increments the reconcile counter for an Experiment
func (m *MetricsServer) IncExperimentReconcile(ex *v1alpha1.Experiment, duration time.Duration) {
	m.reconcileExperimentHistogram.WithLabelValues(ex.Namespace, ex.Name).Observe(duration.Seconds())
}

// IncAnalysisRunReconcile increments the reconcile counter for an AnalysisRun
func (m *MetricsServer) IncAnalysisRunReconcile(ar *v1alpha1.AnalysisRun, duration time.Duration) {
	m.reconcileAnalysisRunHistogram.WithLabelValues(ar.Namespace, ar.Name).Observe(duration.Seconds())
}

// IncError increments the reconcile counter for an rollout
func (m *MetricsServer) IncError(namespace, name string, kind string) {
	switch kind {
	case log.RolloutKey:
		m.errorRolloutCounter.WithLabelValues(namespace, name).Inc()
	case log.AnalysisRunKey:
		m.errorAnalysisRunCounter.WithLabelValues(namespace, name).Inc()
	case log.ExperimentKey:
		m.errorExperimentCounter.WithLabelValues(namespace, name).Inc()
	}
}

func boolFloat64(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
