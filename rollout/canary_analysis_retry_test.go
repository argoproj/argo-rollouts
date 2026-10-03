package rollout

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	core "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

func TestCanaryAnalysisRetry(t *testing.T) {
	tests := []struct {
		name           string
		step           bool
		background     bool
		phase          v1alpha1.AnalysisPhase
		scaling        bool
		scalingFailure bool
		statusFailure  bool
		pause          bool
	}{
		{name: "StepFailed", step: true, phase: v1alpha1.AnalysisPhaseFailed},
		{name: "StepErrorDuringScaling", step: true, phase: v1alpha1.AnalysisPhaseError, scaling: true},
		{name: "BackgroundError", background: true, phase: v1alpha1.AnalysisPhaseError},
		{name: "BackgroundFailedDuringScaling", background: true, phase: v1alpha1.AnalysisPhaseFailed, scaling: true},
		{name: "StepAndBackgroundDuringScaling", step: true, background: true, phase: v1alpha1.AnalysisPhaseFailed, scaling: true},
		{name: "TerminatingStepDuringScaling", step: true, phase: v1alpha1.AnalysisPhaseRunning, scaling: true},
		{name: "TerminatingBackground", background: true, phase: v1alpha1.AnalysisPhaseRunning},
		{name: "ScalingFailure", step: true, background: true, phase: v1alpha1.AnalysisPhaseError, scaling: true, scalingFailure: true},
		{name: "StatusFailure", step: true, background: true, phase: v1alpha1.AnalysisPhaseFailed, statusFailure: true},
		{name: "PauseAndStartingStep", step: true, background: true, phase: v1alpha1.AnalysisPhaseError, scaling: true, pause: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			defer f.Close()
			ctx := context.Background()
			at := analysisTemplate("retry-metric")
			analysis := v1alpha1.RolloutAnalysis{Templates: []v1alpha1.AnalysisTemplateRef{{TemplateName: at.Name}}}
			steps := []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](50)}}
			if tt.step {
				steps = []v1alpha1.CanaryStep{{Analysis: &analysis}}
			}
			if tt.pause {
				steps = append([]v1alpha1.CanaryStep{{Pause: &v1alpha1.RolloutPause{}}}, steps...)
			}
			r1 := newCanaryRollout("retry-canary", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(0))
			r2 := bumpVersion(r1)
			if tt.background {
				r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{RolloutAnalysis: analysis}
				if tt.pause {
					r2.Spec.Strategy.Canary.Analysis.StartingStep = ptr.To[int32](1)
				}
			}
			rs1 := newReplicaSetWithStatus(r1, 1, 1)
			rs2 := newReplicaSetWithStatus(r2, 0, 0)
			if tt.scaling {
				rs1.Annotations[annotations.DesiredReplicasAnnotation] = "2"
			}
			stable := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			candidate := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			r2 = updateCanaryRolloutStatus(r2, stable, 1, 0, 1, false)
			r2.Status.Abort = true
			now := timeutil.MetaNow()
			r2.Status.AbortedAt = &now
			var oldRuns []*v1alpha1.AnalysisRun
			if tt.step {
				old := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r2)
				index := 0
				if tt.pause {
					index = 1
					old.Labels[v1alpha1.RolloutCanaryStepIndexLabel] = "1"
				}
				old.Name = fmt.Sprintf("%s-%s-2-%d", r2.Name, candidate, index)
				oldRuns = append(oldRuns, old)
				r2.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{Name: old.Name, Status: tt.phase}
			}
			if tt.background {
				old := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
				oldRuns = append(oldRuns, old)
				r2.Status.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{Name: old.Name, Status: tt.phase}
			}
			f.objects = append(f.objects, r2, at)
			for _, old := range oldRuns {
				old.Status.Phase = tt.phase
				// Termination is asynchronous: a manually aborted run may still report Running.
				old.Spec.Terminate = tt.phase == v1alpha1.AnalysisPhaseRunning
				f.objects = append(f.objects, old)
				f.analysisRunLister = append(f.analysisRunLister, old)
			}
			f.kubeobjects = append(f.kubeobjects, rs1, rs2)
			f.rolloutLister = append(f.rolloutLister, r2)
			f.analysisTemplateLister = append(f.analysisTemplateLister, at)
			f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
			c, i, k := f.newController(noResyncPeriodFunc)
			ros := f.client.ArgoprojV1alpha1().Rollouts(r2.Namespace)
			ars := f.client.ArgoprojV1alpha1().AnalysisRuns(r2.Namespace)
			rss := f.kubeclient.AppsV1().ReplicaSets(r2.Namespace)
			key := getKey(r2, t)

			// Rebuild caches from persisted API state so retries span real reconciliation passes.
			reconcile := func(expectError bool) *v1alpha1.Rollout {
				ro, err := ros.Get(ctx, r2.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.NoError(t, c.rolloutsIndexer.Update(ro))
				c.rolloutVersionTracker.Forget(key)
				sets, err := rss.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for j := range sets.Items {
					require.NoError(t, k.Apps().V1().ReplicaSets().Informer().GetIndexer().Update(&sets.Items[j]))
				}
				runs, err := ars.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for j := range runs.Items {
					require.NoError(t, i.Argoproj().V1alpha1().AnalysisRuns().Informer().GetIndexer().Update(&runs.Items[j]))
				}
				err = c.syncHandler(ctx, key)
				if expectError {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				ro, err = ros.Get(ctx, r2.Name, metav1.GetOptions{})
				require.NoError(t, err)
				return ro
			}
			failNextWrite := func(resource string) {
				fail := true
				reactor := func(action core.Action) (bool, runtime.Object, error) {
					if action.GetVerb() != "patch" && action.GetVerb() != "update" {
						return false, nil, nil
					}
					if !fail {
						return false, nil, nil
					}
					fail = false
					return true, nil, errors.New("API temporarily unavailable")
				}
				if resource == "rollouts" {
					f.client.PrependReactor("*", resource, reactor)
				} else {
					f.kubeclient.PrependReactor("*", resource, reactor)
				}
			}
			retry := func() {
				_, err := ros.Patch(ctx, r2.Name, types.MergePatchType, []byte(`{"status":{"abort":false}}`), metav1.PatchOptions{}, "status")
				require.NoError(t, err)
			}
			currentRuns := func(ro *v1alpha1.Rollout) []*v1alpha1.AnalysisRun {
				var runs []*v1alpha1.AnalysisRun
				refs := []*v1alpha1.RolloutAnalysisRunStatus{}
				if tt.step {
					refs = append(refs, ro.Status.Canary.CurrentStepAnalysisRunStatus)
				}
				if tt.background {
					refs = append(refs, ro.Status.Canary.CurrentBackgroundAnalysisRunStatus)
				}
				for _, ref := range refs {
					require.NotNil(t, ref)
					run, err := ars.Get(ctx, ref.Name, metav1.GetOptions{})
					require.NoError(t, err)
					assert.False(t, run.Spec.Terminate)
					assert.Equal(t, candidate, run.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
					assert.Equal(t, "2", run.Annotations[annotations.RevisionAnnotation])
					runs = append(runs, run)
				}
				return runs
			}

			retry()
			if tt.scalingFailure {
				failNextWrite("replicasets")
				reconcile(true)
			}
			if tt.statusFailure {
				failNextWrite("rollouts")
				reconcile(true)
			}
			ro := reconcile(false)
			require.False(t, ro.Status.Abort)

			// Reconstruct the controller after retry has begun, including any created runs.
			f.objects = []runtime.Object{ro, at}
			runs, err := ars.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			for j := range runs.Items {
				f.objects = append(f.objects, &runs.Items[j])
			}
			sets, err := rss.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			f.kubeobjects = nil
			for j := range sets.Items {
				f.kubeobjects = append(f.kubeobjects, &sets.Items[j])
			}
			c, i, k = f.newController(noResyncPeriodFunc)
			ros = f.client.ArgoprojV1alpha1().Rollouts(r2.Namespace)
			ars = f.client.ArgoprojV1alpha1().AnalysisRuns(r2.Namespace)
			rss = f.kubeclient.AppsV1().ReplicaSets(r2.Namespace)
			for j := 0; j < 3; j++ {
				ro = reconcile(false)
				require.False(t, ro.Status.Abort, "the previous analysis must not re-abort the first retry")
			}
			resume := func() {
				if tt.pause {
					require.NotEmpty(t, ro.Status.PauseConditions, "retry must preserve the manual pause")
					_, err := ros.Patch(ctx, r2.Name, types.MergePatchType, []byte(`{"status":{"pauseConditions":null}}`), metav1.PatchOptions{}, "status")
					require.NoError(t, err)
					for j := 0; j < 3; j++ {
						ro = reconcile(false)
					}
				}
			}
			if tt.pause {
				runs, err := ars.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				assert.Len(t, runs.Items, len(oldRuns), "analysis must wait for its starting step")
			}
			resume()
			fresh := currentRuns(ro)
			for j, old := range oldRuns {
				assert.NotEqual(t, old.Name, fresh[j].Name)
				historical, err := ars.Get(ctx, old.Name, metav1.GetOptions{})
				require.NoError(t, err)
				assert.Equal(t, tt.phase, historical.Status.Phase)
			}
			runs, err = ars.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			assert.Len(t, runs.Items, 2*len(oldRuns), "only one fresh attempt is created")

			fresh[0].Status.Phase = v1alpha1.AnalysisPhaseFailed
			_, err = ars.UpdateStatus(ctx, fresh[0], metav1.UpdateOptions{})
			require.NoError(t, err)
			if tt.scalingFailure {
				// An error during a fresh attempt must not discard its pending failure.
				stableRS, err := rss.Get(ctx, rs1.Name, metav1.GetOptions{})
				require.NoError(t, err)
				stableRS.Annotations[annotations.DesiredReplicasAnnotation] = "2"
				_, err = rss.Update(ctx, stableRS, metav1.UpdateOptions{})
				require.NoError(t, err)
				failNextWrite("replicasets")
				reconcile(true)
			}
			for j := 0; j < 3 && !ro.Status.Abort; j++ {
				ro = reconcile(false)
			}
			require.True(t, ro.Status.Abort, "a fresh failure must still abort")
			reconcile(false)
			retry()
			for j := 0; j < 3; j++ {
				ro = reconcile(false)
				require.False(t, ro.Status.Abort)
			}
			resume()
			last := currentRuns(ro)
			for j, run := range last {
				assert.NotEqual(t, fresh[j].Name, run.Name)
				run.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
				_, err := ars.UpdateStatus(ctx, run, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			for j := 0; j < 6; j++ {
				// Model pods becoming ready as their ReplicaSets reach the requested sizes.
				sets, err := rss.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for _, rs := range sets.Items {
					rs.Status.Replicas = *rs.Spec.Replicas
					rs.Status.ReadyReplicas = *rs.Spec.Replicas
					rs.Status.AvailableReplicas = *rs.Spec.Replicas
					_, err := rss.UpdateStatus(ctx, &rs, metav1.UpdateOptions{})
					require.NoError(t, err)
				}
				ro = reconcile(false)
			}
			assert.False(t, ro.Status.Abort)
			assert.Equal(t, candidate, ro.Status.StableRS)
			assert.Equal(t, v1alpha1.RolloutPhaseHealthy, ro.Status.Phase)
			runs, err = ars.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			assert.Len(t, runs.Items, 3*len(oldRuns), "completed analysis must not be duplicated")
		})
	}
}
