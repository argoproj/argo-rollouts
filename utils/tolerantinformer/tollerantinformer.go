package tolerantinformer

import (
	"encoding/json"
	"fmt"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
		return coerceToTyped(obj, newFn)
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

// coerceToTyped converts a cache object into T. Already-typed objects are returned
// as-is (callers DeepCopy when exposing them). Unstructured objects are converted
// with the same tolerance as makeTransform.
//
// This is required because some controllers (notably argoproj/notifications-engine)
// write dynamic-client Patch results (*unstructured.Unstructured) directly into
// SharedIndexInformer.GetStore(), bypassing SetTransform. Generated typed listers
// hard-cast and would panic on those objects.
func coerceToTyped[T runtime.Object](obj any, newFn func() T) (T, error) {
	if typed, ok := obj.(T); ok {
		return typed, nil
	}
	un, ok := obj.(*unstructured.Unstructured)
	if !ok {
		var zero T
		return zero, fmt.Errorf("unexpected type %T", obj)
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

func listTyped[T runtime.Object](indexer cache.Indexer, namespace string, selector labels.Selector, newFn func() T, deepCopy func(T) T) ([]T, error) {
	var out []T
	appendOne := func(m any) {
		typed, err := coerceToTyped(m, newFn)
		if err != nil {
			warnSkipCacheObject(m, err)
			return
		}
		out = append(out, deepCopy(typed))
	}
	var err error
	if namespace == "" {
		err = cache.ListAll(indexer, selector, appendOne)
	} else {
		err = cache.ListAllByNamespace(indexer, namespace, selector, appendOne)
	}
	return out, err
}

// warnSkipCacheObject logs a skipped cache object with enough identity for field
// diagnosis (namespace/name/key when available), matching logutil.WithObject style.
func warnSkipCacheObject(obj any, err error) {
	if ro, ok := obj.(runtime.Object); ok {
		logutil.WithObject(ro).WithField("type", fmt.Sprintf("%T", obj)).
			Warnf("tolerantinformer: skipping cache object: %v", err)
		return
	}
	fields := log.Fields{"type": fmt.Sprintf("%T", obj)}
	if key, keyErr := cache.MetaNamespaceKeyFunc(obj); keyErr == nil {
		fields["key"] = key
		if ns, name, splitErr := cache.SplitMetaNamespaceKey(key); splitErr == nil {
			fields["namespace"] = ns
			fields["name"] = name
		}
	}
	log.WithFields(fields).Warnf("tolerantinformer: skipping cache object: %v", err)
}

func getTyped[T runtime.Object](indexer cache.Indexer, resource schema.GroupResource, namespace, name string, newFn func() T, deepCopy func(T) T) (T, error) {
	var zero T
	key := name
	if namespace != "" {
		key = namespace + "/" + name
	}
	obj, exists, err := indexer.GetByKey(key)
	if err != nil {
		return zero, err
	}
	if !exists {
		return zero, errors.NewNotFound(resource, name)
	}
	typed, err := coerceToTyped(obj, newFn)
	if err != nil {
		return zero, err
	}
	return deepCopy(typed), nil
}

// transformingInformer wraps a SharedIndexInformer so callers that mutate the
// store via GetStore()/GetIndexer() (e.g. notifications-engine after a dynamic
// Patch) still run the unstructured→typed transform. The Reflector/FIFO path
// uses the informer's internal indexer field directly and is unaffected.
type transformingInformer struct {
	cache.SharedIndexInformer
	transform cache.TransformFunc
}

func (t *transformingInformer) GetStore() cache.Store {
	return &transformingIndexer{Indexer: t.SharedIndexInformer.GetIndexer(), transform: t.transform}
}

func (t *transformingInformer) GetIndexer() cache.Indexer {
	return &transformingIndexer{Indexer: t.SharedIndexInformer.GetIndexer(), transform: t.transform}
}

type transformingIndexer struct {
	cache.Indexer
	transform cache.TransformFunc
}

func (t *transformingIndexer) apply(obj any) (any, error) {
	if t.transform == nil {
		return obj, nil
	}
	return t.transform(obj)
}

func (t *transformingIndexer) Add(obj any) error {
	obj, err := t.apply(obj)
	if err != nil {
		return err
	}
	return t.Indexer.Add(obj)
}

func (t *transformingIndexer) Update(obj any) error {
	obj, err := t.apply(obj)
	if err != nil {
		return err
	}
	return t.Indexer.Update(obj)
}

func (t *transformingIndexer) Replace(list []any, resourceVersion string) error {
	transformed := make([]any, len(list))
	for i, obj := range list {
		obj, err := t.apply(obj)
		if err != nil {
			return err
		}
		transformed[i] = obj
	}
	return t.Indexer.Replace(transformed, resourceVersion)
}
