package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"k8s.io/client-go/tools/cache"
)

func newService(name string, port int, selector map[string]string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports: []corev1.ServicePort{{
				Protocol:   "TCP",
				Port:       int32(port),
				TargetPort: intstr.FromInt(port),
			}},
		},
	}
}

func newFakeServiceController(svc *corev1.Service, rollout *v1alpha1.Rollout) (*ServiceController, *k8sfake.Clientset, map[string]int) {
	client := fake.NewSimpleClientset()
	kubeclient := k8sfake.NewSimpleClientset()
	i := informers.NewSharedInformerFactory(client, 0)
	k8sI := kubeinformers.NewSharedInformerFactory(kubeclient, 0)

	rolloutWorkqueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Rollouts")
	serviceWorkqueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Services")

	c := NewServiceController(kubeclient,
		k8sI.Core().V1().Services(),
		i.Argoproj().V1alpha1().Rollouts(),
		0,
		rolloutWorkqueue,
		serviceWorkqueue,
		metrics.NewMetricsServer("localhost:8080", i.Argoproj().V1alpha1().Rollouts().Lister()))
	enqueuedObjects := map[string]int{}
	c.enqueueRollout = func(obj interface{}) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
			panic(err)
		}
		count, ok := enqueuedObjects[key]
		if !ok {
			count = 0
		}
		count++
		enqueuedObjects[key] = count
	}

	if svc != nil {
		k8sI.Core().V1().Services().Informer().GetIndexer().Add(svc)
	}
	if rollout != nil {
		i.Argoproj().V1alpha1().Rollouts().Informer().GetIndexer().Add(rollout)
	}
	return c, kubeclient, enqueuedObjects
}

func TestSyncMissingService(t *testing.T) {
	ctrl, _, _ := newFakeServiceController(nil, nil)

	err := ctrl.syncService("default/test-service")
	assert.NoError(t, err)
}

func TestSyncServiceNotReferencedByRollout(t *testing.T) {
	svc := newService("test-service", 80, map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: "abc",
	})

	ctrl, kubeclient, _ := newFakeServiceController(svc, nil)

	err := ctrl.syncService("default/test-service")
	assert.NoError(t, err)
	actions := kubeclient.Actions()
	assert.Len(t, actions, 1)
	patch, ok := actions[0].(k8stesting.PatchAction)
	assert.True(t, ok)
	assert.Equal(t, string(patch.GetPatch()), `[{ "op": "remove", "path": "/spec/selector/rollouts-pod-template-hash" }]`)
}

func TestSyncServiceReferencedByRollout(t *testing.T) {
	svc := newService("test-service", 80, map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: "abc",
	})

	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{
					ActiveService:  "test-service",
					PreviewService: "test-service-preview",
				},
			},
		},
	}

	ctrl, kubeclient, enqueuedObjects := newFakeServiceController(svc, rollout)

	err := ctrl.syncService("default/test-service")
	assert.NoError(t, err)
	actions := kubeclient.Actions()
	assert.Len(t, actions, 0)
	assert.Equal(t, 1, enqueuedObjects["default/rollout"])
}
