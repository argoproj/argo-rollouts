package analysis

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kunstructured "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/unstructured"
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
				{
					Name:   "dry-run-metric",
					Phase:  v1alpha1.AnalysisPhaseRunning,
					DryRun: true,
				},
				{
					Name:  "yet-another-metric",
					Phase: v1alpha1.AnalysisPhaseRunning,
				},
			},
		},
	}
	// Verify that when the metric is not failing or in the error state then we don't terminate.
	successRate := run.Status.MetricResults[1]
	assert.False(t, IsTerminating(run))
	// Metric failing in the dryRun mode shouldn't impact the terminal decision.
	dryRunMetricResult := run.Status.MetricResults[2]
	dryRunMetricResult.Phase = v1alpha1.AnalysisPhaseError
	run.Status.MetricResults[2] = dryRunMetricResult
	assert.False(t, IsTerminating(run))
	// Verify that a wet run metric failure/error which is executed after a dry-run metric results in terminal decision.
	yetAnotherMetric := run.Status.MetricResults[3]
	yetAnotherMetric.Phase = v1alpha1.AnalysisPhaseError
	run.Status.MetricResults[3] = yetAnotherMetric
	assert.True(t, IsTerminating(run))
	// Verify that a wet run metric failure/error results in terminal decision.
	successRate.Phase = v1alpha1.AnalysisPhaseError
	run.Status.MetricResults[1] = successRate
	assert.True(t, IsTerminating(run))
	successRate.Phase = v1alpha1.AnalysisPhaseFailed
	run.Status.MetricResults[1] = successRate
	assert.True(t, IsTerminating(run))
	// Verify that an inconclusive wet run metric results in terminal decision.
	successRate.Phase = v1alpha1.AnalysisPhaseInconclusive
	run.Status.MetricResults[1] = successRate
	assert.True(t, IsTerminating(run))
	// Verify that we don't terminate when there are no metric results or when the status is empty.
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

func TestArrayMeasurement(t *testing.T) {
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
	assert.Nil(t, ArrayMeasurement(run, "non-existent"))
	assert.Equal(t, run.Status.MetricResults[0].Measurements, ArrayMeasurement(run, "success-rate"))
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
					DryRun: []v1alpha1.DryRun{{
						MetricName: "foo",
					}},
					MeasurementRetention: []v1alpha1.MeasurementRetention{{
						MetricName: "foo",
					}},
					Args: nil,
				},
			}, {
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: []v1alpha1.Metric{barMetric},
					DryRun: []v1alpha1.DryRun{{
						MetricName: "bar",
					}},
					MeasurementRetention: []v1alpha1.MeasurementRetention{{
						MetricName: "bar",
					}},
					Args: nil,
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
					DryRun: []v1alpha1.DryRun{
						{
							MetricName: "foo",
						},
					},
					MeasurementRetention: []v1alpha1.MeasurementRetention{
						{
							MetricName: "foo",
						},
					},
					Args: nil,
				},
			},
		}, []*v1alpha1.ClusterAnalysisTemplate{
			{
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: []v1alpha1.Metric{barMetric},
					DryRun: []v1alpha1.DryRun{
						{
							MetricName: "bar",
						},
					},
					MeasurementRetention: []v1alpha1.MeasurementRetention{
						{
							MetricName: "bar",
						},
					},
					Args: nil,
				},
			},
		})
		assert.Nil(t, err)
		assert.Nil(t, template.Spec.Args)
		assert.Len(t, template.Spec.Metrics, 2)
		assert.Equal(t, fooMetric, template.Spec.Metrics[0])
		assert.Equal(t, barMetric, template.Spec.Metrics[1])
	})
	t.Run("Merge fail with name collision", func(t *testing.T) {
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
	t.Run("Merge fail with dry-run name collision", func(t *testing.T) {
		fooMetric := metric("foo", "true")
		barMetric := metric("bar", "true")
		template, err := FlattenTemplates([]*v1alpha1.AnalysisTemplate{
			{
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: []v1alpha1.Metric{fooMetric},
					DryRun: []v1alpha1.DryRun{
						{
							MetricName: "foo",
						},
					},
					Args: nil,
				},
			}, {
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: []v1alpha1.Metric{barMetric},
					DryRun: []v1alpha1.DryRun{
						{
							MetricName: "foo",
						},
					},
					Args: nil,
				},
			},
		}, []*v1alpha1.ClusterAnalysisTemplate{})
		assert.Nil(t, template)
		assert.Equal(t, err, fmt.Errorf("two Dry-Run metric rules have the same name 'foo'"))
	})
	t.Run("Merge fail with measurement retention metrics name collision", func(t *testing.T) {
		fooMetric := metric("foo", "true")
		barMetric := metric("bar", "true")
		template, err := FlattenTemplates([]*v1alpha1.AnalysisTemplate{
			{
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: []v1alpha1.Metric{fooMetric},
					MeasurementRetention: []v1alpha1.MeasurementRetention{
						{
							MetricName: "foo",
						},
					},
					Args: nil,
				},
			}, {
				Spec: v1alpha1.AnalysisTemplateSpec{
					Metrics: []v1alpha1.Metric{barMetric},
					MeasurementRetention: []v1alpha1.MeasurementRetention{
						{
							MetricName: "foo",
						},
					},
					Args: nil,
				},
			},
		}, []*v1alpha1.ClusterAnalysisTemplate{})
		assert.Nil(t, template)
		assert.Equal(t, err, fmt.Errorf("two Measurement Retention metric rules have the same name 'foo'"))
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

	var clusterTemplates []*v1alpha1.ClusterAnalysisTemplate

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
	run, err := NewAnalysisRunFromTemplates(templates, clusterTemplates, args, []v1alpha1.DryRun{}, []v1alpha1.MeasurementRetention{}, "foo-run", "foo-run-generate-", "my-ns")
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
	run, err = NewAnalysisRunFromTemplates(templates, clusterTemplates, args, []v1alpha1.DryRun{}, []v1alpha1.MeasurementRetention{}, "foo-run", "foo-run-generate-", "my-ns")
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
	run, err = NewAnalysisRunFromTemplates(templates, clusterTemplates, args, []v1alpha1.DryRun{}, []v1alpha1.MeasurementRetention{}, "foo-run", "foo-run-generate-", "my-ns")
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

func TestNewAnalysisRunFromUnstructured(t *testing.T) {
	template := v1alpha1.AnalysisTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "foo",
			Namespace:       metav1.NamespaceDefault,
			ResourceVersion: "12345",
		},
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "success-rate",
				},
			},
			Args: []v1alpha1.Argument{
				{
					Name: "my-arg-1",
				},
				{
					Name: "my-arg-2",
				},
			},
		},
	}
	args := []v1alpha1.Argument{
		{
			Name:  "my-arg-1",
			Value: pointer.StringPtr("my-val-1"),
		},
		{
			Name:  "my-arg-2",
			Value: pointer.StringPtr("my-val-2"),
		},
	}

	jsonStr, err := json.Marshal(template)
	assert.NoError(t, err)
	obj, err := unstructured.StrToUnstructured(string(jsonStr))
	assert.NoError(t, err)

	obj, err = NewAnalysisRunFromUnstructured(obj, args, "foo-run", "foo-run-generate-", "my-ns")
	assert.NoError(t, err)
	_, found, err := kunstructured.NestedString(obj.Object, "metadata", "resourceVersion")
	assert.NoError(t, err)
	assert.False(t, found)
	arArgs, _, err := kunstructured.NestedSlice(obj.Object, "spec", "args")
	assert.NoError(t, err)
	assert.Equal(t, len(args), len(arArgs))

	for i, arg := range arArgs {
		argnv := arg.(map[string]interface{})
		assert.Equal(t, *args[i].Value, argnv["value"])
	}
}

func TestCompatibilityNewAnalysisRunFromTemplate(t *testing.T) {
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
	analysisTemplates := []*v1alpha1.AnalysisTemplate{&template}
	run, err := NewAnalysisRunFromTemplates(analysisTemplates, nil, args, nil, nil, "foo-run", "foo-run-generate-", "my-ns")
	assert.NoError(t, err)
	assert.Equal(t, "foo-run", run.Name)
	assert.Equal(t, "foo-run-generate-", run.GenerateName)
	assert.Equal(t, "my-ns", run.Namespace)
	assert.Equal(t, "my-arg", run.Spec.Args[0].Name)
	assert.Equal(t, "my-val", *run.Spec.Args[0].Value)
}

func TestCompatibilityNewAnalysisRunFromClusterTemplate(t *testing.T) {
	clusterTemplate := v1alpha1.ClusterAnalysisTemplate{
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
	clusterAnalysisTemplates := []*v1alpha1.ClusterAnalysisTemplate{&clusterTemplate}
	run, err := NewAnalysisRunFromTemplates(nil, clusterAnalysisTemplates, args, nil, nil, "foo-run", "foo-run-generate-", "my-ns")
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

func TestGetDryRunMetrics(t *testing.T) {
	t.Run("GetDryRunMetrics returns the metric names map", func(t *testing.T) {
		failureLimit := intstr.FromInt(2)
		count := intstr.FromInt(1)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					Count:        &count,
					FailureLimit: &failureLimit,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
			DryRun: []v1alpha1.DryRun{
				{
					MetricName: "success-rate",
				},
			},
		}
		dryRunMetricNamesMap, err := GetDryRunMetrics(spec.DryRun, spec.Metrics)
		assert.Nil(t, err)
		assert.True(t, dryRunMetricNamesMap["success-rate"])
	})
	t.Run("GetDryRunMetrics handles the RegEx rules", func(t *testing.T) {
		failureLimit := intstr.FromInt(2)
		count := intstr.FromInt(1)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					Count:        &count,
					FailureLimit: &failureLimit,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
				{
					Name:         "error-rate",
					Count:        &count,
					FailureLimit: &failureLimit,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
			DryRun: []v1alpha1.DryRun{
				{
					MetricName: ".*",
				},
			},
		}
		dryRunMetricNamesMap, err := GetDryRunMetrics(spec.DryRun, spec.Metrics)
		assert.Nil(t, err)
		assert.Equal(t, len(dryRunMetricNamesMap), 2)
	})
	t.Run("GetDryRunMetrics throw error when a rule doesn't get matched", func(t *testing.T) {
		failureLimit := intstr.FromInt(2)
		count := intstr.FromInt(1)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					Count:        &count,
					FailureLimit: &failureLimit,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
			DryRun: []v1alpha1.DryRun{
				{
					MetricName: "error-rate",
				},
			},
		}
		dryRunMetricNamesMap, err := GetDryRunMetrics(spec.DryRun, spec.Metrics)
		assert.EqualError(t, err, "dryRun[0]: Rule didn't match any metric name(s)")
		assert.Equal(t, len(dryRunMetricNamesMap), 0)
	})
}

func TestGetMeasurementRetentionMetrics(t *testing.T) {
	t.Run("GetMeasurementRetentionMetrics returns the metric names map", func(t *testing.T) {
		failureLimit := intstr.FromInt(2)
		count := intstr.FromInt(1)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					Count:        &count,
					FailureLimit: &failureLimit,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
			MeasurementRetention: []v1alpha1.MeasurementRetention{
				{
					MetricName: "success-rate",
					Limit:      10,
				},
			},
		}
		measurementRetentionMetricNamesMap, err := GetMeasurementRetentionMetrics(spec.MeasurementRetention, spec.Metrics)
		assert.Nil(t, err)
		assert.NotNil(t, measurementRetentionMetricNamesMap["success-rate"])
	})
	t.Run("GetMeasurementRetentionMetrics handles the RegEx rules", func(t *testing.T) {
		failureLimit := intstr.FromInt(2)
		count := intstr.FromInt(1)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					Count:        &count,
					FailureLimit: &failureLimit,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
				{
					Name:         "error-rate",
					Count:        &count,
					FailureLimit: &failureLimit,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
			MeasurementRetention: []v1alpha1.MeasurementRetention{
				{
					MetricName: ".*",
					Limit:      15,
				},
			},
		}
		measurementRetentionMetricNamesMap, err := GetMeasurementRetentionMetrics(spec.MeasurementRetention, spec.Metrics)
		assert.Nil(t, err)
		assert.Equal(t, len(measurementRetentionMetricNamesMap), 2)
	})
	t.Run("GetMeasurementRetentionMetrics throw error when a rule doesn't get matched", func(t *testing.T) {
		failureLimit := intstr.FromInt(2)
		count := intstr.FromInt(1)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					Count:        &count,
					FailureLimit: &failureLimit,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
			MeasurementRetention: []v1alpha1.MeasurementRetention{
				{
					MetricName: "error-rate",
					Limit:      11,
				},
			},
		}
		measurementRetentionMetricNamesMap, err := GetMeasurementRetentionMetrics(spec.MeasurementRetention, spec.Metrics)
		assert.EqualError(t, err, "measurementRetention[0]: Rule didn't match any metric name(s)")
		assert.Equal(t, len(measurementRetentionMetricNamesMap), 0)
	})
}
