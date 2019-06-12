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
