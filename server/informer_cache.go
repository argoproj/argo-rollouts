package server

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutclientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	rolloutinformers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
)

const (
	// ownerUIDIndex indexes objects by the UID of every owner reference, so the
	// ReplicaSets of a Rollout and the Pods of a ReplicaSet are found in O(owned)
	// instead of O(namespace).
	ownerUIDIndex = "byOwnerUID"

	// defaultInformerIdleTimeout is how long the informers of a namespace are kept
	// running after the last request or stream using them has finished.
	defaultInformerIdleTimeout = 5 * time.Minute

	informerSyncPollInterval = 50 * time.Millisecond
)

func indexByOwnerUID(obj any) ([]string, error) {
	meta, ok := obj.(metav1.Object)
	if !ok {
		return nil, nil
	}
	refs := meta.GetOwnerReferences()
	uids := make([]string, 0, len(refs))
	for _, ref := range refs {
		uids = append(uids, string(ref.UID))
	}
	return uids, nil
}

// namespaceCache holds the informers of a single namespace. It is shared by every
// request and stream of that namespace.
type namespaceCache struct {
	rolloutInformer    cache.SharedIndexInformer
	replicaSetInformer cache.SharedIndexInformer
	podInformer        cache.SharedIndexInformer

	cancel context.CancelFunc

	// refs and idleTimer are guarded by informerCache.mu
	refs      int
	idleTimer *time.Timer

	errLock  sync.Mutex
	watchErr error
}

func (nc *namespaceCache) hasSynced() bool {
	return nc.rolloutInformer.HasSynced() && nc.replicaSetInformer.HasSynced() && nc.podInformer.HasSynced()
}

func (nc *namespaceCache) setWatchError(err error) {
	nc.errLock.Lock()
	defer nc.errLock.Unlock()
	nc.watchErr = err
}

func (nc *namespaceCache) watchError() error {
	nc.errLock.Lock()
	defer nc.errLock.Unlock()
	return nc.watchErr
}

// waitForSync blocks until the informers are synced. It returns early when the context
// is done, or when an informer failed to list before the first sync, so that a
// namespace which cannot be listed returns an error instead of blocking.
func (nc *namespaceCache) waitForSync(ctx context.Context) error {
	ticker := time.NewTicker(informerSyncPollInterval)
	defer ticker.Stop()
	for {
		if nc.hasSynced() {
			return nil
		}
		if err := nc.watchError(); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// rolloutInfo returns the shallow RolloutInfo (ReplicaSets and Pods, no Experiments or
// AnalysisRuns) of a Rollout.
func (nc *namespaceCache) rolloutInfo(ro *v1alpha1.Rollout) (*rollout.RolloutInfo, error) {
	rsObjs, err := nc.replicaSetInformer.GetIndexer().ByIndex(ownerUIDIndex, string(ro.UID))
	if err != nil {
		return nil, err
	}
	replicaSets := make([]*appsv1.ReplicaSet, 0, len(rsObjs))
	var pods []*corev1.Pod
	for _, rsObj := range rsObjs {
		rs, ok := rsObj.(*appsv1.ReplicaSet)
		if !ok {
			continue
		}
		replicaSets = append(replicaSets, rs)
		podObjs, err := nc.podInformer.GetIndexer().ByIndex(ownerUIDIndex, string(rs.UID))
		if err != nil {
			return nil, err
		}
		for _, podObj := range podObjs {
			if pod, ok := podObj.(*corev1.Pod); ok {
				pods = append(pods, pod)
			}
		}
	}
	return info.NewRolloutInfo(ro, replicaSets, pods, nil, nil, nil), nil
}

// rollouts returns every cached Rollout, sorted by namespace and name.
func (nc *namespaceCache) rollouts() []*v1alpha1.Rollout {
	objs := nc.rolloutInformer.GetIndexer().List()
	rollouts := make([]*v1alpha1.Rollout, 0, len(objs))
	for _, obj := range objs {
		if ro, ok := obj.(*v1alpha1.Rollout); ok {
			rollouts = append(rollouts, ro)
		}
	}
	sort.Slice(rollouts, func(i, j int) bool {
		if rollouts[i].Namespace != rollouts[j].Namespace {
			return rollouts[i].Namespace < rollouts[j].Namespace
		}
		return rollouts[i].Name < rollouts[j].Name
	})
	return rollouts
}

// rollout returns the cached Rollout of a namespace/name key, or nil if it is not cached.
func (nc *namespaceCache) rollout(key string) (*v1alpha1.Rollout, error) {
	obj, exists, err := nc.rolloutInformer.GetIndexer().GetByKey(key)
	if err != nil || !exists {
		return nil, err
	}
	ro, _ := obj.(*v1alpha1.Rollout)
	return ro, nil
}

// rolloutKeysForPod returns the namespace/name keys of the Rollouts which own the
// ReplicaSets of a Pod.
func (nc *namespaceCache) rolloutKeysForPod(pod *corev1.Pod) []string {
	var keys []string
	for _, podOwner := range pod.GetOwnerReferences() {
		if podOwner.Kind != "ReplicaSet" {
			continue
		}
		obj, exists, err := nc.replicaSetInformer.GetIndexer().GetByKey(pod.Namespace + "/" + podOwner.Name)
		if err != nil || !exists {
			continue
		}
		rs, ok := obj.(*appsv1.ReplicaSet)
		if !ok {
			continue
		}
		for _, rsOwner := range rs.GetOwnerReferences() {
			if rsOwner.APIVersion == v1alpha1.SchemeGroupVersion.String() && rsOwner.Kind == "Rollout" {
				keys = append(keys, rs.Namespace+"/"+rsOwner.Name)
			}
		}
	}
	return keys
}

// informerCache lazily creates one namespaceCache per namespace and stops it once it
// has been unused for idleTimeout.
type informerCache struct {
	kubeClientset     kubernetes.Interface
	rolloutsClientset rolloutclientset.Interface
	idleTimeout       time.Duration

	mu         sync.Mutex
	namespaces map[string]*namespaceCache
}

func newInformerCache(kubeClientset kubernetes.Interface, rolloutsClientset rolloutclientset.Interface) *informerCache {
	return &informerCache{
		kubeClientset:     kubeClientset,
		rolloutsClientset: rolloutsClientset,
		idleTimeout:       defaultInformerIdleTimeout,
		namespaces:        map[string]*namespaceCache{},
	}
}

// acquire returns the synced namespaceCache of a namespace. The returned release func
// must be called when the caller is done with the cache.
func (c *informerCache) acquire(ctx context.Context, namespace string) (*namespaceCache, func(), error) {
	c.mu.Lock()
	nc, ok := c.namespaces[namespace]
	if !ok {
		var err error
		nc, err = c.newNamespaceCache(namespace)
		if err != nil {
			c.mu.Unlock()
			return nil, nil, err
		}
		c.namespaces[namespace] = nc
	}
	nc.refs++
	if nc.idleTimer != nil {
		nc.idleTimer.Stop()
		nc.idleTimer = nil
	}
	c.mu.Unlock()

	var once sync.Once
	release := func() {
		once.Do(func() { c.release(namespace, nc) })
	}
	if err := nc.waitForSync(ctx); err != nil {
		release()
		return nil, nil, err
	}
	return nc, release, nil
}

func (c *informerCache) release(namespace string, nc *namespaceCache) {
	c.mu.Lock()
	defer c.mu.Unlock()
	nc.refs--
	if nc.refs > 0 {
		return
	}
	nc.idleTimer = time.AfterFunc(c.idleTimeout, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if nc.refs > 0 || c.namespaces[namespace] != nc {
			return
		}
		nc.cancel()
		delete(c.namespaces, namespace)
	})
}

func (c *informerCache) newNamespaceCache(namespace string) (*namespaceCache, error) {
	rolloutsInformerFactory := rolloutinformers.NewSharedInformerFactoryWithOptions(c.rolloutsClientset, 0, rolloutinformers.WithNamespace(namespace))
	kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(c.kubeClientset, 0, kubeinformers.WithNamespace(namespace))

	nc := &namespaceCache{
		rolloutInformer:    rolloutsInformerFactory.Argoproj().V1alpha1().Rollouts().Informer(),
		replicaSetInformer: kubeInformerFactory.Apps().V1().ReplicaSets().Informer(),
		podInformer:        kubeInformerFactory.Core().V1().Pods().Informer(),
	}

	for _, informer := range []cache.SharedIndexInformer{nc.replicaSetInformer, nc.podInformer} {
		if err := informer.AddIndexers(cache.Indexers{ownerUIDIndex: indexByOwnerUID}); err != nil {
			return nil, fmt.Errorf("failed to add %s indexer: %w", ownerUIDIndex, err)
		}
	}
	for _, informer := range []cache.SharedIndexInformer{nc.rolloutInformer, nc.replicaSetInformer, nc.podInformer} {
		err := informer.SetWatchErrorHandlerWithContext(func(ctx context.Context, r *cache.Reflector, err error) {
			cache.DefaultWatchErrorHandler(ctx, r, err)
			nc.setWatchError(err)
		})
		if err != nil {
			return nil, fmt.Errorf("failed to set watch error handler: %w", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	nc.cancel = cancel
	rolloutsInformerFactory.Start(ctx.Done())
	kubeInformerFactory.Start(ctx.Done())
	return nc, nil
}
