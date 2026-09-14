package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/argoproj/pkg/kubeclientmetrics"
)

const expectedKubernetesRequest = `# TYPE controller_clientset_k8s_request_total counter
controller_clientset_k8s_request_total{kind="Unknown",name="Unknown",namespace="Unknown",status_code="200",verb="Unknown"} 1
controller_clientset_k8s_request_total{kind="replicasets",name="N/A",namespace="default",status_code="200",verb="List"} 1`

func TestIncKubernetesRequest(t *testing.T) {
	config := newFakeServerConfig()
	metricsServ := NewMetricsServer(config)
	config.K8SRequestProvider.IncKubernetesRequest(kubeclientmetrics.ResourceInfo{
		Kind:       "replicasets",
		Namespace:  "default",
		Name:       "test",
		Verb:       kubeclientmetrics.List,
		StatusCode: 200,
	})
	config.K8SRequestProvider.IncKubernetesRequest(kubeclientmetrics.ResourceInfo{
		Verb:       kubeclientmetrics.Unknown,
		StatusCode: 200,
	})
	testHttpResponse(t, metricsServ.Handler, expectedKubernetesRequest, assert.Contains)
}
