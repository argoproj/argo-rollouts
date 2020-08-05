package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/argoproj/argo-rollouts/metricproviders"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutlister "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/analysis"
)

var (
	descAnalysisRunInfo = prometheus.NewDesc(
		"analysis_run_info",
		"Information about analysis run.",
		descDefaultLabels,
		nil,
	)

	descAnalysisRunPhaseLabels = append(descDefaultLabels, "phase")

	descAnalysisRunMetricLabels = append(descDefaultLabels, "metric", "type")

	descAnalysisRunMetricPhase = append(descAnalysisRunMetricLabels, "phase")

	descAnalysisRunPhase = prometheus.NewDesc(
		"analysis_run_phase",
		"Information on the state of the Analysis Run",
		descAnalysisRunPhaseLabels,
		nil,
	)

	descMetricType = prometheus.NewDesc(
		"analysis_run_metric_type",
		"Information on the type of a specific metric in the Analysis Runs",
		descAnalysisRunMetricLabels,
		nil,
	)

	descMetricPhase = prometheus.NewDesc(
		"analysis_run_metric_phase",
		"Information on the duration of a specific metric in the Analysis Run",
		descAnalysisRunMetricPhase,
		nil,
	)
)

type analysisRunCollector struct {
	store rolloutlister.AnalysisRunLister
}

// NewAnalysisRunCollector returns a prometheus collector for AnalysisRun metrics
func NewAnalysisRunCollector(analysisRunLister rolloutlister.AnalysisRunLister) prometheus.Collector {
	return &analysisRunCollector{
		store: analysisRunLister,
	}
}

// Describe implements the prometheus.Collector interface
func (c *analysisRunCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descAnalysisRunInfo
}

// Collect implements the prometheus.Collector interface
func (c *analysisRunCollector) Collect(ch chan<- prometheus.Metric) {
	analysisRuns, err := c.store.List(labels.NewSelector())
	if err != nil {
		log.Warnf("Failed to collect analysisRuns: %v", err)
		return
	}
	for _, ar := range analysisRuns {
		collectAnalysisRuns(ch, ar)
	}
}

func collectAnalysisRuns(ch chan<- prometheus.Metric, ar *v1alpha1.AnalysisRun) {

	addConstMetric := func(desc *prometheus.Desc, t prometheus.ValueType, v float64, lv ...string) {
		lv = append([]string{ar.Namespace, ar.Name}, lv...)
		ch <- prometheus.MustNewConstMetric(desc, t, v, lv...)
	}
	addGauge := func(desc *prometheus.Desc, v float64, lv ...string) {
		addConstMetric(desc, prometheus.GaugeValue, v, lv...)
	}

	addGauge(descAnalysisRunInfo, 1)

	calculatedPhase := ar.Status.Phase
	addGauge(descAnalysisRunPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhasePending || calculatedPhase == ""), string(v1alpha1.AnalysisPhasePending))
	addGauge(descAnalysisRunPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseError), string(v1alpha1.AnalysisPhaseError))
	addGauge(descAnalysisRunPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseFailed), string(v1alpha1.AnalysisPhaseFailed))
	addGauge(descAnalysisRunPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseSuccessful), string(v1alpha1.AnalysisPhaseSuccessful))
	addGauge(descAnalysisRunPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseRunning), string(v1alpha1.AnalysisPhaseRunning))
	addGauge(descAnalysisRunPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseInconclusive), string(v1alpha1.AnalysisPhaseInconclusive))
	for _, metric := range ar.Spec.Metrics {
		metricType := metricproviders.Type(metric)
		metricResult := analysis.GetResult(ar, metric.Name)
		addGauge(descMetricType, 1, metric.Name, metricType)
		calculatedPhase := v1alpha1.AnalysisPhase("")
		if metricResult != nil {
			calculatedPhase = metricResult.Phase
		}
		addGauge(descMetricPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhasePending || calculatedPhase == ""), metric.Name, metricType, string(v1alpha1.AnalysisPhasePending))
		addGauge(descMetricPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseError), metric.Name, metricType, string(v1alpha1.AnalysisPhaseError))
		addGauge(descMetricPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseFailed), metric.Name, metricType, string(v1alpha1.AnalysisPhaseFailed))
		addGauge(descMetricPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseSuccessful), metric.Name, metricType, string(v1alpha1.AnalysisPhaseSuccessful))
		addGauge(descMetricPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseRunning), metric.Name, metricType, string(v1alpha1.AnalysisPhaseRunning))
		addGauge(descMetricPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseInconclusive), metric.Name, metricType, string(v1alpha1.AnalysisPhaseInconclusive))
	}
}
