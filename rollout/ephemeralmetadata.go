package rollout

import (
	"context"
	"fmt"
	"k8s.io/client-go/util/retry"

	"github.com/argoproj/argo-rollouts/utils/diff"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

// reconcileEphemeralMetadata syncs canary/stable ephemeral metadata to ReplicaSets and pods
func (c *rolloutContext) reconcileEphemeralMetadata() error {
	ctx := context.TODO()
	var newMetadata, stableMetadata *v1alpha1.PodTemplateMetadata
	if c.rollout.Spec.Strategy.Canary != nil {
		newMetadata = c.rollout.Spec.Strategy.Canary.CanaryMetadata
		stableMetadata = c.rollout.Spec.Strategy.Canary.StableMetadata
	} else if c.rollout.Spec.Strategy.BlueGreen != nil {
		newMetadata = c.rollout.Spec.Strategy.BlueGreen.PreviewMetadata
		stableMetadata = c.rollout.Spec.Strategy.BlueGreen.ActiveMetadata
	} else {
		return nil
	}
	fullyRolledOut := c.rollout.Status.StableRS == "" || c.rollout.Status.StableRS == replicasetutil.GetPodTemplateHash(c.newRS)

	if fullyRolledOut {
		// We are in a steady-state (fully rolled out). newRS is the stableRS. there is no longer a canary
		err := c.syncEphemeralMetadata(ctx, c.newRS, stableMetadata)
		if err != nil {
			return err
		}
	} else {
		// we are in a upgrading state. newRS is a canary
		err := c.syncEphemeralMetadata(ctx, c.newRS, newMetadata)
		if err != nil {
			return err
		}
		// sync stable metadata to the stable rs
		err = c.syncEphemeralMetadata(ctx, c.stableRS, stableMetadata)
		if err != nil {
			return err
		}
	}

	// Iterate all other ReplicaSets and verify we don't have injected metadata for them
	for _, rs := range c.otherRSs {
		err := c.syncEphemeralMetadata(ctx, rs, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *rolloutContext) syncEphemeralMetadata(ctx context.Context, rs *appsv1.ReplicaSet, podMetadata *v1alpha1.PodTemplateMetadata) error {
	if rs == nil {
		return nil
	}
	modifiedRS, modified := replicasetutil.SyncReplicaSetEphemeralPodMetadata(rs, podMetadata)
	if !modified {
		return nil
	}
	// 1. Sync ephemeral metadata to pods
	pods, err := replicasetutil.GetPodsOwnedByReplicaSet(ctx, c.kubeclientset, rs)
	if err != nil {
		return err
	}
	existingPodMetadata := replicasetutil.ParseExistingPodMetadata(rs)
	for _, pod := range pods {
		newPodObjectMeta, podModified := replicasetutil.SyncEphemeralPodMetadata(&pod.ObjectMeta, existingPodMetadata, podMetadata)
		if podModified {
			pod.ObjectMeta = *newPodObjectMeta
			_, err = c.kubeclientset.CoreV1().Pods(pod.Namespace).Update(ctx, pod, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
			c.log.Infof("synced ephemeral metadata %v to Pod %s", podMetadata, pod.Name)
		}
	}

	// 2. Update ReplicaSet so that any new pods it creates will have the metadata
	rs, err = c.kubeclientset.AppsV1().ReplicaSets(modifiedRS.Namespace).Update(ctx, modifiedRS, metav1.UpdateOptions{})
	if err != nil {
		if errors.IsConflict(err) {
			// Retry with a merge patch
			errConflict := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				rs, err = c.kubeclientset.AppsV1().ReplicaSets(modifiedRS.Namespace).Get(ctx, modifiedRS.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				rs.ObjectMeta.ResourceVersion = ""
				rs.ObjectMeta.ResourceVersion = ""
				rs.ObjectMeta.ManagedFields = nil
				rs.ObjectMeta.ManagedFields = nil
				patch, changed, err := diff.CreateTwoWayMergePatch(rs, modifiedRS, appsv1.ReplicaSet{})
				if err != nil {
					return err
				}
				if changed {
					rs, err = c.kubeclientset.AppsV1().ReplicaSets(modifiedRS.Namespace).Patch(ctx, modifiedRS.Name, types.MergePatchType, patch, metav1.PatchOptions{})
					if err != nil {
						return err
					}
				}
				return nil
			})
			if errConflict != nil {
				return fmt.Errorf("error updating replicaset in syncEphemeralMetadata in RetryOnConflict: %w", errConflict)
			}
		}
		return fmt.Errorf("error updating replicaset in syncEphemeralMetadata: %w", err)
	}
	err = c.replicaSetInformer.GetIndexer().Update(rs)
	if err != nil {
		return fmt.Errorf("error updating replicaset informer in syncEphemeralMetadata: %w", err)
	}
	c.log.Infof("synced ephemeral metadata %v to ReplicaSet %s", podMetadata, rs.Name)
	return nil
}
