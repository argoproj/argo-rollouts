package istio

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestDoesIstioExist(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	assert.True(t, DoesIstioExist(dynamicClient, metav1.NamespaceAll, "v1alpha3"))
	assert.Len(t, dynamicClient.Actions(), 1)
	assert.Equal(t, "list", dynamicClient.Actions()[0].GetVerb())
}

func TestGetIstioGVR(t *testing.T) {
	gvr := GetIstioGVR("v1alpha3")
	assert.Equal(t, "networking.istio.io", gvr.Group)
	assert.Equal(t, "v1alpha3", gvr.Version)
	assert.Equal(t, "virtualservices", gvr.Resource)
}
