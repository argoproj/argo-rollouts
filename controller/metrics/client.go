package metrics

import (
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	goclient "github.com/argoproj/argo-rollouts/utils/go-client"
)

const (
	clientsetMetricsNamespace = "controller_clientset"
)

type K8sRequestsCountProvider struct {
	k8sRequestsCount *prometheus.CounterVec
}

func (f *K8sRequestsCountProvider) Register(registerer prometheus.Registerer) {
	f.k8sRequestsCount = k8sRequestsCount
	registerer.MustRegister(f.k8sRequestsCount)
}

var (
	// Custom events metric
	k8sRequestsCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: clientsetMetricsNamespace,
			Name:      "k8s_request_total",
			Help:      "Number of kubernetes requests executed during application reconciliation.",
		},
		[]string{"kind", "namespace", "name", "verb", "statusCode"},
	)
)

// IncKubernetesRequest increments the kubernetes client counter
func (m *K8sRequestsCountProvider) IncKubernetesRequest(resourceInfo goclient.ResourceInfo) error {
	name := resourceInfo.Name
	namespace := resourceInfo.Namespace
	kind := resourceInfo.Kind
	statusCode := strconv.Itoa(resourceInfo.StatusCode)
	if resourceInfo.Verb == goclient.List {
		name = "N/A"
	}
	if resourceInfo.Verb == goclient.Unknown {
		namespace = "Unknown"
		name = "Unknown"
		kind = "Unknown"
	}

	m.k8sRequestsCount.WithLabelValues(kind, namespace, name, string(resourceInfo.Verb), statusCode).Inc()
	return nil
}
