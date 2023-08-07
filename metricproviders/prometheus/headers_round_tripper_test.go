package prometheus

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/prometheus/client_golang/api"
	"github.com/stretchr/testify/assert"
)

type testHttpHeaderRoundTripper struct {
	expectedHeaders []v1alpha1.WebMetricHeader
	t               *testing.T
}

func (rt *testHttpHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	assert.Equal(rt.t, fromWebMetricHeadersToHttpHeaders(rt.expectedHeaders), req.Header)

	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewBufferString("ahoj")),
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
	}, nil
}

func fromWebMetricHeadersToHttpHeaders(headers []v1alpha1.WebMetricHeader) http.Header {
	header := http.Header{}
	for _, h := range headers {
		header.Add(h.Key, h.Value)
	}

	return header
}

func Test_httpHeadersRoundTripper_RoundTrip(t *testing.T) {
	tests := []struct {
		name            string
		initialHeaders  []v1alpha1.WebMetricHeader
		appendedHeaders []v1alpha1.WebMetricHeader
		expectedHeaders []v1alpha1.WebMetricHeader
	}{
		{
			name: "have header and append header",
			initialHeaders: []v1alpha1.WebMetricHeader{
				{Key: "header1", Value: "value1"},
			},
			appendedHeaders: []v1alpha1.WebMetricHeader{
				{Key: "appendedHeader", Value: "appendedHeaderValue"},
			},
			expectedHeaders: []v1alpha1.WebMetricHeader{
				{Key: "appendedHeader", Value: "appendedHeaderValue"},
				{Key: "header1", Value: "value1"},
			},
		},
		{
			name:           "only append header",
			initialHeaders: []v1alpha1.WebMetricHeader{},
			appendedHeaders: []v1alpha1.WebMetricHeader{
				{Key: "appendedHeader", Value: "appendedHeaderValue"},
			},
			expectedHeaders: []v1alpha1.WebMetricHeader{
				{Key: "appendedHeader", Value: "appendedHeaderValue"},
			},
		},
		{
			name: "have header and not append header",
			initialHeaders: []v1alpha1.WebMetricHeader{
				{Key: "header1", Value: "value1"},
			},
			appendedHeaders: []v1alpha1.WebMetricHeader{},
			expectedHeaders: []v1alpha1.WebMetricHeader{
				{Key: "header1", Value: "value1"},
			},
		},
		{
			name:            "empty headers",
			initialHeaders:  []v1alpha1.WebMetricHeader{},
			appendedHeaders: []v1alpha1.WebMetricHeader{},
			expectedHeaders: []v1alpha1.WebMetricHeader{},
		},
		{
			name:           "append multiple headers",
			initialHeaders: []v1alpha1.WebMetricHeader{},
			appendedHeaders: []v1alpha1.WebMetricHeader{
				{Key: "appendedHeader1", Value: "appendedHeaderValue1"},
				{Key: "appendedHeader2", Value: "appendedHeaderValue2"},
			},
			expectedHeaders: []v1alpha1.WebMetricHeader{
				{Key: "appendedHeader1", Value: "appendedHeaderValue1"},
				{Key: "appendedHeader2", Value: "appendedHeaderValue2"},
			},
		},
		{
			name: "overwrite header",
			initialHeaders: []v1alpha1.WebMetricHeader{
				{Key: "header", Value: "value"},
			},
			appendedHeaders: []v1alpha1.WebMetricHeader{
				{Key: "header", Value: "newValue"},
			},
			expectedHeaders: []v1alpha1.WebMetricHeader{
				{Key: "header", Value: "newValue"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := httpHeadersRoundTripper{
				headers: tt.appendedHeaders,
				roundTripper: &testHttpHeaderRoundTripper{
					expectedHeaders: tt.expectedHeaders,
					t:               t,
				},
			}

			c, err := api.NewClient(api.Config{
				Address:      "http://fake-address",
				RoundTripper: rt,
			})
			if err != nil {
				t.Fatal(err)
			}
			r := &http.Request{
				URL:    &url.URL{Scheme: "http", Host: "fake-host", Path: "/"},
				Header: fromWebMetricHeadersToHttpHeaders(tt.initialHeaders),
			}
			if _, _, err = c.Do(context.Background(), r); err != nil {
				t.Fatal(err)
			}

		})
	}
}
