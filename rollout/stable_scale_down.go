package rollout

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

// stableScaleDownHold is the value of the stable-scale-down-hold annotation.
type stableScaleDownHold struct {
	Deadline metav1.Time `json:"deadline"`
	Replicas int32       `json:"replicas"`
}

// holdStableScaleDown applies stableScaleDownPolicy to the desired stable ReplicaSet count and returns
// the count to scale to. A scale-down is held at the current count until delaySeconds have passed
// since the desired count last dropped: a lower count requested during a hold restarts the clock, so
// the final traffic shift gets the full delay even when the steps before it were shorter than it.
// An abort that is still moving traffic back to the stable drops the hold and keeps the stable's pods.
func (c *rolloutContext) holdStableScaleDown(desired int32) (int32, error) {
	delay := defaults.GetStableScaleDownDelaySeconds(c.rollout)
	if delay == nil {
		return desired, nil
	}
	rs := c.stableRS
	current := *rs.Spec.Replicas
	weights := c.rollout.Status.Canary.Weights
	abortShifting := c.rollout.Status.Abort && (weights == nil || weights.Canary.Weight > 0)
	if desired >= current || abortShifting {
		return max(desired, current), c.setStableScaleDownHold(rs, nil)
	}
	hold := getStableScaleDownHold(rs)
	if hold == nil || desired != hold.Replicas {
		// A rise keeps the deadline: only a drop moves traffic off the stable.
		if hold == nil || desired < hold.Replicas {
			hold = &stableScaleDownHold{Deadline: metav1.NewTime(timeutil.Now().Add(*delay))}
		}
		hold.Replicas = desired
		if err := c.setStableScaleDownHold(rs, hold); err != nil {
			return current, err
		}
	}
	remaining := hold.Deadline.Sub(timeutil.Now())
	if remaining <= 0 {
		return desired, nil
	}
	c.log.Infof("Holding stable RS '%s' at %d replicas (desired %d) for %s", rs.Name, current, desired, remaining.Round(time.Second))
	c.enqueueRolloutAfter(c.rollout, remaining)
	return current, nil
}

func getStableScaleDownHold(rs *appsv1.ReplicaSet) *stableScaleDownHold {
	value, ok := rs.Annotations[v1alpha1.DefaultStableScaleDownHoldAnnotationKey]
	if !ok {
		return nil
	}
	var hold stableScaleDownHold
	if err := json.Unmarshal([]byte(value), &hold); err != nil {
		return nil
	}
	return &hold
}

// setStableScaleDownHold writes the hold annotation, or removes it when hold is nil.
func (c *rolloutContext) setStableScaleDownHold(rs *appsv1.ReplicaSet, hold *stableScaleDownHold) error {
	key := v1alpha1.DefaultStableScaleDownHoldAnnotationKey
	var value any
	if hold == nil {
		if _, ok := rs.Annotations[key]; !ok {
			return nil
		}
	} else {
		b, err := json.Marshal(hold)
		if err != nil {
			return err
		}
		value = string(b)
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]any{key: value}}})
	if err != nil {
		return err
	}
	updated, err := c.kubeclientset.AppsV1().ReplicaSets(rs.Namespace).Patch(context.TODO(), rs.Name, patchtypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("error patching '%s' annotation on RS '%s': %w", key, rs.Name, err)
	}
	// A scale later in this pass updates rs, so it needs the new resourceVersion.
	updated.DeepCopyInto(rs)
	return nil
}
