package rollout

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	patchtypes "k8s.io/apimachinery/pkg/types"
	core "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

func TestBlueGreenPostPromotionAnalysisRetry(t *testing.T) {
	tests := []struct {
		name            string
		phase           v1alpha1.AnalysisPhase
		ready           bool
		manualPromotion bool
		scalingEvent    bool
	}{
		{name: "FailedUnavailable", phase: v1alpha1.AnalysisPhaseFailed, manualPromotion: true},
		{name: "ErrorUnavailable", phase: v1alpha1.AnalysisPhaseError},
		{name: "FailedReady", phase: v1alpha1.AnalysisPhaseFailed, ready: true},
		{name: "ErrorReady", phase: v1alpha1.AnalysisPhaseError, ready: true, manualPromotion: true},
		{name: "ManualAbortRunning", phase: v1alpha1.AnalysisPhaseRunning, ready: true},
		{name: "RetryDuringScaling", phase: v1alpha1.AnalysisPhaseFailed, scalingEvent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			phase := tt.phase
			f := newFixture(t)
			defer f.Close()
			ctx := context.Background()
			at := analysisTemplate("post-analysis")
			r1 := newBlueGreenRollout("retry-post-analysis", 1, nil, "active", "preview")
			r2 := bumpVersion(r1)
			r2.Spec.Strategy.BlueGreen.AutoPromotionEnabled = ptr.To(!tt.manualPromotion)
			r2.Spec.Strategy.BlueGreen.AbortScaleDownDelaySeconds = ptr.To[int32](1)
			r2.Spec.Strategy.BlueGreen.ScaleDownDelaySeconds = ptr.To[int32](600)
			r2.Spec.Strategy.BlueGreen.PostPromotionAnalysis = &v1alpha1.RolloutAnalysis{
				Templates: []v1alpha1.AnalysisTemplateRef{{TemplateName: at.Name}},
			}
			rs1 := newReplicaSetWithStatus(r1, 1, 1)
			rs2 := newReplicaSetWithStatus(r2, 0, 0)
			if tt.ready {
				rs2 = newReplicaSetWithStatus(r2, 1, 1)
			}
			if tt.scalingEvent {
				rs1.Annotations[annotations.DesiredReplicasAnnotation] = "2"
			}
			stableHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			candidateHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			r2 = updateBlueGreenRolloutStatus(r2, candidateHash, stableHash, stableHash, 1, 0, 1, 1, false, true, false)
			r2.Status.Abort = true
			now := timeutil.MetaNow()
			r2.Status.AbortedAt = &now
			oldAR := analysisRun(at, v1alpha1.RolloutTypePostPromotionLabel, r2)
			oldAR.Status.Phase = phase
			r2.Status.BlueGreen.PostPromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{Name: oldAR.Name, Status: phase}
			active := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: stableHash}, r2)
			preview := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: candidateHash}, r2)
			f.objects = append(f.objects, r2, at, oldAR)
			f.kubeobjects = append(f.kubeobjects, rs1, rs2, active, preview)
			f.rolloutLister = append(f.rolloutLister, r2)
			f.analysisTemplateLister = append(f.analysisTemplateLister, at)
			f.analysisRunLister = append(f.analysisRunLister, oldAR)
			f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
			f.serviceLister = append(f.serviceLister, active, preview)
			c, i, k8sI := f.newController(noResyncPeriodFunc)
			ros := f.client.ArgoprojV1alpha1().Rollouts(r2.Namespace)
			ars := f.client.ArgoprojV1alpha1().AnalysisRuns(r2.Namespace)
			rss := f.kubeclient.AppsV1().ReplicaSets(r2.Namespace)
			svcs := f.kubeclient.CoreV1().Services(r2.Namespace)
			checkActiveTraffic := func(core.Action) (bool, runtime.Object, error) {
				activeSvc, err := svcs.Get(ctx, active.Name, metav1.GetOptions{})
				assert.NoError(t, err)
				assert.Equal(t, candidateHash, activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey], "post analysis must only start after active traffic switches to the candidate")
				return false, nil, nil
			}
			f.client.PrependReactor("create", "analysisruns", checkActiveTraffic)
			key := getKey(r2, t)

			// Feed persisted API state into the caches without asynchronous informer watches.
			// Each pass must rediscover the current analysis from the persisted rollout status.
			reconcile := func() *v1alpha1.Rollout {
				ro, err := ros.Get(ctx, r2.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.NoError(t, c.rolloutsIndexer.Update(ro))
				c.rolloutVersionTracker.Forget(key)
				rsList, err := rss.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for n := range rsList.Items {
					require.NoError(t, k8sI.Apps().V1().ReplicaSets().Informer().GetIndexer().Update(&rsList.Items[n]))
				}
				svcList, err := svcs.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for n := range svcList.Items {
					require.NoError(t, k8sI.Core().V1().Services().Informer().GetIndexer().Update(&svcList.Items[n]))
				}
				arList, err := ars.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for n := range arList.Items {
					require.NoError(t, i.Argoproj().V1alpha1().AnalysisRuns().Informer().GetIndexer().Update(&arList.Items[n]))
				}
				require.NoError(t, c.syncHandler(ctx, key))
				ro, err = ros.Get(ctx, r2.Name, metav1.GetOptions{})
				require.NoError(t, err)
				return ro
			}

			if phase == v1alpha1.AnalysisPhaseRunning {
				// A manual abort must terminate the in-flight analysis before retrying.
				reconcile()
				terminated, err := ars.Get(ctx, oldAR.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.True(t, terminated.Spec.Terminate)
			}
			// This is the same status-only patch used by the retry CLI.
			_, err := ros.Patch(ctx, r2.Name, patchtypes.MergePatchType, []byte(`{"status":{"abort":false}}`), metav1.PatchOptions{}, "status")
			require.NoError(t, err)
			if !tt.ready {
				for n := 0; n < 3; n++ {
					ro := reconcile()
					assert.False(t, ro.Status.Abort, "old %s analysis must not re-abort retry while candidate is unavailable (pass %d)", phase, n)
					assert.Equal(t, stableHash, ro.Status.BlueGreen.ActiveSelector)
					arList, err := ars.List(ctx, metav1.ListOptions{})
					require.NoError(t, err)
					assert.Len(t, arList.Items, 1, "post analysis must wait for active traffic")
					if n == 0 {
						// Restart from persisted objects while the candidate remains unavailable.
						// No in-memory attempt state may be needed while readiness is delayed.
						f.objects = []runtime.Object{ro, at}
						for j := range arList.Items {
							f.objects = append(f.objects, &arList.Items[j])
						}
						rsList, err := rss.List(ctx, metav1.ListOptions{})
						require.NoError(t, err)
						svcList, err := svcs.List(ctx, metav1.ListOptions{})
						require.NoError(t, err)
						f.kubeobjects = nil
						for j := range rsList.Items {
							f.kubeobjects = append(f.kubeobjects, &rsList.Items[j])
						}
						for j := range svcList.Items {
							f.kubeobjects = append(f.kubeobjects, &svcList.Items[j])
						}
						c, i, k8sI = f.newController(noResyncPeriodFunc)
						f.client.PrependReactor("create", "analysisruns", checkActiveTraffic)
						ros = f.client.ArgoprojV1alpha1().Rollouts(r2.Namespace)
						ars = f.client.ArgoprojV1alpha1().AnalysisRuns(r2.Namespace)
						rss = f.kubeclient.AppsV1().ReplicaSets(r2.Namespace)
						svcs = f.kubeclient.CoreV1().Services(r2.Namespace)
					}
				}
				candidate, err := rss.Get(ctx, rs2.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.Equal(t, int32(1), *candidate.Spec.Replicas, "retry must scale up the desired candidate")
				candidate.Status.Replicas = 1
				candidate.Status.ReadyReplicas = 1
				candidate.Status.AvailableReplicas = 1
				_, err = rss.UpdateStatus(ctx, candidate, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			ro := reconcile()
			// Manual promotion remains required when autoPromotionEnabled is false.
			assert.False(t, ro.Status.Abort)
			if tt.manualPromotion {
				assert.Equal(t, stableHash, ro.Status.BlueGreen.ActiveSelector)
				_, err := ros.Patch(ctx, r2.Name, patchtypes.MergePatchType, []byte(`{"status":{"pauseConditions":null}}`), metav1.PatchOptions{}, "status")
				require.NoError(t, err)
			}
			for n := 0; n < 3; n++ {
				ro = reconcile()
			}
			assert.False(t, ro.Status.Abort)
			assert.Equal(t, candidateHash, ro.Status.BlueGreen.ActiveSelector)
			require.NotNil(t, ro.Status.BlueGreen.PostPromotionAnalysisRunStatus)
			assert.NotEqual(t, oldAR.Name, ro.Status.BlueGreen.PostPromotionAnalysisRunStatus.Name)
			arList, err := ars.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			assert.Len(t, arList.Items, 2)
			historical, err := ars.Get(ctx, oldAR.Name, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, phase, historical.Status.Phase)
			fresh, err := ars.Get(ctx, ro.Status.BlueGreen.PostPromotionAnalysisRunStatus.Name, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, candidateHash, fresh.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
			assert.Equal(t, "2", fresh.Annotations[annotations.RevisionAnnotation])
			assert.False(t, fresh.Spec.Terminate)
			assert.False(t, fresh.Status.Phase.Completed(), "retry must evaluate a new analysis")

			// A new failure must still abort, and another retry must evaluate a third run.
			fresh.Status.Phase = v1alpha1.AnalysisPhaseFailed
			_, err = ars.UpdateStatus(ctx, fresh, metav1.UpdateOptions{})
			require.NoError(t, err)
			ro = reconcile()
			require.True(t, ro.Status.Abort)
			reconcile() // return traffic to stable and schedule candidate scale-down
			assert.Equal(t, stableHash, reconcile().Status.BlueGreen.ActiveSelector)
			later := timeutil.Now().Add(2 * time.Second)
			timeutil.SetNowTimeFunc(func() time.Time { return later })
			reconcile() // let the abort scale-down deadline expire
			candidate, err := rss.Get(ctx, rs2.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Zero(t, *candidate.Spec.Replicas)
			candidate.Status.Replicas = 0
			candidate.Status.AvailableReplicas = 0
			candidate.Status.ReadyReplicas = 0
			_, err = rss.UpdateStatus(ctx, candidate, metav1.UpdateOptions{})
			require.NoError(t, err)
			_, err = ros.Patch(ctx, r2.Name, patchtypes.MergePatchType, []byte(`{"status":{"abort":false}}`), metav1.PatchOptions{}, "status")
			require.NoError(t, err)
			for n := 0; n < 3; n++ {
				ro = reconcile()
				require.False(t, ro.Status.Abort)
				assert.Equal(t, stableHash, ro.Status.BlueGreen.ActiveSelector)
			}
			candidate, err = rss.Get(ctx, rs2.Name, metav1.GetOptions{})
			require.NoError(t, err)
			candidate.Status.Replicas = 1
			candidate.Status.AvailableReplicas = 1
			candidate.Status.ReadyReplicas = 1
			_, err = rss.UpdateStatus(ctx, candidate, metav1.UpdateOptions{})
			require.NoError(t, err)
			reconcile()
			if tt.manualPromotion {
				_, err = ros.Patch(ctx, r2.Name, patchtypes.MergePatchType, []byte(`{"status":{"pauseConditions":null}}`), metav1.PatchOptions{}, "status")
				require.NoError(t, err)
			}
			for n := 0; n < 3; n++ {
				ro = reconcile()
			}
			require.False(t, ro.Status.Abort)
			require.NotNil(t, ro.Status.BlueGreen.PostPromotionAnalysisRunStatus)
			lastName := ro.Status.BlueGreen.PostPromotionAnalysisRunStatus.Name
			assert.NotEqual(t, fresh.Name, lastName)
			assert.NotEqual(t, oldAR.Name, lastName)
			last, err := ars.Get(ctx, lastName, metav1.GetOptions{})
			require.NoError(t, err)
			last.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
			_, err = ars.UpdateStatus(ctx, last, metav1.UpdateOptions{})
			require.NoError(t, err)
			for n := 0; n < 3; n++ {
				ro = reconcile()
			}
			assert.False(t, ro.Status.Abort)
			assert.Equal(t, candidateHash, ro.Status.StableRS)
			assert.Equal(t, candidateHash, ro.Status.BlueGreen.ActiveSelector)
			assert.Equal(t, v1alpha1.RolloutPhaseHealthy, ro.Status.Phase)
			arList, err = ars.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			assert.Len(t, arList.Items, 3, "successful post analysis must not be duplicated")
		})
	}
}
