package rollout

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
)

func adoptionRollout() *v1alpha1.Rollout {
	r := newCanaryRollout("adopted", 1, nil, nil, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r.Spec.WorkloadRef = &v1alpha1.ObjectRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "source", ScaleDown: "never"}
	r.Annotations = map[string]string{
		annotations.LegacyAdoptionStateAnnotation:            annotations.LegacyAdoptionStatePromoted,
		annotations.LegacyAdoptionBaseSelectorAnnotation:     "app=adopted",
		annotations.LegacyAdoptionWeightAnnotationAnnotation: "example.com/weight",
		annotations.LegacyAdoptionActiveWeightAnnotation:     "active",
		annotations.LegacyAdoptionCandidateWeightAnnotation:  "preview",
		annotations.LegacyAdoptionLegacyWeightAnnotation:     "legacy",
		annotations.LegacyAdoptionInactiveWeightAnnotation:   "inactive",
	}
	return r
}

func TestAdoptionSyncRestoresLegacyBeforeRemovingFinalizer(t *testing.T) {
	for _, scenario := range []string{"deleting", "deleting-write-fails", "finalizer-write-fails", "resumed", "restore-fails", "detect-fails"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			defer f.Close()
			r := adoptionRollout()
			if scenario == "deleting" || scenario == "deleting-write-fails" || scenario == "finalizer-write-fails" {
				now := metav1.Now()
				r.DeletionTimestamp = &now
				r.Finalizers = []string{annotations.LegacyAdoptionCleanupFinalizer, "example.com/other-finalizer"}
			}
			deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: r.Namespace}}
			legacy := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "legacy", Namespace: r.Namespace, UID: "legacy-pod", Labels: map[string]string{"app": "adopted"}}}
			candidate := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "candidate", Namespace: r.Namespace, UID: "candidate-pod", Labels: map[string]string{"app": "adopted", v1alpha1.DefaultRolloutUniqueLabelKey: "green"}}}
			f.objects = append(f.objects, r)
			f.rolloutLister = append(f.rolloutLister, r)
			f.kubeobjects = append(f.kubeobjects, deployment, legacy, candidate)
			c, _, _ := f.newController(noResyncPeriodFunc)
			wantError := scenario != "deleting" && scenario != "resumed"
			if scenario == "deleting-write-fails" || scenario == "restore-fails" {
				f.kubeclient.PrependReactor("patch", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("traffic write failed")
				})
			}
			if scenario == "detect-fails" {
				f.kubeclient.PrependReactor("get", "deployments", func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("deployment read failed")
				})
			}
			if scenario == "finalizer-write-fails" {
				f.client.PrependReactor("patch", "rollouts", func(ktesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("finalizer write failed")
				})
			}
			err := c.syncHandler(context.Background(), getKey(r, t))
			if wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			live, err := f.client.ArgoprojV1alpha1().Rollouts(r.Namespace).Get(context.Background(), r.Name, metav1.GetOptions{})
			require.NoError(t, err)
			if scenario == "deleting" {
				require.Equal(t, []string{"example.com/other-finalizer"}, live.Finalizers)
			} else if r.DeletionTimestamp != nil {
				require.Contains(t, live.Finalizers, annotations.LegacyAdoptionCleanupFinalizer, "failed restore must retain cleanup finalizer")
			}
			for _, a := range f.kubeclient.Actions() {
				require.NotContains(t, []string{"delete", "create"}, a.GetVerb())
			}
			if !wantError {
				pod, err := f.kubeclient.CoreV1().Pods(r.Namespace).Get(context.Background(), "candidate", metav1.GetOptions{})
				require.NoError(t, err)
				require.Equal(t, "inactive", pod.Annotations["example.com/weight"])
				require.Equal(t, candidate.UID, pod.UID)
			}
		})
	}
}

func TestDeploymentResumeEventRestoresOnlyAdoptedWorkload(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	r := adoptionRollout()
	plain := r.DeepCopy()
	plain.Name = "plain"
	plain.Annotations = nil
	unrelated := r.DeepCopy()
	unrelated.Name = "unrelated"
	unrelated.Spec.WorkloadRef.Name = "other"
	f.objects = append(f.objects, r, plain, unrelated)
	f.rolloutLister = append(f.rolloutLister, r, plain, unrelated)
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: r.Namespace}, Spec: appsv1.DeploymentSpec{Paused: true}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "legacy", Namespace: r.Namespace, UID: "unchanged", Labels: map[string]string{"app": "adopted"}}}
	f.kubeobjects = append(f.kubeobjects, dep, pod)
	c, _, k8sI := f.newController(noResyncPeriodFunc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer c.rolloutWorkqueue.ShutDown()
	go k8sI.Apps().V1().Deployments().Informer().Run(ctx.Done())
	require.True(t, cache.WaitForCacheSync(ctx.Done(), k8sI.Apps().V1().Deployments().Informer().HasSynced))
	dep.Spec.Paused = false
	_, err := f.kubeclient.AppsV1().Deployments(r.Namespace).Update(ctx, dep, metav1.UpdateOptions{})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		live, err := f.kubeclient.CoreV1().Pods(r.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		return err == nil && live.Annotations["example.com/weight"] == "legacy"
	}, 5*time.Second, time.Millisecond)
	require.Eventually(t, func() bool { return c.rolloutWorkqueue.Len() == 1 }, 5*time.Second, time.Millisecond)
	key, shutdown := c.rolloutWorkqueue.Get()
	require.False(t, shutdown)
	require.Equal(t, getKey(r, t), key)
	c.rolloutWorkqueue.Done(key)
	c.rolloutWorkqueue.Forget(key)
}

func TestAdoptionRetirementStopsOnFailureForBothStrategies(t *testing.T) {
	for _, strategy := range []string{"canary", "blueGreen"} {
		for _, scenario := range []string{"success", "missing-deployment", "update-error"} {
			t.Run(strategy+"/"+scenario, func(t *testing.T) {
				var injected error
				if scenario == "update-error" {
					injected = errors.New("update failed")
				}
				ctx := createScaleDownRolloutContext(v1alpha1.ScaleDownNever, 3, scenario != "missing-deployment", injected)
				ctx.rollout.Annotations[annotations.LegacyAdoptionPromotedAnnotation] = "true"
				ctx.newRS = rs("next", 0, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "next"}, noTimestamp, nil)
				ctx.stableRS = rs("stable", 3, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "stable"}, noTimestamp, nil)
				var err error
				if strategy == "canary" {
					ctx.rollout.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{}
					_, err = ctx.reconcileCanaryReplicaSets()
				} else {
					err = ctx.reconcileBlueGreenReplicaSets(nil)
				}
				if scenario == "success" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
	}
}

func TestTemplateRollbackCleanupFailurePreservesBaseline(t *testing.T) {
	for _, mode := range []string{"template", "traffic"} {
		t.Run(mode, func(t *testing.T) {
			r := adoptionRollout()
			baseline := rs("baseline", 3, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "blue"}, metav1.NewTime(time.Now().Add(-time.Hour)), nil)
			failed := rs("failed", 3, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "green"}, metav1.Now(), nil)
			client := k8sfake.NewSimpleClientset(baseline, failed)
			client.PrependReactor("update", "replicasets", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("scale failed") })
			ctx := &rolloutContext{rollout: r, newRS: baseline, stableRS: baseline, otherRSs: []*appsv1.ReplicaSet{failed}, pauseContext: &pauseContext{rollout: r, log: logutil.WithRollout(r)}, reconcilerBase: reconcilerBase{kubeclientset: client, recorder: record.NewFakeEventRecorder()}}
			if mode == "traffic" {
				r.Annotations[annotations.TrafficOverrideHashAnnotation] = "blue"
				r.Annotations[annotations.TrafficOverrideSourceHashAnnotation] = "green"
				ctx.newRS = rs("next", 0, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "next"}, metav1.Now(), nil)
			}
			_, err := ctx.reconcileOtherReplicaSets()
			require.ErrorContains(t, err, "scale failed")
			for _, a := range client.Actions() {
				if update, ok := a.(ktesting.UpdateAction); ok {
					require.Equal(t, failed.Name, update.GetObject().(*appsv1.ReplicaSet).Name)
				}
			}
		})
	}
}
