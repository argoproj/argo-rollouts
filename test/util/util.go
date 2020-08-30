package util

import (
	"io/ioutil"

	"github.com/ghodss/yaml"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	body, err := ioutil.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return ObjectFromYAML(string(body))
}
