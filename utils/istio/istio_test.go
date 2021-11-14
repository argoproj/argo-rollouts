package istio

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func NewFakeDynamicClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	vsvcGVR := GetIstioVirtualServiceGVR()
	druleGVR := GetIstioDestinationRuleGVR()

	listMapping := map[schema.GroupVersionResource]string{
		vsvcGVR:  vsvcGVR.Resource + "List",
		druleGVR: druleGVR.Resource + "List",
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listMapping, objects...)
}

func TestDoesIstioExist(t *testing.T) {
	dynamicClient := NewFakeDynamicClient()
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
	defaults.SetIstioAPIVersion("v1alpha4")
	gvr := GetIstioDestinationRuleGVR()
	assert.Equal(t, "networking.istio.io", gvr.Group)
	assert.Equal(t, "v1alpha4", gvr.Version)
	assert.Equal(t, "destinationrules", gvr.Resource)
	defaults.SetIstioAPIVersion("v1alpha3")
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
		VirtualService: &v1alpha1.IstioVirtualService{},
	}
	assert.Len(t, GetRolloutVirtualServiceKeys(ro), 0)
	ro.Spec.Strategy.Canary.TrafficRouting.Istio = &v1alpha1.IstioTrafficRouting{
		VirtualServices: []v1alpha1.IstioVirtualService{},
	}
	assert.Len(t, GetRolloutVirtualServiceKeys(ro), 0)

	multipleVirtualService := []v1alpha1.IstioVirtualService{{Name: "test1", Routes: nil}, {Name: "test2", Routes: nil}}
	ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService = &v1alpha1.IstioVirtualService{
		Name: "test",
	}
	ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualServices = multipleVirtualService
	assert.Len(t, GetRolloutVirtualServiceKeys(ro), 0)

	ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualServices = nil
	ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Name = "test"
	keys := GetRolloutVirtualServiceKeys(ro)
	assert.Len(t, keys, 1)
	assert.Equal(t, keys[0], "default/test")

	ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Name = "test.namespace"
	keys = GetRolloutVirtualServiceKeys(ro)
	assert.Len(t, keys, 1)
	assert.Equal(t, keys[0], "namespace/test")

	ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService.Name = "test.namespace.cluster.local"
	keys = GetRolloutVirtualServiceKeys(ro)
	assert.Len(t, keys, 1)
	assert.Equal(t, keys[0], "namespace/test")

	ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualService = nil
	virtualServices := []v1alpha1.IstioVirtualService{{Name: "test", Routes: nil}}
	ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualServices = virtualServices
	keys = GetRolloutVirtualServiceKeys(ro)
	assert.Len(t, keys, 1)
	assert.Equal(t, keys[0], "default/test")

	multipleVirtualService = []v1alpha1.IstioVirtualService{{Name: "test1", Routes: nil}, {Name: "test2", Routes: nil}}
	ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualServices = multipleVirtualService
	keys = GetRolloutVirtualServiceKeys(ro)
	assert.Len(t, keys, 2)
	assert.Equal(t, keys[0], "default/test1")
	assert.Equal(t, keys[1], "default/test2")

	multipleVirtualService = []v1alpha1.IstioVirtualService{{Name: "test1.namespace", Routes: nil}, {Name: "test2.namespace", Routes: nil}}
	ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualServices = multipleVirtualService
	keys = GetRolloutVirtualServiceKeys(ro)
	assert.Len(t, keys, 2)
	assert.Equal(t, keys[0], "namespace/test1")
	assert.Equal(t, keys[1], "namespace/test2")

	multipleVirtualService = []v1alpha1.IstioVirtualService{{Name: "test1.namespace.cluster.local", Routes: nil}, {Name: "test2.namespace.cluster.local", Routes: nil}}
	ro.Spec.Strategy.Canary.TrafficRouting.Istio.VirtualServices = multipleVirtualService
	keys = GetRolloutVirtualServiceKeys(ro)
	assert.Len(t, keys, 2)
	assert.Equal(t, keys[0], "namespace/test1")
	assert.Equal(t, keys[1], "namespace/test2")

}

func TestGetRolloutDesinationRuleKeys(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
			},
		},
	}
	// rollout doesn't reference any dest rule
	assert.Len(t, GetRolloutDesinationRuleKeys(ro), 0)

	// rollout references a destination rule
	ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		Istio: &v1alpha1.IstioTrafficRouting{
			DestinationRule: &v1alpha1.IstioDestinationRule{
				Name: "foo",
			},
		},
	}
	keys := GetRolloutDesinationRuleKeys(ro)
	assert.Len(t, keys, 1)
	assert.Equal(t, "default/foo", keys[0])
}
