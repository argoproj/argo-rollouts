package plugin

import (
	"path/filepath"
	"testing"

	"github.com/argoproj/argo-rollouts/utils/defaults"

	"github.com/argoproj/argo-rollouts/utils/config"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetPluginLocation(t *testing.T) {
	t.Run("tests getting plugin location of metric provider plugins", func(t *testing.T) {

		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "argo-rollouts-config",
				Namespace: "argo-rollouts",
			},
			Data: map[string]string{"metricProviderPlugins": "\n  - name: argoproj-labs/http\n    location: https://test/plugin\n  - name: argoproj-labs/http-sha\n    location: https://test/plugin\n    sha256: 74657374e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		}
		client := fake.NewSimpleClientset(cm)

		_, err := config.InitializeConfig(client, "argo-rollouts-config")
		assert.NoError(t, err)

		location, err := GetPluginLocation("argoproj-labs/http")
		assert.NoError(t, err)
		fp, err := filepath.Abs(filepath.Join(defaults.DefaultRolloutPluginFolder, "argoproj-labs/http"))
		assert.NoError(t, err)
		assert.Equal(t, fp, location)
	})

	t.Run("tests getting plugin location of traffic router plugins", func(t *testing.T) {

		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "argo-rollouts-config",
				Namespace: "argo-rollouts",
			},
			Data: map[string]string{"trafficRouterPlugins": "\n  - name: argoproj-labs/router\n    location: https://test/plugin\n  - name: argoproj-labs/router-sha\n    location: https://test/plugin\n    sha256: 74657374e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		}
		client := fake.NewSimpleClientset(cm)

		_, err := config.InitializeConfig(client, "argo-rollouts-config")
		assert.NoError(t, err)

		location, err := GetPluginLocation("argoproj-labs/router")
		assert.NoError(t, err)
		fp, err := filepath.Abs(filepath.Join(defaults.DefaultRolloutPluginFolder, "argoproj-labs/router"))
		assert.NoError(t, err)
		assert.Equal(t, fp, location)
	})

	t.Run("test getting plugin location of a plugin that does not exists", func(t *testing.T) {
		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "argo-rollouts-config",
				Namespace: "argo-rollouts",
			},
			Data: map[string]string{"metricProviderPlugins": "\n  - name: argoproj-labs/http\n    location: https://test/plugin\n  - name: argoproj-labs/http-sha\n    location: https://test/plugin\n    sha256: 74657374e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		}
		client := fake.NewSimpleClientset(cm)

		_, err := config.InitializeConfig(client, "argo-rollouts-config")
		assert.NoError(t, err)

		location, err := GetPluginLocation("does-not-exist")
		assert.Error(t, err)
		assert.Equal(t, "plugin does-not-exist not configured in configmap", err.Error())
		assert.Equal(t, "", location)
	})
}
