package controller

import (
	"net/http"
	"net/http/pprof"
)

// NewPProfServer returns a new pprof server to gather runtime profiling data
func NewPProfServer() *http.ServeMux {
	mux := http.NewServeMux()

	// TODO: Remove enumerating all pprof endpoints if/when a more ergonomic
	// attachment solution is introduced. See: https://github.com/golang/go/issues/71213.
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline/", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile/", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol/", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace/", pprof.Trace)

	return mux
}
