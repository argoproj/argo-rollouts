package istio

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
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

	// rollout references a destination rule in another namespace
	ro.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule.Namespace = "istio-routing"
	keys = GetRolloutDesinationRuleKeys(ro)
	assert.Len(t, keys, 1)
	assert.Equal(t, "istio-routing/foo", keys[0])
}

func TestGetVirtualServiceNamespaceName(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		expectedNamespace string
		expectedName      string
	}{
		{
			name:              "bare name, no namespace",
			input:             "virtualservice",
			expectedNamespace: "",
			expectedName:      "virtualservice",
		},
		{
			name:              "name.namespace cross-namespace shorthand",
			input:             "virtualservice.istio",
			expectedNamespace: "istio",
			expectedName:      "virtualservice",
		},
		{
			// Issue #4709: a VirtualService whose name contains a period must
			// not have that period treated as a namespace separator. The VS
			// "virtualservice.example" lives in namespace "istio" and is
			// referenced as "virtualservice.example.istio". The namespace is the
			// last segment; everything before it is the name.
			name:              "name with period plus cross-namespace",
			input:             "virtualservice.example.istio",
			expectedNamespace: "istio",
			expectedName:      "virtualservice.example",
		},
		{
			name:              "multiple periods in name plus namespace",
			input:             "my.virtual.service.istio",
			expectedNamespace: "istio",
			expectedName:      "my.virtual.service",
		},
		{
			name:              "fully qualified with cluster.local suffix",
			input:             "test.namespace.cluster.local",
			expectedNamespace: "namespace",
			expectedName:      "test",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ns, name := GetVirtualServiceNamespaceName(tc.input)
			assert.Equal(t, tc.expectedNamespace, ns)
			assert.Equal(t, tc.expectedName, name)
		})
	}
}

func TestGetVirtualServiceRefNamespaceName(t *testing.T) {
	tests := []struct {
		name              string
		vsvc              v1alpha1.IstioVirtualService
		expectedNamespace string
		expectedName      string
	}{
		{
			name:              "no namespace defaults to the rollout namespace",
			vsvc:              v1alpha1.IstioVirtualService{Name: "vsvc"},
			expectedNamespace: "default",
			expectedName:      "vsvc",
		},
		{
			name:              "namespace field",
			vsvc:              v1alpha1.IstioVirtualService{Name: "vsvc", Namespace: "istio-routing"},
			expectedNamespace: "istio-routing",
			expectedName:      "vsvc",
		},
		{
			name:              "name.namespace shorthand",
			vsvc:              v1alpha1.IstioVirtualService{Name: "vsvc.istio-routing"},
			expectedNamespace: "istio-routing",
			expectedName:      "vsvc",
		},
		{
			name:              "name.namespace shorthand takes precedence over the namespace field",
			vsvc:              v1alpha1.IstioVirtualService{Name: "vsvc.istio-routing", Namespace: "other"},
			expectedNamespace: "istio-routing",
			expectedName:      "vsvc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			namespace, name := GetVirtualServiceRefNamespaceName(tt.vsvc, metav1.NamespaceDefault)
			assert.Equal(t, tt.expectedNamespace, namespace)
			assert.Equal(t, tt.expectedName, name)
		})
	}
}

func TestGetRolloutVirtualServiceKeysNamespaceField(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Istio: &v1alpha1.IstioTrafficRouting{
							VirtualServices: []v1alpha1.IstioVirtualService{
								{Name: "test1", Namespace: "istio-routing"},
								{Name: "test2.other", Namespace: "istio-routing"},
								{Name: "test3"},
							},
						},
					},
				},
			},
		},
	}
	assert.Equal(t, []string{"istio-routing/test1", "other/test2", "default/test3"}, GetRolloutVirtualServiceKeys(ro))
}

func TestDestinationRuleManagedByValue(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
	}

	// same namespace keeps the plain Rollout name
	value := GetDestinationRuleManagedByValue(ro, metav1.NamespaceDefault)
	assert.Equal(t, "foo", value)
	namespace, name := ParseDestinationRuleManagedByValue(value, metav1.NamespaceDefault)
	assert.Equal(t, metav1.NamespaceDefault, namespace)
	assert.Equal(t, "foo", name)

	// another namespace records the Rollout namespace too
	value = GetDestinationRuleManagedByValue(ro, "istio-routing")
	assert.Equal(t, "default/foo", value)
	namespace, name = ParseDestinationRuleManagedByValue(value, "istio-routing")
	assert.Equal(t, metav1.NamespaceDefault, namespace)
	assert.Equal(t, "foo", name)
}
