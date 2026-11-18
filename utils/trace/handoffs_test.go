package trace

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	oteltrace "go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestHandoffsCreationAndResultLineage(t *testing.T) {
	ro, run := handoffObjects()
	creator, result := handoffContext(1), handoffContext(2)
	h := NewHandoffs()
	h.RecordCreation(ro, run, creator)
	require.True(t, h.CreationParent(ro, run).Equal(creator))

	// Status writes do not change the creating attempt's identity.
	run.ResourceVersion = "status-update"
	require.True(t, h.CreationParent(ro, run).Equal(creator))
	terminal := terminalRun(run)
	h.RecordResult(run, terminal, result)
	require.True(t, h.ResultParent(ro, terminal, time.Now()).Equal(result))
	require.True(t, h.ResultParent(ro, terminal, time.Now()).Equal(result), "an unconsumed retry retains its causal parent")

	h.ConsumeResult(terminal, creator)
	require.True(t, h.ResultParent(ro, terminal, time.Now()).Equal(result), "another span cannot consume the result")
	wrongVersion := terminal.DeepCopy()
	wrongVersion.ResourceVersion = "another-write"
	h.ConsumeResult(wrongVersion, result)
	require.True(t, h.ResultParent(ro, terminal, time.Now()).Equal(result), "another result version cannot consume the result")

	// Consumption remains valid after the caller has advanced the Rollout step.
	*ro.Status.CurrentStepIndex = 1
	h.ConsumeResult(terminal, result)
	*ro.Status.CurrentStepIndex = 0
	assert.False(t, h.ResultParent(ro, terminal, time.Now()).IsValid())
	assert.True(t, h.CreationParent(ro, terminal).Equal(creator))
	h.RecordResult(run, terminal, handoffContext(3))
	assert.False(t, h.ResultParent(ro, terminal, time.Now()).IsValid(), "a stale retry cannot republish a consumed result")
}

func TestHandoffsCreationUsesReturnedPodHash(t *testing.T) {
	ro, run := handoffObjects()
	ro.Status.CurrentPodHash = "previous-template"
	h := NewHandoffs()
	creator := handoffContext(1)
	h.RecordCreation(ro, run, creator)
	assert.False(t, h.CreationParent(ro, run).IsValid())
	ro.Status.CurrentPodHash = run.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	assert.True(t, h.CreationParent(ro, run).Equal(creator))
}

func TestHandoffsResultMustBePublishedBeforeAttempt(t *testing.T) {
	now := time.Now()
	h := newHandoffs(2, time.Hour, func() time.Time { return now })
	ro, run := handoffObjects()
	h.RecordCreation(ro, run, handoffContext(1))
	attemptStarted := now.Add(time.Second)
	// The terminal API write completes while an existing Rollout attempt is
	// preparing its context. It cannot cause that already-started attempt.
	now = now.Add(2 * time.Second)
	terminal := terminalRun(run)
	result := handoffContext(2)
	h.RecordResult(run, terminal, result)
	assert.False(t, h.ResultParent(ro, terminal, attemptStarted).IsValid())
	assert.False(t, h.ResultParent(ro, terminal, time.Time{}).IsValid())
	assert.True(t, h.ResultParent(ro, terminal, now).Equal(result), "publication at the attempt boundary is eligible")
	assert.True(t, h.ResultParent(ro, terminal, now.Add(time.Nanosecond)).Equal(result), "publication before the attempt is eligible")
}

func TestHandoffsRejectChangedBindings(t *testing.T) {
	tests := map[string]func(*v1alpha1.Rollout, *v1alpha1.AnalysisRun){
		"rollout UID":          func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { ro.UID = "replacement" },
		"rollout name":         func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { ro.Name = "replacement" },
		"rollout namespace":    func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { ro.Namespace = "other" },
		"superseding revision": func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { ro.Generation++ },
		"rollout pod hash":     func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { ro.Status.CurrentPodHash = "other" },
		"rollout step":         func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { *ro.Status.CurrentStepIndex = 1 },
		"missing step":         func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { ro.Status.CurrentStepIndex = nil },
		"missing reference": func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) {
			ro.Status.Canary.CurrentStepAnalysisRunStatus = nil
		},
		"other reference": func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) {
			ro.Status.Canary.CurrentStepAnalysisRunStatus.Name = "other"
		},
		"non canary":        func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { ro.Spec.Strategy.Canary = nil },
		"non analysis step": func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { ro.Spec.Strategy.Canary.Steps[0].Analysis = nil },
		"rollout deletion":  func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { ro.DeletionTimestamp = &metav1.Time{} },
		"run UID":           func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.UID = "replacement" },
		"run generation":    func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.Generation++ },
		"run name":          func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.Name = "replacement" },
		"run namespace":     func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.Namespace = "other" },
		"run pod hash": func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) {
			run.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = "other"
		},
		"run role": func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) {
			run.Labels[v1alpha1.RolloutTypeLabel] = v1alpha1.RolloutTypeBackgroundRunLabel
		},
		"run step": func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) {
			run.Labels[v1alpha1.RolloutCanaryStepIndexLabel] = "1"
		},
		"malformed step": func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) {
			run.Labels[v1alpha1.RolloutCanaryStepIndexLabel] = "invalid"
		},
		"negative step": func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) {
			run.Labels[v1alpha1.RolloutCanaryStepIndexLabel] = "-1"
		},
		"overflowing step": func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) {
			run.Labels[v1alpha1.RolloutCanaryStepIndexLabel] = "2147483648"
		},
		"noncanonical step": func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) {
			run.Labels[v1alpha1.RolloutCanaryStepIndexLabel] = "00"
		},
		"missing owner":       func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.OwnerReferences = nil },
		"owner UID":           func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.OwnerReferences[0].UID = "other" },
		"owner name":          func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.OwnerReferences[0].Name = "other" },
		"owner kind":          func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.OwnerReferences[0].Kind = "Experiment" },
		"owner API version":   func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.OwnerReferences[0].APIVersion = "other/v1" },
		"not controller":      func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { *run.OwnerReferences[0].Controller = false },
		"run deletion":        func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.DeletionTimestamp = &metav1.Time{} },
		"missing run version": func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.ResourceVersion = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			ro, run := handoffObjects()
			h := NewHandoffs()
			h.RecordCreation(ro, run, handoffContext(1))
			terminal := terminalRun(run)
			h.RecordResult(run, terminal, handoffContext(2))
			mutate(ro, terminal)
			assert.False(t, h.CreationParent(ro, terminal).IsValid())
			assert.False(t, h.ResultParent(ro, terminal, time.Now()).IsValid())
		})
	}
}

func TestHandoffsRejectInvalidCreation(t *testing.T) {
	tests := map[string]func(*v1alpha1.Rollout, *v1alpha1.AnalysisRun){
		"missing rollout UID":        func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { ro.UID = "" },
		"missing rollout generation": func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { ro.Generation = 0 },
		"missing rollout name":       func(ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { ro.Name = "" },
		"missing namespace":          func(ro *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { ro.Namespace, run.Namespace = "", "" },
		"missing run UID":            func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.UID = "" },
		"missing run name":           func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.Name = "" },
		"missing run generation":     func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.Generation = 0 },
		"missing run version":        func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.ResourceVersion = "" },
		"missing hash": func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) {
			delete(run.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)
		},
		"step out of range": func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) {
			run.Labels[v1alpha1.RolloutCanaryStepIndexLabel] = "4"
		},
		"different owner": func(_ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.OwnerReferences[0].UID = "other" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			ro, run := handoffObjects()
			mutate(ro, run)
			h := NewHandoffs()
			h.RecordCreation(ro, run, handoffContext(1))
			assert.Empty(t, h.entries)
		})
	}
}

func TestHandoffsResultRequiresExactPersistedTransition(t *testing.T) {
	tests := map[string]func(*v1alpha1.AnalysisRun, *v1alpha1.AnalysisRun){
		"already terminal":     func(orig, _ *v1alpha1.AnalysisRun) { orig.Status.Phase = v1alpha1.AnalysisPhaseSuccessful },
		"not terminal":         func(_, persisted *v1alpha1.AnalysisRun) { persisted.Status.Phase = v1alpha1.AnalysisPhaseRunning },
		"no version change":    func(orig, persisted *v1alpha1.AnalysisRun) { persisted.ResourceVersion = orig.ResourceVersion },
		"missing version":      func(_, persisted *v1alpha1.AnalysisRun) { persisted.ResourceVersion = "" },
		"replaced run":         func(_, persisted *v1alpha1.AnalysisRun) { persisted.UID = "replacement" },
		"changed generation":   func(_, persisted *v1alpha1.AnalysisRun) { persisted.Generation++ },
		"changed owner":        func(_, persisted *v1alpha1.AnalysisRun) { persisted.OwnerReferences[0].UID = "other" },
		"stale original owner": func(orig, _ *v1alpha1.AnalysisRun) { orig.OwnerReferences[0].UID = "other" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			ro, run := handoffObjects()
			h := NewHandoffs()
			h.RecordCreation(ro, run, handoffContext(1))
			terminal := terminalRun(run)
			mutate(run, terminal)
			h.RecordResult(run, terminal, handoffContext(2))
			assert.False(t, h.ResultParent(ro, terminal, time.Now()).IsValid())
		})
	}
}

func TestHandoffsResultsDoNotOverwriteOrAcceptLaterVersions(t *testing.T) {
	ro, run := handoffObjects()
	h := NewHandoffs()
	creator, result := handoffContext(1), handoffContext(2)
	h.RecordCreation(ro, run, creator)
	h.RecordCreation(ro, run, handoffContext(3))
	assert.True(t, h.CreationParent(ro, run).Equal(creator))
	terminal := terminalRun(run)
	h.RecordResult(run, terminal, result)
	h.RecordResult(run, terminal, handoffContext(3))
	assert.True(t, h.ResultParent(ro, terminal, time.Now()).Equal(result))
	terminal.ResourceVersion = "later-metadata-write"
	assert.False(t, h.ResultParent(ro, terminal, time.Now()).IsValid())
	terminal.ResourceVersion = "terminal-write"
	terminal.Status.Phase = v1alpha1.AnalysisPhaseFailed
	assert.False(t, h.ResultParent(ro, terminal, time.Now()).IsValid())
	terminal.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
	ro.Status.Canary.CurrentStepAnalysisRunStatus.Status = terminal.Status.Phase
	assert.False(t, h.ResultParent(ro, terminal, time.Now()).IsValid(), "persisted Rollout status already records this phase")
}

func TestHandoffsDoNotTrustAnnotationsOrUnknownContexts(t *testing.T) {
	ro, run := handoffObjects()
	carrier := map[string]string{
		"observability.demo/rollout-traceparent":         "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"observability.demo/analysis-result-traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}
	ro.Annotations, run.Annotations = carrier, carrier
	h := NewHandoffs()
	terminal := terminalRun(run)
	h.RecordResult(run, terminal, handoffContext(2))
	assert.False(t, h.CreationParent(ro, run).IsValid())
	assert.False(t, h.ResultParent(ro, terminal, time.Now()).IsValid())
	for _, invalid := range []oteltrace.SpanContext{
		{},
		oteltrace.NewSpanContext(oteltrace.SpanContextConfig{TraceID: oteltrace.TraceID{1}}),
		oteltrace.NewSpanContext(oteltrace.SpanContextConfig{SpanID: oteltrace.SpanID{1}}),
	} {
		h.RecordCreation(ro, run, invalid)
		assert.False(t, h.CreationParent(ro, run).IsValid())
	}
	h.RecordCreation(ro, run, handoffContext(1))
	h.RecordResult(run, terminal, oteltrace.SpanContext{})
	assert.False(t, h.ResultParent(ro, terminal, time.Now()).IsValid())
	assert.False(t, NewHandoffs().CreationParent(ro, run).IsValid(), "a restarted manager cannot reconstruct parentage from annotations")
	assert.False(t, NewHandoffs().ResultParent(ro, terminal, time.Now()).IsValid())
}

func TestHandoffsExpiryAndCapacity(t *testing.T) {
	now := time.Unix(1_000, 0)
	h := newHandoffs(2, time.Minute, func() time.Time { return now })
	ro, first := handoffObjects()
	second, third := first.DeepCopy(), first.DeepCopy()
	second.UID, third.UID = "second", "third"
	h.RecordCreation(ro, first, handoffContext(1))
	now = now.Add(20 * time.Second)
	h.RecordCreation(ro, second, handoffContext(2))
	// Reading an entry does not refresh its insertion order or expiry.
	require.True(t, h.CreationParent(ro, first).IsValid())
	h.RecordCreation(ro, third, handoffContext(3))
	assert.False(t, h.CreationParent(ro, first).IsValid())
	assert.True(t, h.CreationParent(ro, second).IsValid())
	assert.True(t, h.CreationParent(ro, third).IsValid())
	require.Len(t, h.entries, 2)
	now = now.Add(time.Minute)
	assert.False(t, h.CreationParent(ro, second).IsValid())
	assert.False(t, h.CreationParent(ro, third).IsValid())
	assert.Empty(t, h.entries)
	assert.Zero(t, h.order.Len())
}

func TestHandoffsNilAndZeroValues(t *testing.T) {
	ro, run := handoffObjects()
	for _, h := range []*Handoffs{nil, NewHandoffs(), {}} {
		h.RecordCreation(nil, run, handoffContext(1))
		h.RecordCreation(ro, nil, handoffContext(1))
		h.RecordResult(nil, run, handoffContext(2))
		h.RecordResult(run, nil, handoffContext(2))
		h.ConsumeResult(nil, handoffContext(2))
		assert.False(t, h.CreationParent(nil, run).IsValid())
		assert.False(t, h.CreationParent(ro, nil).IsValid())
		assert.False(t, h.ResultParent(nil, run, time.Now()).IsValid())
		assert.False(t, h.ResultParent(ro, nil, time.Now()).IsValid())
	}
	var zero Handoffs
	zero.RecordCreation(ro, run, handoffContext(1))
	assert.True(t, zero.CreationParent(ro, run).IsValid())
}

func TestHandoffsConcurrentAccess(t *testing.T) {
	h := newHandoffs(8, time.Minute, time.Now)
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			ro, run := handoffObjects()
			run.UID = types.UID(fmt.Sprintf("run-%d", i))
			terminal := terminalRun(run)
			for range 20 {
				h.RecordCreation(ro, run, handoffContext(1))
				h.CreationParent(ro, run)
				h.RecordResult(run, terminal, handoffContext(2))
				parent := h.ResultParent(ro, terminal, time.Now())
				h.ConsumeResult(terminal, parent)
			}
		})
	}
	wg.Wait()
	assert.LessOrEqual(t, len(h.entries), 8)
	assert.Equal(t, len(h.entries), h.order.Len())
}

func handoffObjects() (*v1alpha1.Rollout, *v1alpha1.AnalysisRun) {
	step := int32(0)
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "rollout", Namespace: "default", UID: "rollout-uid", Generation: 1},
		Spec: v1alpha1.RolloutSpec{Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{
			Steps: []v1alpha1.CanaryStep{{Analysis: &v1alpha1.RolloutAnalysis{}}},
		}}},
		Status: v1alpha1.RolloutStatus{
			CurrentPodHash: "template", CurrentStepIndex: &step,
			Canary: v1alpha1.CanaryStatus{CurrentStepAnalysisRunStatus: &v1alpha1.RolloutAnalysisRunStatus{
				Name: "analysis", Status: v1alpha1.AnalysisPhaseRunning,
			}},
		},
	}
	run := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: "analysis", Namespace: "default", UID: "analysis-uid", Generation: 1, ResourceVersion: "created",
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ro, v1alpha1.SchemeGroupVersion.WithKind("Rollout"))},
			Labels: map[string]string{
				v1alpha1.RolloutTypeLabel:             v1alpha1.RolloutTypeStepLabel,
				v1alpha1.DefaultRolloutUniqueLabelKey: "template",
				v1alpha1.RolloutCanaryStepIndexLabel:  "0",
			},
		},
		Status: v1alpha1.AnalysisRunStatus{Phase: v1alpha1.AnalysisPhaseRunning},
	}
	return ro, run
}

func terminalRun(run *v1alpha1.AnalysisRun) *v1alpha1.AnalysisRun {
	terminal := run.DeepCopy()
	terminal.ResourceVersion = "terminal-write"
	terminal.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
	return terminal
}

func handoffContext(id byte) oteltrace.SpanContext {
	return oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID: oteltrace.TraceID{1}, SpanID: oteltrace.SpanID{id}, TraceFlags: oteltrace.FlagsSampled,
	})
}
