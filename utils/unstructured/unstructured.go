package unstructured

import (
	"regexp"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/ghodss/yaml"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func StrToUnstructuredUnsafe(jsonStr string) *unstructured.Unstructured {
	obj := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(jsonStr), &obj)
	if err != nil {
		panic(err)
	}
	return &unstructured.Unstructured{Object: obj}
}

func StrToUnstructured(jsonStr string) (*unstructured.Unstructured, error) {
	obj := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(jsonStr), &obj)
	if err != nil {
		return nil, err
	}
	return &unstructured.Unstructured{Object: obj}, nil
}

func ObjectToRollout(obj interface{}) *v1alpha1.Rollout {
	un, ok := obj.(*unstructured.Unstructured)
	if ok {
		var ro v1alpha1.Rollout
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(un.Object, &ro)
		if err != nil {
			log.Warnf("Failed to convert Rollout from Unstructured object: %v", err)
			return nil
		}
		return &ro
	}
	ro, ok := obj.(*v1alpha1.Rollout)
	if !ok {
		log.Warn("Object is neither a rollout or unstructured")
	}
	return ro
}

func ObjectToAnalysisRun(obj interface{}) *v1alpha1.AnalysisRun {
	un, ok := obj.(*unstructured.Unstructured)
	if ok {
		var ar v1alpha1.AnalysisRun
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(un.Object, &ar)
		if err != nil {
			log.Warnf("Failed to convert AnalysisRun from Unstructured object: %v", err)
			return nil
		}
		return &ar
	}
	ar, ok := obj.(*v1alpha1.AnalysisRun)
	if !ok {
		log.Warn("Object is neither a rollout or unstructured")
	}
	return ar
}

var diffSeparator = regexp.MustCompile(`\n---`)

// SplitYAML splits a YAML file into unstructured objects. Returns list of all unstructured objects
// found in the yaml. Panics if any errors occurred.
func SplitYAML(out string) ([]*unstructured.Unstructured, error) {
	parts := diffSeparator.Split(out, -1)
	var objs []*unstructured.Unstructured
	for _, part := range parts {
		var objMap map[string]interface{}
		err := yaml.Unmarshal([]byte(part), &objMap)
		if err != nil {
			return objs, err
		}
		if len(objMap) == 0 {
			// handles case where theres no content between `---`
			continue
		}
		obj := unstructured.Unstructured{Object: objMap}
		objs = append(objs, &obj)
	}
	return objs, nil
}
