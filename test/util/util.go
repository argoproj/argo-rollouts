package util

import (
	"os"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/yaml"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
)

// ObjectFromYAML returns a runtime.Object from a yaml string
func ObjectFromYAML(yamlStr string) *unstructured.Unstructured {
	obj := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(yamlStr), &obj)
	if err != nil {
		panic(err)
	}
	return &unstructured.Unstructured{Object: obj}
}

// ObjectFromPath returns a runtime.Object from the given path. Path is a relative path from source root
func ObjectFromPath(path string) *unstructured.Unstructured {
	path = "../../" + path
	body, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return ObjectFromYAML(string(body))
}

// NewFakeDynamicClient is a convenience that returns a FakeDynamicClient with all the list objects
// we use during testing already registered
func NewFakeDynamicClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	vsvcGVR := istioutil.GetIstioVirtualServiceGVR()
	druleGVR := istioutil.GetIstioDestinationRuleGVR()
	tgbGVR := schema.GroupVersionResource{
		Group:    "elbv2.k8s.aws",
		Version:  "v1beta1",
		Resource: "targetgroupbindings",
	}

	listMapping := map[schema.GroupVersionResource]string{
		vsvcGVR:                             "VirtualServiceList",
		druleGVR:                            "DestinationRuleList",
		v1alpha1.RolloutGVR:                 rollouts.RolloutKind + "List",
		v1alpha1.AnalysisTemplateGVR:        rollouts.AnalysisTemplateKind + "List",
		v1alpha1.AnalysisRunGVR:             rollouts.AnalysisRunKind + "List",
		v1alpha1.ExperimentGVR:              rollouts.ExperimentKind + "List",
		v1alpha1.ClusterAnalysisTemplateGVR: rollouts.ClusterAnalysisTemplateKind + "List",
		tgbGVR:                              "TargetGroupBindingList",
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listMapping, objects...)
}
