package info

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info/testdata"
)

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
	rolloutObjs := testdata.NewBlueGreenRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods, rolloutObjs.Experiments, rolloutObjs.AnalysisRuns)
	assert.Equal(t, roInfo.Name, rolloutObjs.Rollouts[0].Name)
	assert.Len(t, roInfo.Revisions(), 3)

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
