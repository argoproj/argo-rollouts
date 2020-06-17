package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/ghodss/yaml"
	extensionsobj "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
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

var crdPaths = map[string]string{
	"Rollout":          "manifests/crds/rollout-crd.yaml",
	"Experiment":       "manifests/crds/experiment-crd.yaml",
	"AnalysisTemplate": "manifests/crds/analysis-template-crd.yaml",
	"AnalysisRun":      "manifests/crds/analysis-run-crd.yaml",
}

func removeValidation(un *unstructured.Unstructured, path string) {
	schemaPath := []string{"spec", "validation", "openAPIV3Schema"}
	for _, part := range strings.Split(path, ".") {
		if strings.HasSuffix(part, "[]") {
			part = strings.TrimSuffix(part, "[]")
			schemaPath = append(schemaPath, "properties", part, "items")
		} else {
			schemaPath = append(schemaPath, "properties", part)
		}
	}
	_, ok, err := unstructured.NestedFieldNoCopy(un.Object, schemaPath...)
	checkErr(err)
	if !ok {
		panic(fmt.Sprintf("%s not found for kind %s", schemaPath, crdKind(un)))
	}
	unstructured.RemoveNestedField(un.Object, schemaPath...)
}

func NewCustomResourceDefinition() []*extensionsobj.CustomResourceDefinition {
	crdYamlBytes, err := exec.Command(
		"controller-gen",
		"paths=./pkg/apis/rollouts/...",
		"crd:trivialVersions=true",
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
	deleteFile("config")

	crds := []*extensionsobj.CustomResourceDefinition{}
	objs := splitYAML(string(crdYamlBytes))

	for i := range objs {
		obj := objs[i]
		removeNestedItems(obj)
		removeDescriptions(obj)
		removeK8S118Fields(obj)
		createMetadataValidation(obj)
		crd := toCRD(obj)
		crd.Spec.Scope = "Namespaced"
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

var diffSeparator = regexp.MustCompile(`\n---`)

// splitYAML splits a YAML file into unstructured objects. Returns list of all unstructured objects
// found in the yaml. Panics if any errors occurred.
func splitYAML(out string) []*unstructured.Unstructured {
	parts := diffSeparator.Split(out, -1)
	var objs []*unstructured.Unstructured
	for _, part := range parts {
		var objMap map[string]interface{}
		err := yaml.Unmarshal([]byte(part), &objMap)
		checkErr(err)
		if len(objMap) == 0 {
			// handles case where theres no content between `---`
			continue
		}
		var obj unstructured.Unstructured
		err = yaml.Unmarshal([]byte(part), &obj)
		checkErr(err)
		objs = append(objs, &obj)
	}
	return objs
}

func deleteFile(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}
	checkErr(os.Remove(path))
}

func createMetadataValidation(un *unstructured.Unstructured) {
	obj := unstructuredutil.StrToUnstructuredUnsafe(metadataValidation)
	kind := crdKind(un)
	path := []string{
		"spec",
		"validation",
		"openAPIV3Schema",
		"properties",
		"spec",
		"properties",
	}
	switch kind {
	case "Rollout":
		roPath := []string{
			"template",
			"properties",
			"metadata",
		}
		roPath = append(path, roPath...)
		unstructured.SetNestedMap(un.Object, obj.Object, roPath...)
	case "Experiment":
		exPath := []string{
			"templates",
			"items",
			"properties",
			"template",
			"properties",
			"metadata",
		}
		exPath = append(path, exPath...)
		unstructured.SetNestedMap(un.Object, obj.Object, exPath...)
	case "AnalysisTemplate", "AnalysisRun":
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
		unstructured.SetNestedMap(un.Object, obj.Object, analysisPathJobMetadata...)

		analysisPathJobTemplateMetadata := []string{
			"spec",
			"properties",
			"template",
			"properties",
			"metadata",
		}
		analysisPathJobTemplateMetadata = append(analysisPath, analysisPathJobTemplateMetadata...)
		unstructured.SetNestedMap(un.Object, obj.Object, analysisPathJobTemplateMetadata...)
	default:
		panic(fmt.Sprintf("unknown kind: %s", kind))
	}
}

// removeDescriptions removes all descriptions which bloats the API spec
func removeDescriptions(un *unstructured.Unstructured) {
	validation, _, _ := unstructured.NestedMap(un.Object, "spec", "validation", "openAPIV3Schema")
	removeFieldHelper(validation, "description")
	unstructured.SetNestedMap(un.Object, validation, "spec", "validation", "openAPIV3Schema")
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

// removeNestedItems completely removes validation for a field  whenever 'items' is used as a sub field name.
// This is due to Kubernetes' inability to properly validate objects with fields with the name 'items'
// (e.g. spec.template.spec.volumes.configMap)
func removeNestedItems(un *unstructured.Unstructured) {
	validation, _, _ := unstructured.NestedMap(un.Object, "spec", "validation", "openAPIV3Schema")
	removeNestedItemsHelper(validation)
	unstructured.SetNestedMap(un.Object, validation, "spec", "validation", "openAPIV3Schema")
}

func removeNestedItemsHelper(obj map[string]interface{}) {
	for k, v := range obj {
		vObj, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		_, ok, _ = unstructured.NestedMap(vObj, "properties", "items", "items")
		if ok {
			delete(obj, k)
		} else {
			removeNestedItemsHelper(vObj)
		}
	}
}

func removeK8S118Fields(un *unstructured.Unstructured) {
	kind := crdKind(un)
	switch kind {
	case "Rollout":
		removeValidation(un, "spec.template.spec.containers[].resources.limits")
		removeValidation(un, "spec.template.spec.containers[].resources.requests")
		removeValidation(un, "spec.template.spec.initContainers[].resources.limits")
		removeValidation(un, "spec.template.spec.initContainers[].resources.requests")
		removeValidation(un, "spec.template.spec.ephemeralContainers[].resources.limits")
		removeValidation(un, "spec.template.spec.ephemeralContainers[].resources.requests")
		validation, _, _ := unstructured.NestedMap(un.Object, "spec", "validation", "openAPIV3Schema")
		removeFieldHelper(validation, "x-kubernetes-list-type")
		removeFieldHelper(validation, "x-kubernetes-list-map-keys")
		unstructured.SetNestedMap(un.Object, validation, "spec", "validation", "openAPIV3Schema")
	case "Experiment":
		removeValidation(un, "spec.templates[].template.spec.containers[].resources.limits")
		removeValidation(un, "spec.templates[].template.spec.containers[].resources.requests")
		removeValidation(un, "spec.templates[].template.spec.initContainers[].resources.limits")
		removeValidation(un, "spec.templates[].template.spec.initContainers[].resources.requests")
		removeValidation(un, "spec.templates[].template.spec.ephemeralContainers[].resources.limits")
		removeValidation(un, "spec.templates[].template.spec.ephemeralContainers[].resources.requests")
		validation, _, _ := unstructured.NestedMap(un.Object, "spec", "validation", "openAPIV3Schema")
		removeFieldHelper(validation, "x-kubernetes-list-type")
		removeFieldHelper(validation, "x-kubernetes-list-map-keys")
		unstructured.SetNestedMap(un.Object, validation, "spec", "validation", "openAPIV3Schema")
	case "AnalysisTemplate", "AnalysisRun":
		removeValidation(un, "spec.metrics[].provider.job.spec.template.spec.containers[].resources.limits")
		removeValidation(un, "spec.metrics[].provider.job.spec.template.spec.containers[].resources.requests")
		removeValidation(un, "spec.metrics[].provider.job.spec.template.spec.initContainers[].resources.limits")
		removeValidation(un, "spec.metrics[].provider.job.spec.template.spec.initContainers[].resources.requests")
		removeValidation(un, "spec.metrics[].provider.job.spec.template.spec.ephemeralContainers[].resources.limits")
		removeValidation(un, "spec.metrics[].provider.job.spec.template.spec.ephemeralContainers[].resources.requests")
		validation, _, _ := unstructured.NestedMap(un.Object, "spec", "validation", "openAPIV3Schema")
		removeFieldHelper(validation, "x-kubernetes-list-type")
		removeFieldHelper(validation, "x-kubernetes-list-map-keys")
		unstructured.SetNestedMap(un.Object, validation, "spec", "validation", "openAPIV3Schema")
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
