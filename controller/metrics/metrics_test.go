package metrics

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informerfactory "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

func newFakeServerConfig(objs ...runtime.Object) ServerConfig {
	fakeClient := fake.NewSimpleClientset(objs...)
	factory := informerfactory.NewSharedInformerFactory(fakeClient, 0)
	roInformer := factory.Argoproj().V1alpha1().Rollouts()
	arInformer := factory.Argoproj().V1alpha1().AnalysisRuns()
	atInformer := factory.Argoproj().V1alpha1().AnalysisTemplates()
	catInformer := factory.Argoproj().V1alpha1().ClusterAnalysisTemplates()
	exInformer := factory.Argoproj().V1alpha1().Experiments()
	ctx, cancel := context.WithCancel(context.TODO())

	var hasSyncedFuncs = make([]cache.InformerSynced, 0)
	for _, inf := range []cache.SharedIndexInformer{
		roInformer.Informer(),
		arInformer.Informer(),
		atInformer.Informer(),
		catInformer.Informer(),
		exInformer.Informer(),
	} {
		go inf.Run(ctx.Done())
		hasSyncedFuncs = append(hasSyncedFuncs, inf.HasSynced)

	}
	cache.WaitForCacheSync(ctx.Done(), hasSyncedFuncs...)
	cancel()

	return ServerConfig{
		RolloutLister:                 roInformer.Lister(),
		AnalysisRunLister:             arInformer.Lister(),
		AnalysisTemplateLister:        atInformer.Lister(),
		ClusterAnalysisTemplateLister: catInformer.Lister(),
		ExperimentLister:              exInformer.Lister(),
		K8SRequestProvider:            &K8sRequestsCountProvider{},
	}
}

func testHttpResponse(t *testing.T, handler http.Handler, expectedResponse string, testFunc func(t assert.TestingT, s interface{}, contains interface{}, msgAndArgs ...interface{}) bool) {
	t.Helper()
	req, err := http.NewRequest("GET", "/metrics", nil)
	assert.NoError(t, err)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, rr.Code, http.StatusOK)
	body := rr.Body.String()
	log.Println(body)
	for _, line := range strings.Split(expectedResponse, "\n") {
		testFunc(t, body, line)
	}
}

type testCombination struct {
	resource         string
	expectedResponse string
}

func TestIncError(t *testing.T) {
	expectedResponse := `# HELP analysis_run_reconcile_error Error occurring during the analysis run
# TYPE analysis_run_reconcile_error counter
analysis_run_reconcile_error{name="name",namespace="ns"} 1
# HELP experiment_reconcile_error Error occurring during the experiment
# TYPE experiment_reconcile_error counter
experiment_reconcile_error{name="name",namespace="ns"} 1
# HELP rollout_reconcile_error Error occurring during the rollout
# TYPE rollout_reconcile_error counter
rollout_reconcile_error{name="name",namespace="ns"} 1`

	metricsServ := NewMetricsServer(newFakeServerConfig())

	metricsServ.IncError("ns", "name", logutil.AnalysisRunKey)
	metricsServ.IncError("ns", "name", logutil.ExperimentKey)
	metricsServ.IncError("ns", "name", logutil.RolloutKey)
	testHttpResponse(t, metricsServ.Handler, expectedResponse, assert.Contains)
}

func TestVersionInfo(t *testing.T) {
	expectedResponse := `# HELP argo_rollouts_controller_info Running Argo-rollouts version
# TYPE argo_rollouts_controller_info gauge`
	metricsServ := NewMetricsServer(newFakeServerConfig())
	testHttpResponse(t, metricsServ.Handler, expectedResponse, assert.Contains)
}

func TestRemove(t *testing.T) {
	defaults.SetMetricCleanupDelaySeconds(1)

	expectedResponse := `analysis_run_reconcile_error{name="name1",namespace="ns"} 1
experiment_reconcile_error{name="name1",namespace="ns"} 1
rollout_reconcile_error{name="name1",namespace="ns"} 1`

	metricsServ := NewMetricsServer(newFakeServerConfig())

	metricsServ.IncError("ns", "name1", logutil.RolloutKey)
	metricsServ.IncError("ns", "name1", logutil.AnalysisRunKey)
	metricsServ.IncError("ns", "name1", logutil.ExperimentKey)
	testHttpResponse(t, metricsServ.Handler, expectedResponse, assert.Contains)

	metricsServ.Remove("ns", "name1", logutil.AnalysisRunKey)
	metricsServ.Remove("ns", "name1", logutil.ExperimentKey)
	metricsServ.Remove("ns", "name1", logutil.RolloutKey)

	//Sleep for 2x the cleanup delay to allow metrics to be removed
	time.Sleep(defaults.GetMetricCleanupDelaySeconds() * 2)
	testHttpResponse(t, metricsServ.Handler, expectedResponse, assert.NotContains)
}
