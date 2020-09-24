package webmetric

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestRunSuite(t *testing.T) {
	// Test Cases
	var tests = []struct {
		webServerStatus      int
		webServerResponse    string
		metric               v1alpha1.Metric
		expectedValue        string
		expectedPhase        v1alpha1.AnalysisPhase
		expectedErrorMessage string
	}{
		// When_numberReturnedInJson_And_MatchesConditions_Then_Succeed
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": 1}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "asInt(result) > 0",
				FailureCondition: "asInt(result) <= 0",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key[0].key2.value}",
						Headers:  []v1alpha1.WebMetricHeader{{Key: "key", Value: "value"}},
					},
				},
			},
			expectedValue: "1",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_numberReturnedInJson_And_DoesNotMatchConditions_Then_Failure
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": 0}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "asInt(result) > 0",
				FailureCondition: "asInt(result) <= 0",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key[0].key2.value}",
					},
				},
			},
			expectedValue: "0",
			expectedPhase: v1alpha1.AnalysisPhaseFailed,
		},
		// When_floatReturnedInJson_And_MatchesConditions_Then_Success
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": 1.1}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "asFloat(result) > 0",
				FailureCondition: "asFloat(result) <= 0",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key[0].key2.value}",
					},
				},
			},
			expectedValue: "1.1",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_floatReturnedInJson_And_DoesNotMatchConditions_Then_Failure
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": -1.1}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "asFloat(result) > 0",
				FailureCondition: "asFloat(result) <= 0",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key[0].key2.value}",
					},
				},
			},
			expectedValue: "-1.1",
			expectedPhase: v1alpha1.AnalysisPhaseFailed,
		},
		// When_stringReturnedInJson_And_MatchesConditions_Then_Succeed
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": "true"}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "true",
				FailureCondition: "false",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key[0].key2.value}",
					},
				},
			},
			expectedValue: "true",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_stringReturnedInJson_And_DoesNotMatchConditions_Then_Fail
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": "true"}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "true",
				FailureCondition: "true",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key[0].key2.value}",
					},
				},
			},
			expectedValue: "true",
			expectedPhase: v1alpha1.AnalysisPhaseFailed,
		},
		// When_non200_Then_Error
		{
			webServerStatus:   300,
			webServerResponse: `{"key": [{"key2": {"value": "true"}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "true",
				FailureCondition: "true",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key[0].key2.value}",
					},
				},
			},
			expectedValue: "true",
			expectedPhase: v1alpha1.AnalysisPhaseError,
		},
		// When_BadURL_Then_Fail
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": "true"}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "true",
				FailureCondition: "true",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						URL:      "bad://url.com",
						JSONPath: "{$.key[0].key2.value}",
					},
				},
			},
			expectedValue:        "true",
			expectedPhase:        v1alpha1.AnalysisPhaseError,
			expectedErrorMessage: "unsupported protocol scheme",
		},
		// When_200Response_And_EmptyBody_Then_Error
		{
			webServerStatus:   200,
			webServerResponse: ``,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "true",
				FailureCondition: "true",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						JSONPath: "{$.key[0].key2.value}",
					},
				},
			},
			expectedValue:        "true",
			expectedPhase:        v1alpha1.AnalysisPhaseError,
			expectedErrorMessage: "Could not parse JSON body",
		},
		// When_200Response_And_InvalidBody_Then_Error
		{
			webServerStatus:   200,
			webServerResponse: `test: notJson`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "true",
				FailureCondition: "true",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						JSONPath: "{$.key[0].key2.value}",
					},
				},
			},
			expectedValue:        "true",
			expectedPhase:        v1alpha1.AnalysisPhaseError,
			expectedErrorMessage: "Could not parse JSON body",
		},
		// When_200Response_And_JsonPathHasNoMatch_Then_Error
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": "true"}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "true",
				FailureCondition: "true",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						JSONPath: "{$.key[0].key2.novalue}",
					},
				},
			},
			expectedValue:        "true",
			expectedPhase:        v1alpha1.AnalysisPhaseError,
			expectedErrorMessage: "Could not find JSONPath in body",
		},
	}

	// Run

	for _, test := range tests {
		// Server setup with response
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if test.webServerStatus < 200 || test.webServerStatus >= 300 {
				http.Error(rw, http.StatusText(test.webServerStatus), test.webServerStatus)
			} else {
				rw.Header().Set("Content-Type", "application/json")
				if test.webServerResponse != "" {
					io.WriteString(rw, test.webServerResponse)
				}
			}
		}))
		defer server.Close()

		// Need to set this dynamically if not present...
		if test.metric.Provider.Web.URL == "" {
			test.metric.Provider.Web.URL = server.URL
		}

		logCtx := log.WithField("test", "test")

		jsonparser, err := NewWebMetricJsonParser(test.metric)
		assert.NoError(t, err)
		provider := NewWebMetricProvider(*logCtx, server.Client(), jsonparser)

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
