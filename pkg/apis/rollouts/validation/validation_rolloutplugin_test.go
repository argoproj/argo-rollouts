package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func validRolloutPlugin() *v1alpha1.RolloutPlugin {
	return &v1alpha1.RolloutPlugin{
		Spec: v1alpha1.RolloutPluginSpec{
			WorkloadRef: v1alpha1.WorkloadRef{
				APIVersion: "apps/v1",
				Kind:       "StatefulSet",
				Name:       "my-app",
			},
			Plugin: v1alpha1.PluginStep{Name: "argoproj/statefulset"},
			Strategy: v1alpha1.RolloutPluginStrategy{
				Canary: &v1alpha1.PluginCanaryStrategy{},
			},
		},
	}
}

func TestValidateRolloutPlugin_TimeoutSeconds(t *testing.T) {
	deadline := func(v int32) *int32 { return &v }

	t.Run("valid positive timeoutSeconds", func(t *testing.T) {
		rp := validRolloutPlugin()
		rp.Spec.TimeoutSeconds = deadline(300)
		rp.Spec.TimeoutAbort = true
		assert.Equal(t, "", ValidateRolloutPlugin(rp))
	})

	t.Run("zero timeoutSeconds is rejected", func(t *testing.T) {
		rp := validRolloutPlugin()
		rp.Spec.TimeoutSeconds = deadline(0)
		assert.Equal(t, "RolloutPlugin spec.timeoutSeconds must be greater than 0", ValidateRolloutPlugin(rp))
	})

	t.Run("negative timeoutSeconds is rejected", func(t *testing.T) {
		rp := validRolloutPlugin()
		rp.Spec.TimeoutSeconds = deadline(-5)
		assert.Equal(t, "RolloutPlugin spec.timeoutSeconds must be greater than 0", ValidateRolloutPlugin(rp))
	})

	t.Run("plugin config is opaque to validation", func(t *testing.T) {
		rp := validRolloutPlugin()
		rp.Spec.Plugin.Config = []byte(`{"anything": {"nested": true}}`)
		assert.Equal(t, "", ValidateRolloutPlugin(rp))
	})

	t.Run("absent timeoutSeconds is valid (default applies)", func(t *testing.T) {
		rp := validRolloutPlugin()
		assert.Equal(t, "", ValidateRolloutPlugin(rp))
	})
}

func TestValidateAnalysisTemplateForRolloutPlugin_Args(t *testing.T) {
	latest := v1alpha1.Latest
	templates := func(args ...v1alpha1.AnalysisRunArgument) AnalysisTemplatesWithType {
		count := intstr.FromInt32(1)
		return AnalysisTemplatesWithType{
			AnalysisTemplates: []*v1alpha1.AnalysisTemplate{{
				ObjectMeta: metav1.ObjectMeta{Name: "hash-check"},
				Spec: v1alpha1.AnalysisTemplateSpec{
					Args:    []v1alpha1.Argument{{Name: "hash"}},
					Metrics: []v1alpha1.Metric{{Name: "m", Count: &count, Provider: v1alpha1.MetricProvider{Job: &v1alpha1.JobMetric{}}}},
				},
			}},
			TemplateType:    InlineAnalysis,
			CanaryStepIndex: 0,
			Args:            args,
		}
	}

	t.Run("valueFrom satisfies a required arg", func(t *testing.T) {
		tmpl := templates(v1alpha1.AnalysisRunArgument{
			Name:      "hash",
			ValueFrom: &v1alpha1.ArgumentValueFrom{PodTemplateHashValue: &latest},
		})
		assert.Empty(t, ValidateAnalysisTemplateForRolloutPlugin(validRolloutPlugin(), tmpl))
	})

	t.Run("literal value satisfies a required arg", func(t *testing.T) {
		tmpl := templates(v1alpha1.AnalysisRunArgument{Name: "hash", Value: "abc"})
		assert.Empty(t, ValidateAnalysisTemplateForRolloutPlugin(validRolloutPlugin(), tmpl))
	})

	t.Run("missing required arg is rejected", func(t *testing.T) {
		errs := ValidateAnalysisTemplateForRolloutPlugin(validRolloutPlugin(), templates())
		if assert.Len(t, errs, 1) {
			assert.Contains(t, errs[0].Detail, "args.hash was not resolved")
		}
	})
}
