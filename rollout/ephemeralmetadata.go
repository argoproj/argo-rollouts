package rollout

import (
	"context"
	"fmt"
	"math"
	"time"

	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

// DefaultEphemeralMetadataThreads is the default number of worker threads to run when reconciling ephemeral metadata
const DefaultEphemeralMetadataThreads = 10

// DefaultEphemeralMetadataPodRetries is the default number of retries when attempting to update pod ephemeral metadata
const DefaultEphemeralMetadataPodRetries = 3

// DefaultEphemeralMetadataRetryBackoff is the base duration for exponential backoff between retry attempts
const DefaultEphemeralMetadataRetryBackoff = 100 * time.Millisecond

func hasEphemeralMetadataConfigured(rollout *v1alpha1.Rollout) bool {
	if rollout.Spec.Strategy.Canary != nil {
		return rollout.Spec.Strategy.Canary.CanaryMetadata != nil || rollout.Spec.Strategy.Canary.StableMetadata != nil
	}
	if rollout.Spec.Strategy.BlueGreen != nil {
		return rollout.Spec.Strategy.BlueGreen.PreviewMetadata != nil || rollout.Spec.Strategy.BlueGreen.ActiveMetadata != nil
	}
	return false
}

func isZeroReplicaReplicaSet(rs *appsv1.ReplicaSet) bool {
	if rs == nil {
		return true
	}
	if rs.Spec.Replicas != nil && *rs.Spec.Replicas == 0 {
		return true
	}
	return false
}

// reconcileEphemeralMetadata syncs canary/stable ephemeral metadata to ReplicaSets and pods
func (c *rolloutContext) reconcileEphemeralMetadata() error {
	if !hasEphemeralMetadataConfigured(c.rollout) {
		return nil
	}

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

	// Iterate all other ReplicaSets and verify we don't have injected metadata for them.
	// Only the RS template needs cleanup; active pods live on newRS/stableRS.
	for _, rs := range c.otherRSs {
		err := c.syncEphemeralMetadataReplicaSetOnly(ctx, rs, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func replicaSetNeedsPodMetadataSync(modified bool, rs *appsv1.ReplicaSet) bool {
	if modified {
		return true
	}
	return replicasetutil.ParseExistingPodMetadata(rs) != nil
}

func (c *rolloutContext) syncEphemeralMetadata(ctx context.Context, rs *appsv1.ReplicaSet, podMetadata *v1alpha1.PodTemplateMetadata) error {
	return c.syncEphemeralMetadataInternal(ctx, rs, podMetadata, true)
}

func (c *rolloutContext) syncEphemeralMetadataReplicaSetOnly(ctx context.Context, rs *appsv1.ReplicaSet, podMetadata *v1alpha1.PodTemplateMetadata) error {
	return c.syncEphemeralMetadataInternal(ctx, rs, podMetadata, false)
}

func (c *rolloutContext) syncEphemeralMetadataInternal(ctx context.Context, rs *appsv1.ReplicaSet, podMetadata *v1alpha1.PodTemplateMetadata, syncPods bool) error {
	if rs == nil {
		return nil
	}
	modifiedRS, modified := replicasetutil.SyncReplicaSetEphemeralPodMetadata(rs, podMetadata)

	// Used to access old ephemeral data when updating pods below
	originalRSCopy := rs.DeepCopy()

	// Order of the following two steps is important to minimize race condition
	// First update replicasets, then pods owned by it.
	// So that any replicas created in the interim between the two steps are using the new updated version.
	// 1. Update ReplicaSet so that any new pods it creates will have the metadata
	if modified {
		rs, err := c.updateReplicaSet(ctx, modifiedRS)
		if err != nil {
			c.log.Infof("failed to sync ephemeral metadata %v to ReplicaSet %s: %v", podMetadata, originalRSCopy.Name, err)
			return fmt.Errorf("failed to sync ephemeral metadata: %w", err)
		}
		c.log.Infof("synced ephemeral metadata %v to ReplicaSet %s", podMetadata, rs.Name)
	}

	if !syncPods {
		return nil
	}

	if isZeroReplicaReplicaSet(rs) {
		return nil
	}

	if !replicaSetNeedsPodMetadataSync(modified, originalRSCopy) {
		return nil
	}

	// 2. Sync ephemeral metadata to pods when the ReplicaSet was updated or previously had
	// injected metadata (handles crash/OOM between RS and pod updates, or partial pod sync).
	pods, err := replicasetutil.GetPodsOwnedByReplicaSet(ctx, c.kubeclientset, rs)
	if err != nil {
		return err
	}
	existingPodMetadata := replicasetutil.ParseExistingPodMetadata(originalRSCopy)

	var eg errgroup.Group
	eg.SetLimit(c.ephemeralMetadataThreads)

	for _, pod := range pods {
		eg.Go(func() error {
			return c.updatePodMetadataWithRetry(ctx, pod, existingPodMetadata, podMetadata)
		})
	}

	return eg.Wait()
}

// updatePodMetadataWithRetry attempts to update a pod's ephemeral metadata with exponential backoff retry
func (c *rolloutContext) updatePodMetadataWithRetry(ctx context.Context, pod *corev1.Pod, existingPodMetadata, podMetadata *v1alpha1.PodTemplateMetadata) error {
	fetchedPod := pod.DeepCopy()
	var lastUpdateErr error

	for attempt := 0; attempt < c.ephemeralMetadataPodRetries; attempt++ {
		// Add exponential backoff for retries (except first attempt)
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * DefaultEphemeralMetadataRetryBackoff
			time.Sleep(backoff)
		}

		newPodObjectMeta, podModified := replicasetutil.SyncEphemeralPodMetadata(&fetchedPod.ObjectMeta, existingPodMetadata, podMetadata)
		if !podModified {
			// No changes needed, exit successfully
			return nil
		}

		fetchedPod.ObjectMeta = *newPodObjectMeta
		_, lastUpdateErr = c.kubeclientset.CoreV1().Pods(fetchedPod.Namespace).Update(ctx, fetchedPod, metav1.UpdateOptions{})
		if lastUpdateErr == nil {
			c.log.Infof("synced ephemeral metadata %v to Pod %s", podMetadata, fetchedPod.Name)
			return nil
		}

		if errors.IsNotFound(lastUpdateErr) {
			c.log.Infof("Skipping sync ephemeral metadata %v to Pod %s: as it no longer exists", podMetadata, fetchedPod.Name)
			return nil
		}

		// If there is a mismatch of versions between the live pod object
		// and sent in the update call then we refetch the pod Object
		if errors.IsConflict(lastUpdateErr) {
			refetchedPod, err := c.kubeclientset.CoreV1().Pods(fetchedPod.Namespace).Get(ctx, fetchedPod.Name, metav1.GetOptions{})
			if err != nil {
				c.log.Infof("failed to refetch pod %s during retry %d: %v", fetchedPod.Name, attempt, err)
			} else {
				fetchedPod = refetchedPod
			}
		}

		c.log.Infof("failed to sync ephemeral metadata %v to Pod %s: %v, in retry attempt %d of %d", podMetadata, fetchedPod.Name, lastUpdateErr, attempt+1, c.ephemeralMetadataPodRetries)
	}

	// If we've exhausted all retries, log final failure and return error
	c.log.Warnf("exhausted all %d retries to sync ephemeral metadata %v to Pod %s", c.ephemeralMetadataPodRetries, podMetadata, fetchedPod.Name)
	return lastUpdateErr
}
