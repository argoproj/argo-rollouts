package informer

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
)

// StripManagedFields is a cache.TransformFunc that drops metadata.managedFields from objects
// before they are stored in an informer cache, reducing controller memory usage. The controller
// never reads managedFields.
//
// managedFields is set to nil (not an empty slice) on purpose: when an Update() request omits
// managedFields the API server keeps the live value, whereas an empty slice would reset it.
func StripManagedFields(obj any) (any, error) {
	// Avoid Unstructured.GetManagedFields(), which decodes the whole list just to check it.
	if un, ok := obj.(*unstructured.Unstructured); ok {
		unstructured.RemoveNestedField(un.Object, "metadata", "managedFields")
		return un, nil
	}
	if accessor, err := meta.Accessor(obj); err == nil && accessor.GetManagedFields() != nil {
		accessor.SetManagedFields(nil)
	}
	return obj, nil
}

// SetStripManagedFieldsTransform installs StripManagedFields on an informer. It must be called
// before the informer is started.
func SetStripManagedFieldsTransform(informer cache.SharedIndexInformer) error {
	if err := informer.SetTransform(StripManagedFields); err != nil {
		return fmt.Errorf("failed to set managedFields transform: %w", err)
	}
	return nil
}
