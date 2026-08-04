package v1alpha1

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

// TestCRDDescriptionsPreserved validates that generated CRD YAML files contain field descriptions.
// This guards against regressions where maxDescLen=0 is re-added to controller-gen, which strips
// all Go doc comments from CRD schemas and breaks kubectl explain output (see issue #4383).
func TestCRDDescriptionsPreserved(t *testing.T) {
	// Path relative to this test file to the manifests/crds directory
	crdDir := filepath.Join("..", "..", "..", "..", "manifests", "crds")

	crdFiles := map[string]string{
		"Rollout":          "rollout-crd.yaml",
		"AnalysisTemplate": "analysis-template-crd.yaml",
		"AnalysisRun":      "analysis-run-crd.yaml",
		"Experiment":       "experiment-crd.yaml",
	}

	for kind, filename := range crdFiles {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(crdDir, filename)
			data, err := os.ReadFile(path)
			require.NoError(t, err, "failed to read CRD file %s", path)

			var crd extensionsv1.CustomResourceDefinition
			require.NoError(t, yaml.Unmarshal(data, &crd), "failed to parse CRD YAML")

			for _, version := range crd.Spec.Versions {
				require.NotNil(t, version.Schema, "CRD %s has nil schema", kind)
				require.NotNil(t, version.Schema.OpenAPIV3Schema, "CRD %s has nil openAPIV3Schema", kind)

				specProps, ok := version.Schema.OpenAPIV3Schema.Properties["spec"]
				require.True(t, ok, "CRD %s has no spec properties", kind)

				// Count how many spec-level fields have descriptions
				descCount := 0
				for _, prop := range specProps.Properties {
					if prop.Description != "" {
						descCount++
					}
				}

				// Ensure descriptions are present — a count of 0 means they were stripped
				assert.Greater(t, descCount, 0,
					"CRD %s spec has zero field descriptions — controller-gen may be stripping them (check maxDescLen in hack/gen-crd-spec/main.go)", kind)
			}
		})
	}
}
