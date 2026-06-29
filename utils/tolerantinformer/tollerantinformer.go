package tolerantinformer

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

// makeTransform returns a cache.TransformFunc that converts a *unstructured.Unstructured
// into a typed pointer produced by newFn. The conversion runs once per object on
// insert/update into the shared informer cache, so subsequent List/Get calls return
// the typed object directly with no per-call reflection cost.
//
// Tolerance: if the reflection-based DefaultUnstructuredConverter fails (e.g., a
// malformed field that fails fast), fall back to encoding/json which continues past
// invalid fields. Preserves the original tolerantinformer behavior from PR #666
// (resolves #389, #517).
//
// Idempotent: if the object has already been converted (e.g., a resync delivers a
// typed object), it is returned unchanged.
func makeTransform[T runtime.Object](newFn func() T) cache.TransformFunc {
	return func(obj any) (any, error) {
		un, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return obj, nil
		}
		typed := newFn()
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(un.Object, typed); err != nil {
			logCtx := logutil.WithObject(un)
			logCtx.Warnf("malformed object: %v", err)
			typed = newFn()
			_ = fromUnstructuredViaJSON(un.Object, typed)
		}
		return typed, nil
	}
}

// installTransform calls SetTransform on the informer, panicking if the informer
// has already started. Constructors must run before factory.Start().
func installTransform(informer cache.SharedIndexInformer, transform cache.TransformFunc, kind string) {
	if err := informer.SetTransform(transform); err != nil {
		panic(fmt.Errorf("tolerantinformer: SetTransform for %s: %w (constructors must run before factory.Start)", kind, err))
	}
}

func fromUnstructuredViaJSON(u map[string]any, obj any) error {
	data, err := json.Marshal(u)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, obj)
}
