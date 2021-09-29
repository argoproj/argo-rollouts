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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
)

func TestRunSuite(t *testing.T) {

	const expectedApiKey = "0123456789abcdef0123456789abcdef"
	const expectedAppKey = "0123456789abcdef0123456789abcdef01234567"

	unixNow = func() int64 { return 1599076435 }

	// Test Cases
	var tests = []struct {
		serverURL               string
		webServerStatus         int
		webServerResponse       string
		metric                  v1alpha1.Metric
		expectedIntervalSeconds int64
		expectedValue           string
		expectedPhase           v1alpha1.AnalysisPhase
		expectedErrorMessage    string
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
				Provider: v1alpha1.MetricProvider{
					Datadog: &v1alpha1.DatadogMetric{
						Query:    "avg:kubernetes.cpu.user.total{*}",
						Interval: "10m",
					},
				},
			},
			expectedIntervalSeconds: 600,
			expectedValue:           "0.0003332881882246533",
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
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
				Provider: v1alpha1.MetricProvider{
					Datadog: &v1alpha1.DatadogMetric{
						Query:    "avg:kubernetes.cpu.user.total{*}",
						Interval: "10m",
					},
				},
			},
			expectedIntervalSeconds: 600,
			expectedValue:           "0.0003332881882246533",
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
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
				Provider: v1alpha1.MetricProvider{
					Datadog: &v1alpha1.DatadogMetric{
						Query: "avg:kubernetes.cpu.user.total{*}",
					},
				},
			},
			expectedIntervalSeconds: 300,
			expectedValue:           "0.006121378742186943",
			expectedPhase:           v1alpha1.AnalysisPhaseFailed,
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
				Provider: v1alpha1.MetricProvider{
					Datadog: &v1alpha1.DatadogMetric{
						Query: "avg:kubernetes.cpu.user.total{*}",
					},
				},
			},
			expectedIntervalSeconds: 300,
			expectedPhase:           v1alpha1.AnalysisPhaseError,
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
				Provider: v1alpha1.MetricProvider{
					Datadog: &v1alpha1.DatadogMetric{
						Query: "avg:kubernetes.cpu.user.total{*}",
					},
				},
			},
			expectedIntervalSeconds: 300,
			expectedPhase:           v1alpha1.AnalysisPhaseError,
			expectedErrorMessage:    "received authentication error response code: 401 {\"errors\": [\"No authenticated user.\"]}",
			useEnvVarForKeys:        false,
		},

		// Safely succeed by excplitily accounting for isNil(result): data provided
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":[{"pointlist":[[1598867910000,0.0020008318672513122],[1598867925000,0.006121378742186943]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "isNil(result) || result < 0.05",
				Provider: v1alpha1.MetricProvider{
					Datadog: &v1alpha1.DatadogMetric{
						Query: "avg:kubernetes.cpu.user.total{*}",
					},
				},
			},
			expectedIntervalSeconds: 300,
			expectedValue:           "0.006121378742186943",
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
			useEnvVarForKeys:        false,
		},

		// Safely succeed by excplitily accounting for isNil(result): no data provided
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":[{"pointlist":[]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "isNil(result) || result < 0.05",
				Provider: v1alpha1.MetricProvider{
					Datadog: &v1alpha1.DatadogMetric{
						Query: "avg:kubernetes.cpu.user.total{*}",
					},
				},
			},
			expectedIntervalSeconds: 300,
			expectedValue:           "0",
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
			useEnvVarForKeys:        false,
		},

		// Expect failure if no data is provided
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":[{"pointlist":[]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "!isNil(result) && result < 0.05",
				Provider: v1alpha1.MetricProvider{
					Datadog: &v1alpha1.DatadogMetric{
						Query: "avg:kubernetes.cpu.user.total{*}",
					},
				},
			},
			expectedIntervalSeconds: 300,
			expectedValue:           "0",
			expectedPhase:           v1alpha1.AnalysisPhaseFailed,
			useEnvVarForKeys:        false,
		},

		// Implicitly succeed if no datapoints are returned
		{
			webServerStatus:   200,
			webServerResponse: `{"status":"ok","series":[{"pointlist":[]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider: v1alpha1.MetricProvider{
					Datadog: &v1alpha1.DatadogMetric{
						Query: "avg:kubernetes.cpu.user.total{*}",
					},
				},
			},
			expectedIntervalSeconds: 300,
			expectedValue:           "0",
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
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
				Provider: v1alpha1.MetricProvider{
					Datadog: &v1alpha1.DatadogMetric{
						Query: "avg:kubernetes.cpu.user.total{*}",
					},
				},
			},
			expectedIntervalSeconds: 300,
			expectedPhase:           v1alpha1.AnalysisPhaseError,
			expectedErrorMessage:    "Could not parse JSON body: json: cannot unmarshal string into Go struct field datadogResponse.Series of type []struct { Pointlist [][]float64 \"json:\\\"pointlist\\\"\" }",
			useEnvVarForKeys:        false,
		},

		// Error if server address is faulty
		{
			serverURL:            "://wrong.schema",
			metric:               v1alpha1.Metric{},
			expectedPhase:        v1alpha1.AnalysisPhaseError,
			expectedErrorMessage: "parse \"://wrong.schema/api/v1/query\": missing protocol scheme",
			useEnvVarForKeys:     false,
		},
	}

	// Run

	for _, test := range tests {
		serverURL := test.serverURL

		if serverURL == "" {
			// Server setup with response
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {

				//Check query variables
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

				//Check headers
				if req.Header.Get("Content-Type") != "application/json" {
					t.Errorf("\nContent-Type header expected to be application/json but got %s", req.Header.Get("Content-Type"))
				}
				if req.Header.Get("DD-API-KEY") != expectedApiKey {
					t.Errorf("\nDD-API-KEY header expected %s but got %s", expectedApiKey, req.Header.Get("DD-API-KEY"))
				}
				if req.Header.Get("DD-APPLICATION-KEY") != expectedAppKey {
					t.Errorf("\nDD-APPLICATION-KEY header expected %s but got %s", expectedAppKey, req.Header.Get("DD-APPLICATION-KEY"))
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

		tokenSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: DatadogTokensSecretName,
			},
			Data: map[string][]byte{
				"address": []byte(serverURL),
				"api-key": []byte(expectedApiKey),
				"app-key": []byte(expectedAppKey),
			},
		}

		if test.useEnvVarForKeys {
			os.Setenv("DD_API_KEY", expectedApiKey)
			os.Setenv("DD_APP_KEY", expectedAppKey)
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

		provider, _ := NewDatadogProvider(*logCtx, fakeClient)

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
