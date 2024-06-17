// These are tests that don't belong to v1 or v2 API

package datadog

import (
	"log"
	"os"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/stretchr/testify/assert"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/kubernetes/scheme"
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
