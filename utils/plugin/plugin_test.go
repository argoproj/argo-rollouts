package plugin

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/tj/assert"
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

	err := InitMetricsPlugin("file://./plugin.go", fd, "")
	assert.NoError(t, err)

	err = InitMetricsPlugin("file://plugin.go", fd, "")
	assert.NoError(t, err)

	err = InitMetricsPlugin("file://./file-not-found.go", fd, "")
	assert.Error(t, err)

	err = InitMetricsPlugin("file://file-not-found.go", fd, "")
	assert.Error(t, err)

	err = InitMetricsPlugin("file://./../../Dockerfile", fd, "")
	assert.NoError(t, err)

	f, err := filepath.Abs("plugin.go")
	if err != nil {
		t.Fatal(err)
	}
	err = InitMetricsPlugin("file://"+f, fd, "")
	assert.NoError(t, err)

	f, err = filepath.Abs("file-not-found.go")
	if err != nil {
		t.Fatal(err)
	}
	err = InitMetricsPlugin("file://"+f, fd, "")
	assert.Error(t, err)

	err = InitMetricsPlugin("http://mock-host:8080/plugin", fd, "74657374e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	assert.NoError(t, err)

	err = InitMetricsPlugin("http://mock-host:8080/plugin", fd, "bad-sha")
	assert.Error(t, err)

	err = InitMetricsPlugin("", fd, "bad-sha")
	assert.NoError(t, err)

	err = InitMetricsPlugin("*(&^%$#@!", fd, "bad-sha")
	assert.Error(t, err)

	os.Remove(defaults.DefaultPluginHttpFileLocation)
}
