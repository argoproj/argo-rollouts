package main

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// analysisJobPodSpecPath is the job pod spec shared by the analysis CRDs.
const analysisJobPodSpecPath = "spec.metrics[].provider.job.spec.template.spec"

// podSpecPaths locates the embedded pod spec in each CRD, using the same
// "field[]" notation as setValidationOverride.
var podSpecPaths = map[string]string{
	"Rollout":                 "spec.template.spec",
	"Experiment":              "spec.templates[].template.spec",
	"AnalysisTemplate":        analysisJobPodSpecPath,
	"ClusterAnalysisTemplate": analysisJobPodSpecPath,
	"AnalysisRun":             analysisJobPodSpecPath,
}

var containerListFields = []string{"containers", "initContainers", "ephemeralContainers"}

// allowDuplicateEnvNames removes the list-map markers that controller-gen copies
// from k8s.io/api onto container env lists. On a CRD the API server enforces
// unique keys in a map-type list, so a Rollout repeating an env var name was
// rejected even though a Deployment with the same pod template is accepted
// (the kubelet uses the last value). Without the markers env is an atomic list
// for server-side apply, as it was before v1.9.0 (#5201).
func allowDuplicateEnvNames(un *unstructured.Unstructured) {
	kind := crdKind(un)
	podSpecPath, ok := podSpecPaths[kind]
	if !ok {
		panic(fmt.Sprintf("unknown kind: %s", kind))
	}

	versions, _, err := unstructured.NestedSlice(un.Object, "spec", "versions")
	checkErr(err)
	for _, v := range versions {
		version := v.(map[string]any)
		for _, containers := range containerListFields {
			envPath := schemaPropertyPath(podSpecPath + "." + containers + "[].env")
			env, found, err := unstructured.NestedMap(version, envPath...)
			checkErr(err)
			if !found {
				panic(fmt.Sprintf("%s not found for kind %s", envPath, kind))
			}
			delete(env, "x-kubernetes-list-type")
			delete(env, "x-kubernetes-list-map-keys")
			checkErr(unstructured.SetNestedMap(version, env, envPath...))
		}
	}
	checkErr(unstructured.SetNestedSlice(un.Object, versions, "spec", "versions"))
}

// schemaPropertyPath converts a dotted field path into the nested keys of a
// CRD version's openAPIV3Schema.
func schemaPropertyPath(path string) []string {
	schemaPath := []string{"schema", "openAPIV3Schema"}
	for _, part := range strings.Split(path, ".") {
		field, isList := strings.CutSuffix(part, "[]")
		schemaPath = append(schemaPath, "properties", field)
		if isList {
			schemaPath = append(schemaPath, "items")
		}
	}
	return schemaPath
}
