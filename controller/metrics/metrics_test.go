package metrics

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/utils/defaults"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
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

func testHttpResponse(t *testing.T, handler http.Handler, expectedResponse string, testFunc func(t assert.TestingT, s any, contains any, msgAndArgs ...any) bool) {
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

// TestEmitRolloutDuration_Promoted tests metric emission for promoted rollouts
func TestEmitRolloutDuration_Promoted(t *testing.T) {
	metricsServ := NewMetricsServer(newFakeServerConfig())

	now := metav1.Now()
	startTime := metav1.NewTime(now.Add(-5 * time.Minute))
	finishedAt := now
	totalManualPauseDuration := int64(60) // 1 minute
	completionStatus := "promoted"

	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
		},
		Status: v1alpha1.RolloutStatus{
			Duration: &v1alpha1.RolloutDurationStatus{
				RolloutStartedAt:         &startTime,
				FinishedAt:               &finishedAt,
				CompletionStatus:         &completionStatus,
				TotalManualPauseDuration: &totalManualPauseDuration,
			},
		},
	}

	metricsServ.EmitRolloutDuration(rollout)

	// Verify all three metrics are emitted with correct sum values
	// Total: 5 minutes = 300 seconds
	// Progression: 5 minutes - 1 minute pause = 240 seconds
	// Manual pause: 1 minute = 60 seconds

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_total_sum{status="promoted"} 300`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_total_count{status="promoted"} 1`, assert.Contains)

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_progression_sum{status="promoted"} 240`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_progression_count{status="promoted"} 1`, assert.Contains)

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_manual_pause_sum{status="promoted"} 60`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_manual_pause_count{status="promoted"} 1`, assert.Contains)
}

// TestEmitRolloutDuration_ManuallyPromoted tests metric emission for manually promoted rollouts
func TestEmitRolloutDuration_ManuallyPromoted(t *testing.T) {
	metricsServ := NewMetricsServer(newFakeServerConfig())

	now := metav1.Now()
	startTime := metav1.NewTime(now.Add(-3 * time.Minute))
	finishedAt := now
	completionStatus := "manually-promoted"

	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
		},
		Status: v1alpha1.RolloutStatus{
			Duration: &v1alpha1.RolloutDurationStatus{
				RolloutStartedAt: &startTime,
				FinishedAt:       &finishedAt,
				CompletionStatus: &completionStatus,
			},
		},
	}

	metricsServ.EmitRolloutDuration(rollout)

	// Verify all three metrics are emitted with correct sum values
	// Total: 3 minutes = 180 seconds
	// Progression: 3 minutes (no pause) = 180 seconds
	// Manual pause: 0 seconds

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_total_sum{status="manually-promoted"} 180`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_total_count{status="manually-promoted"} 1`, assert.Contains)

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_progression_sum{status="manually-promoted"} 180`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_progression_count{status="manually-promoted"} 1`, assert.Contains)

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_manual_pause_sum{status="manually-promoted"} 0`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_manual_pause_count{status="manually-promoted"} 1`, assert.Contains)
}

// TestEmitRolloutDuration_Aborted tests metric emission for aborted rollouts
func TestEmitRolloutDuration_Aborted(t *testing.T) {
	metricsServ := NewMetricsServer(newFakeServerConfig())

	now := metav1.Now()
	startTime := metav1.NewTime(now.Add(-2 * time.Minute))
	finishedAt := now
	completionStatus := "aborted"

	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
		},
		Status: v1alpha1.RolloutStatus{
			Duration: &v1alpha1.RolloutDurationStatus{
				RolloutStartedAt: &startTime,
				FinishedAt:       &finishedAt,
				CompletionStatus: &completionStatus,
			},
		},
	}

	metricsServ.EmitRolloutDuration(rollout)

	// Verify all three metrics are emitted with correct sum values
	// Total: 2 minutes = 120 seconds
	// Progression: 2 minutes (no pause) = 120 seconds
	// Manual pause: 0 seconds

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_total_sum{status="aborted"} 120`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_total_count{status="aborted"} 1`, assert.Contains)

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_progression_sum{status="aborted"} 120`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_progression_count{status="aborted"} 1`, assert.Contains)

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_manual_pause_sum{status="aborted"} 0`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_manual_pause_count{status="aborted"} 1`, assert.Contains)
}

// TestEmitRolloutDuration_Superseded tests metric emission for superseded rollouts
func TestEmitRolloutDuration_Superseded(t *testing.T) {
	metricsServ := NewMetricsServer(newFakeServerConfig())

	now := metav1.Now()
	startTime := metav1.NewTime(now.Add(-1 * time.Minute))
	finishedAt := now
	completionStatus := "superseded"

	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
		},
		Status: v1alpha1.RolloutStatus{
			Duration: &v1alpha1.RolloutDurationStatus{
				RolloutStartedAt: &startTime,
				FinishedAt:       &finishedAt,
				CompletionStatus: &completionStatus,
			},
		},
	}

	metricsServ.EmitRolloutDuration(rollout)

	// Verify all three metrics are emitted with correct sum values
	// Total: 1 minute = 60 seconds
	// Progression: 1 minute (no pause) = 60 seconds
	// Manual pause: 0 seconds

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_total_sum{status="superseded"} 60`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_total_count{status="superseded"} 1`, assert.Contains)

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_progression_sum{status="superseded"} 60`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_progression_count{status="superseded"} 1`, assert.Contains)

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_manual_pause_sum{status="superseded"} 0`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_manual_pause_count{status="superseded"} 1`, assert.Contains)
}

// TestEmitRolloutDuration_WithManualPause tests that manual pause time is correctly calculated
func TestEmitRolloutDuration_WithManualPause(t *testing.T) {
	metricsServ := NewMetricsServer(newFakeServerConfig())

	now := metav1.Now()
	startTime := metav1.NewTime(now.Add(-10 * time.Minute))
	finishedAt := now
	totalManualPauseDuration := int64(300) // 5 minutes
	completionStatus := "promoted"

	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout-pause",
			Namespace: "default",
		},
		Status: v1alpha1.RolloutStatus{
			Duration: &v1alpha1.RolloutDurationStatus{
				RolloutStartedAt:         &startTime,
				FinishedAt:               &finishedAt,
				CompletionStatus:         &completionStatus,
				TotalManualPauseDuration: &totalManualPauseDuration,
			},
		},
	}

	metricsServ.EmitRolloutDuration(rollout)

	// Verify all three metrics are emitted with correct sum values
	// Total: 10 minutes = 600 seconds
	// Progression: 10 minutes - 5 minutes pause = 300 seconds
	// Manual pause: 5 minutes = 300 seconds

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_total_sum{status="promoted"} 600`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_total_count{status="promoted"} 1`, assert.Contains)

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_progression_sum{status="promoted"} 300`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_progression_count{status="promoted"} 1`, assert.Contains)

	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_manual_pause_sum{status="promoted"} 300`, assert.Contains)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration_seconds_manual_pause_count{status="promoted"} 1`, assert.Contains)
}

// TestEmitRolloutDuration_NilDurationStatus tests that no metrics are emitted when durationStatus is nil
func TestEmitRolloutDuration_NilDurationStatus(t *testing.T) {
	metricsServ := NewMetricsServer(newFakeServerConfig())

	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
		},
		Status: v1alpha1.RolloutStatus{
			Duration: nil,
		},
	}

	// Should not panic and should not emit metrics
	metricsServ.EmitRolloutDuration(rollout)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration`, assert.NotContains)
}

// TestEmitRolloutDuration_NilRolloutStartedAt tests that no metrics are emitted when rolloutStartedAt is nil
func TestEmitRolloutDuration_NilRolloutStartedAt(t *testing.T) {
	metricsServ := NewMetricsServer(newFakeServerConfig())

	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
		},
		Status: v1alpha1.RolloutStatus{
			Duration: &v1alpha1.RolloutDurationStatus{
				RolloutStartedAt: nil,
			},
		},
	}

	// Should not panic and should not emit metrics
	metricsServ.EmitRolloutDuration(rollout)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration`, assert.NotContains)
}

// TestEmitRolloutDuration_NilFinishedAt tests that no metrics are emitted when finishedAt is nil (rollout still in progress)
func TestEmitRolloutDuration_NilFinishedAt(t *testing.T) {
	metricsServ := NewMetricsServer(newFakeServerConfig())

	now := metav1.Now()
	startTime := metav1.NewTime(now.Add(-5 * time.Minute))

	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
		},
		Status: v1alpha1.RolloutStatus{
			Duration: &v1alpha1.RolloutDurationStatus{
				RolloutStartedAt: &startTime,
				FinishedAt:       nil, // Still in progress
			},
		},
	}

	// Should not panic and should not emit metrics
	metricsServ.EmitRolloutDuration(rollout)
	testHttpResponse(t, metricsServ.Handler, `rollout_duration`, assert.NotContains)
}
