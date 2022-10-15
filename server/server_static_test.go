package server

import (
	"embed"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tj/assert"
)

const TestRootPath = "/test-root"

var (
	//go:embed static_test/*
	staticTestData embed.FS //nolint
	mockServer     ArgoRolloutsServer
)

func init() {
	static = staticTestData
	staticBasePath = "static_test"
	indexHtmlFile = staticBasePath + "/index.html"
	mockServer = mockArgoRolloutServer()
}

func TestIndexHtmlIsServed(t *testing.T) {
	tests := []struct {
		requestPath string
	}{
		{TestRootPath + "/"},
		{TestRootPath + "/index.html"},
		{TestRootPath + "/nonsense/../index.html"},
		{TestRootPath + "/test-dir/test.css"},
	}
	for _, test := range tests {
		t.Run(test.requestPath, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.requestPath, nil)
			w := httptest.NewRecorder()
			mockServer.staticFileHttpHandler(w, req)
			res := w.Result()
			defer res.Body.Close()
			data, err := io.ReadAll(res.Body)
			assert.NoError(t, err)
			assert.Equal(t, res.StatusCode, http.StatusOK)
			if strings.HasSuffix(test.requestPath, ".css") {
				assert.Equal(t, res.Header.Get(ContentType), mime.TypeByExtension(".css"))
				assert.Contains(t, string(data), "empty by intent")
			} else {
				assert.Equal(t, res.Header.Get(ContentType), mime.TypeByExtension(".html"))
				assert.Contains(t, string(data), "<title>index-title</title>")
			}
		})
	}
}

func TestWhenFileNotFoundSendIndexPageForUiReactRouter(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, TestRootPath+"/namespace-default", nil)
	w := httptest.NewRecorder()
	mockServer.staticFileHttpHandler(w, req)
	res := w.Result()
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	assert.NoError(t, err)
	assert.Equal(t, res.StatusCode, http.StatusOK)
	assert.Contains(t, string(data), "<title>index-title</title>")
}

func TestSlashWillBeRedirectedToRootPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	mockServer.staticFileHttpHandler(w, req)
	res := w.Result()
	defer res.Body.Close()
	_, err := io.ReadAll(res.Body)
	assert.NoError(t, err)
	assert.Equal(t, res.StatusCode, http.StatusFound)
	assert.Contains(t, res.Header.Get("Location"), TestRootPath)
}

func TestInvalidFilesOrHackingAttemptReturn404(t *testing.T) {
	tests := []struct {
		requestPath string
	}{
		{"/index.html"}, // should fail, because not prefixed with Option.RootPath
		{"/etc/passwd"},
		{TestRootPath + "/../etc/passwd"},
		{TestRootPath + "/../../etc/passwd"},
		{TestRootPath + "/../../../etc/passwd"},
	}
	for _, test := range tests {
		t.Run(test.requestPath, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.requestPath, nil)
			w := httptest.NewRecorder()
			mockServer.staticFileHttpHandler(w, req)
			res := w.Result()
			defer res.Body.Close()
			assert.Equal(t, res.StatusCode, http.StatusNotFound)
		})
	}
}

func mockArgoRolloutServer() ArgoRolloutsServer {
	s := ArgoRolloutsServer{
		Options: ServerOptions{
			RootPath: TestRootPath,
		},
	}
	return s
}
