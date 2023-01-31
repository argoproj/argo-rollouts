package config

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/tj/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
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

func TestInitPlugin(t *testing.T) {
	fd := &MockFileDownloader{}
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argo-rollouts-config",
			Namespace: "argo-rollouts",
		},
		Data: map[string]string{"plugins": "metrics:\n  - name: http\n    pluginLocation: https://test/plugin\n  - name: http-sha\n    pluginLocation: https://test/plugin\n    pluginSha256: 74657374e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	}
	client := fake.NewSimpleClientset(cm)
	i := informers.NewSharedInformerFactory(client, 0)
	i.Start(make(chan struct{}))
	cmi := i.Core().V1().ConfigMaps()
	go cmi.Informer().Run(make(chan struct{}))

	err := i.Core().V1().ConfigMaps().Informer().GetIndexer().Add(cm)
	assert.NoError(t, err)

	_, err = InitializeConfig(i.Core().V1().ConfigMaps(), "argo-rollouts-config", fd)
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
	fd := &MockFileDownloader{}
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.DefaultRolloutsConfigMapName,
			Namespace: defaults.Namespace(),
		},
		Data: map[string]string{"plugins": "metrics:\n  - name: http-badsha\n    pluginLocation: https://test/plugin\n    pluginSha256: badsha352"},
	}
	client := fake.NewSimpleClientset(cm)
	i := informers.NewSharedInformerFactory(client, 0)
	i.Start(make(chan struct{}))
	cmi := i.Core().V1().ConfigMaps()
	go cmi.Informer().Run(make(chan struct{}))

	err := i.Core().V1().ConfigMaps().Informer().GetIndexer().Add(cm)
	assert.NoError(t, err)

	_, err = InitializeConfig(i.Core().V1().ConfigMaps(), defaults.DefaultRolloutsConfigMapName, fd)
	assert.Error(t, err)

	err = os.Remove(filepath.Join(defaults.DefaultRolloutPluginFolder, "http-badsha"))
	assert.NoError(t, err)
	err = os.RemoveAll(defaults.DefaultRolloutPluginFolder)
	assert.NoError(t, err)
}

func TestInitPluginConfigNotFound(t *testing.T) {
	fd := &MockFileDownloader{}
	client := fake.NewSimpleClientset()
	i := informers.NewSharedInformerFactory(client, 0)
	i.Start(make(chan struct{}))
	cmi := i.Core().V1().ConfigMaps()
	go cmi.Informer().Run(make(chan struct{}))

	cm, err := InitializeConfig(i.Core().V1().ConfigMaps(), defaults.DefaultRolloutsConfigMapName, fd)
	assert.NoError(t, err)
	assert.Nil(t, cm)
}

func TestFileMove(t *testing.T) {
	fd := &MockFileDownloader{}
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.DefaultRolloutsConfigMapName,
			Namespace: defaults.Namespace(),
		},
		Data: map[string]string{"plugins": "metrics:\n  - name: file-plugin\n    pluginLocation: file://./config.go"},
	}
	client := fake.NewSimpleClientset(cm)
	i := informers.NewSharedInformerFactory(client, 0)
	i.Start(make(chan struct{}))
	cmi := i.Core().V1().ConfigMaps()
	go cmi.Informer().Run(make(chan struct{}))

	err := i.Core().V1().ConfigMaps().Informer().GetIndexer().Add(cm)
	assert.NoError(t, err)

	_, err = InitializeConfig(i.Core().V1().ConfigMaps(), defaults.DefaultRolloutsConfigMapName, fd)
	assert.NoError(t, err)

	err = os.Remove(filepath.Join(defaults.DefaultRolloutPluginFolder, "file-plugin"))
	assert.NoError(t, err)
	err = os.RemoveAll(defaults.DefaultRolloutPluginFolder)
	assert.NoError(t, err)
}

func TestDoubleInit(t *testing.T) {
	fd := &MockFileDownloader{}
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.DefaultRolloutsConfigMapName,
			Namespace: defaults.Namespace(),
		},
		Data: map[string]string{"plugins": "metrics:\n  - name: file-plugin\n    pluginLocation: file://./config.go"},
	}
	client := fake.NewSimpleClientset(cm)
	i := informers.NewSharedInformerFactory(client, 0)
	i.Start(make(chan struct{}))
	cmi := i.Core().V1().ConfigMaps()
	go cmi.Informer().Run(make(chan struct{}))

	err := i.Core().V1().ConfigMaps().Informer().GetIndexer().Add(cm)
	assert.NoError(t, err)

	_, err = InitializeConfig(i.Core().V1().ConfigMaps(), defaults.DefaultRolloutsConfigMapName, fd)
	assert.NoError(t, err)

	_, err = InitializeConfig(i.Core().V1().ConfigMaps(), defaults.DefaultRolloutsConfigMapName, fd)
	assert.NoError(t, err)

	err = os.Remove(filepath.Join(defaults.DefaultRolloutPluginFolder, "file-plugin"))
	assert.NoError(t, err)
	err = os.RemoveAll(defaults.DefaultRolloutPluginFolder)
	assert.NoError(t, err)
}

func TestNotInitialized(t *testing.T) {
	configMemoryCache = nil
	_, err := GetConfig()
	assert.Error(t, err)
}

func TestBadConfigMap(t *testing.T) {
	fd := &MockFileDownloader{}
	cm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "argo-rollouts-config",
			Namespace: "argo-rollouts",
		},
		Data: map[string]string{"plugins": "badconfigmap"},
	}
	client := fake.NewSimpleClientset(cm)
	i := informers.NewSharedInformerFactory(client, 0)
	i.Start(make(chan struct{}))
	cmi := i.Core().V1().ConfigMaps()
	go cmi.Informer().Run(make(chan struct{}))

	err := i.Core().V1().ConfigMaps().Informer().GetIndexer().Add(cm)
	assert.NoError(t, err)

	_, err = InitializeConfig(i.Core().V1().ConfigMaps(), "argo-rollouts-config", fd)
	assert.Error(t, err)
}
