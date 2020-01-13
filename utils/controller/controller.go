package controller

import (
	"fmt"
	"runtime/debug"
	"time"

	log "github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

// processNextWatchObj will process a single object from the watch by seeing if
// that object is in an index and enqueueing the value object from the indexer
func processNextWatchObj(watchEvent watch.Event, queue workqueue.RateLimitingInterface, indexer cache.Indexer, index string) {
	obj := watchEvent.Object
	acc, err := meta.Accessor(obj)
	if err != nil {
		log.Errorf("Error processing object from watch: %v", err)
		return
	}
	objsToEnqueue, err := indexer.ByIndex(index, fmt.Sprintf("%s/%s", acc.GetNamespace(), acc.GetName()))
	if err != nil {
		log.Errorf("Cannot process indexer: %s", err.Error())
		return
	}
	for i := range objsToEnqueue {
		Enqueue(objsToEnqueue[i], queue)
	}
}

// WatchResource is a long-running function that will continually call the
// processNextWatchObj function in order to watch changes on a resource kind
// and enqueue a different resources kind that interact with them
func WatchResource(client dynamic.Interface, gvk schema.GroupVersionResource, queue workqueue.RateLimitingInterface, indexer cache.Indexer, index string) {
	log.Infof("Starting watch on resource '%s'", gvk.Resource)
	watch, err := client.Resource(gvk).Watch(metav1.ListOptions{})
	if k8serrors.IsNotFound(err) {
		log.Infof("Watch for resource '%s' failed: resource not found", gvk.Resource)
		return
	}
	if err != nil {
		panic(err)
	}
	for watchEvent := range watch.ResultChan() {
		processNextWatchObj(watchEvent, queue, indexer, index)
	}
}

// RunWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func RunWorker(workqueue workqueue.RateLimitingInterface, objType string, syncHandler func(string) error, metricServer *metrics.MetricsServer) {
	for processNextWorkItem(workqueue, objType, syncHandler, metricServer) {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func processNextWorkItem(workqueue workqueue.RateLimitingInterface, objType string, syncHandler func(string) error, metricsServer *metrics.MetricsServer) bool {
	obj, shutdown := workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			workqueue.Forget(obj)
			runtime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		namespace, name, err := cache.SplitMetaNamespaceKey(key)
		logCtx := log.WithField(objType, name).WithField(logutil.NamespaceKey, namespace)
		if err != nil {
			return err
		}
		runSyncHandler := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					logCtx.Errorf("Recovered from panic: %+v\n%s", r, debug.Stack())
					err = fmt.Errorf("Recovered from Panic")
				}
			}()
			return syncHandler(key)
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// Rollout resource to be synced.
		if err := runSyncHandler(); err != nil {
			metricsServer.IncError(namespace, name)
			// Put the item back on the workqueue to handle any transient errors.
			workqueue.AddRateLimited(key)
			return err
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		workqueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		runtime.HandleError(err)
		return true
	}

	return true
}

func Enqueue(obj interface{}, q workqueue.RateLimitingInterface) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	q.Add(key)
}

func EnqueueAfter(obj interface{}, duration time.Duration, q workqueue.RateLimitingInterface) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	q.AddAfter(key, duration)
}

func EnqueueRateLimited(obj interface{}, q workqueue.RateLimitingInterface) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	q.AddRateLimited(key)
}

// EnqueueParentObject will take any resource implementing metav1.Object and attempt
// to find the ownerType resource that 'owns' it. It does this by looking at the
// objects metadata.ownerReferences field for an appropriate OwnerReference.
// It then enqueues that ownerType resource to be processed. If the object does not
// have an appropriate OwnerReference, it will simply be skipped.
// This function assumes parent object is in the same namespace as the child
func EnqueueParentObject(obj interface{}, ownerType string, enqueue func(obj interface{})) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			runtime.HandleError(fmt.Errorf("error decoding object, invalid type"))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			runtime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
			return
		}
		log.Infof("Recovered deleted object '%s' from tombstone", object.GetName())
	}

	if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
		// If this object is not owned by the ownerType, we should not do anything more with it.
		if ownerRef.Kind != ownerType {
			return
		}
		namespace := object.GetNamespace()
		parent := cache.ExplicitKey(namespace + "/" + ownerRef.Name)
		log.Infof("Enqueuing parent of %s/%s: %s %s", namespace, object.GetName(), ownerRef.Kind, parent)
		enqueue(parent)
	}
}

// InstanceIDRequirement returns the label requirement to filter against a controller instance (or not)
func InstanceIDRequirement(instanceID string) labels.Requirement {
	var instanceIDReq *labels.Requirement
	var err error
	if instanceID != "" {
		instanceIDReq, err = labels.NewRequirement(v1alpha1.LabelKeyControllerInstanceID, selection.Equals, []string{instanceID})
	} else {
		instanceIDReq, err = labels.NewRequirement(v1alpha1.LabelKeyControllerInstanceID, selection.DoesNotExist, nil)
	}
	if err != nil {
		panic(err)
	}
	return *instanceIDReq
}
