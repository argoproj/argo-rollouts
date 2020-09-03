package unstructured

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestStrToUnstructuredSuccesfully(t *testing.T) {
	rsStr := `apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: test
spec:
  replicas: 1
`
	t.Run("Safe method", func(t *testing.T) {
		obj, err := StrToUnstructured(rsStr)
		assert.Nil(t, err)
		assert.NotNil(t, obj)
		assert.Equal(t, "test", obj.GetName())
		assert.Equal(t, "ReplicaSet", obj.GetKind())
		assert.Equal(t, "apps/v1", obj.GetAPIVersion())
		replicas, exists, err := unstructured.NestedFloat64(obj.Object, "spec", "replicas")
		assert.True(t, exists)
		assert.Nil(t, err)
		assert.Equal(t, float64(1), replicas)
	})

	t.Run("Unsafe method", func(t *testing.T) {
		obj := StrToUnstructuredUnsafe(rsStr)
		assert.NotNil(t, obj)
		assert.Equal(t, "test", obj.GetName())
		assert.Equal(t, "ReplicaSet", obj.GetKind())
		assert.Equal(t, "apps/v1", obj.GetAPIVersion())
		replicas, exists, err := unstructured.NestedFloat64(obj.Object, "spec", "replicas")
		assert.True(t, exists)
		assert.Nil(t, err)
		assert.Equal(t, float64(1), replicas)
	})
}

func TestStrToUnstructuredFails(t *testing.T) {
	t.Run("Safe method", func(t *testing.T) {
		obj, err := StrToUnstructured("{")
		assert.Nil(t, obj)
		assert.NotNil(t, err)
	})

	t.Run("Unsafe method", func(t *testing.T) {
		var obj *unstructured.Unstructured
		assert.Panics(t, func() {
			obj = StrToUnstructuredUnsafe("{")
		})
		assert.Nil(t, obj)
	})
}

func TestSplitYAML(t *testing.T) {
	rsStr := `
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: test1
spec:
  replicas: 1
---
apiVersion: apps/v1
kind: ReplicaSet
metadata:
  name: test2
spec:
  replicas: 2
`
	uns, err := SplitYAML(rsStr)
	assert.NoError(t, err)
	assert.Len(t, uns, 2)

	{
		obj := uns[0]
		assert.Equal(t, "test1", obj.GetName())
		assert.Equal(t, "ReplicaSet", obj.GetKind())
		assert.Equal(t, "apps/v1", obj.GetAPIVersion())
		replicas, exists, err := unstructured.NestedFloat64(obj.Object, "spec", "replicas")
		assert.True(t, exists)
		assert.Nil(t, err)
		assert.Equal(t, float64(1), replicas)
	}
	{
		obj := uns[1]
		assert.Equal(t, "test2", obj.GetName())
		assert.Equal(t, "ReplicaSet", obj.GetKind())
		assert.Equal(t, "apps/v1", obj.GetAPIVersion())
		replicas, exists, err := unstructured.NestedFloat64(obj.Object, "spec", "replicas")
		assert.True(t, exists)
		assert.Nil(t, err)
		assert.Equal(t, float64(2), replicas)
	}

}
