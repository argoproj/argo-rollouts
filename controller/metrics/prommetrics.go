package metrics

import "github.com/prometheus/client_golang/prometheus"

// Follow Prometheus naming practices
// https://prometheus.io/docs/practices/naming/
var (
	nameNamespaceLabels = []string{"namespace", "name"}
)

// Rollout metrics
var (
	MetricRolloutReconcile = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "rollout_reconcile",
			Help:    "Rollout reconciliation performance.",
			Buckets: []float64{0.01, 0.15, .25, .5, 1},
		},
		nameNamespaceLabels,
	)

	MetricRolloutReconcileError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rollout_reconcile_error",
			Help: "Error occurring during the rollout",
		},
		nameNamespaceLabels,
	)

	MetricRolloutInfo = prometheus.NewDesc(
		"rollout_info",
		"Information about rollout.",
		append(nameNamespaceLabels, "strategy", "phase"),
		nil,
	)

	MetricRolloutInfoReplicasAvailable = prometheus.NewDesc(
		"rollout_info_replicas_available",
		"The number of available replicas per rollout.",
		nameNamespaceLabels,
		nil,
	)

	MetricRolloutInfoReplicasUnavailable = prometheus.NewDesc(
		"rollout_info_replicas_unavailable",
		"The number of unavailable replicas per rollout.",
		nameNamespaceLabels,
		nil,
	)

	MetricRolloutInfoReplicasDesired = prometheus.NewDesc(
		"rollout_info_replicas_desired",
		"The number of desired replicas per rollout.",
		nameNamespaceLabels,
		nil,
	)

	MetricRolloutUpdatedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rollout_updated_total",
			Help: "Count of rollout updates",
		},
		nameNamespaceLabels,
	)

	MetricRolloutAbortedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rollout_aborted_total",
			Help: "Count of rollout aborts",
		},
		nameNamespaceLabels,
	)

	MetricRolloutCompletedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rollout_completed_total",
			Help: "Count of rollout successfully completed updates",
		},
		nameNamespaceLabels,
	)

	// DEPRECATED in favor of rollout_info
	MetricRolloutPhase = prometheus.NewDesc(
		"rollout_phase",
		"Information on the state of the rollout (DEPRECATED - use rollout_info)",
		append(nameNamespaceLabels, "strategy", "phase"),
		nil,
	)
)

// AnalysisRun metrics
var (
	MetricAnalysisRunReconcile = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "analysis_run_reconcile",
			Help:    "Analysis Run reconciliation performance.",
			Buckets: []float64{0.01, 0.15, .25, .5, 1},
		},
		nameNamespaceLabels,
	)

	MetricAnalysisRunReconcileError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "analysis_run_reconcile_error",
			Help: "Error occurring during the analysis run",
		},
		nameNamespaceLabels,
	)
	MetricAnalysisRunInfo = prometheus.NewDesc(
		"analysis_run_info",
		"Information about analysis run.",
		append(nameNamespaceLabels, "phase"),
		nil,
	)

	// DEPRECATED in favor of analysis_run_info
	MetricAnalysisRunPhase = prometheus.NewDesc(
		"analysis_run_phase",
		"Information on the state of the Analysis Run (DEPRECATED - use analysis_run_info)",
		append(nameNamespaceLabels, "phase"),
		nil,
	)

	MetricAnalysisRunMetricType = prometheus.NewDesc(
		"analysis_run_metric_type",
		"Information on the type of a specific metric in the Analysis Runs",
		append(nameNamespaceLabels, "metric", "type"),
		nil,
	)

	MetricAnalysisRunMetricPhase = prometheus.NewDesc(
		"analysis_run_metric_phase",
		"Information on the duration of a specific metric in the Analysis Run",
		append(nameNamespaceLabels, "metric", "type", "phase"),
		nil,
	)
)

// AnalysisTemplate metrics
var (
	MetricAnalysisTemplateInfo = prometheus.NewDesc(
		"analysis_template_info",
		"Information about analysis templates.",
		append(nameNamespaceLabels),
		nil,
	)

	MetricAnalysisTemplateMetricInfo = prometheus.NewDesc(
		"analysis_template_metric_info",
		"Information on metrics in analysis templates.",
		append(nameNamespaceLabels, "type"),
		nil,
	)
)

// Experiment metrics
var (
	MetricExperimentReconcile = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "experiment_reconcile",
			Help:    "Experiments reconciliation performance.",
			Buckets: []float64{0.01, 0.15, .25, .5, 1},
		},
		nameNamespaceLabels,
	)

	MetricExperimentReconcileError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "experiment_reconcile_error",
			Help: "Error occurring during the experiment",
		},
		nameNamespaceLabels,
	)

	MetricExperimentInfo = prometheus.NewDesc(
		"experiment_info",
		"Information about Experiment.",
		append(nameNamespaceLabels, "phase"),
		nil,
	)

	// DEPRECATED in favor of experiment_info
	MetricExperimentPhase = prometheus.NewDesc(
		"experiment_phase",
		"Information on the state of the experiment (DEPRECATED - use experiment_info)",
		append(nameNamespaceLabels, "phase"),
		nil,
	)
)

// K8s Client metrics
var (
	// Custom events metric
	MetricK8sRequestTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: clientsetMetricsNamespace,
			Name:      "k8s_request_total",
			Help:      "Number of kubernetes requests executed during application reconciliation.",
		},
		[]string{"kind", "namespace", "name", "verb", "status_code"},
	)
)
