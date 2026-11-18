package rollout

import (
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	traceutil "github.com/argoproj/argo-rollouts/utils/trace"
)

type rolloutTraceResult struct {
	run    *v1alpha1.AnalysisRun
	parent oteltrace.SpanContext
	step   int32
}

// traceResult starts the attempt only when the native canary gate consumes a
// result. Queue timing and user-provided propagation metadata are not evidence.
func (c *rolloutContext) traceResult(run *v1alpha1.AnalysisRun) {
	if c.rollout == nil || run == nil || c.traceAttempt == nil || c.traceAttempt.SpanContext().IsValid() {
		return
	}
	statuses := []*v1alpha1.RolloutAnalysisRunStatus{
		c.rollout.Status.Canary.CurrentStepAnalysisRunStatus,
		c.rollout.Status.Canary.CurrentBackgroundAnalysisRunStatus,
		c.rollout.Status.BlueGreen.PrePromotionAnalysisRunStatus,
		c.rollout.Status.BlueGreen.PostPromotionAnalysisRunStatus,
	}
	runs := []*v1alpha1.AnalysisRun{
		c.currentArs.CanaryStep, c.currentArs.CanaryBackground,
		c.currentArs.BlueGreenPrePromotion, c.currentArs.BlueGreenPostPromotion,
	}
	seen := make(map[string]bool)
	for i, current := range runs {
		status := statuses[i]
		if current == nil || status == nil || status.Name != current.Name || status.Status == current.Status.Phase ||
			!current.Status.Phase.Completed() || !traceRunOwnedByRollout(current, c.rollout) {
			continue
		}
		seen[string(current.UID)] = true
	}
	if len(seen) != 1 || !seen[string(run.UID)] {
		return
	}
	parent := c.traceHandoffs.ResultParent(c.rollout, run, c.traceAttempt.StartTime())
	if !parent.IsValid() {
		return
	}
	_, started := c.traceAttempt.Start(parent,
		attribute.String("argo.rollouts.reconciliation.cause", "analysis_run_result"),
		attribute.String("argo.rollouts.analysis_run.name", run.Name),
		attribute.String("argo.rollouts.analysis_run.uid", string(run.UID)),
		attribute.String("argo.rollouts.analysis_run.previous_phase", traceutil.AnalysisPhaseValue(c.rollout.Status.Canary.CurrentStepAnalysisRunStatus.Status)),
		attribute.String("argo.rollouts.analysis_run.phase", traceutil.AnalysisPhaseValue(run.Status.Phase)),
	)
	if started {
		_, step := replicasetutil.GetCurrentCanaryStep(c.rollout)
		if step != nil {
			c.pendingTraceResult = &rolloutTraceResult{run: run, parent: parent, step: *step}
		}
	}
}

func traceRunOwnedByRollout(run *v1alpha1.AnalysisRun, rollout *v1alpha1.Rollout) bool {
	if run.UID == "" || rollout.UID == "" || run.Namespace != rollout.Namespace || run.DeletionTimestamp != nil {
		return false
	}
	owner := metav1.GetControllerOf(run)
	return owner != nil && owner.APIVersion == v1alpha1.SchemeGroupVersion.String() &&
		owner.Kind == "Rollout" && owner.Name == rollout.Name && owner.UID == rollout.UID
}

// tracePersistedStatus is called only after an API patch succeeds. A failed
// patch leaves the result available for the next reconciliation attempt.
func (c *rolloutContext) tracePersistedStatus(before, after *v1alpha1.Rollout) {
	if !rolloutTraceResponseMatches(before, after) {
		return
	}
	if pending := c.pendingTraceResult; pending != nil {
		status := after.Status.Canary.CurrentStepAnalysisRunStatus
		recorded := status != nil && status.Name == pending.run.Name && status.Status == pending.run.Status.Phase
		advanced := after.Status.CurrentStepIndex != nil && *after.Status.CurrentStepIndex > pending.step
		if recorded || advanced {
			c.traceHandoffs.ConsumeResult(pending.run, pending.parent)
			c.pendingTraceResult = nil
		}
	}
	if before.Status.CurrentStepIndex != nil && after.Status.CurrentStepIndex != nil && *after.Status.CurrentStepIndex > *before.Status.CurrentStepIndex {
		c.traceAttempt.AddEvent("rollout.step.advanced", attribute.Int64("argo.rollouts.rollout.current_step.index", int64(*after.Status.CurrentStepIndex)))
	}
	if after.Status.CurrentPodHash != "" && before.Status.StableRS != after.Status.StableRS && after.Status.StableRS == after.Status.CurrentPodHash {
		c.traceAttempt.AddEvent("rollout.promoted")
	}
	if len(before.Status.PauseConditions) == 0 && len(after.Status.PauseConditions) > 0 {
		c.traceAttempt.AddEvent("rollout.paused")
	}
	if !before.Status.Abort && after.Status.Abort {
		c.traceAttempt.AddEvent("rollout.aborted")
	}
	if completed := c.pendingDurationMetric; completed != nil && completed.GetCompletionStatus() == v1alpha1.CompletionStatusSuperseded {
		c.traceAttempt.AddEvent("rollout.superseded")
	}
}

// A successful write can race with resource replacement or a new generation.
// Keep that response in the native reconciler, but retain the original tracing
// identity rather than attaching its attributes to the original attempt parent.
func rolloutTraceSnapshot(before, after *v1alpha1.Rollout) *v1alpha1.Rollout {
	if !rolloutTraceResponseMatches(before, after) {
		return before
	}
	return after
}

func rolloutTraceResponseMatches(before, after *v1alpha1.Rollout) bool {
	return before != nil && after != nil && before.UID != "" &&
		before.UID == after.UID && before.Name == after.Name &&
		before.Namespace == after.Namespace && before.Generation == after.Generation
}

func rolloutTraceCause(rollout *v1alpha1.Rollout) string {
	switch {
	case rollout.DeletionTimestamp != nil:
		return "deletion"
	case rollout.Status.ObservedGeneration != strconv.FormatInt(rollout.Generation, 10):
		return "spec_change"
	case rollout.Status.Abort && rollout.Status.AbortedAt == nil:
		return "abort_request"
	default:
		return "status_or_owned_resource_change"
	}
}

func rolloutTraceAttributes(rollout *v1alpha1.Rollout) []attribute.KeyValue {
	if rollout == nil {
		return nil
	}
	strategy := "unknown"
	if rollout.Spec.Strategy.Canary != nil {
		strategy = "canary"
	} else if rollout.Spec.Strategy.BlueGreen != nil {
		strategy = "blue_green"
	}
	phase := traceutil.RolloutPhaseValue(rollout.Status.Phase)
	attrs := []attribute.KeyValue{
		attribute.String("k8s.namespace.name", rollout.Namespace),
		attribute.String("argo.rollouts.rollout.name", rollout.Name),
		attribute.String("argo.rollouts.rollout.uid", string(rollout.UID)),
		attribute.Int64("argo.rollouts.rollout.generation", rollout.Generation),
		attribute.String("argo.rollouts.rollout.strategy", strategy),
		attribute.String("argo.rollouts.rollout.phase", phase),
		attribute.Bool("argo.rollouts.rollout.aborted", rollout.Status.Abort || rollout.Status.AbortedAt != nil),
		attribute.Bool("argo.rollouts.rollout.promote_full", rollout.Status.PromoteFull),
	}
	if generation, err := strconv.ParseInt(rollout.Status.ObservedGeneration, 10, 64); err == nil && generation >= 0 {
		attrs = append(attrs, attribute.Int64("argo.rollouts.rollout.observed_generation", generation))
	}
	if revision, err := strconv.ParseInt(rollout.Annotations[annotations.RevisionAnnotation], 10, 64); err == nil && revision > 0 {
		attrs = append(attrs, attribute.Int64("argo.rollouts.rollout.revision", revision))
	}
	if validRolloutTraceHash(rollout.Status.CurrentPodHash) {
		attrs = append(attrs, attribute.String("argo.rollouts.rollout.pod_template_hash", rollout.Status.CurrentPodHash))
	}
	if validRolloutTraceHash(rollout.Status.CurrentStepHash) {
		attrs = append(attrs, attribute.String("argo.rollouts.rollout.step_hash", rollout.Status.CurrentStepHash))
	}
	step, index := replicasetutil.GetCurrentCanaryStep(rollout)
	if index != nil {
		attrs = append(attrs, attribute.Int64("argo.rollouts.rollout.current_step.index", int64(*index)))
		typeName, value := canaryStepTraceValue(step)
		attrs = append(attrs, attribute.String("argo.rollouts.rollout.current_step.type", typeName))
		if value != "" {
			attrs = append(attrs, attribute.String("argo.rollouts.rollout.current_step.value", value))
		}
	}
	return attrs
}

func validRolloutTraceHash(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, c := range value {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func canaryStepTraceValue(step *v1alpha1.CanaryStep) (string, string) {
	switch {
	case step == nil:
		return "completed", ""
	case step.SetWeight != nil:
		return "set_weight", strconv.Itoa(int(*step.SetWeight))
	case step.Pause != nil && step.Pause.Duration != nil:
		if seconds := step.Pause.DurationSeconds(); seconds >= 0 {
			return "pause", strconv.Itoa(int(seconds))
		}
		return "pause", "invalid"
	case step.Pause != nil:
		return "pause", "indefinite"
	case step.Analysis != nil:
		return "analysis", ""
	case step.Experiment != nil:
		return "experiment", ""
	case step.SetCanaryScale != nil:
		return "set_canary_scale", ""
	case step.SetHeaderRoute != nil:
		return "set_header_route", ""
	case step.SetMirrorRoute != nil:
		return "set_mirror_route", ""
	case step.Plugin != nil:
		return "plugin", ""
	default:
		return "unknown", ""
	}
}
