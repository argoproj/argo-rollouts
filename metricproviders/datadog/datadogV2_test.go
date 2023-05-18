package datadog

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestRunSuiteV2(t *testing.T) {

	const expectedApiKey = "0123456789abcdef0123456789abcdef"
	const expectedAppKey = "0123456789abcdef0123456789abcdef01234567"

	unixNow = func() int64 { return 1599076435 }

	ddProviderIntervalDefault := v1alpha1.MetricProvider{
		Datadog: &v1alpha1.DatadogMetric{
			Query:      "avg:kubernetes.cpu.user.total{*}",
			ApiVersion: "v2",
		},
	}

	ddProviderInterval10m := v1alpha1.MetricProvider{
		Datadog: &v1alpha1.DatadogMetric{
			Query:      "avg:kubernetes.cpu.user.total{*}",
			Interval:   "10m",
			ApiVersion: "v2",
		},
	}

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
			webServerResponse: `{"data": {"attributes": {"values": [[0.0020008318672513122, 0.0003332881882246533]], "times": [1598867910000, 1598867925000]}}}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         ddProviderInterval10m,
			},
			expectedIntervalSeconds: 600,
			expectedValue:           "0.0003332881882246533",
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
			useEnvVarForKeys:        false,
		},
		// Same test as above, but derive DD keys from env var instead of k8s secret
		{
			webServerStatus:   200,
			webServerResponse: `{"data": {"attributes": {"values": [[0.0020008318672513122, 0.0003332881882246533]], "times": [1598867910000, 1598867925000]}}}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         ddProviderInterval10m,
			},
			expectedIntervalSeconds: 600,
			expectedValue:           "0.0003332881882246533",
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
			useEnvVarForKeys:        true,
		},
		// When last value of time series does not match condition then fail.
		{
			webServerStatus:   200,
			webServerResponse: `{"data": {"attributes": {"values": [[0.0020008318672513122, 0.006121378742186943]], "times": [1598867910000, 1598867925000]}}}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         ddProviderIntervalDefault,
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
				Provider:         ddProviderIntervalDefault,
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
				Provider:         ddProviderIntervalDefault,
			},
			expectedIntervalSeconds: 300,
			expectedPhase:           v1alpha1.AnalysisPhaseError,
			expectedErrorMessage:    "received authentication error response code: 401 {\"errors\": [\"No authenticated user.\"]}",
			useEnvVarForKeys:        false,
		},

		// Expect success with default() and data
		{
			webServerStatus:   200,
			webServerResponse: `{"data": {"attributes": {"values": [[0.0020008318672513122, 0.006121378742186943]], "times": [1598867910000, 1598867925000]}}}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "default(result, 0) < 0.05",
				Provider:         ddProviderIntervalDefault,
			},
			expectedIntervalSeconds: 300,
			expectedValue:           "0.006121378742186943",
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
			useEnvVarForKeys:        false,
		},

		// Expect error with no default() and no data
		{
			webServerStatus:   200,
			webServerResponse: `{"data": {"attributes": {"values": [], "times": []}}}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.05",
				Provider:         ddProviderIntervalDefault,
			},
			expectedIntervalSeconds: 300,
			expectedPhase:           v1alpha1.AnalysisPhaseError,
			expectedErrorMessage:    `invalid operation: < (mismatched types <nil> and float64)`,
			useEnvVarForKeys:        false,
		},

		// Expect success with default() and no data
		{
			webServerStatus:   200,
			webServerResponse: `{"data": {"attributes": {"values": [], "times": []}}}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "default(result, 0) < 0.05",
				Provider:         ddProviderIntervalDefault,
			},
			expectedIntervalSeconds: 300,
			expectedValue:           `{"Values":[],"Times":[]}`,
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
			useEnvVarForKeys:        false,
		},

		// Expect failure with bad default() and no data
		{
			webServerStatus:   200,
			webServerResponse: `{"data": {"attributes": {"values": [], "times": []}}}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "default(result, 1) < 0.05",
				Provider:         ddProviderIntervalDefault,
			},
			expectedIntervalSeconds: 300,
			expectedValue:           `{"Values":[],"Times":[]}`,
			expectedPhase:           v1alpha1.AnalysisPhaseFailed,
			useEnvVarForKeys:        false,
		},

		// Expect success with bad default() and good data
		{
			webServerStatus:   200,
			webServerResponse: `{"data": {"attributes": {"values": [[0.0020008318672513122, 0.006121378742186943]], "times": [1598867910000, 1598867925000]}}}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "default(result, 1) < 0.05",
				Provider:         ddProviderIntervalDefault,
			},
			expectedIntervalSeconds: 300,
			expectedValue:           `0.006121378742186943`,
			expectedPhase:           v1alpha1.AnalysisPhaseSuccessful,
			useEnvVarForKeys:        false,
		},

		// Error if datadog returns non-array values
		{
			webServerStatus:   200,
			webServerResponse: `{"data": {"attributes": {"values": "invalid", "times": "invalid"}}}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result < 0.001",
				FailureCondition: "result >= 0.001",
				Provider:         ddProviderIntervalDefault,
			},
			expectedIntervalSeconds: 300,
			expectedPhase:           v1alpha1.AnalysisPhaseError,
			expectedErrorMessage:    "Could not parse JSON body: json: cannot unmarshal string into Go struct field .Data.Attributes.Values of type [][]float64",
			useEnvVarForKeys:        false,
		},

		// Error if server address is faulty
		{
			serverURL:            "://wrong.schema",
			metric:               v1alpha1.Metric{},
			expectedPhase:        v1alpha1.AnalysisPhaseError,
			expectedErrorMessage: "parse \"://wrong.schema\": missing protocol scheme",
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
				bodyBytes, err := ioutil.ReadAll(req.Body)
				if err != nil {
					t.Errorf("\nreceived no bytes in request: %v", err)
				}

				var reqBody datadogRequest
				err = json.Unmarshal(bodyBytes, &reqBody)
				if err != nil {
					t.Errorf("\nCould not parse JSON request body: %v", err)
				}

				actualQuery := reqBody.Data.Attributes.Queries[0]["query"]
				actualFrom := reqBody.Data.Attributes.From
				actualTo := reqBody.Data.Attributes.To

				if actualQuery != "avg:kubernetes.cpu.user.total{*}" {
					t.Errorf("\nquery expected avg:kubernetes.cpu.user.total{*} but got %s", actualQuery)
				}

				if actualFrom != (unixNow()-test.expectedIntervalSeconds)*1000 {
					t.Errorf("\nfrom %d expected be equal to %d", actualFrom, (unixNow()-test.expectedIntervalSeconds)*1000)
				} else if err != nil {
					t.Errorf("\nfailed to parse from: %v", err)
				}

				if actualTo != unixNow()*1000 {
					t.Errorf("\nto %d was expected be equal to %d", actualTo, unixNow()*1000)
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
