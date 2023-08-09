package prometheus

import (
	"net/http"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

type httpHeadersRoundTripper struct {
	headers      []v1alpha1.WebMetricHeader
	roundTripper http.RoundTripper
}

// RoundTrip implements the http.RoundTripper interface.
func (h httpHeadersRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	for _, header := range h.headers {
		r.Header.Set(header.Key, header.Value)
	}

	return h.roundTripper.RoundTrip(r)
}
