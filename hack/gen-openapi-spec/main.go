package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"os"

	crdutil "github.com/ant31/crd-validation/pkg"
	"github.com/ghodss/yaml"
	extensionsobj "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	plural             = "rollouts"
	group              = "argoproj.io"
	version            = "v1alpha1"
	kind               = "Rollout"
	shortName          = "ro"
	specDefinitionName = "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1.Rollout"
	scope              = "Namespaced"

	crdPath = "manifests/crds/rollout-crd.yaml"
)

func NewRolloutCustomResourceDefinition() *extensionsobj.CustomResourceDefinition {

	crd := &extensionsobj.CustomResourceDefinition{

		ObjectMeta: metav1.ObjectMeta{
			Name: plural + "." + group,
		},
		TypeMeta: crdutil.CustomResourceDefinitionTypeMeta,
		Spec: extensionsobj.CustomResourceDefinitionSpec{
			Group:   group,
			Version: version,
			Scope:   extensionsobj.ResourceScope(scope),
			Names: extensionsobj.CustomResourceDefinitionNames{
				Plural:     plural,
				Kind:       kind,
				ShortNames: []string{shortName},
			},
		},
	}
	labelSelector :=  ".status.selector"
	crd.Spec.Subresources = &extensionsobj.CustomResourceSubresources{
		Scale: &extensionsobj.CustomResourceSubresourceScale{
			SpecReplicasPath: ".spec.replicas",
			StatusReplicasPath: ".status.HPAReplicas",
			LabelSelectorPath: &labelSelector,

		},
	}
	crd.Spec.Validation = crdutil.GetCustomResourceValidation(specDefinitionName, v1alpha1.GetOpenAPIDefinitions)

	return crd
}

// Generate OpenAPI spec definitions for Rollout Resource
func main() {
	crd := NewRolloutCustomResourceDefinition()

	jsonBytes, err := json.Marshal(crd)
	if err != nil {
		log.Fatal(err)
	}

	var r unstructured.Unstructured
	if err := json.Unmarshal(jsonBytes, &r.Object); err != nil {
		log.Fatal(err)
	}

	unstructured.RemoveNestedField(r.Object, "status")
	unstructured.RemoveNestedField(r.Object, "metadata", "creationTimestamp")
	jsonBytes, err = json.MarshalIndent(r.Object, "", "    ")
	if err != nil {
		log.Fatal(err)
	}
	yamlBytes, err := yaml.JSONToYAML(jsonBytes)
	if err != nil {
		log.Fatal(err)
	}

	err = ioutil.WriteFile(crdPath, yamlBytes, 0644)
	if err != nil {
		panic(err)
	}
	os.Exit(0)

}
