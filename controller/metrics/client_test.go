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

func incRequest(provider *K8sRequestsCountProvider, kind, namespace, name string, verb kubeclientmetrics.K8sRequestVerb) {
	provider.IncKubernetesRequest(kubeclientmetrics.ResourceInfo{
		Kind:       kind,
		Namespace:  namespace,
		Name:       name,
		Verb:       verb,
		StatusCode: 200,
	})
}

func TestIncKubernetesRequestEphemeralResourcesSanitized(t *testing.T) {
	config := newFakeServerConfig()
	metricsServ := NewMetricsServer(config)

	// AnalysisRuns with unique names should all be sanitized to N/A
	incRequest(config.K8SRequestProvider, "analysisruns", "default", "my-rollout-abc123-1", kubeclientmetrics.Get)
	incRequest(config.K8SRequestProvider, "analysisruns", "default", "my-rollout-def456-2", kubeclientmetrics.Get)

	// Experiments with unique names should also be sanitized to N/A
	incRequest(config.K8SRequestProvider, "experiments", "default", "my-experiment-abc123-1", kubeclientmetrics.Get)

	// Rollouts have stable names and should NOT be sanitized
	incRequest(config.K8SRequestProvider, "rollouts", "default", "my-rollout", kubeclientmetrics.Get)

	// Both analysisrun requests should be collapsed into a single metric with name="N/A" and count=2
	expectedAnalysisRuns := `controller_clientset_k8s_request_total{kind="analysisruns",name="N/A",namespace="default",status_code="200",verb="Get"} 2`
	testHttpResponse(t, metricsServ.Handler, expectedAnalysisRuns, assert.Contains)

	// Experiment request should be sanitized to N/A
	expectedExperiments := `controller_clientset_k8s_request_total{kind="experiments",name="N/A",namespace="default",status_code="200",verb="Get"} 1`
	testHttpResponse(t, metricsServ.Handler, expectedExperiments, assert.Contains)

	// Rollout name should be preserved (not sanitized)
	expectedRollout := `controller_clientset_k8s_request_total{kind="rollouts",name="my-rollout",namespace="default",status_code="200",verb="Get"} 1`
	testHttpResponse(t, metricsServ.Handler, expectedRollout, assert.Contains)

	// The unique ephemeral names should NOT appear in the metrics output
	for _, unexpectedName := range []string{`name="my-rollout-abc123-1"`, `name="my-rollout-def456-2"`, `name="my-experiment-abc123-1"`} {
		testHttpResponse(t, metricsServ.Handler, unexpectedName, assert.NotContains)
	}
}
