package experiment

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"time"

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

func TestCalculateTemplateReplicasCount(t *testing.T) {
	e := &v1alpha1.Experiment{}
	template := v1alpha1.TemplateSpec{
		Name: "template",
	}
	assert.Equal(t, int32(1), CalculateTemplateReplicasCount(e, template))

	e.Status.Running = pointer.BoolPtr(false)
	assert.Equal(t, int32(0), CalculateTemplateReplicasCount(e, template))
}

func TestPassedDurations(t *testing.T) {
	e := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
	}
	assert.False(t, PassedDurations(e))

	e.Spec.Duration = pointer.Int32Ptr(1)
	assert.False(t, PassedDurations(e))

	now := metav1.Now()
	e.Status.AvailableAt = &now
	assert.False(t, PassedDurations(e))

	e.Status.AvailableAt = &metav1.Time{Time: now.Add(-2 * time.Second)}
	assert.True(t, PassedDurations(e))
}

func TestGetTemplateStatusMapping(t *testing.T) {
	ts := v1alpha1.ExperimentStatus{
		TemplateStatuses: []v1alpha1.TemplateStatus{
			{
				Name:     "test",
				Replicas: int32(1),
			},
			{
				Name:     "test2",
				Replicas: int32(2),
			},
		},
	}
	mapping := GetTemplateStatusMapping(ts)
	assert.Len(t, mapping, 2)
	assert.Equal(t, int32(1), mapping["test"].Replicas)
	assert.Equal(t, int32(2), mapping["test2"].Replicas)
}
