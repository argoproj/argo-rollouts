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
	rolloutAnalysis := &v1alpha1.RolloutAnalysis{
		Args: []v1alpha1.AnalysisRunArgument{
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
	args := BuildArgumentsForRolloutAnalysisRun(rolloutAnalysis.Args, stableRS, newRS)
	assert.Contains(t, args, v1alpha1.Argument{Name: "hard-coded-value-key", Value: pointer.StringPtr("hard-coded-value")})
	assert.Contains(t, args, v1alpha1.Argument{Name: "stable-key", Value: pointer.StringPtr("abcdef")})
	assert.Contains(t, args, v1alpha1.Argument{Name: "new-key", Value: pointer.StringPtr("123456")})

}

func TestPrePromotionLabels(t *testing.T) {
	podHash := "abcd123"
	expected := map[string]string{
		v1alpha1.LabelKeyControllerInstanceID: "test",
		v1alpha1.RolloutTypeLabel:             v1alpha1.RolloutTypePrePromotionLabel,
		v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
	}
	generated := PrePromotionLabels(podHash, "test")
	assert.Equal(t, expected, generated)
}

func TestPostPromotionLabels(t *testing.T) {
	podHash := "abcd123"
	expected := map[string]string{
		v1alpha1.LabelKeyControllerInstanceID: "test",
		v1alpha1.RolloutTypeLabel:             v1alpha1.RolloutTypePostPromotionLabel,
		v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
	}
	generated := PostPromotionLabels(podHash, "test")
	assert.Equal(t, expected, generated)
}

func TestStepLabels(t *testing.T) {
	podHash := "abcd123"
	expected := map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
		v1alpha1.RolloutTypeLabel:             v1alpha1.RolloutTypeStepLabel,
		v1alpha1.RolloutCanaryStepIndexLabel:  "1",
		v1alpha1.LabelKeyControllerInstanceID: "test",
	}
	generated := StepLabels(1, podHash, "test")
	assert.Equal(t, expected, generated)
}

func TestBackgroundLabels(t *testing.T) {
	podHash := "abcd123"
	expected := map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
		v1alpha1.RolloutTypeLabel:             v1alpha1.RolloutTypeBackgroundRunLabel,
		v1alpha1.LabelKeyControllerInstanceID: "test",
	}
	generated := BackgroundLabels(podHash, "test")
	assert.Equal(t, expected, generated)
}

func TestValidateMetrics(t *testing.T) {
	t.Run("Ensure count >= failureLimit", func(t *testing.T) {
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					Count:        1,
					FailureLimit: 2,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
		}
		err := ValidateMetrics(spec.Metrics)
		assert.EqualError(t, err, "metrics[0]: count must be >= failureLimit")
		spec.Metrics[0].Count = 0
		err = ValidateMetrics(spec.Metrics)
		assert.NoError(t, err)
	})
	t.Run("Ensure count must be >= inconclusiveLimit", func(t *testing.T) {
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:              "success-rate",
					Count:             1,
					InconclusiveLimit: 2,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
		}
		err := ValidateMetrics(spec.Metrics)
		assert.EqualError(t, err, "metrics[0]: count must be >= inconclusiveLimit")
		spec.Metrics[0].Count = 0
		err = ValidateMetrics(spec.Metrics)
		assert.NoError(t, err)
	})
	t.Run("Validate metric", func(t *testing.T) {
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					Count:        2,
					Interval:     "60s",
					FailureLimit: 2,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
		}
		err := ValidateMetrics(spec.Metrics)
		assert.NoError(t, err)
	})
	t.Run("Ensure valid internal string", func(t *testing.T) {
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					Count:        2,
					Interval:     "60s-typo",
					FailureLimit: 2,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
		}
		err := ValidateMetrics(spec.Metrics)
		assert.Regexp(t, `metrics\[0\]: invalid interval string: time: unknown unit (")?s-typo(")? in duration (")?60s-typo(")?`, err)
	})
	t.Run("Ensure valid initialDelay string", func(t *testing.T) {
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					Count:        2,
					Interval:     "60s",
					InitialDelay: "60s-typo",
					FailureLimit: 2,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
		}
		err := ValidateMetrics(spec.Metrics)
		assert.Regexp(t, `metrics\[0\]: invalid startDelay string: time: unknown unit (")?s-typo(")? in duration (")?60s-typo(")?`, err)
	})
	t.Run("Ensure metric provider listed", func(t *testing.T) {
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{},
		}
		err := ValidateMetrics(spec.Metrics)
		assert.EqualError(t, err, "no metrics specified")
	})
	t.Run("Ensure interval set when count > 1", func(t *testing.T) {
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
		err := ValidateMetrics(spec.Metrics)
		assert.EqualError(t, err, "metrics[0]: interval must be specified when count > 1")
	})
	t.Run("Ensure no duplicate metric names", func(t *testing.T) {
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
		err := ValidateMetrics(spec.Metrics)
		assert.EqualError(t, err, "metrics[1]: duplicate name 'success-rate")
	})
	t.Run("Ensure failureLimit >= 0", func(t *testing.T) {
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					FailureLimit: -1,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
		}
		err := ValidateMetrics(spec.Metrics)
		assert.EqualError(t, err, "metrics[0]: failureLimit must be >= 0")
	})
	t.Run("Ensure inconclusiveLimit >= 0", func(t *testing.T) {
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:              "success-rate",
					InconclusiveLimit: -1,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
		}
		err := ValidateMetrics(spec.Metrics)
		assert.EqualError(t, err, "metrics[0]: inconclusiveLimit must be >= 0")
	})
	t.Run("Ensure consecutiveErrorLimit >= 0", func(t *testing.T) {
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:                  "success-rate",
					ConsecutiveErrorLimit: pointer.Int32Ptr(-1),
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
		}
		err := ValidateMetrics(spec.Metrics)
		assert.EqualError(t, err, "metrics[0]: consecutiveErrorLimit must be >= 0")
	})
	t.Run("Ensure metric has provider", func(t *testing.T) {
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:  "success-rate",
					Count: 1,
				},
			},
		}
		err := ValidateMetrics(spec.Metrics)
		assert.EqualError(t, err, "metrics[0]: no provider specified")
	})
	t.Run("Ensure metric does not have more than 1 provider", func(t *testing.T) {
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "success-rate",
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
						Job:        &v1alpha1.JobMetric{},
						Wavefront:  &v1alpha1.WavefrontMetric{},
						Kayenta:    &v1alpha1.KayentaMetric{},
						Web:        &v1alpha1.WebMetric{},
					},
				},
			},
		}
		err := ValidateMetrics(spec.Metrics)
		assert.EqualError(t, err, "metrics[0]: multiple providers specified")
	})
}
