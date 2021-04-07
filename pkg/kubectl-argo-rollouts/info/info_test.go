package info

import (
	"strconv"
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
			Name:       "can-guestbook",
			Namespace:  "test",
			Generation: 1,
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
			ObservedGeneration: "1",
			CurrentStepIndex:   pointer.Int32Ptr(1),
			Replicas:           4,
			ReadyReplicas:      1,
			UpdatedReplicas:    3,
			AvailableReplicas:  2,
		},
	}
}

func newBlueGreenRollout() *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "bg-guestbook",
			Namespace:  "test",
			Generation: 1,
		},
		Spec: v1alpha1.RolloutSpec{
			Replicas: pointer.Int32Ptr(5),
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{},
			},
		},
		Status: v1alpha1.RolloutStatus{
			ObservedGeneration: "1",
			Replicas:           4,
			ReadyReplicas:      1,
			UpdatedReplicas:    3,
			AvailableReplicas:  2,
		},
	}
}
func TestAge(t *testing.T) {
	m := metav1.ObjectMeta{
		CreationTimestamp: metav1.NewTime(time.Now().Add(-7 * time.Hour * time.Duration(24))),
	}
	assert.Equal(t, "7d", Age(m))
}

func TestCanaryRolloutInfo(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns)
	assert.Equal(t, roInfo.ObjectMeta.Name, rolloutObjs.Rollouts[0].Name)
	assert.Len(t, Revisions(roInfo), 3)

	assert.Equal(t, Images(roInfo), []ImageInfo{
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
		assert.Equal(t, roInfo.ObjectMeta.Name, rolloutObjs.Rollouts[0].Name)
		assert.Len(t, Revisions(roInfo), 3)

		assert.Len(t, ReplicaSetsByRevision(roInfo, 11), 1)
		assert.Len(t, ReplicaSetsByRevision(roInfo, 10), 1)
		assert.Len(t, ReplicaSetsByRevision(roInfo, 8), 1)

		assert.Equal(t, roInfo.ReplicaSets[0].ScaleDownDeadline, "")
		assert.Equal(t, ScaleDownDelay(*roInfo.ReplicaSets[0]), "")

		assert.Equal(t, Images(roInfo), []ImageInfo{
			{
				Image: "argoproj/rollouts-demo:blue",
				Tags:  []string{InfoTagStable, InfoTagActive},
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

		assert.Equal(t, roInfo.ReplicaSets[1].ObjectMeta.UID, delayedRs)
		assert.Equal(t, roInfo.ReplicaSets[1].ScaleDownDeadline, inFourHours)
		assert.Equal(t, ScaleDownDelay(*roInfo.ReplicaSets[1]), "3h59m")
	}
}

func TestExperimentAnalysisRolloutInfo(t *testing.T) {
	rolloutObjs := testdata.NewExperimentAnalysisRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns)
	assert.Equal(t, roInfo.ObjectMeta.Name, rolloutObjs.Rollouts[0].Name)
	assert.Len(t, Revisions(roInfo), 2)

	assert.Len(t, ReplicaSetsByRevision(roInfo, 1), 1)
	assert.Len(t, ReplicaSetsByRevision(roInfo, 2), 1)
	assert.Len(t, ExperimentsByRevision(roInfo, 2), 1)
	assert.Len(t, AnalysisRunsByRevision(roInfo, 2), 1)

	assert.Equal(t, Images(roInfo), []ImageInfo{
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
	assert.Equal(t, expInfo.ObjectMeta.Name, rolloutObjs.Experiments[0].Name)

	assert.Equal(t, ExperimentImages(expInfo), []ImageInfo{
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
		Type:    v1alpha1.RolloutProgressing,
		Reason:  "ProgressDeadlineExceeded",
		Message: "timed out",
	})
	status, message := RolloutStatusString(ro)
	assert.Equal(t, "Degraded", status)
	assert.Equal(t, "ProgressDeadlineExceeded: timed out", message)
}

func TestRolloutStatusInvalidSpec(t *testing.T) {
	rolloutObjs := testdata.NewInvalidRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns)
	assert.Equal(t, "Degraded", roInfo.Status)
	assert.Equal(t, "InvalidSpec: The Rollout \"rollout-invalid\" is invalid: spec.template.metadata.labels: Invalid value: map[string]string{\"app\":\"doesnt-match\"}: `selector` does not match template `labels`", roInfo.Message)
}

func TestRolloutAborted(t *testing.T) {
	rolloutObjs := testdata.NewAbortedRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns)
	assert.Equal(t, "Degraded", roInfo.Status)
	assert.Equal(t, `RolloutAborted: metric "web" assessed Failed due to failed (1) > failureLimit (0)`, roInfo.Message)
}

func TestRolloutStatusPaused(t *testing.T) {
	ro := newCanaryRollout()
	ro.Spec.Paused = true
	status, message := RolloutStatusString(ro)
	assert.Equal(t, "Paused", status)
	assert.Equal(t, "manually paused", message)
}

func TestRolloutStatusProgressing(t *testing.T) {
	{
		ro := newCanaryRollout()
		ro.Spec.Replicas = pointer.Int32Ptr(5)
		ro.Status.UpdatedReplicas = 4
		ro.Status.AvailableReplicas = 4
		ro.Status.Replicas = 5
		status, message := RolloutStatusString(ro)
		assert.Equal(t, "Progressing", status)
		assert.Equal(t, "more replicas need to be updated", message)
	}
	{
		ro := newCanaryRollout()
		ro.Spec.Replicas = pointer.Int32Ptr(5)
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 4
		ro.Status.Replicas = 5
		status, message := RolloutStatusString(ro)
		assert.Equal(t, "Progressing", status)
		assert.Equal(t, "updated replicas are still becoming available", message)
	}
	{
		ro := newCanaryRollout()
		ro.Spec.Replicas = pointer.Int32Ptr(5)
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		ro.Status.Replicas = 7
		status, message := RolloutStatusString(ro)
		assert.Equal(t, "Progressing", status)
		assert.Equal(t, "old replicas are pending termination", message)
	}
	{
		ro := newBlueGreenRollout()
		ro.Status.BlueGreen.ActiveSelector = "abc1234"
		ro.Status.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "def5678"
		ro.Spec.Replicas = pointer.Int32Ptr(5)
		ro.Status.Replicas = 5
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		status, message := RolloutStatusString(ro)
		assert.Equal(t, "Progressing", status)
		assert.Equal(t, "active service cutover pending", message)
	}
	{
		ro := newBlueGreenRollout()
		ro.Status.BlueGreen.ActiveSelector = "def5678"
		ro.Status.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "def5678"
		ro.Spec.Replicas = pointer.Int32Ptr(5)
		ro.Status.Replicas = 5
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		status, message := RolloutStatusString(ro)
		assert.Equal(t, "Progressing", status)
		assert.Equal(t, "waiting for analysis to complete", message)
	}
	{
		// Scenario when a newly created rollout has partially filled in status (with hashes)
		// but no updated replica count
		ro := newCanaryRollout()
		ro.Spec.Replicas = nil
		ro.Status = v1alpha1.RolloutStatus{
			ObservedGeneration: strconv.Itoa(int(ro.Generation)),
			StableRS:           "abc1234",
			CurrentPodHash:     "abc1234",
		}
		status, message := RolloutStatusString(ro)
		assert.Equal(t, "Progressing", status)
		assert.Equal(t, "more replicas need to be updated", message)
	}
	{
		// Rollout observed generation is not updated
		ro := newCanaryRollout()
		ro.Generation = 2
		ro.Spec.Replicas = nil
		ro.Status = v1alpha1.RolloutStatus{
			StableRS:           "abc1234",
			CurrentPodHash:     "abc1234",
			ObservedGeneration: "1",
		}
		status, message := RolloutStatusString(ro)
		assert.Equal(t, "Progressing", status)
		assert.Equal(t, "waiting for rollout spec update to be observed", message)
	}
	{
		// Make sure we skip isGenerationObserved check when rollout is a v0.9 legacy rollout using
		// a hash and not a numeric observed generation
		ro := newCanaryRollout()
		ro.Generation = 2
		ro.Spec.Replicas = nil
		ro.Status = v1alpha1.RolloutStatus{
			StableRS:           "abc1234",
			CurrentPodHash:     "abc1234",
			ObservedGeneration: "7d66d4485f",
		}
		status, message := RolloutStatusString(ro)
		assert.Equal(t, "Progressing", status)
		assert.Equal(t, "more replicas need to be updated", message)
	}
	{
		// Verify isGenerationObserved detects a v0.9 legacy rollout which has an all numeric hash
		ro := newCanaryRollout()
		ro.Generation = 2
		ro.Spec.Replicas = nil
		ro.Status = v1alpha1.RolloutStatus{
			StableRS:           "abc1234",
			CurrentPodHash:     "abc1234",
			ObservedGeneration: "1366344857",
		}
		status, message := RolloutStatusString(ro)
		assert.Equal(t, "Progressing", status)
		assert.Equal(t, "more replicas need to be updated", message)
	}
}

func TestRolloutStatusHealthy(t *testing.T) {
	{
		ro := newCanaryRollout()
		ro.Status.Replicas = 5
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		ro.Status.ReadyReplicas = 5
		ro.Status.StableRS = "abc1234"
		ro.Status.CurrentPodHash = "abc1234"
		status, message := RolloutStatusString(ro)
		assert.Equal(t, "Healthy", status)
		assert.Equal(t, "", message)
	}
	{
		ro := newBlueGreenRollout()
		ro.Status.Replicas = 5
		ro.Status.UpdatedReplicas = 5
		ro.Status.AvailableReplicas = 5
		ro.Status.ReadyReplicas = 5
		ro.Status.BlueGreen.ActiveSelector = "abc1234"
		ro.Status.CurrentPodHash = "abc1234"
		ro.Status.StableRS = "abc1234"
		status, message := RolloutStatusString(ro)
		assert.Equal(t, "Healthy", status)
		assert.Equal(t, "", message)
	}
}
