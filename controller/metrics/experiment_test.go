package metrics

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	fakeExperiment = `
apiVersion: argoproj.io/v1alpha1
kind: Experiment
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"argoproj.io/v1alpha1","kind":"Experiment","metadata":{"annotations":{},"name":"experiment-with-analysis","namespace":"argo-rollouts"},"spec":{"analyses":[{"name":"pass","templateName":"pass"}],"duration":"30s","templates":[{"name":"purple","selector":{"matchLabels":{"app":"rollouts-demo"}},"template":{"metadata":{"labels":{"app":"rollouts-demo"}},"spec":{"containers":[{"image":"argoproj/rollouts-demo:purple","imagePullPolicy":"Always","name":"rollouts-demo"}]}}},{"name":"orange","selector":{"matchLabels":{"app":"rollouts-demo"}},"template":{"metadata":{"labels":{"app":"rollouts-demo"}},"spec":{"containers":[{"image":"argoproj/rollouts-demo:orange","imagePullPolicy":"Always","name":"rollouts-demo"}]}}}]}}
  creationTimestamp: "2020-02-04T19:12:03Z"
  generation: 16
  name: experiment-with-analysis
  namespace: argo-rollouts
  resourceVersion: "81876006"
  selfLink: /apis/argoproj.io/v1alpha1/namespaces/argo-rollouts/experiments/experiment-with-analysis
  uid: 3a64d561-4782-11ea-b316-42010aa80065
spec:
  analyses:
  - name: pass
    templateName: pass
  duration: 30s
  templates:
  - name: purple
    selector:
      matchLabels:
        app: rollouts-demo
    template:
      metadata:
        labels:
          app: rollouts-demo
      spec:
        containers:
        - image: argoproj/rollouts-demo:purple
          imagePullPolicy: Always
          name: rollouts-demo
  - name: orange
    selector:
      matchLabels:
        app: rollouts-demo
    template:
      metadata:
        labels:
          app: rollouts-demo
      spec:
        containers:
        - image: argoproj/rollouts-demo:orange
          imagePullPolicy: Always
          name: rollouts-demo
status:
  analysisRuns:
  - analysisRun: experiment-with-analysis-pass
    name: pass
    phase: Running
  availableAt: "2020-02-04T19:12:07Z"
  conditions:
  - lastTransitionTime: "2020-02-04T19:12:37Z"
    lastUpdateTime: "2020-02-04T19:12:37Z"
    message: Experiment "experiment-with-analysis" has successfully ran and completed.
    reason: ExperimentCompleted
    status: "False"
    type: Progressing
  phase: Successful
  templateStatuses:
  - availableReplicas: 0
    lastTransitionTime: "2020-02-04T19:12:37Z"
    name: purple
    readyReplicas: 0
    replicas: 0
    status: Successful
    updatedReplicas: 0
  - availableReplicas: 0
    lastTransitionTime: "2020-02-04T19:12:37Z"
    name: orange
    readyReplicas: 0
    replicas: 0
    status: Successful
    updatedReplicas: 0
`
)

const expectedExperimentResponse = `
# HELP experiment_info Information about Experiment.
# TYPE experiment_info gauge
experiment_info{name="experiment-with-analysis",namespace="argo-rollouts",phase="Successful"} 1
# HELP experiment_phase Information on the state of the experiment (DEPRECATED - use experiment_info)
# TYPE experiment_phase gauge
experiment_phase{name="experiment-with-analysis",namespace="argo-rollouts",phase="Error"} 0
experiment_phase{name="experiment-with-analysis",namespace="argo-rollouts",phase="Inconclusive"} 0
experiment_phase{name="experiment-with-analysis",namespace="argo-rollouts",phase="Pending"} 0
experiment_phase{name="experiment-with-analysis",namespace="argo-rollouts",phase="Running"} 0
experiment_phase{name="experiment-with-analysis",namespace="argo-rollouts",phase="Successful"} 1
`

func newFakeExperiment(fakeExperiment string) *v1alpha1.Experiment {
	var experiment v1alpha1.Experiment
	err := yaml.Unmarshal([]byte(fakeExperiment), &experiment)
	if err != nil {
		panic(err)
	}
	return &experiment
}

func TestCollectExperiments(t *testing.T) {
	combinations := []testCombination{
		{
			resource:         fakeExperiment,
			expectedResponse: expectedExperimentResponse,
		},
	}

	for _, combination := range combinations {
		testExperimentDescribe(t, combination.resource, combination.expectedResponse)
	}
}

func testExperimentDescribe(t *testing.T, fakeExperiment string, expectedResponse string) {
	registry := prometheus.NewRegistry()
	config := newFakeServerConfig(newFakeExperiment(fakeExperiment))
	registry.MustRegister(NewExperimentCollector(config.ExperimentLister))
	mux := http.NewServeMux()
	mux.Handle(MetricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	testHttpResponse(t, mux, expectedResponse, assert.Contains)
}

func TestIncExperimentReconcile(t *testing.T) {
	expectedResponse := `# HELP experiment_reconcile Experiments reconciliation performance.
# TYPE experiment_reconcile histogram
experiment_reconcile_bucket{name="ex-test",namespace="ex-namespace",le="0.01"} 1
experiment_reconcile_bucket{name="ex-test",namespace="ex-namespace",le="0.15"} 1
experiment_reconcile_bucket{name="ex-test",namespace="ex-namespace",le="0.25"} 1
experiment_reconcile_bucket{name="ex-test",namespace="ex-namespace",le="0.5"} 1
experiment_reconcile_bucket{name="ex-test",namespace="ex-namespace",le="1"} 1
experiment_reconcile_bucket{name="ex-test",namespace="ex-namespace",le="+Inf"} 1
experiment_reconcile_sum{name="ex-test",namespace="ex-namespace"} 0.001
experiment_reconcile_count{name="ex-test",namespace="ex-namespace"} 1`

	metricsServ := NewMetricsServer(newFakeServerConfig())
	ex := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ex-test",
			Namespace: "ex-namespace",
		},
	}
	metricsServ.IncExperimentReconcile(ex, time.Millisecond)
	testHttpResponse(t, metricsServ.Handler, expectedResponse, assert.Contains)
}
