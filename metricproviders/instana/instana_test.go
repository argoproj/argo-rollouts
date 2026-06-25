package instana

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}

func newInstanaMetric(metricType, metricID string) v1alpha1.MetricProvider {
	return v1alpha1.MetricProvider{
		Instana: &v1alpha1.InstanaMetric{
			MetricType:     metricType,
			MetricID:       metricID,
			Aggregation:    "mean",
			RollupInterval: 60,
		},
	}
}

func newInstanaMetricWithSecret(metricType, metricID, secretName string, namespaced bool) v1alpha1.MetricProvider {
	return v1alpha1.MetricProvider{
		Instana: &v1alpha1.InstanaMetric{
			MetricType:     metricType,
			MetricID:       metricID,
			Aggregation:    "mean",
			RollupInterval: 60,
			SecretRef: v1alpha1.InstanaSecretRef{
				Name:       secretName,
				Namespaced: namespaced,
			},
		},
	}
}

func buildSecret(serverURL string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: InstanaTokensSecretName,
		},
		Data: map[string][]byte{
			InstanaAddress:  []byte(serverURL),
			InstanaAPIToken: []byte("test-api-token"),
		},
	}
}

func applicationResponse(metricID string, value float64) string {
	v := value
	resp := map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"metrics": map[string]interface{}{
					metricID: [][2]interface{}{{1698000000000.0, v}},
				},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

func emptyApplicationResponse() string {
	resp := map[string]interface{}{
		"items": []interface{}{},
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

// ---------------------------------------------------------------------------
// Provider type test
// ---------------------------------------------------------------------------

func TestProviderType(t *testing.T) {
	p := Provider{}
	assert.Equal(t, ProviderType, p.Type())
}

func TestGetMetadata(t *testing.T) {
	p := Provider{}
	assert.Nil(t, p.GetMetadata(v1alpha1.Metric{}))
}

// ---------------------------------------------------------------------------
// validateMetric
// ---------------------------------------------------------------------------

func TestValidateMetric(t *testing.T) {
	tests := []struct {
		name        string
		metric      *v1alpha1.InstanaMetric
		expectError bool
		errorMsg    string
	}{
		{
			name:        "missing metricId",
			metric:      &v1alpha1.InstanaMetric{MetricType: MetricTypeApplication},
			expectError: true,
			errorMsg:    "metricId is required",
		},
		{
			name:        "missing metricType",
			metric:      &v1alpha1.InstanaMetric{MetricID: "calls.latency.p99"},
			expectError: true,
			errorMsg:    "metricType is required",
		},
		{
			name:        "invalid metricType",
			metric:      &v1alpha1.InstanaMetric{MetricID: "calls.latency.p99", MetricType: "unknown"},
			expectError: true,
			errorMsg:    "invalid",
		},
		{
			name:        "valid application metric",
			metric:      &v1alpha1.InstanaMetric{MetricID: "calls.latency.p99", MetricType: MetricTypeApplication},
			expectError: false,
		},
		{
			name:        "valid infrastructure metric",
			metric:      &v1alpha1.InstanaMetric{MetricID: "cpu.user", MetricType: MetricTypeInfrastructure},
			expectError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMetric(tc.metric)
			if tc.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Run — application metrics (happy path + error paths)
// ---------------------------------------------------------------------------

func TestRunApplicationMetrics(t *testing.T) {
	const metricID = "calls.latency.p99"

	tests := []struct {
		name              string
		serverStatus      int
		serverResponse    string
		successCondition  string
		failureCondition  string
		expectedPhase     v1alpha1.AnalysisPhase
		expectedValue     string
		expectedErrSubstr string
	}{
		{
			name:             "success: value satisfies successCondition",
			serverStatus:     http.StatusOK,
			serverResponse:   applicationResponse(metricID, 42.5),
			successCondition: "result < 100",
			expectedPhase:    v1alpha1.AnalysisPhaseSuccessful,
			expectedValue:    "42.5",
		},
		{
			name:             "failure: value violates successCondition",
			serverStatus:     http.StatusOK,
			serverResponse:   applicationResponse(metricID, 200.0),
			successCondition: "result < 100",
			failureCondition: "result >= 100",
			expectedPhase:    v1alpha1.AnalysisPhaseFailed,
			expectedValue:    "200",
		},
		{
			name:              "error: 401 unauthorized",
			serverStatus:      http.StatusUnauthorized,
			serverResponse:    `{"errors":["Unauthorized"]}`,
			successCondition:  "result < 100",
			expectedPhase:     v1alpha1.AnalysisPhaseError,
			expectedErrSubstr: "authentication error",
		},
		{
			name:              "error: non-2xx response",
			serverStatus:      http.StatusBadRequest,
			serverResponse:    `{"errors":["bad request"]}`,
			successCondition:  "result < 100",
			expectedPhase:     v1alpha1.AnalysisPhaseError,
			expectedErrSubstr: "non-2xx response code 400",
		},
		{
			name:             "empty result with default()",
			serverStatus:     http.StatusOK,
			serverResponse:   emptyApplicationResponse(),
			successCondition: "default(result, 0) < 100",
			expectedPhase:    v1alpha1.AnalysisPhaseSuccessful,
			expectedValue:    "[]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				// Verify auth header
				assert.Equal(t, "apiToken test-api-token", req.Header.Get("Authorization"))
				assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

				// Read and discard body
				io.ReadAll(req.Body)

				if tc.serverStatus != http.StatusOK {
					http.Error(rw, tc.serverResponse, tc.serverStatus)
					return
				}
				rw.Header().Set("Content-Type", "application/json")
				io.WriteString(rw, tc.serverResponse)
			}))
			defer server.Close()

			logCtx := *log.WithField("test", "test")
			fakeClient := k8sfake.NewSimpleClientset()
			fakeClient.PrependReactor("get", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
				return true, buildSecret(server.URL), nil
			})

			metric := v1alpha1.Metric{
				Name:             "test-metric",
				SuccessCondition: tc.successCondition,
				FailureCondition: tc.failureCondition,
				Provider:         newInstanaMetric(MetricTypeApplication, metricID),
			}

			provider, err := NewInstanaProvider(logCtx, fakeClient, "default", metric)
			assert.NoError(t, err)

			measurement := provider.Run(newAnalysisRun(), metric)

			assert.Equal(t, string(tc.expectedPhase), string(measurement.Phase))
			if tc.expectedErrSubstr != "" {
				assert.Contains(t, measurement.Message, tc.expectedErrSubstr)
			} else {
				assert.Equal(t, tc.expectedValue, measurement.Value)
				assert.NotNil(t, measurement.StartedAt)
				assert.NotNil(t, measurement.FinishedAt)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Run — infrastructure metrics
// ---------------------------------------------------------------------------

func TestRunInfrastructureMetrics(t *testing.T) {
	const metricID = "cpu.user"
	serverResponse := applicationResponse(metricID, 15.3) // same shape

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		io.WriteString(rw, serverResponse)
	}))
	defer server.Close()

	logCtx := *log.WithField("test", "test")
	fakeClient := k8sfake.NewSimpleClientset()
	fakeClient.PrependReactor("get", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, buildSecret(server.URL), nil
	})

	metric := v1alpha1.Metric{
		Name:             "cpu-usage",
		SuccessCondition: "result < 80",
		Provider:         newInstanaMetric(MetricTypeInfrastructure, metricID),
	}

	provider, err := NewInstanaProvider(logCtx, fakeClient, "default", metric)
	assert.NoError(t, err)

	measurement := provider.Run(newAnalysisRun(), metric)
	assert.Equal(t, string(v1alpha1.AnalysisPhaseSuccessful), string(measurement.Phase))
	assert.Equal(t, "15.3", measurement.Value)
}

// ---------------------------------------------------------------------------
// Credential resolution
// ---------------------------------------------------------------------------

func TestCredentialFromEnvVars(t *testing.T) {
	os.Setenv("INSTANA_ENDPOINT", "https://test.instana.io")
	os.Setenv("INSTANA_API_TOKEN", "env-api-token")
	defer func() {
		os.Unsetenv("INSTANA_ENDPOINT")
		os.Unsetenv("INSTANA_API_TOKEN")
	}()

	// Server will respond OK
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// This shouldn't be called since endpoint comes from env vars and points elsewhere
		http.Error(rw, "not called", http.StatusNotFound)
	}))
	defer server.Close()

	os.Setenv("INSTANA_ENDPOINT", server.URL)

	logCtx := *log.WithField("test", "test")
	fakeClient := k8sfake.NewSimpleClientset()
	// Secret lookup will fail (no secret), should fall through to env vars
	fakeClient.PrependReactor("get", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, nil
	})

	metric := v1alpha1.Metric{
		Provider: newInstanaMetric(MetricTypeApplication, "calls.latency.p99"),
	}

	endpoint, apiToken, err := findCredentials(logCtx, fakeClient, "default", metric)
	assert.NoError(t, err)
	assert.Equal(t, server.URL, endpoint)
	assert.Equal(t, "env-api-token", apiToken)
}

func TestCredentialFromNamespacedSecret(t *testing.T) {
	const secretName = "my-instana-secret"
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		io.WriteString(rw, applicationResponse("calls.latency.p99", 5.0))
	}))
	defer server.Close()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: "my-namespace",
		},
		Data: map[string][]byte{
			InstanaAddress:  []byte(server.URL),
			InstanaAPIToken: []byte("namespaced-token"),
		},
	}

	logCtx := *log.WithField("test", "test")
	fakeClient := k8sfake.NewSimpleClientset()
	fakeClient.PrependReactor("get", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, secret, nil
	})

	metric := v1alpha1.Metric{
		Name:             "test",
		SuccessCondition: "result < 100",
		Provider:         newInstanaMetricWithSecret(MetricTypeApplication, "calls.latency.p99", secretName, true),
	}

	provider, err := NewInstanaProvider(logCtx, fakeClient, "my-namespace", metric)
	assert.NoError(t, err)

	measurement := provider.Run(newAnalysisRun(), metric)
	assert.Equal(t, string(v1alpha1.AnalysisPhaseSuccessful), string(measurement.Phase))
	assert.Equal(t, "5", measurement.Value)
}

func TestCredentialNotFound(t *testing.T) {
	os.Unsetenv("INSTANA_ENDPOINT")
	os.Unsetenv("INSTANA_API_TOKEN")

	logCtx := *log.WithField("test", "test")
	fakeClient := k8sfake.NewSimpleClientset()
	fakeClient.PrependReactor("get", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, nil
	})

	metric := v1alpha1.Metric{
		Provider: newInstanaMetric(MetricTypeApplication, "calls.latency.p99"),
	}

	_, err := NewInstanaProvider(logCtx, fakeClient, "default", metric)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to find credentials")
}

func TestNamespacedSecretRequiresName(t *testing.T) {
	logCtx := *log.WithField("test", "test")
	fakeClient := k8sfake.NewSimpleClientset()

	metric := v1alpha1.Metric{
		Provider: newInstanaMetricWithSecret(MetricTypeApplication, "calls.latency.p99", "", true),
	}

	_, _, err := findCredentials(logCtx, fakeClient, "default", metric)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "secret name is required")
}

// ---------------------------------------------------------------------------
// Resume / Terminate / GarbageCollect no-ops
// ---------------------------------------------------------------------------

func TestNoOps(t *testing.T) {
	logCtx := *log.WithField("test", "test")
	p := &Provider{logCtx: logCtx}

	m := v1alpha1.Measurement{}
	assert.Equal(t, m, p.Resume(newAnalysisRun(), v1alpha1.Metric{}, m))
	assert.Equal(t, m, p.Terminate(newAnalysisRun(), v1alpha1.Metric{}, m))
	assert.NoError(t, p.GarbageCollect(newAnalysisRun(), v1alpha1.Metric{}, 0))
}

// ---------------------------------------------------------------------------
// envKey helper
// ---------------------------------------------------------------------------

func TestEnvKey(t *testing.T) {
	assert.Equal(t, "API_TOKEN", envKey("api-token"))
	assert.Equal(t, "ENDPOINT", envKey("endpoint"))
}
