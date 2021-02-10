package istio

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestDoesIstioExist(t *testing.T) {
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	assert.True(t, DoesIstioExist(dynamicClient, metav1.NamespaceAll))
	assert.Len(t, dynamicClient.Actions(), 1)
	assert.Equal(t, "list", dynamicClient.Actions()[0].GetVerb())
}

func TestGetIstioVirtualServiceGVR(t *testing.T) {
	gvr := GetIstioVirtualServiceGVR()
	assert.Equal(t, "networking.istio.io", gvr.Group)
	assert.Equal(t, "v1alpha3", gvr.Version)
	assert.Equal(t, "virtualservices", gvr.Resource)
}

func TestGetIstioDestinationRuleGVR(t *testing.T) {
	SetIstioAPIVersion("v1alpha4")
	gvr := GetIstioDestinationRuleGVR()
	assert.Equal(t, "networking.istio.io", gvr.Group)
	assert.Equal(t, "v1alpha4", gvr.Version)
	assert.Equal(t, "destinationrules", gvr.Resource)
	SetIstioAPIVersion("v1alpha3")
}

func TestGetRolloutVirtualServiceKeys(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{},
		},
	}
	assert.Len(t, GetRolloutVirtualServiceKeys(ro), 0)
	ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{}
	assert.Len(t, GetRolloutVirtualServiceKeys(ro), 0)
	ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	assert.Len(t, GetRolloutVirtualServiceKeys(ro), 0)
	ro.Spec.Strategy.Canary.TrafficRouting.Istio = &v1alpha1.IstioTrafficRouting{
		VirtualService: v1alpha1.IstioVirtualService{},
	}
	assert.Len(t, GetRolloutVirtualServiceKeys(ro), 0)
	ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Name = "test"
	keys := GetRolloutVirtualServiceKeys(ro)
	assert.Len(t, keys, 1)
	assert.Equal(t, keys[0], "default/test")
}
