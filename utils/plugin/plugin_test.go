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

func TestGetPluginInfo(t *testing.T) {
	cmdArgs := []string{"-l 2"}
	t.Run("tests getting plugin location of metric provider plugins", func(t *testing.T) {

		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "argo-rollouts-config",
				Namespace: "argo-rollouts",
			},
			Data: map[string]string{"metricProviderPlugins": "\n  - name: argoproj-labs/http\n    location: https://test/plugin\n    args: [\"-l 2\"]\n  - name: argoproj-labs/http-sha\n    location: https://test/plugin\n    sha256: 74657374e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		}
		client := fake.NewSimpleClientset(cm)

		_, err := config.InitializeConfig(client, "argo-rollouts-config")
		assert.NoError(t, err)

		location, args, err := GetPluginInfo("argoproj-labs/http")
		assert.NoError(t, err)
		fp, err := filepath.Abs(filepath.Join(defaults.DefaultRolloutPluginFolder, "argoproj-labs/http"))
		assert.NoError(t, err)
		assert.Equal(t, fp, location)
		assert.Equal(t, args, cmdArgs)

		_, args, _ = GetPluginInfo("argoproj-labs/http-sha")
		assert.Equal(t, len(args), 0)
	})

	t.Run("tests getting plugin location of traffic router plugins", func(t *testing.T) {

		cm := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "argo-rollouts-config",
				Namespace: "argo-rollouts",
			},
			Data: map[string]string{"trafficRouterPlugins": "\n  - name: argoproj-labs/router\n    location: https://test/plugin\n    args: [\"-l 2\"]\n  - name: argoproj-labs/router-sha\n    location: https://test/plugin\n    sha256: 74657374e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		}
		client := fake.NewSimpleClientset(cm)

		_, err := config.InitializeConfig(client, "argo-rollouts-config")
		assert.NoError(t, err)

		location, args, err := GetPluginInfo("argoproj-labs/router")
		assert.NoError(t, err)
		fp, err := filepath.Abs(filepath.Join(defaults.DefaultRolloutPluginFolder, "argoproj-labs/router"))
		assert.NoError(t, err)
		assert.Equal(t, fp, location)
		assert.Equal(t, args, cmdArgs)

		_, args, _ = GetPluginInfo("argoproj-labs/router-sha")
		assert.Equal(t, len(args), 0)
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

		location, args, err := GetPluginInfo("does-not-exist")
		assert.Error(t, err)
		assert.Equal(t, "plugin does-not-exist not configured in configmap", err.Error())
		assert.Equal(t, "", location)
		assert.Equal(t, len(args), 0)
	})
}
