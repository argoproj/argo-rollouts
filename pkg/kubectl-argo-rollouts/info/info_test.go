package info

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info/testdata"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

func TestAge(t *testing.T) {
	m := metav1.ObjectMeta{
		CreationTimestamp: metav1.NewTime(time.Now().Add(-7 * time.Hour * time.Duration(24))),
	}
	assert.Equal(t, "7d", Age(m))
}

func TestCanaryRolloutInfo(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
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

func TestPingPongCanaryRolloutInfo(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[3], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
	assert.Equal(t, roInfo.ObjectMeta.Name, rolloutObjs.Rollouts[3].Name)
	assert.Len(t, Revisions(roInfo), 3)

	assert.Equal(t, Images(roInfo), []ImageInfo{
		{
			Image: "argoproj/rollouts-demo:does-not-exist",
			Tags:  []string{InfoTagCanary, InfoTagPing},
		},
		{
			Image: "argoproj/rollouts-demo:green",
			Tags:  []string{InfoTagStable, InfoTagPong},
		},
	})
}

func TestBlueGreenRolloutInfo(t *testing.T) {
	{
		rolloutObjs := testdata.NewBlueGreenRollout()
		roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
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
		inFourHours := timeutil.Now().Add(4 * time.Hour).Truncate(time.Second).UTC().Format(time.RFC3339)
		rolloutObjs.ReplicaSets[0].Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inFourHours
		delayedRs := rolloutObjs.ReplicaSets[0].ObjectMeta.UID
		roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)

		assert.Equal(t, roInfo.ReplicaSets[1].ObjectMeta.UID, delayedRs)
		assert.Equal(t, roInfo.ReplicaSets[1].ScaleDownDeadline, inFourHours)
		assert.Equal(t, ScaleDownDelay(*roInfo.ReplicaSets[1]), "3h59m")
	}
}

func TestExperimentAnalysisRolloutInfo(t *testing.T) {
	rolloutObjs := testdata.NewExperimentAnalysisRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
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

func TestRolloutStatusInvalidSpec(t *testing.T) {
	rolloutObjs := testdata.NewInvalidRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
	assert.Equal(t, "Degraded", roInfo.Status)
	assert.Equal(t, "InvalidSpec: The Rollout \"rollout-invalid\" is invalid: spec.template.metadata.labels: Invalid value: map[string]string{\"app\":\"doesnt-match\"}: `selector` does not match template `labels`", roInfo.Message)
}

func TestRolloutAborted(t *testing.T) {
	rolloutObjs := testdata.NewAbortedRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns, nil)
	assert.Equal(t, "Degraded", roInfo.Status)
	assert.Equal(t, `RolloutAborted: metric "web" assessed Failed due to failed (1) > failureLimit (0)`, roInfo.Message)
}
