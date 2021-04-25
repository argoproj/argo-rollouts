package metrics

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	//noAnalysisRuns  = ""
	fakeAnalysisRun = `
apiVersion: argoproj.io/v1alpha1
kind: AnalysisRun
metadata:
  creationTimestamp: "2020-03-16T20:01:13Z"
  name: http-benchmark-test-tr8rn
  namespace: jesse-test
spec:
  metrics:
  - name: webmetric
    provider:
      web:
        jsonPath: .
        url: https://www.google.com
    successCondition: "true"
status:
  metricResults:
  - consecutiveError: 5
    error: 5
    measurements:
    - finishedAt: "2020-03-16T20:02:15Z"
      message: 'Could not parse JSON body: invalid character ''<'' looking for beginning
        of value'
      phase: Error
      startedAt: "2020-03-16T20:02:14Z"
    name: webmetric
    phase: Error
  phase: Error
  startedAt: "2020-03-16T20:02:15Z"
`
)
const expectedAnalysisRunResponse = `# HELP analysis_run_info Information about analysis run.
# TYPE analysis_run_info gauge
analysis_run_info{name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Error"} 1
# HELP analysis_run_metric_phase Information on the duration of a specific metric in the Analysis Run
# TYPE analysis_run_metric_phase gauge
analysis_run_metric_phase{metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Error",type="WebMetric"} 1
analysis_run_metric_phase{metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Failed",type="WebMetric"} 0
analysis_run_metric_phase{metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Inconclusive",type="WebMetric"} 0
analysis_run_metric_phase{metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Pending",type="WebMetric"} 0
analysis_run_metric_phase{metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Running",type="WebMetric"} 0
analysis_run_metric_phase{metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Successful",type="WebMetric"} 0
# HELP analysis_run_metric_type Information on the type of a specific metric in the Analysis Runs
# TYPE analysis_run_metric_type gauge
analysis_run_metric_type{metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",type="WebMetric"} 1
# HELP analysis_run_phase Information on the state of the Analysis Run (DEPRECATED - use analysis_run_info)
# TYPE analysis_run_phase gauge
analysis_run_phase{name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Error"} 1
analysis_run_phase{name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Failed"} 0
analysis_run_phase{name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Inconclusive"} 0
analysis_run_phase{name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Pending"} 0
analysis_run_phase{name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Running"} 0
analysis_run_phase{name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Successful"} 0
`

func newFakeAnalysisRun(fakeAnalysisRun string) *v1alpha1.AnalysisRun {
	var ar v1alpha1.AnalysisRun
	err := yaml.Unmarshal([]byte(fakeAnalysisRun), &ar)
	if err != nil {
		panic(err)
	}
	return &ar
}

func TestCollectAnalysisRuns(t *testing.T) {
	combinations := []testCombination{
		{
			resource:         fakeAnalysisRun,
			expectedResponse: expectedAnalysisRunResponse,
		},
	}

	for _, combination := range combinations {
		testAnalysisRunDescribe(t, combination.resource, combination.expectedResponse)
	}
}

func TestCollectAnalysisRunsListFails(t *testing.T) {
	buf := bytes.NewBufferString("")
	logrus.SetOutput(buf)
	registry := prometheus.NewRegistry()
	fakeLister := fakeAnalysisRunLister{
		error: fmt.Errorf("Error with lister"),
	}
	registry.MustRegister(NewAnalysisRunCollector(fakeLister))
	mux := http.NewServeMux()
	mux.Handle(MetricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	testHttpResponse(t, mux, "")
	assert.Contains(t, buf.String(), "Error with lister")
}

func testAnalysisRunDescribe(t *testing.T, fakeAnalysisRun string, expectedResponse string) {
	registry := prometheus.NewRegistry()
	fakeLister := fakeAnalysisRunLister{
		analysisRuns: []*v1alpha1.AnalysisRun{newFakeAnalysisRun(fakeAnalysisRun)},
	}
	registry.MustRegister(NewAnalysisRunCollector(fakeLister))
	mux := http.NewServeMux()
	mux.Handle(MetricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	testHttpResponse(t, mux, expectedResponse)
}

func TestIncAnalysisRunReconcile(t *testing.T) {
	expectedResponse := `# HELP analysis_run_reconcile Analysis Run reconciliation performance.
# TYPE analysis_run_reconcile histogram
analysis_run_reconcile_bucket{name="ar-test",namespace="ar-namespace",le="0.01"} 1
analysis_run_reconcile_bucket{name="ar-test",namespace="ar-namespace",le="0.15"} 1
analysis_run_reconcile_bucket{name="ar-test",namespace="ar-namespace",le="0.25"} 1
analysis_run_reconcile_bucket{name="ar-test",namespace="ar-namespace",le="0.5"} 1
analysis_run_reconcile_bucket{name="ar-test",namespace="ar-namespace",le="1"} 1
analysis_run_reconcile_bucket{name="ar-test",namespace="ar-namespace",le="+Inf"} 1
analysis_run_reconcile_sum{name="ar-test",namespace="ar-namespace"} 0.001
analysis_run_reconcile_count{name="ar-test",namespace="ar-namespace"} 1`
	provider := &K8sRequestsCountProvider{}

	metricsServ := NewMetricsServer(ServerConfig{
		RolloutLister:      fakeRolloutLister{},
		ExperimentLister:   fakeExperimentLister{},
		AnalysisRunLister:  fakeAnalysisRunLister{},
		K8SRequestProvider: provider,
	})
	ar := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ar-test",
			Namespace: "ar-namespace",
		},
	}
	metricsServ.IncAnalysisRunReconcile(ar, time.Millisecond)
	testHttpResponse(t, metricsServ.Handler, expectedResponse)
}
