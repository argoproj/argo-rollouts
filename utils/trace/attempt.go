package trace

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// InstrumentationName identifies controller instrumentation from Argo Rollouts.
const InstrumentationName = "github.com/argoproj/argo-rollouts"

// Attempt starts a span when useful work or a validated handoff is established.
// Its timestamp includes the preparation before that decision. An unstarted,
// unchanged successful attempt emits no span. An Attempt belongs to one worker.
type Attempt struct {
	ctx     context.Context
	tracer  oteltrace.Tracer
	name    string
	started time.Time
	attrs   []attribute.KeyValue
	span    oteltrace.Span
	ended   bool
}

// NewAttempt captures an attempt's start without emitting a span yet.
func NewAttempt(ctx context.Context, tracer oteltrace.Tracer, name string, started time.Time, attrs ...attribute.KeyValue) *Attempt {
	if ctx == nil {
		ctx = context.Background()
	}
	if tracer == nil {
		tracer = otel.Tracer(InstrumentationName)
	}
	return &Attempt{ctx: ctx, tracer: tracer, name: name, started: started, attrs: attrs}
}

func (a *Attempt) StartTime() time.Time {
	if a == nil {
		return time.Time{}
	}
	return a.started
}

// Start selects a validated parent only before the span has started. Without a
// valid parent, the queue's context does not become the reconciliation's parent.
func (a *Attempt) Start(parent oteltrace.SpanContext, attrs ...attribute.KeyValue) (context.Context, bool) {
	if a == nil {
		return context.Background(), false
	}
	if a.span != nil || a.ended {
		return a.ctx, false
	}
	opts := []oteltrace.SpanStartOption{
		oteltrace.WithTimestamp(a.started),
		oteltrace.WithAttributes(append(a.attrs, attrs...)...),
	}
	ctx := a.ctx
	if parent.IsValid() {
		ctx = oteltrace.ContextWithSpanContext(ctx, parent)
	} else {
		opts = append(opts, oteltrace.WithNewRoot())
	}
	a.ctx, a.span = a.tracer.Start(ctx, a.name, opts...)
	a.attrs = nil
	return a.ctx, true
}

func (a *Attempt) Context() context.Context {
	ctx, _ := a.Start(oteltrace.SpanContext{})
	return ctx
}

func (a *Attempt) SpanContext() oteltrace.SpanContext {
	if a == nil || a.span == nil {
		return oteltrace.SpanContext{}
	}
	return a.span.SpanContext()
}

func (a *Attempt) SetAttributes(attrs ...attribute.KeyValue) {
	if a == nil || a.ended {
		return
	}
	if a.span == nil {
		a.attrs = append(a.attrs, attrs...)
		return
	}
	a.span.SetAttributes(attrs...)
}

func (a *Attempt) AddEvent(name string, attrs ...attribute.KeyValue) {
	if a == nil || a.ended {
		return
	}
	a.Context()
	a.span.AddEvent(name, oteltrace.WithAttributes(attrs...))
}

// Finish records execution errors separately from the resource's rollout or
// analysis outcome. Error details remain in the controller logs.
func (a *Attempt) Finish(err error, recovered any, statusChanged bool, attrs ...attribute.KeyValue) {
	if a == nil || a.ended {
		return
	}
	if a.span == nil && err == nil && recovered == nil && !statusChanged {
		a.ended = true
		return
	}
	a.Context()
	outcome := "no_change"
	if statusChanged {
		outcome = "state_changed"
	}
	if err != nil {
		outcome = "error"
		a.span.SetStatus(codes.Error, "reconciliation error")
	}
	if recovered != nil {
		outcome = "panic"
		a.span.SetStatus(codes.Error, "reconciliation panic")
	}
	a.SetAttributes(
		attribute.String("argo.rollouts.reconciliation.outcome", outcome),
		attribute.Bool("argo.rollouts.reconciliation.status_changed", statusChanged),
	)
	a.SetAttributes(attrs...)
	a.span.End()
	a.ended = true
}
