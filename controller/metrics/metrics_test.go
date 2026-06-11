package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/prometheus/client_golang/prometheus/testutil"

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
	// log.Println(body)
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
	m := NewMetricsServer(newFakeServerConfig())

	now := metav1.Now()
	startTime := metav1.NewTime(now.Add(-5 * time.Minute))
	finishedAt := now
	totalManualPauseDuration := int64(60) // 1 minute
	completionStatus := v1alpha1.CompletionStatusPromoted

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

	// Total: 5 minutes = 300 seconds
	// Progression: 5 minutes - 1 minute pause = 240 seconds
	// Manual pause: 1 minute = 60 seconds
	expected := `
# HELP rollout_duration_seconds_manual_pause Time spent in manual pause waiting for human intervention
# TYPE rollout_duration_seconds_manual_pause histogram
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="0"} 0
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="60"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="300"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="600"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="1800"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="3600"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="7200"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="14400"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="28800"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="+Inf"} 1
rollout_duration_seconds_manual_pause_sum{status="promoted"} 60
rollout_duration_seconds_manual_pause_count{status="promoted"} 1

# HELP rollout_duration_seconds_progression Active progression time for a rollout (excluding manual pause time)
# TYPE rollout_duration_seconds_progression histogram
rollout_duration_seconds_progression_bucket{status="promoted",le="30"} 0
rollout_duration_seconds_progression_bucket{status="promoted",le="60"} 0
rollout_duration_seconds_progression_bucket{status="promoted",le="120"} 0
rollout_duration_seconds_progression_bucket{status="promoted",le="300"} 1
rollout_duration_seconds_progression_bucket{status="promoted",le="600"} 1
rollout_duration_seconds_progression_bucket{status="promoted",le="900"} 1
rollout_duration_seconds_progression_bucket{status="promoted",le="1800"} 1
rollout_duration_seconds_progression_bucket{status="promoted",le="3600"} 1
rollout_duration_seconds_progression_bucket{status="promoted",le="+Inf"} 1
rollout_duration_seconds_progression_sum{status="promoted"} 240
rollout_duration_seconds_progression_count{status="promoted"} 1

# HELP rollout_duration_seconds_total Total wall-clock time for a rollout from start to completion/abort/supersede
# TYPE rollout_duration_seconds_total histogram
rollout_duration_seconds_total_bucket{status="promoted",le="30"} 0
rollout_duration_seconds_total_bucket{status="promoted",le="60"} 0
rollout_duration_seconds_total_bucket{status="promoted",le="120"} 0
rollout_duration_seconds_total_bucket{status="promoted",le="300"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="600"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="1200"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="1800"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="3600"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="7200"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="14400"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="28800"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="+Inf"} 1
rollout_duration_seconds_total_sum{status="promoted"} 300
rollout_duration_seconds_total_count{status="promoted"} 1
`
	m.EmitRolloutDuration(rollout.Status.Duration)

	err := testutil.GatherAndCompare(m.Registry, strings.NewReader(expected), "rollout_duration_seconds_total", "rollout_duration_seconds_progression", "rollout_duration_seconds_manual_pause")
	require.NoError(t, err)
}

// TestEmitRolloutDuration_ManuallyPromoted tests metric emission for manually promoted rollouts
func TestEmitRolloutDuration_ManuallyPromoted(t *testing.T) {
	m := NewMetricsServer(newFakeServerConfig())

	now := metav1.Now()
	startTime := metav1.NewTime(now.Add(-3 * time.Minute))
	finishedAt := now
	completionStatus := v1alpha1.CompletionStatusFastPromoted

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

	// Total: 3 minutes = 180 seconds
	// Progression: 3 minutes (no pause) = 180 seconds
	// Manual pause: 0 seconds
	expected := `
# HELP rollout_duration_seconds_manual_pause Time spent in manual pause waiting for human intervention
# TYPE rollout_duration_seconds_manual_pause histogram
rollout_duration_seconds_manual_pause_bucket{status="fast-promoted",le="0"} 1
rollout_duration_seconds_manual_pause_bucket{status="fast-promoted",le="60"} 1
rollout_duration_seconds_manual_pause_bucket{status="fast-promoted",le="300"} 1
rollout_duration_seconds_manual_pause_bucket{status="fast-promoted",le="600"} 1
rollout_duration_seconds_manual_pause_bucket{status="fast-promoted",le="1800"} 1
rollout_duration_seconds_manual_pause_bucket{status="fast-promoted",le="3600"} 1
rollout_duration_seconds_manual_pause_bucket{status="fast-promoted",le="7200"} 1
rollout_duration_seconds_manual_pause_bucket{status="fast-promoted",le="14400"} 1
rollout_duration_seconds_manual_pause_bucket{status="fast-promoted",le="28800"} 1
rollout_duration_seconds_manual_pause_bucket{status="fast-promoted",le="+Inf"} 1
rollout_duration_seconds_manual_pause_sum{status="fast-promoted"} 0
rollout_duration_seconds_manual_pause_count{status="fast-promoted"} 1

# HELP rollout_duration_seconds_progression Active progression time for a rollout (excluding manual pause time)
# TYPE rollout_duration_seconds_progression histogram
rollout_duration_seconds_progression_bucket{status="fast-promoted",le="30"} 0
rollout_duration_seconds_progression_bucket{status="fast-promoted",le="60"} 0
rollout_duration_seconds_progression_bucket{status="fast-promoted",le="120"} 0
rollout_duration_seconds_progression_bucket{status="fast-promoted",le="300"} 1
rollout_duration_seconds_progression_bucket{status="fast-promoted",le="600"} 1
rollout_duration_seconds_progression_bucket{status="fast-promoted",le="900"} 1
rollout_duration_seconds_progression_bucket{status="fast-promoted",le="1800"} 1
rollout_duration_seconds_progression_bucket{status="fast-promoted",le="3600"} 1
rollout_duration_seconds_progression_bucket{status="fast-promoted",le="+Inf"} 1
rollout_duration_seconds_progression_sum{status="fast-promoted"} 180
rollout_duration_seconds_progression_count{status="fast-promoted"} 1

# HELP rollout_duration_seconds_total Total wall-clock time for a rollout from start to completion/abort/supersede
# TYPE rollout_duration_seconds_total histogram
rollout_duration_seconds_total_bucket{status="fast-promoted",le="30"} 0
rollout_duration_seconds_total_bucket{status="fast-promoted",le="60"} 0
rollout_duration_seconds_total_bucket{status="fast-promoted",le="120"} 0
rollout_duration_seconds_total_bucket{status="fast-promoted",le="300"} 1
rollout_duration_seconds_total_bucket{status="fast-promoted",le="600"} 1
rollout_duration_seconds_total_bucket{status="fast-promoted",le="1200"} 1
rollout_duration_seconds_total_bucket{status="fast-promoted",le="1800"} 1
rollout_duration_seconds_total_bucket{status="fast-promoted",le="3600"} 1
rollout_duration_seconds_total_bucket{status="fast-promoted",le="7200"} 1
rollout_duration_seconds_total_bucket{status="fast-promoted",le="14400"} 1
rollout_duration_seconds_total_bucket{status="fast-promoted",le="28800"} 1
rollout_duration_seconds_total_bucket{status="fast-promoted",le="+Inf"} 1
rollout_duration_seconds_total_sum{status="fast-promoted"} 180
rollout_duration_seconds_total_count{status="fast-promoted"} 1
`
	m.EmitRolloutDuration(rollout.Status.Duration)

	err := testutil.GatherAndCompare(m.Registry, strings.NewReader(expected), "rollout_duration_seconds_total", "rollout_duration_seconds_progression", "rollout_duration_seconds_manual_pause")
	require.NoError(t, err)
}

// TestEmitRolloutDuration_Aborted tests metric emission for aborted rollouts
func TestEmitRolloutDuration_Aborted(t *testing.T) {
	m := NewMetricsServer(newFakeServerConfig())

	now := metav1.Now()
	startTime := metav1.NewTime(now.Add(-2 * time.Minute))
	finishedAt := now
	completionStatus := v1alpha1.CompletionStatusAborted

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

	// Total: 2 minutes = 120 seconds
	// Progression: 2 minutes (no pause) = 120 seconds
	// Manual pause: 0 seconds
	expected := `
# HELP rollout_duration_seconds_manual_pause Time spent in manual pause waiting for human intervention
# TYPE rollout_duration_seconds_manual_pause histogram
rollout_duration_seconds_manual_pause_bucket{status="aborted",le="0"} 1
rollout_duration_seconds_manual_pause_bucket{status="aborted",le="60"} 1
rollout_duration_seconds_manual_pause_bucket{status="aborted",le="300"} 1
rollout_duration_seconds_manual_pause_bucket{status="aborted",le="600"} 1
rollout_duration_seconds_manual_pause_bucket{status="aborted",le="1800"} 1
rollout_duration_seconds_manual_pause_bucket{status="aborted",le="3600"} 1
rollout_duration_seconds_manual_pause_bucket{status="aborted",le="7200"} 1
rollout_duration_seconds_manual_pause_bucket{status="aborted",le="14400"} 1
rollout_duration_seconds_manual_pause_bucket{status="aborted",le="28800"} 1
rollout_duration_seconds_manual_pause_bucket{status="aborted",le="+Inf"} 1
rollout_duration_seconds_manual_pause_sum{status="aborted"} 0
rollout_duration_seconds_manual_pause_count{status="aborted"} 1

# HELP rollout_duration_seconds_progression Active progression time for a rollout (excluding manual pause time)
# TYPE rollout_duration_seconds_progression histogram
rollout_duration_seconds_progression_bucket{status="aborted",le="30"} 0
rollout_duration_seconds_progression_bucket{status="aborted",le="60"} 0
rollout_duration_seconds_progression_bucket{status="aborted",le="120"} 1
rollout_duration_seconds_progression_bucket{status="aborted",le="300"} 1
rollout_duration_seconds_progression_bucket{status="aborted",le="600"} 1
rollout_duration_seconds_progression_bucket{status="aborted",le="900"} 1
rollout_duration_seconds_progression_bucket{status="aborted",le="1800"} 1
rollout_duration_seconds_progression_bucket{status="aborted",le="3600"} 1
rollout_duration_seconds_progression_bucket{status="aborted",le="+Inf"} 1
rollout_duration_seconds_progression_sum{status="aborted"} 120
rollout_duration_seconds_progression_count{status="aborted"} 1

# HELP rollout_duration_seconds_total Total wall-clock time for a rollout from start to completion/abort/supersede
# TYPE rollout_duration_seconds_total histogram
rollout_duration_seconds_total_bucket{status="aborted",le="30"} 0
rollout_duration_seconds_total_bucket{status="aborted",le="60"} 0
rollout_duration_seconds_total_bucket{status="aborted",le="120"} 1
rollout_duration_seconds_total_bucket{status="aborted",le="300"} 1
rollout_duration_seconds_total_bucket{status="aborted",le="600"} 1
rollout_duration_seconds_total_bucket{status="aborted",le="1200"} 1
rollout_duration_seconds_total_bucket{status="aborted",le="1800"} 1
rollout_duration_seconds_total_bucket{status="aborted",le="3600"} 1
rollout_duration_seconds_total_bucket{status="aborted",le="7200"} 1
rollout_duration_seconds_total_bucket{status="aborted",le="14400"} 1
rollout_duration_seconds_total_bucket{status="aborted",le="28800"} 1
rollout_duration_seconds_total_bucket{status="aborted",le="+Inf"} 1
rollout_duration_seconds_total_sum{status="aborted"} 120
rollout_duration_seconds_total_count{status="aborted"} 1
`
	m.EmitRolloutDuration(rollout.Status.Duration)

	err := testutil.GatherAndCompare(m.Registry, strings.NewReader(expected), "rollout_duration_seconds_total", "rollout_duration_seconds_progression", "rollout_duration_seconds_manual_pause")
	require.NoError(t, err)
}

// TestEmitRolloutDuration_Superseded tests metric emission for superseded rollouts
func TestEmitRolloutDuration_Superseded(t *testing.T) {
	m := NewMetricsServer(newFakeServerConfig())

	now := metav1.Now()
	startTime := metav1.NewTime(now.Add(-1 * time.Minute))
	finishedAt := now
	completionStatus := v1alpha1.CompletionStatusSuperseded

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

	// Total: 1 minute = 60 seconds
	// Progression: 1 minute (no pause) = 60 seconds
	// Manual pause: 0 seconds
	expected := `
# HELP rollout_duration_seconds_manual_pause Time spent in manual pause waiting for human intervention
# TYPE rollout_duration_seconds_manual_pause histogram
rollout_duration_seconds_manual_pause_bucket{status="superseded",le="0"} 1
rollout_duration_seconds_manual_pause_bucket{status="superseded",le="60"} 1
rollout_duration_seconds_manual_pause_bucket{status="superseded",le="300"} 1
rollout_duration_seconds_manual_pause_bucket{status="superseded",le="600"} 1
rollout_duration_seconds_manual_pause_bucket{status="superseded",le="1800"} 1
rollout_duration_seconds_manual_pause_bucket{status="superseded",le="3600"} 1
rollout_duration_seconds_manual_pause_bucket{status="superseded",le="7200"} 1
rollout_duration_seconds_manual_pause_bucket{status="superseded",le="14400"} 1
rollout_duration_seconds_manual_pause_bucket{status="superseded",le="28800"} 1
rollout_duration_seconds_manual_pause_bucket{status="superseded",le="+Inf"} 1
rollout_duration_seconds_manual_pause_sum{status="superseded"} 0
rollout_duration_seconds_manual_pause_count{status="superseded"} 1

# HELP rollout_duration_seconds_progression Active progression time for a rollout (excluding manual pause time)
# TYPE rollout_duration_seconds_progression histogram
rollout_duration_seconds_progression_bucket{status="superseded",le="30"} 0
rollout_duration_seconds_progression_bucket{status="superseded",le="60"} 1
rollout_duration_seconds_progression_bucket{status="superseded",le="120"} 1
rollout_duration_seconds_progression_bucket{status="superseded",le="300"} 1
rollout_duration_seconds_progression_bucket{status="superseded",le="600"} 1
rollout_duration_seconds_progression_bucket{status="superseded",le="900"} 1
rollout_duration_seconds_progression_bucket{status="superseded",le="1800"} 1
rollout_duration_seconds_progression_bucket{status="superseded",le="3600"} 1
rollout_duration_seconds_progression_bucket{status="superseded",le="+Inf"} 1
rollout_duration_seconds_progression_sum{status="superseded"} 60
rollout_duration_seconds_progression_count{status="superseded"} 1

# HELP rollout_duration_seconds_total Total wall-clock time for a rollout from start to completion/abort/supersede
# TYPE rollout_duration_seconds_total histogram
rollout_duration_seconds_total_bucket{status="superseded",le="30"} 0
rollout_duration_seconds_total_bucket{status="superseded",le="60"} 1
rollout_duration_seconds_total_bucket{status="superseded",le="120"} 1
rollout_duration_seconds_total_bucket{status="superseded",le="300"} 1
rollout_duration_seconds_total_bucket{status="superseded",le="600"} 1
rollout_duration_seconds_total_bucket{status="superseded",le="1200"} 1
rollout_duration_seconds_total_bucket{status="superseded",le="1800"} 1
rollout_duration_seconds_total_bucket{status="superseded",le="3600"} 1
rollout_duration_seconds_total_bucket{status="superseded",le="7200"} 1
rollout_duration_seconds_total_bucket{status="superseded",le="14400"} 1
rollout_duration_seconds_total_bucket{status="superseded",le="28800"} 1
rollout_duration_seconds_total_bucket{status="superseded",le="+Inf"} 1
rollout_duration_seconds_total_sum{status="superseded"} 60
rollout_duration_seconds_total_count{status="superseded"} 1
`
	m.EmitRolloutDuration(rollout.Status.Duration)

	err := testutil.GatherAndCompare(m.Registry, strings.NewReader(expected), "rollout_duration_seconds_total", "rollout_duration_seconds_progression", "rollout_duration_seconds_manual_pause")
	require.NoError(t, err)
}

// TestEmitRolloutDuration_WithManualPause tests that manual pause time is correctly calculated
func TestEmitRolloutDuration_WithManualPause(t *testing.T) {
	m := NewMetricsServer(newFakeServerConfig())

	now := metav1.Now()
	startTime := metav1.NewTime(now.Add(-10 * time.Minute))
	finishedAt := now
	totalManualPauseDuration := int64(300) // 5 minutes
	completionStatus := v1alpha1.CompletionStatusPromoted

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

	// Total: 10 minutes = 600 seconds
	// Progression: 10 minutes - 5 minutes pause = 300 seconds
	// Manual pause: 5 minutes = 300 seconds
	expected := `
# HELP rollout_duration_seconds_manual_pause Time spent in manual pause waiting for human intervention
# TYPE rollout_duration_seconds_manual_pause histogram
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="0"} 0
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="60"} 0
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="300"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="600"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="1800"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="3600"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="7200"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="14400"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="28800"} 1
rollout_duration_seconds_manual_pause_bucket{status="promoted",le="+Inf"} 1
rollout_duration_seconds_manual_pause_sum{status="promoted"} 300
rollout_duration_seconds_manual_pause_count{status="promoted"} 1

# HELP rollout_duration_seconds_progression Active progression time for a rollout (excluding manual pause time)
# TYPE rollout_duration_seconds_progression histogram
rollout_duration_seconds_progression_bucket{status="promoted",le="30"} 0
rollout_duration_seconds_progression_bucket{status="promoted",le="60"} 0
rollout_duration_seconds_progression_bucket{status="promoted",le="120"} 0
rollout_duration_seconds_progression_bucket{status="promoted",le="300"} 1
rollout_duration_seconds_progression_bucket{status="promoted",le="600"} 1
rollout_duration_seconds_progression_bucket{status="promoted",le="900"} 1
rollout_duration_seconds_progression_bucket{status="promoted",le="1800"} 1
rollout_duration_seconds_progression_bucket{status="promoted",le="3600"} 1
rollout_duration_seconds_progression_bucket{status="promoted",le="+Inf"} 1
rollout_duration_seconds_progression_sum{status="promoted"} 300
rollout_duration_seconds_progression_count{status="promoted"} 1

# HELP rollout_duration_seconds_total Total wall-clock time for a rollout from start to completion/abort/supersede
# TYPE rollout_duration_seconds_total histogram
rollout_duration_seconds_total_bucket{status="promoted",le="30"} 0
rollout_duration_seconds_total_bucket{status="promoted",le="60"} 0
rollout_duration_seconds_total_bucket{status="promoted",le="120"} 0
rollout_duration_seconds_total_bucket{status="promoted",le="300"} 0
rollout_duration_seconds_total_bucket{status="promoted",le="600"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="1200"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="1800"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="3600"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="7200"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="14400"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="28800"} 1
rollout_duration_seconds_total_bucket{status="promoted",le="+Inf"} 1
rollout_duration_seconds_total_sum{status="promoted"} 600
rollout_duration_seconds_total_count{status="promoted"} 1
`
	m.EmitRolloutDuration(rollout.Status.Duration)

	err := testutil.GatherAndCompare(m.Registry, strings.NewReader(expected), "rollout_duration_seconds_total", "rollout_duration_seconds_progression", "rollout_duration_seconds_manual_pause")
	require.NoError(t, err)
}

// TestEmitRolloutDuration_NilDurationStatus tests that no metrics are emitted when durationStatus is nil
func TestEmitRolloutDuration_NilDurationStatus(t *testing.T) {
	m := NewMetricsServer(newFakeServerConfig())

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
	m.EmitRolloutDuration(rollout.Status.Duration)

	expected := ``
	err := testutil.GatherAndCompare(m.Registry, strings.NewReader(expected), "rollout_duration_seconds_total", "rollout_duration_seconds_progression", "rollout_duration_seconds_manual_pause")
	require.NoError(t, err)
}

// TestEmitRolloutDuration_NilRolloutStartedAt tests that no metrics are emitted when rolloutStartedAt is nil
func TestEmitRolloutDuration_NilRolloutStartedAt(t *testing.T) {
	m := NewMetricsServer(newFakeServerConfig())

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
	m.EmitRolloutDuration(rollout.Status.Duration)

	expected := ``
	err := testutil.GatherAndCompare(m.Registry, strings.NewReader(expected), "rollout_duration_seconds_total", "rollout_duration_seconds_progression", "rollout_duration_seconds_manual_pause")
	require.NoError(t, err)
}

// TestEmitRolloutDuration_NilFinishedAt tests that no metrics are emitted when finishedAt is nil (rollout still in progress)
func TestEmitRolloutDuration_NilFinishedAt(t *testing.T) {
	m := NewMetricsServer(newFakeServerConfig())

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
	m.EmitRolloutDuration(rollout.Status.Duration)

	expected := ``
	err := testutil.GatherAndCompare(m.Registry, strings.NewReader(expected), "rollout_duration_seconds_total", "rollout_duration_seconds_progression", "rollout_duration_seconds_manual_pause")
	require.NoError(t, err)
}
