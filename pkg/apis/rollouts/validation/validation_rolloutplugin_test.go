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

func TestValidateRolloutPlugin_TimeoutConfig(t *testing.T) {
	t.Run("valid positive timeoutSeconds in plugin config", func(t *testing.T) {
		rp := validRolloutPlugin()
		rp.Spec.Plugin.Config = []byte(`{"timeoutSeconds": 300, "timeoutAbort": true}`)
		assert.Equal(t, "", ValidateRolloutPlugin(rp))
	})

	t.Run("zero timeoutSeconds is rejected", func(t *testing.T) {
		rp := validRolloutPlugin()
		rp.Spec.Plugin.Config = []byte(`{"timeoutSeconds": 0}`)
		assert.Equal(t, "RolloutPlugin spec.plugin.config.timeoutSeconds must be greater than 0", ValidateRolloutPlugin(rp))
	})

	t.Run("negative timeoutSeconds is rejected", func(t *testing.T) {
		rp := validRolloutPlugin()
		rp.Spec.Plugin.Config = []byte(`{"timeoutSeconds": -5}`)
		assert.Equal(t, "RolloutPlugin spec.plugin.config.timeoutSeconds must be greater than 0", ValidateRolloutPlugin(rp))
	})

	t.Run("malformed plugin config is rejected", func(t *testing.T) {
		rp := validRolloutPlugin()
		rp.Spec.Plugin.Config = []byte(`{not json`)
		assert.Contains(t, ValidateRolloutPlugin(rp), "spec.plugin.config is not valid")
	})

	t.Run("absent config is valid (default timeout applies)", func(t *testing.T) {
		rp := validRolloutPlugin()
		assert.Equal(t, "", ValidateRolloutPlugin(rp))
	})
}
