package experiment

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestHasStarted(t *testing.T) {
	e := &v1alpha1.Experiment{}
	assert.False(t, HasStarted(e))

	e.Status.Running = pointer.BoolPtr(true)
	assert.True(t, HasStarted(e))
}

func TestHasFinished(t *testing.T) {
	e := &v1alpha1.Experiment{}
	assert.False(t, HasFinished(e))

	e.Status.Running = pointer.BoolPtr(true)
	assert.False(t, HasFinished(e))

	e.Status.Running = pointer.BoolPtr(false)
	assert.True(t, HasFinished(e))
}

func TestGetTemplateStatus(t *testing.T) {
	e := &v1alpha1.Experiment{}
	template := v1alpha1.TemplateSpec{
		Name: "template",
	}
	templateStatus, index := GetTemplateStatus(e, template)
	assert.Nil(t, templateStatus)
	assert.Nil(t, index)

	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{{
		Name: "template",
	}}
	templateStatus, index = GetTemplateStatus(e, template)
	assert.NotNil(t, templateStatus)
	assert.Equal(t, 0, *index)
}

func TestCalculateTemplateReplicasCount(t *testing.T) {
	e := &v1alpha1.Experiment{}
	template := v1alpha1.TemplateSpec{
		Name: "template",
	}
	assert.Equal(t, int32(1), CalculateTemplateReplicasCount(e, template))

	e.Status.Running = pointer.BoolPtr(false)
	assert.Equal(t, int32(0), CalculateTemplateReplicasCount(e, template))
}

func TestGetCollisionCountForTemplate(t *testing.T) {
	e := &v1alpha1.Experiment{
		Status: v1alpha1.ExperimentStatus{
			TemplateStatuses: []v1alpha1.TemplateStatus{{
				Name: "template",
			}},
		},
	}
	template := v1alpha1.TemplateSpec{
		Name: "template",
	}
	assert.Nil(t, GetCollisionCountForTemplate(e, template))

	e.Status.TemplateStatuses[0].CollisionCount = pointer.Int32Ptr(1)
	assert.Equal(t, int32(1), *GetCollisionCountForTemplate(e, template))

}

func TestReplicasetNameFromExperiment(t *testing.T) {
	e := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
		Status: v1alpha1.ExperimentStatus{
			TemplateStatuses: []v1alpha1.TemplateStatus{{
				Name: "template",
			}},
		},
	}
	template := v1alpha1.TemplateSpec{
		Name: "template",
	}
	assert.Equal(t, ReplicasetNameFromExperiment(e, template), "foo-template-685bdb47d8")
}