package tolerantinformer

import (
	"encoding/json"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

// convertObject converts a runtime.Object into the supplied concrete typed object
// typedObj should be a pointer to a typed object which is desired to be filled in.
// This is a best effort conversion which ignores unmarshalling errors.
func convertObject(object runtime.Object, typedObj interface{}) error {
	un, ok := object.(*unstructured.Unstructured)
	if !ok {
		return fmt.Errorf("malformed object: expected \"*unstructured.Unstructured\", got \"%s\"", reflect.TypeOf(object).Name())
	}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(un.Object, typedObj)
	if err != nil {
		logCtx := logutil.WithObject(un)
		logCtx.Warnf("malformed object: %v", err)
		// When DefaultUnstructuredConverter.FromUnstructured fails to convert an object, it
		// fails fast, not bothering to unmarshal the rest of the contents.
		// When this happens, we fall back to golang json unmarshalling, since golang json
		// unmarshalling continues to unmarshal all the remaining fields, which allows us to
		// return back a mostly complete, and likely still usable object. This approach is
		// preferred over the other options, which is to either return an error, or ignore the
		// object completely.
		_ = fromUnstructuredViaJSON(un.Object, typedObj)
	}
	return nil
}

func fromUnstructuredViaJSON(u map[string]interface{}, obj interface{}) error {
	data, err := json.Marshal(u)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, obj)
}
