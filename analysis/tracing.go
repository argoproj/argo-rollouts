package analysis

import (
	"bytes"
	"encoding/json"

	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	traceutil "github.com/argoproj/argo-rollouts/utils/trace"
)

func setAnalysisRunReconciliationCause(attempt *traceutil.Attempt, cause string) {
	if attempt != nil {
		attempt.SetAttributes(attribute.String("argo.rollouts.reconciliation.cause", cause))
	}
}

func finishAnalysisRunAttempt(attempt *traceutil.Attempt, before, after *v1alpha1.AnalysisRun, err error, recovered any) {
	if attempt == nil {
		return
	}
	statusChanged := before != nil && after != nil && before != after && !analysisRunStatusesEqual(before.Status, after.Status)
	if statusChanged && before.Status.Phase != after.Status.Phase {
		attempt.AddEvent("analysis_run.phase_changed",
			attribute.String("argo.rollouts.analysis_run.previous_phase", traceutil.AnalysisPhaseValue(before.Status.Phase)),
			attribute.String("argo.rollouts.analysis_run.phase", traceutil.AnalysisPhaseValue(after.Status.Phase)),
		)
	}
	attempt.Finish(err, recovered, statusChanged, analysisRunTraceAttributes(after)...)
}

func (c *Controller) analysisRunCreationParent(run *v1alpha1.AnalysisRun) oteltrace.SpanContext {
	if run == nil || c.rolloutLister == nil {
		return oteltrace.SpanContext{}
	}
	owner := metav1.GetControllerOf(run)
	if owner == nil || owner.APIVersion != v1alpha1.SchemeGroupVersion.String() || owner.Kind != "Rollout" || owner.UID == "" {
		return oteltrace.SpanContext{}
	}
	rollout, err := c.rolloutLister.Rollouts(run.Namespace).Get(owner.Name)
	if err != nil || rollout.UID != owner.UID {
		return oteltrace.SpanContext{}
	}
	return c.traceHandoffs.CreationParent(rollout, run)
}

// Result context describes only the terminal status accepted by the API server.
// Admission changes and replaced resources must not publish the attempted result.
func persistedAnalysisRunResultMatches(orig, intended, persisted *v1alpha1.AnalysisRun) bool {
	return intended != nil && sameAnalysisRunIdentity(orig, persisted) &&
		!orig.Status.Phase.Completed() && intended.Status.Phase.Completed() &&
		analysisRunStatusesEqual(intended.Status, persisted.Status)
}

func sameAnalysisRunIdentity(orig, persisted *v1alpha1.AnalysisRun) bool {
	return orig != nil && persisted != nil && orig.UID != "" &&
		orig.UID == persisted.UID && orig.Name == persisted.Name &&
		orig.Namespace == persisted.Namespace && orig.Generation == persisted.Generation
}

// Compare the persisted representation: metav1.Time loses subsecond precision
// when marshaled, and omitted empty fields can return as nil after an API write.
func analysisRunStatusesEqual(a, b v1alpha1.AnalysisRunStatus) bool {
	aJSON, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bJSON, err := json.Marshal(b)
	return err == nil && bytes.Equal(aJSON, bJSON)
}

func analysisRunTraceAttributes(run *v1alpha1.AnalysisRun) []attribute.KeyValue {
	if run == nil {
		return nil
	}
	attrs := []attribute.KeyValue{
		attribute.String("k8s.namespace.name", run.Namespace),
		attribute.String("argo.rollouts.analysis_run.name", run.Name),
		attribute.Int64("argo.rollouts.analysis_run.generation", run.Generation),
		attribute.String("argo.rollouts.analysis_run.phase", traceutil.AnalysisPhaseValue(run.Status.Phase)),
		attribute.Int("argo.rollouts.analysis_run.metrics.count", len(run.Spec.Metrics)),
	}
	if run.UID != "" {
		attrs = append(attrs, attribute.String("argo.rollouts.analysis_run.uid", string(run.UID)))
	}
	counts := make(map[v1alpha1.AnalysisPhase]int)
	for _, result := range run.Status.MetricResults {
		counts[result.Phase]++
	}
	return append(attrs,
		attribute.Int("argo.rollouts.analysis_run.metric_results.count", len(run.Status.MetricResults)),
		attribute.Int("argo.rollouts.analysis_run.metric_results.pending.count", counts[v1alpha1.AnalysisPhasePending]),
		attribute.Int("argo.rollouts.analysis_run.metric_results.running.count", counts[v1alpha1.AnalysisPhaseRunning]),
		attribute.Int("argo.rollouts.analysis_run.metric_results.successful.count", counts[v1alpha1.AnalysisPhaseSuccessful]),
		attribute.Int("argo.rollouts.analysis_run.metric_results.failed.count", counts[v1alpha1.AnalysisPhaseFailed]),
		attribute.Int("argo.rollouts.analysis_run.metric_results.error.count", counts[v1alpha1.AnalysisPhaseError]),
		attribute.Int("argo.rollouts.analysis_run.metric_results.inconclusive.count", counts[v1alpha1.AnalysisPhaseInconclusive]),
	)
}
