package metricproviders

import (
	"fmt"

	"github.com/argoproj/argo-rollouts/metricproviders/influxdb"

	"github.com/argoproj/argo-rollouts/metricproviders/cloudwatch"
	"github.com/argoproj/argo-rollouts/metricproviders/datadog"
	"github.com/argoproj/argo-rollouts/metricproviders/graphite"
	"github.com/argoproj/argo-rollouts/metricproviders/kayenta"
	"github.com/argoproj/argo-rollouts/metricproviders/newrelic"
	"github.com/argoproj/argo-rollouts/metricproviders/wavefront"
	"github.com/argoproj/argo-rollouts/metricproviders/webmetric"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	batchlisters "k8s.io/client-go/listers/batch/v1"

	"github.com/argoproj/argo-rollouts/metricproviders/job"
	"github.com/argoproj/argo-rollouts/metricproviders/prometheus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// Provider methods to query a external systems and generate a measurement
type Provider interface {
	// Run start a new external system call for a measurement
	// Should be idempotent and do nothing if a call has already been started
	Run(*v1alpha1.AnalysisRun, v1alpha1.Metric) v1alpha1.Measurement
	// Checks if the external system call is finished and returns the current measurement
	Resume(*v1alpha1.AnalysisRun, v1alpha1.Metric, v1alpha1.Measurement) v1alpha1.Measurement
	// Terminate will terminate an in-progress measurement
	Terminate(*v1alpha1.AnalysisRun, v1alpha1.Metric, v1alpha1.Measurement) v1alpha1.Measurement
	// GarbageCollect is used to garbage collect completed measurements to the specified limit
	GarbageCollect(*v1alpha1.AnalysisRun, v1alpha1.Metric, int) error
	// Type gets the provider type
	Type() string
	// GetMetadata returns any additional metadata which providers need to store/display as part
	// of the metric result. For example, Prometheus uses is to store the final resolved queries.
	GetMetadata(metric v1alpha1.Metric) map[string]string
}

type ProviderFactory struct {
	KubeClient kubernetes.Interface
	JobLister  batchlisters.JobLister
}

type ProviderFactoryFunc func(logCtx log.Entry, metric v1alpha1.Metric) (Provider, error)

// NewProvider creates the correct provider based on the provider type of the Metric
func (f *ProviderFactory) NewProvider(logCtx log.Entry, metric v1alpha1.Metric) (Provider, error) {
	switch provider := Type(metric); provider {
	case prometheus.ProviderType:
		api, err := prometheus.NewPrometheusAPI(metric)
		if err != nil {
			return nil, err
		}
		return prometheus.NewPrometheusProvider(api, logCtx), nil
	case job.ProviderType:
		return job.NewJobProvider(logCtx, f.KubeClient, f.JobLister), nil
	case kayenta.ProviderType:
		c := kayenta.NewHttpClient()
		return kayenta.NewKayentaProvider(logCtx, c), nil
	case webmetric.ProviderType:
		c := webmetric.NewWebMetricHttpClient(metric)
		p, err := webmetric.NewWebMetricJsonParser(metric)
		if err != nil {
			return nil, err
		}
		return webmetric.NewWebMetricProvider(logCtx, c, p), nil
	case datadog.ProviderType:
		return datadog.NewDatadogProvider(logCtx, f.KubeClient)
	case wavefront.ProviderType:
		client, err := wavefront.NewWavefrontAPI(metric, f.KubeClient)
		if err != nil {
			return nil, err
		}
		return wavefront.NewWavefrontProvider(client, logCtx), nil
	case newrelic.ProviderType:
		client, err := newrelic.NewNewRelicAPIClient(metric, f.KubeClient)
		if err != nil {
			return nil, err
		}
		return newrelic.NewNewRelicProvider(client, logCtx), nil
	case graphite.ProviderType:
		client, err := graphite.NewAPIClient(metric, logCtx)
		if err != nil {
			return nil, err
		}
		return graphite.NewGraphiteProvider(client, logCtx), nil
	case cloudwatch.ProviderType:
		client, err := cloudwatch.NewCloudWatchAPIClient(metric)
		if err != nil {
			return nil, err
		}
		return cloudwatch.NewCloudWatchProvider(client, logCtx), nil
	case influxdb.ProviderType:
		client, err := influxdb.NewInfluxdbAPI(metric, f.KubeClient)
		if err != nil {
			return nil, err
		}
		return influxdb.NewInfluxdbProvider(client, logCtx), nil
	default:
		return nil, fmt.Errorf("no valid provider in metric '%s'", metric.Name)
	}
}

func Type(metric v1alpha1.Metric) string {
	if metric.Provider.Prometheus != nil {
		return prometheus.ProviderType
	} else if metric.Provider.Job != nil {
		return job.ProviderType
	} else if metric.Provider.Kayenta != nil {
		return kayenta.ProviderType
	} else if metric.Provider.Web != nil {
		return webmetric.ProviderType
	} else if metric.Provider.Datadog != nil {
		return datadog.ProviderType
	} else if metric.Provider.Wavefront != nil {
		return wavefront.ProviderType
	} else if metric.Provider.NewRelic != nil {
		return newrelic.ProviderType
	} else if metric.Provider.CloudWatch != nil {
		return cloudwatch.ProviderType
	} else if metric.Provider.Graphite != nil {
		return graphite.ProviderType
	} else if metric.Provider.Influxdb != nil {
		return influxdb.ProviderType
	}

	return "Unknown Provider"
}
