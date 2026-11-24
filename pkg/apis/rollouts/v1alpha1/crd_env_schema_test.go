package v1alpha1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	extensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/yaml"
)

// schemaAt walks a dotted field path, where "field[]" steps into list items.
func schemaAt(t *testing.T, schema extensionsv1.JSONSchemaProps, path string) extensionsv1.JSONSchemaProps {
	t.Helper()
	for _, part := range strings.Split(path, ".") {
		field, isList := strings.CutSuffix(part, "[]")
		next, ok := schema.Properties[field]
		require.True(t, ok, "field %q not found in %s", field, path)
		schema = next
		if isList {
			require.NotNil(t, schema.Items, "%q has no items in %s", field, path)
			require.NotNil(t, schema.Items.Schema, "%q has no item schema in %s", field, path)
			schema = *schema.Items.Schema
		}
	}
	return schema
}

// TestCRDEnvListsAllowDuplicateNames guards against the env list markers from
// k8s.io/api coming back on regeneration. On a CRD they make the API server
// reject a repeated env var name that a Deployment accepts (see issue #5201).
func TestCRDEnvListsAllowDuplicateNames(t *testing.T) {
	crdDir := filepath.Join("..", "..", "..", "..", "manifests", "crds")

	podSpecPaths := map[string]string{
		"rollout-crd.yaml":                   "spec.template.spec",
		"experiment-crd.yaml":                "spec.templates[].template.spec",
		"analysis-template-crd.yaml":         "spec.metrics[].provider.job.spec.template.spec",
		"cluster-analysis-template-crd.yaml": "spec.metrics[].provider.job.spec.template.spec",
		"analysis-run-crd.yaml":              "spec.metrics[].provider.job.spec.template.spec",
	}

	for filename, podSpecPath := range podSpecPaths {
		t.Run(filename, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(crdDir, filename))
			require.NoError(t, err)

			var crd extensionsv1.CustomResourceDefinition
			require.NoError(t, yaml.Unmarshal(data, &crd))

			for _, version := range crd.Spec.Versions {
				require.NotNil(t, version.Schema)
				require.NotNil(t, version.Schema.OpenAPIV3Schema)

				for _, containers := range []string{"containers", "initContainers", "ephemeralContainers"} {
					env := schemaAt(t, *version.Schema.OpenAPIV3Schema, podSpecPath+"."+containers+"[].env")
					assert.Nil(t, env.XListType, "%s %s env must not be a map-type list (check hack/gen-crd-spec)", filename, containers)
					assert.Empty(t, env.XListMapKeys, "%s %s env must not have list map keys (check hack/gen-crd-spec)", filename, containers)
				}
			}
		})
	}
}
