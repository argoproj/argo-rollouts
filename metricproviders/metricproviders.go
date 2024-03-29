package metricproviders

import (
	"fmt"
	"os"

	"github.com/argoproj/argo-rollouts/metric"
	"github.com/argoproj/argo-rollouts/metricproviders/influxdb"
	"github.com/argoproj/argo-rollouts/metricproviders/skywalking"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/argoproj/argo-rollouts/metricproviders/cloudwatch"
	"github.com/argoproj/argo-rollouts/metricproviders/datadog"
	"github.com/argoproj/argo-rollouts/metricproviders/graphite"
	"github.com/argoproj/argo-rollouts/metricproviders/kayenta"
	"github.com/argoproj/argo-rollouts/metricproviders/newrelic"
	"github.com/argoproj/argo-rollouts/metricproviders/plugin"
	"github.com/argoproj/argo-rollouts/metricproviders/wavefront"
	"github.com/argoproj/argo-rollouts/metricproviders/webmetric"

	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	batchlisters "k8s.io/client-go/listers/batch/v1"

	"github.com/argoproj/argo-rollouts/metricproviders/job"
	"github.com/argoproj/argo-rollouts/metricproviders/prometheus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	InclusterKubeconfig      = "in-cluster"
	AnalysisJobKubeconfigEnv = "ARGO_ROLLOUTS_ANALYSIS_JOB_KUBECONFIG"
	AnalysisJobNamespaceEnv  = "ARGO_ROLLOUTS_ANALYSIS_JOB_NAMESPACE"
)

type ProviderFactory struct {
	KubeClient kubernetes.Interface
	JobLister  batchlisters.JobLister
}

type ProviderFactoryFunc func(logCtx log.Entry, metric v1alpha1.Metric) (metric.Provider, error)

// NewProvider creates the correct provider based on the provider type of the Metric
func (f *ProviderFactory) NewProvider(logCtx log.Entry, metric v1alpha1.Metric) (metric.Provider, error) {
	switch provider := Type(metric); provider {
	case prometheus.ProviderType:
		api, err := prometheus.NewPrometheusAPI(metric)
		if err != nil {
			return nil, err
		}
		return prometheus.NewPrometheusProvider(api, logCtx, metric)
	case job.ProviderType:
		kubeClient, customKubeconfig, err := GetAnalysisJobClientset(f.KubeClient)
		if err != nil {
			return nil, err
		}

		return job.NewJobProvider(logCtx, kubeClient, f.JobLister, GetAnalysisJobNamespace(), customKubeconfig), nil
	case kayenta.ProviderType:
		c := kayenta.NewHttpClient()
		return kayenta.NewKayentaProvider(logCtx, c), nil
	case webmetric.ProviderType:
		c, err := webmetric.NewWebMetricHttpClient(metric)
		if err != nil {
			return nil, err
		}
		p, err := webmetric.NewWebMetricJsonParser(metric)
		if err != nil {
			return nil, err
		}
		return webmetric.NewWebMetricProvider(logCtx, c, p), nil
	case datadog.ProviderType:
		return datadog.NewDatadogProvider(logCtx, f.KubeClient, metric)
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
	case skywalking.ProviderType:
		client, err := skywalking.NewSkyWalkingClient(metric, f.KubeClient)
		if err != nil {
			return nil, err
		}
		return skywalking.NewSkyWalkingProvider(client, logCtx), nil
	case plugin.ProviderType:
		plugin, err := plugin.NewRpcPlugin(metric)
		if err != nil {
			return nil, fmt.Errorf("failed to create plugin: %v", err)
		}
		return plugin, nil
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
	} else if metric.Provider.SkyWalking != nil {
		return skywalking.ProviderType
	} else if metric.Provider.Plugin != nil {
		return plugin.ProviderType
	}

	return "Unknown Provider"
}

// GetAnalysisJobClientset returns kubernetes clientset for executing the analysis job metric,
// if the AnalysisJobKubeconfigEnv is set to InclusterKubeconfig, it will return the incluster client
// else if it's set to a kubeconfig file it will return the clientset corresponding to the kubeconfig file.
// If empty it returns the provided defaultClientset
func GetAnalysisJobClientset(defaultClientset kubernetes.Interface) (kubernetes.Interface, bool, error) {
	customJobKubeconfig := os.Getenv(AnalysisJobKubeconfigEnv)
	if customJobKubeconfig != "" {
		var (
			cfg *rest.Config
			err error
		)
		if customJobKubeconfig == InclusterKubeconfig {
			cfg, err = rest.InClusterConfig()
		} else {
			cfg, err = clientcmd.BuildConfigFromFlags("", customJobKubeconfig)
		}
		if err != nil {
			return nil, true, err
		}
		clientSet, err := kubernetes.NewForConfig(cfg)
		return clientSet, true, err
	}
	return defaultClientset, false, nil
}

func GetAnalysisJobNamespace() string {
	return os.Getenv(AnalysisJobNamespaceEnv)
}
