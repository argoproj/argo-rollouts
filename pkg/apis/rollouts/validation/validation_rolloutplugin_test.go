package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"

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
