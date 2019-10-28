package info

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info/testdata"
)

func TestCanaryRolloutInfo(t *testing.T) {
	rolloutObjs := testdata.NewCanaryRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods)
	assert.Equal(t, roInfo.Name, rolloutObjs.Rollouts[0].Name)
}

func TestBlueGreenRolloutInfo(t *testing.T) {
	rolloutObjs := testdata.NewBlueGreenRollout()
	roInfo := NewRolloutInfo(rolloutObjs.Rollouts[0], rolloutObjs.ReplicaSets, rolloutObjs.Pods)
	assert.Equal(t, roInfo.Name, rolloutObjs.Rollouts[0].Name)
}
