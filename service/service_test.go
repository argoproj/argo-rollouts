package service

import (
	"context"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/utils/queue"

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

func TestGenerateRemovePatch(t *testing.T) {
	svc := &corev1.Service{}
	assert.Equal(t, "", generateRemovePatch(svc))
	svc = &corev1.Service{
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				v1alpha1.DefaultRolloutUniqueLabelKey: "abc123",
			},
		},
	}
	assert.Equal(t, removeSelectorPatch, generateRemovePatch(svc))
	svc.Annotations = map[string]string{
		v1alpha1.ManagedByRolloutsKey: "test",
	}
	assert.Equal(t, removeSelectorAndManagedByPatch, generateRemovePatch(svc))
}

func newFakeServiceController(svc *corev1.Service, rollout *v1alpha1.Rollout) (*Controller, *k8sfake.Clientset, *fake.Clientset, map[string]int) {
	client := fake.NewSimpleClientset()
	if rollout != nil {
		client = fake.NewSimpleClientset(rollout)
	}
	kubeclient := k8sfake.NewSimpleClientset()
	if svc != nil {
		kubeclient = k8sfake.NewSimpleClientset(svc)
	}
	i := informers.NewSharedInformerFactory(client, 0)
	k8sI := kubeinformers.NewSharedInformerFactory(kubeclient, 0)

	rolloutWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	serviceWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Services")
	metricsServer := metrics.NewMetricsServer(metrics.ServerConfig{
		Addr:               "localhost:8080",
		K8SRequestProvider: &metrics.K8sRequestsCountProvider{},
	})
	c := NewController(ControllerConfig{
		Kubeclientset:     kubeclient,
		Argoprojclientset: client,
		RolloutsInformer:  i.Argoproj().V1alpha1().Rollouts(),
		ServicesInformer:  k8sI.Core().V1().Services(),
		RolloutWorkqueue:  rolloutWorkqueue,
		ServiceWorkqueue:  serviceWorkqueue,
		ResyncPeriod:      0,
		MetricsServer:     metricsServer,
	})
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
	return c, kubeclient, client, enqueuedObjects
}

func TestSyncMissingService(t *testing.T) {
	ctrl, _, _, _ := newFakeServiceController(nil, nil)

	err := ctrl.syncService(context.Background(), "default/test-service")
	assert.NoError(t, err)
}

// TestSyncMissingServiceInCache confirms that the controller does not return an error when a patch fails because the
// service does not exist anymore
func TestSyncMissingServiceInCache(t *testing.T) {
	svc := newService("test-service", 80, map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: "abc",
	})
	ctrl, _, _, _ := newFakeServiceController(svc, nil)
	ctrl.kubeclientset = k8sfake.NewSimpleClientset()
	err := ctrl.syncService(context.Background(), "default/test-service")
	assert.NoError(t, err)
}

func TestSyncServiceNotReferencedByRollout(t *testing.T) {
	svc := newService("test-service", 80, map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: "abc",
	})

	ctrl, kubeclient, _, _ := newFakeServiceController(svc, nil)

	err := ctrl.syncService(context.Background(), "default/test-service")
	assert.NoError(t, err)
	actions := kubeclient.Actions()
	assert.Len(t, actions, 1)
	patch, ok := actions[0].(k8stesting.PatchAction)
	assert.True(t, ok)
	assert.Equal(t, string(patch.GetPatch()), removeSelectorPatch)
}

// TestSyncServiceWithNoManagedBy ensures a Rollout without a managed-by but has a Rollout referencing it
// does not have the controller delete the hash selector
func TestSyncServiceWithNoManagedBy(t *testing.T) {
	svc := newService("test-service", 80, map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: "abc",
	})
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{
					ActiveService: "test-service",
				},
			},
		},
	}

	ctrl, kubeclient, client, _ := newFakeServiceController(svc, ro)

	err := ctrl.syncService(context.Background(), "default/test-service")
	assert.NoError(t, err)
	actions := kubeclient.Actions()
	assert.Len(t, actions, 0)
	// No argo api call since the controller reads from the indexer
	argoActions := client.Actions()
	assert.Len(t, argoActions, 0)
}

// TestSyncServiceWithManagedByWithNoRolloutReference ensures the service controller removes the
// pod template service and managed-by annotation if the rollout listed doesn't have reference
// the service.
func TestSyncServiceWithManagedByWithNoRolloutReference(t *testing.T) {
	svc := newService("test-service", 80, map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: "abc",
	})
	svc.Annotations = map[string]string{
		v1alpha1.ManagedByRolloutsKey: "test",
	}
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: metav1.NamespaceDefault,
		},
	}

	ctrl, kubeclient, client, _ := newFakeServiceController(svc, ro)

	err := ctrl.syncService(context.Background(), "default/test-service")
	assert.NoError(t, err)
	actions := kubeclient.Actions()
	patch, ok := actions[0].(k8stesting.PatchAction)
	assert.True(t, ok)
	assert.Equal(t, string(patch.GetPatch()), removeSelectorAndManagedByPatch)
	assert.Len(t, actions, 1)
	argoActions := client.Actions()
	assert.Len(t, argoActions, 1)
}

func TestSyncServiceReferencedByRollout(t *testing.T) {
	svc := newService("test-service", 80, map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: "abc",
	})
	svc.Annotations = map[string]string{
		v1alpha1.ManagedByRolloutsKey: "rollout",
	}
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

	ctrl, kubeclient, _, enqueuedObjects := newFakeServiceController(svc, rollout)

	err := ctrl.syncService(context.Background(), "default/test-service")
	assert.NoError(t, err)
	actions := kubeclient.Actions()
	assert.Len(t, actions, 0)
	assert.Equal(t, 1, enqueuedObjects["default/rollout"])
}

func TestRun(t *testing.T) {
	// make sure we can start and top the controller
	c, _, _, _ := newFakeServiceController(nil, nil)
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	go func() {
		time.Sleep(1000 * time.Millisecond)
		c.serviceWorkqueue.ShutDownWithDrain()
		cancel()
	}()
	c.Run(ctx, 1)
}
