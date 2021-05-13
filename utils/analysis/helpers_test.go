package analysis

import (
	"errors"
	"fmt"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
)

func TestIsWorst(t *testing.T) {
	assert.False(t, IsWorse(v1alpha1.AnalysisPhaseSuccessful, v1alpha1.AnalysisPhaseSuccessful))
	assert.True(t, IsWorse(v1alpha1.AnalysisPhaseSuccessful, v1alpha1.AnalysisPhaseInconclusive))
	assert.True(t, IsWorse(v1alpha1.AnalysisPhaseSuccessful, v1alpha1.AnalysisPhaseError))
	assert.True(t, IsWorse(v1alpha1.AnalysisPhaseSuccessful, v1alpha1.AnalysisPhaseFailed))

	assert.False(t, IsWorse(v1alpha1.AnalysisPhaseInconclusive, v1alpha1.AnalysisPhaseSuccessful))
	assert.False(t, IsWorse(v1alpha1.AnalysisPhaseInconclusive, v1alpha1.AnalysisPhaseInconclusive))
	assert.True(t, IsWorse(v1alpha1.AnalysisPhaseInconclusive, v1alpha1.AnalysisPhaseError))
	assert.True(t, IsWorse(v1alpha1.AnalysisPhaseInconclusive, v1alpha1.AnalysisPhaseFailed))

	assert.False(t, IsWorse(v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseError))
	assert.False(t, IsWorse(v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseSuccessful))
	assert.False(t, IsWorse(v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseInconclusive))
	assert.True(t, IsWorse(v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed))

	assert.False(t, IsWorse(v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseSuccessful))
	assert.False(t, IsWorse(v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseInconclusive))
	assert.False(t, IsWorse(v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseError))
	assert.False(t, IsWorse(v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseFailed))
}

func TestWorst(t *testing.T) {
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, Worst(v1alpha1.AnalysisPhaseSuccessful, v1alpha1.AnalysisPhaseFailed))
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, Worst(v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseSuccessful))
}

func TestIsFastFailTerminating(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:  "other-metric",
					Phase: v1alpha1.AnalysisPhaseRunning,
				},
				{
					Name:  "success-rate",
					Phase: v1alpha1.AnalysisPhaseRunning,
				},
			},
		},
	}
	successRate := run.Status.MetricResults[1]
	assert.False(t, IsTerminating(run))
	successRate.Phase = v1alpha1.AnalysisPhaseError
	run.Status.MetricResults[1] = successRate
	assert.True(t, IsTerminating(run))
	successRate.Phase = v1alpha1.AnalysisPhaseFailed
	run.Status.MetricResults[1] = successRate
	assert.True(t, IsTerminating(run))
	successRate.Phase = v1alpha1.AnalysisPhaseInconclusive
	run.Status.MetricResults[1] = successRate
	assert.True(t, IsTerminating(run))
	run.Status.MetricResults = nil
	assert.False(t, IsTerminating(run))
	run.Status = v1alpha1.AnalysisRunStatus{}
	assert.False(t, IsTerminating(run))
}

func TestGetResult(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:  "success-rate",
					Phase: v1alpha1.AnalysisPhaseRunning,
				},
			},
		},
	}
	assert.Nil(t, GetResult(run, "non-existent"))
	assert.Equal(t, run.Status.MetricResults[0], *GetResult(run, "success-rate"))
}

func TestSetResult(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Status: v1alpha1.AnalysisRunStatus{},
	}
	res := v1alpha1.MetricResult{
		Name:  "success-rate",
		Phase: v1alpha1.AnalysisPhaseRunning,
	}

	SetResult(run, res)
	assert.Equal(t, res, run.Status.MetricResults[0])
	res.Phase = v1alpha1.AnalysisPhaseFailed
	SetResult(run, res)
	assert.Equal(t, res, run.Status.MetricResults[0])
}

func TestMetricCompleted(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:  "success-rate",
					Phase: v1alpha1.AnalysisPhaseRunning,
				},
			},
		},
	}
	assert.False(t, MetricCompleted(run, "non-existent"))
	assert.False(t, MetricCompleted(run, "success-rate"))

	run.Status.MetricResults[0] = v1alpha1.MetricResult{
		Name:  "success-rate",
		Phase: v1alpha1.AnalysisPhaseError,
	}
	assert.True(t, MetricCompleted(run, "success-rate"))
}

func TestLastMeasurement(t *testing.T) {
	m1 := v1alpha1.Measurement{
		Phase: v1alpha1.AnalysisPhaseSuccessful,
		Value: "99",
	}
	m2 := v1alpha1.Measurement{
		Phase: v1alpha1.AnalysisPhaseSuccessful,
		Value: "98",
	}
	run := &v1alpha1.AnalysisRun{
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:         "success-rate",
					Phase:        v1alpha1.AnalysisPhaseRunning,
					Measurements: []v1alpha1.Measurement{m1, m2},
				},
			},
		},
	}
	assert.Nil(t, LastMeasurement(run, "non-existent"))
	assert.Equal(t, m2, *LastMeasurement(run, "success-rate"))
	successRate := run.Status.MetricResults[0]
	successRate.Measurements = []v1alpha1.Measurement{}
	run.Status.MetricResults[0] = successRate
	assert.Nil(t, LastMeasurement(run, "success-rate"))
}

func TestIsTerminating(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:  "other-metric",
					Phase: v1alpha1.AnalysisPhaseRunning,
				},
				{
					Name:  "success-rate",
					Phase: v1alpha1.AnalysisPhaseRunning,
				},
			},
		},
	}
	assert.False(t, IsTerminating(run))
	run.Spec.Terminate = true
	assert.True(t, IsTerminating(run))
	run.Spec.Terminate = false
	successRate := run.Status.MetricResults[1]
	successRate.Phase = v1alpha1.AnalysisPhaseError
	run.Status.MetricResults[1] = successRate
	assert.True(t, IsTerminating(run))
}

func TestTerminateRun(t *testing.T) {
	e := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
	}
	client := fake.NewSimpleClientset(e)
	patched := false
	client.PrependReactor("patch", "analysisruns", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if patchAction, ok := action.(kubetesting.PatchAction); ok {
			if string(patchAction.GetPatch()) == `{"spec":{"terminate":true}}` {
				patched = true
			}
		}
		return true, e, nil
	})
	runIf := client.ArgoprojV1alpha1().AnalysisRuns(metav1.NamespaceDefault)
	err := TerminateRun(runIf, "foo")
	assert.NoError(t, err)
	assert.True(t, patched)
}

func TestIsSemanticallyEqual(t *testing.T) {
	left := &v1alpha1.AnalysisRunSpec{
		Metrics: []v1alpha1.Metric{
			{
				Name: "success-rate",
			},
		},
	}
	right := left.DeepCopy()
	right.Terminate = true
	assert.True(t, IsSemanticallyEqual(*left, *right))
	right.Metrics[0].Name = "foo"
	assert.False(t, IsSemanticallyEqual(*left, *right))
}

func TestCreateWithCollisionCounterNoController(t *testing.T) {
	run := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
	}
	client := fake.NewSimpleClientset(&run)
	runIf := client.ArgoprojV1alpha1().AnalysisRuns(metav1.NamespaceDefault)
	logCtx := log.NewEntry(log.New())
	_, err := CreateWithCollisionCounter(logCtx, runIf, run)
	assert.EqualError(t, err, "Supplied run does not have an owner reference")
}

func TestCreateWithCollisionCounterError(t *testing.T) {
	run := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
			OwnerReferences: []metav1.OwnerReference{
				{
					UID:        types.UID("fake-uid"),
					Controller: pointer.BoolPtr(true),
				},
			},
		},
	}
	client := fake.NewSimpleClientset(&run)
	client.PrependReactor("create", "analysisruns", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("intentional error")
	})
	runIf := client.ArgoprojV1alpha1().AnalysisRuns(metav1.NamespaceDefault)
	logCtx := log.NewEntry(log.New())
	_, err := CreateWithCollisionCounter(logCtx, runIf, run)
	assert.EqualError(t, err, "intentional error")
}

func TestCreateWithCollisionCounterStillRunning(t *testing.T) {
	run := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
			OwnerReferences: []metav1.OwnerReference{
				{
					UID:        types.UID("fake-uid"),
					Controller: pointer.BoolPtr(true),
				},
			},
		},
	}
	client := fake.NewSimpleClientset(&run)
	runIf := client.ArgoprojV1alpha1().AnalysisRuns(metav1.NamespaceDefault)
	logCtx := log.NewEntry(log.New())
	createdRun, err := CreateWithCollisionCounter(logCtx, runIf, run)
	assert.NoError(t, err)
	assert.Equal(t, run.Name, createdRun.Name)
}

func TestCreateWithCollisionCounter(t *testing.T) {
	run := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
			OwnerReferences: []metav1.OwnerReference{
				{
					UID:        types.UID("fake-uid"),
					Controller: pointer.BoolPtr(true),
				},
			},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseFailed,
		},
	}
	client := fake.NewSimpleClientset(&run)
	runIf := client.ArgoprojV1alpha1().AnalysisRuns(metav1.NamespaceDefault)
	logCtx := log.NewEntry(log.New())
	createdRun, err := CreateWithCollisionCounter(logCtx, runIf, run)
	assert.NoError(t, err)
	assert.Equal(t, run.Name+".1", createdRun.Name)
}

func TestFlattenTemplates(t *testing.T) {
	metric := func(name, successCondition string) v1alpha1.Metric {
		return v1alpha1.Metric{
			Name:             name,
			SuccessCondition: successCondition,
		}
	}
	arg := func(name string, value *string) v1alpha1.Argument {
		return v1alpha1.Argument{
			Name:  name,
			Value: value,
		}
	}
	t.Run("Handle empty list", func(t *testing.T) {
		template, err := FlattenTemplates([]*v1alpha1.AnalysisTemplate{}, []*v1alpha1.ClusterAnalysisTemplate{})
		assert.Nil(t, err)
		assert.Len(t, template.Spec.Metrics, 0)
		assert.Len(t, template.Spec.Args, 0)

	})
	t.Run("No changes on single template", func(t *testing.T) {
		orig := &v1alpha1.AnalysisTemplate{
			Spec: v1alpha1.AnalysisTemplateSpec{
				Metrics: []v1alpha1.Metric{metric("foo", "{{args.test}}")},
				Args:    []v1alpha1.Argument{arg("test", pointer.StringPtr("true"))},
			},
		}
		template, err := FlattenTemplates([]*v1alpha1.AnalysisTemplate{orig}, []*v1alpha1.ClusterAnalysisTemplate{})
		assert.Nil(t, err)
		assert.Equal(t, orig.Spec, template.Spec)
	})
	t.Run("Merge multiple metrics successfully", func(t *testing.T) {
		fooMetric := metric("foo", "true")
		barMetric := metric("bar", "true")
		template, err := FlattenTemplates([]*v1alpha1.AnalysisTemplate{
			{
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: []v1alpha1.Metric{fooMetric},
					Args:    nil,
				},
			}, {
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: []v1alpha1.Metric{barMetric},
					Args:    nil,
				},
			},
		}, []*v1alpha1.ClusterAnalysisTemplate{})
		assert.Nil(t, err)
		assert.Nil(t, template.Spec.Args)
		assert.Len(t, template.Spec.Metrics, 2)
		assert.Equal(t, fooMetric, template.Spec.Metrics[0])
		assert.Equal(t, barMetric, template.Spec.Metrics[1])
	})
	t.Run("Merge analysis templates and cluster templates successfully", func(t *testing.T) {
		fooMetric := metric("foo", "true")
		barMetric := metric("bar", "true")
		template, err := FlattenTemplates([]*v1alpha1.AnalysisTemplate{
			{
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: []v1alpha1.Metric{fooMetric},
					Args:    nil,
				},
			},
		}, []*v1alpha1.ClusterAnalysisTemplate{
			{
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: []v1alpha1.Metric{barMetric},
					Args:    nil,
				},
			},
		})
		assert.Nil(t, err)
		assert.Nil(t, template.Spec.Args)
		assert.Len(t, template.Spec.Metrics, 2)
		assert.Equal(t, fooMetric, template.Spec.Metrics[0])
		assert.Equal(t, barMetric, template.Spec.Metrics[1])
	})
	t.Run(" Merge fail with name collision", func(t *testing.T) {
		fooMetric := metric("foo", "true")
		template, err := FlattenTemplates([]*v1alpha1.AnalysisTemplate{
			{
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: []v1alpha1.Metric{fooMetric},
					Args:    nil,
				},
			}, {
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: []v1alpha1.Metric{fooMetric},
					Args:    nil,
				},
			},
		}, []*v1alpha1.ClusterAnalysisTemplate{})
		assert.Nil(t, template)
		assert.Equal(t, err, fmt.Errorf("two metrics have the same name 'foo'"))
	})
	t.Run("Merge multiple args successfully", func(t *testing.T) {
		fooArgs := arg("foo", pointer.StringPtr("true"))
		barArgs := arg("bar", pointer.StringPtr("true"))
		template, err := FlattenTemplates([]*v1alpha1.AnalysisTemplate{
			{
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: nil,
					Args:    []v1alpha1.Argument{fooArgs},
				},
			}, {
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: nil,
					Args:    []v1alpha1.Argument{barArgs},
				},
			},
		}, []*v1alpha1.ClusterAnalysisTemplate{})
		assert.Nil(t, err)
		assert.Len(t, template.Spec.Args, 2)
		assert.Equal(t, fooArgs, template.Spec.Args[0])
		assert.Equal(t, barArgs, template.Spec.Args[1])
	})
	t.Run(" Merge args with same name but only one has value", func(t *testing.T) {
		fooArgsValue := arg("foo", pointer.StringPtr("true"))
		fooArgsNoValue := arg("foo", nil)
		template, err := FlattenTemplates([]*v1alpha1.AnalysisTemplate{
			{
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: nil,
					Args:    []v1alpha1.Argument{fooArgsValue},
				},
			}, {
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: nil,
					Args:    []v1alpha1.Argument{fooArgsNoValue},
				},
			},
		}, []*v1alpha1.ClusterAnalysisTemplate{})
		assert.Nil(t, err)
		assert.Len(t, template.Spec.Args, 1)
		assert.Contains(t, template.Spec.Args, fooArgsValue)
	})
	t.Run("Error: merge args with same name and both have values", func(t *testing.T) {
		fooArgs := arg("foo", pointer.StringPtr("true"))
		fooArgsWithDiffValue := arg("foo", pointer.StringPtr("false"))
		template, err := FlattenTemplates([]*v1alpha1.AnalysisTemplate{
			{
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: nil,
					Args:    []v1alpha1.Argument{fooArgs},
				},
			}, {
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: nil,
					Args:    []v1alpha1.Argument{fooArgsWithDiffValue},
				},
			},
		}, []*v1alpha1.ClusterAnalysisTemplate{})
		assert.Equal(t, fmt.Errorf("Argument `foo` specified multiple times with different values: 'true', 'false'"), err)
		assert.Nil(t, template)
	})
}

func TestNewAnalysisRunFromTemplates(t *testing.T) {
	templates := []*v1alpha1.AnalysisTemplate{{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "success-rate",
				},
			},
			Args: []v1alpha1.Argument{
				{
					Name: "my-arg",
				},
				{
					Name: "my-secret",
				},
			},
		},
	}}

	clustertemplates := []*v1alpha1.ClusterAnalysisTemplate{}

	arg := v1alpha1.Argument{
		Name:  "my-arg",
		Value: pointer.StringPtr("my-val"),
	}
	secretArg := v1alpha1.Argument{
		Name: "my-secret",
		ValueFrom: &v1alpha1.ValueFrom{
			SecretKeyRef: &v1alpha1.SecretKeyRef{
				Name: "name",
				Key:  "key",
			},
		},
	}

	args := []v1alpha1.Argument{arg, secretArg}
	run, err := NewAnalysisRunFromTemplates(templates, clustertemplates, args, "foo-run", "foo-run-generate-", "my-ns")
	assert.NoError(t, err)
	assert.Equal(t, "foo-run", run.Name)
	assert.Equal(t, "foo-run-generate-", run.GenerateName)
	assert.Equal(t, "my-ns", run.Namespace)

	assert.Len(t, run.Spec.Args, 2)
	assert.Contains(t, run.Spec.Args, arg)
	assert.Contains(t, run.Spec.Args, secretArg)

	// Fail Merge Args
	unresolvedArg := v1alpha1.Argument{Name: "unresolved"}
	templates[0].Spec.Args = append(templates[0].Spec.Args, unresolvedArg)
	run, err = NewAnalysisRunFromTemplates(templates, clustertemplates, args, "foo-run", "foo-run-generate-", "my-ns")
	assert.Nil(t, run)
	assert.Equal(t, fmt.Errorf("args.unresolved was not resolved"), err)
	// Fail flatten metric
	matchingMetric := &v1alpha1.AnalysisTemplate{
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{{
				Name: "success-rate",
			}},
		},
	}
	// Fail Flatten Templates
	templates = append(templates, matchingMetric)
	run, err = NewAnalysisRunFromTemplates(templates, clustertemplates, args, "foo-run", "foo-run-generate-", "my-ns")
	assert.Nil(t, run)
	assert.Equal(t, fmt.Errorf("two metrics have the same name 'success-rate'"), err)
}

func TestMergeArgs(t *testing.T) {
	{
		// nil list
		args, err := MergeArgs(nil, nil)
		assert.NoError(t, err)
		assert.Nil(t, args)
	}
	{
		// empty list
		args, err := MergeArgs(nil, []v1alpha1.Argument{})
		assert.NoError(t, err)
		assert.Equal(t, []v1alpha1.Argument{}, args)
	}
	{
		// use defaults
		args, err := MergeArgs(
			nil, []v1alpha1.Argument{
				{
					Name:  "foo",
					Value: pointer.StringPtr("bar"),
				},
				{
					Name: "my-secret",
					ValueFrom: &v1alpha1.ValueFrom{
						SecretKeyRef: &v1alpha1.SecretKeyRef{
							Name: "name",
							Key:  "key",
						},
					},
				},
			})
		assert.NoError(t, err)
		assert.Len(t, args, 2)
		assert.Equal(t, "foo", args[0].Name)
		assert.Equal(t, "bar", *args[0].Value)
		assert.Equal(t, "my-secret", args[1].Name)
		assert.NotNil(t, args[1].ValueFrom)
	}
	{
		// overwrite defaults
		args, err := MergeArgs(
			[]v1alpha1.Argument{
				{
					Name:  "foo",
					Value: pointer.StringPtr("overwrite"),
				},
			}, []v1alpha1.Argument{
				{
					Name:  "foo",
					Value: pointer.StringPtr("bar"),
				},
			})
		assert.NoError(t, err)
		assert.Len(t, args, 1)
		assert.Equal(t, "foo", args[0].Name)
		assert.Equal(t, "overwrite", *args[0].Value)
	}
	{
		// not resolved
		args, err := MergeArgs(
			[]v1alpha1.Argument{
				{
					Name: "foo",
				},
			}, []v1alpha1.Argument{
				{
					Name: "foo",
				},
			})
		assert.EqualError(t, err, "args.foo was not resolved")
		assert.Nil(t, args)
	}
	{
		// extra arg
		args, err := MergeArgs(
			[]v1alpha1.Argument{
				{
					Name:  "foo",
					Value: pointer.StringPtr("my-value"),
				},
				{
					Name:  "extra-arg",
					Value: pointer.StringPtr("extra-value"),
				},
			}, []v1alpha1.Argument{
				{
					Name: "foo",
				},
			})
		assert.NoError(t, err)
		assert.Len(t, args, 1)
		assert.Equal(t, "foo", args[0].Name)
		assert.Equal(t, "my-value", *args[0].Value)
	}
}

//TODO(dthomson) remove this test in v0.9.0
func TestNewAnalysisRunFromTemplate(t *testing.T) {
	template := v1alpha1.AnalysisTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "success-rate",
				},
			},
			Args: []v1alpha1.Argument{
				{
					Name: "my-arg",
				},
			},
		},
	}
	args := []v1alpha1.Argument{
		{
			Name:  "my-arg",
			Value: pointer.StringPtr("my-val"),
		},
	}
	run, err := NewAnalysisRunFromTemplate(&template, args, "foo-run", "foo-run-generate-", "my-ns")
	assert.NoError(t, err)
	assert.Equal(t, "foo-run", run.Name)
	assert.Equal(t, "foo-run-generate-", run.GenerateName)
	assert.Equal(t, "my-ns", run.Namespace)
	assert.Equal(t, "my-arg", run.Spec.Args[0].Name)
	assert.Equal(t, "my-val", *run.Spec.Args[0].Value)
}

//TODO(dthomson) remove this test in v0.9.0
func TestNewAnalysisRunFromClusterTemplate(t *testing.T) {
	template := v1alpha1.ClusterAnalysisTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "success-rate",
				},
			},
			Args: []v1alpha1.Argument{
				{
					Name: "my-arg",
				},
			},
		},
	}
	args := []v1alpha1.Argument{
		{
			Name:  "my-arg",
			Value: pointer.StringPtr("my-val"),
		},
	}
	run, err := NewAnalysisRunFromClusterTemplate(&template, args, "foo-run", "foo-run-generate-", "my-ns")
	assert.NoError(t, err)
	assert.Equal(t, "foo-run", run.Name)
	assert.Equal(t, "foo-run-generate-", run.GenerateName)
	assert.Equal(t, "my-ns", run.Namespace)
	assert.Equal(t, "my-arg", run.Spec.Args[0].Name)
	assert.Equal(t, "my-val", *run.Spec.Args[0].Value)
}

func TestGetInstanceID(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
			Labels: map[string]string{
				v1alpha1.LabelKeyControllerInstanceID: "test",
			},
		},
	}
	assert.Equal(t, "test", GetInstanceID(run))
	run.Labels = nil
	assert.Equal(t, "", GetInstanceID(run))
	var nilRun *v1alpha1.AnalysisRun
	assert.Panics(t, func() { GetInstanceID(nilRun) })

}
