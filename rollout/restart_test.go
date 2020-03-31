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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/log"
)

func rollout(restartAt metav1.Time, restartedAt *metav1.Time) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			RestartAt: restartAt.DeepCopy(),
		},
		Status: v1alpha1.RolloutStatus{
			RestartedAt: restartedAt.DeepCopy(),
		},
	}
}

func pod(selector string, time metav1.Time) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: time,
			Labels: map[string]string{
				"test": selector,
			},
		},
	}
}

func replicaSet(selector string, replicas, available int32) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
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

func TestCheckEnqueueRollout(t *testing.T) {
	now := metav1.Now()
	t.Run(".Spec.Restart not set", func(t *testing.T) {
		roCtx := &canaryContext{
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
		ro := rollout(metav1.NewTime(now.Add(-10*time.Minute)), nil)
		roCtx := &canaryContext{
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
		ro := rollout(metav1.NewTime(now.Add(5*time.Minute)), nil)
		enqueued := false
		roCtx := &canaryContext{
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
		ro := rollout(metav1.NewTime(now.Add(5*time.Minute)), nil)
		roCtx := &canaryContext{
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

func TestReconcile(t *testing.T) {
	now := metav1.Now()
	olderPod := pod("test", metav1.NewTime(now.Add(-10*time.Second)))
	newerPod := pod("test", metav1.NewTime(now.Add(10*time.Second)))
	rs := replicaSet("test", 1, 1)

	t.Run("No Restart Needed", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		r := RolloutPodRestarter{client: client}
		noRestartRo := rollout(now, &now)
		roCtx := &canaryContext{
			rollout: noRestartRo,
			log:     log.WithRollout(noRestartRo),
		}
		err := r.Reconcile(roCtx)
		assert.Nil(t, err)
		assert.Len(t, client.Actions(), 0)
	})

	t.Run("Not all ReplicaSets are fully available", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		notFullyAvailable := replicaSet("test", 1, 0)
		logrus.New()
		buf := bytes.NewBufferString("")
		logger := logrus.New()
		logger.SetOutput(buf)
		roCtx := &canaryContext{
			rollout: rollout(now, nil),
			log:     logrus.NewEntry(logger),
			allRSs:  []*appsv1.ReplicaSet{notFullyAvailable},
		}
		r := RolloutPodRestarter{client: client}
		err := r.Reconcile(roCtx)
		assert.Nil(t, err)
		assert.Len(t, client.Actions(), 0)
		assert.Contains(t, buf.String(), "cannot restart pods as not all ReplicasSets are fully available")

	})
	t.Run("Fails to delete Pod", func(t *testing.T) {
		expectedErrMsg := "big bad error"
		client := fake.NewSimpleClientset(rs, olderPod)
		roCtx := &canaryContext{
			rollout: rollout(now, nil),
			log:     logrus.WithField("", ""),
			allRSs:  []*appsv1.ReplicaSet{rs},
		}
		client.PrependReactor("delete", "pods", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, fmt.Errorf(expectedErrMsg)
		})
		r := RolloutPodRestarter{client: client}
		err := r.Reconcile(roCtx)
		assert.Errorf(t, err, expectedErrMsg)
	})
	t.Run("Deletes Pod", func(t *testing.T) {
		client := fake.NewSimpleClientset(rs, olderPod)
		roCtx := &canaryContext{
			rollout: rollout(now, nil),
			log:     logrus.WithField("", ""),
			allRSs:  []*appsv1.ReplicaSet{rs},
		}
		r := RolloutPodRestarter{client: client}
		err := r.Reconcile(roCtx)
		assert.Nil(t, err)
		actions := client.Actions()
		assert.Len(t, actions, 2)
		_, ok := actions[1].(k8stesting.DeleteAction)
		assert.True(t, ok)
	})
	t.Run("No more pods to delete", func(t *testing.T) {
		client := fake.NewSimpleClientset(rs, newerPod)
		roCtx := &canaryContext{
			rollout: rollout(now, nil),
			log:     logrus.WithField("", ""),
			allRSs:  []*appsv1.ReplicaSet{rs},
		}
		r := RolloutPodRestarter{client: client}
		err := r.Reconcile(roCtx)
		assert.Nil(t, err)
		assert.Len(t, client.Actions(), 1)
		assert.Equal(t, now, *roCtx.NewStatus().RestartedAt)
	})
}

func TestReconcilePodsInReplicaSet(t *testing.T) {
	now := metav1.Now()
	ro := rollout(now, nil)
	roCtx := &canaryContext{
		rollout: ro,
		log:     log.WithRollout(ro),
	}
	olderPod := pod("test", metav1.NewTime(now.Add(-10*time.Second)))
	newerPod := pod("test", metav1.NewTime(now.Add(10*time.Second)))
	differentSelector := pod("test2", metav1.NewTime(now.Add(-10*time.Second)))
	rs := replicaSet("test", 1, 1)
	t.Run("Finds no pods to delete to due to different label selector", func(t *testing.T) {
		client := fake.NewSimpleClientset(rs, differentSelector)
		r := RolloutPodRestarter{client: client}
		deletedPod, err := r.reconcilePodsInReplicaSet(roCtx, rs)
		assert.False(t, deletedPod)
		assert.Nil(t, err)
		// Client uses list API but not the delete API
		assert.Len(t, client.Actions(), 1)

	})
	t.Run("Delete Pod successfully", func(t *testing.T) {
		client := fake.NewSimpleClientset(rs, olderPod)
		r := RolloutPodRestarter{client: client}
		deletedPod, err := r.reconcilePodsInReplicaSet(roCtx, rs)
		assert.True(t, deletedPod)
		assert.Nil(t, err)
		actions := client.Actions()
		// Client uses list and delete API
		assert.Len(t, actions, 2)
		_, ok := actions[1].(k8stesting.DeleteAction)
		assert.True(t, ok)
	})
	t.Run("No Pod Deletion required", func(t *testing.T) {
		client := fake.NewSimpleClientset(rs, newerPod)
		r := RolloutPodRestarter{client: client}
		deletedPod, err := r.reconcilePodsInReplicaSet(roCtx, rs)
		assert.False(t, deletedPod)
		assert.Nil(t, err)
		// Client uses list API but not the delete API
		assert.Len(t, client.Actions(), 1)

	})
	t.Run("Pod List error", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		expectedErrMsg := "big bad error"
		client.PrependReactor("list", "pods", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
			return true, nil, fmt.Errorf(expectedErrMsg)
		})
		r := RolloutPodRestarter{client: client}
		roCtx := &canaryContext{
			rollout: ro,
			log:     log.WithRollout(ro),
		}
		deletedPod, err := r.reconcilePodsInReplicaSet(roCtx, rs)
		assert.False(t, deletedPod)
		assert.Error(t, err, expectedErrMsg)
	})

}

func TestSortReplicaSetsByPriority(t *testing.T) {
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
		roCtx := &canaryContext{
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
