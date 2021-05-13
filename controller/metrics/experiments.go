package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutlister "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
)

type experimentCollector struct {
	store rolloutlister.ExperimentLister
}

// NewExperimentCollector returns a prometheus collector for experiment metrics
func NewExperimentCollector(experimentLister rolloutlister.ExperimentLister) prometheus.Collector {
	return &experimentCollector{
		store: experimentLister,
	}
}

// Describe implements the prometheus.Collector interface
func (c *experimentCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- MetricExperimentInfo
}

// Collect implements the prometheus.Collector interface
func (c *experimentCollector) Collect(ch chan<- prometheus.Metric) {
	experiments, err := c.store.List(labels.NewSelector())
	if err != nil {
		log.Warnf("Failed to collect experiments: %v", err)
		return
	}
	for _, experiment := range experiments {
		collectExperiments(ch, experiment)
	}
}

func collectExperiments(ch chan<- prometheus.Metric, ex *v1alpha1.Experiment) {
	addGauge := func(desc *prometheus.Desc, v float64, lv ...string) {
		lv = append([]string{ex.Namespace, ex.Name}, lv...)
		ch <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, v, lv...)
	}
	calculatedPhase := ex.Status.Phase

	addGauge(MetricExperimentInfo, 1, string(calculatedPhase))

	// DEPRECATED
	addGauge(MetricExperimentPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhasePending || calculatedPhase == ""), string("Pending"))
	addGauge(MetricExperimentPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseError), string("Error"))
	addGauge(MetricExperimentPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseSuccessful), string("Successful"))
	addGauge(MetricExperimentPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseRunning), string("Running"))
	addGauge(MetricExperimentPhase, boolFloat64(calculatedPhase == v1alpha1.AnalysisPhaseInconclusive), string("Inconclusive"))
}
