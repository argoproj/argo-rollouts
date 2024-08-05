package controller

import (
	"fmt"
	"net/http"
	"net/http/pprof"
)

const (
	ProfilingPath = "/debug/pprof"
)

// NewPProfServer returns a new pprof server to gather runtime profiling data
func NewPProfServer() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc(ProfilingPath, pprof.Index)
	mux.HandleFunc(fmt.Sprintf("%s/cmdline", ProfilingPath), pprof.Cmdline)
	mux.HandleFunc(fmt.Sprintf("%s/profile", ProfilingPath), pprof.Profile)
	mux.HandleFunc(fmt.Sprintf("%s/symbol", ProfilingPath), pprof.Symbol)
	mux.HandleFunc(fmt.Sprintf("%s/trace", ProfilingPath), pprof.Trace)

	return mux
}
