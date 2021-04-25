package metrics

import (
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	lister "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

func testHttpResponse(t *testing.T, handler http.Handler, expectedResponse string) {
	t.Helper()
	req, err := http.NewRequest("GET", "/metrics", nil)
	assert.NoError(t, err)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, rr.Code, http.StatusOK)
	body := rr.Body.String()
	log.Println(body)
	for _, line := range strings.Split(expectedResponse, "\n") {
		assert.Contains(t, body, line)
	}
}

type testCombination struct {
	resource         string
	expectedResponse string
}

type fakeRolloutLister struct {
	rollouts []*v1alpha1.Rollout
	error    error
}

func (f fakeRolloutLister) List(selector labels.Selector) ([]*v1alpha1.Rollout, error) {
	return f.rollouts, f.error
}

func (f fakeRolloutLister) Rollouts(namespace string) lister.RolloutNamespaceLister {
	return nil
}

type fakeExperimentLister struct {
	experiments []*v1alpha1.Experiment
	error       error
}

func (f fakeExperimentLister) List(selector labels.Selector) (exp []*v1alpha1.Experiment, err error) {
	return f.experiments, f.error
}

func (f fakeExperimentLister) Experiments(namespace string) lister.ExperimentNamespaceLister {
	return nil
}

type fakeAnalysisRunLister struct {
	analysisRuns []*v1alpha1.AnalysisRun
	error        error
}

func (f fakeAnalysisRunLister) List(selector labels.Selector) (ars []*v1alpha1.AnalysisRun, err error) {
	return f.analysisRuns, f.error
}

func (f fakeAnalysisRunLister) AnalysisRuns(namespace string) lister.AnalysisRunNamespaceLister {
	return nil
}

func TestIncError(t *testing.T) {
	expectedResponse := `# HELP analysis_run_reconcile_error Error occurring during the analysis run
# TYPE analysis_run_reconcile_error counter
analysis_run_reconcile_error{name="name",namespace="ns"} 1
# HELP experiment_reconcile_error Error occurring during the experiment
# TYPE experiment_reconcile_error counter
# HELP rollout_reconcile_error Error occurring during the rollout
# TYPE rollout_reconcile_error counter
rollout_reconcile_error{name="name",namespace="ns"} 1`

	provider := &K8sRequestsCountProvider{}

	metricsServ := NewMetricsServer(ServerConfig{
		RolloutLister:      fakeRolloutLister{},
		ExperimentLister:   fakeExperimentLister{},
		AnalysisRunLister:  fakeAnalysisRunLister{},
		K8SRequestProvider: provider,
	})

	metricsServ.IncError("ns", "name", logutil.AnalysisRunKey)
	metricsServ.IncError("ns", "name", logutil.ExperimentKey)
	metricsServ.IncError("ns", "name", logutil.RolloutKey)
	testHttpResponse(t, metricsServ.Handler, expectedResponse)
}
