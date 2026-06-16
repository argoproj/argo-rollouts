package resourceversion

import "sync"

// Tracker remembers the ResourceVersion of the last successful write this controller
// made for each object key, so sync can short-circuit (with a brief requeue) when the
// informer cache hasn't yet observed our previous write.
//
// Without this guard, a reconcile enqueued while a prior write is still propagating to
// the informer can read pre-write state and act on stale inputs — or, worse, overwrite
// the informer cache directly (the writeBackToInformer anti-pattern).
//
// TODO: if Argo Rollouts moves to controller-runtime, replace this with the cached
// client's ReadYourOwnWrites option once kubernetes-sigs/controller-runtime#3473 lands.
type Tracker struct {
	lastWritten map[string]string
	mu          sync.Mutex
}

// NewTracker returns an empty tracker ready for use.
func NewTracker() *Tracker {
	return &Tracker{lastWritten: make(map[string]string)}
}

// Record stores rv as the most recent ResourceVersion we wrote for key. No-op for empty rv.
func (t *Tracker) Record(key, rv string) {
	if rv == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastWritten[key] = rv
}

// Forget drops any record for key. Call when an object has been deleted.
func (t *Tracker) Forget(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.lastWritten, key)
}

// IsCacheStale reports whether cachedRV is strictly older than the last ResourceVersion
// we wrote for key. Returns false when we have no record, when the cache is at or ahead
// of our last write, or when either ResourceVersion is malformed (fail open).
func (t *Tracker) IsCacheStale(key, cachedRV string) bool {
	if cachedRV == "" {
		return false
	}
	t.mu.Lock()
	last, ok := t.lastWritten[key]
	t.mu.Unlock()
	if !ok {
		return false
	}
	cmp, err := CompareResourceVersion(cachedRV, last)
	if err != nil {
		return false
	}
	return cmp < 0
}
