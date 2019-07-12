package controller

import (
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	register "github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
	"github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

// runWorker is a long-running function that will continually call the
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
		if err != nil {
			return err
		}

		// Run the syncHandler, passing it the namespace/name string of the
		// Rollout resource to be synced.
		if err := syncHandler(key); err != nil {
			metricsServer.IncError(namespace, name)
			// Put the item back on the workqueue to handle any transient errors.
			workqueue.AddRateLimited(key)
			return err
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		workqueue.Forget(obj)
		log.WithField(objType, name).WithField(logutil.NamespaceKey, namespace).Info("Successfully synced")
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
func EnqueueParentObject(obj interface{}, ownerType string, lister interface{}, enqueue func(obj interface{})) {
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
	log.Infof("Processing object: %s", object.GetName())
	if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
		// If this object is not owned by the ownerType, we should not do anything more
		// with it.
		if ownerRef.Kind != ownerType {
			return
		}
		var parentObj interface{}
		var err error
		switch ownerType {
		case register.RolloutKind:
			parentObj, err = lister.(v1alpha1.RolloutLister).Rollouts(object.GetNamespace()).Get(ownerRef.Name)
		case register.ExperimentKind:
			parentObj, err = lister.(v1alpha1.ExperimentLister).Experiments(object.GetNamespace()).Get(ownerRef.Name)
		default:
			panic("OwnerType of parent is not a Rollout or a Experiment")
		}

		if err != nil {
			log.Infof("ignoring orphaned object '%s' of %s '%s'", object.GetSelfLink(), ownerType, ownerRef.Name)
			return
		}

		enqueue(parentObj)
		return
	}
}