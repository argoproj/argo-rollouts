package controller

import (
	"fmt"
	"net/http"
)

const (
	// HealthzPath is the endpoint to probe if controller is running
	HealthzPath = "/healthz"
)

type healthzHandler struct{}

func (h *healthzHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	fmt.Fprintf(w, "ok")
}

func NewHealthzServer(addr string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle(HealthzPath, &healthzHandler{})

	return &http.Server{
		Addr:    addr,
		Handler: mux,
	}
}
