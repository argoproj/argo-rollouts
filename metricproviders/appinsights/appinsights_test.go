package appinsights

import (
	"io"
	"net/http"
	"net/http/httptest"
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

	const expectedApiId = "0123456789abcdef0123456789abcdef"
	const expectedApiKey = "0123456789abcdef0123456789abcdef01234567"

	aiProviderIntervalDefault := v1alpha1.MetricProvider{
		AppInsights: &v1alpha1.AppInsightsMetric{
			Query: "requests| summarize Failure=count(success == False), Success=count(success == True)| extend Percentage=((Success*1.0)/(Success+Failure))*100",
		},
	}

	aiProviderIntervalProfile := v1alpha1.MetricProvider{
		AppInsights: &v1alpha1.AppInsightsMetric{
			Profile: "appinsights-profile-test",
			Query:   "requests| summarize Failure=count(success == False), Success=count(success == True)| extend Percentage=((Success*1.0)/(Success+Failure))*100",
		},
	}

	// Test Cases
	var tests = []struct {
		serverURL            string
		webServerStatus      int
		webServerResponse    string
		metric               v1alpha1.Metric
		expectedValue        string
		expectedPhase        v1alpha1.AnalysisPhase
		expectedErrorMessage string
	}{
		// When first value of time series matches condition then succeed.
		{
			webServerStatus:   200,
			webServerResponse: `{"tables":[{"name":"PrimaryResult","columns":[{"name":"Failure","type":"long"},{"name":"Success","type":"long"},{"name":"Percentage","type":"real"}],"rows":[[40,1107,96.512641673932]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.Percentage[0] > 95.001",
				FailureCondition: "result.Percentage[0] <= 95.000",
				Provider:         aiProviderIntervalDefault,
			},
			expectedValue: "{\"Failure\":[40],\"Percentage\":[96.512641673932],\"Success\":[1107]}",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// Same as above but with different profile secret.
		{
			webServerStatus:   200,
			webServerResponse: `{"tables":[{"name":"PrimaryResult","columns":[{"name":"Failure","type":"long"},{"name":"Success","type":"long"},{"name":"Percentage","type":"real"}],"rows":[[40,1107,96.512641673932]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.Percentage[0] > 95.001",
				FailureCondition: "result.Percentage[0] <= 95.000",
				Provider:         aiProviderIntervalProfile,
			},
			expectedValue: "{\"Failure\":[40],\"Percentage\":[96.512641673932],\"Success\":[1107]}",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When second value of time series matches condition then succeed.
		{
			webServerStatus:   200,
			webServerResponse: `{"tables":[{"name":"PrimaryResult","columns":[{"name":"Failure","type":"long"},{"name":"Success","type":"long"},{"name":"Percentage","type":"real"}],"rows":[[2,732,99.72752043596729],[6,232,97.47899159663865]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.Percentage[1] > 95.001",
				FailureCondition: "result.Percentage[1] <= 95.000",
				Provider:         aiProviderIntervalDefault,
			},
			expectedValue: "{\"Failure\":[2,6],\"Percentage\":[99.72752043596729,97.47899159663865],\"Success\":[732,232]}",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When first value of time series match failure condition then fail.
		{
			webServerStatus:   200,
			webServerResponse: `{"tables":[{"name":"PrimaryResult","columns":[{"name":"Failure","type":"long"},{"name":"Success","type":"long"},{"name":"Percentage","type":"real"}],"rows":[[40,1107,94.512641673932]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.Percentage[0] > 95.001",
				FailureCondition: "result.Percentage[0] <= 95.000",
				Provider:         aiProviderIntervalDefault,
			},
			expectedValue: "{\"Failure\":[40],\"Percentage\":[94.512641673932],\"Success\":[1107]}",
			expectedPhase: v1alpha1.AnalysisPhaseFailed,
		},
		// Expect error with no default() and no data
		{
			webServerStatus:   200,
			webServerResponse: `{"tables":[{"name":"PrimaryResult","columns":[{"name":"Failure","type":"long"},{"name":"Success","type":"long"},{"name":"Percentage","type":"real"}],"rows":[[]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.Percentage[0] > 95.001",
				FailureCondition: "result.Percentage[0] <= 95.000",
				Provider:         aiProviderIntervalDefault,
			},
			expectedErrorMessage: "type <nil> has no field Percentage",
			expectedPhase:        v1alpha1.AnalysisPhaseError,
		},

		// When first value of time series of two results matches condition then succeed.
		{
			webServerStatus:   200,
			webServerResponse: `{"tables":[{"name":"PrimaryResult","columns":[{"name":"Failure","type":"long"},{"name":"Success","type":"long"},{"name":"Percentage","type":"real"}],"rows":[[40,1107,96.512641673932]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.Percentage[0] > 95.001 && result.Failure[0] < 50",
				FailureCondition: "result.Percentage[0] <= 95.000",
				Provider:         aiProviderIntervalDefault,
			},
			expectedValue: "{\"Failure\":[40],\"Percentage\":[96.512641673932],\"Success\":[1107]}",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When first value of time series of two results matches one failure condition then fail.
		{
			webServerStatus:   200,
			webServerResponse: `{"tables":[{"name":"PrimaryResult","columns":[{"name":"Failure","type":"long"},{"name":"Success","type":"long"},{"name":"Percentage","type":"real"}],"rows":[[40,1107,94.512641673932]]}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.Percentage[0] > 95.001 && result.Failure[0] < 50",
				FailureCondition: "result.Percentage[0] < 95.001 && result.Failure[0] > 39",
				Provider:         aiProviderIntervalDefault,
			},
			expectedValue: "{\"Failure\":[40],\"Percentage\":[94.512641673932],\"Success\":[1107]}",
			expectedPhase: v1alpha1.AnalysisPhaseFailed,
		},
		// Error if the request is invalid
		{
			webServerStatus:   400,
			webServerResponse: `{"status":"error","error":"error messsage"}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.Percentage[0] > 95.001",
				FailureCondition: "result.Percentage[0] <= 95.000",
				Provider:         aiProviderIntervalDefault,
			},
			expectedPhase:        v1alpha1.AnalysisPhaseError,
			expectedErrorMessage: "received non 2xx response code: 400 {\"status\":\"error\",\"error\":\"error messsage\"}",
		},
		// Error if the authentication is invalid
		{
			webServerStatus:   401,
			webServerResponse: `{"error":{"message":"Valid authentication was not provided","code":"AuthorizationRequiredError","correlationId":"00000000-0000-0000-0000-000000000000"}}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.Percentage[0] > 95.001",
				FailureCondition: "result.Percentage[0] <= 95.000",
				Provider:         aiProviderIntervalDefault,
			},
			expectedPhase:        v1alpha1.AnalysisPhaseError,
			expectedErrorMessage: "received authentication error response code: 401 {\"error\":{\"message\":\"Valid authentication was not provided\",\"code\":\"AuthorizationRequiredError\",\"correlationId\":\"00000000-0000-0000-0000-000000000000\"}}",
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

				if actualQuery != "requests| summarize Failure=count(success == False), Success=count(success == True)| extend Percentage=((Success*1.0)/(Success+Failure))*100" {
					t.Errorf("\nquery expected requests| summarize Failure=count(success == False), Success=count(success == True)| extend Percentage=((Success*1.0)/(Success+Failure))*100 but got %s", actualQuery)
				}

				//Check headers
				if req.Header.Get("Content-Type") != "application/json" {
					t.Errorf("\nContent-Type header expected to be application/json but got %s", req.Header.Get("Content-Type"))
				}
				if req.Header.Get("x-api-key") != expectedApiKey {
					t.Errorf("\nx-api-key header expected %s but got %s", expectedApiKey, req.Header.Get("x-api-key"))
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
			ObjectMeta: metav1.ObjectMeta{},
			Data: map[string][]byte{
				"address": []byte(serverURL),
				"api-key": []byte(expectedApiKey),
				"api-id":  []byte(expectedApiId)},
		}

		tokenSecret.ObjectMeta.Name = AppInsightsTokensSecretName
		if test.metric.Provider.AppInsights.Profile != "" {
			tokenSecret.ObjectMeta.Name = test.metric.Provider.AppInsights.Profile
		}

		logCtx := log.WithField("test", "test")

		fakeClient := k8sfake.NewSimpleClientset()
		fakeClient.PrependReactor("get", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
			return true, tokenSecret, nil
		})

		provider, _ := NewAppInsightsProvider(*logCtx, fakeClient, test.metric)

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
