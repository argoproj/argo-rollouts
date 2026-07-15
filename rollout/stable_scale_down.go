package rollout

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

const stableScaleDownPolicyLogPrefix = "[stableScaleDownPolicy]"

// addStableScaleDownDelay injects the stable-scale-down-deadline annotation on the stable ReplicaSet.
func (c *rolloutContext) addStableScaleDownDelay(rs *appsv1.ReplicaSet, delay time.Duration) error {
	if rs == nil {
		return nil
	}
	ctx := context.TODO()
	deadline := timeutil.MetaNow().Add(delay).UTC().Format(time.RFC3339)
	var patch string
	if rs.Annotations == nil {
		patch = fmt.Sprintf(`[{ "op": "add", "path": "/metadata/annotations", "value": {"%s": "%s"}}]`,
			v1alpha1.DefaultStableScaleDownDeadlineAnnotationKey, deadline)
	} else {
		patch = fmt.Sprintf(addScaleDownAtAnnotationsPatch, v1alpha1.DefaultStableScaleDownDeadlineAnnotationKey, deadline)
	}
	_, err := c.kubeclientset.AppsV1().ReplicaSets(rs.Namespace).Patch(ctx, rs.Name, patchtypes.JSONPatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("error adding stable-scale-down-deadline annotation to RS '%s': %w", rs.Name, err)
	}
	if rs.Annotations == nil {
		rs.Annotations = map[string]string{}
	}
	rs.Annotations[v1alpha1.DefaultStableScaleDownDeadlineAnnotationKey] = deadline
	c.log.Infof("%s set '%s' annotation on stable RS '%s' to %s (delay=%s)",
		stableScaleDownPolicyLogPrefix, v1alpha1.DefaultStableScaleDownDeadlineAnnotationKey, rs.Name, deadline, delay)
	return nil
}

// removeStableScaleDownDelay removes the stable-scale-down-deadline annotation from the stable ReplicaSet.
func (c *rolloutContext) removeStableScaleDownDelay(rs *appsv1.ReplicaSet) error {
	if rs == nil || !replicasetutil.HasStableScaleDownDeadline(rs) {
		return nil
	}
	ctx := context.TODO()
	patch := fmt.Sprintf(removeScaleDownAtAnnotationsPatch, v1alpha1.DefaultStableScaleDownDeadlineAnnotationKey)
	_, err := c.kubeclientset.AppsV1().ReplicaSets(rs.Namespace).Patch(ctx, rs.Name, patchtypes.JSONPatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return fmt.Errorf("error removing stable-scale-down-deadline annotation from RS '%s': %w", rs.Name, err)
	}
	c.log.Infof("%s removed '%s' annotation from stable RS '%s'",
		stableScaleDownPolicyLogPrefix, v1alpha1.DefaultStableScaleDownDeadlineAnnotationKey, rs.Name)
	delete(rs.Annotations, v1alpha1.DefaultStableScaleDownDeadlineAnnotationKey)
	return nil
}

// applyStableScaleDownPolicy gates stable ReplicaSet scale-down when stableScaleDownPolicy is configured.
// It returns the replica count to scale to and whether reconciliation should short-circuit after scaling.
func (c *rolloutContext) applyStableScaleDownPolicy(desiredStableRSReplicaCount int32) (int32, bool, error) {
	delay := defaults.GetStableScaleDownDelaySeconds(c.rollout)
	if delay == nil || c.stableRS == nil || c.stableRS.Spec.Replicas == nil {
		c.log.Debugf("%s policy inactive (delay=%v stableRS=%v)", stableScaleDownPolicyLogPrefix, delay, c.stableRS != nil)
		return desiredStableRSReplicaCount, false, nil
	}

	currentReplicas := *c.stableRS.Spec.Replicas
	c.log.Debugf("%s evaluating stable scale: desired=%d current=%d delay=%s",
		stableScaleDownPolicyLogPrefix, desiredStableRSReplicaCount, currentReplicas, *delay)

	if desiredStableRSReplicaCount >= currentReplicas {
		if replicasetutil.HasStableScaleDownDeadline(c.stableRS) {
			c.log.Infof("%s scale-up or hold requested (desired=%d >= current=%d), clearing deadline annotation",
				stableScaleDownPolicyLogPrefix, desiredStableRSReplicaCount, currentReplicas)
			if err := c.removeStableScaleDownDelay(c.stableRS); err != nil {
				return desiredStableRSReplicaCount, false, err
			}
		}
		return desiredStableRSReplicaCount, false, nil
	}

	if replicasetutil.HasStableScaleDownDeadline(c.stableRS) {
		remainingTime, err := replicasetutil.GetTimeRemainingBeforeStableScaleDownDeadline(c.stableRS)
		if err != nil {
			c.log.Warnf("%s failed reading deadline on stable RS '%s': %v", stableScaleDownPolicyLogPrefix, c.stableRS.Name, err)
			return desiredStableRSReplicaCount, false, err
		}
		if remainingTime != nil {
			c.log.Infof("%s holding stable RS '%s' at %d replicas for %s (desired=%d)",
				stableScaleDownPolicyLogPrefix, c.stableRS.Name, currentReplicas, *remainingTime, desiredStableRSReplicaCount)
			logutil.WithRollout(c.rollout).Info("rollout enqueue due to stableScaleDownPolicy")
			if *remainingTime < c.resyncPeriod {
				c.enqueueRolloutAfter(c.rollout, *remainingTime)
			}
			return currentReplicas, true, nil
		}
		c.log.Infof("%s deadline elapsed on stable RS '%s', allowing scale-down to %d",
			stableScaleDownPolicyLogPrefix, c.stableRS.Name, desiredStableRSReplicaCount)
		if err := c.removeStableScaleDownDelay(c.stableRS); err != nil {
			return desiredStableRSReplicaCount, false, err
		}
		return desiredStableRSReplicaCount, false, nil
	}

	c.log.Infof("%s scale-down requested (desired=%d < current=%d), stamping deadline for %s",
		stableScaleDownPolicyLogPrefix, desiredStableRSReplicaCount, currentReplicas, *delay)
	if err := c.addStableScaleDownDelay(c.stableRS, *delay); err != nil {
		return desiredStableRSReplicaCount, false, err
	}
	logutil.WithRollout(c.rollout).Info("rollout enqueue due to stableScaleDownPolicy")
	c.enqueueRolloutAfter(c.rollout, *delay)
	return currentReplicas, true, nil
}
