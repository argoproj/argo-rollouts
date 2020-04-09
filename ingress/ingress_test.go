package ingress

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"k8s.io/client-go/tools/cache"
)

func newNginxIngress(name string, port int, serviceName string) *extensionsv1beta1.Ingress {
	return &extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{
				"kubernetes.io/ingress.class": "nginx",
			},
		},
		Spec: extensionsv1beta1.IngressSpec{
			Rules: []extensionsv1beta1.IngressRule{
				{
					Host: "fakehost.example.com",
					IngressRuleValue: extensionsv1beta1.IngressRuleValue{
						HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
							Paths: []extensionsv1beta1.HTTPIngressPath{
								{
									Path: "/foo",
									Backend: extensionsv1beta1.IngressBackend{
										ServiceName: serviceName,
										ServicePort: intstr.FromInt(port),
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func newFakeIngressController(ing *extensionsv1beta1.Ingress, rollout *v1alpha1.Rollout) (*Controller, *k8sfake.Clientset, map[string]int) {
	client := fake.NewSimpleClientset()
	if rollout != nil {
		client = fake.NewSimpleClientset(rollout)
	}
	kubeclient := k8sfake.NewSimpleClientset()
	if ing != nil {
		kubeclient = k8sfake.NewSimpleClientset(ing)
	}
	i := informers.NewSharedInformerFactory(client, 0)
	k8sI := kubeinformers.NewSharedInformerFactory(kubeclient, 0)

	rolloutWorkqueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Rollouts")
	ingressWorkqueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Ingresses")

	c := NewController(ControllerConfig{
		Client:           kubeclient,
		IngressInformer:  k8sI.Extensions().V1beta1().Ingresses(),
		IngressWorkQueue: ingressWorkqueue,

		RolloutsInformer: i.Argoproj().V1alpha1().Rollouts(),
		RolloutWorkQueue: rolloutWorkqueue,
		ALBClasses:       []string{"alb"},
		NGINXClasses:     []string{"nginx"},
		MetricsServer: metrics.NewMetricsServer(metrics.ServerConfig{
			Addr:               "localhost:8080",
			K8SRequestProvider: &metrics.K8sRequestsCountProvider{},
		}),
	})
	enqueuedObjects := map[string]int{}
	var enqueuedObjectsLock sync.Mutex
	c.enqueueRollout = func(obj interface{}) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
			panic(err)
		}
		enqueuedObjectsLock.Lock()
		defer enqueuedObjectsLock.Unlock()
		count, ok := enqueuedObjects[key]
		if !ok {
			count = 0
		}
		count++
		enqueuedObjects[key] = count
	}

	if ing != nil {
		k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(ing)
	}
	if rollout != nil {
		i.Argoproj().V1alpha1().Rollouts().Informer().GetIndexer().Add(rollout)
	}
	return c, kubeclient, enqueuedObjects
}

func TestSyncMissingIngress(t *testing.T) {
	ctrl, _, _ := newFakeIngressController(nil, nil)

	err := ctrl.syncIngress("default/test-ingress")
	assert.NoError(t, err)
}

func TestSyncIngressNotReferencedByRollout(t *testing.T) {
	ing := newNginxIngress("test-stable-ingress", 80, "test-stable-service")

	ctrl, kubeclient, _ := newFakeIngressController(ing, nil)

	err := ctrl.syncIngress("default/test-stable-ingress")
	assert.NoError(t, err)
	actions := kubeclient.Actions()
	assert.Len(t, actions, 0)
}

func TestSyncIngressReferencedByRollout(t *testing.T) {
	ing := newNginxIngress("test-stable-ingress", 80, "stable-service")

	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: "stable-service",
					CanaryService: "canary-service",
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Nginx: &v1alpha1.NginxTrafficRouting{
							StableIngress: "test-stable-ingress",
						},
					},
				},
			},
		},
	}

	ctrl, kubeclient, enqueuedObjects := newFakeIngressController(ing, rollout)

	err := ctrl.syncIngress("default/test-stable-ingress")
	assert.NoError(t, err)
	actions := kubeclient.Actions()
	assert.Len(t, actions, 0)
	assert.Equal(t, 1, enqueuedObjects["default/rollout"])
}

func TestSkipIngressWithNoAnnotations(t *testing.T) {
	ing := newNginxIngress("test-stable-ingress", 80, "stable-service")
	ing.Annotations = nil
	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: "stable-service",
					CanaryService: "canary-service",
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Nginx: &v1alpha1.NginxTrafficRouting{
							StableIngress: "test-stable-ingress",
						},
					},
				},
			},
		},
	}

	ctrl, kubeclient, enqueuedObjects := newFakeIngressController(ing, rollout)

	err := ctrl.syncIngress("default/test-stable-ingress")
	assert.NoError(t, err)
	actions := kubeclient.Actions()
	assert.Len(t, actions, 0)
	assert.Len(t, enqueuedObjects, 0)
}
