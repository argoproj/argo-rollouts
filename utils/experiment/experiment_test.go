package experiment

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
)

func TestHasStarted(t *testing.T) {
	e := &v1alpha1.Experiment{}
	assert.False(t, HasStarted(e))

	e.Status.Status = v1alpha1.AnalysisStatusPending
	assert.True(t, HasStarted(e))
}

func TestHasFinished(t *testing.T) {
	e := &v1alpha1.Experiment{}
	assert.False(t, HasFinished(e))

	e.Status.Status = v1alpha1.AnalysisStatusRunning
	assert.False(t, HasFinished(e))

	e.Status.Status = v1alpha1.AnalysisStatusSuccessful
	assert.True(t, HasFinished(e))
}

func TestCalculateTemplateReplicasCount(t *testing.T) {
	e := &v1alpha1.Experiment{}
	template := v1alpha1.TemplateSpec{
		Name: "template",
	}
	assert.Equal(t, int32(1), CalculateTemplateReplicasCount(e, template))

	e.Status.Status = v1alpha1.AnalysisStatusSuccessful
	assert.Equal(t, int32(0), CalculateTemplateReplicasCount(e, template))

	e.Status.Status = v1alpha1.AnalysisStatusRunning
	e.Status.TemplateStatuses = append(e.Status.TemplateStatuses, v1alpha1.TemplateStatus{
		Name:   "template",
		Status: v1alpha1.TemplateStatusFailed,
	})
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

func TestIsTeriminating(t *testing.T) {
	{
		e := &v1alpha1.Experiment{
			Spec: v1alpha1.ExperimentSpec{
				Terminate: true,
			},
		}
		assert.True(t, IsTerminating(e))
	}
	{
		e := &v1alpha1.Experiment{
			Status: v1alpha1.ExperimentStatus{
				Status: v1alpha1.AnalysisStatusFailed,
			},
		}
		assert.True(t, IsTerminating(e))
	}
	{
		e := &v1alpha1.Experiment{
			Status: v1alpha1.ExperimentStatus{
				Status: v1alpha1.AnalysisStatusRunning,
				TemplateStatuses: []v1alpha1.TemplateStatus{
					{
						Status: v1alpha1.TemplateStatusFailed,
					},
				},
			},
		}
		assert.True(t, IsTerminating(e))
	}
	{
		e := &v1alpha1.Experiment{
			Status: v1alpha1.ExperimentStatus{
				Status: v1alpha1.AnalysisStatusRunning,
				AnalysisRuns: []v1alpha1.ExperimentAnalysisRunStatus{
					{
						Status: v1alpha1.AnalysisStatusFailed,
					},
				},
			},
		}
		assert.True(t, IsTerminating(e))
	}

	{
		e := &v1alpha1.Experiment{}
		assert.False(t, IsTerminating(e))
	}
}

func TestGetAnalysisRunStatus(t *testing.T) {
	e := &v1alpha1.Experiment{
		Status: v1alpha1.ExperimentStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			AnalysisRuns: []v1alpha1.ExperimentAnalysisRunStatus{
				{
					Name:   "foo",
					Status: v1alpha1.AnalysisStatusFailed,
				},
			},
		},
	}
	assert.Equal(t, &e.Status.AnalysisRuns[0], GetAnalysisRunStatus(e.Status, "foo"))
	assert.Nil(t, GetAnalysisRunStatus(e.Status, "bar"))
}

func TestGetTemplateStatus(t *testing.T) {
	e := &v1alpha1.Experiment{
		Status: v1alpha1.ExperimentStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			TemplateStatuses: []v1alpha1.TemplateStatus{
				{
					Name:   "foo",
					Status: v1alpha1.TemplateStatusFailed,
				},
			},
		},
	}
	assert.Equal(t, &e.Status.TemplateStatuses[0], GetTemplateStatus(e.Status, "foo"))
	assert.Nil(t, GetTemplateStatus(e.Status, "bar"))
}

func TestSetTemplateStatus(t *testing.T) {
	es := &v1alpha1.ExperimentStatus{}
	fooStatus := v1alpha1.TemplateStatus{
		Name: "foo",
	}
	SetTemplateStatus(es, fooStatus)
	assert.Equal(t, fooStatus, es.TemplateStatuses[0])
	barStatus := v1alpha1.TemplateStatus{
		Name: "bar",
	}
	SetTemplateStatus(es, barStatus)
	assert.Equal(t, barStatus, es.TemplateStatuses[1])
	fooStatus.Status = v1alpha1.TemplateStatusFailed
	SetTemplateStatus(es, fooStatus)
	assert.Len(t, es.TemplateStatuses, 2)
	assert.Equal(t, v1alpha1.TemplateStatusFailed, es.TemplateStatuses[0].Status)
}

func TestSetAnalysisStatus(t *testing.T) {
	es := &v1alpha1.ExperimentStatus{}
	fooStatus := v1alpha1.ExperimentAnalysisRunStatus{
		Name: "foo",
	}
	SetAnalysisRunStatus(es, fooStatus)
	assert.Equal(t, fooStatus, es.AnalysisRuns[0])
	barStatus := v1alpha1.ExperimentAnalysisRunStatus{
		Name: "bar",
	}
	SetAnalysisRunStatus(es, barStatus)
	assert.Equal(t, barStatus, es.AnalysisRuns[1])
	fooStatus.Status = v1alpha1.AnalysisStatusFailed
	SetAnalysisRunStatus(es, fooStatus)
	assert.Len(t, es.AnalysisRuns, 2)
	assert.Equal(t, v1alpha1.AnalysisStatusFailed, es.AnalysisRuns[0].Status)
}

func TestTemplateIsWorse(t *testing.T) {
	{
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusSuccessful, v1alpha1.TemplateStatusSuccessful))
		assert.True(t, TemplateIsWorse(v1alpha1.TemplateStatusSuccessful, v1alpha1.TemplateStatusRunning))
		assert.True(t, TemplateIsWorse(v1alpha1.TemplateStatusSuccessful, v1alpha1.TemplateStatusProgressing))
		assert.True(t, TemplateIsWorse(v1alpha1.TemplateStatusSuccessful, v1alpha1.TemplateStatusError))
		assert.True(t, TemplateIsWorse(v1alpha1.TemplateStatusSuccessful, v1alpha1.TemplateStatusFailed))
	}
	{
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusRunning, v1alpha1.TemplateStatusSuccessful))
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusRunning, v1alpha1.TemplateStatusRunning))
		assert.True(t, TemplateIsWorse(v1alpha1.TemplateStatusRunning, v1alpha1.TemplateStatusProgressing))
		assert.True(t, TemplateIsWorse(v1alpha1.TemplateStatusRunning, v1alpha1.TemplateStatusError))
		assert.True(t, TemplateIsWorse(v1alpha1.TemplateStatusRunning, v1alpha1.TemplateStatusFailed))
	}
	{
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusProgressing, v1alpha1.TemplateStatusSuccessful))
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusProgressing, v1alpha1.TemplateStatusRunning))
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusProgressing, v1alpha1.TemplateStatusProgressing))
		assert.True(t, TemplateIsWorse(v1alpha1.TemplateStatusProgressing, v1alpha1.TemplateStatusError))
		assert.True(t, TemplateIsWorse(v1alpha1.TemplateStatusProgressing, v1alpha1.TemplateStatusFailed))
	}
	{
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusError, v1alpha1.TemplateStatusSuccessful))
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusError, v1alpha1.TemplateStatusRunning))
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusError, v1alpha1.TemplateStatusProgressing))
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusError, v1alpha1.TemplateStatusError))
		assert.True(t, TemplateIsWorse(v1alpha1.TemplateStatusError, v1alpha1.TemplateStatusFailed))
	}
	{
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusFailed, v1alpha1.TemplateStatusSuccessful))
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusFailed, v1alpha1.TemplateStatusRunning))
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusFailed, v1alpha1.TemplateStatusProgressing))
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusFailed, v1alpha1.TemplateStatusError))
		assert.False(t, TemplateIsWorse(v1alpha1.TemplateStatusFailed, v1alpha1.TemplateStatusFailed))
	}
}

func TestWorse(t *testing.T) {
	assert.Equal(t, v1alpha1.TemplateStatusFailed, Worst(v1alpha1.TemplateStatusSuccessful, v1alpha1.TemplateStatusFailed))
	assert.Equal(t, v1alpha1.TemplateStatusFailed, Worst(v1alpha1.TemplateStatusFailed, v1alpha1.TemplateStatusSuccessful))
	assert.Equal(t, v1alpha1.TemplateStatusSuccessful, Worst(v1alpha1.TemplateStatusSuccessful, v1alpha1.TemplateStatusSuccessful))
}

func TestTerminate(t *testing.T) {
	e := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
	}
	client := fake.NewSimpleClientset(e)
	patched := false
	client.PrependReactor("patch", "experiments", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			if string(patchAction.GetPatch()) == `{"spec":{"terminate":true}}` {
				patched = true
			}
		}
		return true, e, nil
	})
	expIf := client.ArgoprojV1alpha1().Experiments(metav1.NamespaceDefault)
	err := Terminate(expIf, "foo")
	assert.NoError(t, err)
	assert.True(t, patched)
}
