package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/utils/queue"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	dynamicinformers "k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	register "github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"github.com/argoproj/argo-rollouts/utils/log"
	"k8s.io/client-go/tools/cache"
)

func TestProcessNextWorkItemHandlePanic(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	q.Add("valid/key")

	metricServer := metrics.NewMetricsServer(metrics.ServerConfig{
		Addr:               "localhost:8080",
		K8SRequestProvider: &metrics.K8sRequestsCountProvider{},
	})
	syncHandler := func(ctx context.Context, key string) error {
		panic("Bad big panic :(")
	}
	assert.True(t, processNextWorkItem(context.Background(), q, log.RolloutKey, syncHandler, metricServer))
}

func TestProcessNextWorkItemShutDownQueue(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	syncHandler := func(ctx context.Context, key string) error {
		return nil
	}
	q.ShutDown()
	assert.False(t, processNextWorkItem(context.Background(), q, log.RolloutKey, syncHandler, nil))
}

func TestProcessNextWorkItemNoTStringKey(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	q.Add(1)
	syncHandler := func(ctx context.Context, key string) error {
		return nil
	}
	assert.True(t, processNextWorkItem(context.Background(), q, log.RolloutKey, syncHandler, nil))
}

func TestProcessNextWorkItemNoValidKey(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	q.Add("invalid.key")
	syncHandler := func(ctx context.Context, key string) error {
		return nil
	}
	assert.True(t, processNextWorkItem(context.Background(), q, log.RolloutKey, syncHandler, nil))
}

func TestProcessNextWorkItemNormalSync(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	q.Add("valid/key")
	syncHandler := func(ctx context.Context, key string) error {
		return nil
	}
	assert.True(t, processNextWorkItem(context.Background(), q, log.RolloutKey, syncHandler, nil))
}

func TestProcessNextWorkItemSyncHandlerReturnError(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	q.Add("valid/key")
	metricServer := metrics.NewMetricsServer(metrics.ServerConfig{
		Addr:               "localhost:8080",
		K8SRequestProvider: &metrics.K8sRequestsCountProvider{},
	})
	syncHandler := func(ctx context.Context, key string) error {
		return fmt.Errorf("error message")
	}
	assert.True(t, processNextWorkItem(context.Background(), q, log.RolloutKey, syncHandler, metricServer))
}

func TestEnqueue(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	r := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName",
			Namespace: "testNamespace",
		},
	}
	Enqueue(r, q)
	assert.Equal(t, 1, q.Len())
}

func TestEnqueueInvalidObj(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	Enqueue(struct{}{}, q)
	assert.Equal(t, 0, q.Len())
}

func TestEnqueueAfter(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	r := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName",
			Namespace: "testNamespace",
		},
	}
	EnqueueAfter(r, time.Duration(1), q)
	assert.Equal(t, 0, q.Len())
	time.Sleep(2 * time.Second)
	assert.Equal(t, 1, q.Len())
}

func TestEnqueueString(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	Enqueue("default/foo", q)
	assert.Equal(t, 1, q.Len())
}

func TestEnqueueAfterInvalidObj(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	EnqueueAfter(struct{}{}, time.Duration(1), q)
	assert.Equal(t, 0, q.Len())
	time.Sleep(2 * time.Second)
	assert.Equal(t, 0, q.Len())
}

func TestEnqueueRateLimited(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	r := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName",
			Namespace: "testNamespace",
		},
	}
	EnqueueRateLimited(r, q)
	assert.Equal(t, 0, q.Len())
	time.Sleep(time.Second)
	assert.Equal(t, 1, q.Len())
}

func TestEnqueueRateLimitedInvalidObject(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	EnqueueRateLimited(struct{}{}, q)
	assert.Equal(t, 0, q.Len())
	time.Sleep(time.Second)
	assert.Equal(t, 0, q.Len())
}

func TestEnqueueParentObjectInvalidObject(t *testing.T) {
	errorMessages := make([]error, 0)
	utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers, func(err error) {
		errorMessages = append(errorMessages, err)
	})
	invalidObject := "invalid-object"
	enqueueFunc := func(obj interface{}) {}
	EnqueueParentObject(invalidObject, register.RolloutKind, enqueueFunc)
	assert.Len(t, errorMessages, 1)
	assert.Error(t, errorMessages[0], "error decoding object, invalid type")
}

func TestEnqueueParentObjectInvalidTombstoneObject(t *testing.T) {
	errorMessages := make([]string, 0)
	utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers, func(err error) {
		errorMessages = append(errorMessages, err.Error())
	})

	invalidObject := cache.DeletedFinalStateUnknown{}
	enqueueFunc := func(obj interface{}) {}
	EnqueueParentObject(invalidObject, register.RolloutKind, enqueueFunc)
	assert.Len(t, errorMessages, 1)
	assert.Equal(t, "error decoding object tombstone, invalid type", errorMessages[0])
}

func TestEnqueueParentObjectNoOwner(t *testing.T) {
	errorMessages := make([]string, 0)
	utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers, func(err error) {
		errorMessages = append(errorMessages, err.Error())
	})
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rs",
			Namespace: "default",
		},
	}
	enqueuedObjs := make([]interface{}, 0)
	enqueueFunc := func(obj interface{}) {
		enqueuedObjs = append(enqueuedObjs, obj)
	}
	EnqueueParentObject(rs, register.RolloutKind, enqueueFunc)
	assert.Len(t, errorMessages, 0)
	assert.Len(t, enqueuedObjs, 0)
}

func TestEnqueueParentObjectDifferentOwnerKind(t *testing.T) {
	experimentKind := v1alpha1.SchemeGroupVersion.WithKind("Experiment")

	errorMessages := make([]string, 0)
	utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers, func(err error) {
		errorMessages = append(errorMessages, err.Error())
	})
	experiment := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ex",
			Namespace: "default",
		},
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "rs",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(experiment, experimentKind)},
		},
	}
	enqueuedObjs := make([]interface{}, 0)
	enqueueFunc := func(obj interface{}) {
		enqueuedObjs = append(enqueuedObjs, obj)
	}
	EnqueueParentObject(rs, register.RolloutKind, enqueueFunc)
	assert.Len(t, errorMessages, 0)
	assert.Len(t, enqueuedObjs, 0)
}

func TestEnqueueParentObjectOtherOwnerTypes(t *testing.T) {
	deploymentKind := appsv1.SchemeGroupVersion.WithKind("Deployment")

	errorMessages := make([]string, 0)
	utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers, func(err error) {
		errorMessages = append(errorMessages, err.Error())
	})
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ex",
			Namespace: "default",
		},
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "rs",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(deployment, deploymentKind)},
		},
	}
	enqueuedObjs := make([]interface{}, 0)
	enqueueFunc := func(obj interface{}) {
		enqueuedObjs = append(enqueuedObjs, obj)
	}
	EnqueueParentObject(rs, "Deployment", enqueueFunc)
	assert.Len(t, errorMessages, 0)
	assert.Len(t, enqueuedObjs, 1)
}

func TestEnqueueParentObjectEnqueueExperiment(t *testing.T) {
	experimentKind := v1alpha1.SchemeGroupVersion.WithKind("Experiment")

	errorMessages := make([]string, 0)
	utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers, func(err error) {
		errorMessages = append(errorMessages, err.Error())
	})
	experiment := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ex",
			Namespace: "default",
		},
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "rs",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(experiment, experimentKind)},
		},
	}
	enqueuedObjs := make([]interface{}, 0)
	enqueueFunc := func(obj interface{}) {
		enqueuedObjs = append(enqueuedObjs, obj)
	}
	client := fake.NewSimpleClientset(experiment)
	i := informers.NewSharedInformerFactory(client, 0)
	i.Argoproj().V1alpha1().Experiments().Informer().GetIndexer().Add(experiment)

	EnqueueParentObject(rs, register.ExperimentKind, enqueueFunc)
	assert.Len(t, errorMessages, 0)
	assert.Len(t, enqueuedObjs, 1)
}

func TestEnqueueParentObjectEnqueueRollout(t *testing.T) {
	rolloutKind := v1alpha1.SchemeGroupVersion.WithKind("Rollout")

	errorMessages := make([]string, 0)
	utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers, func(err error) {
		errorMessages = append(errorMessages, err.Error())
	})
	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ex",
			Namespace: "default",
		},
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "rs",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(rollout, rolloutKind)},
		},
	}
	enqueuedObjs := make([]interface{}, 0)
	enqueueFunc := func(obj interface{}) {
		enqueuedObjs = append(enqueuedObjs, obj)
	}
	client := fake.NewSimpleClientset(rollout)
	i := informers.NewSharedInformerFactory(client, 0)
	i.Argoproj().V1alpha1().Rollouts().Informer().GetIndexer().Add(rollout)

	EnqueueParentObject(rs, register.RolloutKind, enqueueFunc)
	assert.Len(t, errorMessages, 0)
	assert.Len(t, enqueuedObjs, 1)
}

func TestEnqueueParentObjectRecoverTombstoneObject(t *testing.T) {
	experimentKind := v1alpha1.SchemeGroupVersion.WithKind("Experiment")
	errorMessages := make([]string, 0)
	utilruntime.ErrorHandlers = append(utilruntime.ErrorHandlers, func(err error) {
		errorMessages = append(errorMessages, err.Error())
	})
	experiment := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ex",
			Namespace: "default",
		},
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "rs",
			Namespace:       "default",
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(experiment, experimentKind)},
		},
	}
	invalidObject := cache.DeletedFinalStateUnknown{
		Key: "default/rs",
		Obj: rs,
	}

	enqueuedObjs := make([]interface{}, 0)
	enqueueFunc := func(obj interface{}) {
		enqueuedObjs = append(enqueuedObjs, obj)
	}
	client := fake.NewSimpleClientset(experiment)
	i := informers.NewSharedInformerFactory(client, 0)
	i.Argoproj().V1alpha1().Experiments().Informer().GetIndexer().Add(experiment)

	EnqueueParentObject(invalidObject, register.ExperimentKind, enqueueFunc)
	assert.Len(t, errorMessages, 0)
	assert.Len(t, enqueuedObjs, 1)
}

func TestInstanceIDRequirement(t *testing.T) {
	setWithLabel := labels.Set{
		v1alpha1.LabelKeyControllerInstanceID: "test",
	}
	setWithNoLabel := labels.Set{}

	instanceID := InstanceIDRequirement("test")
	noInstanceID := InstanceIDRequirement("")

	assert.True(t, instanceID.Matches(setWithLabel))
	assert.False(t, instanceID.Matches(setWithNoLabel))

	assert.False(t, noInstanceID.Matches(setWithLabel))
	assert.True(t, noInstanceID.Matches(setWithNoLabel))

	assert.Panics(t, func() { InstanceIDRequirement(".%&(") })
}

func newObj(name, kind, apiVersion string) *unstructured.Unstructured {
	obj := make(map[string]interface{})
	obj["apiVersion"] = apiVersion
	obj["kind"] = kind
	obj["metadata"] = map[string]interface{}{
		"name":      name,
		"namespace": metav1.NamespaceDefault,
	}
	return &unstructured.Unstructured{Object: obj}
}

func TestWatchResourceNotFound(t *testing.T) {
	obj := newObj("foo", "Object", "example.com/v1")
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), obj)
	gvk := schema.ParseGroupResource("objects.example.com").WithVersion("v1")
	returnsError := false
	client.PrependWatchReactor("*", func(action kubetesting.Action) (handled bool, ret watch.Interface, err error) {
		returnsError = true
		return true, nil, k8serrors.NewNotFound(gvk.GroupResource(), "virtualservices")
	})
	err := WatchResource(client, metav1.NamespaceAll, gvk, nil, nil, "not-used")
	assert.True(t, returnsError)
	assert.Equal(t, k8serrors.NewNotFound(gvk.GroupResource(), "virtualservices"), err)

	returnsError = false
	err = WatchResource(client, metav1.NamespaceDefault, gvk, nil, nil, "not-used")
	assert.True(t, returnsError)
	assert.Equal(t, k8serrors.NewNotFound(gvk.GroupResource(), "virtualservices"), err)
}

func TestWatchResourceHandleStop(t *testing.T) {
	obj := newObj("foo", "Object", "example.com/v1")
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), obj)
	gvk := schema.ParseGroupResource("objects.example.com").WithVersion("v1")
	watchI := watch.NewRaceFreeFake()
	watchI.Stop()
	client.PrependWatchReactor("*", func(action kubetesting.Action) (handled bool, ret watch.Interface, err error) {
		return true, watchI, nil
	})

	WatchResource(client, metav1.NamespaceAll, gvk, nil, nil, "not-used")
}

func TestProcessNextWatchObj(t *testing.T) {
	obj := newObj("foo", "Object", "example.com/v1")
	client := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), obj)
	gvk := schema.ParseGroupResource("objects.example.com").WithVersion("v1")
	dInformer := dynamicinformers.NewDynamicSharedInformerFactory(client, func() time.Duration { return 0 }())
	indexer := dInformer.ForResource(gvk).Informer().GetIndexer()
	indexer.AddIndexers(cache.Indexers{
		"testIndexer": func(obj interface{}) (strings []string, e error) {
			return []string{"default/foo"}, nil
		},
	})
	indexer.Add(obj)
	{
		wq := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
		watchEvent := watch.Event{
			Object: obj,
		}
		processNextWatchObj(watchEvent, wq, indexer, "testIndexer")
		assert.Equal(t, 1, wq.Len())
	}
	{
		wq := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
		watchEvent := watch.Event{
			Object: obj,
		}
		processNextWatchObj(watchEvent, wq, indexer, "no-indexer")
		assert.Equal(t, 0, wq.Len())
	}
	{
		wq := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
		invalidWatchEvent := watch.Event{}
		processNextWatchObj(invalidWatchEvent, wq, indexer, "testIndexer")
		assert.Equal(t, 0, wq.Len())
	}
}
