package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/argoproj/pkg/kubeclientmetrics"
)

const (
	clientsetMetricsNamespace = "controller_clientset"
)

type K8sRequestsCountProvider struct {
	k8sRequestsCount *prometheus.CounterVec
}

func (f *K8sRequestsCountProvider) MustRegister(registerer prometheus.Registerer) {
	f.k8sRequestsCount = MetricK8sRequestTotal
	registerer.MustRegister(f.k8sRequestsCount)
}

// IncKubernetesRequest increments the kubernetes client counter
func (m *K8sRequestsCountProvider) IncKubernetesRequest(resourceInfo kubeclientmetrics.ResourceInfo) error {
	name := resourceInfo.Name
	namespace := resourceInfo.Namespace
	kind := resourceInfo.Kind
	statusCode := strconv.Itoa(resourceInfo.StatusCode)
	if resourceInfo.Verb == kubeclientmetrics.List || kind == "events" || kind == "replicasets" {
		name = "N/A"
	}
	if resourceInfo.Verb == kubeclientmetrics.Unknown {
		namespace = "Unknown"
		name = "Unknown"
		kind = "Unknown"
	}
	if m.k8sRequestsCount != nil {
		m.k8sRequestsCount.WithLabelValues(kind, namespace, name, string(resourceInfo.Verb), statusCode).Inc()
	}
	return nil
}
