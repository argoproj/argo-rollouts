package prometheus

import "net/http"

type httpHeadersRoundTripper struct {
	headers      map[string]string
	roundTripper http.RoundTripper
}

// RoundTrip implements the http.RoundTripper interface.
func (h httpHeadersRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	for k, v := range h.headers {
		r.Header.Set(k, v)
	}

	return h.roundTripper.RoundTrip(r)
}
