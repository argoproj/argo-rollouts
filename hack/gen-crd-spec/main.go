package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"

	"github.com/blang/semver"
	extensionsobj "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kubeopenapiutil "k8s.io/kube-openapi/pkg/util"
	spec "k8s.io/kube-openapi/pkg/validation/spec"
	"sigs.k8s.io/yaml"
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
		"crd:crdVersions=v1,maxDescLen=0",
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

// loadK8SDefinitions loads K8S types API schema definitions
func loadK8SDefinitions() (spec.Definitions, error) {
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
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	schema := spec.Schema{}
	err = json.Unmarshal(data, &schema)
	if err != nil {
		return nil, err
	}
	return schema.Definitions, nil
}

// normalizeRef normalizes rollouts and k8s type references since they are slightly different:
// rollout refs are prefixed with #/definitions/ and k8s types refs starts with io.k8s instead of k8s.io and have no /
func normalizeRef(ref string) string {
	if strings.HasPrefix(ref, "#/definitions/") {
		ref = ref[len("#/definitions/"):]
	}

	if strings.HasPrefix(ref, "io.k8s.") {
		ref = "k8s.io." + ref[len("io.k8s."):]
	}
	return strings.ReplaceAll(ref, "/", ".")
}

var patchAnnotationKeys = map[string]bool{
	"x-kubernetes-patch-merge-key": true,
	"x-kubernetes-patch-strategy":  true,
	"x-kubernetes-list-map-keys":   true,
	"x-kubernetes-list-type":       true,
}

// injectPatchAnnotations injects patch annotations from given schema definitions and drop properties that don't have
// patch annotations injected
func injectPatchAnnotations(prop map[string]interface{}, propSchema spec.Schema, schemaDefinitions spec.Definitions) (bool, error) {
	injected := false
	for k, v := range propSchema.Extensions {
		if patchAnnotationKeys[k] {
			prop[k] = v
			injected = true
		}
	}

	var propSchemas map[string]spec.Schema
	refStr := propSchema.Ref.String()
	normalizedRef := normalizeRef(refStr)
	switch {
	case normalizedRef == "":
		propSchemas = propSchema.Properties
	default:
		schema, ok := schemaDefinitions[normalizedRef]
		if !ok {
			return false, fmt.Errorf("not supported ref: %s", refStr)
		}
		propSchemas = schema.Properties
	}

	childProps, ok := prop["properties"].(map[string]interface{})
	if !ok {
		childProps = map[string]interface{}{}
	}

	for k, v := range childProps {
		childInjected, err := injectPatchAnnotations(v.(map[string]interface{}), propSchemas[k], schemaDefinitions)
		if err != nil {
			return false, err
		}
		if !childInjected {
			delete(childProps, k)
		} else {
			injected = true
			childProps[k] = v
		}
	}
	return injected, nil
}

const (
	rolloutsDefinitionsPrefix = "github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
)

// generateKustomizeSchema generates open api schema that has properties with patch annotations only
func generateKustomizeSchema(crds []*extensionsobj.CustomResourceDefinition, outputPath string) error {
	k8sDefinitions, err := loadK8SDefinitions()
	if err != nil {
		return err
	}
	schemaDefinitions := map[string]spec.Schema{}
	for k, v := range k8sDefinitions {
		schemaDefinitions[normalizeRef(k)] = v
	}

	for k, v := range v1alpha1.GetOpenAPIDefinitions(func(path string) spec.Ref {
		return spec.MustCreateRef(path)
	}) {
		schemaDefinitions[normalizeRef(k)] = v.Schema
	}

	definitions := map[string]interface{}{}
	for _, crd := range crds {
		var version string
		var props map[string]extensionsobj.JSONSchemaProps
		for _, v := range crd.Spec.Versions {
			if v.Schema == nil || v.Schema.OpenAPIV3Schema == nil {
				continue
			}
			version = v.Name
			props = v.Schema.OpenAPIV3Schema.Properties
		}

		data, err := json.Marshal(props)
		if err != nil {
			return err
		}
		propsMap := map[string]interface{}{}
		err = json.Unmarshal(data, &propsMap)
		if err != nil {
			return err
		}

		crdSchema := schemaDefinitions[normalizeRef(fmt.Sprintf("%s/%s.%s", rolloutsDefinitionsPrefix, version, crd.Spec.Names.Kind))]
		for k, p := range propsMap {
			injected, err := injectPatchAnnotations(p.(map[string]interface{}), crdSchema.Properties[k], schemaDefinitions)
			if err != nil {
				return err
			}
			if injected {
				propsMap[k] = p
			} else {
				delete(propsMap, k)
			}
		}

		definitionName := kubeopenapiutil.ToRESTFriendlyName(fmt.Sprintf("%s/%s.%s", crd.Spec.Group, version, crd.Spec.Names.Kind))
		definitions[definitionName] = map[string]interface{}{
			"properties": propsMap,
			"x-kubernetes-group-version-kind": []map[string]string{{
				"group":   crd.Spec.Group,
				"kind":    crd.Spec.Names.Kind,
				"version": version,
			}},
		}
	}
	data, err := json.MarshalIndent(map[string]interface{}{
		"definitions": definitions,
	}, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, data, 0644)
}

// Generate CRD spec for Rollout Resource
func main() {
	crds := NewCustomResourceDefinition()

	err := generateKustomizeSchema(crds, "docs/features/kustomize/rollout_cr_schema.json")
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
		err = os.WriteFile(path, yamlBytes, 0644)
		checkErr(err)
	}
}
