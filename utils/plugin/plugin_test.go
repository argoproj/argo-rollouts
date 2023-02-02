package plugin

import (
	"github.com/argoproj/argo-rollouts/utils/config"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"testing"
)

func TestGetPluginLocationNotInit(t *testing.T) {
	_, err := GetPluginLocation("http")
	assert.Error(t, err)
}

func TestGetPluginLocation(t *testing.T) {
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argo-rollouts-config",
			Namespace: "argo-rollouts",
		},
		Data: map[string]string{"plugins": "metrics:\n  - name: http\n    pluginLocation: https://test/plugin\n  - name: http-sha\n    pluginLocation: https://test/plugin\n    pluginSha256: 74657374e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	}
	client := fake.NewSimpleClientset(cm)

	_, err := config.InitializeConfig(client, "argo-rollouts-config")
	assert.NoError(t, err)

	location, err := GetPluginLocation("http")
	assert.NoError(t, err)
	assert.Equal(t, "/Users/zaller/Development/argo-rollouts/utils/plugin/plugin-bin/http", location)
}
