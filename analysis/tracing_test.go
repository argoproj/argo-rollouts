package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutlisters "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	traceutil "github.com/argoproj/argo-rollouts/utils/trace"
)

func TestAnalysisRunAttemptRecordsPersistedOutcomeWithoutPrivateData(t *testing.T) {
	f, controller, run, recorder := newAnalysisRunTracingFixture(t)
	workerCtx, worker := controller.tracer.Start(context.Background(), "worker")
	require.NoError(t, controller.syncHandler(workerCtx, getKey(run, t)))
	worker.End()

	span := analysisRunAttemptSpan(t, recorder.Ended())
	assert.False(t, span.Parent().IsValid())
	assert.Empty(t, span.Links())
	assert.Equal(t, codes.Unset, span.Status().Code, "a failed analysis is a domain result")
	assert.Equal(t, map[string]attribute.Value{
		"k8s.namespace.name":                                           attribute.StringValue(metav1.NamespaceDefault),
		"argo.rollouts.analysis_run.name":                              attribute.StringValue(run.Name),
		"argo.rollouts.analysis_run.uid":                               attribute.StringValue(string(run.UID)),
		"argo.rollouts.analysis_run.generation":                        attribute.Int64Value(7),
		"argo.rollouts.analysis_run.phase":                             attribute.StringValue("Failed"),
		"argo.rollouts.analysis_run.metrics.count":                     attribute.IntValue(1),
		"argo.rollouts.analysis_run.metric_results.count":              attribute.IntValue(1),
		"argo.rollouts.analysis_run.metric_results.pending.count":      attribute.IntValue(0),
		"argo.rollouts.analysis_run.metric_results.running.count":      attribute.IntValue(0),
		"argo.rollouts.analysis_run.metric_results.successful.count":   attribute.IntValue(0),
		"argo.rollouts.analysis_run.metric_results.failed.count":       attribute.IntValue(1),
		"argo.rollouts.analysis_run.metric_results.error.count":        attribute.IntValue(0),
		"argo.rollouts.analysis_run.metric_results.inconclusive.count": attribute.IntValue(0),
		"argo.rollouts.reconciliation.cause":                           attribute.StringValue("resource_event"),
		"argo.rollouts.reconciliation.outcome":                         attribute.StringValue("state_changed"),
		"argo.rollouts.reconciliation.status_changed":                  attribute.BoolValue(true),
	}, analysisRunAttemptAttributes(span))
	require.Len(t, span.Events(), 1)
	assert.Equal(t, "analysis_run.phase_changed", span.Events()[0].Name)
	patched := f.getPatchedAnalysisRun(0)
	assert.Empty(t, patched.Annotations, "native tracing must not write annotation carriers")
}

func TestAnalysisRunAttemptRecordsWriteFailureFromOriginalState(t *testing.T) {
	f, controller, run, recorder := newAnalysisRunTracingFixture(t)
	f.client.PrependReactor("patch", "analysisruns", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("private API failure")
	})
	require.Error(t, controller.syncHandler(context.Background(), getKey(run, t)))
	span := analysisRunAttemptSpan(t, recorder.Ended())
	assert.Equal(t, codes.Error, span.Status().Code)
	assert.Equal(t, "reconciliation error", span.Status().Description)
	attrs := analysisRunAttemptAttributes(span)
	assert.Equal(t, attribute.StringValue("Unknown"), attrs["argo.rollouts.analysis_run.phase"])
	assert.Equal(t, attribute.StringValue("error"), attrs["argo.rollouts.reconciliation.outcome"])
	assert.Equal(t, attribute.BoolValue(false), attrs["argo.rollouts.reconciliation.status_changed"])
	assert.Empty(t, span.Events())
}

func TestAnalysisRunAttemptBoundsStoredPhaseValues(t *testing.T) {
	tracer, recorder := newAnalysisRunAttemptTracer(t)
	before := &v1alpha1.AnalysisRun{Status: v1alpha1.AnalysisRunStatus{Phase: "private previous phase"}}
	after := before.DeepCopy()
	after.Status.Phase = "private current phase"
	attempt := traceutil.NewAttempt(context.Background(), tracer, "controller.AnalysisRun", time.Now(), analysisRunTraceAttributes(before)...)
	finishAnalysisRunAttempt(attempt, before, after, nil, nil)

	span := analysisRunAttemptSpan(t, recorder.Ended())
	assert.Equal(t, attribute.StringValue("Unknown"), analysisRunAttemptAttributes(span)["argo.rollouts.analysis_run.phase"])
	require.Len(t, span.Events(), 1)
	assert.Equal(t, []attribute.KeyValue{
		attribute.String("argo.rollouts.analysis_run.previous_phase", "Unknown"),
		attribute.String("argo.rollouts.analysis_run.phase", "Unknown"),
	}, span.Events()[0].Attributes)
}

func TestAnalysisRunAttemptRecordsInvalidKeyAndPanic(t *testing.T) {
	for _, test := range []struct {
		name, key, cause, outcome, description string
		panics                                 bool
	}{
		{name: "invalid key", key: "invalid/key/value", cause: "invalid_key", outcome: "error", description: "reconciliation error"},
		{name: "panic", key: "default/analysis", cause: "resource_lookup_error", outcome: "panic", description: "reconciliation panic", panics: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tracer, recorder := newAnalysisRunAttemptTracer(t)
			controller := &Controller{tracer: tracer}
			if test.panics {
				assert.Panics(t, func() { _ = controller.syncHandler(context.Background(), test.key) })
			} else {
				require.Error(t, controller.syncHandler(context.Background(), test.key))
			}
			span := analysisRunAttemptSpan(t, recorder.Ended())
			assert.Equal(t, codes.Error, span.Status().Code)
			assert.Equal(t, test.description, span.Status().Description)
			attrs := analysisRunAttemptAttributes(span)
			assert.Equal(t, attribute.StringValue(test.cause), attrs["argo.rollouts.reconciliation.cause"])
			assert.Equal(t, attribute.StringValue(test.outcome), attrs["argo.rollouts.reconciliation.outcome"])
			assert.Equal(t, attribute.BoolValue(false), attrs["argo.rollouts.reconciliation.status_changed"])
			assert.Empty(t, span.Events())
		})
	}
}

func TestAnalysisRunAttemptSuppressesUnchangedTerminalRun(t *testing.T) {
	_, controller, run, recorder := newAnalysisRunTracingFixture(t)
	rollout := configureAnalysisRunHandoff(controller, run)
	traceID, err := oteltrace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	require.NoError(t, err)
	spanID, err := oteltrace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)
	controller.traceHandoffs.RecordCreation(rollout, run, oteltrace.NewSpanContext(oteltrace.SpanContextConfig{TraceID: traceID, SpanID: spanID}))
	run.Status.Phase = v1alpha1.AnalysisPhaseFailed
	require.NoError(t, controller.syncHandler(context.Background(), getKey(run, t)))
	assert.Empty(t, recorder.Ended())
	assert.False(t, controller.traceHandoffs.ResultParent(rollout, run, time.Now()).IsValid())
}

func TestAnalysisRunAttemptsUseOnlyTrustedCreatingParent(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*Controller, *v1alpha1.Rollout, *v1alpha1.AnalysisRun)
		parented  bool
	}{
		{name: "trusted creator", parented: true},
		{name: "restart", configure: func(c *Controller, _ *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) {
			c.traceHandoffs = traceutil.NewHandoffs()
		}},
		{name: "analysis only", configure: func(c *Controller, _ *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { c.rolloutLister = nil }},
		{name: "replaced rollout", configure: func(_ *Controller, ro *v1alpha1.Rollout, _ *v1alpha1.AnalysisRun) { ro.UID = "replacement" }},
		{name: "replaced analysis run", configure: func(_ *Controller, _ *v1alpha1.Rollout, run *v1alpha1.AnalysisRun) { run.UID = "replacement" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, controller, run, recorder := newAnalysisRunTracingFixture(t)
			rollout := configureAnalysisRunHandoff(controller, run)
			_, creator := controller.tracer.Start(context.Background(), "creator")
			controller.traceHandoffs.RecordCreation(rollout, run, creator.SpanContext())
			creator.End()
			if test.configure != nil {
				test.configure(controller, rollout, run)
			}
			for range 2 {
				attempt := traceutil.NewAttempt(context.Background(), controller.tracer, "controller.AnalysisRun", time.Now())
				attempt.Start(controller.analysisRunCreationParent(run))
				attempt.Finish(nil, nil, true)
			}
			var spans []sdktrace.ReadOnlySpan
			for _, span := range recorder.Ended() {
				if span.Name() == "controller.AnalysisRun" {
					spans = append(spans, span)
				}
			}
			require.Len(t, spans, 2)
			for _, span := range spans {
				assert.Equal(t, test.parented, span.Parent().IsValid())
				if test.parented {
					assert.Equal(t, creator.SpanContext().SpanID(), span.Parent().SpanID())
					assert.Equal(t, creator.SpanContext().TraceID(), span.SpanContext().TraceID())
				}
			}
			assert.NotEqual(t, spans[0].SpanContext().SpanID(), spans[1].SpanContext().SpanID())
		})
	}
}

func TestAnalysisRunPublishesOnlyPersistedTerminalResults(t *testing.T) {
	for _, test := range []struct {
		name          string
		configure     func(*v1alpha1.AnalysisRun)
		writeErr      bool
		published     bool
		expectedPhase string
		changed       bool
	}{
		{name: "successful terminal patch", published: true, expectedPhase: "Failed", changed: true},
		{name: "failed terminal patch", writeErr: true, expectedPhase: "Unknown"},
		{name: "replacement response", configure: func(run *v1alpha1.AnalysisRun) { run.UID = "replacement" }, expectedPhase: "Unknown"},
		{name: "missing response UID", configure: func(run *v1alpha1.AnalysisRun) { run.UID = "" }, expectedPhase: "Unknown"},
		{name: "changed response name", configure: func(run *v1alpha1.AnalysisRun) { run.Name = "replacement" }, expectedPhase: "Unknown"},
		{name: "changed response namespace", configure: func(run *v1alpha1.AnalysisRun) { run.Namespace = "replacement" }, expectedPhase: "Unknown"},
		{name: "changed response generation", configure: func(run *v1alpha1.AnalysisRun) { run.Generation++ }, expectedPhase: "Unknown"},
		{name: "admission changed phase", configure: func(run *v1alpha1.AnalysisRun) { run.Status.Phase = v1alpha1.AnalysisPhaseInconclusive }, expectedPhase: "Inconclusive", changed: true},
		{name: "admission changed result", configure: func(run *v1alpha1.AnalysisRun) { run.Status.MetricResults = nil }, expectedPhase: "Failed", changed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f, controller, run, recorder := newAnalysisRunTracingFixture(t)
			rollout := configureAnalysisRunHandoff(controller, run)
			_, creator := controller.tracer.Start(context.Background(), "creator")
			controller.traceHandoffs.RecordCreation(rollout, run, creator.SpanContext())
			creator.End()
			var response *v1alpha1.AnalysisRun
			f.client.PrependReactor("patch", "analysisruns", func(action clienttesting.Action) (bool, runtime.Object, error) {
				if test.writeErr {
					return true, nil, errors.New("private API error")
				}
				response = run.DeepCopy()
				require.NoError(t, json.Unmarshal(action.(clienttesting.PatchAction).GetPatch(), response))
				response.ResourceVersion = "2"
				if test.configure != nil {
					test.configure(response)
				}
				return true, response, nil
			})
			err := controller.syncHandler(context.Background(), getKey(run, t))
			if test.writeErr {
				require.Error(t, err)
				response = run.DeepCopy()
				response.Status.Phase = v1alpha1.AnalysisPhaseFailed
				response.ResourceVersion = "2"
			} else {
				require.NoError(t, err)
			}
			parent := controller.traceHandoffs.ResultParent(rollout, response, time.Now())
			assert.Equal(t, test.published, parent.IsValid())
			span := analysisRunAttemptSpan(t, recorder.Ended())
			attrs := analysisRunAttemptAttributes(span)
			assert.Equal(t, attribute.StringValue(string(run.UID)), attrs["argo.rollouts.analysis_run.uid"])
			assert.Equal(t, attribute.StringValue(run.Name), attrs["argo.rollouts.analysis_run.name"])
			assert.Equal(t, attribute.StringValue(run.Namespace), attrs["k8s.namespace.name"])
			assert.Equal(t, attribute.Int64Value(run.Generation), attrs["argo.rollouts.analysis_run.generation"])
			assert.Equal(t, attribute.StringValue(test.expectedPhase), attrs["argo.rollouts.analysis_run.phase"])
			assert.Equal(t, attribute.BoolValue(test.changed), attrs["argo.rollouts.reconciliation.status_changed"])
			if test.changed {
				require.Len(t, span.Events(), 1)
				assert.Contains(t, span.Events()[0].Attributes, attribute.String("argo.rollouts.analysis_run.phase", test.expectedPhase))
			} else {
				assert.Empty(t, span.Events())
			}
			if test.published {
				assert.Equal(t, span.SpanContext(), parent)
				assert.Equal(t, creator.SpanContext().SpanID(), span.Parent().SpanID())
			}
		})
	}
}

func newAnalysisRunTracingFixture(t *testing.T) (*fixture, *Controller, *v1alpha1.AnalysisRun, *tracetest.SpanRecorder) {
	t.Helper()
	f := newFixture(t)
	t.Cleanup(f.Close)
	count := intstr.FromInt(1)
	run := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name: "checkout-analysis", Namespace: metav1.NamespaceDefault,
			UID: types.UID("analysis-run-uid"), Generation: 7, ResourceVersion: "1",
			Labels: map[string]string{"private-label": "sensitive-value"},
			Annotations: map[string]string{
				"private-annotation":                             "sensitive-value",
				"observability.demo/traceparent":                 "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
				"observability.demo/rollout-traceparent":         "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
				"observability.demo/analysis-result-traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			},
		},
		Spec: v1alpha1.AnalysisRunSpec{Metrics: []v1alpha1.Metric{{
			Name: "private-metric-name", Count: &count,
			Provider: v1alpha1.MetricProvider{Prometheus: &v1alpha1.PrometheusMetric{Query: "private query"}},
		}}},
	}
	f.analysisRunLister = append(f.analysisRunLister, run)
	f.objects = append(f.objects, run)
	controller, _, _ := f.newController(noResyncPeriodFunc)
	f.provider.On("Run", mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseFailed), nil)
	f.provider.On("GetMetadata", mock.Anything, mock.Anything).Return(map[string]string{}, nil)
	tracer, recorder := newAnalysisRunAttemptTracer(t)
	controller.tracer = tracer
	return f, controller, run, recorder
}

func configureAnalysisRunHandoff(controller *Controller, run *v1alpha1.AnalysisRun) *v1alpha1.Rollout {
	index := int32(0)
	isController := true
	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "checkout", Namespace: run.Namespace, UID: "rollout-uid", Generation: 1},
		Spec: v1alpha1.RolloutSpec{Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{
			Steps: []v1alpha1.CanaryStep{{Analysis: &v1alpha1.RolloutAnalysis{}}},
		}}},
		Status: v1alpha1.RolloutStatus{
			CurrentPodHash: "pod-hash", CurrentStepIndex: &index,
			Canary: v1alpha1.CanaryStatus{CurrentStepAnalysisRunStatus: &v1alpha1.RolloutAnalysisRunStatus{Name: run.Name, Status: v1alpha1.AnalysisPhaseRunning}},
		},
	}
	run.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = rollout.Status.CurrentPodHash
	run.Labels[v1alpha1.RolloutCanaryStepIndexLabel] = "0"
	run.Labels[v1alpha1.RolloutTypeLabel] = v1alpha1.RolloutTypeStepLabel
	run.OwnerReferences = []metav1.OwnerReference{{
		APIVersion: v1alpha1.SchemeGroupVersion.String(), Kind: "Rollout", Name: rollout.Name, UID: rollout.UID, Controller: &isController,
	}}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	_ = indexer.Add(rollout)
	controller.rolloutLister = rolloutlisters.NewRolloutLister(indexer)
	controller.traceHandoffs = traceutil.NewHandoffs()
	return rollout
}

func newAnalysisRunAttemptTracer(t *testing.T) (oteltrace.Tracer, *tracetest.SpanRecorder) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	return provider.Tracer("analysis-test"), recorder
}

func analysisRunAttemptSpan(t *testing.T, spans []sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		if span.Name() == "controller.AnalysisRun" {
			return span
		}
	}
	t.Fatal("AnalysisRun attempt span not found")
	return nil
}

func analysisRunAttemptAttributes(span sdktrace.ReadOnlySpan) map[string]attribute.Value {
	attrs := make(map[string]attribute.Value, len(span.Attributes()))
	for _, attr := range span.Attributes() {
		attrs[string(attr.Key)] = attr.Value
	}
	return attrs
}
