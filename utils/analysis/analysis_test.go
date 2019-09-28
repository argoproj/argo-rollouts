package analysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestValidateMetrics(t *testing.T) {
	{
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:        "success-rate",
					Count:       1,
					MaxFailures: 2,
				},
			},
		}
		err := ValidateAnalysisTemplateSpec(spec)
		assert.EqualError(t, err, "metrics[0]: count must be >= maxFailures")
	}
	{
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:        "success-rate",
					Count:       2,
					Interval:    pointer.Int32Ptr(60),
					MaxFailures: 2,
				},
			},
		}
		err := ValidateAnalysisTemplateSpec(spec)
		assert.NoError(t, err)
	}
	{
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{},
		}
		err := ValidateAnalysisTemplateSpec(spec)
		assert.EqualError(t, err, "no metrics specified")
	}
	{
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:  "success-rate",
					Count: 2,
				},
			},
		}
		err := ValidateAnalysisTemplateSpec(spec)
		assert.EqualError(t, err, "metrics[0]: interval must be specified when count > 1")
	}
	{
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "success-rate",
				},
				{
					Name: "success-rate",
				},
			},
		}
		err := ValidateAnalysisTemplateSpec(spec)
		assert.EqualError(t, err, "metrics[1]: duplicate name 'success-rate")
	}
	{
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:        "success-rate",
					MaxFailures: -1,
				},
			},
		}
		err := ValidateAnalysisTemplateSpec(spec)
		assert.EqualError(t, err, "metrics[0]: maxFailures must be >= 0")
	}
	{
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:                 "success-rate",
					MaxConsecutiveErrors: pointer.Int32Ptr(-1),
				},
			},
		}
		err := ValidateAnalysisTemplateSpec(spec)
		assert.EqualError(t, err, "metrics[0]: maxConsecutiveErrors must be >= 0")
	}
}

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

func TestIsFailing(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Status: &v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: map[string]v1alpha1.MetricResult{
				"other-metric": {
					Status: v1alpha1.AnalysisStatusRunning,
				},
				"success-rate": {
					Status: v1alpha1.AnalysisStatusRunning,
				},
			},
		},
	}
	successRate := run.Status.MetricResults["success-rate"]
	assert.False(t, IsFailing(run))
	successRate.Status = v1alpha1.AnalysisStatusError
	run.Status.MetricResults["success-rate"] = successRate
	assert.True(t, IsFailing(run))
	successRate.Status = v1alpha1.AnalysisStatusFailed
	run.Status.MetricResults["success-rate"] = successRate
	assert.True(t, IsFailing(run))
}

func TestMetricResult(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Status: &v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: map[string]v1alpha1.MetricResult{
				"success-rate": {
					Status: v1alpha1.AnalysisStatusRunning,
				},
			},
		},
	}
	assert.Nil(t, MetricResult(run, "non-existent"))
	assert.Equal(t, run.Status.MetricResults["success-rate"], *MetricResult(run, "success-rate"))
}

func TestMetricCompleted(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Status: &v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: map[string]v1alpha1.MetricResult{
				"success-rate": {
					Status: v1alpha1.AnalysisStatusRunning,
				},
			},
		},
	}
	assert.False(t, MetricCompleted(run, "non-existent"))
	assert.False(t, MetricCompleted(run, "success-rate"))

	run.Status.MetricResults["success-rate"] = v1alpha1.MetricResult{
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
		Status: &v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: map[string]v1alpha1.MetricResult{
				"success-rate": {
					Status:       v1alpha1.AnalysisStatusRunning,
					Measurements: []v1alpha1.Measurement{m1, m2},
				},
			},
		},
	}
	assert.Nil(t, LastMeasurement(run, "non-existent"))
	assert.Equal(t, m2, *LastMeasurement(run, "success-rate"))
	successRate := run.Status.MetricResults["success-rate"]
	successRate.Measurements = []v1alpha1.Measurement{}
	run.Status.MetricResults["success-rate"] = successRate
	assert.Nil(t, LastMeasurement(run, "success-rate"))
}

func TestIsTerminating(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Status: &v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: map[string]v1alpha1.MetricResult{
				"other-metric": {
					Status: v1alpha1.AnalysisStatusRunning,
				},
				"success-rate": {
					Status: v1alpha1.AnalysisStatusRunning,
				},
			},
		},
	}
	assert.False(t, IsTerminating(run))
	run.Spec.Terminate = true
	assert.True(t, IsTerminating(run))
	run.Spec.Terminate = false
	successRate := run.Status.MetricResults["success-rate"]
	successRate.Status = v1alpha1.AnalysisStatusError
	run.Status.MetricResults["success-rate"] = successRate
	assert.True(t, IsTerminating(run))
}

func TestConsecutiveErrors(t *testing.T) {
	{
		result := v1alpha1.MetricResult{
			Measurements: []v1alpha1.Measurement{},
		}
		assert.Equal(t, 0, ConsecutiveErrors(result))
	}
	{
		result := v1alpha1.MetricResult{
			Measurements: []v1alpha1.Measurement{
				{
					Status: v1alpha1.AnalysisStatusError,
				},
				{
					Status: v1alpha1.AnalysisStatusSuccessful,
				},
				{
					Status: v1alpha1.AnalysisStatusError,
				},
			},
		}
		assert.Equal(t, 1, ConsecutiveErrors(result))
	}
	{
		result := v1alpha1.MetricResult{
			Measurements: []v1alpha1.Measurement{
				{
					Status: v1alpha1.AnalysisStatusError,
				},
				{
					Status: v1alpha1.AnalysisStatusSuccessful,
				},
			},
		}
		assert.Equal(t, 0, ConsecutiveErrors(result))
	}
	{
		result := v1alpha1.MetricResult{
			Measurements: []v1alpha1.Measurement{
				{
					Status: v1alpha1.AnalysisStatusError,
				},
				{
					Status: v1alpha1.AnalysisStatusError,
				},
			},
		}
		assert.Equal(t, 2, ConsecutiveErrors(result))
	}
}
