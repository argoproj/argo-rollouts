package info

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info/testdata"
)

func newCanaryRollout() *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "can-guestbook",
			Namespace: "test",
		},
		Spec: v1alpha1.RolloutSpec{
			Replicas: pointer.Int32Ptr(5),
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					Steps: []v1alpha1.CanaryStep{
						{
							SetWeight: pointer.Int32Ptr(10),
						},
						{
							Pause: &v1alpha1.RolloutPause{
								Duration: v1alpha1.DurationFromInt(60),
							},
						},
						{
							SetWeight: pointer.Int32Ptr(20),
						},
					},
				},
			},
		},
		Status: v1alpha1.RolloutStatus{
			CurrentStepIndex:  pointer.Int32Ptr(1),
			Replicas:          4,
			ReadyReplicas:     1,
			UpdatedReplicas:   3,
			AvailableReplicas: 2,
		},
	}
}

func newBlueGreenRollout() *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "bg-guestbook",
			Namespace: "test",
		},
		Spec: v1alpha1.RolloutSpec{
			Replicas: pointer.Int32Ptr(5),
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{},
			},
		},
		Status: v1alpha1.RolloutStatus{
			CurrentStepIndex:  pointer.Int32Ptr(1),
			Replicas:          4,
			ReadyReplicas:     1,
			UpdatedReplicas:   3,
			AvailableReplicas: 2,
		},
	}
}

func TestAge(t *testing.T) {
	m := Metadata{
		CreationTimestamp: metav1.NewTime(time.Now().Add(-7 * time.Hour * time.Duration(24))),
	}
	assert.Equal(t, "7d", m.Age())
}

func TestCanaryRolloutInfo(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns)
	assert.Equal(t, roInfo.Name, rolloutObjs.Rollouts[0].Name)
	assert.Len(t, roInfo.Revisions(), 3)

	assert.Equal(t, roInfo.Images(), []ImageInfo{
		{
			Image: "argoproj/rollouts-demo:does-not-exist",
			Tags:  []string{InfoTagCanary},
		},
		{
			Image: "argoproj/rollouts-demo:green",
			Tags:  []string{InfoTagStable},
		},
	})
}

func TestBlueGreenRolloutInfo(t *testing.T) {
	{
		rolloutObjs := testdata.NewBlueGreenRollout()
		roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns)
		assert.Equal(t, roInfo.Name, rolloutObjs.Rollouts[0].Name)
		assert.Len(t, roInfo.Revisions(), 3)

		assert.Len(t, roInfo.ReplicaSetsByRevision(11), 1)
		assert.Len(t, roInfo.ReplicaSetsByRevision(10), 1)
		assert.Len(t, roInfo.ReplicaSetsByRevision(8), 1)

		assert.Equal(t, roInfo.ReplicaSets[0].ScaleDownDeadline, "")
		assert.Equal(t, roInfo.ReplicaSets[0].ScaleDownDelay(), "")

		assert.Equal(t, roInfo.Images(), []ImageInfo{
			{
				Image: "argoproj/rollouts-demo:blue",
				Tags:  []string{InfoTagActive},
			},
			{
				Image: "argoproj/rollouts-demo:green",
				Tags:  []string{InfoTagPreview},
			},
		})
	}
	{
		rolloutObjs := testdata.NewBlueGreenRollout()
		inFourHours := metav1.Now().Add(4 * time.Hour).Truncate(time.Second).UTC().Format(time.RFC3339)
		rolloutObjs.ReplicaSets[0].Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inFourHours
		delayedRs := rolloutObjs.ReplicaSets[0].ObjectMeta.UID
		roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns)

		assert.Equal(t, roInfo.ReplicaSets[1].UID, delayedRs)
		assert.Equal(t, roInfo.ReplicaSets[1].ScaleDownDeadline, inFourHours)
		assert.Equal(t, roInfo.ReplicaSets[1].ScaleDownDelay(), "3h59m")
	}
}

func TestExperimentAnalysisRolloutInfo(t *testing.T) {
	rolloutObjs := testdata.NewExperimentAnalysisRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns)
	assert.Equal(t, roInfo.Name, rolloutObjs.Rollouts[0].Name)
	assert.Len(t, roInfo.Revisions(), 2)

	assert.Len(t, roInfo.ReplicaSetsByRevision(1), 1)
	assert.Len(t, roInfo.ReplicaSetsByRevision(2), 1)
	assert.Len(t, roInfo.ExperimentsByRevision(2), 1)
	assert.Len(t, roInfo.AnalysisRunsByRevision(2), 1)

	assert.Equal(t, roInfo.Images(), []ImageInfo{
		{
			Image: "argoproj/rollouts-demo:blue",
			Tags:  []string{InfoTagStable},
		},
		{
			Image: "argoproj/rollouts-demo:yellow",
			Tags:  []string{InfoTagCanary},
		},
	})
}

func TestExperimentInfo(t *testing.T) {
	rolloutObjs := testdata.NewExperimentAnalysisRollout()
	expInfo := NewExperimentInfo(rolloutObjs.Experiments[0], rolloutObjs.ReplicaSets, rolloutObjs.AnalysisRuns, rolloutObjs.Pods)
	assert.Equal(t, expInfo.Name, rolloutObjs.Experiments[0].Name)

	assert.Equal(t, expInfo.Images(), []ImageInfo{
		{
			Image: "argoproj/rollouts-demo:blue",
		},
		{
			Image: "argoproj/rollouts-demo:yellow",
		},
	})
}

func TestRolloutStatusDegraded(t *testing.T) {
	ro := newCanaryRollout()
	ro.Status.Conditions = append(ro.Status.Conditions, v1alpha1.RolloutCondition{
		Type:   v1alpha1.RolloutProgressing,
		Reason: "ProgressDeadlineExceeded",
	})
	assert.Equal(t, "Degraded", RolloutStatusString(ro))
}

func TestRolloutStatusInvalidSpec(t *testing.T) {
	ro := newCanaryRollout()
	ro.Status.Conditions = append(ro.Status.Conditions, v1alpha1.RolloutCondition{
		Type: v1alpha1.InvalidSpec,
	})
	assert.Equal(t, string(v1alpha1.InvalidSpec), RolloutStatusString(ro))
}

func TestRolloutStatusPaused(t *testing.T) {
	ro := newCanaryRollout()
	ro.Spec.Paused = true
	assert.Equal(t, "Paused", RolloutStatusString(ro))
}

func TestRolloutStatusProgressing(t *testing.T) {
	{
		ro := newCanaryRollout()
		assert.Equal(t, "Progressing", RolloutStatusString(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.UpdatedReplicas = 1
		ro.Status.Replicas = 2
		assert.Equal(t, "Progressing", RolloutStatusString(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.UpdatedReplicas = 2
		ro.Status.Replicas = 1
		assert.Equal(t, "Progressing", RolloutStatusString(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.AvailableReplicas = 1
		ro.Status.UpdatedReplicas = 2
		assert.Equal(t, "Progressing", RolloutStatusString(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.AvailableReplicas = 1
		ro.Status.UpdatedReplicas = 2
		assert.Equal(t, "Progressing", RolloutStatusString(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.StableRS = ""
		assert.Equal(t, "Progressing", RolloutStatusString(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "def5678"
		assert.Equal(t, "Progressing", RolloutStatusString(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.BlueGreen.ActiveSelector = ""
		assert.Equal(t, "Progressing", RolloutStatusString(ro))
	}
	{
		ro := newCanaryRollout()
		ro.Status.BlueGreen.ActiveSelector = "abc1234"
		ro.Status.CurrentPodHash = "def5678"
		assert.Equal(t, "Progressing", RolloutStatusString(ro))
	}
}

func TestRolloutStatusHealthy(t *testing.T) {
	{
		ro := newCanaryRollout()
		ro.Status.Replicas = 1
		ro.Status.UpdatedReplicas = 1
		ro.Status.AvailableReplicas = 1
		ro.Status.ReadyReplicas = 1
		ro.Status.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "abc1234"
		assert.Equal(t, "Healthy", RolloutStatusString(ro))
	}
	{
		ro := newBlueGreenRollout()
		ro.Status.Replicas = 1
		ro.Status.UpdatedReplicas = 1
		ro.Status.AvailableReplicas = 1
		ro.Status.ReadyReplicas = 1
		ro.Status.BlueGreen.ActiveSelector = "abc1234"
		ro.Status.CurrentPodHash = "abc1234"
		assert.Equal(t, "Healthy", RolloutStatusString(ro))
	}
}
