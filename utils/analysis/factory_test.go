package analysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestBuildArgumentsForRolloutAnalysisRun(t *testing.T) {
	new := v1alpha1.Latest
	stable := v1alpha1.Stable
	rolloutAnalysisStep := &v1alpha1.RolloutAnalysisStep{
		Arguments: []v1alpha1.AnalysisRunArgument{
			{
				Name:  "hard-coded-value-key",
				Value: "hard-coded-value",
			},
			{
				Name: "stable-key",
				ValueFrom: &v1alpha1.ArgumentValueFrom{
					PodTemplateHashValue: &stable,
				},
			},
			{
				Name: "new-key",
				ValueFrom: &v1alpha1.ArgumentValueFrom{
					PodTemplateHashValue: &new,
				},
			},
		},
	}
	stableRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "stable-rs",
			Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "abcdef"},
		},
	}
	newRS := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "new-rs",
			Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "123456"},
		},
	}
	args := BuildArgumentsForRolloutAnalysisRun(rolloutAnalysisStep, stableRS, newRS)
	assert.Contains(t, args, v1alpha1.Argument{Name: "hard-coded-value-key", Value: "hard-coded-value"})
	assert.Contains(t, args, v1alpha1.Argument{Name: "stable-key", Value: "abcdef"})
	assert.Contains(t, args, v1alpha1.Argument{Name: "new-key", Value: "123456"})

}

func TestStepLabels(t *testing.T) {
	podHash := "abcd123"
	expected := map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
		v1alpha1.RolloutTypeLabel:             v1alpha1.RolloutTypeStepLabel,
		v1alpha1.RolloutCanaryStepIndexLabel:  "1",
	}
	generated := StepLabels(1, podHash)
	assert.Equal(t, expected, generated)
}

func TestBackgroundLabels(t *testing.T) {
	podHash := "abcd123"
	expected := map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
		v1alpha1.RolloutTypeLabel:             v1alpha1.RolloutTypeBackgroundRunLabel,
	}
	generated := BackgroundLabels(podHash)
	assert.Equal(t, expected, generated)
}

func TestValidateMetrics(t *testing.T) {
	{
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:        "success-rate",
					Count:       1,
					MaxFailures: 2,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
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
					Name:            "success-rate",
					Count:           1,
					MaxInconclusive: 2,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
		}
		err := ValidateAnalysisTemplateSpec(spec)
		assert.EqualError(t, err, "metrics[0]: count must be >= maxInconclusive")
	}
	{
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:        "success-rate",
					Count:       2,
					Interval:    pointer.Int32Ptr(60),
					MaxFailures: 2,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
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
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
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
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
				{
					Name: "success-rate",
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
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
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
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
					Name:            "success-rate",
					MaxInconclusive: -1,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
		}
		err := ValidateAnalysisTemplateSpec(spec)
		assert.EqualError(t, err, "metrics[0]: maxInconclusive must be >= 0")
	}
	{
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:                 "success-rate",
					MaxConsecutiveErrors: pointer.Int32Ptr(-1),
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
		}
		err := ValidateAnalysisTemplateSpec(spec)
		assert.EqualError(t, err, "metrics[0]: maxConsecutiveErrors must be >= 0")
	}
	{
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:  "success-rate",
					Count: 1,
				},
			},
		}
		err := ValidateAnalysisTemplateSpec(spec)
		assert.EqualError(t, err, "metrics[0]: no provider specified")
	}
	{
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "success-rate",
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
						Job:        &v1alpha1.JobMetric{},
					},
				},
			},
		}
		err := ValidateAnalysisTemplateSpec(spec)
		assert.EqualError(t, err, "metrics[0]: multiple providers specified")
	}
}
