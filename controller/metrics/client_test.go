package metrics

import (
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	kubeclientmetrics "github.com/argoproj/argo-rollouts/utils/kubeclientmetrics"
)

const expectedKubernetesRequest = `# TYPE controller_clientset_k8s_request_total counter
controller_clientset_k8s_request_total{kind="Unknown",name="Unknown",namespace="Unknown",status_code="200",verb="Unknown"} 1
controller_clientset_k8s_request_total{kind="replicasets",name="N/A",namespace="default",status_code="200",verb="List"} 1`

func TestIncKubernetesRequest(t *testing.T) {
	cancel, rolloutLister := newFakeLister(noRollouts)
	defer cancel()
	provider := &K8sRequestsCountProvider{}
	metricsServ := NewMetricsServer("localhost:8080", rolloutLister, provider)
	provider.IncKubernetesRequest(kubeclientmetrics.ResourceInfo{
		Kind:       "replicasets",
		Namespace:  "default",
		Name:       "test",
		Verb:       kubeclientmetrics.List,
		StatusCode: 200,
	})
	provider.IncKubernetesRequest(kubeclientmetrics.ResourceInfo{
		Verb:       kubeclientmetrics.Unknown,
		StatusCode: 200,
	})
	req, err := http.NewRequest("GET", "/metrics", nil)
	assert.NoError(t, err)
	rr := httptest.NewRecorder()
	metricsServ.Handler.ServeHTTP(rr, req)
	assert.Equal(t, rr.Code, http.StatusOK)
	body := rr.Body.String()
	log.Println(body)
	assertMetricsPrinted(t, expectedKubernetesRequest, body)
}
