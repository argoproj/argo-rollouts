package datadog

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
)

const (
	ExpectedApiKey = "0123456789abcdef0123456789abcdef"
	ExpectedAppKey = "0123456789abcdef0123456789abcdef01234567"
)

func TestRunSuite(t *testing.T) {

	unixNow = func() int64 { return 1599076435 }

	// Test Cases
	tests := []struct {
		name                    string
		serverURL               string
		webServerStatus         int
		webServerResponse       string
		metric                  v1alpha1.Metric
		expectedIntervalSeconds int64
		expectedValue           string
		expectedPhase           v1alpha1.AnalysisPhase
		expectedErrorMessage    string
		expectedErrorProvider   bool
		expectedApiKey          string
		expectedAppKey          string
		useEnvVarForKeys        bool
	}{
		// When last value of time series matches condition then succeed.
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":[{"pointlist":[[1598867910000,0.0020008318672513122],[1598867925000,0.0003332881882246533]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         ddProviderInterval10m(),
			},
			expectedIntervalSeconds: 600,
			expectedValue:           "0.0003332881882246533",
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
			expectedApiKey:          ExpectedApiKey,
			expectedAppKey:          ExpectedAppKey,
			useEnvVarForKeys:        false,
		},
		// Same test as above, but derive DD keys from env var instead of k8s secret
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":[{"pointlist":[[1598867910000,0.0020008318672513122],[1598867925000,0.0003332881882246533]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         ddProviderInterval10m(),
			},
			expectedIntervalSeconds: 600,
			expectedValue:           "0.0003332881882246533",
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
			expectedApiKey:          ExpectedApiKey,
			expectedAppKey:          ExpectedAppKey,
			useEnvVarForKeys:        true,
		},
		// When last value of time series does not match condition then fail.
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":[{"pointlist":[[1598867910000,0.0020008318672513122],[1598867925000,0.006121378742186943]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         ddProviderIntervalDefault(),
			},
			expectedIntervalSeconds: 300,
			expectedValue:           "0.006121378742186943",
			expectedPhase:           v1alpha1.AnalysisPhaseFailed,
			expectedApiKey:          ExpectedApiKey,
			expectedAppKey:          ExpectedAppKey,
			useEnvVarForKeys:        false,
		},
		// Error if the request is invalid
		{
			webServerStatus:   400,
			webServerResponse: `{"status":"error","error":"error messsage"}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         ddProviderIntervalDefault(),
			},
			expectedIntervalSeconds: 300,
			expectedPhase:           v1alpha1.AnalysisPhaseError,
			expectedApiKey:          ExpectedApiKey,
			expectedAppKey:          ExpectedAppKey,
			expectedErrorMessage:    "received non 2xx response code: 400 {\"status\":\"error\",\"error\":\"error messsage\"}",
			useEnvVarForKeys:        false,
		},
		// Error if there is an authentication issue
		{
			webServerStatus:   401,
			webServerResponse: `{"errors": ["No authenticated user."]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         ddProviderIntervalDefault(),
			},
			expectedIntervalSeconds: 300,
			expectedPhase:           v1alpha1.AnalysisPhaseError,
			expectedApiKey:          ExpectedApiKey,
			expectedAppKey:          ExpectedAppKey,
			expectedErrorMessage:    "received authentication error response code: 401 {\"errors\": [\"No authenticated user.\"]}",
			useEnvVarForKeys:        false,
		},

		// Expect success with default() and data
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":[{"pointlist":[[1598867910000,0.0020008318672513122],[1598867925000,0.006121378742186943]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "default(result, 0) < 0.05",
				Provider:         ddProviderIntervalDefault(),
			},
			expectedIntervalSeconds: 300,
			expectedValue:           "0.006121378742186943",
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
			expectedApiKey:          ExpectedApiKey,
			expectedAppKey:          ExpectedAppKey,
			useEnvVarForKeys:        false,
		},

		// Expect error with no default() and no data
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":[]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.05",
				Provider:         ddProviderIntervalDefault(),
			},
			expectedIntervalSeconds: 300,
			expectedPhase:           v1alpha1.AnalysisPhaseError,
			expectedApiKey:          ExpectedApiKey,
			expectedAppKey:          ExpectedAppKey,
			expectedErrorMessage:    `invalid operation: < (mismatched types <nil> and float64)`,
			useEnvVarForKeys:        false,
		},

		// Expect success with default() and no data
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":[]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "default(result, 0) < 0.05",
				Provider:         ddProviderIntervalDefault(),
			},
			expectedIntervalSeconds: 300,
			expectedValue:           `[]`,
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
			expectedApiKey:          ExpectedApiKey,
			expectedAppKey:          ExpectedAppKey,
			useEnvVarForKeys:        false,
		},

		// Expect failure with bad default() and no data
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":[]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "default(result, 1) < 0.05",
				Provider:         ddProviderIntervalDefault(),
			},
			expectedIntervalSeconds: 300,
			expectedValue:           `[]`,
			expectedPhase:           v1alpha1.AnalysisPhaseFailed,
			expectedApiKey:          ExpectedApiKey,
			expectedAppKey:          ExpectedAppKey,
			useEnvVarForKeys:        false,
		},

		// Expect success with bad default() and good data
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":[{"pointlist":[[1598867910000,0.0020008318672513122],[1598867925000,0.006121378742186943]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "default(result, 1) < 0.05",
				Provider:         ddProviderIntervalDefault(),
			},
			expectedIntervalSeconds: 300,
			expectedValue:           `0.006121378742186943`,
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
			expectedApiKey:          ExpectedApiKey,
			expectedAppKey:          ExpectedAppKey,
			useEnvVarForKeys:        false,
		},

		// Error if datadog returns non-array series
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":"invalid"}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         ddProviderIntervalDefault(),
			},
			expectedIntervalSeconds: 300,
			expectedPhase:           v1alpha1.AnalysisPhaseError,
			expectedApiKey:          ExpectedApiKey,
			expectedAppKey:          ExpectedAppKey,
			expectedErrorMessage:    "Could not parse JSON body: json: cannot unmarshal string into Go struct field datadogResponseV1.Series of type []struct { Pointlist [][]float64 \"json:\\\"pointlist\\\"\" }",
			useEnvVarForKeys:        false,
		},

		// Error if server address is faulty
		{
			serverURL: "://wrong.schema",
			metric: v1alpha1.Metric{
				Provider: ddProviderInterval10m(),
			},
			expectedPhase:        v1alpha1.AnalysisPhaseError,
			expectedApiKey:       ExpectedApiKey,
			expectedAppKey:       ExpectedAppKey,
			expectedErrorMessage: "parse \"://wrong.schema\": missing protocol scheme",
			useEnvVarForKeys:     false,
		},
		// When secret passed name passed in the analysis template, expect success
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":[{"pointlist":[[1598867910000,0.0020008318672513122],[1598867925000,0.0003332881882246533]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         ddProviderNamespacedSecret(),
			},
			expectedIntervalSeconds: 600,
			expectedValue:           "0.0003332881882246533",
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
			expectedApiKey:          ExpectedApiKey,
			expectedAppKey:          ExpectedAppKey,
			useEnvVarForKeys:        false,
			expectedErrorProvider:   false,
		},
		// When secretRef is namespaced but no secret is not found, expect failure
		{
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         ddProviderNamespacedTrue(),
			},
			expectedApiKey:        ExpectedApiKey,
			expectedAppKey:        ExpectedAppKey,
			useEnvVarForKeys:      false,
			expectedErrorProvider: true,
		},
		// When secretRef is namespaced but no secret is not found, expect failure ---
		{
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         ddProviderNamespacedSecret(),
			},
			useEnvVarForKeys:      false,
			expectedErrorProvider: true,
		},
	}

	// Run

	for _, test := range tests {
		serverURL := test.serverURL

		if serverURL == "" {
			// Server setup with response
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				// Check query variables
				actualQuery := req.URL.Query().Get("query")
				actualFrom := req.URL.Query().Get("from")
				actualTo := req.URL.Query().Get("to")

				if actualQuery != "avg:kubernetes.cpu.user.total{*}" {
					t.Errorf("\nquery expected avg:kubernetes.cpu.user.total{*} but got %s", actualQuery)
				}

				if from, err := strconv.ParseInt(actualFrom, 10, 64); err == nil && from != unixNow()-test.expectedIntervalSeconds {
					t.Errorf("\nfrom %d expected be equal to %d", from, unixNow()-test.expectedIntervalSeconds)
				} else if err != nil {
					t.Errorf("\nfailed to parse from: %v", err)
				}

				if to, err := strconv.ParseInt(actualTo, 10, 64); err == nil && to != unixNow() {
					t.Errorf("\nto %d was expected be equal to %d", to, unixNow())
				} else if err != nil {
					t.Errorf("\nfailed to parse to: %v", err)
				}

				// Check headers
				if req.Header.Get("Content-Type") != "application/json" {
					t.Errorf("\nContent-Type header expected to be application/json but got %s", req.Header.Get("Content-Type"))
				}
				if req.Header.Get("DD-API-KEY") != ExpectedApiKey {
					t.Errorf("\nDD-API-KEY header expected %s but got %s", ExpectedApiKey, req.Header.Get("DD-API-KEY"))
				}
				if req.Header.Get("DD-APPLICATION-KEY") != ExpectedAppKey {
					t.Errorf("\nDD-APPLICATION-KEY header expected %s but got %s", ExpectedAppKey, req.Header.Get("DD-APPLICATION-KEY"))
				}

				// Return mock response
				if test.webServerStatus < 200 || test.webServerStatus >= 300 {
					http.Error(rw, test.webServerResponse, test.webServerStatus)
				} else {
					rw.Header().Set("Content-Type", "application/json")
					io.WriteString(rw, test.webServerResponse)
				}
			}))
			defer server.Close()

			serverURL = server.URL
		}

		tokenSecret := newSecret(serverURL, test.expectedApiKey, test.expectedAppKey)

		if test.useEnvVarForKeys {
			os.Setenv("DD_API_KEY", ExpectedApiKey)
			os.Setenv("DD_APP_KEY", ExpectedAppKey)
			os.Setenv("DD_ADDRESS", serverURL)
		} else {
			os.Unsetenv("DD_API_KEY")
			os.Unsetenv("DD_APP_KEY")
			os.Unsetenv("DD_ADDRESS")
		}

		logCtx := log.WithField("test", "test")

		fakeClient := k8sfake.NewSimpleClientset()
		fakeClient.PrependReactor("get", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
			if test.useEnvVarForKeys {
				return true, nil, nil
			}
			return true, tokenSecret, nil
		})

		// Enforce these having defaults
		if test.metric.Provider.Datadog.ApiVersion == "" {
			test.metric.Provider.Datadog.ApiVersion = "v1"
		}

		if test.metric.Provider.Datadog.Interval == "" {
			test.metric.Provider.Datadog.Interval = "5m"
		}
		namespace := "namespace"

		provider, err := NewDatadogProvider(*logCtx, fakeClient, namespace, test.metric)
		assert.Equal(t, err != nil, test.expectedErrorProvider)
		if test.expectedErrorProvider {
			continue
		}

		metricsMetadata := provider.GetMetadata(test.metric)
		assert.Nil(t, metricsMetadata)

		// Get our result
		measurement := provider.Run(newAnalysisRun(), test.metric)

		// Common Asserts
		assert.NotNil(t, measurement)
		assert.Equal(t, string(test.expectedPhase), string(measurement.Phase))

		// Phase specific cases
		switch test.expectedPhase {
		case v1alpha1.AnalysisPhaseSuccessful:
			assert.NotNil(t, measurement.StartedAt)
			assert.Equal(t, test.expectedValue, measurement.Value)
			assert.NotNil(t, measurement.FinishedAt)
		case v1alpha1.AnalysisPhaseFailed:
			assert.NotNil(t, measurement.StartedAt)
			assert.Equal(t, test.expectedValue, measurement.Value)
			assert.NotNil(t, measurement.FinishedAt)
		case v1alpha1.AnalysisPhaseError:
			assert.Contains(t, measurement.Message, test.expectedErrorMessage)
		}

	}
}

func newAnalysisRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{}
}

func ddProviderIntervalDefault() v1alpha1.MetricProvider {
	return v1alpha1.MetricProvider{
		Datadog: &v1alpha1.DatadogMetric{
			Query: "avg:kubernetes.cpu.user.total{*}",
		},
	}
}

func ddProviderInterval10m() v1alpha1.MetricProvider {
	return v1alpha1.MetricProvider{
		Datadog: &v1alpha1.DatadogMetric{
			Query:    "avg:kubernetes.cpu.user.total{*}",
			Interval: "10m",
		},
	}
}

func ddProviderNamespacedSecret() v1alpha1.MetricProvider {
	return v1alpha1.MetricProvider{
		Datadog: &v1alpha1.DatadogMetric{
			Query:    "avg:kubernetes.cpu.user.total{*}",
			Interval: "10m",
			SecretRef: v1alpha1.SecretRef{
				Name:       "secret",
				Namespaced: true,
			},
		},
	}
}

func ddProviderNamespacedTrue() v1alpha1.MetricProvider {

	return v1alpha1.MetricProvider{
		Datadog: &v1alpha1.DatadogMetric{
			Query:    "avg:kubernetes.cpu.user.total{*}",
			Interval: "10m",
			SecretRef: v1alpha1.SecretRef{
				Namespaced: true,
			},
		},
	}
}
func newSecret(serverURL, expectedApiKey, expectedAppKey string) *corev1.Secret {
	return NewSecretBuilder().
		WithName("DatadogTokensSecretName").
		WithData("address", []byte(serverURL)).
		WithData("api-key", []byte(expectedApiKey)).
		WithData("app-key", []byte(expectedAppKey)).
		Build()
}
