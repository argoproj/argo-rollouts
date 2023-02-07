package plugin

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/argoproj/argo-rollouts/utils/config"

	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/tj/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type MockFileDownloader struct {
	FileDownloader
}

func (m MockFileDownloader) Get(url string) (*http.Response, error) {
	responseBody := io.NopCloser(bytes.NewReader([]byte(`test`)))
	return &http.Response{
		Status:        "200",
		StatusCode:    200,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        nil,
		Body:          responseBody,
		ContentLength: 4,
	}, nil
}

func TestNotInitialized(t *testing.T) {
	_, err := config.GetConfig()
	assert.Error(t, err)
}

func TestInitPlugin(t *testing.T) {
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argo-rollouts-config",
			Namespace: "argo-rollouts",
		},
		Data: map[string]string{"plugins": "metrics:\n  - name: http\n    pluginLocation: https://test/plugin\n  - name: http-sha\n    pluginLocation: https://test/plugin\n    pluginSha256: 74657374e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	}
	client := fake.NewSimpleClientset(cm)

	config.UnInitializeConfig()

	_, err := config.InitializeConfig(client, "argo-rollouts-config")
	assert.NoError(t, err)

	err = DownloadPlugins(MockFileDownloader{})
	assert.NoError(t, err)

	filepath.Join(defaults.DefaultRolloutPluginFolder, "http")
	err = os.Remove(filepath.Join(defaults.DefaultRolloutPluginFolder, "http"))
	assert.NoError(t, err)
	err = os.Remove(filepath.Join(defaults.DefaultRolloutPluginFolder, "http-sha"))
	assert.NoError(t, err)
	err = os.RemoveAll(defaults.DefaultRolloutPluginFolder)
	assert.NoError(t, err)
}

func TestInitPluginBadSha(t *testing.T) {
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.DefaultRolloutsConfigMapName,
			Namespace: defaults.Namespace(),
		},
		Data: map[string]string{"plugins": "metrics:\n  - name: http-badsha\n    pluginLocation: https://test/plugin\n    pluginSha256: badsha352"},
	}
	client := fake.NewSimpleClientset(cm)

	config.UnInitializeConfig()

	_, err := config.InitializeConfig(client, defaults.DefaultRolloutsConfigMapName)
	assert.NoError(t, err)

	err = DownloadPlugins(MockFileDownloader{})
	assert.Error(t, err)

	err = os.Remove(filepath.Join(defaults.DefaultRolloutPluginFolder, "http-badsha"))
	assert.NoError(t, err)
	err = os.RemoveAll(defaults.DefaultRolloutPluginFolder)
	assert.NoError(t, err)
}

func TestInitPluginConfigNotFound(t *testing.T) {
	client := fake.NewSimpleClientset()

	config.UnInitializeConfig()

	cm, err := config.InitializeConfig(client, defaults.DefaultRolloutsConfigMapName)
	assert.NoError(t, err)
	assert.Equal(t, cm, &config.Config{})

	err = DownloadPlugins(MockFileDownloader{})
	assert.NoError(t, err)
	err = os.RemoveAll(defaults.DefaultRolloutPluginFolder)
	assert.NoError(t, err)
}

func TestFileMove(t *testing.T) {
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.DefaultRolloutsConfigMapName,
			Namespace: defaults.Namespace(),
		},
		Data: map[string]string{"plugins": "metrics:\n  - name: file-plugin\n    pluginLocation: file://./plugin.go"},
	}
	client := fake.NewSimpleClientset(cm)

	config.UnInitializeConfig()

	_, err := config.InitializeConfig(client, defaults.DefaultRolloutsConfigMapName)
	assert.NoError(t, err)

	err = DownloadPlugins(MockFileDownloader{})
	assert.NoError(t, err)

	err = os.Remove(filepath.Join(defaults.DefaultRolloutPluginFolder, "file-plugin"))
	assert.NoError(t, err)
	err = os.RemoveAll(defaults.DefaultRolloutPluginFolder)
	assert.NoError(t, err)
}

func TestDoubleInit(t *testing.T) {
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.DefaultRolloutsConfigMapName,
			Namespace: defaults.Namespace(),
		},
		Data: map[string]string{"plugins": "metrics:\n  - name: file-plugin\n    pluginLocation: file://./plugin.go"},
	}
	client := fake.NewSimpleClientset(cm)

	config.UnInitializeConfig()

	_, err := config.InitializeConfig(client, defaults.DefaultRolloutsConfigMapName)
	assert.NoError(t, err)

	_, err = config.InitializeConfig(client, defaults.DefaultRolloutsConfigMapName)
	assert.NoError(t, err)

	err = DownloadPlugins(MockFileDownloader{})
	assert.NoError(t, err)

	err = os.Remove(filepath.Join(defaults.DefaultRolloutPluginFolder, "file-plugin"))
	assert.NoError(t, err)
	err = os.RemoveAll(defaults.DefaultRolloutPluginFolder)
	assert.NoError(t, err)
}

func TestBadConfigMap(t *testing.T) {
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argo-rollouts-config",
			Namespace: "argo-rollouts",
		},
		Data: map[string]string{"plugins": "badconfigmap"},
	}
	client := fake.NewSimpleClientset(cm)

	config.UnInitializeConfig()

	_, err := config.InitializeConfig(client, "argo-rollouts-config")
	assert.Error(t, err)

	err = DownloadPlugins(MockFileDownloader{})
	assert.Error(t, err)
}

func TestBadLocation(t *testing.T) {
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argo-rollouts-config",
			Namespace: "argo-rollouts",
		},
		Data: map[string]string{"plugins": "metrics:\n  - name: http\n    pluginLocation: agwegasdlkjf2324"},
	}
	client := fake.NewSimpleClientset(cm)

	config.UnInitializeConfig()

	_, err := config.InitializeConfig(client, "argo-rollouts-config")
	assert.NoError(t, err)

	err = DownloadPlugins(MockFileDownloader{})
	assert.Error(t, err)

	err = os.RemoveAll(defaults.DefaultRolloutPluginFolder)
	assert.NoError(t, err)
}

func TestCheckPluginExits(t *testing.T) {
	err := checkPluginExists("nonexistentplugin")
	assert.Error(t, err)

	realfile, err := filepath.Abs("plugin.go")
	assert.NoError(t, err)
	err = checkPluginExists(realfile)
	assert.NoError(t, err)
}

func TestCheckShaOfPlugin(t *testing.T) {
	_, err := checkShaOfPlugin("nonexistentplugin", "")
	assert.Error(t, err)

	realfile, err := filepath.Abs("plugin.go")
	assert.NoError(t, err)
	_, err = checkShaOfPlugin(realfile, "")
	assert.NoError(t, err)
}

func TestDownloadFile(t *testing.T) {
	err := downloadFile("error", "", FileDownloaderImpl{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to download file from")
}
