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

func TestHasFinished(t *testing.T) {
	e := &v1alpha1.Experiment{}
	assert.False(t, HasFinished(e))

	e.Status.Phase = v1alpha1.AnalysisPhaseRunning
	assert.False(t, HasFinished(e))

	e.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
	assert.True(t, HasFinished(e))
}

func TestCalculateTemplateReplicasCount(t *testing.T) {
	e := &v1alpha1.Experiment{}
	template := v1alpha1.TemplateSpec{
		Name: "template",
	}
	assert.Equal(t, int32(1), CalculateTemplateReplicasCount(e, template))

	e.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
	assert.Equal(t, int32(0), CalculateTemplateReplicasCount(e, template))

	e.Status.Phase = v1alpha1.AnalysisPhaseRunning
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

	e.Spec.Duration = "1s-typo"
	passedDuration, _ = PassedDurations(e)
	assert.False(t, passedDuration)

	e.Spec.Duration = "1s"
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
	assert.Equal(t, "foo-template-76bbb58f74", ReplicasetNameFromExperiment(e, template))

	newTemplateStatus := v1alpha1.TemplateStatus{
		Name:           templateName,
		CollisionCount: pointer.Int32Ptr(1),
	}
	e.Status.TemplateStatuses = append(e.Status.TemplateStatuses, newTemplateStatus)
	assert.Equal(t, "foo-template-688c48b575", ReplicasetNameFromExperiment(e, template))
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

func TestIsTerminating(t *testing.T) {
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
				Phase: v1alpha1.AnalysisPhaseFailed,
			},
		}
		assert.True(t, IsTerminating(e))
	}
	{
		e := &v1alpha1.Experiment{
			Status: v1alpha1.ExperimentStatus{
				Phase: v1alpha1.AnalysisPhaseRunning,
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
				Phase: v1alpha1.AnalysisPhaseRunning,
				AnalysisRuns: []v1alpha1.ExperimentAnalysisRunStatus{
					{
						Phase: v1alpha1.AnalysisPhaseFailed,
					},
				},
			},
		}
		assert.True(t, IsTerminating(e))
	}
	{
		e := &v1alpha1.Experiment{
			Spec: v1alpha1.ExperimentSpec{
				Analyses: []v1alpha1.ExperimentAnalysisTemplateRef{{
					Name:                  "foo",
					RequiredForCompletion: true,
				}},
			},
			Status: v1alpha1.ExperimentStatus{
				Phase: v1alpha1.AnalysisPhaseRunning,
				AnalysisRuns: []v1alpha1.ExperimentAnalysisRunStatus{
					{
						Name:  "foo",
						Phase: v1alpha1.AnalysisPhaseSuccessful,
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
			Phase: v1alpha1.AnalysisPhaseRunning,
			AnalysisRuns: []v1alpha1.ExperimentAnalysisRunStatus{
				{
					Name:  "foo",
					Phase: v1alpha1.AnalysisPhaseFailed,
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
			Phase: v1alpha1.AnalysisPhaseRunning,
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
	fooStatus.Phase = v1alpha1.AnalysisPhaseFailed
	SetAnalysisRunStatus(es, fooStatus)
	assert.Len(t, es.AnalysisRuns, 2)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, es.AnalysisRuns[0].Phase)
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

func TestIsSemanticallyEqual(t *testing.T) {
	left := &v1alpha1.ExperimentSpec{
		Templates: []v1alpha1.TemplateSpec{
			{
				Name: "canary",
			},
		},
	}
	right := left.DeepCopy()
	right.Terminate = true
	assert.True(t, IsSemanticallyEqual(*left, *right))
	right.Templates[0].Replicas = pointer.Int32Ptr(1)
	assert.False(t, IsSemanticallyEqual(*left, *right))
}

func TestRequiredAnalysisRunsSuccessful(t *testing.T) {
	e := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
	}
	assert.False(t, RequiredAnalysisRunsSuccessful(e, nil))
	assert.False(t, RequiredAnalysisRunsSuccessful(e, &e.Status))
	e.Spec.Analyses = []v1alpha1.ExperimentAnalysisTemplateRef{{
		Name: "foo",
	}}
	e.Status.AnalysisRuns = []v1alpha1.ExperimentAnalysisRunStatus{{
		Name:  "foo",
		Phase: v1alpha1.AnalysisPhaseRunning,
	}}
	assert.False(t, RequiredAnalysisRunsSuccessful(e, &e.Status))
	e.Spec.Analyses[0].RequiredForCompletion = true
	assert.False(t, RequiredAnalysisRunsSuccessful(e, &e.Status))
	e.Status.AnalysisRuns[0].Phase = v1alpha1.AnalysisPhaseSuccessful
	assert.True(t, RequiredAnalysisRunsSuccessful(e, &e.Status))
}

func TestHasRequiredAnalysisRuns(t *testing.T) {
	e := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
	}
	assert.False(t, HasRequiredAnalysisRuns(e))
	e.Spec.Analyses = []v1alpha1.ExperimentAnalysisTemplateRef{{
		Name: "foo",
	}}
	assert.False(t, HasRequiredAnalysisRuns(e))
	e.Spec.Analyses[0].RequiredForCompletion = true
	assert.True(t, HasRequiredAnalysisRuns(e))
}
