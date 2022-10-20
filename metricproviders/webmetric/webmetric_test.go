package webmetric

import (
	"bytes"
	"encoding/json"
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
		expectedMethod       string
		expectedBody         string
		expectedValue        string
		expectedPhase        v1alpha1.AnalysisPhase
		expectedErrorMessage string
		expectedJsonBody     string
	}{
		// When_noJSONPathSpecified_And_MatchesConditions_Then_Succeed
		{
			webServerStatus:   200,
			webServerResponse: `{"a": 1, "b": true, "c": [1, 2, 3, 4], "d": null}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.a > 0 && result.b && all(result.c, {# < 5}) && result.d == nil",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						Headers: []v1alpha1.WebMetricHeader{{Key: "key", Value: "value"}},
					},
				},
			},
			expectedValue: `{"a":1,"b":true,"c":[1,2,3,4],"d":null}`,
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_matchesNeitherCondition_Then_Inconclusive
		{
			webServerStatus:   200,
			webServerResponse: `{"a": 1, "b": true, "c": [1, 2, 3, 4], "d": null}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.a >= 2 && result.b && all(result.c, {# < 5}) && result.d == nil",
				FailureCondition: "result.a <= 0 && result.b && all(result.c, {# < 5}) && result.d == nil",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						Headers: []v1alpha1.WebMetricHeader{{Key: "key", Value: "value"}},
					},
				},
			},
			expectedValue: `{"a":1,"b":true,"c":[1,2,3,4],"d":null}`,
			expectedPhase: v1alpha1.AnalysisPhaseInconclusive,
		},
		// When_intStringReturnedInJson_And_MatchesConditions_Then_Succeed
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": "1"}}]}`,
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
			expectedValue: "\"1\"",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_intStringReturnedInJson_And_DoesNotMatcheConditions_Then_Failure
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": "0"}}]}`,
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
			expectedValue: "\"0\"",
			expectedPhase: v1alpha1.AnalysisPhaseFailed,
		},
		// When_floatStringReturnedInJson_And_MatchesConditions_Then_Succeed
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": "1.2"}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "asFloat(result) > 1.1",
				FailureCondition: "asFloat(result) <= 0",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key[0].key2.value}",
						Headers:  []v1alpha1.WebMetricHeader{{Key: "key", Value: "value"}},
					},
				},
			},
			expectedValue: `"1.2"`,
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_floatStringReturnedInJson_And_DoesNotMatcheConditions_Then_Failure
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": "1.2"}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "asFloat(result) > 1.1",
				FailureCondition: "asFloat(result) < 1.3",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key[0].key2.value}",
					},
				},
			},
			expectedValue: `"1.2"`,
			expectedPhase: v1alpha1.AnalysisPhaseFailed,
		},
		// When_numberReturnedInJson_And_MatchesConditions_Then_Succeed
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": 1}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result > 0",
				FailureCondition: "result <= 0",
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
				SuccessCondition: "result > 0",
				FailureCondition: "result <= 0",
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
				SuccessCondition: "result > 0",
				FailureCondition: "result <= 0",
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
				SuccessCondition: "result > 0",
				FailureCondition: "result <= 0",
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
				SuccessCondition: `result == "true"`,
				FailureCondition: `result == "false"`,
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key[0].key2.value}",
					},
				},
			},
			expectedValue: `"true"`,
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_stringReturnedInJson_And_DoesNotMatchConditions_Then_Fail
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": "true"}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: `result == "true"`,
				FailureCondition: `result == "true"`,
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key[0].key2.value}",
					},
				},
			},
			expectedValue: `"true"`,
			expectedPhase: v1alpha1.AnalysisPhaseFailed,
		},
		// When_boolReturnedInJson_And_MatchesConditions_Then_Succeed
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": true}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result == true",
				FailureCondition: "result == false",
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
		// When_boolReturnedInJson_And_DoesNotMatchConditions_Then_Fail
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [{"key2": {"value": false}}]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result == true",
				FailureCondition: "result == false",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key[0].key2.value}",
					},
				},
			},
			expectedValue: "false",
			expectedPhase: v1alpha1.AnalysisPhaseFailed,
		},
		// When_listReturnedInJson_And_MatchesConditions_Then_Succeed
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [1, 2, 3, 4, 5, 6]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "any(result, {# > 5})",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key}",
					},
				},
			},
			expectedValue: "[1,2,3,4,5,6]",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_listReturnedInJson_And_DoesNotMatchConditions_Then_Fail
		{
			webServerStatus:   200,
			webServerResponse: `{"key": [1, 2, 3, 4, 5, 6]}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "any(result, {# > 6})",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key}",
					},
				},
			},
			expectedValue: "[1,2,3,4,5,6]",
			expectedPhase: v1alpha1.AnalysisPhaseFailed,
		},
		// When_mapReturnedInJson_And_MatchesConditions_Then_Succeed
		{
			webServerStatus:   200,
			webServerResponse: `{"key":{"num":1.2, "bool":true, "mapfield":{"foo":"bar"}}}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: `result.num > 1.1 && result.bool && result.mapfield.foo == "bar"`,
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key}",
					},
				},
			},
			expectedValue: `{"bool":true,"mapfield":{"foo":"bar"},"num":1.2}`,
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_mapReturnedInJson_And_DoesNotMatchConditions_Then_Fail
		{
			webServerStatus:   200,
			webServerResponse: `{"key":{"num":1.2, "bool":true, "mapfield":{"foo":"bar"}}}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: `result.num > 1.1 && result.bool && result.mapfield.foo == "bar"`,
				FailureCondition: `result.num > 1.1 && result.bool && result.mapfield.foo == "bar"`,
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						JSONPath: "{$.key}",
					},
				},
			},
			expectedValue: `{"bool":true,"mapfield":{"foo":"bar"},"num":1.2}`,
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
		// When_200Response_And_EmptyBody_Then_Succeed
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
			expectedValue: "",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_200Response_And_NonJsonBody_Then_Succeed
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
			expectedValue: "test: notJson",
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
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
		// When_200Response_And_NilBody_Then_Succeed
		{
			webServerStatus: 200,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "true",
				FailureCondition: "true",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{},
				},
			},
			expectedPhase: v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_200Response_And_AnyJson_Then_Succeed
		{
			webServerStatus:   200,
			webServerResponse: `{}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "true",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{},
				},
			},
			expectedValue:        "{}",
			expectedPhase:        v1alpha1.AnalysisPhaseSuccessful,
			expectedErrorMessage: "",
		},
		// When_non200Response_And_NoBody_Then_Failure
		{
			webServerStatus:   400,
			webServerResponse: ``,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{},
				},
			},
			expectedValue:        "",
			expectedPhase:        v1alpha1.AnalysisPhaseError,
			expectedErrorMessage: "",
		},
		// When_methodEmpty_Then_server_gets_GET
		{
			webServerStatus:   200,
			webServerResponse: `{"a": 1, "b": true, "c": [1, 2, 3, 4], "d": null}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.a > 0 && result.b && all(result.c, {# < 5}) && result.d == nil",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						Headers: []v1alpha1.WebMetricHeader{{Key: "key", Value: "value"}},
					},
				},
			},
			expectedMethod: "GET",
			expectedValue:  `{"a":1,"b":true,"c":[1,2,3,4],"d":null}`,
			expectedPhase:  v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_methodGET_Then_server_gets_GET
		{
			webServerStatus:   200,
			webServerResponse: `{"a": 1, "b": true, "c": [1, 2, 3, 4], "d": null}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.a > 0 && result.b && all(result.c, {# < 5}) && result.d == nil",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						Method: v1alpha1.WebMetricMethodGet,
						// URL:      server.URL,
						Headers: []v1alpha1.WebMetricHeader{{Key: "key", Value: "value"}},
					},
				},
			},
			expectedMethod: "GET",
			expectedValue:  `{"a":1,"b":true,"c":[1,2,3,4],"d":null}`,
			expectedPhase:  v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_methodPOST_Then_server_gets_body
		{
			webServerStatus:   200,
			webServerResponse: `{"a": 1, "b": true, "c": [1, 2, 3, 4], "d": null}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.a > 0 && result.b && all(result.c, {# < 5}) && result.d == nil",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						Method: v1alpha1.WebMetricMethodPost,
						// URL:      server.URL,
						Headers: []v1alpha1.WebMetricHeader{{Key: "key", Value: "value"}},
						Body:    "some body",
					},
				},
			},
			expectedMethod: "POST",
			expectedBody:   "some body",
			expectedValue:  `{"a":1,"b":true,"c":[1,2,3,4],"d":null}`,
			expectedPhase:  v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_methodPUT_Then_server_gets_body
		{
			webServerStatus:   200,
			webServerResponse: `{"a": 1, "b": true, "c": [1, 2, 3, 4], "d": null}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.a > 0 && result.b && all(result.c, {# < 5}) && result.d == nil",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						Method: v1alpha1.WebMetricMethodPut,
						// URL:      server.URL,
						Headers: []v1alpha1.WebMetricHeader{{Key: "key", Value: "value"}},
						Body:    "some body",
					},
				},
			},
			expectedMethod: "PUT",
			expectedBody:   "some body",
			expectedValue:  `{"a":1,"b":true,"c":[1,2,3,4],"d":null}`,
			expectedPhase:  v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_sendingBodyWithGet_Then_Failure
		{
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.a > 0 && result.b && all(result.c, {# < 5}) && result.d == nil",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						Headers: []v1alpha1.WebMetricHeader{{Key: "key", Value: "value"}},
						Body:    "some body",
					},
				},
			},
			expectedValue: "Body can only be used with POST or PUT WebMetric Method types",
			expectedPhase: v1alpha1.AnalysisPhaseError,
		},
		// When_methodPOST_Then_server_gets_jsonBody_Then_Succeed
		{
			webServerStatus:   200,
			webServerResponse: `{"a": 1, "b": true, "c": [1, 2, 3, 4], "d": null}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.a > 0 && result.b && all(result.c, {# < 5}) && result.d == nil",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						Method: v1alpha1.WebMetricMethodPost,
						// URL:      server.URL,
						Headers:  []v1alpha1.WebMetricHeader{{Key: "key", Value: "value"}},
						JSONBody: json.RawMessage(`{"key1": "value1", "key2": "value2"}`),
					},
				},
			},
			expectedMethod:   "POST",
			expectedJsonBody: `{"key1": "value1", "key2": "value2"}`,
			expectedValue:    `{"a":1,"b":true,"c":[1,2,3,4],"d":null}`,
			expectedPhase:    v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_methodPUT_Then_server_gets_jsonBody_Then_Succeed
		{
			webServerStatus:   200,
			webServerResponse: `{"a": 1, "b": true, "c": [1, 2, 3, 4], "d": null}`,
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.a > 0 && result.b && all(result.c, {# < 5}) && result.d == nil",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						Method: v1alpha1.WebMetricMethodPut,
						// URL:      server.URL,
						Headers:  []v1alpha1.WebMetricHeader{{Key: "key", Value: "value"}, {Key: ContentTypeKey, Value: ContentTypeJsonValue}},
						JSONBody: json.RawMessage(`{"key1": "value1", "key2": { "key3" : "value3"}}`),
					},
				},
			},
			expectedMethod:   "PUT",
			expectedJsonBody: `{"key1": "value1", "key2": { "key3" : "value3"}}`,
			expectedValue:    `{"a":1,"b":true,"c":[1,2,3,4],"d":null}`,
			expectedPhase:    v1alpha1.AnalysisPhaseSuccessful,
		},
		// When_sendingJsonBodyWithGet_Then_Failure
		{
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.a > 0 && result.b && all(result.c, {# < 5}) && result.d == nil",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						Headers:  []v1alpha1.WebMetricHeader{{Key: "key", Value: "value"}},
						JSONBody: json.RawMessage(`{"key1": "value1", "key2": { "key3" : "value3"}}`),
					},
				},
			},
			expectedValue: "Body/JSONBody can only be used with POST or PUT WebMetric Method types",
			expectedPhase: v1alpha1.AnalysisPhaseError,
		},
		// When_sending_BothBodyAndJsonBodyWithGet_Then_Failure
		{
			metric: v1alpha1.Metric{
				Name:             "foo",
				SuccessCondition: "result.a > 0 && result.b && all(result.c, {# < 5}) && result.d == nil",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						// URL:      server.URL,
						Headers:  []v1alpha1.WebMetricHeader{{Key: "key", Value: "value"}},
						JSONBody: json.RawMessage(`{"key1": "value1", "key2": { "key3" : "value3"}}`),
						Body:     "test body",
					},
				},
			},
			expectedValue: "use either Body or JSONBody; both cannot exists for WebMetric payload",
			expectedPhase: v1alpha1.AnalysisPhaseError,
		},
	}

	// Run

	for _, test := range tests {
		// Server setup with response
		server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if test.expectedMethod != "" {
				assert.Equal(t, test.expectedMethod, req.Method)
			}

			if test.expectedBody != "" {
				buf := new(bytes.Buffer)
				buf.ReadFrom(req.Body)
				assert.Equal(t, test.expectedBody, buf.String())
			}

			if test.expectedJsonBody != "" {
				bodyBytes, _ := io.ReadAll(req.Body)
				assert.Equal(t, test.expectedJsonBody, string(bodyBytes))
				assert.Equal(t, ContentTypeJsonValue, req.Header.Get(ContentTypeKey))
			}

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

		metricsMetadata := provider.GetMetadata(test.metric)
		assert.Nil(t, metricsMetadata)

		// Get our result
		measurement := provider.Run(newAnalysisRun(), test.metric)

		// Common Asserts
		assert.NotNil(t, measurement)
		if !assert.Equal(t, string(test.expectedPhase), string(measurement.Phase)) {
			assert.NotNil(t, measurement)
		}

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
