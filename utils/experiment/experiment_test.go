package experiment

import (
	"sort"
	"testing"
	"time"

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
	passedDuration, _ := PassedDurations(e)
	assert.False(t, passedDuration)

	e.Spec.Duration = pointer.Int32Ptr(1)
	passedDuration, _ = PassedDurations(e)
	assert.False(t, passedDuration)

	now := metav1.Now()
	e.Status.AvailableAt = &now
	passedDuration, _ = PassedDurations(e)
	assert.False(t, passedDuration)

	e.Status.AvailableAt = &metav1.Time{Time: now.Add(-2 * time.Second)}
	passedDuration, _ = PassedDurations(e)
	assert.True(t, passedDuration)

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
func TestReplicaSetNameFromExperiment(t *testing.T) {
	templateName := "template"
	template := v1alpha1.TemplateSpec{
		Name: templateName,
	}
	e := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
	}
	assert.Equal(t, "foo-template-6cb88c6bcf", ReplicasetNameFromExperiment(e, template))

	newTemplateStatus := v1alpha1.TemplateStatus{
		Name:           templateName,
		CollisionCount: pointer.Int32Ptr(1),
	}
	e.Status.TemplateStatuses = append(e.Status.TemplateStatuses, newTemplateStatus)
	assert.Equal(t, "foo-template-868df74786", ReplicasetNameFromExperiment(e, template))
}

func TestExperimentByCreationTimestamp(t *testing.T) {

	now := metav1.Now()
	before := metav1.NewTime(metav1.Now().Add(-5 * time.Second))

	newExperiment := func(createTimeStamp metav1.Time, name string) *v1alpha1.Experiment {
		return &v1alpha1.Experiment{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				CreationTimestamp: createTimeStamp,
			},
		}
	}

	t.Run("Use name if both have same creation timestamp", func(t *testing.T) {
		ex := []*v1alpha1.Experiment{
			newExperiment(now, "xyz"),
			newExperiment(now, "abc"),
		}
		expected := []*v1alpha1.Experiment{
			newExperiment(now, "abc"),
			newExperiment(now, "xyz"),
		}
		sort.Sort(ExperimentByCreationTimestamp(ex))
		assert.Equal(t, expected, ex)
	})
	t.Run("Use same creation timestamp", func(t *testing.T) {
		ex := []*v1alpha1.Experiment{
			newExperiment(now, "xyz"),
			newExperiment(before, "abc"),
		}
		expected := []*v1alpha1.Experiment{
			newExperiment(before, "abc"),
			newExperiment(now, "xyz"),
		}
		sort.Sort(ExperimentByCreationTimestamp(ex))
		assert.Equal(t, expected, ex)
	})
}

func TestExperimentGeneratedNameFromRollout(t *testing.T) {
	r := v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					Steps: []v1alpha1.CanaryStep{{
						Experiment: &v1alpha1.RolloutExperimentStep{},
					}},
				},
			},
		},
	}
	name := ExperimentGeneratedNameFromRollout(&r)
	assert.Equal(t, "foo-6cb88c6bcf-0-", name)

	r.Status.CurrentStepIndex = pointer.Int32Ptr(1)
	name = ExperimentGeneratedNameFromRollout(&r)
	assert.Equal(t, "foo-6cb88c6bcf-1-", name)
}
