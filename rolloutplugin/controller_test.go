package rolloutplugin

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	fakeargo "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
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

// stubPluginManager returns a fixed plugin for any name and reports statefulset enabled.
type stubPluginManager struct {
	plugin ResourcePlugin
}

func (s *stubPluginManager) GetPlugin(name string) (ResourcePlugin, error) { return s.plugin, nil }
func (s *stubPluginManager) IsBuiltinEnabled(id string) bool               { return true }
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

	setWeightCalls []int32
	// weightChanges records only the SetWeight calls that changed the weight. Pause and analysis
	// steps re-apply the last setWeight on every reconcile, so this is the sequence of weights the
	// workload actually went through.
	weightChanges []int32
	promoteCalls  int
	abortCalls    int
	restartCalls  int
	statusCalls   int
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
		ReadyReplicas:     p.replicas,
		AvailableReplicas: p.replicas,
		CurrentRevision:   p.stableRevision,
		UpdatedRevision:   p.updateRevision,
		Ready:             true,

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
	p.stableRevision = p.updateRevision
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
