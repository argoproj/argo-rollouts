package rolloutplugin

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8stesting "k8s.io/client-go/testing"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	fakeargo "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/record"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

// testScheme builds a scheme with the RolloutPlugin API and the workload kind registered.
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	return scheme
}

// testWorkload is the StatefulSet validRolloutPlugin references.
func testWorkload() *appsv1.StatefulSet {
	return &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: "my-sts", Namespace: "default"}}
}

// stubPluginManager returns a fixed plugin for any name, or err if set, and reports statefulset
// enabled.
type stubPluginManager struct {
	plugin ResourcePlugin
	err    error
}

func (s *stubPluginManager) GetPlugin(name string) (ResourcePlugin, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.plugin, nil
}
func (s *stubPluginManager) IsBuiltinEnabled(id string) bool { return true }
func (s *stubPluginManager) EnabledPlugins() (map[string]ResourcePlugin, error) {
	return map[string]ResourcePlugin{"stub": s.plugin}, nil
}

// validRolloutPlugin returns a spec that passes verifyRolloutPluginSpec.
func validRolloutPlugin(name string) *v1alpha1.RolloutPlugin {
	return &v1alpha1.RolloutPlugin{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Spec: v1alpha1.RolloutPluginSpec{
			WorkloadRef: v1alpha1.WorkloadRef{
				Name:       "my-sts",
				Kind:       "StatefulSet",
				APIVersion: "apps/v1",
			},
			Plugin: v1alpha1.PluginStep{Name: "argoproj/statefulset"},
			Strategy: v1alpha1.RolloutPluginStrategy{
				Canary: &v1alpha1.PluginCanaryStrategy{},
			},
		},
	}
}

// canaryWorkloadPlugin is a stateful fake workload: SetWeight moves pods onto the update revision,
// PromoteFull makes the update revision stable, Abort moves all pods back to stable. It records
// every call so tests can assert what the controller actually did to the workload.
type canaryWorkloadPlugin struct {
	fakeResourcePlugin
	replicas       int32
	stableRevision string
	updateRevision string
	weight         int32
	// weightStuck makes VerifyWeight never succeed, simulating pods that never become available.
	weightStuck bool
	// validateErr is returned by Validate, simulating the plugin rejecting the spec.
	validateErr error
	// unready is the number of pods that are not Ready. Like a StatefulSet, the stable revision
	// only moves to the update revision once every pod is Ready.
	unready int32
	// deletionCleanupErr is returned by DeletionCleanup.
	deletionCleanupErr error

	setWeightCalls []int32
	// weightChanges records only the SetWeight calls that changed the weight. Pause and analysis
	// steps re-apply the last setWeight on every reconcile, so this is the sequence of weights the
	// workload actually went through.
	weightChanges []int32
	promoteCalls  int
	abortCalls    int
	restartCalls  int
	statusCalls   int
	cleanupCalls  int
	// observedRV is the workload resourceVersion the plugin reports having computed status from.
	observedRV string
}

func newCanaryWorkloadPlugin() *canaryWorkloadPlugin {
	return &canaryWorkloadPlugin{replicas: 10, stableRevision: "rev-1", updateRevision: "rev-1", weight: 100}
}

// deploy simulates a user updating the workload template to a new revision.
func (p *canaryWorkloadPlugin) deploy(revision string) {
	p.updateRevision = revision
	p.weight = 0
}

func (p *canaryWorkloadPlugin) GetResourceStatus(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) (*ResourceStatus, error) {
	p.statusCalls++
	updated := p.replicas
	if p.stableRevision != p.updateRevision {
		updated = p.replicas * p.weight / 100
	}
	return &ResourceStatus{
		Replicas:          p.replicas,
		UpdatedReplicas:   updated,
		ReadyReplicas:     p.replicas - p.unready,
		AvailableReplicas: p.replicas - p.unready,
		CurrentRevision:   p.stableRevision,
		UpdatedRevision:   p.updateRevision,
		Ready:             p.unready == 0,

		ObservedResourceVersion: p.observedRV,
	}, nil
}

func (p *canaryWorkloadPlugin) Validate(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	return p.validateErr
}

func (p *canaryWorkloadPlugin) SetWeight(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, weight int32) error {
	p.setWeightCalls = append(p.setWeightCalls, weight)
	if weight != p.weight {
		p.weightChanges = append(p.weightChanges, weight)
	}
	p.weight = weight
	return nil
}

func (p *canaryWorkloadPlugin) VerifyWeight(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, weight int32) (bool, error) {
	return !p.weightStuck && p.weight >= weight, nil
}

func (p *canaryWorkloadPlugin) PromoteFull(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	p.promoteCalls++
	p.weight = 100
	if p.unready == 0 {
		p.stableRevision = p.updateRevision
	}
	return nil
}

func (p *canaryWorkloadPlugin) Abort(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	p.abortCalls++
	p.weight = 0
	return nil
}

func (p *canaryWorkloadPlugin) Restart(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	p.restartCalls++
	p.weight = 0
	return nil
}

func (p *canaryWorkloadPlugin) DeletionCleanup(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	p.cleanupCalls++
	return p.deletionCleanupErr
}

// reconcileHarness drives RolloutPluginReconciler.reconcile across multiple reconciles against
// fake clients, re-reading the RolloutPlugin before each one as the real controller would.
type reconcileHarness struct {
	t      *testing.T
	r      *RolloutPluginReconciler
	argo   *fakeargo.Clientset
	plugin *canaryWorkloadPlugin
	key    types.NamespacedName
	now    time.Time
}

func newReconcileHarness(t *testing.T, rp *v1alpha1.RolloutPlugin, templates ...*v1alpha1.AnalysisTemplate) *reconcileHarness {
	t.Helper()
	scheme := testScheme(t)
	objs := []client.Object{rp, testWorkload()}
	for _, tmpl := range templates {
		objs = append(objs, tmpl)
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.RolloutPlugin{}, &v1alpha1.AnalysisRun{}).
		Build()

	argoObjs := make([]runtime.Object, 0, len(templates))
	for _, tmpl := range templates {
		argoObjs = append(argoObjs, tmpl.DeepCopy())
	}
	argo := fakeargo.NewSimpleClientset(argoObjs...)

	h := &reconcileHarness{
		t:      t,
		argo:   argo,
		plugin: newCanaryWorkloadPlugin(),
		key:    types.NamespacedName{Namespace: rp.Namespace, Name: rp.Name},
		now:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	h.r = &RolloutPluginReconciler{
		Client:            c,
		Scheme:            scheme,
		ArgoProjClientset: argo,
		PluginManager:     &stubPluginManager{plugin: h.plugin},
	}
	timeutil.SetNowTimeFunc(func() time.Time { return h.now })
	t.Cleanup(func() { timeutil.SetNowTimeFunc(time.Now) })
	return h
}

func (h *reconcileHarness) advance(d time.Duration) { h.now = h.now.Add(d) }

func (h *reconcileHarness) get() *v1alpha1.RolloutPlugin {
	h.t.Helper()
	rp := &v1alpha1.RolloutPlugin{}
	require.NoError(h.t, h.r.Get(context.Background(), h.key, rp))
	return rp
}

func (h *reconcileHarness) reconcile() (ctrl.Result, *v1alpha1.RolloutPlugin) {
	h.t.Helper()
	result, err := h.r.reconcile(context.Background(), h.get(), log.WithField("test", h.t.Name()))
	require.NoError(h.t, err)
	return result, h.get()
}

func (h *reconcileHarness) reconcileN(n int) *v1alpha1.RolloutPlugin {
	h.t.Helper()
	var rp *v1alpha1.RolloutPlugin
	for i := 0; i < n; i++ {
		_, rp = h.reconcile()
	}
	return rp
}

func (h *reconcileHarness) updateSpec(mutate func(rp *v1alpha1.RolloutPlugin)) {
	h.t.Helper()
	rp := h.get()
	mutate(rp)
	require.NoError(h.t, h.r.Update(context.Background(), rp))
}

func (h *reconcileHarness) updateStatus(mutate func(status *v1alpha1.RolloutPluginStatus)) {
	h.t.Helper()
	rp := h.get()
	mutate(&rp.Status)
	require.NoError(h.t, h.r.Status().Update(context.Background(), rp))
}

func (h *reconcileHarness) analysisRuns() []v1alpha1.AnalysisRun {
	h.t.Helper()
	list, err := h.argo.ArgoprojV1alpha1().AnalysisRuns(h.key.Namespace).List(context.Background(), metav1.ListOptions{})
	require.NoError(h.t, err)
	return list.Items
}

func (h *reconcileHarness) setAnalysisRunPhase(name string, phase v1alpha1.AnalysisPhase) {
	h.t.Helper()
	ars := h.argo.ArgoprojV1alpha1().AnalysisRuns(h.key.Namespace)
	ar, err := ars.Get(context.Background(), name, metav1.GetOptions{})
	require.NoError(h.t, err)
	ar.Status.Phase = phase
	_, err = ars.Update(context.Background(), ar, metav1.UpdateOptions{})
	require.NoError(h.t, err)
}

func stepIndex(t *testing.T, rp *v1alpha1.RolloutPlugin) int32 {
	t.Helper()
	require.NotNil(t, rp.Status.CurrentStepIndex)
	return *rp.Status.CurrentStepIndex
}

func progressingReason(t *testing.T, rp *v1alpha1.RolloutPlugin) string {
	t.Helper()
	cond := conditions.GetRolloutPluginCondition(rp.Status, v1alpha1.RolloutPluginConditionProgressing)
	require.NotNil(t, cond)
	return cond.Reason
}

func int32Ptr(i int32) *int32 { return &i }

func canaryRolloutPlugin(name string, steps ...v1alpha1.PluginCanaryStep) *v1alpha1.RolloutPlugin {
	rp := validRolloutPlugin(name)
	rp.UID = types.UID(name + "-uid")
	rp.Spec.Strategy.Canary.Steps = steps
	return rp
}

func successTemplate() *v1alpha1.AnalysisTemplate {
	return &v1alpha1.AnalysisTemplate{
		ObjectMeta: metav1.ObjectMeta{Name: "success-rate", Namespace: "default"},
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{{
				Name:     "success-rate",
				Provider: v1alpha1.MetricProvider{Web: &v1alpha1.WebMetric{URL: "http://example.com"}},
			}},
		},
	}
}

func analysisStep() v1alpha1.PluginCanaryStep {
	return v1alpha1.PluginCanaryStep{Analysis: &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{{TemplateName: "success-rate"}},
	}}
}

// TestTimedPauseAdvances walks a timed pause step: the step must requeue with the remaining
// time, report Progressing=Unknown/Paused while waiting, then advance once the duration elapses
// and continue to completion. A numeric duration (`duration: 10`) is covered because it was once
// fed to time.ParseDuration and failed the reconcile.
func TestTimedPauseAdvances(t *testing.T) {
	for name, pauseDuration := range map[string]intstr.IntOrString{
		"string duration":  intstr.FromString("10s"),
		"numeric duration": intstr.FromInt(10),
	} {
		t.Run(name, func(t *testing.T) {
			rp := canaryRolloutPlugin("timed-pause",
				v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(50)},
				v1alpha1.PluginCanaryStep{Pause: &v1alpha1.RolloutPause{Duration: &pauseDuration}},
				v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(100)},
			)
			h := newReconcileHarness(t, rp)
			h.reconcile()
			h.plugin.deploy("rev-2")

			cur := h.reconcileN(1)
			assert.Equal(t, int32(1), stepIndex(t, cur), "setWeight 50 verified, now at the pause step")

			_, cur = h.reconcile()
			require.Len(t, cur.Status.PauseConditions, 1, "pause step adds a CanaryPauseStep pause condition")
			assert.Equal(t, v1alpha1.PauseReasonCanaryPauseStep, cur.Status.PauseConditions[0].Reason)

			h.advance(4 * time.Second)
			result, cur := h.reconcile()
			assert.Equal(t, 6*time.Second, result.RequeueAfter, "requeue for the remaining pause time")
			assert.Equal(t, int32(1), stepIndex(t, cur))
			assert.Equal(t, conditions.RolloutPluginPausedReason, progressingReason(t, cur))
			assert.Equal(t, v1alpha1.RolloutPluginPhasePaused, cur.Status.Phase)

			h.advance(7 * time.Second)
			_, cur = h.reconcile()
			assert.Equal(t, int32(2), stepIndex(t, cur), "pause duration elapsed, step advanced")
			assert.Empty(t, cur.Status.PauseConditions)
			assert.False(t, cur.Status.ControllerPause)

			cur = h.reconcileN(3)
			assert.Equal(t, []int32{50, 100}, h.plugin.weightChanges)
			assert.Equal(t, v1alpha1.RolloutPluginPhaseHealthy, cur.Status.Phase)
			completed := conditions.GetRolloutPluginCondition(cur.Status, v1alpha1.RolloutPluginConditionCompleted)
			require.NotNil(t, completed)
			assert.Equal(t, corev1.ConditionTrue, completed.Status)
		})
	}
}

// TestIndefinitePausePromote verifies an indefinite pause holds its step (a paused, not yet
// converged rollout must not be mistaken for a new revision and restarted from step 0), does not
// time out, and advances once the user promotes (clears pauseConditions), with the timeout timer
// reset on resume.
func TestIndefinitePausePromote(t *testing.T) {
	rp := canaryRolloutPlugin("indefinite-pause",
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(20)},
		v1alpha1.PluginCanaryStep{Pause: &v1alpha1.RolloutPause{}},
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(60)},
	)
	rp.Spec.TimeoutSeconds = int32Ptr(30)
	h := newReconcileHarness(t, rp)
	h.reconcile()
	h.plugin.deploy("rev-2")

	cur := h.reconcileN(3)
	assert.Equal(t, int32(1), stepIndex(t, cur))
	assert.Equal(t, conditions.RolloutPluginPausedReason, progressingReason(t, cur))

	h.advance(time.Hour)
	cur = h.reconcileN(3)
	assert.Equal(t, int32(1), stepIndex(t, cur), "indefinite pause holds")
	assert.Equal(t, conditions.RolloutPluginPausedReason, progressingReason(t, cur),
		"pause time must not count toward timeoutSeconds")
	assert.Equal(t, []int32{20}, h.plugin.weightChanges)

	h.updateStatus(func(status *v1alpha1.RolloutPluginStatus) { status.PauseConditions = nil })
	_, cur = h.reconcile()
	assert.Equal(t, int32(2), stepIndex(t, cur), "promotion advances past the pause step")

	_, cur = h.reconcile()
	assert.Equal(t, int32(3), stepIndex(t, cur))
	assert.Equal(t, []int32{20, 60}, h.plugin.weightChanges)
	assert.Equal(t, conditions.RolloutPluginProgressingReason, progressingReason(t, cur),
		"resuming resets the progress deadline timer instead of timing out immediately")
}

// TestManualPauseHoldsRollout covers spec.paused mid-rollout: across reconciles
// the step index must not advance, the workload must not be mutated, and the rollout must not
// time out while paused. Unpausing continues from the held step.
func TestManualPauseHoldsRollout(t *testing.T) {
	rp := canaryRolloutPlugin("manual-pause",
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(20)},
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(50)},
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(100)},
	)
	rp.Spec.TimeoutSeconds = int32Ptr(30)
	rp.Spec.TimeoutAbort = true
	h := newReconcileHarness(t, rp)
	h.reconcile()
	h.plugin.deploy("rev-2")

	cur := h.reconcileN(1)
	require.Equal(t, int32(1), stepIndex(t, cur))

	h.updateSpec(func(rp *v1alpha1.RolloutPlugin) { rp.Spec.Paused = true })
	for i := 0; i < 3; i++ {
		h.advance(10 * time.Minute)
		_, cur = h.reconcile()
		assert.Equal(t, int32(1), stepIndex(t, cur), "step index must not advance while paused")
		assert.Equal(t, v1alpha1.RolloutPluginPhasePaused, cur.Status.Phase)
		assert.Equal(t, conditions.RolloutPluginPausedReason, progressingReason(t, cur))
	}
	assert.Equal(t, []int32{20}, h.plugin.setWeightCalls, "no workload mutation while paused")
	assert.Zero(t, h.plugin.abortCalls, "paused rollout must not time out and abort")
	assert.Zero(t, h.plugin.promoteCalls)
	assert.False(t, cur.Status.Aborted)

	h.updateSpec(func(rp *v1alpha1.RolloutPlugin) { rp.Spec.Paused = false })
	_, cur = h.reconcile()
	assert.Equal(t, int32(2), stepIndex(t, cur), "resumes from the held step")
	assert.Equal(t, []int32{20, 50}, h.plugin.setWeightCalls)
	assert.Equal(t, conditions.RolloutPluginProgressingReason, progressingReason(t, cur))

	cur = h.reconcileN(3)
	assert.Equal(t, v1alpha1.RolloutPluginPhaseHealthy, cur.Status.Phase)
	assert.Zero(t, h.plugin.abortCalls)
}

// TestAbortWhileManuallyPaused verifies spec.paused does not block a user abort: the workload is
// rolled back and the rollout ends Degraded rather than staying Paused.
func TestAbortWhileManuallyPaused(t *testing.T) {
	rp := canaryRolloutPlugin("abort-paused",
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(20)},
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(50)},
	)
	h := newReconcileHarness(t, rp)
	h.reconcile()
	h.plugin.deploy("rev-2")
	h.reconcileN(1)
	h.updateSpec(func(rp *v1alpha1.RolloutPlugin) { rp.Spec.Paused = true })
	h.reconcileN(1)

	h.updateStatus(func(status *v1alpha1.RolloutPluginStatus) { status.Abort = true })
	cur := h.reconcileN(1)
	assert.Equal(t, 1, h.plugin.abortCalls)
	assert.True(t, cur.Status.Aborted)
	assert.Equal(t, "rev-2", cur.Status.AbortedRevision)
	assert.Equal(t, v1alpha1.RolloutPluginPhaseDegraded, cur.Status.Phase)
	assert.Equal(t, []int32{20}, h.plugin.setWeightCalls)
}

// TestTimeoutAbort verifies spec.timeoutSeconds and
// spec.timeoutAbort drive timeout and abort of a stuck rollout.
func TestTimeoutAbort(t *testing.T) {
	for _, abort := range []bool{false, true} {
		t.Run(map[bool]string{false: "NoAbort", true: "Abort"}[abort], func(t *testing.T) {
			rp := canaryRolloutPlugin("deadline",
				v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(50)},
			)
			rp.Spec.TimeoutSeconds = int32Ptr(30)
			rp.Spec.TimeoutAbort = abort
			h := newReconcileHarness(t, rp)
			h.reconcile()
			h.plugin.deploy("rev-2")
			h.plugin.weightStuck = true

			cur := h.reconcileN(2)
			assert.Equal(t, int32(0), stepIndex(t, cur))
			h.advance(20 * time.Second)
			cur = h.reconcileN(1)
			assert.Equal(t, conditions.RolloutPluginProgressingReason, progressingReason(t, cur), "not yet past the deadline")

			h.advance(20 * time.Second)
			cur = h.reconcileN(2)
			if abort {
				assert.True(t, cur.Status.Aborted)
				assert.Positive(t, h.plugin.abortCalls)
				assert.Equal(t, v1alpha1.RolloutPluginPhaseDegraded, cur.Status.Phase)
			} else {
				assert.Equal(t, conditions.RolloutPluginTimedOutReason, progressingReason(t, cur))
				assert.False(t, cur.Status.Aborted)
				assert.Zero(t, h.plugin.abortCalls)
				assert.Equal(t, v1alpha1.RolloutPluginPhaseDegraded, cur.Status.Phase)
			}
		})
	}
}

// TestRestartRerunsAnalysis verifies restart after an analysis-failure abort
// clears AbortedAt and the failed AnalysisRun reference, so step 0 re-runs and the analysis
// step gets a fresh AnalysisRun instead of re-aborting on the old one.
func TestRestartRerunsAnalysis(t *testing.T) {
	rp := canaryRolloutPlugin("restart",
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(20)},
		analysisStep(),
	)
	h := newReconcileHarness(t, rp, successTemplate())
	h.reconcile()
	h.plugin.deploy("rev-2")
	cur := h.reconcileN(3)
	failedAR := cur.Status.Canary.CurrentStepAnalysisRunStatus.Name
	h.setAnalysisRunPhase(failedAR, v1alpha1.AnalysisPhaseFailed)
	cur = h.reconcileN(2)
	require.True(t, cur.Status.Aborted)

	h.updateStatus(func(status *v1alpha1.RolloutPluginStatus) { status.Restart = true })
	_, cur = h.reconcile()
	assert.Equal(t, 1, h.plugin.restartCalls)
	assert.False(t, cur.Status.Aborted)
	assert.Nil(t, cur.Status.AbortedAt)
	assert.Nil(t, cur.Status.Canary.CurrentStepAnalysisRunStatus)
	assert.Equal(t, int32(0), stepIndex(t, cur))

	cur = h.reconcileN(3)
	assert.False(t, cur.Status.Aborted, "the old failed AnalysisRun must not re-abort the restarted rollout")
	assert.Equal(t, int32(1), stepIndex(t, cur))
	require.NotNil(t, cur.Status.Canary.CurrentStepAnalysisRunStatus)
	newAR := cur.Status.Canary.CurrentStepAnalysisRunStatus.Name
	assert.NotEqual(t, failedAR, newAR)
	assert.Len(t, h.analysisRuns(), 2)

	h.setAnalysisRunPhase(newAR, v1alpha1.AnalysisPhaseSuccessful)
	cur = h.reconcileN(3)
	assert.Equal(t, v1alpha1.RolloutPluginPhaseHealthy, cur.Status.Phase)
	assert.Equal(t, []int32{20, 20}, h.plugin.weightChanges)
}

// TestPluginValidationRejectsSpec verifies a spec rejected by the plugin's Validate is marked
// InvalidSpec without touching the workload, stays stable across reconciles, and the rollout
// proceeds once the spec is fixed.
func TestPluginValidationRejectsSpec(t *testing.T) {
	rp := canaryRolloutPlugin("plugin-invalid",
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(20)},
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(100)},
	)
	rp.Spec.Plugin.Config = []byte(`{"unknownField":true}`)
	h := newReconcileHarness(t, rp)
	h.plugin.validateErr = errors.New("unknown config field unknownField")
	h.plugin.deploy("rev-2")

	_, cur := h.reconcile()
	cond := conditions.GetRolloutPluginCondition(cur.Status, v1alpha1.RolloutPluginConditionInvalidSpec)
	require.NotNil(t, cond)
	assert.Equal(t, corev1.ConditionTrue, cond.Status)
	assert.Equal(t, conditions.RolloutPluginInvalidSpecReason, cond.Reason)
	assert.Equal(t, "Plugin 'argoproj/statefulset' rejected the spec: unknown config field unknownField", cond.Message)
	assert.Contains(t, cur.Status.Message, cond.Message)

	h.advance(time.Minute)
	_, cur = h.reconcile()
	again := conditions.GetRolloutPluginCondition(cur.Status, v1alpha1.RolloutPluginConditionInvalidSpec)
	require.NotNil(t, again)
	assert.True(t, cond.LastTransitionTime.Equal(&again.LastTransitionTime), "unchanged rejection must not reset the condition")
	assert.Empty(t, h.plugin.setWeightCalls, "no workload mutation while the plugin rejects the spec")
	assert.Zero(t, h.plugin.promoteCalls)

	h.plugin.validateErr = nil
	h.updateSpec(func(rp *v1alpha1.RolloutPlugin) { rp.Spec.Plugin.Config = nil })
	_, cur = h.reconcile()
	assert.Nil(t, conditions.GetRolloutPluginCondition(cur.Status, v1alpha1.RolloutPluginConditionInvalidSpec))
	assert.Equal(t, int32(1), stepIndex(t, cur))
	assert.Equal(t, []int32{20}, h.plugin.setWeightCalls)
}

// TestPluginViewFence verifies the controller doesn't act on a plugin view of the workload older
// than the workload event that triggered the reconcile, and proceeds once the view catches up.
// Resource versions that can't be compared, or aren't reported, never hold the rollout back.
func TestPluginViewFence(t *testing.T) {
	for name, tt := range map[string]struct {
		observed func(workloadRV int64) string
		behind   bool
	}{
		"older":          {observed: func(rv int64) string { return strconv.FormatInt(rv-1, 10) }, behind: true},
		"equal":          {observed: func(rv int64) string { return strconv.FormatInt(rv, 10) }},
		"newer":          {observed: func(rv int64) string { return strconv.FormatInt(rv+1, 10) }},
		"not reported":   {observed: func(int64) string { return "" }},
		"not comparable": {observed: func(int64) string { return "not-a-number" }},
	} {
		t.Run(name, func(t *testing.T) {
			h := newReconcileHarness(t, canaryRolloutPlugin("fence", v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(50)}))
			h.reconcile()
			h.plugin.deploy("rev-2")

			workload := testWorkload()
			require.NoError(t, h.r.Get(context.Background(), client.ObjectKeyFromObject(workload), workload))
			workloadRV, err := strconv.ParseInt(workload.ResourceVersion, 10, 64)
			require.NoError(t, err)
			h.plugin.observedRV = tt.observed(workloadRV)

			before := h.get()
			result, cur := h.reconcile()
			if !tt.behind {
				assert.Equal(t, int32(1), stepIndex(t, cur))
				assert.Equal(t, []int32{50}, h.plugin.setWeightCalls)
				return
			}
			assert.Equal(t, controllerutil.StaleCacheRequeueDelay, result.RequeueAfter)
			assert.Equal(t, before.ResourceVersion, cur.ResourceVersion, "no status write from a stale plugin view")
			assert.Empty(t, h.plugin.setWeightCalls, "no workload mutation from a stale plugin view")

			h.plugin.observedRV = workload.ResourceVersion
			_, cur = h.reconcile()
			assert.Equal(t, int32(1), stepIndex(t, cur), "proceeds once the plugin's view catches up")
			assert.Equal(t, []int32{50}, h.plugin.setWeightCalls)
		})
	}
}

// TestWorkloadRef verifies a missing workload is a reconciliation error and a workloadRef of
// another kind than the plugin manages is rejected, both before the plugin is asked about it.
func TestWorkloadRef(t *testing.T) {
	t.Run("missing workload", func(t *testing.T) {
		rp := canaryRolloutPlugin("missing", v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(50)})
		rp.Spec.WorkloadRef.Name = "missing"
		h := newReconcileHarness(t, rp)
		result, cur := h.reconcile()
		assert.Equal(t, time.Second, result.RequeueAfter)
		assert.Equal(t, conditions.RolloutPluginReconciliationErrorReason, progressingReason(t, cur))
		assert.Contains(t, cur.Status.Message, `Failed to get workload: statefulsets.apps "missing" not found`)
		assert.Zero(t, h.plugin.statusCalls, "the plugin must not be called without a workload")
	})

	t.Run("workloadRef kind differs from the plugin's", func(t *testing.T) {
		rp := canaryRolloutPlugin("wrong-kind", v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(50)})
		rp.Spec.WorkloadRef.Kind = "Deployment"
		h := newReconcileHarness(t, rp)
		_, cur := h.reconcile()
		cond := conditions.GetRolloutPluginCondition(cur.Status, v1alpha1.RolloutPluginConditionInvalidSpec)
		require.NotNil(t, cond)
		assert.Equal(t, "Plugin 'argoproj/statefulset' rejected the spec: spec.workloadRef must reference a StatefulSet.apps, the kind the plugin manages, got apps/v1 Deployment", cond.Message)
		assert.Zero(t, h.plugin.statusCalls)
	})
}

func backgroundAnalysis() *v1alpha1.RolloutAnalysisBackground {
	return &v1alpha1.RolloutAnalysisBackground{RolloutAnalysis: v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{{TemplateName: "success-rate"}},
	}}
}

// reconcileRequest runs the full Reconcile entry point (finalizer and deletion handling included).
func (h *reconcileHarness) reconcileRequest() (ctrl.Result, error) {
	h.t.Helper()
	return h.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: h.key})
}

func (h *reconcileHarness) analysisRun(name string) *v1alpha1.AnalysisRun {
	h.t.Helper()
	ar, err := h.argo.ArgoprojV1alpha1().AnalysisRuns(h.key.Namespace).Get(context.Background(), name, metav1.GetOptions{})
	require.NoError(h.t, err)
	return ar
}

func countEvents(events []string, reason string) int {
	n := 0
	for _, e := range events {
		if e == reason {
			n++
		}
	}
	return n
}

// hasReconcileMetric reports whether rolloutplugin_reconcile has a series for the named RolloutPlugin.
func hasReconcileMetric(t *testing.T, name string) bool {
	t.Helper()
	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(metrics.MetricRolloutPluginReconcile))
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		for _, m := range family.GetMetric() {
			for _, label := range m.GetLabel() {
				if label.GetName() == "name" && label.GetValue() == name {
					return true
				}
			}
		}
	}
	return false
}

// TestDeletionCleanupFinalizer verifies the deletion-cleanup finalizer: it is added once, a
// deleted RolloutPlugin runs the plugin's DeletionCleanup before only that finalizer is removed,
// a failed cleanup keeps the finalizer and retries, a plugin that isn't enabled is skipped, and
// the RolloutPlugin's metrics are removed once it is gone.
func TestDeletionCleanupFinalizer(t *testing.T) {
	const foreignFinalizer = "example.com/keep"
	ctx := context.Background()

	setup := func(t *testing.T, name string, finalizers ...string) (*reconcileHarness, *record.FakeEventRecorder) {
		rp := canaryRolloutPlugin(name, v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(50)})
		rp.Finalizers = finalizers
		h := newReconcileHarness(t, rp)
		recorder := record.NewFakeEventRecorder()
		h.r.Recorder = recorder
		return h, recorder
	}
	deleteRP := func(h *reconcileHarness) {
		require.NoError(h.t, h.r.Delete(ctx, h.get()))
	}
	isGone := func(h *reconcileHarness) bool {
		err := h.r.Get(ctx, h.key, &v1alpha1.RolloutPlugin{})
		if err == nil {
			return false
		}
		require.True(h.t, apierrors.IsNotFound(err), err)
		return true
	}

	t.Run("finalizer added once, other finalizers kept", func(t *testing.T) {
		h, _ := setup(t, "add", foreignFinalizer)
		for i := 0; i < 2; i++ {
			_, err := h.reconcileRequest()
			require.NoError(t, err)
		}
		assert.Equal(t, []string{foreignFinalizer, DeletionCleanupFinalizer}, h.get().Finalizers)
	})

	t.Run("cleanup runs, then only its finalizer is removed", func(t *testing.T) {
		h, recorder := setup(t, "cleanup", foreignFinalizer)
		_, err := h.reconcileRequest()
		require.NoError(t, err)
		deleteRP(h)

		_, err = h.reconcileRequest()
		require.NoError(t, err)
		assert.Equal(t, 1, h.plugin.cleanupCalls)
		cur := h.get()
		assert.NotNil(t, cur.DeletionTimestamp)
		assert.Equal(t, []string{foreignFinalizer}, cur.Finalizers, "another controller's finalizer must be kept")
		assert.Contains(t, recorder.Events(), "RolloutPluginDeleted")

		_, err = h.reconcileRequest()
		require.NoError(t, err)
		assert.Equal(t, 1, h.plugin.cleanupCalls, "cleanup runs once")
	})

	t.Run("deleted once cleaned up, then its metrics are removed", func(t *testing.T) {
		defaults.SetMetricCleanupDelaySeconds(0)
		t.Cleanup(func() { defaults.SetMetricCleanupDelaySeconds(defaults.DefaultMetricCleanupDelay) })
		h, _ := setup(t, "metrics-removed")
		h.r.MetricsServer = metrics.NewMetricsServer(metrics.ServerConfig{K8SRequestProvider: &metrics.K8sRequestsCountProvider{}})
		_, err := h.reconcileRequest()
		require.NoError(t, err)
		require.True(t, hasReconcileMetric(t, "metrics-removed"))
		deleteRP(h)

		_, err = h.reconcileRequest()
		require.NoError(t, err)
		assert.Equal(t, 1, h.plugin.cleanupCalls)
		assert.True(t, isGone(h))

		_, err = h.reconcileRequest()
		require.NoError(t, err)
		assert.Eventually(t, func() bool { return !hasReconcileMetric(t, "metrics-removed") }, 5*time.Second, 10*time.Millisecond)
	})

	t.Run("failed cleanup keeps the finalizer and is retried", func(t *testing.T) {
		h, recorder := setup(t, "cleanup-fails")
		_, err := h.reconcileRequest()
		require.NoError(t, err)
		h.plugin.deletionCleanupErr = errors.New("conflict patching the workload")
		deleteRP(h)

		_, err = h.reconcileRequest()
		require.ErrorContains(t, err, "conflict patching the workload")
		assert.Equal(t, []string{DeletionCleanupFinalizer}, h.get().Finalizers)
		assert.Contains(t, recorder.Events(), "DeletionCleanupFailed")

		h.plugin.deletionCleanupErr = nil
		_, err = h.reconcileRequest()
		require.NoError(t, err)
		assert.Equal(t, 2, h.plugin.cleanupCalls)
		assert.True(t, isGone(h))
	})

	t.Run("plugin not enabled skips cleanup", func(t *testing.T) {
		h, _ := setup(t, "not-enabled")
		_, err := h.reconcileRequest()
		require.NoError(t, err)
		h.r.PluginManager.(*stubPluginManager).err = fmt.Errorf("plugin %q is %w", "argoproj/statefulset", ErrPluginNotEnabled)
		deleteRP(h)

		_, err = h.reconcileRequest()
		require.NoError(t, err)
		assert.Zero(t, h.plugin.cleanupCalls)
		assert.True(t, isGone(h))
	})

	t.Run("plugin unavailable keeps the finalizer", func(t *testing.T) {
		h, recorder := setup(t, "unavailable")
		_, err := h.reconcileRequest()
		require.NoError(t, err)
		h.r.PluginManager.(*stubPluginManager).err = errors.New("plugin process exited")
		deleteRP(h)

		_, err = h.reconcileRequest()
		require.ErrorContains(t, err, "plugin process exited")
		assert.Zero(t, h.plugin.cleanupCalls)
		assert.Equal(t, []string{DeletionCleanupFinalizer}, h.get().Finalizers)
		assert.Contains(t, recorder.Events(), "DeletionCleanupFailed")
	})

	t.Run("deleted before the finalizer was added", func(t *testing.T) {
		h, _ := setup(t, "never-reconciled", foreignFinalizer)
		deleteRP(h)

		_, err := h.reconcileRequest()
		require.NoError(t, err)
		assert.Zero(t, h.plugin.cleanupCalls)
		assert.Equal(t, []string{foreignFinalizer}, h.get().Finalizers, "a deleted RolloutPlugin must not get the finalizer")
	})
}

// TestPromoteFullCancelsInconclusiveAnalysis verifies promoteFull on a rollout paused by an
// Inconclusive background AnalysisRun: the current runs are cancelled and their references dropped
// so they can't pause the rollout again, and promoteFull stays set until the pods converge, like
// Rollouts.
func TestPromoteFullCancelsInconclusiveAnalysis(t *testing.T) {
	rp := canaryRolloutPlugin("promote-inconclusive",
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(20)},
		analysisStep(),
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(50)},
	)
	rp.Spec.Strategy.Canary.Analysis = backgroundAnalysis()
	h := newReconcileHarness(t, rp, successTemplate())
	h.reconcile()
	h.plugin.deploy("rev-2")

	cur := h.reconcileN(2)
	require.Equal(t, int32(1), stepIndex(t, cur))
	require.NotNil(t, cur.Status.Canary.CurrentBackgroundAnalysisRunStatus)
	require.NotNil(t, cur.Status.Canary.CurrentStepAnalysisRunStatus)
	backgroundAR := cur.Status.Canary.CurrentBackgroundAnalysisRunStatus.Name
	stepAR := cur.Status.Canary.CurrentStepAnalysisRunStatus.Name
	h.setAnalysisRunPhase(backgroundAR, v1alpha1.AnalysisPhaseInconclusive)
	_, cur = h.reconcile()
	require.Len(t, cur.Status.PauseConditions, 1)
	require.Equal(t, v1alpha1.PauseReasonInconclusiveAnalysis, cur.Status.PauseConditions[0].Reason)

	// The pods take a while to converge after the full promotion.
	h.plugin.unready = 5
	h.updateStatus(func(status *v1alpha1.RolloutPluginStatus) { status.PromoteFull = true })
	for i := 0; i < 3; i++ {
		_, cur = h.reconcile()
		assert.Empty(t, cur.Status.PauseConditions, "the Inconclusive AnalysisRun must not pause the promotion again")
		assert.True(t, cur.Status.PromoteFull, "promoteFull stays set until the pods converge")
		assert.Nil(t, cur.Status.Canary.CurrentBackgroundAnalysisRunStatus)
		assert.Nil(t, cur.Status.Canary.CurrentStepAnalysisRunStatus)
		assert.Equal(t, "Waiting for all pods to be updated", cur.Status.Message)
	}
	assert.True(t, h.analysisRun(stepAR).Spec.Terminate, "the running step AnalysisRun is cancelled")
	assert.Len(t, h.analysisRuns(), 2, "no new AnalysisRun while promoting")
	assert.Equal(t, []int32{20}, h.plugin.weightChanges, "the remaining steps are skipped")

	h.plugin.unready = 0
	_, cur = h.reconcile()
	assert.False(t, cur.Status.PromoteFull, "cleared once the pods converged")
	cur = h.reconcileN(1)
	assert.Equal(t, v1alpha1.RolloutPluginPhaseHealthy, cur.Status.Phase)
	completed := conditions.GetRolloutPluginCondition(cur.Status, v1alpha1.RolloutPluginConditionCompleted)
	require.NotNil(t, completed)
	assert.Equal(t, corev1.ConditionTrue, completed.Status)
}

// TestAnalysisRunCreateErrorResumesAtStep verifies a failure creating an AnalysisRun is reported
// as a ReconciliationError (one event and a stable condition while it repeats) and retried, and
// the rollout then resumes at the same step instead of restarting from step 0.
func TestAnalysisRunCreateErrorResumesAtStep(t *testing.T) {
	rp := canaryRolloutPlugin("ar-create-error",
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(20)},
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(40)},
		analysisStep(),
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(60)},
	)
	h := newReconcileHarness(t, rp, successTemplate())
	recorder := record.NewFakeEventRecorder()
	h.r.Recorder = recorder
	deny := false
	h.argo.PrependReactor("create", "analysisruns", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if deny {
			return true, nil, errors.New(`admission webhook "deny-ar" denied the request`)
		}
		return false, nil, nil
	})
	h.reconcile()
	h.plugin.deploy("rev-2")
	cur := h.reconcileN(2)
	require.Equal(t, int32(2), stepIndex(t, cur))

	deny = true
	result, cur := h.reconcile()
	assert.Equal(t, time.Second, result.RequeueAfter)
	assert.Equal(t, int32(2), stepIndex(t, cur))
	cond := conditions.GetRolloutPluginCondition(cur.Status, v1alpha1.RolloutPluginConditionProgressing)
	require.NotNil(t, cond)
	assert.Equal(t, corev1.ConditionFalse, cond.Status)
	assert.Equal(t, conditions.RolloutPluginReconciliationErrorReason, cond.Reason)
	assert.Contains(t, cond.Message, "Failed to reconcile AnalysisRuns: ")
	assert.Contains(t, cond.Message, `admission webhook "deny-ar" denied the request`)
	assert.Equal(t, v1alpha1.RolloutPluginPhaseDegraded, cur.Status.Phase)

	h.advance(time.Minute)
	_, cur = h.reconcile()
	again := conditions.GetRolloutPluginCondition(cur.Status, v1alpha1.RolloutPluginConditionProgressing)
	require.NotNil(t, again)
	assert.Equal(t, conditions.RolloutPluginReconciliationErrorReason, again.Reason)
	assert.True(t, cond.LastTransitionTime.Equal(&again.LastTransitionTime), "a repeated error must not reset the condition")
	assert.Equal(t, 1, countEvents(recorder.Events(), conditions.RolloutPluginReconciliationErrorReason), "one event while the same error repeats")

	deny = false
	_, cur = h.reconcile()
	assert.Equal(t, int32(2), stepIndex(t, cur), "resumes at the analysis step")
	assert.Equal(t, conditions.RolloutPluginProgressingReason, progressingReason(t, cur))
	require.NotNil(t, cur.Status.Canary.CurrentStepAnalysisRunStatus)
	assert.Equal(t, []int32{20, 40}, h.plugin.weightChanges, "the rollout must not restart from step 0")

	h.setAnalysisRunPhase(cur.Status.Canary.CurrentStepAnalysisRunStatus.Name, v1alpha1.AnalysisPhaseSuccessful)
	h.reconcileN(2)
	assert.Equal(t, []int32{20, 40, 60}, h.plugin.weightChanges)
}

// TestTimeoutCancelsAnalysisRuns verifies a rollout that times out without timeoutAbort cancels
// its running AnalysisRuns, keeping their references, and creates no new ones.
func TestTimeoutCancelsAnalysisRuns(t *testing.T) {
	rp := canaryRolloutPlugin("timeout-analysis", v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(50)})
	rp.Spec.Strategy.Canary.Analysis = backgroundAnalysis()
	rp.Spec.TimeoutSeconds = int32Ptr(30)
	h := newReconcileHarness(t, rp, successTemplate())
	h.reconcile()
	h.plugin.deploy("rev-2")
	h.plugin.weightStuck = true

	cur := h.reconcileN(2)
	require.NotNil(t, cur.Status.Canary.CurrentBackgroundAnalysisRunStatus)
	backgroundAR := cur.Status.Canary.CurrentBackgroundAnalysisRunStatus.Name
	require.False(t, h.analysisRun(backgroundAR).Spec.Terminate)

	h.advance(40 * time.Second)
	cur = h.reconcileN(2)
	assert.Equal(t, conditions.RolloutPluginTimedOutReason, progressingReason(t, cur))
	assert.True(t, h.analysisRun(backgroundAR).Spec.Terminate, "a timed-out rollout cancels its AnalysisRuns")
	require.NotNil(t, cur.Status.Canary.CurrentBackgroundAnalysisRunStatus)
	assert.Equal(t, backgroundAR, cur.Status.Canary.CurrentBackgroundAnalysisRunStatus.Name, "the reference is kept")
	assert.Len(t, h.analysisRuns(), 1)
	assert.False(t, cur.Status.Aborted)
	assert.Zero(t, h.plugin.abortCalls)
}

// TestPodProgressRefreshesTimeout verifies pods becoming Ready count as progress for
// spec.timeoutSeconds even while the step doesn't advance, and the rollout times out once they
// stop making progress.
func TestPodProgressRefreshesTimeout(t *testing.T) {
	rp := canaryRolloutPlugin("pod-progress", v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(50)})
	rp.Spec.TimeoutSeconds = int32Ptr(30)
	h := newReconcileHarness(t, rp)
	h.reconcile()
	h.plugin.deploy("rev-2")
	h.plugin.weightStuck = true
	h.plugin.unready = 5
	h.reconcileN(2)

	h.advance(20 * time.Second)
	h.plugin.unready = 3
	cur := h.reconcileN(1)
	assert.Equal(t, conditions.RolloutPluginProgressingReason, progressingReason(t, cur))
	cond := conditions.GetRolloutPluginCondition(cur.Status, v1alpha1.RolloutPluginConditionProgressing)
	assert.True(t, cond.LastUpdateTime.Time.Equal(h.now), "more Ready pods reset the timer")

	h.advance(20 * time.Second)
	cur = h.reconcileN(1)
	assert.Equal(t, conditions.RolloutPluginProgressingReason, progressingReason(t, cur),
		"40s since the step started, but only 20s since the last progress")

	h.advance(11 * time.Second)
	cur = h.reconcileN(1)
	assert.Equal(t, conditions.RolloutPluginTimedOutReason, progressingReason(t, cur))
	assert.Equal(t, int32(0), stepIndex(t, cur))
}

// TestTransientPluginErrorRetries verifies an enabled plugin that can't be reached is a retried
// ReconciliationError rather than an invalid spec, and the rollout resumes at the same step.
func TestTransientPluginErrorRetries(t *testing.T) {
	rp := canaryRolloutPlugin("plugin-unavailable",
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(20)},
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(60)},
	)
	h := newReconcileHarness(t, rp)
	h.reconcile()
	h.plugin.deploy("rev-2")
	cur := h.reconcileN(1)
	require.Equal(t, int32(1), stepIndex(t, cur))

	pluginManager := h.r.PluginManager.(*stubPluginManager)
	pluginManager.err = errors.New("plugin process exited")
	result, cur := h.reconcile()
	assert.Equal(t, time.Second, result.RequeueAfter)
	cond := conditions.GetRolloutPluginCondition(cur.Status, v1alpha1.RolloutPluginConditionProgressing)
	require.NotNil(t, cond)
	assert.Equal(t, conditions.RolloutPluginReconciliationErrorReason, cond.Reason)
	assert.Equal(t, "Failed to get plugin: plugin process exited", cond.Message)
	assert.Nil(t, conditions.GetRolloutPluginCondition(cur.Status, v1alpha1.RolloutPluginConditionInvalidSpec))
	assert.Equal(t, v1alpha1.RolloutPluginPhaseDegraded, cur.Status.Phase)

	pluginManager.err = nil
	_, cur = h.reconcile()
	assert.Equal(t, int32(2), stepIndex(t, cur), "resumes at the same step")
	assert.Equal(t, []int32{20, 60}, h.plugin.setWeightCalls, "the rollout must not restart from step 0")
	assert.Equal(t, conditions.RolloutPluginProgressingReason, progressingReason(t, cur))
}

// TestPromoteFullClearedOnNewRevision verifies a promoteFull still waiting for the pods to
// converge doesn't carry over to a new revision, which runs its steps from step 0.
func TestPromoteFullClearedOnNewRevision(t *testing.T) {
	rp := canaryRolloutPlugin("promote-new-revision",
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(20)},
		v1alpha1.PluginCanaryStep{Pause: &v1alpha1.RolloutPause{}},
		v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(60)},
	)
	h := newReconcileHarness(t, rp)
	h.reconcile()
	h.plugin.deploy("rev-2")
	cur := h.reconcileN(2)
	require.Len(t, cur.Status.PauseConditions, 1)

	h.plugin.unready = 5
	h.updateStatus(func(status *v1alpha1.RolloutPluginStatus) { status.PromoteFull = true })
	_, cur = h.reconcile()
	require.Equal(t, 1, h.plugin.promoteCalls)
	require.True(t, cur.Status.PromoteFull)

	h.plugin.deploy("rev-3")
	_, cur = h.reconcile()
	assert.False(t, cur.Status.PromoteFull)
	assert.Equal(t, int32(1), stepIndex(t, cur), "the new revision starts from step 0")
	_, cur = h.reconcile()
	require.Len(t, cur.Status.PauseConditions, 1, "the new revision pauses at the pause step")
	assert.Equal(t, v1alpha1.PauseReasonCanaryPauseStep, cur.Status.PauseConditions[0].Reason)
	assert.Equal(t, 1, h.plugin.promoteCalls, "the new revision must not be fully promoted")
	assert.Equal(t, []int32{20, 20}, h.plugin.weightChanges)
}

// TestAbortIgnoredWhenUpdatedPodsNotReady verifies a user abort is ignored when every pod already
// runs the updated revision, even if they aren't Ready (e.g. crash-looping), rather than waiting
// forever for a rollback that has nothing to roll back.
func TestAbortIgnoredWhenUpdatedPodsNotReady(t *testing.T) {
	rp := canaryRolloutPlugin("abort-not-ready", v1alpha1.PluginCanaryStep{SetWeight: int32Ptr(100)})
	h := newReconcileHarness(t, rp)
	recorder := record.NewFakeEventRecorder()
	h.r.Recorder = recorder
	h.reconcile()
	h.plugin.deploy("rev-2")
	h.reconcileN(1)

	h.plugin.stableRevision = "rev-2"
	h.plugin.unready = h.plugin.replicas
	_, cur := h.reconcile()
	require.Equal(t, "Waiting for all pods to be updated", cur.Status.Message)

	h.updateStatus(func(status *v1alpha1.RolloutPluginStatus) { status.Abort = true })
	_, cur = h.reconcile()
	assert.False(t, cur.Status.Abort)
	assert.False(t, cur.Status.Aborted)
	assert.Zero(t, h.plugin.abortCalls)
	assert.Equal(t, "Abort ignored: nothing to roll back: all pods already run revision rev-2", cur.Status.Message)
	assert.Contains(t, recorder.Events(), conditions.RolloutPluginAbortIgnoredReason)
}

// TestShouldReconcileOnDeletion verifies the update predicate reconciles when deletion starts, so
// the deletion cleanup runs, but not on metadata-only changes such as adding a finalizer.
func TestShouldReconcileOnDeletion(t *testing.T) {
	oldRP := &v1alpha1.RolloutPlugin{ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1", Generation: 1}}

	deleting := oldRP.DeepCopy()
	deleting.ResourceVersion = "2"
	now := metav1.Now()
	deleting.DeletionTimestamp = &now
	assert.True(t, shouldReconcileRolloutPluginUpdate(oldRP, deleting))

	finalizerAdded := oldRP.DeepCopy()
	finalizerAdded.ResourceVersion = "2"
	finalizerAdded.Finalizers = []string{DeletionCleanupFinalizer}
	assert.False(t, shouldReconcileRolloutPluginUpdate(oldRP, finalizerAdded))
}
