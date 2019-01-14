package main

import (
	"os"

	crdutil "github.com/ant31/crd-validation/pkg"
	extensionsobj "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1alpha1 "github.com/argoproj/rollout-controller/pkg/apis/rollouts/v1alpha1"
)

const (
	plural             = "rollouts"
	group              = "argoproj.io"
	version            = "v1alpha1"
	kind               = "Rollout"
	shortName          = "ro"
	specDefinitionName = "github.com/argoproj/rollout-controller/pkg/apis/rollouts/v1alpha1.Rollout"
	scope              = "Namespaced"
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
	crd.Spec.Validation = crdutil.GetCustomResourceValidation(specDefinitionName, v1alpha1.GetOpenAPIDefinitions)

	return crd
}

// Generate OpenAPI spec definitions for Rollout Resource
func main() {
	crd := NewRolloutCustomResourceDefinition()
	crdutil.MarshallCrd(crd, "yaml")
	os.Exit(0)

}
