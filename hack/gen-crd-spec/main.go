package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/blang/semver/v4"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	kubeopenapiutil "k8s.io/kube-openapi/pkg/util"
	kOpenAPISpec "k8s.io/kube-openapi/pkg/validation/spec"

	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"

	"github.com/ghodss/yaml"
	extensionsobj "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
		setValidationOverride(un, preserveUnknownFields, "spec.template.spec.volumes[]")
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

type xKubernetesGroupVersionKind struct {
	Group   string `json:"group"`
	Kind    string `json:"kind"`
	Version string `json:"version"`
}
type gvkMeta struct {
	XKubernetesGroupVersionKind []xKubernetesGroupVersionKind `json:"x-kubernetes-group-version-kind"`
}
type k8sGvkMapping struct {
	Definitions map[string]gvkMeta `json:"definitions"`
}

type openAPISchema struct {
	kOpenAPISpec.Schema
	XKubernetesGroupVersionKind []xKubernetesGroupVersionKind `json:"x-kubernetes-group-version-kind"`
}

//Add marshal function so we don't call the embeeded marshal
func (s openAPISchema) MarshalJSON() ([]byte, error) {
	b1, err := json.Marshal(s.Schema)
	if err != nil {
		return nil, fmt.Errorf("schema %v", err)
	}

	if s.XKubernetesGroupVersionKind != nil {
		b2, err := json.Marshal(s.XKubernetesGroupVersionKind)
		if err != nil {
			return nil, fmt.Errorf("x-kubernetes-group-version-kind %v", err)
		}
		b1 = append(b1[:len(b1)-1], fmt.Sprintf(",\"x-kubernetes-group-version-kind\":%s}", string(b2))...)
	}
	return b1, nil
}

// loadK8SDefinitions loads K8S types API schema definitions starting with the version specified in go.mod then the fucnction
// parameter versions
func loadK8SDefinitions(versions []int) (*k8sGvkMapping, error) {
	// detects minor version of k8s client
	k8sVersionCmd := exec.Command("sh", "-c", "cat go.mod | grep \"k8s.io/client-go\" |  head -n 1 | cut -d' ' -f2")
	versionData, err := k8sVersionCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to determine k8s client version: %v", err)
	}
	v, err := semver.Parse(strings.TrimSpace(strings.Replace(string(versionData), "v", "", 1)))
	if err != nil {
		return nil, err
	}

	resp, err := http.Get(fmt.Sprintf("https://raw.githubusercontent.com/kubernetes/kubernetes/release-1.%d/api/openapi-spec/swagger.json", v.Minor))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	schemaGoMod := k8sGvkMapping{}
	err = json.Unmarshal(data, &schemaGoMod)
	if err != nil {
		return nil, err
	}

	for _, v := range versions {
		//Download fixe old version to keep old schema's compatibility
		resp, err = http.Get(fmt.Sprintf("https://raw.githubusercontent.com/kubernetes/kubernetes/release-1.%d/api/openapi-spec/swagger.json", v))
		if err != nil {
			return nil, err
		}
		data, err = ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		schemaFixedVer := k8sGvkMapping{}
		err = json.Unmarshal(data, &schemaFixedVer)
		if err != nil {
			return nil, err
		}

		//Merge old and new schema
		for k, v := range schemaFixedVer.Definitions {
			schemaGoMod.Definitions[k] = v
		}
	}

	return &schemaGoMod, nil
}

func generateOpenApiSchema(outputPath string) error {
	// We replace the generated names with group specific names aka argocd is `argocd.argoproj.io` instead of the real
	// group kind because within all the argo projects we have overlapping types due to all argo projects being under the same
	// argoproj.io group. Kustomize does not care about the name as long as all the links match up and the `x-kubernetes-group-version-kind`
	// metadata is correct.
	var argoMappings = map[string]string{
		"github.com/argoproj/argo-cd/v2/pkg/apis/application":     "argocd.argoproj.io",
		"github.com/argoproj/argo-events/pkg/apis/eventbus":       "eventbus.argoproj.io",
		"github.com/argoproj/argo-events/pkg/apis/eventsource":    "eventsource.argoproj.io",
		"github.com/argoproj/argo-events/pkg/apis/sensor":         "sensor.argoproj.io",
		"github.com/argoproj/argo-rollouts/pkg/apis/rollouts":     "rollouts.argoproj.io",
		"github.com/argoproj/argo-workflows/v3/pkg/apis/workflow": "workflow.argoproj.io",
	}

	d := v1alpha1.GetOpenAPIDefinitions(func(path string) kOpenAPISpec.Ref {
		for k, v := range argoMappings {
			path = strings.ReplaceAll(path, k, v)
		}
		return kOpenAPISpec.MustCreateRef(fmt.Sprintf("#/definitions/%s", kubeopenapiutil.ToRESTFriendlyName(path)))
	})

	var def = make(map[string]openAPISchema)
	for pathKey, definition := range d {
		for k, v := range argoMappings {
			pathKey = strings.ReplaceAll(pathKey, k, v)
		}
		def[kubeopenapiutil.ToRESTFriendlyName(pathKey)] = openAPISchema{
			Schema:                      definition.Schema,
			XKubernetesGroupVersionKind: make([]xKubernetesGroupVersionKind, 0),
		}
	}

	k8sDefs, err := loadK8SDefinitions([]int{18, 21, 24})
	checkErr(err)
	for k, v := range def {
		//We pull out argo crd information based on the dot pattern of the key in the dictionary we are also setting it for all
		//argo types instead of just the ones needed this could be incorrect as far as spec goes, but it works.
		if strings.HasPrefix(k, "io.argoproj") {
			argoGVK := strings.Split(k, ".")
			v.XKubernetesGroupVersionKind = []xKubernetesGroupVersionKind{{
				Group:   "argoproj.io",
				Kind:    argoGVK[4],
				Version: argoGVK[3],
			}}
			def[k] = v
			continue
		}

		// Pull the group version kind information from the k8s definitions that we downloaded via loadK8SDefinitions
		entry, ok := k8sDefs.Definitions[k]
		if ok {
			e, ok := def[k]
			if ok {
				if len(entry.XKubernetesGroupVersionKind) > 0 {
					e.XKubernetesGroupVersionKind = entry.XKubernetesGroupVersionKind
					def[k] = e
				}
			}
		}

	}

	data, err := json.MarshalIndent(map[string]interface{}{
		"definitions": def,
	}, "", "    ")
	if err != nil {
		return err
	}

	return ioutil.WriteFile(outputPath, data, 0644)
}

// Generate CRD spec for Rollout Resource
func main() {
	crds := NewCustomResourceDefinition()

	err := generateOpenApiSchema("docs/features/kustomize/rollout_cr_schema.json")
	checkErr(err)

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
