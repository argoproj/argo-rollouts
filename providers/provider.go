package providers

import (
	"fmt"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	batchlisters "k8s.io/client-go/listers/batch/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/providers/job"
	"github.com/argoproj/argo-rollouts/providers/prometheus"
)

// Provider methods to query a external systems and generate a measurement
type Provider interface {
	// Run start a new external system call for a measurement
	//idempotent and do nothing if a call has been started
	Run(*v1alpha1.AnalysisRun, v1alpha1.Metric, []v1alpha1.Argument) (v1alpha1.Measurement, error)
	// Checks if the external system call is finished and returns the current measurement
	Resume(*v1alpha1.AnalysisRun, v1alpha1.Metric, []v1alpha1.Argument, v1alpha1.Measurement) (v1alpha1.Measurement, error)
	// Terminate will terminate an in-progress measurement
	Terminate(*v1alpha1.AnalysisRun, v1alpha1.Metric, []v1alpha1.Argument, v1alpha1.Measurement) (v1alpha1.Measurement, error)
	// Type gets the provider type
	Type() string
}

type ProviderFactory struct {
	KubeClient kubernetes.Interface
	JobLister  batchlisters.JobLister
}

// NewProvider creates the correct provider based on the provider type of the Metric
func (f *ProviderFactory) NewProvider(logCtx log.Entry, metric v1alpha1.Metric) (Provider, error) {
	if metric.Provider.Prometheus != nil {
		api, err := prometheus.NewPrometheusAPI(metric)
		if err != nil {
			return nil, err
		}
		return prometheus.NewPrometheusProvider(api, logCtx), nil
	} else if metric.Provider.Job != nil {
		return job.NewProvider(logCtx, f.KubeClient, f.JobLister), nil
	}
	return nil, fmt.Errorf("no valid provider in metric '%s'", metric.Name)
}
