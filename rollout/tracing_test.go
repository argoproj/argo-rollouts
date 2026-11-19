package rollout

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"
	testclient "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	rolloutlisters "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	traceutil "github.com/argoproj/argo-rollouts/utils/trace"
)

func rolloutTestTracer(t *testing.T) (oteltrace.Tracer, *tracetest.SpanRecorder) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	return provider.Tracer("rollout-test"), recorder
}

func newRolloutTraceGate(t *testing.T, phase v1alpha1.AnalysisPhase) (*rolloutContext, oteltrace.Tracer, *tracetest.SpanRecorder, oteltrace.SpanContext) {
	t.Helper()
	tracer, recorder := rolloutTestTracer(t)
	ro := newRollout("checkout", 1, nil, nil)
	ro.ResourceVersion = "10"
	ro.Status.CurrentPodHash = "abc123"
	ro.Status.CurrentStepIndex = ptr.To[int32](0)
	ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{}
	ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{Analysis: &v1alpha1.RolloutAnalysis{Templates: []v1alpha1.AnalysisTemplateRef{{TemplateName: "gate"}}}}}
	ro.Status.CurrentStepHash = conditions.ComputeStepHash(ro)
	ro.Status.ObservedGeneration = "123"
	ro.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{Name: "checkout-gate", Status: v1alpha1.AnalysisPhaseRunning}
	run := &v1alpha1.AnalysisRun{ObjectMeta: metav1.ObjectMeta{
		Name: "checkout-gate", Namespace: ro.Namespace, UID: types.UID("gate-uid"), Generation: 1, ResourceVersion: "11",
		Labels:          analysisutil.StepLabels(0, "abc123", ""),
		Annotations:     map[string]string{annotations.RevisionAnnotation: "1"},
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ro, v1alpha1.SchemeGroupVersion.WithKind("Rollout"))},
	}, Status: v1alpha1.AnalysisRunStatus{Phase: v1alpha1.AnalysisPhaseRunning}}
	handoffs := traceutil.NewHandoffs()
	_, creator := tracer.Start(context.Background(), "creating-rollout")
	handoffs.RecordCreation(ro, run, creator.SpanContext())
	creator.End()
	_, result := tracer.Start(context.Background(), "analysis-result")
	terminal := run.DeepCopy()
	terminal.ResourceVersion = "12"
	terminal.Status.Phase = phase
	handoffs.RecordResult(run, terminal, result.SpanContext())
	parent := result.SpanContext()
	result.End()
	ctx := &rolloutContext{
		rollout: ro, log: logutil.WithRollout(ro),
		traceAttempt:   newRolloutTraceAttempt(context.Background(), tracer, time.Now()),
		currentArs:     analysisutil.CurrentAnalysisRuns{CanaryStep: terminal},
		pauseContext:   &pauseContext{rollout: ro},
		reconcilerBase: reconcilerBase{traceHandoffs: handoffs, recorder: record.NewFakeEventRecorder()},
	}
	return ctx, tracer, recorder, parent
}

func finishRolloutTrace(c *rolloutContext, err error, changed bool) {
	c.traceAttempt.Finish(err, nil, changed, rolloutTraceAttributes(c.rollout)...)
}

func rolloutRecordedAttempt(t *testing.T, recorder *tracetest.SpanRecorder) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range recorder.Ended() {
		if span.Name() == "controller.Rollout" {
			return span
		}
	}
	t.Fatal("controller.Rollout span missing")
	return nil
}

func TestRolloutTraceParentsOnlyNativeGateConsumption(t *testing.T) {
	for _, phase := range []v1alpha1.AnalysisPhase{v1alpha1.AnalysisPhaseSuccessful, v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseInconclusive} {
		t.Run(string(phase), func(t *testing.T) {
			c, _, recorder, parent := newRolloutTraceGate(t, phase)
			if phase == v1alpha1.AnalysisPhaseSuccessful {
				require.True(t, c.completedCurrentCanaryStep())
			} else {
				_, err := c.reconcileStepBasedAnalysisRun()
				require.NoError(t, err)
			}
			finishRolloutTrace(c, nil, true)
			span := rolloutRecordedAttempt(t, recorder)
			assert.Equal(t, parent, span.Parent())
			assert.Equal(t, codes.Unset, span.Status().Code)
			require.NotNil(t, c.pendingTraceResult)
		})
	}
}

func TestRolloutTraceAmbiguousOrUnprocessedResultsStayRoots(t *testing.T) {
	for _, scenario := range []string{"paused", "aborted", "ambiguous", "superseded", "already recorded", "forged annotations", "early error"} {
		t.Run(scenario, func(t *testing.T) {
			c, _, recorder, parent := newRolloutTraceGate(t, v1alpha1.AnalysisPhaseFailed)
			switch scenario {
			case "paused":
				c.rollout.Status.PauseConditions = []v1alpha1.PauseCondition{{Reason: v1alpha1.PauseReasonCanaryPauseStep}}
			case "aborted":
				c.rollout.Status.Abort = true
			case "ambiguous":
				other := c.currentArs.CanaryStep.DeepCopy()
				other.Name, other.UID = "background", "background-uid"
				c.currentArs.CanaryBackground = other
				c.rollout.Status.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{Name: other.Name, Status: v1alpha1.AnalysisPhaseRunning}
			case "superseded":
				c.rollout.Generation++
			case "already recorded":
				c.rollout.Status.Canary.CurrentStepAnalysisRunStatus.Status = v1alpha1.AnalysisPhaseFailed
			case "forged annotations":
				c.traceHandoffs = traceutil.NewHandoffs()
				c.currentArs.CanaryStep.Annotations["observability.demo/analysis-result-traceparent"] = "00-" + parent.TraceID().String() + "-" + parent.SpanID().String() + "-01"
			}
			if scenario != "early error" {
				_, err := c.reconcileStepBasedAnalysisRun()
				require.NoError(t, err)
			}
			finishRolloutTrace(c, errors.New("private execution failure"), false)
			span := rolloutRecordedAttempt(t, recorder)
			assert.False(t, span.Parent().IsValid())
			assert.Equal(t, codes.Error, span.Status().Code)
			assert.Empty(t, span.Events())
			assert.NotContains(t, span.Status().Description, "private")
		})
	}
}

func TestRolloutTraceAcknowledgesOnlyConfirmedConsumption(t *testing.T) {
	c, _, _, parent := newRolloutTraceGate(t, v1alpha1.AnalysisPhaseSuccessful)
	require.True(t, c.completedCurrentCanaryStep())
	run := c.currentArs.CanaryStep
	unrelated := c.rollout.DeepCopy()
	unrelated.Status.AvailableReplicas++
	c.tracePersistedStatus(c.rollout, unrelated)
	assert.Equal(t, parent, c.traceHandoffs.ResultParent(c.rollout, run, c.traceAttempt.StartTime()))
	advanced := unrelated.DeepCopy()
	advanced.Status.CurrentStepIndex = ptr.To[int32](1)
	advanced.Status.Canary.CurrentStepAnalysisRunStatus = nil
	c.tracePersistedStatus(c.rollout, advanced)
	assert.False(t, c.traceHandoffs.ResultParent(c.rollout, run, c.traceAttempt.StartTime()).IsValid())
}

func TestRolloutTraceFailedPatchRetainsResultForRetry(t *testing.T) {
	c, tracer, recorder, parent := newRolloutTraceGate(t, v1alpha1.AnalysisPhaseFailed)
	client := fake.NewSimpleClientset(c.rollout.DeepCopy())
	patchFails := true
	client.PrependReactor("patch", "rollouts", func(testclient.Action) (bool, runtime.Object, error) {
		if patchFails {
			return true, nil, errors.New("sensitive patch response")
		}
		return false, nil, nil
	})
	c.argoprojclientset = client
	c.metricsServer = newFakeMetricsRecorder(t)
	_, err := c.reconcileStepBasedAnalysisRun()
	require.NoError(t, err)
	newStatus := c.rollout.Status.DeepCopy()
	newStatus.Canary.CurrentStepAnalysisRunStatus.Status = v1alpha1.AnalysisPhaseFailed
	err = c.persistRolloutStatus(newStatus)
	require.Error(t, err)
	finishRolloutTrace(c, err, false)
	assert.Equal(t, parent, c.traceHandoffs.ResultParent(c.rollout, c.currentArs.CanaryStep, c.traceAttempt.StartTime()))
	assert.Equal(t, codes.Error, rolloutRecordedAttempt(t, recorder).Status().Code)

	patchFails = false
	c.traceAttempt = newRolloutTraceAttempt(context.Background(), tracer, time.Now())
	c.pendingTraceResult = nil
	_, err = c.reconcileStepBasedAnalysisRun()
	require.NoError(t, err)
	err = c.persistRolloutStatus(newStatus)
	require.NoError(t, err)
	finishRolloutTrace(c, nil, true)
	assert.False(t, c.traceHandoffs.ResultParent(c.rollout, c.currentArs.CanaryStep, c.traceAttempt.StartTime()).IsValid())
	attempts := []sdktrace.ReadOnlySpan{}
	for _, span := range recorder.Ended() {
		if span.Name() == "controller.Rollout" {
			attempts = append(attempts, span)
		}
	}
	require.Len(t, attempts, 2)
	assert.Equal(t, parent, attempts[1].Parent())
	assert.Equal(t, codes.Unset, attempts[1].Status().Code)
}

func TestRolloutTraceStaleAndDeletedAttemptsAreSuppressed(t *testing.T) {
	for _, scenario := range []string{"stale", "deleting", "not found"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			defer f.Close()
			ro := newRollout("checkout", 1, nil, nil)
			ro.ResourceVersion = "10"
			if scenario == "deleting" {
				now := metav1.Now()
				ro.DeletionTimestamp = &now
			}
			if scenario != "not found" {
				f.rolloutLister = append(f.rolloutLister, ro)
				f.objects = append(f.objects, ro)
			}
			controller, _, _ := f.newController(noResyncPeriodFunc)
			tracer, recorder := rolloutTestTracer(t)
			controller.tracer = tracer
			if scenario == "stale" {
				controller.rolloutVersionTracker.Record(getKey(ro, t), "20")
			}
			err := controller.syncHandler(context.Background(), getKey(ro, t))
			if scenario == "stale" {
				require.ErrorIs(t, err, controllerutil.StaleCacheError)
			} else {
				require.NoError(t, err)
			}
			assert.Empty(t, recorder.Ended())
		})
	}
}

func TestRolloutTraceFixedErrorAndPanic(t *testing.T) {
	for _, panicExpected := range []bool{false, true} {
		tracer, recorder := rolloutTestTracer(t)
		controller := &Controller{tracer: tracer}
		_, worker := tracer.Start(context.Background(), "worker")
		if panicExpected {
			assert.Panics(t, func() { _ = controller.syncHandler(context.Background(), "default/checkout") })
		} else {
			require.Error(t, controller.syncHandler(context.Background(), "invalid/key/value"))
		}
		worker.End()
		span := rolloutRecordedAttempt(t, recorder)
		assert.Equal(t, codes.Error, span.Status().Code)
		assert.False(t, span.Parent().IsValid())
		assert.Empty(t, span.Events())
		attrs := rolloutTraceAttributeMap(span.Attributes())
		expectedCause := "invalid_key"
		if panicExpected {
			expectedCause = "resource_lookup_error"
		}
		assert.Equal(t, attribute.StringValue(expectedCause), attrs["argo.rollouts.reconciliation.cause"])
	}
}

func TestRolloutTraceAttributesKeepPrivateMetadataOut(t *testing.T) {
	ro := newRollout("checkout", 1, nil, nil)
	ro.Annotations["private"] = "private-annotation"
	ro.Annotations[annotations.RevisionAnnotation] = "private-revision"
	ro.Labels = map[string]string{"private": "private-label"}
	ro.Status.Message = "private-status-message"
	ro.Status.Phase = "private-phase"
	ro.Status.CurrentPodHash = "private invalid hash"
	ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{}
	ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{Plugin: &v1alpha1.PluginStep{Name: "private-plugin", Config: nil}}}
	ro.Status.CurrentStepIndex = ptr.To[int32](0)
	attrs := rolloutTraceAttributes(ro)
	for _, attr := range attrs {
		assert.NotContains(t, attr.Value.AsString(), "private")
	}
	values := map[attribute.Key]attribute.Value{}
	for _, attr := range attrs {
		values[attr.Key] = attr.Value
	}
	assert.Equal(t, attribute.StringValue("plugin"), values["argo.rollouts.rollout.current_step.type"])
	assert.Equal(t, attribute.StringValue("Unknown"), values["argo.rollouts.rollout.phase"])
}

func TestRolloutTraceCreationDoesNotRebindAdoptedRun(t *testing.T) {
	c, tracer, recorder, _ := newRolloutTraceGate(t, v1alpha1.AnalysisPhaseSuccessful)
	c.traceHandoffs = traceutil.NewHandoffs()
	c.newRS = &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "checkout-rs", Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "abc123"}}}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	require.NoError(t, indexer.Add(analysisTemplate("gate")))
	c.analysisTemplateLister = rolloutlisters.NewAnalysisTemplateLister(indexer)
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "analysisruns", func(action testclient.Action) (bool, runtime.Object, error) {
		run := action.(testclient.CreateAction).GetObject().(*v1alpha1.AnalysisRun).DeepCopy()
		if _, err := client.Tracker().Get(v1alpha1.SchemeGroupVersion.WithResource("analysisruns"), run.Namespace, run.Name); err == nil {
			return false, nil, nil
		}
		run.UID, run.ResourceVersion, run.Generation = "created-uid", "20", 1
		require.NoError(t, client.Tracker().Add(run))
		return true, run, nil
	})
	c.argoprojclientset = client
	analysis := c.rollout.Spec.Strategy.Canary.Steps[0].Analysis
	created, err := c.createAnalysisRun(analysis, "0", analysisutil.StepLabels(0, "abc123", ""))
	require.NoError(t, err)
	c.rollout.Status.Canary.CurrentStepAnalysisRunStatus.Name = created.Name
	creator := c.traceAttempt.SpanContext()
	require.True(t, creator.IsValid())
	assert.Equal(t, creator, c.traceHandoffs.CreationParent(c.rollout, created))
	finishRolloutTrace(c, nil, false)

	c.traceAttempt = newRolloutTraceAttempt(context.Background(), tracer, time.Now())
	adopted, err := c.createAnalysisRun(analysis, "0", analysisutil.StepLabels(0, "abc123", ""))
	require.NoError(t, err)
	assert.Equal(t, created.UID, adopted.UID)
	assert.False(t, c.traceAttempt.SpanContext().IsValid())
	assert.Equal(t, creator, c.traceHandoffs.CreationParent(c.rollout, adopted))
	finishRolloutTrace(c, nil, false)
	count := 0
	for _, span := range recorder.Ended() {
		if span.Name() == "controller.Rollout" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

// Capturing sampler parameters verifies the attributes visible before a native
// operation starts the span, separately from the attributes added at Finish.
type rolloutTraceSamplingRecorder struct {
	attributes []attribute.KeyValue
}

func (s *rolloutTraceSamplingRecorder) ShouldSample(parameters sdktrace.SamplingParameters) sdktrace.SamplingResult {
	s.attributes = append([]attribute.KeyValue(nil), parameters.Attributes...)
	return sdktrace.SamplingResult{Decision: sdktrace.RecordAndSample}
}

func (*rolloutTraceSamplingRecorder) Description() string { return "rollout tracing test sampler" }

func rolloutTraceAttributeMap(attrs []attribute.KeyValue) map[string]attribute.Value {
	values := make(map[string]attribute.Value, len(attrs))
	for _, attr := range attrs {
		values[string(attr.Key)] = attr.Value
	}
	return values
}

func TestRolloutTraceIdentityIsAvailableToSampler(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	ro := newRollout("checkout", 1, nil, map[string]string{"app": "checkout"})
	ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{}
	ro.Annotations["private"] = "private-annotation"
	ro.Spec.WorkloadRef = &v1alpha1.ObjectRef{APIVersion: "apps/v1", Kind: "Deployment", Name: "checkout-source"}
	f.rolloutLister = append(f.rolloutLister, ro)
	f.workloadRefResolveFn = func(*v1alpha1.Rollout) error { panic("private preparation failure") }
	controller, _, _ := f.newController(noResyncPeriodFunc)
	sampler := &rolloutTraceSamplingRecorder{}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sampler))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	controller.tracer = provider.Tracer("rollout-sampler-test")
	assert.Panics(t, func() { _ = controller.syncHandler(context.Background(), getKey(ro, t)) })
	attrs := rolloutTraceAttributeMap(sampler.attributes)
	assert.Equal(t, attribute.StringValue(ro.Namespace), attrs["k8s.namespace.name"])
	assert.Equal(t, attribute.StringValue(ro.Name), attrs["argo.rollouts.rollout.name"])
	assert.Equal(t, attribute.StringValue(string(ro.UID)), attrs["argo.rollouts.rollout.uid"])
	assert.Equal(t, attribute.Int64Value(ro.Generation), attrs["argo.rollouts.rollout.generation"])
	assert.Equal(t, attribute.StringValue("canary"), attrs["argo.rollouts.rollout.strategy"])
	assert.Equal(t, attribute.StringValue("spec_change"), attrs["argo.rollouts.reconciliation.cause"])
	for _, attr := range sampler.attributes {
		assert.NotContains(t, attr.Value.AsString(), "private")
	}
}

func TestRolloutTraceLateResultDuringPreparationStaysRoot(t *testing.T) {
	c, tracer, recorder, _ := newRolloutTraceGate(t, v1alpha1.AnalysisPhaseSuccessful)
	c.traceHandoffs = traceutil.NewHandoffs()
	orig := c.currentArs.CanaryStep.DeepCopy()
	orig.ResourceVersion = "11"
	orig.Status.Phase = v1alpha1.AnalysisPhaseRunning
	_, creator := tracer.Start(context.Background(), "creating-rollout-late-test")
	c.traceHandoffs.RecordCreation(c.rollout, orig, creator.SpanContext())
	creator.End()
	c.traceAttempt = newRolloutTraceAttempt(context.Background(), tracer, time.Now())
	_, result := tracer.Start(context.Background(), "late-analysis-result")
	c.traceHandoffs.RecordResult(orig, c.currentArs.CanaryStep, result.SpanContext())
	result.End()
	require.True(t, c.completedCurrentCanaryStep())
	finishRolloutTrace(c, nil, true)
	span := rolloutRecordedAttempt(t, recorder)
	assert.False(t, span.Parent().IsValid())
	assert.Nil(t, c.pendingTraceResult)
	assert.True(t, c.traceHandoffs.ResultParent(c.rollout, c.currentArs.CanaryStep, time.Now()).IsValid(), "late publication remains available to a later attempt")
}

func TestRolloutTraceRejectsReplacementResponseSnapshot(t *testing.T) {
	for _, field := range []string{"uid", "name", "namespace", "generation"} {
		t.Run(field, func(t *testing.T) {
			c, _, recorder, parent := newRolloutTraceGate(t, v1alpha1.AnalysisPhaseFailed)
			_, err := c.reconcileStepBasedAnalysisRun()
			require.NoError(t, err)
			response := c.rollout.DeepCopy()
			switch field {
			case "uid":
				response.UID = "replacement-uid"
			case "name":
				response.Name = "replacement-name"
			case "namespace":
				response.Namespace = "replacement-namespace"
			case "generation":
				response.Generation++
			}
			response.Status.Abort = true
			response.Status.Phase = v1alpha1.RolloutPhaseDegraded
			response.Status.Canary.CurrentStepAnalysisRunStatus.Status = v1alpha1.AnalysisPhaseFailed
			c.tracePersistedStatus(c.rollout, response)
			traced := rolloutTraceSnapshot(c.rollout, response)
			require.Same(t, c.rollout, traced)
			c.traceAttempt.Finish(nil, nil, false, rolloutTraceAttributes(traced)...)
			span := rolloutRecordedAttempt(t, recorder)
			attrs := rolloutTraceAttributeMap(span.Attributes())
			assert.Equal(t, parent, span.Parent())
			assert.Equal(t, attribute.StringValue(string(c.rollout.UID)), attrs["argo.rollouts.rollout.uid"])
			assert.Equal(t, attribute.StringValue(c.rollout.Name), attrs["argo.rollouts.rollout.name"])
			assert.Equal(t, attribute.StringValue(c.rollout.Namespace), attrs["k8s.namespace.name"])
			assert.Equal(t, attribute.Int64Value(c.rollout.Generation), attrs["argo.rollouts.rollout.generation"])
			assert.Equal(t, attribute.BoolValue(false), attrs["argo.rollouts.reconciliation.status_changed"])
			assert.Empty(t, span.Events())
			assert.True(t, c.traceHandoffs.ResultParent(c.rollout, c.currentArs.CanaryStep, time.Now()).IsValid())
		})
	}
}

func TestRolloutTraceKeepsStatusWrittenDuringValidation(t *testing.T) {
	for _, tracingEnabled := range []bool{false, true} {
		for _, patchFails := range []bool{false, true} {
			name := fmt.Sprintf("tracing=%t/patchFails=%t", tracingEnabled, patchFails)
			t.Run(name, func(t *testing.T) {
				f := newFixture(t)
				defer f.Close()
				ro := newRollout("checkout", 1, nil, map[string]string{"app": "checkout"})
				ro.ResourceVersion = "10"
				ro.Status.Phase = v1alpha1.RolloutPhaseHealthy
				ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{}
				ro.Spec.Selector = nil
				original := ro.DeepCopy()
				f.rolloutLister = append(f.rolloutLister, ro)
				f.objects = append(f.objects, ro)
				controller, _, _ := f.newController(noResyncPeriodFunc)
				tracer, recorder := rolloutTestTracer(t)
				require.Nil(t, controller.tracer, "tracing is disabled by default")
				if tracingEnabled {
					controller.tracer = tracer
				} else {
					require.Nil(t, newRolloutTraceAttempt(context.Background(), controller.tracer, time.Now()))
				}
				delayed := false
				controller.enqueueRolloutAfter = func(any, time.Duration) { delayed = true }
				writeErr := errors.New("status write failed")
				if patchFails {
					f.client.PrependReactor("patch", "rollouts", func(testclient.Action) (bool, runtime.Object, error) {
						return true, nil, writeErr
					})
				}
				err := controller.syncHandler(context.Background(), getKey(ro, t))
				if patchFails {
					require.ErrorIs(t, err, writeErr)
				} else {
					var validationErr *field.Error
					require.ErrorAs(t, err, &validationErr)
				}
				assert.False(t, delayed, "context setup still returns its native error")
				assert.Equal(t, original, ro, "the informer object remains an immutable baseline")
				accepted, err := f.client.ArgoprojV1alpha1().Rollouts(ro.Namespace).Get(context.Background(), ro.Name, metav1.GetOptions{})
				require.NoError(t, err)
				expectedPhase := v1alpha1.RolloutPhaseHealthy
				if !patchFails {
					expectedPhase = v1alpha1.RolloutPhaseDegraded
					require.NotNil(t, conditions.GetRolloutCondition(accepted.Status, v1alpha1.InvalidSpec))
				}
				assert.Equal(t, expectedPhase, accepted.Status.Phase)
				if !tracingEnabled {
					assert.Empty(t, recorder.Ended())
					return
				}
				span := rolloutRecordedAttempt(t, recorder)
				attrs := rolloutTraceAttributeMap(span.Attributes())
				assert.Equal(t, attribute.StringValue(string(expectedPhase)), attrs["argo.rollouts.rollout.phase"])
				assert.Equal(t, attribute.BoolValue(!patchFails), attrs["argo.rollouts.reconciliation.status_changed"])
				assert.Equal(t, codes.Error, span.Status().Code, "the native validation error remains an execution error")
				assert.False(t, span.Parent().IsValid())
			})
		}
	}
}
