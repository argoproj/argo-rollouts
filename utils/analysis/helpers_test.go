package analysis

import (
	"errors"
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
	assert.False(t, IsWorse(v1alpha1.AnalysisStatusSuccessful, v1alpha1.AnalysisStatusSuccessful))
	assert.True(t, IsWorse(v1alpha1.AnalysisStatusSuccessful, v1alpha1.AnalysisStatusInconclusive))
	assert.True(t, IsWorse(v1alpha1.AnalysisStatusSuccessful, v1alpha1.AnalysisStatusError))
	assert.True(t, IsWorse(v1alpha1.AnalysisStatusSuccessful, v1alpha1.AnalysisStatusFailed))

	assert.False(t, IsWorse(v1alpha1.AnalysisStatusInconclusive, v1alpha1.AnalysisStatusSuccessful))
	assert.False(t, IsWorse(v1alpha1.AnalysisStatusInconclusive, v1alpha1.AnalysisStatusInconclusive))
	assert.True(t, IsWorse(v1alpha1.AnalysisStatusInconclusive, v1alpha1.AnalysisStatusError))
	assert.True(t, IsWorse(v1alpha1.AnalysisStatusInconclusive, v1alpha1.AnalysisStatusFailed))

	assert.False(t, IsWorse(v1alpha1.AnalysisStatusError, v1alpha1.AnalysisStatusError))
	assert.False(t, IsWorse(v1alpha1.AnalysisStatusError, v1alpha1.AnalysisStatusSuccessful))
	assert.False(t, IsWorse(v1alpha1.AnalysisStatusError, v1alpha1.AnalysisStatusInconclusive))
	assert.True(t, IsWorse(v1alpha1.AnalysisStatusError, v1alpha1.AnalysisStatusFailed))

	assert.False(t, IsWorse(v1alpha1.AnalysisStatusFailed, v1alpha1.AnalysisStatusSuccessful))
	assert.False(t, IsWorse(v1alpha1.AnalysisStatusFailed, v1alpha1.AnalysisStatusInconclusive))
	assert.False(t, IsWorse(v1alpha1.AnalysisStatusFailed, v1alpha1.AnalysisStatusError))
	assert.False(t, IsWorse(v1alpha1.AnalysisStatusFailed, v1alpha1.AnalysisStatusFailed))
}

func TestWorst(t *testing.T) {
	assert.Equal(t, v1alpha1.AnalysisStatusFailed, Worst(v1alpha1.AnalysisStatusSuccessful, v1alpha1.AnalysisStatusFailed))
	assert.Equal(t, v1alpha1.AnalysisStatusFailed, Worst(v1alpha1.AnalysisStatusFailed, v1alpha1.AnalysisStatusSuccessful))
}

func TestIsFastFailTerminating(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Status: v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:   "other-metric",
					Status: v1alpha1.AnalysisStatusRunning,
				},
				{
					Name:   "success-rate",
					Status: v1alpha1.AnalysisStatusRunning,
				},
			},
		},
	}
	successRate := run.Status.MetricResults[1]
	assert.False(t, IsTerminating(run))
	successRate.Status = v1alpha1.AnalysisStatusError
	run.Status.MetricResults[1] = successRate
	assert.True(t, IsTerminating(run))
	successRate.Status = v1alpha1.AnalysisStatusFailed
	run.Status.MetricResults[1] = successRate
	assert.True(t, IsTerminating(run))
	successRate.Status = v1alpha1.AnalysisStatusInconclusive
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
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:   "success-rate",
					Status: v1alpha1.AnalysisStatusRunning,
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
		Name:   "success-rate",
		Status: v1alpha1.AnalysisStatusRunning,
	}

	SetResult(run, res)
	assert.Equal(t, res, run.Status.MetricResults[0])
	res.Status = v1alpha1.AnalysisStatusFailed
	SetResult(run, res)
	assert.Equal(t, res, run.Status.MetricResults[0])
}

func TestMetricCompleted(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Status: v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:   "success-rate",
					Status: v1alpha1.AnalysisStatusRunning,
				},
			},
		},
	}
	assert.False(t, MetricCompleted(run, "non-existent"))
	assert.False(t, MetricCompleted(run, "success-rate"))

	run.Status.MetricResults[0] = v1alpha1.MetricResult{
		Name:   "success-rate",
		Status: v1alpha1.AnalysisStatusError,
	}
	assert.True(t, MetricCompleted(run, "success-rate"))
}

func TestLastMeasurement(t *testing.T) {
	m1 := v1alpha1.Measurement{
		Status: v1alpha1.AnalysisStatusSuccessful,
		Value:  "99",
	}
	m2 := v1alpha1.Measurement{
		Status: v1alpha1.AnalysisStatusSuccessful,
		Value:  "98",
	}
	run := &v1alpha1.AnalysisRun{
		Status: v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:         "success-rate",
					Status:       v1alpha1.AnalysisStatusRunning,
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
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:   "other-metric",
					Status: v1alpha1.AnalysisStatusRunning,
				},
				{
					Name:   "success-rate",
					Status: v1alpha1.AnalysisStatusRunning,
				},
			},
		},
	}
	assert.False(t, IsTerminating(run))
	run.Spec.Terminate = true
	assert.True(t, IsTerminating(run))
	run.Spec.Terminate = false
	successRate := run.Status.MetricResults[1]
	successRate.Status = v1alpha1.AnalysisStatusError
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
		AnalysisSpec: v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "success-rate",
				},
			},
		},
	}
	right := left.DeepCopy()
	assert.True(t, IsSemanticallyEqual(*left, *right))
	right.AnalysisSpec.Metrics[0].Name = "foo"
	assert.False(t, IsSemanticallyEqual(*left, *right))
}

func TestCreateWithCollisionCounterNoControlller(t *testing.T) {
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
			Status: v1alpha1.AnalysisStatusFailed,
		},
	}
	client := fake.NewSimpleClientset(&run)
	runIf := client.ArgoprojV1alpha1().AnalysisRuns(metav1.NamespaceDefault)
	logCtx := log.NewEntry(log.New())
	createdRun, err := CreateWithCollisionCounter(logCtx, runIf, run)
	assert.NoError(t, err)
	assert.Equal(t, run.Name+".1", createdRun.Name)
}
