package trace

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func attemptRecorder(t *testing.T) (oteltrace.Tracer, *tracetest.SpanRecorder) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	return provider.Tracer("attempt-test"), recorder
}

func TestAttemptSuppressesIdleWork(t *testing.T) {
	tracer, recorder := attemptRecorder(t)
	attempt := NewAttempt(t.Context(), tracer, "reconcile", time.Now())
	attempt.Finish(nil, nil, false)
	assert.Empty(t, recorder.Ended())
	assert.False(t, attempt.SpanContext().IsValid())
}

func TestAttemptRecordsFullDurationAndIndependentRoot(t *testing.T) {
	tracer, recorder := attemptRecorder(t)
	worker := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{TraceID: oteltrace.TraceID{1}, SpanID: oteltrace.SpanID{2}})
	started := time.Now().Add(-time.Second)
	attempt := NewAttempt(oteltrace.ContextWithSpanContext(t.Context(), worker), tracer, "reconcile", started)
	attempt.SetAttributes(attribute.String("resource", "checkout"))
	attempt.Finish(nil, nil, true)
	attempt.Finish(nil, nil, true)
	require.Len(t, recorder.Ended(), 1)
	span := recorder.Ended()[0]
	assert.Equal(t, started, span.StartTime())
	assert.False(t, span.Parent().IsValid())
	assert.Equal(t, codes.Unset, span.Status().Code)
	assert.Contains(t, span.Attributes(), attribute.String("argo.rollouts.reconciliation.outcome", "state_changed"))
}

func TestAttemptSelectsParentOnlyBeforeStart(t *testing.T) {
	for _, rootFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "native handoff", true: "earlier independent work"}[rootFirst], func(t *testing.T) {
			tracer, recorder := attemptRecorder(t)
			_, parent := tracer.Start(t.Context(), "parent")
			parent.End()
			attempt := NewAttempt(t.Context(), tracer, "reconcile", time.Now())
			if rootFirst {
				attempt.Context()
			}
			_, selected := attempt.Start(parent.SpanContext(), attribute.String("cause", "analysis_run_result"))
			assert.Equal(t, !rootFirst, selected)
			attempt.Context()
			attempt.Finish(nil, nil, false)
			require.Len(t, recorder.Ended(), 2)
			span := recorder.Ended()[1]
			if rootFirst {
				assert.False(t, span.Parent().IsValid())
			} else {
				assert.Equal(t, parent.SpanContext().SpanID(), span.Parent().SpanID())
				assert.Equal(t, parent.SpanContext().TraceID(), span.SpanContext().TraceID())
			}
		})
	}
}

func TestAttemptExecutionFailuresKeepDetailsOutOfTelemetry(t *testing.T) {
	for _, panicValue := range []any{nil, "private panic detail"} {
		tracer, recorder := attemptRecorder(t)
		attempt := NewAttempt(t.Context(), tracer, "reconcile", time.Now())
		attempt.Finish(errors.New("private API error"), panicValue, false)
		require.Len(t, recorder.Ended(), 1)
		span := recorder.Ended()[0]
		assert.Equal(t, codes.Error, span.Status().Code)
		assert.NotContains(t, span.Status().Description, "private")
		assert.Empty(t, span.Events())
	}
}

func TestNilAttempt(t *testing.T) {
	attempt := NewAttempt(t.Context(), nil, "disabled", time.Now())
	require.Nil(t, attempt)
	assert.NotNil(t, attempt.Context())
	assert.False(t, attempt.SpanContext().IsValid())
	attempt.SetAttributes(attribute.String("ignored", "value"))
	attempt.AddEvent("ignored")
	attempt.Finish(nil, nil, false)
}
