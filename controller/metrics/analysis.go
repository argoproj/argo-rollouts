package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/argoproj/argo-rollouts/metricproviders"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutlister "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
)

type analysisRunCollector struct {
	runs             rolloutlister.AnalysisRunLister
	templates        rolloutlister.AnalysisTemplateLister
	clusterTemplates rolloutlister.ClusterAnalysisTemplateLister
}

// NewAnalysisRunCollector returns a prometheus collector for AnalysisRun metrics
func NewAnalysisRunCollector(
	analysisRunLister rolloutlister.AnalysisRunLister,
	analysisTemplateLister rolloutlister.AnalysisTemplateLister,
	clusterAnalysisTemplateLister rolloutlister.ClusterAnalysisTemplateLister,
) prometheus.Collector {
	return &analysisRunCollector{
		runs:             analysisRunLister,
		templates:        analysisTemplateLister,
		clusterTemplates: clusterAnalysisTemplateLister,
	}
}

// Describe implements the prometheus.Collector interface
func (c *analysisRunCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- MetricAnalysisRunInfo
}

// Collect implements the prometheus.Collector interface
func (c *analysisRunCollector) Collect(ch chan<- prometheus.Metric) {
	analysisRuns, err := c.runs.List(labels.NewSelector())
	if err != nil {
		log.Warnf("Failed to collect analysis runs: %v", err)
	} else {
		for _, ar := range analysisRuns {
			collectAnalysisRuns(ch, ar)
		}
	}
	analysisTemplates, err := c.templates.List(labels.NewSelector())
	if err != nil {
		log.Warnf("Failed to collect analysis templates: %v", err)
	} else {
		for _, at := range analysisTemplates {
			collectAnalysisTemplate(ch, at.Namespace, at.Name, &at.Spec)
		}
	}
	clusterAnalysisTemplates, err := c.clusterTemplates.List(labels.NewSelector())
	if err != nil {
		log.Warnf("Failed to collect cluster analysis templates: %v", err)
	} else {
		for _, at := range clusterAnalysisTemplates {
			collectAnalysisTemplate(ch, at.Namespace, at.Name, &at.Spec)
		}
	}
}

func collectAnalysisRuns(ch chan<- prometheus.Metric, ar *v1alpha1.AnalysisRun) {
	addGauge := func(desc *prometheus.Desc, v float64, lv ...string) {
		lv = append([]string{ar.Namespace, ar.Name}, lv...)
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v, lv...)
	}
	calculatedPhase := ar.Status.Phase

	addGauge(MetricAnalysisRunInfo, 1, string(calculatedPhase))

	// DEPRECATED
	addGauge(MetricAnalysisRunPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhasePending || calculatedPhase == ""), string(v1alpha1.AnalysisPhasePending))
	addGauge(MetricAnalysisRunPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseError), string(v1alpha1.AnalysisPhaseError))
	addGauge(MetricAnalysisRunPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseFailed), string(v1alpha1.AnalysisPhaseFailed))
	addGauge(MetricAnalysisRunPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseSuccessful), string(v1alpha1.AnalysisPhaseSuccessful))
	addGauge(MetricAnalysisRunPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseRunning), string(v1alpha1.AnalysisPhaseRunning))
	addGauge(MetricAnalysisRunPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseInconclusive), string(v1alpha1.AnalysisPhaseInconclusive))

	dryRunMetricsMap, _ := analysisutil.GetDryRunMetrics(ar.Spec.DryRun, ar.Spec.Metrics)
	for _, metric := range ar.Spec.Metrics {
		metricType := metricproviders.Type(metric)
		metricResult := analysisutil.GetResult(ar, metric.Name)
		addGauge(MetricAnalysisRunMetricType, 1, metric.Name, metricType)
		calculatedPhase := v1alpha1.AnalysisPhase("")
		if metricResult != nil {
			calculatedPhase = metricResult.Phase
		}
		addGauge(MetricAnalysisRunMetricPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhasePending || calculatedPhase == ""), metric.Name, metricType, fmt.Sprint(dryRunMetricsMap[metric.Name]), string(v1alpha1.AnalysisPhasePending))
		addGauge(MetricAnalysisRunMetricPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseError), metric.Name, metricType, fmt.Sprint(dryRunMetricsMap[metric.Name]), string(v1alpha1.AnalysisPhaseError))
		addGauge(MetricAnalysisRunMetricPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseFailed), metric.Name, metricType, fmt.Sprint(dryRunMetricsMap[metric.Name]), string(v1alpha1.AnalysisPhaseFailed))
		addGauge(MetricAnalysisRunMetricPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseSuccessful), metric.Name, metricType, fmt.Sprint(dryRunMetricsMap[metric.Name]), string(v1alpha1.AnalysisPhaseSuccessful))
		addGauge(MetricAnalysisRunMetricPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseRunning), metric.Name, metricType, fmt.Sprint(dryRunMetricsMap[metric.Name]), string(v1alpha1.AnalysisPhaseRunning))
		addGauge(MetricAnalysisRunMetricPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseInconclusive), metric.Name, metricType, fmt.Sprint(dryRunMetricsMap[metric.Name]), string(v1alpha1.AnalysisPhaseInconclusive))
	}
}

func collectAnalysisTemplate(ch chan<- prometheus.Metric, namespace, name string, at *v1alpha1.AnalysisTemplateSpec) {
	addGauge := func(desc *prometheus.Desc, v float64, lv ...string) {
		lv = append([]string{namespace, name}, lv...)
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v, lv...)
	}
	addGauge(MetricAnalysisTemplateInfo, 1)

	for _, metric := range at.Metrics {
		metricType := metricproviders.Type(metric)
		addGauge(MetricAnalysisTemplateMetricInfo, 1, metricType, metric.Name)
	}
}
