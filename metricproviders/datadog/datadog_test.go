// These are tests that don't belong to v1 or v2 API

package datadog

import (
	"encoding/json"
	"os"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	kubetesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestDatadogSpecDefaults(t *testing.T) {
	_ = apiextv1.AddToScheme(scheme.Scheme)
	decode := scheme.Codecs.UniversalDeserializer().Decode

	// Load CRD yaml. Is this icky? It seems like the best way to guarantee
	// what we expect, since setting the default is via annotations
	// Only bothering with the analysis-template-crd. We know cluster-analysis-template-crd
	// & analysis-run-crd are all generated from the same source object.
	data, err := os.ReadFile("../../manifests/crds/analysis-template-crd.yaml")
	if err != nil {
		log.Fatalf("could not read CRD file: %v", err)
	}
	obj, gVK, err := decode(data, nil, nil)
	if err != nil {
		log.Fatalf("error parsing file: %v", err)
	}
	if gVK.Kind != "CustomResourceDefinition" {
		log.Fatalf("object was not a CRD")
	}

	o := obj.(*apiextv1.CustomResourceDefinition)
	ddSpec := o.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"].Properties["metrics"].Items.Schema.Properties["provider"].Properties["datadog"]

	t.Run("apiVersion: Validate default is v1", func(t *testing.T) {
		defaultVersion := string(ddSpec.Properties["apiVersion"].Default.Raw)
		assert.Equal(t, "\"v1\"", defaultVersion, "Default version should be v1")
	})

	t.Run("apiVersion: Validate enum exists to restrict apiVersion to 2 options", func(t *testing.T) {
		versionEnums := ddSpec.Properties["apiVersion"].Enum
		assert.Equal(t, 2, len(versionEnums), "Expecting 2 enum options")
		assert.Equal(t, "\"v1\"", string(versionEnums[0].Raw), "\"v1\" expected, got %s", string(versionEnums[0].Raw))
		assert.Equal(t, "\"v2\"", string(versionEnums[1].Raw), "\"v2\" is missing, got %s", string(versionEnums[1].Raw))
	})

	t.Run("interval: Validate default is 5m", func(t *testing.T) {
		defaultInterval := string(ddSpec.Properties["interval"].Default.Raw)
		assert.Equal(t, "\"5m\"", defaultInterval, "Default interval should be \"5m\" ")
	})

	t.Run("aggregator: Validate enum exists to restrict aggregator to 9 options", func(t *testing.T) {
		aggregatorEnums := ddSpec.Properties["aggregator"].Enum
		assert.Equal(t, 9, len(aggregatorEnums), "Expecting 9 enum options")
		assert.Equal(t, "\"avg\"", string(aggregatorEnums[0].Raw), "\"avg\" expected, got %s", string(aggregatorEnums[0].Raw))
		assert.Equal(t, "\"min\"", string(aggregatorEnums[1].Raw), "\"min\" expected, got %s", string(aggregatorEnums[1].Raw))
		assert.Equal(t, "\"max\"", string(aggregatorEnums[2].Raw), "\"max\" expected, got %s", string(aggregatorEnums[2].Raw))
		assert.Equal(t, "\"sum\"", string(aggregatorEnums[3].Raw), "\"sum\" expected, got %s", string(aggregatorEnums[3].Raw))
		assert.Equal(t, "\"last\"", string(aggregatorEnums[4].Raw), "\"last\" expected, got %s", string(aggregatorEnums[4].Raw))
		assert.Equal(t, "\"percentile\"", string(aggregatorEnums[5].Raw), "\"percentile\" expected, got %s", string(aggregatorEnums[5].Raw))
		assert.Equal(t, "\"mean\"", string(aggregatorEnums[6].Raw), "\"mean\" expected, got %s", string(aggregatorEnums[6].Raw))
		assert.Equal(t, "\"l2norm\"", string(aggregatorEnums[7].Raw), "\"l2norm\" expected, got %s", string(aggregatorEnums[7].Raw))
		assert.Equal(t, "\"area\"", string(aggregatorEnums[8].Raw), "\"area\" expected, got %s", string(aggregatorEnums[8].Raw))
	})
}

func TestValidateIncomingProps(t *testing.T) {
	tests := []struct {
		name                 string
		metric               *v1alpha1.DatadogMetric
		expectedErrorMessage string
	}{
		{
			name: "query and queries missing",
			metric: &v1alpha1.DatadogMetric{
				ApiVersion: "v1",
				Query:      "",
				Queries:    nil,
			},
			expectedErrorMessage: "Must have either a query or queries",
		},
		{
			name: "both query and queries",
			metric: &v1alpha1.DatadogMetric{
				ApiVersion: "v1",
				Query:      "foo",
				Queries:    map[string]string{"a": "sum:api_gateway.request.count{*}.as_count()"},
			},
			expectedErrorMessage: "Cannot have both a query and queries",
		},
		{
			name: "queries with v1 api",
			metric: &v1alpha1.DatadogMetric{
				ApiVersion: "v1",
				Queries:    map[string]string{"a": "sum:api_gateway.request.count{*}.as_count()"},
			},
			expectedErrorMessage: "Query is empty. API Version v1 only supports using the query parameter in your Analysis Template.",
		},
		{
			name: "formula/queries with wrong apiVersion",
			metric: &v1alpha1.DatadogMetric{
				ApiVersion: "v1",
				Queries:    map[string]string{"a": "sum:api_gateway.request.count{*}.as_count()"},
				Formula:    "a",
			},
			expectedErrorMessage: "Query is empty. API Version v1 only supports using the query parameter in your Analysis Template.",
		},
		{
			name: "formula without queries",
			metric: &v1alpha1.DatadogMetric{
				ApiVersion: "v1",
				Formula:    "foo / bar",
				Query:      "foo",
			},
			expectedErrorMessage: "Formula are only valid when queries are set",
		},
		{
			name: "v1 query with aggregator",
			metric: &v1alpha1.DatadogMetric{
				ApiVersion: "v1",
				Query:      "foo",
				Aggregator: "sum",
			},
			expectedErrorMessage: "Aggregator is not supported in v1. Please review the Analysis Template.",
		},
		{
			name: "More than 1 queries with no formula",
			metric: &v1alpha1.DatadogMetric{
				ApiVersion: "v2",
				Query:      "",
				Queries:    map[string]string{"a": "sum:api_gateway.request.count{*}.as_count()", "b": "fish bike"},
			},
			expectedErrorMessage: "When multiple queries are provided you must include a formula.",
		},
		{
			name: "valid simple query with v2",
			metric: &v1alpha1.DatadogMetric{
				ApiVersion: "v2",
				Query:      "foo",
			},
			expectedErrorMessage: "",
		},
		{
			name: "valid queries with v2",
			metric: &v1alpha1.DatadogMetric{
				ApiVersion: "v2",
				Query:      "",
				Queries:    map[string]string{"a": "sum:api_gateway.request.count{*}.as_count()", "b": "fish bike"},
				Formula:    "a + b",
			},
			expectedErrorMessage: "",
		},
		{
			name: "valid queries with v2 and an aggregator",
			metric: &v1alpha1.DatadogMetric{
				ApiVersion: "v2",
				Query:      "",
				Queries:    map[string]string{"a": "sum:api_gateway.request.count{*}.as_count()", "b": "fish bike"},
				Formula:    "a + b",
				Aggregator: "avg",
			},
			expectedErrorMessage: "",
		},
		{
			name: "v1 query with reducer",
			metric: &v1alpha1.DatadogMetric{
				ApiVersion: "v1",
				Query:      "foo",
				Reducer:    "max",
			},
			expectedErrorMessage: "Reducer is not supported in v1. Please review the Analysis Template.",
		},
		{
			name: "valid v2 query with reducer",
			metric: &v1alpha1.DatadogMetric{
				ApiVersion: "v2",
				Query:      "sum:errors{*} by {resource_name}",
				Reducer:    "max",
			},
			expectedErrorMessage: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateIncomingProps(test.metric)
			if test.expectedErrorMessage != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), test.expectedErrorMessage)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFindCredentials(t *testing.T) {

	testCases := []struct {
		name                string
		secret              *corev1.Secret
		expectsError        bool
		expectsEmptyAddress bool
		metric              v1alpha1.Metric
	}{
		{
			name:   "when secretRef is set and secret found, should success",
			secret: NewSecretBuilderDefaultData().Build(),
			metric: newMetric("datadog", true),
		},
		{
			name:                "when secretRef without address is set and secret found, should success",
			secret:              NewSecretBuilder().WithData("api-key", []byte("apiKey")).WithData("app-key", []byte("appKey")).Build(),
			metric:              newMetric("datadog", true),
			expectsEmptyAddress: true,
		},
		{
			name:         "when secretRef is set but secret not found, should fail",
			secret:       NewSecretBuilder().Build(),
			metric:       newMetric("datadog", true),
			expectsError: true,
		},
		{
			name:         "when secretRef name is not set but namespaced is true, should fail",
			secret:       NewSecretBuilder().Build(),
			metric:       newMetric("", true),
			expectsError: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			logCtx := *log.WithField("test", "test")
			fakeClient := k8sfake.NewSimpleClientset()
			fakeClient.PrependReactor("get", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
				return true, testCase.secret, nil
			})

			address, apiKey, appKey, err := findCredentials(logCtx, fakeClient, "namespace", testCase.metric)
			assert.Equal(t, err != nil, testCase.expectsError)
			if !testCase.expectsError {
				if testCase.expectsEmptyAddress {
					assert.Empty(t, address)
				} else {
					assert.Equal(t, string(testCase.secret.Data["address"]), address)
				}
				assert.Equal(t, string(testCase.secret.Data["api-key"]), apiKey)
				assert.Equal(t, string(testCase.secret.Data["app-key"]), appKey)
			}
		})

	}
}

func newMetric(name string, namespaced bool) v1alpha1.Metric {
	return v1alpha1.Metric{
		Provider: v1alpha1.MetricProvider{
			Datadog: &v1alpha1.DatadogMetric{
				Query:      "avg:kubernetes.cpu.user.total{*}",
				Interval:   "5m",
				Aggregator: "sum",
				ApiVersion: "v2",
				SecretRef: v1alpha1.SecretRef{
					Name:       name,
					Namespaced: namespaced,
				},
			},
		},
	}
}

func rawJSON(values ...string) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(values))
	for _, v := range values {
		out = append(out, json.RawMessage(v))
	}
	return out
}

func TestDatadogV2ColumnNumeric(t *testing.T) {
	// Mix of valid numbers, null, and a stray non-numeric value. Numeric()
	// should keep only the parseable floats.
	col := datadogV2Column{
		Type:   "number",
		Values: rawJSON(`1.5`, `null`, `"not-a-number"`, `2`),
	}
	assert.Equal(t, []float64{1.5, 2}, col.Numeric())
}

func TestDatadogV2ColumnStrings(t *testing.T) {
	// Mix of strings, null, and a stray number. Strings() should keep only the
	// real strings - this is what protects us from a group column that has a
	// null tag value or a malformed entry.
	col := datadogV2Column{
		Type:   "group",
		Values: rawJSON(`"alpha"`, `null`, `42`, `"beta"`),
	}
	assert.Equal(t, []string{"alpha", "beta"}, col.Strings())
}

func TestFindGroupValues(t *testing.T) {
	// Returns the strings from the group column when present.
	cols := []datadogV2Column{
		{Type: "number", Values: rawJSON(`1`, `2`)},
		{Type: "group", Values: rawJSON(`"a"`, `"b"`)},
	}
	assert.Equal(t, []string{"a", "b"}, findGroupValues(cols))

	// Returns nil when no group column is present - the ungrouped case.
	cols = []datadogV2Column{
		{Type: "number", Values: rawJSON(`1`)},
	}
	assert.Nil(t, findGroupValues(cols))
}

func TestIdentifyFailingGroups(t *testing.T) {
	logCtx := log.WithField("test", "test")
	metric := v1alpha1.Metric{
		Name:             "test",
		SuccessCondition: "result < 5",
		FailureCondition: "result >= 5",
	}

	// Straight path: values 1 and 7 against `result < 5` - only 7 fails.
	failing := identifyFailingGroups(metric, []float64{1, 7}, []string{"ok", "bad"}, *logCtx)
	assert.Equal(t, []string{"bad=7"}, failing)

	// Mismatched lengths (more values than group names): we break out of the
	// loop rather than indexing past the slice. This guards against malformed
	// Datadog responses.
	failing = identifyFailingGroups(metric, []float64{1, 7, 9}, []string{"a"}, *logCtx)
	assert.Equal(t, 0, len(failing))

	// Bad condition expression: EvaluateResult returns an error per group and
	// we skip it without aborting the whole attribution.
	bad := v1alpha1.Metric{
		Name:             "bad",
		SuccessCondition: "this is not valid expr (",
	}
	failing = identifyFailingGroups(bad, []float64{1, 2}, []string{"a", "b"}, *logCtx)
	assert.Equal(t, 0, len(failing))
}

func TestReduceValues(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		reducer  string
		expected float64
		errMsg   string
	}{
		{name: "single value short-circuits", values: []float64{42}, reducer: "", expected: 42},
		{name: "avg", values: []float64{1, 2, 3, 4}, reducer: "avg", expected: 2.5},
		{name: "min", values: []float64{3, 1, 2}, reducer: "min", expected: 1},
		{name: "max", values: []float64{3, 1, 7, 2}, reducer: "max", expected: 7},
		{name: "sum", values: []float64{1, 2, 3}, reducer: "sum", expected: 6},
		{name: "last", values: []float64{1, 2, 9}, reducer: "last", expected: 9},
		{name: "multiple values require reducer", values: []float64{1, 2}, reducer: "", errMsg: "no reducer was set"},
		{name: "unknown reducer is rejected", values: []float64{1, 2}, reducer: "median", errMsg: "Unsupported reducer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := reduceValues(tt.values, tt.reducer)
			if tt.errMsg != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}
