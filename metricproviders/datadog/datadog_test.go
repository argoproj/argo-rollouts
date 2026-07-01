// These are tests that don't belong to v1 or v2 API

package datadog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
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

func rawJSON(values ...string) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(values))
	for _, v := range values {
		out = append(out, json.RawMessage(v))
	}
	return out
}

func TestExtractValues(t *testing.T) {
	// Group columns are arrays of tag values per group on the wire
	// (`[][]string`), e.g. `["GET /a"]` for `by {resource_name}`. The fixtures
	// below use that real shape so the tests exercise the same parsing path
	// the live Datadog API does — a flat `"a"` would never reach this code.
	t.Run("ungrouped numeric column", func(t *testing.T) {
		cols := []datadogV2Column{
			{Type: "number", Name: "q", Values: rawJSON(`1.5`, `2`)},
		}
		values, groups, grouped, err := extractValues(cols)
		assert.NoError(t, err)
		assert.Equal(t, []float64{1.5, 2}, values)
		assert.Nil(t, groups)
		assert.False(t, grouped)
	})

	t.Run("legacy column with no type field is treated as numeric", func(t *testing.T) {
		cols := []datadogV2Column{
			{Type: "", Values: rawJSON(`0.5`)},
		}
		values, groups, grouped, err := extractValues(cols)
		assert.NoError(t, err)
		assert.Equal(t, []float64{0.5}, values)
		assert.Nil(t, groups)
		assert.False(t, grouped)
	})

	t.Run("grouped query pairs numeric values with group names", func(t *testing.T) {
		cols := []datadogV2Column{
			{Type: "group", Values: rawJSON(`["a"]`, `["b"]`)},
			{Type: "number", Values: rawJSON(`1`, `2`)},
		}
		values, groups, grouped, err := extractValues(cols)
		assert.NoError(t, err)
		assert.Equal(t, []float64{1, 2}, values)
		assert.Equal(t, []groupedValue{{Name: "a", Value: 1}, {Name: "b", Value: 2}}, groups)
		assert.True(t, grouped)
	})

	t.Run("multi-tag grouping joins across the per-tag group columns", func(t *testing.T) {
		// `by {env, resource_name}` returns one group column per tag; the label
		// is joined across them. A single column with multi-element rows is not
		// a shape the API sends for multi-tag.
		cols := []datadogV2Column{
			{Type: "group", Values: rawJSON(`["prod"]`, `["prod"]`)},
			{Type: "group", Values: rawJSON(`["GET /a"]`, `["GET /b"]`)},
			{Type: "number", Values: rawJSON(`1`, `2`)},
		}
		_, groups, grouped, err := extractValues(cols)
		assert.NoError(t, err)
		assert.Equal(t, []groupedValue{{Name: "prod,GET /a", Value: 1}, {Name: "prod,GET /b", Value: 2}}, groups)
		assert.True(t, grouped)
	})

	t.Run("null group label errors rather than pairing a blank name", func(t *testing.T) {
		// A JSON null (or []) in a group column parses without error but yields
		// no tag, which would silently pair the value with an empty label.
		// The contract says that's malformed, so extractValues must error.
		cols := []datadogV2Column{
			{Type: "group", Name: "g", Values: rawJSON(`["a"]`, `null`)},
			{Type: "number", Values: rawJSON(`1`, `2`)},
		}
		_, _, _, err := extractValues(cols)
		assert.ErrorContains(t, err, "could not parse group label")
	})

	t.Run("single-group query is still grouped, not a scalar", func(t *testing.T) {
		// A `by {tag}` query that matches one group must report grouped=true so
		// the caller returns a slice and group metadata, not a bare scalar.
		cols := []datadogV2Column{
			{Type: "group", Values: rawJSON(`["only"]`)},
			{Type: "number", Values: rawJSON(`0.03`)},
		}
		values, groups, grouped, err := extractValues(cols)
		assert.NoError(t, err)
		assert.Equal(t, []float64{0.03}, values)
		assert.Equal(t, []groupedValue{{Name: "only", Value: 0.03}}, groups)
		assert.True(t, grouped)
	})

	t.Run("null entries skip their group entry to keep pairing aligned", func(t *testing.T) {
		// values[0] is null → first group ("a") must also be dropped, NOT
		// shifted onto values[1].
		cols := []datadogV2Column{
			{Type: "group", Values: rawJSON(`["a"]`, `["b"]`, `["c"]`)},
			{Type: "number", Values: rawJSON(`null`, `0.02`, `0.03`)},
		}
		values, groups, grouped, err := extractValues(cols)
		assert.NoError(t, err)
		assert.Equal(t, []float64{0.02, 0.03}, values)
		assert.Equal(t, []groupedValue{{Name: "b", Value: 0.02}, {Name: "c", Value: 0.03}}, groups)
		assert.True(t, grouped)
	})

	t.Run("non-numeric entry in number column errors loudly", func(t *testing.T) {
		// Datadog shouldn't return a string in a number column, but if it
		// does we'd rather fail the analysis than silently drop the value
		// and ship a rollout against incomplete data.
		cols := []datadogV2Column{
			{Type: "number", Name: "q", Values: rawJSON(`1`, `"oops"`)},
		}
		_, _, _, err := extractValues(cols)
		assert.ErrorContains(t, err, "could not parse numeric value")
	})

	t.Run("grouped query with an unparseable group label errors rather than misaligning", func(t *testing.T) {
		// A group label we can't read would otherwise drop a group while
		// keeping its value, silently shifting every later (name, value) pair.
		// Fail loudly instead.
		cols := []datadogV2Column{
			{Type: "group", Name: "g", Values: rawJSON(`["a"]`, `42`)},
			{Type: "number", Values: rawJSON(`1`, `2`)},
		}
		_, _, _, err := extractValues(cols)
		assert.ErrorContains(t, err, "could not parse group label")
	})

	t.Run("group-only response (no number column) returns no values", func(t *testing.T) {
		cols := []datadogV2Column{
			{Type: "group", Values: rawJSON(`["a"]`, `["b"]`)},
		}
		values, groups, grouped, err := extractValues(cols)
		assert.NoError(t, err)
		assert.Empty(t, values)
		assert.Empty(t, groups)
		assert.True(t, grouped)
	})

	t.Run("group column placed after number column still works", func(t *testing.T) {
		cols := []datadogV2Column{
			{Type: "number", Values: rawJSON(`1`, `2`)},
			{Type: "group", Values: rawJSON(`["a"]`, `["b"]`)},
		}
		values, groups, grouped, err := extractValues(cols)
		assert.NoError(t, err)
		assert.Equal(t, []float64{1, 2}, values)
		assert.Equal(t, []groupedValue{{Name: "a", Value: 1}, {Name: "b", Value: 2}}, groups)
		assert.True(t, grouped)
	})
}

func TestGroupNameAt(t *testing.T) {
	// Two group columns (`by {region, host}`), plus a single column carrying
	// aliased values, a non-array, and a null to exercise the edge cases.
	region := &datadogV2Column{Type: "group", Values: rawJSON(`["us-east"]`, `["us-west"]`)}
	host := &datadogV2Column{Type: "group", Values: rawJSON(`["host-a"]`, `["host-b"]`)}
	edge := &datadogV2Column{Type: "group", Values: rawJSON(`["x","y"]`, `42`, `null`, `[]`)}

	t.Run("index past end returns false", func(t *testing.T) {
		_, ok := groupNameAt([]*datadogV2Column{region}, 99)
		assert.False(t, ok)
	})

	t.Run("single group column returns the name", func(t *testing.T) {
		name, ok := groupNameAt([]*datadogV2Column{region}, 0)
		assert.True(t, ok)
		assert.Equal(t, "us-east", name)
	})

	t.Run("joins across per-tag group columns", func(t *testing.T) {
		name, ok := groupNameAt([]*datadogV2Column{region, host}, 1)
		assert.True(t, ok)
		assert.Equal(t, "us-west,host-b", name)
	})

	t.Run("joins across six group columns in order", func(t *testing.T) {
		cols := []*datadogV2Column{
			{Type: "group", Values: rawJSON(`["us-east"]`)},
			{Type: "group", Values: rawJSON(`["host-a"]`)},
			{Type: "group", Values: rawJSON(`["/checkout"]`)},
			{Type: "group", Values: rawJSON(`["GET"]`)},
			{Type: "group", Values: rawJSON(`["prod"]`)},
			{Type: "group", Values: rawJSON(`["v2"]`)},
		}
		name, ok := groupNameAt(cols, 0)
		assert.True(t, ok)
		assert.Equal(t, "us-east,host-a,/checkout,GET,prod,v2", name)
	})

	t.Run("aliased values within one dimension join too", func(t *testing.T) {
		name, ok := groupNameAt([]*datadogV2Column{edge}, 0)
		assert.True(t, ok)
		assert.Equal(t, "x,y", name)
	})

	t.Run("non-array entry returns false rather than corrupting the label", func(t *testing.T) {
		_, ok := groupNameAt([]*datadogV2Column{edge}, 1)
		assert.False(t, ok)
	})

	t.Run("null entry returns false", func(t *testing.T) {
		_, ok := groupNameAt([]*datadogV2Column{edge}, 2)
		assert.False(t, ok)
	})

	t.Run("empty array entry returns false", func(t *testing.T) {
		_, ok := groupNameAt([]*datadogV2Column{edge}, 3)
		assert.False(t, ok)
	})

	t.Run("a row missing from a later column returns false", func(t *testing.T) {
		short := &datadogV2Column{Type: "group", Values: rawJSON(`["only"]`)}
		_, ok := groupNameAt([]*datadogV2Column{region, short}, 1)
		assert.False(t, ok)
	})
}

func TestGroupsMetadata(t *testing.T) {
	t.Run("small group list is returned in full, not truncated", func(t *testing.T) {
		groups := []groupedValue{{Name: "a", Value: 1}, {Name: "b", Value: 2}}
		out, truncated := groupsMetadata(groups)
		assert.False(t, truncated)
		assert.JSONEq(t, `[{"name":"a","value":1},{"name":"b","value":2}]`, out)
	})

	t.Run("exactly the cap is not truncated", func(t *testing.T) {
		// Boundary: the trim triggers on `> cap`, so a list of exactly cap
		// entries must pass through whole. Guards against an off-by-one (`>=`).
		groups := make([]groupedValue, maxGroupsInMetadata)
		for i := range groups {
			groups[i] = groupedValue{Name: "g", Value: float64(i)}
		}
		_, truncated := groupsMetadata(groups)
		assert.False(t, truncated)
	})

	t.Run("high-cardinality list is capped to the highest-valued groups", func(t *testing.T) {
		// One more than the cap: the single smallest-valued group must be the
		// one dropped, and the result flagged as truncated.
		groups := make([]groupedValue, 0, maxGroupsInMetadata+1)
		groups = append(groups, groupedValue{Name: "smallest", Value: -1})
		for i := 0; i < maxGroupsInMetadata; i++ {
			groups = append(groups, groupedValue{Name: "keep", Value: float64(i)})
		}
		out, truncated := groupsMetadata(groups)
		assert.True(t, truncated)

		var kept []groupedValue
		assert.NoError(t, json.Unmarshal([]byte(out), &kept))
		assert.Len(t, kept, maxGroupsInMetadata)
		for _, g := range kept {
			assert.NotEqual(t, "smallest", g.Name, "the lowest-valued group should have been dropped")
		}
	})
}

// errReader is an io.Reader that always returns an error - used to simulate
// a torn-down HTTP response body in tests.
type errReader struct{}

func (*errReader) Read([]byte) (int, error) { return 0, errors.New("forced read error") }

func TestParseResponseV2TruncatesHighCardinalityGroups(t *testing.T) {
	// End-to-end wiring: a response with more groups than the cap must come out
	// of parseResponseV2 with metadata.groups trimmed and groups_truncated set,
	// while the evaluated result slice keeps every value (the cap is display
	// only and must never change what the gate sees).
	const n = maxGroupsInMetadata + 25
	groupVals := make([]string, n)
	numVals := make([]string, n)
	for i := 0; i < n; i++ {
		groupVals[i] = fmt.Sprintf(`["g%d"]`, i)
		numVals[i] = strconv.Itoa(i)
	}
	body := fmt.Sprintf(
		`{"data":{"attributes":{"columns":[{"name":"host","type":"group","values":[%s]},{"name":"q","type":"number","values":[%s]}]}}}`,
		strings.Join(groupVals, ","), strings.Join(numVals, ","),
	)

	p := &Provider{logCtx: *log.WithField("test", "test")}
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	metric := v1alpha1.Metric{Name: "high-cardinality", SuccessCondition: "max(result) < 1000000"}

	value, _, metadata, err := p.parseResponseV2(metric, resp)
	assert.NoError(t, err)
	assert.Equal(t, "true", metadata["groups_truncated"])

	var kept []groupedValue
	assert.NoError(t, json.Unmarshal([]byte(metadata["groups"]), &kept))
	assert.Len(t, kept, maxGroupsInMetadata)

	// The result slice itself is untouched: all n values are still evaluated.
	assert.Equal(t, n, strings.Count(value, ",")+1)
}

func TestParseResponseV2BodyReadError(t *testing.T) {
	p := &Provider{logCtx: *log.WithField("test", "test")}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(&errReader{}),
	}
	_, phase, _, err := p.parseResponseV2(v1alpha1.Metric{}, resp)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, phase)
	assert.ErrorContains(t, err, "Received no bytes in response")
}

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
