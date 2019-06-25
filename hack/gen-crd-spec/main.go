package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/ghodss/yaml"
	extensionsobj "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	crdPath = "manifests/crds/rollout-crd.yaml"
)

func NewRolloutCustomResourceDefinition() *extensionsobj.CustomResourceDefinition {
	crdYamlBytes, err := exec.Command(
		"controller-gen",
		"paths=./pkg/apis/rollouts/...",
		"crd:trivialVersions=true",
		"output:crd:stdout",
	).Output()
	checkErr(err)
	if len(crdYamlBytes) == 0 {
		panic("controller-gen produced no output")
	}

	// clean up stuff left by controller-gen
	deleteFile("config/webhook/manifests.yaml")
	deleteFile("config/webhook")
	deleteFile("config")

	var un unstructured.Unstructured
	checkErr(yaml.Unmarshal(crdYamlBytes, &un.Object))

	// We need to completely remove validation of problematic fields such as creationTimestamp,
	// which get marshalled to `null`, but are typed as as a `string` during Open API validation
	removeValidataion(&un, "metadata.creationTimestamp")
	removeValidataion(&un, "spec.template.metadata.creationTimestamp")
	removeResourceValidation(&un)
	removeNestedItems(&un)
	removeDescriptions(&un)

	crd := toCRD(&un)
	crd.Spec.Scope = "Namespaced"
	return crd
}

func deleteFile(path string) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}
	checkErr(os.Remove(path))
}

func removeValidataion(un *unstructured.Unstructured, path string) {
	schemaPath := []string{"spec", "validation", "openAPIV3Schema"}
	for _, part := range strings.Split(path, ".") {
		schemaPath = append(schemaPath, "properties", part)
	}
	unstructured.RemoveNestedField(un.Object, schemaPath...)
}

// removeDescriptions removes all descriptions which bloats the API spec
func removeDescriptions(un *unstructured.Unstructured) {
	validation, _, _ := unstructured.NestedMap(un.Object, "spec", "validation", "openAPIV3Schema")
	removeDescriptionsHelper(validation)
	unstructured.SetNestedMap(un.Object, validation, "spec", "validation", "openAPIV3Schema")
}

func removeDescriptionsHelper(obj map[string]interface{}) {
	for k, v := range obj {
		if k == "description" {
			delete(obj, k)
			continue
		}
		if vObj, ok := v.(map[string]interface{}); ok {
			removeDescriptionsHelper(vObj)
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

var resourcesSchemaPath = []string{
	"spec",
	"validation",
	"openAPIV3Schema", "properties",
	"spec", "properties",
	"template", "properties",
	"spec", "properties",
	"containers", "items", "properties",
	"resources", "properties",
}

// removeResourceValidation needs to be removed since open api cannot accept both numbers and strings
func removeResourceValidation(un *unstructured.Unstructured) {
	containersFieldIf, ok, err := unstructured.NestedFieldNoCopy(un.Object, resourcesSchemaPath...)
	checkErr(err)
	if !ok {
		panic(fmt.Sprintf("%s not found", resourcesSchemaPath))
	}
	containers := containersFieldIf.(map[string]interface{})
	unstructured.RemoveNestedField(containers, "limits")
	unstructured.RemoveNestedField(containers, "requests")
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
	crd := NewRolloutCustomResourceDefinition()

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

	err = ioutil.WriteFile(crdPath, yamlBytes, 0644)
	checkErr(err)
}
