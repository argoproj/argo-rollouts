package rollout

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"math"
	"time"

	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"

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

	// Used to access old ephemeral data when updating pods below
	originalRSCopy := rs.DeepCopy()

	// Order of the following two steps is important to minimize race condition
	// First update replicasets, then pods owned by it.
	// So that any replicas created in the interim between the two steps are using the new updated version.
	// 1. Update ReplicaSet so that any new pods it creates will have the metadata
	if modified {
		_, err := c.patchReplicaSetEphemeralMetadata(ctx, rs, modifiedRS)
		if err != nil {
			c.log.Infof("failed to sync ephemeral metadata %v to ReplicaSet %s: %v", podMetadata, originalRSCopy.Name, err)
			return fmt.Errorf("failed to sync ephemeral metadata: %w", err)
		}
		c.log.Infof("synced ephemeral metadata %v to ReplicaSet %s", podMetadata, originalRSCopy.Name)
	}

	if isZeroReplicaReplicaSet(rs) {
		return nil
	}

	// 2. Sync ephemeral metadata to pods (always do this, even if replicaset wasn't modified so that we handle cases where
	// the replicaset already had the correct metadata but some pods don't: e.g. a crash/OOM of the controller between the two steps,
	// or simply a failure to update some pods in a previous attempt)
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

// errNotEphemeralMetadataChange is returned by replicaSetEphemeralMetadataPatch for a change that it cannot express.
var errNotEphemeralMetadataChange = stderrors.New("the change is not limited to the annotations of the ReplicaSet and the labels and annotations of its pod template")

// patchReplicaSetEphemeralMetadata applies the ephemeral metadata change that syncEphemeralMetadata computed: it
// changes the ReplicaSet from, the copy the change was computed for, into synced, and returns the ReplicaSet as the
// API server stored it.
//
// Like the scale (see patchReplicaSetScale), the change is sent as a JSON merge patch without a resourceVersion, so
// that it is not rejected when something else has written to the ReplicaSet since it was read. A ReplicaSet that
// loses its metadata is usually one the Rollout has moved away from while it still has its pods, so the ReplicaSet
// controller is updating its status, and the write comes after the pods of the new and the stable ReplicaSet have
// been listed. A full Update is then rejected on every reconcile, and it is rejected before anything is scaled
// (see #5227).
//
// The patch names only the keys that change: the ephemeral metadata annotation of the ReplicaSet, and the labels and
// annotations that are added to, changed in or removed from its pod template. Those are written by this controller
// only, and newRolloutContext does not start a reconcile before the informer has delivered our previous write to
// them (see staleReplicaSet). Everything else is left as it is on the server, including a scale that was sent
// earlier in the same reconcile and that from does not have yet.
//
// A change that the patch cannot express is sent as an Update, as it was before. There is none today: this is for a
// later change to what the ephemeral metadata sync modifies.
func (c *rolloutContext) patchReplicaSetEphemeralMetadata(ctx context.Context, from, synced *appsv1.ReplicaSet) (*appsv1.ReplicaSet, error) {
	patch, err := replicaSetEphemeralMetadataPatch(from, synced)
	if stderrors.Is(err, errNotEphemeralMetadataChange) {
		c.log.Warnf("Updating ReplicaSet '%s' instead of patching its ephemeral metadata: %v", from.Name, err)
		return c.updateReplicaSet(ctx, synced)
	}
	if err != nil {
		return nil, fmt.Errorf("error creating the ephemeral metadata patch for replicaset %s: %w", from.Name, err)
	}
	if patch == nil {
		return synced, nil
	}
	patchedRS, err := c.kubeclientset.AppsV1().ReplicaSets(from.Namespace).Patch(ctx, from.Name, patchtypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return nil, fmt.Errorf("error patching the ephemeral metadata of replicaset %s: %w", from.Name, err)
	}
	c.recordReplicaSetWrite(patchedRS)
	return patchedRS, nil
}

// replicaSetEphemeralMetadataPatch returns the JSON merge patch that changes the annotations of the ReplicaSet from,
// and the labels and annotations of its pod template, into those of synced, or nil if they are the same. If synced
// differs from from in anything else it returns errNotEphemeralMetadataChange, so that a change is never applied in
// part.
//
// Labels and annotations are patched key by key (see stringMapPatch). The UID is a precondition: the API server
// rejects the patch if the ReplicaSet was deleted and recreated under the same name.
func replicaSetEphemeralMetadataPatch(from, synced *appsv1.ReplicaSet) ([]byte, error) {
	// expected is from with the fields this patch carries taken from synced, so it equals synced unless something
	// else was changed.
	expected := from.DeepCopy()
	expected.Annotations = synced.Annotations
	expected.Spec.Template.Labels = synced.Spec.Template.Labels
	expected.Spec.Template.Annotations = synced.Spec.Template.Annotations
	if !apiequality.Semantic.DeepEqual(expected, synced) {
		return nil, errNotEphemeralMetadataChange
	}

	metadata := map[string]any{}
	if annotationsPatch := stringMapPatch(from.Annotations, synced.Annotations); annotationsPatch != nil {
		metadata["annotations"] = annotationsPatch
	}
	templateMetadata := map[string]any{}
	if labelsPatch := stringMapPatch(from.Spec.Template.Labels, synced.Spec.Template.Labels); labelsPatch != nil {
		templateMetadata["labels"] = labelsPatch
	}
	if annotationsPatch := stringMapPatch(from.Spec.Template.Annotations, synced.Spec.Template.Annotations); annotationsPatch != nil {
		templateMetadata["annotations"] = annotationsPatch
	}
	if len(metadata) == 0 && len(templateMetadata) == 0 {
		return nil, nil
	}

	if from.UID != "" {
		metadata["uid"] = string(from.UID)
	}
	patch := map[string]any{}
	if len(metadata) > 0 {
		patch["metadata"] = metadata
	}
	if len(templateMetadata) > 0 {
		patch["spec"] = map[string]any{"template": map[string]any{"metadata": templateMetadata}}
	}
	return json.Marshal(patch)
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
