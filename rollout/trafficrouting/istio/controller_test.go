package istio

import (
	"testing"

	"github.com/tj/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutfake "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	rolloutinformers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
)

func NewFakeIstioController() *IstioController {
	schema := runtime.NewScheme()
	rolloutClient := rolloutfake.NewSimpleClientset()
	rolloutInformerFactory := rolloutinformers.NewSharedInformerFactory(rolloutClient, 3)
	dynamicClientSet := dynamicfake.NewSimpleDynamicClient(schema)
	dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClientSet, 0)
	virtualServiceInformer := dynamicInformerFactory.ForResource(istioutil.GetIstioVirtualServiceGVR()).Informer()
	destinationRuleInformer := dynamicInformerFactory.ForResource(istioutil.GetIstioDestinationRuleGVR()).Informer()

	c := NewIstioController(IstioControllerConfig{
		ArgoprojClientSet:       rolloutClient,
		DynamicClientSet:        dynamicClientSet,
		EnqueueRollout:          func(ro interface{}) {},
		RolloutsInformer:        rolloutInformerFactory.Argoproj().V1alpha1().Rollouts(),
		VirtualServiceInformer:  virtualServiceInformer,
		DestinationRuleInformer: destinationRuleInformer,
	})
	return c
}

func TestGetReferencedVirtualServices(t *testing.T) {
	ro := v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
			},
		},
	}
	ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		Istio: &v1alpha1.IstioTrafficRouting{
			VirtualService: v1alpha1.IstioVirtualService{
				Name: "istio-vsvc-name",
			},
		},
	}
	ro.Namespace = metav1.NamespaceDefault

	t.Run("get referenced virtualService - fail", func(t *testing.T) {
		c := NewFakeIstioController()
		_, err := c.GetReferencedVirtualServices(&ro)
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio", "virtualService", "name"), "istio-vsvc-name", "virtualservices.networking.istio.io \"istio-vsvc-name\" not found")
		assert.Equal(t, expectedErr.Error(), err.Error())
	})
}
