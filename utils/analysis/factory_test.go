package analysis

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/utils/pointer"
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
			{
				Name: "metadata.labels['app']",
				ValueFrom: &v1alpha1.ArgumentValueFrom{
					FieldRef: &v1alpha1.FieldRef{FieldPath: "metadata.labels['app']"},
				},
			},
			{
				Name: "metadata.labels['env']",
				ValueFrom: &v1alpha1.ArgumentValueFrom{
					FieldRef: &v1alpha1.FieldRef{FieldPath: "metadata.labels['env']"},
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

	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			UID:       uuid.NewUUID(),
			Name:      "test",
			Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{
				annotations.RevisionAnnotation: "1",
			},
			Labels: map[string]string{
				"app": "app",
				"env": "test",
			},
		},
		Spec: v1alpha1.RolloutSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "app",
						"env": "test",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container-name",
							Image: "foo/bar",
						},
					},
				},
			},
			RevisionHistoryLimit: nil,
			Replicas:             func() *int32 { i := int32(1); return &i }(),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"app": "app",
				"env": "test",
			}},
		},
		Status: v1alpha1.RolloutStatus{},
	}

	args := BuildArgumentsForRolloutAnalysisRun(rolloutAnalysis.Args, stableRS, newRS, ro)
	assert.Contains(t, args, v1alpha1.Argument{Name: "hard-coded-value-key", Value: pointer.StringPtr("hard-coded-value")})
	assert.Contains(t, args, v1alpha1.Argument{Name: "stable-key", Value: pointer.StringPtr("abcdef")})
	assert.Contains(t, args, v1alpha1.Argument{Name: "new-key", Value: pointer.StringPtr("123456")})
	assert.Contains(t, args, v1alpha1.Argument{Name: "metadata.labels['app']", Value: pointer.StringPtr("app")})
	assert.Contains(t, args, v1alpha1.Argument{Name: "metadata.labels['env']", Value: pointer.StringPtr("test")})

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
		}
		err := ValidateMetrics(spec.Metrics)
		assert.EqualError(t, err, "metrics[0]: count must be >= failureLimit")
		count = intstr.FromInt(0)
		spec.Metrics[0].Count = &count
		err = ValidateMetrics(spec.Metrics)
		assert.NoError(t, err)
	})
	t.Run("Ensure count must be >= inconclusiveLimit", func(t *testing.T) {
		inconclusiveLimit := intstr.FromInt(2)
		count := intstr.FromInt(1)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:              "success-rate",
					Count:             &count,
					InconclusiveLimit: &inconclusiveLimit,
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{},
					},
				},
			},
		}
		err := ValidateMetrics(spec.Metrics)
		assert.EqualError(t, err, "metrics[0]: count must be >= inconclusiveLimit")
		count = intstr.FromInt(0)
		spec.Metrics[0].Count = &count
		err = ValidateMetrics(spec.Metrics)
		assert.NoError(t, err)
	})
	t.Run("Validate metric", func(t *testing.T) {
		failureLimit := intstr.FromInt(2)
		count := intstr.FromInt(2)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					Count:        &count,
					Interval:     "60s",
					FailureLimit: &failureLimit,
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
		failureLimit := intstr.FromInt(2)
		count := intstr.FromInt(2)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					Count:        &count,
					Interval:     "60s-typo",
					FailureLimit: &failureLimit,
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
		failureLimit := intstr.FromInt(2)
		count := intstr.FromInt(2)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					Count:        &count,
					Interval:     "60s",
					InitialDelay: "60s-typo",
					FailureLimit: &failureLimit,
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
		count := intstr.FromInt(2)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:  "success-rate",
					Count: &count,
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
		failureLimit := intstr.FromInt(-1)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					FailureLimit: &failureLimit,
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
		inconclusiveLimit := intstr.FromInt(-1)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:              "success-rate",
					InconclusiveLimit: &inconclusiveLimit,
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
		errorLimit := intstr.FromInt(-1)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:                  "success-rate",
					ConsecutiveErrorLimit: &errorLimit,
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
		count := intstr.FromInt(1)
		spec := v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:  "success-rate",
					Count: &count,
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
						Datadog:    &v1alpha1.DatadogMetric{},
						NewRelic:   &v1alpha1.NewRelicMetric{},
					},
				},
			},
		}
		err := ValidateMetrics(spec.Metrics)
		assert.EqualError(t, err, "metrics[0]: multiple providers specified")
	})
}

// TestResolveMetricArgs verifies that metric arguments are resolved
func TestResolveMetricArgs(t *testing.T) {
	arg1, arg2 := "success-rate", "success-rate2"
	args := []v1alpha1.Argument{
		{
			Name:  "metric-name",
			Value: &arg1,
		},
		{
			Name:  "metric-name2",
			Value: &arg2,
		},
	}
	metric1 := v1alpha1.Metric{Name: "metric-name", SuccessCondition: "result > {{args.metric-name}}"}
	metric2 := v1alpha1.Metric{Name: "metric-name2", SuccessCondition: "result < {{args.metric-name2}}"}
	newMetric1, _ := ResolveMetricArgs(metric1, args)
	newMetric2, _ := ResolveMetricArgs(metric2, args)
	assert.Equal(t, fmt.Sprintf("result > %s", arg1), newMetric1.SuccessCondition)
	assert.Equal(t, fmt.Sprintf("result < %s", arg2), newMetric2.SuccessCondition)
}

//TestResolveMetricArgsWithQuotes verifies that metric arguments with quotes are resolved
func TestResolveMetricArgsWithQuotes(t *testing.T) {
	arg := "foo \"bar\" baz"

	arguments := []v1alpha1.Argument{{
		Name:  "rate",
		Value: &arg,
	}}
	metric := v1alpha1.Metric{
		Name:             "rate",
		SuccessCondition: "{{args.rate}}",
	}
	newMetric, err := ResolveMetricArgs(metric, arguments)
	assert.NoError(t, err)
	assert.Equal(t, fmt.Sprintf(arg), newMetric.SuccessCondition)
}

func TestResolveMetrics(t *testing.T) {
	count, failureLimit := "5", "1"
	args := []v1alpha1.Argument{
		{
			Name:  "count",
			Value: &count,
		},
		{
			Name:  "failure-limit",
			Value: &failureLimit,
		},
		{
			Name: "secret",
			ValueFrom: &v1alpha1.ValueFrom{
				SecretKeyRef: &v1alpha1.SecretKeyRef{
					Name: "web-metric-secret",
					Key:  "apikey",
				},
			},
		},
	}

	countVal := intstr.FromString("{{args.count}}")
	failureLimitVal := intstr.FromString("{{args.failure-limit}}")
	metrics := []v1alpha1.Metric{{
		Name:         "metric-name",
		Count:        &countVal,
		FailureLimit: &failureLimitVal,
	}}

	resolvedMetrics, err := ResolveMetrics(metrics, args)
	assert.Nil(t, err)
	assert.Equal(t, count, resolvedMetrics[0].Count.String())
	assert.Equal(t, failureLimit, resolvedMetrics[0].FailureLimit.String())
}
