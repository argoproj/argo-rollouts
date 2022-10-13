package istio

import (
	"context"
	"testing"
	"time"

	"github.com/tj/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutfake "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	rolloutinformers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	testutil "github.com/argoproj/argo-rollouts/test/util"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

func NewFakeIstioController(objs ...runtime.Object) *IstioController {
	var argoprojObjs []runtime.Object
	var istioObjs []runtime.Object

	for _, obj := range objs {
		switch obj.(type) {
		case *v1alpha1.Rollout:
			argoprojObjs = append(argoprojObjs, obj)
		case *unstructured.Unstructured:
			istioObjs = append(istioObjs, obj)
		}
	}

	rolloutClient := rolloutfake.NewSimpleClientset(argoprojObjs...)
	rolloutInformerFactory := rolloutinformers.NewSharedInformerFactory(rolloutClient, 0)
	dynamicClientSet := testutil.NewFakeDynamicClient(istioObjs...)
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
			VirtualService: &v1alpha1.IstioVirtualService{
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

const istiovsvc1 = `apiVersion: networking.istio.io/v1alpha3
kind: VirtualService
metadata:
  name: istio-vsvc1-name
  namespace: default
spec:
  gateways:
  - istio-rollout-gateway
  hosts:
  - istio-rollout.dev.argoproj.io
  http:
  - name: primary
    route:
    - destination:
        host: 'stable'
      weight: 100
    - destination:
        host: canary
      weight: 0`

func TestGetReferencedMultipleVirtualServices(t *testing.T) {

	multipleVirtualService := []v1alpha1.IstioVirtualService{{Name: "istio-vsvc1-name", Routes: nil}, {Name: "istio-vsvc2-name", Routes: nil}}

	ro := v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
			},
		},
	}
	ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		Istio: &v1alpha1.IstioTrafficRouting{
			VirtualServices: multipleVirtualService,
		},
	}
	ro.Namespace = metav1.NamespaceDefault

	vService := unstructuredutil.StrToUnstructuredUnsafe(istiovsvc1)

	t.Run("get referenced virtualService - fail", func(t *testing.T) {
		c := NewFakeIstioController(vService)
		_, err := c.GetReferencedVirtualServices(&ro)
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "istio", "virtualServices", "name"), "istio-vsvc2-name", "virtualservices.networking.istio.io \"istio-vsvc2-name\" not found")
		assert.Equal(t, expectedErr.Error(), err.Error())
	})
}

func TestSyncDestinationRule(t *testing.T) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-rollout",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Istio: &v1alpha1.IstioTrafficRouting{
							VirtualService: &v1alpha1.IstioVirtualService{
								Name: "istio-vsvc",
							},
							DestinationRule: &v1alpha1.IstioDestinationRule{
								Name:             "istio-destrule",
								CanarySubsetName: "canary",
								StableSubsetName: "stable",
							},
						},
					},
				},
			},
		},
	}
	destRule := unstructuredutil.StrToUnstructuredUnsafe(`
apiVersion: networking.istio.io/v1alpha3
kind: DestinationRule
metadata:
  name: istio-destrule
  namespace: default
  annotations:
    argo-rollouts.argoproj.io/managed-by-rollouts: istio-rollout
spec:
  subsets:
  - name: stable
    labels:
      app: istio-subset-split
      rollouts-pod-template-hash: abc123
  - name: canary
    labels:
      app: istio-subset-split
      rollouts-pod-template-hash: def456
`)
	{
		// Verify we don't clean a DestinationRule when it is still being referenced
		c := NewFakeIstioController(ro, destRule)
		err := c.DestinationRuleInformer.GetIndexer().Add(destRule)
		assert.NoError(t, err)
		key, err := cache.MetaNamespaceKeyFunc(destRule)
		assert.NoError(t, err)
		enqueueCalled := false
		c.EnqueueRollout = func(obj interface{}) {
			enqueueCalled = true
		}

		err = c.syncDestinationRule(context.Background(), key)
		assert.NoError(t, err)
		actions := c.DynamicClientSet.(*dynamicfake.FakeDynamicClient).Actions()
		assert.Len(t, actions, 0)
		assert.True(t, enqueueCalled)
	}

	{
		// Verify clean a DestinationRule rule when rollout no longer references it
		ro = ro.DeepCopy()
		ro.Spec.Strategy.Canary.TrafficRouting.Istio.DestinationRule = nil
		c := NewFakeIstioController(ro, destRule)
		err := c.DestinationRuleInformer.GetIndexer().Add(destRule)
		assert.NoError(t, err)
		key, err := cache.MetaNamespaceKeyFunc(destRule)
		assert.NoError(t, err)
		enqueueCalled := false
		c.EnqueueRollout = func(obj interface{}) {
			enqueueCalled = true
		}

		err = c.syncDestinationRule(context.Background(), key)
		assert.NoError(t, err)
		actions := c.DynamicClientSet.(*dynamicfake.FakeDynamicClient).Actions()
		assert.Len(t, actions, 1)
		assert.Equal(t, "update", actions[0].GetVerb())
		assert.False(t, enqueueCalled)
	}

	{
		// Verify clean a DestinationRule rule when rollout no longer exists
		c := NewFakeIstioController(destRule)
		err := c.DestinationRuleInformer.GetIndexer().Add(destRule)
		assert.NoError(t, err)
		key, err := cache.MetaNamespaceKeyFunc(destRule)
		assert.NoError(t, err)
		enqueueCalled := false
		c.EnqueueRollout = func(obj interface{}) {
			enqueueCalled = true
		}

		err = c.syncDestinationRule(context.Background(), key)
		assert.NoError(t, err)
		actions := c.DynamicClientSet.(*dynamicfake.FakeDynamicClient).Actions()
		assert.Len(t, actions, 1)
		assert.Equal(t, "update", actions[0].GetVerb())
		assert.False(t, enqueueCalled)
	}
}

func TestRun(t *testing.T) {
	// make sure we can start and top the controller
	c := NewFakeIstioController()
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	go func() {
		time.Sleep(1000 * time.Millisecond)
		c.destinationRuleWorkqueue.ShutDownWithDrain()
		cancel()
	}()
	go c.DestinationRuleInformer.Run(ctx.Done())
	go c.VirtualServiceInformer.Run(ctx.Done())
	c.Run(ctx)
}
