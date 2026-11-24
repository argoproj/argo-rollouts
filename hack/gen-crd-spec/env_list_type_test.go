package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func listMapSchema(keys ...any) map[string]any {
	return map[string]any{
		"type":                       "array",
		"x-kubernetes-list-type":     "map",
		"x-kubernetes-list-map-keys": keys,
	}
}

// newTestCRD builds a CRD whose pod spec has map-typed env and ports lists on
// every container list, as controller-gen generates them.
func newTestCRD(t *testing.T, kind string) *unstructured.Unstructured {
	t.Helper()
	version := map[string]any{"name": "v1alpha1"}
	for _, containers := range containerListFields {
		containerPath := podSpecPaths[kind] + "." + containers + "[]"
		require.NoError(t, unstructured.SetNestedMap(version, listMapSchema("name"), schemaPropertyPath(containerPath+".env")...))
		require.NoError(t, unstructured.SetNestedMap(version, listMapSchema("containerPort", "protocol"), schemaPropertyPath(containerPath+".ports")...))
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"names":    map[string]any{"kind": kind},
			"versions": []any{version},
		},
	}}
}

func nestedSchema(t *testing.T, un *unstructured.Unstructured, path string) map[string]any {
	t.Helper()
	versions, _, err := unstructured.NestedSlice(un.Object, "spec", "versions")
	require.NoError(t, err)
	require.Len(t, versions, 1)
	schema, found, err := unstructured.NestedMap(versions[0].(map[string]any), schemaPropertyPath(path)...)
	require.NoError(t, err)
	require.True(t, found, "%s not found", path)
	return schema
}

func TestAllowDuplicateEnvNames(t *testing.T) {
	for kind, podSpecPath := range podSpecPaths {
		t.Run(kind, func(t *testing.T) {
			crd := newTestCRD(t, kind)

			allowDuplicateEnvNames(crd)

			for _, containers := range containerListFields {
				containerPath := podSpecPath + "." + containers + "[]"
				env := nestedSchema(t, crd, containerPath+".env")
				assert.Equal(t, map[string]any{"type": "array"}, env, "%s env should be a plain list", containers)

				ports := nestedSchema(t, crd, containerPath+".ports")
				assert.Equal(t, listMapSchema("containerPort", "protocol"), ports, "%s ports should keep its markers", containers)
			}
		})
	}
}

func TestAllowDuplicateEnvNamesPanicsWhenEnvIsMissing(t *testing.T) {
	crd := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"names":    map[string]any{"kind": "Rollout"},
			"versions": []any{map[string]any{"name": "v1alpha1"}},
		},
	}}

	assert.Panics(t, func() { allowDuplicateEnvNames(crd) })
}

func TestAllowDuplicateEnvNamesPanicsOnUnknownKind(t *testing.T) {
	crd := newTestCRD(t, "Rollout")
	require.NoError(t, unstructured.SetNestedField(crd.Object, "Unknown", "spec", "names", "kind"))

	assert.Panics(t, func() { allowDuplicateEnvNames(crd) })
}

func TestSchemaPropertyPath(t *testing.T) {
	assert.Equal(t,
		[]string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "templates", "items", "properties", "env"},
		schemaPropertyPath("spec.templates[].env"),
	)
}
