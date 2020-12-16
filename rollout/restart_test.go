package rollout

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/log"
)

func rollout(selector string, restartAt metav1.Time, restartedAt *metav1.Time) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "restart-rollout",
		},
		Spec: v1alpha1.RolloutSpec{
			RestartAt: restartAt.DeepCopy(),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"test": selector,
				},
			},
		},
		Status: v1alpha1.RolloutStatus{
			RestartedAt: restartedAt.DeepCopy(),
		},
	}
}

func pod(name, selector string, time metav1.Time, rs *appsv1.ReplicaSet) *corev1.Pod {
	rsKind := appsv1.SchemeGroupVersion.WithKind("ReplicaSets")
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			CreationTimestamp: time,
			Labels: map[string]string{

				"test": selector,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(rs, rsKind),
			},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}

func replicaSet(name, selector string, replicas, available int32) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  "11111111-2222-3333-4444-555555555555",
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"test": selector,
				},
			},
			Replicas: &replicas,
		},
		Status: appsv1.ReplicaSetStatus{
			AvailableReplicas: available,
		},
	}
}

func TestRestartCheckEnqueueRollout(t *testing.T) {
	now := metav1.Now()
	t.Run(".Spec.Restart not set", func(t *testing.T) {
		roCtx := &rolloutContext{
			rollout: &v1alpha1.Rollout{},
			log:     logrus.WithField("", ""),
		}
		p := RolloutPodRestarter{
			enqueueAfter: func(obj interface{}, duration time.Duration) {
				assert.Fail(t, "Should not enqueue rollout")
			},
		}
		p.checkEnqueueRollout(roCtx)
	})
	t.Run(".Spec.Restart has already past", func(t *testing.T) {
		ro := rollout("test", metav1.NewTime(now.Add(-10*time.Minute)), nil)
		roCtx := &rolloutContext{
			rollout: ro,
			log:     logrus.WithField("", ""),
		}
		p := RolloutPodRestarter{
			resyncPeriod: 10 * time.Minute,
			enqueueAfter: func(obj interface{}, duration time.Duration) {
				assert.Fail(t, "Should not enqueue rollout")
			},
		}
		p.checkEnqueueRollout(roCtx)
	})
	t.Run("Enqueue Rollout since before next resync", func(t *testing.T) {
		ro := rollout("test", metav1.NewTime(now.Add(5*time.Minute)), nil)
		enqueued := false
		roCtx := &rolloutContext{
			rollout: ro,
			log:     logrus.WithField("", ""),
		}
		p := RolloutPodRestarter{
			resyncPeriod: 10 * time.Minute,
			enqueueAfter: func(obj interface{}, duration time.Duration) {
				enqueued = true
			},
		}
		p.checkEnqueueRollout(roCtx)
		assert.True(t, enqueued)
	})
	t.Run("Do not enqueue Rollout since after next resync", func(t *testing.T) {
		enqueued := false
		ro := rollout("test", metav1.NewTime(now.Add(5*time.Minute)), nil)
		roCtx := &rolloutContext{
			rollout: ro,
			log:     logrus.WithField("", ""),
		}
		p := RolloutPodRestarter{
			resyncPeriod: 2 * time.Minute,
			enqueueAfter: func(obj interface{}, duration time.Duration) {
				enqueued = true
			},
		}
		p.checkEnqueueRollout(roCtx)
		assert.False(t, enqueued)
	})
}

func TestRestartReconcile(t *testing.T) {
	now := metav1.Now()
	rs := replicaSet("rollout-restart-abc123", "test", 1, 1)
	olderPod := pod("older", "test", metav1.NewTime(now.Add(-10*time.Second)), rs)
	newerPod := pod("newer", "test", metav1.NewTime(now.Add(10*time.Second)), rs)

	t.Run("No Restart Needed", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		r := RolloutPodRestarter{client: client}
		noRestartRo := rollout("test", now, &now)
		roCtx := &rolloutContext{
			rollout: noRestartRo,
			log:     log.WithRollout(noRestartRo),
		}
		err := r.Reconcile(roCtx)
		assert.Nil(t, err)
		assert.Len(t, client.Actions(), 0)
	})

	t.Run("Not all ReplicaSets are fully available", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		notFullyAvailable := replicaSet("rollout-restart-abc123", "test", 1, 0)
		logrus.New()
		buf := bytes.NewBufferString("")
		logger := logrus.New()
		logger.SetOutput(buf)
		roCtx := &rolloutContext{
			rollout: rollout("test", now, nil),
			log:     logrus.NewEntry(logger),
			allRSs:  []*appsv1.ReplicaSet{notFullyAvailable},
		}
		r := RolloutPodRestarter{
			client:       client,
			resyncPeriod: 2 * time.Minute,
			enqueueAfter: func(obj interface{}, duration time.Duration) {},
		}
		err := r.Reconcile(roCtx)
		assert.Nil(t, err)
		assert.Len(t, client.Actions(), 1) // list pods
		assert.Contains(t, buf.String(), "all 0 pods are current. setting restartedAt")
	})
	t.Run("Fails to delete Pod", func(t *testing.T) {
		expectedErrMsg := "big bad error"
		client := fake.NewSimpleClientset(rs, olderPod)
		roCtx := &rolloutContext{
			rollout: rollout("test", now, nil),
			log:     logrus.WithField("", ""),
			allRSs:  []*appsv1.ReplicaSet{rs},
		}
		client.PrependReactor("create", "pods", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
			// this is the pod eviction
			return true, nil, fmt.Errorf(expectedErrMsg)
		})
		r := RolloutPodRestarter{
			client:       client,
			enqueueAfter: func(obj interface{}, duration time.Duration) {},
		}
		err := r.Reconcile(roCtx)
		assert.Errorf(t, err, expectedErrMsg)
	})
	t.Run("Deletes Pod", func(t *testing.T) {
		client := fake.NewSimpleClientset(rs, olderPod)
		roCtx := &rolloutContext{
			rollout: rollout("test", now, nil),
			log:     logrus.WithField("", ""),
			allRSs:  []*appsv1.ReplicaSet{rs},
		}
		r := RolloutPodRestarter{
			client:       client,
			enqueueAfter: func(obj interface{}, duration time.Duration) {},
		}
		err := r.Reconcile(roCtx)
		assert.Nil(t, err)
		actions := client.Actions()
		assert.Len(t, actions, 2)
		_, ok := actions[1].(k8stesting.CreateAction) // eviction
		assert.True(t, ok)
	})
	t.Run("No more pods to delete", func(t *testing.T) {
		client := fake.NewSimpleClientset(rs, newerPod)
		roCtx := &rolloutContext{
			rollout: rollout("test", now, nil),
			log:     logrus.WithField("", ""),
			allRSs:  []*appsv1.ReplicaSet{rs},
		}
		r := RolloutPodRestarter{
			client:       client,
			enqueueAfter: func(obj interface{}, duration time.Duration) {},
		}
		err := r.Reconcile(roCtx)
		assert.Nil(t, err)
		assert.Len(t, client.Actions(), 1)
		assert.Equal(t, now, *roCtx.newStatus.RestartedAt)
	})
	t.Run("restartedAt equals creationTimestamp", func(t *testing.T) {
		equalPod := pod("equal", "test", now, rs)
		client := fake.NewSimpleClientset(rs, equalPod)
		roCtx := &rolloutContext{
			rollout: rollout("test", now, nil),
			log:     logrus.WithField("", ""),
			allRSs:  []*appsv1.ReplicaSet{rs},
		}
		r := RolloutPodRestarter{
			client:       client,
			enqueueAfter: func(obj interface{}, duration time.Duration) {},
		}
		err := r.Reconcile(roCtx)
		assert.Nil(t, err)
		assert.Len(t, client.Actions(), 1)
		assert.Equal(t, now, *roCtx.newStatus.RestartedAt)
	})
}

func TestRestartReplicaSetPod(t *testing.T) {
	now := metav1.Now()
	ro := rollout("test", now, nil)
	rs := replicaSet("rollout-restart-abc123", "test", 1, 1)
	roCtx := &rolloutContext{
		rollout:  ro,
		log:      log.WithRollout(ro),
		stableRS: rs,
		allRSs:   []*appsv1.ReplicaSet{rs},
	}
	olderPod := pod("older", "test", metav1.NewTime(now.Add(-10*time.Second)), rs)
	newerPod := pod("newer", "test", metav1.NewTime(now.Add(10*time.Second)), rs)
	differentSelector := pod("older", "test2", metav1.NewTime(now.Add(-10*time.Second)), rs)
	t.Run("Finds no pods to delete to due to different label selector", func(t *testing.T) {
		client := fake.NewSimpleClientset(rs, differentSelector)
		r := RolloutPodRestarter{client: client}
		err := r.Reconcile(roCtx)
		assert.Nil(t, err)
		assert.Equal(t, now, *roCtx.newStatus.RestartedAt)

		// Client uses list API but not the delete API
		assert.Len(t, client.Actions(), 1)
	})
	t.Run("Delete Pod successfully", func(t *testing.T) {
		client := fake.NewSimpleClientset(rs, olderPod)
		r := RolloutPodRestarter{client: client}
		err := r.Reconcile(roCtx)
		assert.NotNil(t, roCtx.newStatus.RestartedAt)
		assert.Nil(t, err)
		actions := client.Actions()
		// Client uses list and evict API
		assert.Len(t, actions, 2)
		_, ok := actions[1].(k8stesting.CreateAction)
		assert.True(t, ok)
	})
	t.Run("No Pod Deletion required", func(t *testing.T) {
		client := fake.NewSimpleClientset(rs, newerPod)
		r := RolloutPodRestarter{client: client}
		err := r.Reconcile(roCtx)
		assert.Equal(t, now, *roCtx.newStatus.RestartedAt)
		assert.Nil(t, err)
		// Client uses list API but not the evict API
		assert.Len(t, client.Actions(), 1)
	})
	t.Run("Pod List error", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		expectedErrMsg := "big bad error"
		client.PrependReactor("list", "pods", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, fmt.Errorf(expectedErrMsg)
		})
		r := RolloutPodRestarter{client: client}
		roCtx := &rolloutContext{
			rollout: ro,
			log:     log.WithRollout(ro),
		}
		err := r.Reconcile(roCtx)
		assert.Nil(t, roCtx.newStatus.RestartedAt)
		assert.Error(t, err, expectedErrMsg)
	})
}

// Verifies we don't delete pods which are not related to rollout (but have same selector)
func TestRestartDoNotDeleteOtherPods(t *testing.T) {
	now := metav1.Now()
	ro := rollout("test", now, nil)
	twoUnavailable := intstr.FromInt(2)
	ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{
		MaxUnavailable: &twoUnavailable,
	}
	rolloutRS := replicaSet("rollout-restart-abc123", "test", 1, 1)
	newerPod := pod("newer", "test", metav1.NewTime(now.Add(10*time.Second)), rolloutRS)
	otherRS := replicaSet("other-rs", "test", 1, 1)
	otherRS.UID = "4444-5555-6666-7777-8888"
	otherPod := pod("other-pod", "test", metav1.NewTime(now.Add(-10*time.Second)), otherRS)
	roCtx := &rolloutContext{
		rollout:  ro,
		log:      log.WithRollout(ro),
		stableRS: rolloutRS,
		allRSs:   []*appsv1.ReplicaSet{rolloutRS},
	}
	client := fake.NewSimpleClientset(rolloutRS, otherRS, newerPod, otherPod)
	r := RolloutPodRestarter{client: client}
	err := r.Reconcile(roCtx)
	assert.Equal(t, now, *roCtx.newStatus.RestartedAt)
	assert.Nil(t, err)
	// Client uses list API but not the delete API
	assert.Len(t, client.Actions(), 1)
}

func TestRestartSortReplicaSetsByPriority(t *testing.T) {
	rs := func(name string, creationTimestamp metav1.Time) *appsv1.ReplicaSet {
		return &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				CreationTimestamp: creationTimestamp,
			},
		}
	}
	rs1 := rs("rs1", metav1.Now())
	rs2 := rs("rs2", metav1.NewTime(metav1.Now().Add(10*time.Second)))
	allRS := []*appsv1.ReplicaSet{rs1, rs2}

	t.Run("NewSortReplicaSetsByPriority()", func(t *testing.T) {
		roCtx := &rolloutContext{
			newRS:    rs1,
			stableRS: rs2,
			allRSs:   allRS,
		}
		s := NewSortReplicaSetsByPriority(roCtx)
		assert.Equal(t, s.newRS, rs1.Name)
		assert.Equal(t, s.stableRS, rs2.Name)
		assert.Len(t, s.allRSs, 2)
	})
	t.Run("Less(): honor stable RS over new RS", func(t *testing.T) {
		s := SortReplicaSetsByPriority{
			allRSs:   allRS,
			stableRS: rs1.Name,
			newRS:    rs2.Name,
		}
		assert.True(t, s.Less(0, 1))
		assert.False(t, s.Less(1, 0))
	})
	t.Run("Less(): honor stable RS over older RS", func(t *testing.T) {
		s := SortReplicaSetsByPriority{
			allRSs:   allRS,
			stableRS: rs1.Name,
		}
		assert.True(t, s.Less(0, 1))
		assert.False(t, s.Less(1, 0))
	})
	t.Run("Less(): honor new RS over older RS", func(t *testing.T) {
		s := SortReplicaSetsByPriority{
			allRSs: allRS,
			newRS:  rs1.Name,
		}
		assert.True(t, s.Less(0, 1))
		assert.False(t, s.Less(1, 0))
	})
	t.Run("Less(): prioritize older ReplicaSet ", func(t *testing.T) {
		s := SortReplicaSetsByPriority{
			allRSs: allRS,
		}
		// RS1 is ten seconds older than RS2
		assert.True(t, s.Less(0, 1))
		assert.False(t, s.Less(1, 0))
	})
	t.Run("Len()", func(t *testing.T) {
		s := SortReplicaSetsByPriority{
			allRSs: allRS,
		}
		assert.Equal(t, s.Len(), 2)
	})
	t.Run("Swap()", func(t *testing.T) {
		s := SortReplicaSetsByPriority{
			allRSs: allRS,
		}
		s.Swap(0, 1)
		assert.Equal(t, s.allRSs[1].Name, rs1.Name)
		assert.Equal(t, s.allRSs[0].Name, rs2.Name)
	})
}

func TestRestartMaxUnavailable(t *testing.T) {
	now := metav1.Now()
	ro := rollout("test", now, nil)
	ro.Spec.Replicas = pointer.Int32Ptr(3)
	twoUnavailable := intstr.FromInt(2)
	ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{
		MaxUnavailable: &twoUnavailable,
	}
	rs := replicaSet("rollout-restart-abc123", "test", 1, 1)
	olderPod1 := pod("older1", "test", metav1.NewTime(now.Add(-10*time.Second)), rs)
	olderPod2 := pod("older2", "test", metav1.NewTime(now.Add(-10*time.Second)), rs)
	newerPod := pod("newer", "test", metav1.NewTime(now.Add(10*time.Second)), rs)

	t.Run("Restart multiple", func(t *testing.T) {
		roCtx := &rolloutContext{
			rollout:  ro,
			log:      log.WithRollout(ro),
			stableRS: rs,
			allRSs:   []*appsv1.ReplicaSet{rs},
		}
		client := fake.NewSimpleClientset(rs, olderPod1, olderPod2, newerPod)
		r := RolloutPodRestarter{client: client}
		err := r.Reconcile(roCtx)
		assert.NotNil(t, roCtx.newStatus.RestartedAt)
		assert.Nil(t, err)
		actions := client.Actions()
		// Client uses list and two evict API
		assert.Len(t, actions, 3)
		_, ok := actions[1].(k8stesting.CreateAction)
		assert.True(t, ok)
		_, ok = actions[2].(k8stesting.CreateAction)
		assert.True(t, ok)
	})
	t.Run("Restart multiple honor availability", func(t *testing.T) {
		roCtx := &rolloutContext{
			rollout:  ro,
			log:      log.WithRollout(ro),
			stableRS: rs,
			allRSs:   []*appsv1.ReplicaSet{rs},
		}
		newerPod := pod("newer", "test", metav1.NewTime(now.Add(10*time.Second)), rs)
		newerPod.Status.Conditions = nil // make newer pod unavailable
		client := fake.NewSimpleClientset(rs, olderPod1, olderPod2, newerPod)
		enqueued := false
		r := RolloutPodRestarter{
			client: client,
			enqueueAfter: func(obj interface{}, duration time.Duration) {
				enqueued = true
			},
		}
		err := r.Reconcile(roCtx)
		assert.Nil(t, roCtx.newStatus.RestartedAt)
		assert.Nil(t, err)
		actions := client.Actions()
		// Client uses list and one evict API
		assert.Len(t, actions, 2)
		_, ok := actions[1].(k8stesting.CreateAction)
		assert.True(t, ok)
		assert.True(t, enqueued)
	})
	t.Run("maxUnavailable zero", func(t *testing.T) {
		ro := ro.DeepCopy()
		zeroUnavailable := intstr.FromInt(0)
		ro.Spec.Strategy.Canary.MaxUnavailable = &zeroUnavailable
		roCtx := &rolloutContext{
			rollout:  ro,
			log:      log.WithRollout(ro),
			stableRS: rs,
			allRSs:   []*appsv1.ReplicaSet{rs},
		}
		client := fake.NewSimpleClientset(rs, olderPod1, olderPod2, newerPod)
		enqueued := false
		r := RolloutPodRestarter{
			client: client,
			enqueueAfter: func(obj interface{}, duration time.Duration) {
				enqueued = true
			},
		}
		err := r.Reconcile(roCtx)
		assert.Nil(t, roCtx.newStatus.RestartedAt)
		assert.Nil(t, err)
		actions := client.Actions()
		// Client uses list and one evict API
		assert.Len(t, actions, 2)
		_, ok := actions[1].(k8stesting.CreateAction)
		assert.True(t, ok)
		assert.True(t, enqueued)
	})
	t.Run("maxUnavailable 100%", func(t *testing.T) {
		ro := ro.DeepCopy()
		allUnavailable := intstr.FromString("100%")
		ro.Spec.Strategy.Canary.MaxUnavailable = &allUnavailable
		roCtx := &rolloutContext{
			rollout:  ro,
			log:      log.WithRollout(ro),
			stableRS: rs,
			allRSs:   []*appsv1.ReplicaSet{rs},
		}
		client := fake.NewSimpleClientset(rs, olderPod1, olderPod2, newerPod)
		enqueued := false
		r := RolloutPodRestarter{
			client: client,
			enqueueAfter: func(obj interface{}, duration time.Duration) {
				enqueued = true
			},
		}
		err := r.Reconcile(roCtx)
		assert.NotNil(t, roCtx.newStatus.RestartedAt)
		assert.Nil(t, err)
		actions := client.Actions()
		// Client uses list and two evict API
		assert.Len(t, actions, 3)
		_, ok := actions[1].(k8stesting.CreateAction)
		assert.True(t, ok)
		_, ok = actions[2].(k8stesting.CreateAction)
		assert.True(t, ok)
		assert.False(t, enqueued)
	})
	t.Run("replicas:1 weight:50", func(t *testing.T) {
		ro := ro.DeepCopy()
		ro.Spec.Replicas = pointer.Int32Ptr(1)
		ro.Spec.Strategy.Canary.MaxUnavailable = nil
		rs2 := replicaSet("rollout-restart-def456", "test", 1, 1)
		olderPod2 := pod("older2", "test", metav1.NewTime(now.Add(-10*time.Second)), rs2)

		roCtx := &rolloutContext{
			rollout:  ro,
			log:      log.WithRollout(ro),
			stableRS: rs,
			allRSs:   []*appsv1.ReplicaSet{rs, rs2},
		}
		client := fake.NewSimpleClientset(rs, rs2, olderPod1, olderPod2)
		enqueued := false
		r := RolloutPodRestarter{
			client: client,
			enqueueAfter: func(obj interface{}, duration time.Duration) {
				enqueued = true
			},
		}
		err := r.Reconcile(roCtx)
		assert.Nil(t, roCtx.newStatus.RestartedAt)
		assert.Nil(t, err)
		actions := client.Actions()
		// Client uses list and two evict API
		assert.Len(t, actions, 2)
		_, ok := actions[1].(k8stesting.CreateAction)
		assert.True(t, ok)
		assert.True(t, enqueued)
	})
	t.Run("replicas:1 weight:50, already at minAvailable", func(t *testing.T) {
		ro := ro.DeepCopy()
		ro.Spec.Replicas = pointer.Int32Ptr(1)
		ro.Spec.Strategy.Canary.MaxUnavailable = nil
		rs := replicaSet("rollout-restart-abc123", "test", 1, 1)
		rs2 := replicaSet("rollout-restart-def456", "test", 1, 1)
		rs2.UID = "4444-5555-6666-7777-8888"
		olderPod2 := pod("older2", "test", metav1.NewTime(now.Add(-10*time.Second)), rs2)
		olderPod2.Status.Conditions = nil // make olderPod2 unavailable

		roCtx := &rolloutContext{
			rollout:  ro,
			log:      log.WithRollout(ro),
			stableRS: rs,
			allRSs:   []*appsv1.ReplicaSet{rs, rs2},
		}
		client := fake.NewSimpleClientset(rs, rs2, olderPod1, olderPod2)
		enqueued := false
		r := RolloutPodRestarter{
			client: client,
			enqueueAfter: func(obj interface{}, duration time.Duration) {
				enqueued = true
			},
		}
		err := r.Reconcile(roCtx)
		assert.Nil(t, roCtx.newStatus.RestartedAt)
		assert.Nil(t, err)
		actions := client.Actions()
		// Client uses list
		assert.Len(t, actions, 1)
		assert.True(t, enqueued)
	})
	t.Run("replicas:0", func(t *testing.T) {
		ro := ro.DeepCopy()
		ro.Spec.Replicas = pointer.Int32Ptr(0)
		rs := replicaSet("rollout-restart-abc123", "test", 0, 0)
		roCtx := &rolloutContext{
			rollout:  ro,
			log:      log.WithRollout(ro),
			stableRS: rs,
			allRSs:   []*appsv1.ReplicaSet{rs},
		}
		client := fake.NewSimpleClientset(rs)
		enqueued := false
		r := RolloutPodRestarter{
			client: client,
			enqueueAfter: func(obj interface{}, duration time.Duration) {
				enqueued = true
			},
		}
		err := r.Reconcile(roCtx)
		assert.NotNil(t, roCtx.newStatus.RestartedAt)
		assert.Nil(t, err)
		actions := client.Actions()
		// Client uses list
		assert.Len(t, actions, 1)
		assert.False(t, enqueued)
	})
}

func TestRestartRespectPodDisruptionBudget(t *testing.T) {
	now := metav1.Now()
	rs := replicaSet("rollout-restart-abc123", "test", 1, 1)
	olderPod := pod("older", "test", metav1.NewTime(now.Add(-10*time.Second)), rs)

	expectedErrMsg := "Cannot evict pod as it would violate the pod's disruption budget."
	client := fake.NewSimpleClientset(rs, olderPod)
	roCtx := &rolloutContext{
		rollout: rollout("test", now, nil),
		log:     logrus.WithField("", ""),
		allRSs:  []*appsv1.ReplicaSet{rs},
	}
	client.PrependReactor("create", "pods", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, k8serrors.NewTooManyRequestsError(expectedErrMsg)
	})
	enqueueCalled := false
	r := RolloutPodRestarter{
		client: client,
		enqueueAfter: func(obj interface{}, duration time.Duration) {
			enqueueCalled = true
		},
	}
	err := r.Reconcile(roCtx)
	assert.NoError(t, err)
	assert.True(t, enqueueCalled)
}
