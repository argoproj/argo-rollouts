package metrics

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
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

	fakeAnalysisTemplate = `
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  creationTimestamp: "2020-03-16T20:01:13Z"
  name: http-benchmark-test
  namespace: jesse-test
spec:
  metrics:
  - name: web-metric-1
    provider:
      web:
        jsonPath: .
        url: https://www.google.com
    successCondition: "true"
  - name: web-metric-2
    dryRun: true
    provider:
      web:
        jsonPath: .
        url: https://www.msn.com
    successCondition: "false"
`

	fakeClusterAnalysisTemplate = `
apiVersion: argoproj.io/v1alpha1
kind: ClusterAnalysisTemplate
metadata:
  creationTimestamp: "2020-03-16T20:01:13Z"
  name: http-benchmark-cluster-test
spec:
  metrics:
  - name: web-metric-1
    provider:
      web:
        jsonPath: .
        url: https://www.google.com
    successCondition: "true"
  - name: web-metric-2
    dryRun: true
    provider:
      web:
        jsonPath: .
        url: https://www.msn.com
    successCondition: "false"
`
)
const expectedAnalysisRunResponse = `# HELP analysis_run_info Information about analysis run.
# TYPE analysis_run_info gauge
analysis_run_info{name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Error"} 1
# HELP analysis_run_metric_phase Information on the duration of a specific metric in the Analysis Run
# TYPE analysis_run_metric_phase gauge
analysis_run_metric_phase{dry_run="false",metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Error",type="Web"} 1
analysis_run_metric_phase{dry_run="false",metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Failed",type="Web"} 0
analysis_run_metric_phase{dry_run="false",metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Inconclusive",type="Web"} 0
analysis_run_metric_phase{dry_run="false",metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Pending",type="Web"} 0
analysis_run_metric_phase{dry_run="false",metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Running",type="Web"} 0
analysis_run_metric_phase{dry_run="false",metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",phase="Successful",type="Web"} 0
# HELP analysis_run_metric_type Information on the type of a specific metric in the Analysis Runs
# TYPE analysis_run_metric_type gauge
analysis_run_metric_type{metric="webmetric",name="http-benchmark-test-tr8rn",namespace="jesse-test",type="Web"} 1
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

func newFakeAnalysisTemplate(yamlStr string) *v1alpha1.AnalysisTemplate {
	var at v1alpha1.AnalysisTemplate
	err := yaml.Unmarshal([]byte(yamlStr), &at)
	if err != nil {
		panic(err)
	}
	return &at
}

func newFakeClusterAnalysisTemplate(yamlStr string) *v1alpha1.ClusterAnalysisTemplate {
	var at v1alpha1.ClusterAnalysisTemplate
	err := yaml.Unmarshal([]byte(yamlStr), &at)
	if err != nil {
		panic(err)
	}
	return &at
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

func testAnalysisRunDescribe(t *testing.T, fakeAnalysisRun string, expectedResponse string) {
	registry := prometheus.NewRegistry()
	serverCfg := newFakeServerConfig(newFakeAnalysisRun(fakeAnalysisRun))
	registry.MustRegister(NewAnalysisRunCollector(serverCfg.AnalysisRunLister, serverCfg.AnalysisTemplateLister, serverCfg.ClusterAnalysisTemplateLister))
	mux := http.NewServeMux()
	mux.Handle(MetricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	testHttpResponse(t, mux, expectedResponse, assert.Contains)
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
	metricsServ := NewMetricsServer(newFakeServerConfig())
	ar := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ar-test",
			Namespace: "ar-namespace",
		},
	}
	metricsServ.IncAnalysisRunReconcile(ar, time.Millisecond)
	testHttpResponse(t, metricsServ.Handler, expectedResponse, assert.Contains)
}

func TestAnalysisTemplateDescribe(t *testing.T) {
	expectedResponse := `# HELP analysis_template_info Information about analysis templates.
# TYPE analysis_template_info gauge
analysis_template_info{name="http-benchmark-cluster-test",namespace=""} 1
analysis_template_info{name="http-benchmark-test",namespace="jesse-test"} 1
# HELP analysis_template_metric_info Information on metrics in analysis templates.
# TYPE analysis_template_metric_info gauge
analysis_template_metric_info{metric="web-metric-1",name="http-benchmark-cluster-test",namespace="",type="Web"} 1
analysis_template_metric_info{metric="web-metric-1",name="http-benchmark-test",namespace="jesse-test",type="Web"} 1
analysis_template_metric_info{metric="web-metric-2",name="http-benchmark-cluster-test",namespace="",type="Web"} 1
analysis_template_metric_info{metric="web-metric-2",name="http-benchmark-test",namespace="jesse-test",type="Web"} 1
`
	registry := prometheus.NewRegistry()
	at := newFakeAnalysisTemplate(fakeAnalysisTemplate)
	cat := newFakeClusterAnalysisTemplate(fakeClusterAnalysisTemplate)
	serverCfg := newFakeServerConfig(at, cat)
	registry.MustRegister(NewAnalysisRunCollector(serverCfg.AnalysisRunLister, serverCfg.AnalysisTemplateLister, serverCfg.ClusterAnalysisTemplateLister))
	mux := http.NewServeMux()
	mux.Handle(MetricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	testHttpResponse(t, mux, expectedResponse, assert.Contains)
}
