package prometheus

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestSigV4Service(t *testing.T) {
	const (
		ampAddress = "https://aps-workspaces.us-east-1.amazonaws.com/workspaces/ws-1234"
		cwAddress  = "https://monitoring.us-east-1.amazonaws.com"
		localAddr  = "http://prometheus.monitoring.svc:9090"
	)

	tests := []struct {
		name        string
		metric      v1alpha1.PrometheusMetric
		wantService string
		wantSign    bool
	}{
		{
			name:     "no sigv4 config, AMP address",
			metric:   v1alpha1.PrometheusMetric{Address: ampAddress},
			wantSign: false,
		},
		{
			name: "sigv4 config, AMP address defaults to aps",
			metric: v1alpha1.PrometheusMetric{
				Address:        ampAddress,
				Authentication: v1alpha1.Authentication{Sigv4: v1alpha1.Sigv4Config{Region: "us-east-1"}},
			},
			wantService: "aps",
			wantSign:    true,
		},
		{
			name: "sigv4 config, non-AMP address does not sign by default",
			metric: v1alpha1.PrometheusMetric{
				Address:        localAddr,
				Authentication: v1alpha1.Authentication{Sigv4: v1alpha1.Sigv4Config{Region: "us-east-1"}},
			},
			wantSign: false,
		},
		{
			name: "explicit service, CloudWatch address",
			metric: v1alpha1.PrometheusMetric{
				Address:        cwAddress,
				Authentication: v1alpha1.Authentication{Sigv4: v1alpha1.Sigv4Config{Region: "us-east-1", Service: "monitoring"}},
			},
			wantService: "monitoring",
			wantSign:    true,
		},
		{
			name: "explicit service wins over AMP address heuristic",
			metric: v1alpha1.PrometheusMetric{
				Address:        ampAddress,
				Authentication: v1alpha1.Authentication{Sigv4: v1alpha1.Sigv4Config{Service: "monitoring"}},
			},
			wantService: "monitoring",
			wantSign:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metric := tt.metric
			service, ok := sigV4Service(&metric)
			assert.Equal(t, tt.wantSign, ok)
			assert.Equal(t, tt.wantService, service)
		})
	}
}

type captureRoundTripper struct {
	req *http.Request
}

func (c *captureRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	c.req = r
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
}

// TestSigV4RoundTripperSignsForService verifies that the configured AWS service name
// flows into the SigV4 credential scope, which is what allows targeting CloudWatch
// ("monitoring") in addition to Amazon Managed Prometheus ("aps").
func TestSigV4RoundTripperSignsForService(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")

	for _, service := range []string{"aps", "monitoring"} {
		t.Run(service, func(t *testing.T) {
			capture := &captureRoundTripper{}
			rt, err := newSigV4RoundTripper(v1alpha1.Sigv4Config{Region: "us-east-1"}, service, capture)
			require.NoError(t, err)

			req, err := http.NewRequest(http.MethodPost, "https://example.com/api/v1/query", strings.NewReader("query=up"))
			require.NoError(t, err)

			_, err = rt.RoundTrip(req)
			require.NoError(t, err)

			auth := capture.req.Header.Get("Authorization")
			assert.Contains(t, auth, "/us-east-1/"+service+"/aws4_request",
				"credential scope should carry the configured service name")

			// The signed request must expose a re-readable body (GetBody) so the
			// transport can replay it on redirects/retries.
			require.NotNil(t, capture.req.GetBody)
			bodyCopy, err := capture.req.GetBody()
			require.NoError(t, err)
			replayed, err := io.ReadAll(bodyCopy)
			require.NoError(t, err)
			assert.Equal(t, "query=up", string(replayed))
		})
	}
}

func TestSigV4RoundTripperRequiresRegion(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	_, err := newSigV4RoundTripper(v1alpha1.Sigv4Config{Service: "monitoring"}, "monitoring", nil)
	assert.ErrorContains(t, err, "region not configured")
}

// errReadCloser always fails on Read, to exercise the request-body read error path.
type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) { return 0, fmt.Errorf("boom") }
func (errReadCloser) Close() error             { return nil }

func TestNewSigV4RoundTripperInvalidProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_PROFILE", "")

	// A profile that cannot be resolved makes LoadDefaultConfig fail, exercising both
	// the profile option and the config-load error path.
	_, err := newSigV4RoundTripper(v1alpha1.Sigv4Config{Region: "us-east-1", Profile: "does-not-exist"}, "aps", nil)
	assert.Error(t, err)
}

func TestNewSigV4RoundTripperWithRoleARN(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")

	// RoleARN wires up an assume-role credential provider (evaluated lazily), which
	// must succeed at construction time.
	rt, err := newSigV4RoundTripper(v1alpha1.Sigv4Config{Region: "us-east-1", RoleARN: "arn:aws:iam::123456789012:role/example"}, "aps", nil)
	require.NoError(t, err)
	assert.NotNil(t, rt)
}

func TestSigV4RoundTripperNoCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true") // avoid an IMDS lookup that would slow the test

	rt, err := newSigV4RoundTripper(v1alpha1.Sigv4Config{Region: "us-east-1"}, "aps", &captureRoundTripper{})
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, "https://example.com/api/v1/query?query=up", nil)
	require.NoError(t, err)

	_, err = rt.RoundTrip(req)
	assert.ErrorContains(t, err, "SigV4 credentials")
}

func TestSigV4RoundTripperBodyReadError(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")

	rt, err := newSigV4RoundTripper(v1alpha1.Sigv4Config{Region: "us-east-1"}, "aps", &captureRoundTripper{})
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, "https://example.com/api/v1/query", errReadCloser{})
	require.NoError(t, err)

	_, err = rt.RoundTrip(req)
	assert.Error(t, err)
}

func TestNewPrometheusAPIWithSigV4(t *testing.T) {
	t.Setenv(EnvVarArgoRolloutsPrometheusAddress, "")
	t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")

	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Address:        "https://aps-workspaces.us-east-1.amazonaws.com/workspaces/ws-123",
				Authentication: v1alpha1.Authentication{Sigv4: v1alpha1.Sigv4Config{Region: "us-east-1"}},
			},
		},
	}
	api, err := NewPrometheusAPI(metric)
	require.NoError(t, err)
	assert.NotNil(t, api)
}

func TestNewPrometheusAPISigV4Error(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvVarArgoRolloutsPrometheusAddress, "")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_PROFILE", "")

	metric := v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Prometheus: &v1alpha1.PrometheusMetric{
				Address:        "https://monitoring.us-east-1.amazonaws.com",
				Authentication: v1alpha1.Authentication{Sigv4: v1alpha1.Sigv4Config{Service: "monitoring"}},
			},
		},
	}
	_, err := NewPrometheusAPI(metric)
	assert.ErrorContains(t, err, "region not configured")
}
