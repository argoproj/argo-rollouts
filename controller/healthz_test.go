package controller

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHealthzServer(t *testing.T) {
	expectedResponse := `ok`

	addr := fmt.Sprintf("0.0.0.0:%d", DefaultHealthzPort)
	healthzServ := NewHealthzServer(addr)

	t.Helper()
	req, err := http.NewRequest("GET", "/healthz", nil)
	assert.NoError(t, err)
	rr := httptest.NewRecorder()
	healthzServ.Handler.ServeHTTP(rr, req)
	assert.Equal(t, rr.Code, http.StatusOK)
	body := rr.Body.String()
	log.Println(body)
	for _, line := range strings.Split(expectedResponse, "\n") {
		assert.Contains(t, body, line)
	}

	req, err = http.NewRequest("GET", "/extraneousPath", nil)
	assert.NoError(t, err)
	rr = httptest.NewRecorder()
	healthzServ.Handler.ServeHTTP(rr, req)
	assert.Equal(t, rr.Code, http.StatusNotFound)
}
