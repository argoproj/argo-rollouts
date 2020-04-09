package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/utils/kubeclientmetrics"
)

const expectedKubernetesRequest = `# TYPE controller_clientset_k8s_request_total counter
controller_clientset_k8s_request_total{kind="Unknown",name="Unknown",namespace="Unknown",status_code="200",verb="Unknown"} 1
controller_clientset_k8s_request_total{kind="replicasets",name="N/A",namespace="default",status_code="200",verb="List"} 1`

func TestIncKubernetesRequest(t *testing.T) {
	provider := &K8sRequestsCountProvider{}

	metricsServ := NewMetricsServer(ServerConfig{
		RolloutLister:      fakeRolloutLister{},
		ExperimentLister:   fakeExperimentLister{},
		AnalysisRunLister:  fakeAnalysisRunLister{},
		K8SRequestProvider: provider,
	})
	err := provider.IncKubernetesRequest(kubeclientmetrics.ResourceInfo{
		Kind:       "replicasets",
		Namespace:  "default",
		Name:       "test",
		Verb:       kubeclientmetrics.List,
		StatusCode: 200,
	})
	assert.Nil(t, err)
	err = provider.IncKubernetesRequest(kubeclientmetrics.ResourceInfo{
		Verb:       kubeclientmetrics.Unknown,
		StatusCode: 200,
	})
	assert.Nil(t, err)
	testHttpResponse(t, metricsServ.Handler, expectedKubernetesRequest)
}
