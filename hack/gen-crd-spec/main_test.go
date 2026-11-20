package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func envSchema(withMarkers bool) map[string]any {
	env := map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":      map[string]any{"type": "string"},
				"value":     map[string]any{"type": "string"},
				"valueFrom": map[string]any{"type": "object"},
			},
		},
	}
	if withMarkers {
		env["x-kubernetes-list-type"] = "map"
		env["x-kubernetes-list-map-keys"] = []any{"name"}
	}
	return env
}

func TestRemoveEnvListType(t *testing.T) {
	t.Run("removes the markers from the env list of every kind of container", func(t *testing.T) {
		container := func() map[string]any {
			return map[string]any{
				"type":       "object",
				"properties": map[string]any{"env": envSchema(true)},
			}
		}
		podSpec := map[string]any{
			"properties": map[string]any{
				"containers":          map[string]any{"type": "array", "items": container()},
				"initContainers":      map[string]any{"type": "array", "items": container()},
				"ephemeralContainers": map[string]any{"type": "array", "items": container()},
			},
		}

		assert.Equal(t, 6, removeEnvListType(podSpec))
		assert.Equal(t, 0, removeEnvListType(podSpec), "the second run has nothing left to remove")

		for _, name := range []string{"containers", "initContainers", "ephemeralContainers"} {
			list := podSpec["properties"].(map[string]any)[name].(map[string]any)
			env := list["items"].(map[string]any)["properties"].(map[string]any)["env"].(map[string]any)
			assert.NotContains(t, env, "x-kubernetes-list-type", name)
			assert.NotContains(t, env, "x-kubernetes-list-map-keys", name)
			assert.Equal(t, "array", env["type"], name)
		}
	})

	t.Run("keeps the markers of other lists", func(t *testing.T) {
		containers := map[string]any{
			"type":                       "array",
			"x-kubernetes-list-type":     "map",
			"x-kubernetes-list-map-keys": []any{"name"},
			"items": map[string]any{
				"properties": map[string]any{
					"env": envSchema(true),
					"ports": map[string]any{
						"type":                       "array",
						"x-kubernetes-list-type":     "map",
						"x-kubernetes-list-map-keys": []any{"containerPort", "protocol"},
					},
				},
			},
		}
		schema := map[string]any{"properties": map[string]any{"containers": containers}}

		assert.Equal(t, 2, removeEnvListType(schema))

		assert.Equal(t, "map", containers["x-kubernetes-list-type"])
		assert.Equal(t, []any{"name"}, containers["x-kubernetes-list-map-keys"])
		ports := containers["items"].(map[string]any)["properties"].(map[string]any)["ports"].(map[string]any)
		assert.Equal(t, "map", ports["x-kubernetes-list-type"])
		assert.Equal(t, []any{"containerPort", "protocol"}, ports["x-kubernetes-list-map-keys"])
	})

	t.Run("ignores env properties that are not the env list of a container", func(t *testing.T) {
		notContainerEnv := map[string]any{
			"type":                   "array",
			"x-kubernetes-list-type": "map",
			"items": map[string]any{
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			},
		}
		schema := map[string]any{"properties": map[string]any{"env": notContainerEnv}}

		assert.Equal(t, 0, removeEnvListType(schema))
		assert.Equal(t, "map", notContainerEnv["x-kubernetes-list-type"])
	})

	t.Run("does not touch env lists without markers", func(t *testing.T) {
		schema := map[string]any{"properties": map[string]any{"env": envSchema(false)}}

		assert.Equal(t, 0, removeEnvListType(schema))
	})
}

// TestGeneratedCRDsAcceptDuplicateEnvNames checks the checked in CRDs. A key based list type on the env
// list makes the API server reject pod templates that set the same variable twice, which a Deployment accepts.
func TestGeneratedCRDsAcceptDuplicateEnvNames(t *testing.T) {
	paths, err := filepath.Glob("../../manifests/crds/*-crd.yaml")
	require.NoError(t, err)
	require.Len(t, paths, len(crdPaths))

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			var crd map[string]any
			require.NoError(t, yaml.Unmarshal(data, &crd))

			// Run the clean up on the checked in CRD: it must not find anything left to remove,
			// but it must have something to look at.
			assert.Equal(t, 0, removeEnvListType(crd), "run `make gen-crd` to regenerate the CRDs")
			assert.Positive(t, countContainerEnvLists(crd), "no container env list found in %s", path)
		})
	}
}

func countContainerEnvLists(node any) int {
	count := 0
	switch n := node.(type) {
	case map[string]any:
		if props, ok := n["properties"].(map[string]any); ok {
			if env, ok := props["env"].(map[string]any); ok && isContainerEnvSchema(env) {
				count++
			}
		}
		for _, child := range n {
			count += countContainerEnvLists(child)
		}
	case []any:
		for _, child := range n {
			count += countContainerEnvLists(child)
		}
	}
	return count
}
