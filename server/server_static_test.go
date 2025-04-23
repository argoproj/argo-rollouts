package server

import (
	"embed"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/tj/assert"
)

const TestRootPath = "/test-root"

var (
	//go:embed static_test/*
	staticTestData   embed.FS //nolint
	mockServerPrefix ArgoRolloutsServer
	mockServerRoot   ArgoRolloutsServer
)

func init() {
	static = staticTestData
	staticBasePath = "static_test"
	indexHtmlFile = staticBasePath + "/index.html"
	mockServerPrefix = mockArgoRolloutServer(TestRootPath)
	mockServerRoot = mockArgoRolloutServer("/")
}

func TestIndexHtmlIsServed(t *testing.T) {
	tests := []struct {
		server      ArgoRolloutsServer
		requestPath string
	}{
		{mockServerPrefix, TestRootPath + "/"},
		{mockServerPrefix, TestRootPath + "/index.html"},
		{mockServerPrefix, TestRootPath + "/nonsense/../index.html"},
		{mockServerPrefix, TestRootPath + "/test-dir/test.css"},

		{mockServerRoot, "/"},
		{mockServerRoot, "/index.html"},
		{mockServerRoot, "/nonsense/../index.html"},
		{mockServerRoot, "/test-dir/test.css"},
	}
	for _, test := range tests {
		t.Run(test.requestPath, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, test.requestPath, nil)
			w := httptest.NewRecorder()
			test.server.staticFileHttpHandler(w, req)
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
	mockServerPrefix.staticFileHttpHandler(w, req)
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
	mockServerPrefix.staticFileHttpHandler(w, req)
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
			mockServerPrefix.staticFileHttpHandler(w, req)
			res := w.Result()
			defer res.Body.Close()
			assert.Equal(t, res.StatusCode, http.StatusNotFound)
		})
	}
}

func TestCacheControlHeader(t *testing.T) {
	tests := []struct {
		envValue       string
		expectedHeader string
	}{
		{"", ""}, // Default behavior: no Cache-Control header
		{"public, max-age=3600", "public, max-age=3600"}, // Custom Cache-Control header
	}

	for _, test := range tests {
		t.Run(test.envValue, func(t *testing.T) {
			os.Setenv("CACHE_CONTROL_HEADER", test.envValue)
			req := httptest.NewRequest(http.MethodGet, TestRootPath+"/main.css", nil)
			w := httptest.NewRecorder()
			mockServerPrefix.staticFileHttpHandler(w, req)
			res := w.Result()
			defer res.Body.Close()
			assert.Equal(t, test.expectedHeader, res.Header.Get("Cache-Control"))
		})
	}
}

func mockArgoRolloutServer(rootPath string) ArgoRolloutsServer {
	s := ArgoRolloutsServer{
		Options: ServerOptions{
			RootPath: rootPath,
		},
	}
	return s
}
