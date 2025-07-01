package rollout

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	argofake "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
)

type fakeRefResolver struct{}

func (f *fakeRefResolver) Resolve(_ *v1alpha1.Rollout) error {
	return nil
}

func TestSetRolloutRevisionServerSideApply(t *testing.T) {
	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-rollout",
			Namespace: "default",
			Annotations: map[string]string{
				// To ensure we don't void existing annotations.
				"existing-annotation":          "true",
				annotations.RevisionAnnotation: "1",
			},
		},
		Spec: v1alpha1.RolloutSpec{
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "test",
							Image: "nginx:1.0",
						},
					},
				},
			},
		},
	}

	fakeArgoprojClientset := argofake.NewSimpleClientset(rollout)
	fakeKubeClientset := kubefake.NewSimpleClientset()
	recorder := record.NewFakeEventRecorder()

	ctx := &rolloutContext{
		reconcilerBase: reconcilerBase{
			argoprojclientset: fakeArgoprojClientset,
			kubeclientset:     fakeKubeClientset,
			recorder:          recorder,
			refResolver:       &fakeRefResolver{},
		},
		rollout: rollout,
		log:     logutil.WithRollout(rollout),
	}

	err := ctx.setRolloutRevision("2")
	require.NoError(t, err)

	actions := fakeArgoprojClientset.Actions()
	require.Len(t, actions, 1)

	action := actions[0]
	assert.Equal(t, "patch", action.GetVerb())
	assert.Equal(t, "rollouts", action.GetResource().Resource)
	assert.Equal(t, "default", action.GetNamespace())

	patchAction, ok := action.(ktesting.PatchAction)
	require.True(t, ok, "Expected PatchAction but got %T", patchAction)
	assert.Equal(t, patchtypes.ApplyPatchType, patchAction.GetPatchType())

	patchData := patchAction.GetPatch()
	var patchedRollout v1alpha1.Rollout
	err = json.Unmarshal(patchData, &patchedRollout)
	require.NoError(t, err)

	assert.Equal(t, "argoproj.io/v1alpha1", patchedRollout.APIVersion)
	assert.Equal(t, "Rollout", patchedRollout.Kind)
	assert.Equal(t, "test-rollout", patchedRollout.Name)
	assert.Equal(t, "default", patchedRollout.Namespace)
	assert.Equal(t, "2", patchedRollout.Annotations[annotations.RevisionAnnotation])

	retrievedRollout, err := fakeArgoprojClientset.ArgoprojV1alpha1().Rollouts("default").Get(context.TODO(), "test-rollout", metav1.GetOptions{})
	require.NoError(t, err)
	assert.Equal(
		t,
		map[string]string{
			"existing-annotation":          "true",
			annotations.RevisionAnnotation: "2",
		},
		retrievedRollout.Annotations,
	)
}
