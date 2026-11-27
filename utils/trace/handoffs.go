package trace

import (
	"container/list"
	"strconv"
	"sync"
	"time"

	oteltrace "go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	handoffCapacity = 10_000
	handoffTTL      = 24 * time.Hour
)

// Handoffs retains controller-created canary-step lineage within one manager.
// Entries expire without extending controller lifetimes or changing reconciliation.
// Restart, eviction, and missing evidence leave attempts as independent roots.
// The zero value is usable. A nil Handoffs disables correlation.
type Handoffs struct {
	mu       sync.Mutex
	entries  map[types.UID]*list.Element
	order    list.List
	capacity int
	ttl      time.Duration
	now      func() time.Time
}

type handoff struct {
	runUID            types.UID
	runName           string
	namespace         string
	runGeneration     int64
	rolloutUID        types.UID
	rolloutName       string
	rolloutGeneration int64
	podHash           string
	stepIndex         int32
	creator           oteltrace.SpanContext
	result            oteltrace.SpanContext
	resultVersion     string
	resultPhase       v1alpha1.AnalysisPhase
	resultPublishedAt time.Time
	expiresAt         time.Time
}

// NewHandoffs creates a bounded, manager-local registry. It trusts only contexts
// supplied at controller API-write hooks and never reads annotation carriers.
func NewHandoffs() *Handoffs {
	return newHandoffs(handoffCapacity, handoffTTL, time.Now)
}

func newHandoffs(capacity int, ttl time.Duration, now func() time.Time) *Handoffs {
	return &Handoffs{capacity: capacity, ttl: ttl, now: now}
}

// RecordCreation records a successful API Create, never an adopted existing run.
// The caller must supply the returned API object and the creating Rollout span.
// The child's pod hash is authoritative here because Rollout status can lag it.
func (h *Handoffs) RecordCreation(ro *v1alpha1.Rollout, run *v1alpha1.AnalysisRun, sc oteltrace.SpanContext) {
	if h == nil || !sc.IsValid() || !validCreation(ro, run) {
		return
	}
	step, _ := canaryStepIndex(run)
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.clock()
	h.prune(now)
	if _, found := h.entries[run.UID]; found {
		return
	}
	for len(h.entries) >= h.capacity {
		h.remove(h.order.Front())
	}
	entry := &handoff{
		runUID: run.UID, runName: run.Name, namespace: run.Namespace,
		runGeneration: run.Generation, rolloutUID: ro.UID, rolloutName: ro.Name,
		rolloutGeneration: ro.Generation, podHash: run.Labels[v1alpha1.DefaultRolloutUniqueLabelKey],
		stepIndex: step, creator: sc, expiresAt: now.Add(h.ttl),
	}
	h.entries[run.UID] = h.order.PushBack(entry)
}

// CreationParent returns a creator only while the exact generated run remains
// the current canary gate for the recorded Rollout generation and pod template.
func (h *Handoffs) CreationParent(ro *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) oteltrace.SpanContext {
	if h == nil || run == nil {
		return oteltrace.SpanContext{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.prune(h.clock())
	entry := h.entry(run.UID)
	if entry == nil || !entry.matches(ro, run) {
		return oteltrace.SpanContext{}
	}
	return entry.creator
}

// RecordResult publishes the context only after a successful terminal status
// write. The caller must also verify that the response contains its intended
// status. Failed writes and no-op reconciliations must not call this method.
func (h *Handoffs) RecordResult(orig, persistedRun *v1alpha1.AnalysisRun, sc oteltrace.SpanContext) {
	if h == nil || orig == nil || persistedRun == nil || !sc.IsValid() ||
		orig.Status.Phase.Completed() || !persistedRun.Status.Phase.Completed() || persistedRun.ResourceVersion == "" ||
		orig.ResourceVersion == persistedRun.ResourceVersion {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.clock()
	h.prune(now)
	entry := h.entry(orig.UID)
	if entry == nil || entry.resultVersion != "" || !entry.matchesRun(orig) || !entry.matchesRun(persistedRun) {
		return
	}
	entry.result = sc
	entry.resultVersion = persistedRun.ResourceVersion
	entry.resultPhase = persistedRun.Status.Phase
	entry.resultPublishedAt = now
}

// ResultParent returns the terminal writer only for its exact persisted result
// version, while Rollout status still records a different phase for this gate.
// A backdated attempt cannot accept a result published after the attempt began.
func (h *Handoffs) ResultParent(ro *v1alpha1.Rollout, run *v1alpha1.AnalysisRun, attemptStarted time.Time) oteltrace.SpanContext {
	if h == nil || run == nil || attemptStarted.IsZero() {
		return oteltrace.SpanContext{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.prune(h.clock())
	entry := h.entry(run.UID)
	if entry == nil || !entry.matches(ro, run) || !entry.matchesResult(run) || entry.resultPublishedAt.After(attemptStarted) ||
		ro.Status.Canary.CurrentStepAnalysisRunStatus.Status == run.Status.Phase {
		return oteltrace.SpanContext{}
	}
	return entry.result
}

// ConsumeResult clears only the exact result the caller has persisted as
// consumed. It does not revalidate the step, which may already have advanced.
func (h *Handoffs) ConsumeResult(run *v1alpha1.AnalysisRun, expectedParent oteltrace.SpanContext) {
	if h == nil || run == nil || !expectedParent.IsValid() {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.prune(h.clock())
	entry := h.entry(run.UID)
	if entry == nil || !entry.matchesResult(run) || !entry.result.Equal(expectedParent) {
		return
	}
	entry.result = oteltrace.SpanContext{}
	// Keep the version as a tombstone so a stale retry cannot republish a result.
}

func validCreation(ro *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) bool {
	if ro == nil || run == nil || ro.UID == "" || ro.Name == "" || ro.Namespace == "" || ro.Generation <= 0 ||
		run.UID == "" || run.Name == "" || run.Namespace != ro.Namespace || run.Generation <= 0 ||
		run.ResourceVersion == "" || ro.DeletionTimestamp != nil || run.DeletionTimestamp != nil ||
		run.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] == "" || !ownedBy(run, ro.Name, ro.UID) {
		return false
	}
	step, ok := canaryStepIndex(run)
	return ok && isAnalysisStep(ro, step)
}

func (e *handoff) matches(ro *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) bool {
	if ro == nil || ro.DeletionTimestamp != nil || ro.UID != e.rolloutUID || ro.Name != e.rolloutName ||
		ro.Namespace != e.namespace || ro.Generation != e.rolloutGeneration ||
		ro.Status.CurrentPodHash != e.podHash || ro.Status.CurrentStepIndex == nil ||
		*ro.Status.CurrentStepIndex != e.stepIndex || !isAnalysisStep(ro, e.stepIndex) {
		return false
	}
	ref := ro.Status.Canary.CurrentStepAnalysisRunStatus
	return ref != nil && ref.Name == e.runName && e.matchesRun(run)
}

func (e *handoff) matchesRun(run *v1alpha1.AnalysisRun) bool {
	if run == nil || run.DeletionTimestamp != nil || run.UID != e.runUID || run.Name != e.runName ||
		run.Namespace != e.namespace || run.Generation != e.runGeneration || run.ResourceVersion == "" ||
		run.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] != e.podHash || !ownedBy(run, e.rolloutName, e.rolloutUID) {
		return false
	}
	step, ok := canaryStepIndex(run)
	return ok && step == e.stepIndex
}

func (e *handoff) matchesResult(run *v1alpha1.AnalysisRun) bool {
	return e.result.IsValid() && run.UID == e.runUID && run.ResourceVersion == e.resultVersion &&
		run.Status.Phase == e.resultPhase
}

func ownedBy(run *v1alpha1.AnalysisRun, name string, uid types.UID) bool {
	owner := metav1.GetControllerOf(run)
	return owner != nil && owner.APIVersion == v1alpha1.SchemeGroupVersion.String() &&
		owner.Kind == "Rollout" && owner.Name == name && owner.UID == uid
}

func canaryStepIndex(run *v1alpha1.AnalysisRun) (int32, bool) {
	if run.Labels[v1alpha1.RolloutTypeLabel] != v1alpha1.RolloutTypeStepLabel {
		return 0, false
	}
	value := run.Labels[v1alpha1.RolloutCanaryStepIndexLabel]
	step, err := strconv.ParseInt(value, 10, 32)
	return int32(step), err == nil && step >= 0 && strconv.FormatInt(step, 10) == value
}

func isAnalysisStep(ro *v1alpha1.Rollout, step int32) bool {
	return ro.Spec.Strategy.Canary != nil && step >= 0 && int(step) < len(ro.Spec.Strategy.Canary.Steps) &&
		ro.Spec.Strategy.Canary.Steps[step].Analysis != nil
}

func (h *Handoffs) entry(uid types.UID) *handoff {
	if element := h.entries[uid]; element != nil {
		return element.Value.(*handoff)
	}
	return nil
}

func (h *Handoffs) clock() time.Time {
	if h.now == nil {
		return time.Now()
	}
	return h.now()
}

// prune also initializes the zero value. Creation order is expiry order because
// result writes and lookups never extend an entry's lifetime.
func (h *Handoffs) prune(now time.Time) {
	if h.entries == nil {
		h.entries = make(map[types.UID]*list.Element)
	}
	if h.capacity <= 0 {
		h.capacity = handoffCapacity
	}
	if h.ttl <= 0 {
		h.ttl = handoffTTL
	}
	for element := h.order.Front(); element != nil; element = h.order.Front() {
		if now.Before(element.Value.(*handoff).expiresAt) {
			break
		}
		h.remove(element)
	}
}

func (h *Handoffs) remove(element *list.Element) {
	delete(h.entries, element.Value.(*handoff).runUID)
	h.order.Remove(element)
}
