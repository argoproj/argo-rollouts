package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/argoproj/argo-rollouts/utils/version"
)

// Follow Prometheus naming practices
// https://prometheus.io/docs/practices/naming/
var (
	namespaceNameLabels = []string{"namespace", "name"}
)

// DefaultReconcileHistogramBuckets are the default buckets (seconds) for the reconcile
// and notification_send histograms. Override with SetReconcileHistogramBuckets.
var DefaultReconcileHistogramBuckets = []float64{0.01, 0.15, .25, .5, 1}

func newReconcileHistogram(name, help string) *prometheus.HistogramVec {
	return prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    name,
			Help:    help,
			Buckets: DefaultReconcileHistogramBuckets,
		},
		namespaceNameLabels,
	)
}

// SetReconcileHistogramBuckets rebuilds the reconcile and notification_send histograms with
// the given buckets. Must be called before NewMetricsServer/NewEventRecorder.
func SetReconcileHistogramBuckets(buckets []float64) {
	if len(buckets) == 0 {
		return
	}
	DefaultReconcileHistogramBuckets = buckets
	MetricRolloutReconcile = newReconcileHistogram("rollout_reconcile", "Rollout reconciliation performance.")
	MetricAnalysisRunReconcile = newReconcileHistogram("analysis_run_reconcile", "Analysis Run reconciliation performance.")
	MetricExperimentReconcile = newReconcileHistogram("experiment_reconcile", "Experiments reconciliation performance.")
	MetricNotificationSend = newReconcileHistogram("notification_send", "Notification send performance.")
}

// Rollout metrics
var (
	MetricRolloutReconcile = newReconcileHistogram("rollout_reconcile", "Rollout reconciliation performance.")

	MetricRolloutReconcileError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rollout_reconcile_error",
			Help: "Error occurring during the rollout",
		},
		namespaceNameLabels,
	)

	MetricRolloutInfo = prometheus.NewDesc(
		"rollout_info",
		"Information about rollout.",
		append(namespaceNameLabels, "strategy", "traffic_router", "phase"),
		nil,
	)

	MetricRolloutInfoReplicasAvailable = prometheus.NewDesc(
		"rollout_info_replicas_available",
		"The number of available replicas per rollout.",
		namespaceNameLabels,
		nil,
	)

	MetricRolloutInfoReplicasUnavailable = prometheus.NewDesc(
		"rollout_info_replicas_unavailable",
		"The number of unavailable replicas per rollout.",
		namespaceNameLabels,
		nil,
	)

	MetricRolloutInfoReplicasDesired = prometheus.NewDesc(
		"rollout_info_replicas_desired",
		"The number of desired replicas per rollout.",
		namespaceNameLabels,
		nil,
	)

	MetricRolloutInfoReplicasUpdated = prometheus.NewDesc(
		"rollout_info_replicas_updated",
		"The number of updated replicas per rollout.",
		namespaceNameLabels,
		nil,
	)

	MetricRolloutEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rollout_events_total",
			Help: "Count of rollout events",
		},
		append(namespaceNameLabels, "type", "reason"),
	)

	// DEPRECATED in favor of rollout_info
	MetricRolloutPhase = prometheus.NewDesc(
		"rollout_phase",
		"Information on the state of the rollout (DEPRECATED - use rollout_info)",
		append(namespaceNameLabels, "strategy", "phase"),
		nil,
	)
)

// AnalysisRun metrics
var (
	MetricAnalysisRunReconcile = newReconcileHistogram("analysis_run_reconcile", "Analysis Run reconciliation performance.")

	MetricAnalysisRunReconcileError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "analysis_run_reconcile_error",
			Help: "Error occurring during the analysis run",
		},
		namespaceNameLabels,
	)
	MetricAnalysisRunInfo = prometheus.NewDesc(
		"analysis_run_info",
		"Information about analysis run.",
		append(namespaceNameLabels, "phase"),
		nil,
	)

	// DEPRECATED in favor of analysis_run_info
	MetricAnalysisRunPhase = prometheus.NewDesc(
		"analysis_run_phase",
		"Information on the state of the Analysis Run (DEPRECATED - use analysis_run_info)",
		append(namespaceNameLabels, "phase"),
		nil,
	)

	MetricAnalysisRunMetricType = prometheus.NewDesc(
		"analysis_run_metric_type",
		"Information on the type of a specific metric in the Analysis Runs",
		append(namespaceNameLabels, "metric", "type"),
		nil,
	)

	MetricAnalysisRunMetricPhase = prometheus.NewDesc(
		"analysis_run_metric_phase",
		"Information on the duration of a specific metric in the Analysis Run",
		append(namespaceNameLabels, "metric", "type", "dry_run", "phase"),
		nil,
	)
)

// AnalysisTemplate metrics
var (
	MetricAnalysisTemplateInfo = prometheus.NewDesc(
		"analysis_template_info",
		"Information about analysis templates.",
		namespaceNameLabels,
		nil,
	)

	MetricAnalysisTemplateMetricInfo = prometheus.NewDesc(
		"analysis_template_metric_info",
		"Information on metrics in analysis templates.",
		append(namespaceNameLabels, "type", "metric"),
		nil,
	)
)

// Experiment metrics
var (
	MetricExperimentReconcile = newReconcileHistogram("experiment_reconcile", "Experiments reconciliation performance.")

	MetricExperimentReconcileError = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "experiment_reconcile_error",
			Help: "Error occurring during the experiment",
		},
		namespaceNameLabels,
	)

	MetricExperimentInfo = prometheus.NewDesc(
		"experiment_info",
		"Information about Experiment.",
		append(namespaceNameLabels, "phase"),
		nil,
	)

	// DEPRECATED in favor of experiment_info
	MetricExperimentPhase = prometheus.NewDesc(
		"experiment_phase",
		"Information on the state of the experiment (DEPRECATED - use experiment_info)",
		append(namespaceNameLabels, "phase"),
		nil,
	)
)

// Notification metrics
var (
	MetricNotificationSuccessTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_send_success",
			Help: "Notification send success.",
		},
		append(namespaceNameLabels, "type", "reason"),
	)

	MetricNotificationFailedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notification_send_error",
			Help: "Error sending the notification",
		},
		append(namespaceNameLabels, "type", "reason"),
	)

	MetricNotificationSend = newReconcileHistogram("notification_send", "Notification send performance.")
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

// MetricVersionGauge version info
var (
	MetricVersionGauge = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        "argo_rollouts_controller_info",
			Help:        "Running Argo-rollouts version",
			ConstLabels: prometheus.Labels{"version": version.GetVersion().Version},
		},
		func() float64 {
			return float64(1)
		},
	)
)
