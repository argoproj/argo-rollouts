package promotion

import (
	"context"
	"fmt"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// Only this error represents a transient precondition. Identity mismatches and
// stale step expectations must never be retried as if they were readiness.
type readinessPendingError struct{ message string }

func (e *readinessPendingError) Error() string { return e.message }

func pendingReadiness(target Target, state map[string]interface{}, reason string) error {
	return &readinessPendingError{fmt.Sprintf("rollout %s/%s waiting: %s", state["namespace"], target.Name, reason)}
}

func observedLatestGeneration(state map[string]interface{}) bool {
	for _, pair := range [][2]string{{"generation", "observedGeneration"}, {"workloadGeneration", "workloadObservedGeneration"}} {
		generation, _ := strconv.ParseInt(fmt.Sprint(state[pair[0]]), 10, 64)
		observed, _ := strconv.ParseInt(fmt.Sprint(state[pair[1]]), 10, 64)
		if generation > observed {
			return false
		}
	}
	return true
}

func validatePromotionIdentity(target Target, state map[string]interface{}) error {
	for _, check := range []struct{ field, expected, key string }{
		{"expectedRolloutUID", target.ExpectedRolloutUID, "rolloutUID"},
		{"expectedPodTemplateHash", target.ExpectedPodTemplateHash, "podTemplateHash"},
		{"expectedCurrentPodHash", target.ExpectedCurrentPodHash, "currentPodHash"},
	} {
		if check.expected != "" && state[check.key] != check.expected {
			return fmt.Errorf("rollout %s/%s safety check failed: %s expected %q but got %v", state["namespace"], target.Name, check.field, check.expected, state[check.key])
		}
	}
	return nil
}

func (c *Controller) checkForwardReadiness(ctx context.Context, namespace string, spec RequestSpec) error {
	var pending error
	for _, target := range spec.Targets {
		if target.Readiness == nil {
			continue
		}
		ns := target.Namespace
		if ns == "" {
			ns = namespace
		}
		state, err := c.observeTarget(ctx, ns, target)
		if err != nil {
			if apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) || apierrors.IsServiceUnavailable(err) || apierrors.IsTooManyRequests(err) {
				pending = &readinessPendingError{fmt.Sprintf("rollout %s/%s API temporarily unavailable: %v", ns, target.Name, err)}
				continue
			}
			return err
		}
		err = validateForwardReadiness(target, state, spec.Action)
		if err == nil {
			continue
		}
		if _, ok := err.(*readinessPendingError); !ok {
			return err
		}
		pending = err
	}
	return pending
}

func (c *Controller) checkCommittedIdentity(ctx context.Context, namespace string, targets []Target) error {
	for _, target := range targets {
		if target.Readiness == nil {
			continue
		}
		ns := target.Namespace
		if ns == "" {
			ns = namespace
		}
		state, err := c.observeTarget(ctx, ns, target)
		if err != nil {
			return err
		}
		if err := validatePromotionIdentity(target, state); err != nil {
			return err
		}
	}
	return nil
}

func validateForwardReadiness(target Target, state map[string]interface{}, action string) error {
	if target.Readiness == nil {
		return nil
	}
	// Check immutable request identity before waiting for status to catch up.
	identity := target
	identity.ExpectedCurrentPodHash = ""
	if err := validatePromotionIdentity(identity, state); err != nil {
		return err
	}
	if err := validateRequestedStrategy(target.Strategy, state); err != nil {
		return err
	}
	if !observedLatestGeneration(state) {
		return pendingReadiness(target, state, "controller has not observed the latest workload generation")
	}
	current, _ := state["currentPodHash"].(string)
	if current == "" && (target.Readiness.RequireCurrentPodHash || target.ExpectedCurrentPodHash != "") {
		return pendingReadiness(target, state, "currentPodHash is not available")
	}
	if err := validatePromotionIdentity(target, state); err != nil {
		return err
	}
	if aborted, _ := state["aborted"].(bool); aborted || state["rolloutPhase"] == "Degraded" {
		return fmt.Errorf("rollout %s/%s is aborted or degraded; refusing promotion", state["namespace"], target.Name)
	}
	if targetAlreadyPromotedToExpected(target, state) {
		return nil
	}
	// Missing baseline fields are transient; a different nonempty baseline is
	// not. Re-check all of them immediately before committing the operation.
	for _, check := range []struct{ expected, key string }{
		{target.ExpectedStableRS, "stableRS"}, {target.ExpectedActiveServiceHash, "activeServiceHash"},
	} {
		if check.expected != "" && state[check.key] == "" {
			return pendingReadiness(target, state, check.key+" is not available")
		}
	}
	if err := validateTargetIdentityExpectations(target, state); err != nil {
		return err
	}
	legacy, _ := state["legacyAdoptionPending"].(bool)
	step, _ := state["currentStepIndex"].(int64)
	if target.ExpectedCurrentStepIndex != nil && !legacy {
		expected := int64(*target.ExpectedCurrentStepIndex)
		if step < expected {
			return pendingReadiness(target, state, fmt.Sprintf("current step %d has not reached authorized step %d", step, expected))
		}
		if step > expected {
			return fmt.Errorf("rollout %s/%s safety check failed: authorized step %d has already passed (current %d)", state["namespace"], target.Name, expected, step)
		}
	}
	if action == v1alpha1.RolloutPromotionActionPromote && !legacy {
		if state["strategy"] == "canary" && len(target.Readiness.AllowedCurrentStepIndexes) > 0 {
			allowed := false
			for _, index := range target.Readiness.AllowedCurrentStepIndexes {
				allowed = allowed || int64(index) == step
			}
			if !allowed {
				return fmt.Errorf("rollout %s/%s safety check failed: step %d is not in allowedCurrentStepIndexes", state["namespace"], target.Name, step)
			}
		}
		if target.Readiness.RequirePaused {
			paused, _ := state["promotionPaused"].(bool)
			if !paused {
				return pendingReadiness(target, state, "authorized promotion pause has not been established")
			}
		}
	}
	return nil
}
