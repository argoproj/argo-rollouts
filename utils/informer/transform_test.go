package informer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

func managedFields() []metav1.ManagedFieldsEntry {
	return []metav1.ManagedFieldsEntry{{
		Manager:    "kubectl",
		Operation:  metav1.ManagedFieldsOperationApply,
		APIVersion: "apps/v1",
		FieldsType: "FieldsV1",
		FieldsV1:   &metav1.FieldsV1{Raw: []byte(`{"f:spec":{"f:replicas":{}}}`)},
	}}
}

func newReplicaSet() *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:          "rs",
			Namespace:     "default",
			Labels:        map[string]string{"app": "test"},
			Annotations:   map[string]string{"kubectl.kubernetes.io/last-applied-configuration": "{}"},
			ManagedFields: managedFields(),
		},
	}
}

func newUnstructuredVirtualService() *unstructured.Unstructured {
	un := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "networking.istio.io/v1alpha3",
		"kind":       "VirtualService",
		"metadata": map[string]any{
			"name":        "vsvc",
			"namespace":   "default",
			"annotations": map[string]any{"kubectl.kubernetes.io/last-applied-configuration": "{}"},
		},
		"spec": map[string]any{"hosts": []any{"istio-rollout"}},
	}}
	un.SetManagedFields(managedFields())
	return un
}

func TestStripManagedFieldsTyped(t *testing.T) {
	rs := newReplicaSet()

	out, err := StripManagedFields(rs)
	require.NoError(t, err)
	stripped, ok := out.(*appsv1.ReplicaSet)
	require.True(t, ok)
	assert.Same(t, rs, stripped, "object should be mutated in place")
	// nil (not empty) so that Update() requests omit managedFields and the API server keeps the live value
	assert.Nil(t, stripped.ManagedFields)
	assert.Equal(t, "rs", stripped.Name)
	assert.Equal(t, map[string]string{"app": "test"}, stripped.Labels)
	assert.Equal(t, "{}", stripped.Annotations["kubectl.kubernetes.io/last-applied-configuration"])
}

func TestStripManagedFieldsUnstructured(t *testing.T) {
	un := newUnstructuredVirtualService()

	out, err := StripManagedFields(un)
	require.NoError(t, err)
	stripped, ok := out.(*unstructured.Unstructured)
	require.True(t, ok)
	assert.Same(t, un, stripped, "object should be mutated in place")
	_, found, err := unstructured.NestedFieldNoCopy(stripped.Object, "metadata", "managedFields")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, stripped.GetManagedFields())
	assert.Equal(t, "vsvc", stripped.GetName())
	assert.Equal(t, "{}", stripped.GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"])
	hosts, _, _ := unstructured.NestedStringSlice(stripped.Object, "spec", "hosts")
	assert.Equal(t, []string{"istio-rollout"}, hosts)
}

func TestStripManagedFieldsIdempotent(t *testing.T) {
	rs := newReplicaSet()
	un := newUnstructuredVirtualService()
	for _, obj := range []any{rs, un} {
		first, err := StripManagedFields(obj)
		require.NoError(t, err)
		second, err := StripManagedFields(first)
		require.NoError(t, err)
		assert.Equal(t, first, second)
	}
	assert.Nil(t, rs.ManagedFields)
	assert.Nil(t, un.GetManagedFields())
}

func TestStripManagedFieldsNoManagedFields(t *testing.T) {
	rs := newReplicaSet()
	rs.ManagedFields = nil
	expected := rs.DeepCopy()
	out, err := StripManagedFields(rs)
	require.NoError(t, err)
	assert.Equal(t, expected, out)

	un := &unstructured.Unstructured{Object: map[string]any{"kind": "VirtualService"}}
	out, err = StripManagedFields(un)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"kind": "VirtualService"}, out.(*unstructured.Unstructured).Object)
}

func TestStripManagedFieldsNonObjectPassthrough(t *testing.T) {
	tombstone := cache.DeletedFinalStateUnknown{Key: "default/rs", Obj: newReplicaSet()}
	out, err := StripManagedFields(tombstone)
	require.NoError(t, err)
	assert.Equal(t, tombstone, out)

	out, err = StripManagedFields("not-an-object")
	require.NoError(t, err)
	assert.Equal(t, "not-an-object", out)

	out, err = StripManagedFields(nil)
	require.NoError(t, err)
	assert.Nil(t, out)
}

func TestTypedInformerFactoryWithTransform(t *testing.T) {
	client := k8sfake.NewSimpleClientset(newReplicaSet())
	factory := kubeinformers.NewSharedInformerFactoryWithOptions(client, 0, kubeinformers.WithTransform(StripManagedFields))
	informer := factory.Apps().V1().ReplicaSets()
	informer.Informer()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	factory.Start(ctx.Done())
	for _, synced := range factory.WaitForCacheSync(ctx.Done()) {
		require.True(t, synced)
	}

	rs, err := informer.Lister().ReplicaSets("default").Get("rs")
	require.NoError(t, err)
	assert.Nil(t, rs.ManagedFields)
	assert.Equal(t, map[string]string{"app": "test"}, rs.Labels)
}

func TestSetStripManagedFieldsTransform(t *testing.T) {
	vsvcGVR := schema.GroupVersionResource{Group: "networking.istio.io", Version: "v1alpha3", Resource: "virtualservices"}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{vsvcGVR: "VirtualServiceList"},
		newUnstructuredVirtualService(),
	)
	factory := dynamicinformer.NewDynamicSharedInformerFactory(client, 0)
	informer := factory.ForResource(vsvcGVR).Informer()
	require.NoError(t, SetStripManagedFieldsTransform(informer))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	factory.Start(ctx.Done())
	for _, synced := range factory.WaitForCacheSync(ctx.Done()) {
		require.True(t, synced)
	}

	obj, exists, err := informer.GetIndexer().GetByKey("default/vsvc")
	require.NoError(t, err)
	require.True(t, exists)
	accessor, err := meta.Accessor(obj)
	require.NoError(t, err)
	assert.Nil(t, accessor.GetManagedFields())
	assert.Equal(t, "vsvc", accessor.GetName())

	// SetTransform is rejected once the informer has started
	assert.Error(t, SetStripManagedFieldsTransform(informer))
}
