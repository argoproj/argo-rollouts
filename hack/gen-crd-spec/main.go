package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/ghodss/yaml"
	extensionsobj "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

const metadataValidation = `properties:
 annotations:
   additionalProperties:
     type: string
   type: object
 labels:
   additionalProperties:
     type: string
   type: object
type: object`

var preserveUnknownFields = map[string]interface{}{
	"x-kubernetes-preserve-unknown-fields": true,
}

var crdPaths = map[string]string{
	"Rollout":                 "manifests/crds/rollout-crd.yaml",
	"Experiment":              "manifests/crds/experiment-crd.yaml",
	"AnalysisTemplate":        "manifests/crds/analysis-template-crd.yaml",
	"ClusterAnalysisTemplate": "manifests/crds/cluster-analysis-template-crd.yaml",
	"AnalysisRun":             "manifests/crds/analysis-run-crd.yaml",
}

func setValidationOverride(un *unstructured.Unstructured, fieldOverride map[string]interface{}, path string) {
	// Prepare variables
	preSchemaPath := []string{"spec", "versions"}
	objVersions, _, _ := unstructured.NestedSlice(un.Object, preSchemaPath...)

	schemaPath := []string{"schema", "openAPIV3Schema"}
	for _, part := range strings.Split(path, ".") {
		if strings.HasSuffix(part, "[]") {
			part = strings.TrimSuffix(part, "[]")
			schemaPath = append(schemaPath, "properties", part, "items")
		} else {
			schemaPath = append(schemaPath, "properties", part)
		}
	}

	// Loop over version's slice
	var finalOverride []interface{}
	for _, v := range objVersions {
		unstructured.SetNestedMap(v.(map[string]interface{}), fieldOverride, schemaPath...)

		_, ok, err := unstructured.NestedFieldNoCopy(v.(map[string]interface{}), schemaPath...)
		checkErr(err)
		if !ok {
			panic(fmt.Sprintf("%s not found for kind %s", schemaPath, crdKind(un)))
		} else {
			finalOverride = append(finalOverride, v)
		}
	}

	// Write back to top object
	unstructured.SetNestedSlice(un.Object, finalOverride, preSchemaPath...)
}

func NewCustomResourceDefinition() []*extensionsobj.CustomResourceDefinition {
	crdYamlBytes, err := exec.Command(
		"controller-gen",
		"paths=./pkg/apis/rollouts/...",
		"crd:trivialVersions=true",
		// The only possible value is 'false' since 'apiextensions.k8s.io/v1'
		// https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#field-pruning
		// It is possible though to opt-out of pruning for specifc sub-trees of fields by adding x-kubernetes-preserve-unknown-fields: true
		// by using the 'setValidationOverride' function in this file.
		"crd:preserveUnknownFields=false",
		"crd:crdVersions=v1",
		"crd:maxDescLen=0",
		"output:crd:stdout",
	).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			fmt.Println(string(exitErr.Stderr))
		}
	}
	checkErr(err)
	if len(crdYamlBytes) == 0 {
		panic("controller-gen produced no output")
	}

	// clean up stuff left by controller-gen
	deleteFile("config/webhook/manifests.yaml")
	deleteFile("config/webhook")
	deleteFile("config/argoproj.io_analysisruns.yaml")
	deleteFile("config/argoproj.io_analysistemplates.yaml")
	deleteFile("config/argoproj.io_clusteranalysistemplates.yaml")
	deleteFile("config/argoproj.io_experiments.yaml")
	deleteFile("config/argoproj.io_rollouts.yaml")
	deleteFile("config")

	crds := []*extensionsobj.CustomResourceDefinition{}
	objs, err := unstructuredutil.SplitYAML(string(crdYamlBytes))
	checkErr(err)

	for i := range objs {
		obj := objs[i]
		removeK8S118Fields(obj)
		createMetadataValidation(obj)
		crd := toCRD(obj)

		if crd.Name == "clusteranalysistemplates.argoproj.io" {
			crd.Spec.Scope = "Cluster"
		} else {
			crd.Spec.Scope = "Namespaced"
		}
		crds = append(crds, crd)
	}

	return crds
}

func crdKind(crd *unstructured.Unstructured) string {
	kind, found, err := unstructured.NestedFieldNoCopy(crd.Object, "spec", "names", "kind")
	checkErr(err)
	if !found {
		panic("kind not found")
	}
	return kind.(string)
}

func deleteFile(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}
	checkErr(os.Remove(path))
}

// createMetadataValidation creates validation checks for metadata in Rollout, Experiment, AnalysisRun and AnalysisTemplate CRDs
func createMetadataValidation(un *unstructured.Unstructured) {
	metadataValidationObj := unstructuredutil.StrToUnstructuredUnsafe(metadataValidation)
	kind := crdKind(un)
	prePath := []string{
		"spec",
		"versions",
	}
	path := []string{
		"schema",
		"openAPIV3Schema",
		"properties",
		"spec",
		"properties",
	}
	objVersions, _, _ := unstructured.NestedSlice(un.Object, prePath...)

	switch kind {
	case "Rollout":
		var roValidated []interface{}
		roPath := []string{
			"template",
			"properties",
			"metadata",
		}
		roPath = append(path, roPath...)
		for _, v := range objVersions {
			unstructured.SetNestedMap(v.(map[string]interface{}), metadataValidationObj.Object, roPath...)
			roValidated = append(roValidated, v)
		}
		unstructured.SetNestedSlice(un.Object, roValidated, prePath...)
	case "Experiment":
		var exValidated []interface{}
		exPath := []string{
			"templates",
			"items",
			"properties",
			"template",
			"properties",
			"metadata",
		}
		exPath = append(path, exPath...)
		for _, v := range objVersions {
			unstructured.SetNestedMap(v.(map[string]interface{}), metadataValidationObj.Object, exPath...)
			exValidated = append(exValidated, v)
		}
		unstructured.SetNestedSlice(un.Object, exValidated, prePath...)
	case "ClusterAnalysisTemplate", "AnalysisTemplate", "AnalysisRun":
		var analysisValidated []interface{}
		analysisPath := []string{
			"metrics",
			"items",
			"properties",
			"provider",
			"properties",
			"job",
			"properties",
		}
		analysisPath = append(path, analysisPath...)

		analysisPathJobMetadata := append(analysisPath, "metadata")
		for _, v := range objVersions {
			unstructured.SetNestedMap(v.(map[string]interface{}), metadataValidationObj.Object, analysisPathJobMetadata...)
			analysisValidated = append(analysisValidated, v)
		}
		unstructured.SetNestedSlice(un.Object, analysisValidated, prePath...)

		var analysisJobValidated []interface{}
		analysisPathJobTemplateMetadata := []string{
			"spec",
			"properties",
			"template",
			"properties",
			"metadata",
		}
		analysisPathJobTemplateMetadata = append(analysisPath, analysisPathJobTemplateMetadata...)
		for _, v := range objVersions {
			unstructured.SetNestedMap(v.(map[string]interface{}), metadataValidationObj.Object, analysisPathJobTemplateMetadata...)
			analysisJobValidated = append(analysisJobValidated, v)
		}
		unstructured.SetNestedSlice(un.Object, analysisJobValidated, prePath...)
	default:
		panic(fmt.Sprintf("unknown kind: %s", kind))
	}
}

func removeFieldHelper(obj map[string]interface{}, fieldName string) {
	for k, v := range obj {
		if k == fieldName {
			delete(obj, k)
			continue
		}
		if vObj, ok := v.(map[string]interface{}); ok {
			removeFieldHelper(vObj, fieldName)
		}
	}
}

func removeK8S118Fields(un *unstructured.Unstructured) {
	kind := crdKind(un)
	switch kind {
	case "Rollout":
		setValidationOverride(un, preserveUnknownFields, "spec.template.spec.containers[].resources.limits")
		setValidationOverride(un, preserveUnknownFields, "spec.template.spec.containers[].resources.requests")
		setValidationOverride(un, preserveUnknownFields, "spec.template.spec.initContainers[].resources.limits")
		setValidationOverride(un, preserveUnknownFields, "spec.template.spec.initContainers[].resources.requests")
		setValidationOverride(un, preserveUnknownFields, "spec.template.spec.ephemeralContainers[].resources.limits")
		setValidationOverride(un, preserveUnknownFields, "spec.template.spec.ephemeralContainers[].resources.requests")
		// Replace this with "spec.template.spec.volumes[].ephemeral.volumeClaimTemplate.spec.resources.{limits/requests}"
		// when it's ok to only support k8s 1.17+
		setValidationOverride(un, preserveUnknownFields, "spec.template.spec.volumes")
	case "Experiment":
		setValidationOverride(un, preserveUnknownFields, "spec.templates[].template.spec.containers[].resources.limits")
		setValidationOverride(un, preserveUnknownFields, "spec.templates[].template.spec.containers[].resources.requests")
		setValidationOverride(un, preserveUnknownFields, "spec.templates[].template.spec.initContainers[].resources.limits")
		setValidationOverride(un, preserveUnknownFields, "spec.templates[].template.spec.initContainers[].resources.requests")
		setValidationOverride(un, preserveUnknownFields, "spec.templates[].template.spec.ephemeralContainers[].resources.limits")
		setValidationOverride(un, preserveUnknownFields, "spec.templates[].template.spec.ephemeralContainers[].resources.requests")
		// Replace this with "spec.templates[].template.spec.volumes[].ephemeral.volumeClaimTemplate.spec.resources.{limits/requests}"
		// when it's ok to only support k8s 1.17+
		setValidationOverride(un, preserveUnknownFields, "spec.templates[].template.spec.volumes")
	case "ClusterAnalysisTemplate", "AnalysisTemplate", "AnalysisRun":
		setValidationOverride(un, preserveUnknownFields, "spec.metrics[].provider.job.spec.template.spec.containers[].resources.limits")
		setValidationOverride(un, preserveUnknownFields, "spec.metrics[].provider.job.spec.template.spec.containers[].resources.requests")
		setValidationOverride(un, preserveUnknownFields, "spec.metrics[].provider.job.spec.template.spec.initContainers[].resources.limits")
		setValidationOverride(un, preserveUnknownFields, "spec.metrics[].provider.job.spec.template.spec.initContainers[].resources.requests")
		setValidationOverride(un, preserveUnknownFields, "spec.metrics[].provider.job.spec.template.spec.ephemeralContainers[].resources.limits")
		setValidationOverride(un, preserveUnknownFields, "spec.metrics[].provider.job.spec.template.spec.ephemeralContainers[].resources.requests")
		// Replace this with "spec.metrics[].provider.job.spec.template.spec.volumes[].ephemeral.volumeClaimTemplate.spec.resources.{limits/requests}"
		// when it's ok to only support k8s 1.17+
		setValidationOverride(un, preserveUnknownFields, "spec.metrics[].provider.job.spec.template.spec.volumes")
	default:
		panic(fmt.Sprintf("unknown kind: %s", kind))
	}
}

func toCRD(un *unstructured.Unstructured) *extensionsobj.CustomResourceDefinition {
	unBytes, err := json.Marshal(un)
	checkErr(err)

	var crd extensionsobj.CustomResourceDefinition
	err = json.Unmarshal(unBytes, &crd)
	checkErr(err)

	return &crd
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

// Generate CRD spec for Rollout Resource
func main() {
	crds := NewCustomResourceDefinition()
	for i := range crds {
		crd := crds[i]
		crdKind := crd.Spec.Names.Kind
		jsonBytes, err := json.Marshal(crd)
		checkErr(err)

		var r unstructured.Unstructured
		err = json.Unmarshal(jsonBytes, &r.Object)
		checkErr(err)

		// Need to explicitly set spec.preserveUnknownFields to false, despite false being the
		// default value in v1, in order to facilitate upgrades from apiextensions.k8s.io/v1beta1 v1.
		// See https://github.com/argoproj/argo-rollouts/issues/1067
		unstructured.SetNestedField(r.Object, false, "spec", "preserveUnknownFields")

		// clean up crd yaml before marshalling
		unstructured.RemoveNestedField(r.Object, "status")
		unstructured.RemoveNestedField(r.Object, "metadata", "creationTimestamp")
		jsonBytes, err = json.MarshalIndent(r.Object, "", "    ")
		checkErr(err)

		yamlBytes, err := yaml.JSONToYAML(jsonBytes)
		checkErr(err)

		path := crdPaths[crdKind]
		if path == "" {
			panic(fmt.Sprintf("unknown kind: %s", crdKind))
		}
		err = ioutil.WriteFile(path, yamlBytes, 0644)
		checkErr(err)
	}
}
