package metricproviders

import (
	"fmt"
	"os/exec"

	"github.com/argoproj/argo-rollouts/metric"
	"github.com/argoproj/argo-rollouts/metricproviders/influxdb"
	"github.com/argoproj/argo-rollouts/metricproviders/plugin"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	goPlugin "github.com/hashicorp/go-plugin"

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

// Provider this is here just for backwards compatibility the interface is now in the metric package
type Provider interface {
	metric.Provider
}

type ProviderFactory struct {
	KubeClient   kubernetes.Interface
	JobLister    batchlisters.JobLister
	pluginClient *goPlugin.Client
	plugin       plugin.MetricsPlugin
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
	case plugin.ProviderType:
		_, plugin, err := f.startPluginSystem(metric)
		return plugin, err
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
	} else if metric.Provider.Plugin != nil {
		return plugin.ProviderType
	}

	return "Unknown Provider"
}

func (f *ProviderFactory) startPluginSystem(metric v1alpha1.Metric) (*goPlugin.Client, plugin.MetricsPlugin, error) {
	if defaults.GetMetricPluginLocation() == "" {
		return nil, nil, fmt.Errorf("no plugin location specified")
	}

	var handshakeConfig = goPlugin.HandshakeConfig{
		ProtocolVersion:  1,
		MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
		MagicCookieValue: "metrics",
	}

	// pluginMap is the map of plugins we can dispense.
	var pluginMap = map[string]goPlugin.Plugin{
		"RpcMetricsPlugin": &plugin.RpcMetricsPlugin{},
	}

	if f.pluginClient == nil || f.pluginClient.Exited() {
		f.pluginClient = goPlugin.NewClient(&goPlugin.ClientConfig{
			HandshakeConfig:  handshakeConfig,
			Plugins:          pluginMap,
			VersionedPlugins: nil,
			Cmd:              exec.Command(defaults.GetMetricPluginLocation()),
			Managed:          true,
		})

		rpcClient, err := f.pluginClient.Client()
		if err != nil {
			return nil, nil, err
		}

		// Request the plugin
		raw, err := rpcClient.Dispense("RpcMetricsPlugin")
		if err != nil {
			return nil, nil, err
		}

		f.plugin = raw.(plugin.MetricsPlugin)

		err = f.plugin.NewMetricsPlugin(metric)
		if err != nil {
			return nil, nil, err
		}
	}

	return f.pluginClient, f.plugin, nil
}
