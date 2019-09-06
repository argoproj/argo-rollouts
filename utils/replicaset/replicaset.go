package replicaset

import (
	"fmt"
	"sort"
	"strconv"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	intstrutil "k8s.io/apimachinery/pkg/util/intstr"
	corev1defaults "k8s.io/kubernetes/pkg/apis/core/v1"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	v1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

// FindNewReplicaSet returns the new RS this given rollout targets from the given list.
// Returns nil if the ReplicaSet does not exist in the list.
func FindNewReplicaSet(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) *appsv1.ReplicaSet {
	var newRSList []*appsv1.ReplicaSet
	for _, rs := range rsList {
		if rs != nil {
			newRSList = append(newRSList, rs)
		}
	}
	rsList = newRSList
	sort.Sort(controller.ReplicaSetsByCreationTimestamp(rsList))
	// First, attempt to find the replicaset by the replicaset naming formula
	replicaSetName := fmt.Sprintf("%s-%s", rollout.Name, controller.ComputeHash(&rollout.Spec.Template, rollout.Status.CollisionCount))
	for _, rs := range rsList {
		if rs.Name == replicaSetName {
			return rs
		}
	}
	// Iterate the ReplicaSet list again, this time doing a deep equal against the template specs.
	// This covers the corner case in which the reason we did not find the replicaset, was because
	// of a change in the controller.ComputeHash function (e.g. due to an update of k8s libraries).
	// When this (rare) situation arises, we do not want to return nil, since nil is considered a
	// PodTemplate change, which in turn would triggers an unexpected redeploy of the replicaset.
	for _, rs := range rsList {
		if PodTemplateEqualIgnoreHash(&rs.Spec.Template, &rollout.Spec.Template) {
			logCtx := logutil.WithRollout(rollout)
			logCtx.Infof("ComputeHash change detected (expected: %s, actual: %s)", replicaSetName, rs.Name)
			return rs
		}
	}
	// new ReplicaSet does not exist.
	return nil
}

// FindOldReplicaSets returns the old replica sets targeted by the given Rollout, with the given slice of RSes.
func FindOldReplicaSets(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet) []*appsv1.ReplicaSet {
	var allRSs []*appsv1.ReplicaSet
	newRS := FindNewReplicaSet(rollout, rsList)
	for _, rs := range rsList {
		// Filter out new replica set
		if newRS != nil && rs.UID == newRS.UID {
			continue
		}
		allRSs = append(allRSs, rs)
	}
	return allRSs
}

// NewRSNewReplicas calculates the number of replicas a Rollout's new RS should have.
// When one of the followings is true, we're rolling out the deployment; otherwise, we're scaling it.
// 1) The new RS is saturated: newRS's replicas == deployment's replicas
// 2) Max number of pods allowed is reached: deployment's replicas + maxSurge == all RSs' replicas
func NewRSNewReplicas(rollout *v1alpha1.Rollout, allRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet) (int32, error) {
	if rollout.Spec.Strategy.BlueGreenStrategy != nil {
		if rollout.Spec.Strategy.BlueGreenStrategy.PreviewReplicaCount != nil {
			activeRS, _ := GetReplicaSetByTemplateHash(allRSs, rollout.Status.BlueGreen.ActiveSelector)
			if activeRS == nil || activeRS.Name == newRS.Name {
				return defaults.GetRolloutReplicasOrDefault(rollout), nil
			}
			if newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] != rollout.Status.CurrentPodHash {
				return *rollout.Spec.Strategy.BlueGreenStrategy.PreviewReplicaCount, nil
			}
			if !rollout.Spec.Paused && rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint {
				return defaults.GetRolloutReplicasOrDefault(rollout), nil
			}
			return *rollout.Spec.Strategy.BlueGreenStrategy.PreviewReplicaCount, nil
		}

		return defaults.GetRolloutReplicasOrDefault(rollout), nil
	}
	if rollout.Spec.Strategy.CanaryStrategy != nil {
		stableRS, olderRSs := GetStableRS(rollout, newRS, allRSs)
		newRSReplicaCount, _ := CalculateReplicaCountsForCanary(rollout, newRS, stableRS, olderRSs)
		return newRSReplicaCount, nil
	}
	return 0, fmt.Errorf("no rollout strategy provided")
}

// MaxRevision finds the highest revision in the replica sets
func MaxRevision(allRSs []*appsv1.ReplicaSet) int64 {
	max := int64(0)
	for _, rs := range allRSs {
		if v, err := Revision(rs); err != nil {
			// Skip the replica sets when it failed to parse their revision information
			log.WithError(err).Info("Couldn't parse revision, rollout controller will skip it when reconciling revisions.")
		} else if v > max {
			max = v
		}
	}
	return max
}

// Revision returns the revision number of the input object.
func Revision(obj runtime.Object) (int64, error) {
	acc, err := meta.Accessor(obj)
	if err != nil {
		return 0, err
	}
	v, ok := acc.GetAnnotations()[annotations.RevisionAnnotation]
	if !ok {
		return 0, nil
	}
	return strconv.ParseInt(v, 10, 64)
}

// FindActiveOrLatest returns the only active or the latest replica set in case there is at most one active
// replica set. If there are more active replica sets, then we should proportionally scale them.
func FindActiveOrLatest(newRS *appsv1.ReplicaSet, oldRSs []*appsv1.ReplicaSet) *appsv1.ReplicaSet {
	if newRS == nil && len(oldRSs) == 0 {
		return nil
	}

	sort.Sort(sort.Reverse(controller.ReplicaSetsByCreationTimestamp(oldRSs)))
	allRSs := controller.FilterActiveReplicaSets(append(oldRSs, newRS))

	switch len(allRSs) {
	case 0:
		// If there is no active replica set then we should return the newest.
		if newRS != nil {
			return newRS
		}
		return oldRSs[0]
	case 1:
		return allRSs[0]
	default:
		return nil
	}
}

// GetReplicaCountForReplicaSets returns the sum of Replicas of the given replica sets.
func GetReplicaCountForReplicaSets(replicaSets []*appsv1.ReplicaSet) int32 {
	totalReplicas := int32(0)
	for _, rs := range replicaSets {
		if rs != nil {
			totalReplicas += *(rs.Spec.Replicas)
		}
	}
	return totalReplicas
}

// GetAvailableReplicaCountForReplicaSets returns the number of available pods corresponding to the given replica sets.
func GetAvailableReplicaCountForReplicaSets(replicaSets []*appsv1.ReplicaSet) int32 {
	totalAvailableReplicas := int32(0)
	for _, rs := range replicaSets {
		if rs != nil {
			totalAvailableReplicas += rs.Status.AvailableReplicas
		}
	}
	return totalAvailableReplicas
}

// GetActualReplicaCountForReplicaSets returns the sum of actual replicas of the given replica sets.
func GetActualReplicaCountForReplicaSets(replicaSets []*appsv1.ReplicaSet) int32 {
	totalActualReplicas := int32(0)
	for _, rs := range replicaSets {
		if rs != nil {
			totalActualReplicas += rs.Status.Replicas
		}
	}
	return totalActualReplicas
}

// GetReadyReplicaCountForReplicaSets returns the number of ready pods corresponding to the given replica sets.
func GetReadyReplicaCountForReplicaSets(replicaSets []*appsv1.ReplicaSet) int32 {
	totalReadyReplicas := int32(0)
	for _, rs := range replicaSets {
		if rs != nil {
			totalReadyReplicas += rs.Status.ReadyReplicas
		}
	}
	return totalReadyReplicas
}

// ResolveFenceposts resolves both maxSurge and maxUnavailable. This needs to happen in one
// step. For example:
//
// 2 desired, max unavailable 1%, surge 0% - should scale old(-1), then new(+1), then old(-1), then new(+1)
// 1 desired, max unavailable 1%, surge 0% - should scale old(-1), then new(+1)
// 2 desired, max unavailable 25%, surge 1% - should scale new(+1), then old(-1), then new(+1), then old(-1)
// 1 desired, max unavailable 25%, surge 1% - should scale new(+1), then old(-1)
// 2 desired, max unavailable 0%, surge 1% - should scale new(+1), then old(-1), then new(+1), then old(-1)
// 1 desired, max unavailable 0%, surge 1% - should scale new(+1), then old(-1)
func resolveFenceposts(maxSurge, maxUnavailable *intstrutil.IntOrString, desired int32) (int32, int32, error) {
	surge, err := intstrutil.GetValueFromIntOrPercent(intstrutil.ValueOrDefault(maxSurge, intstrutil.FromInt(0)), int(desired), true)
	if err != nil {
		return 0, 0, err
	}
	unavailable, err := intstrutil.GetValueFromIntOrPercent(intstrutil.ValueOrDefault(maxUnavailable, intstrutil.FromInt(0)), int(desired), false)
	if err != nil {
		return 0, 0, err
	}

	if surge == 0 && unavailable == 0 {
		// Validation should never allow the user to explicitly use zero values for both maxSurge
		// maxUnavailable. Due to rounding down maxUnavailable though, it may resolve to zero.
		// If both fenceposts resolve to zero, then we should set maxUnavailable to 1 on the
		// theory that surge might not workf due to quota.
		unavailable = 1
	}

	return int32(surge), int32(unavailable), nil
}

// MaxUnavailable returns the maximum unavailable pods a rolling deployment can take.
func MaxUnavailable(rollout *v1alpha1.Rollout) int32 {
	rolloutReplicas := defaults.GetRolloutReplicasOrDefault(rollout)
	if rollout.Spec.Strategy.CanaryStrategy == nil || rolloutReplicas == 0 {
		return int32(0)
	}

	// Error caught by validation
	_, maxUnavailable, _ := resolveFenceposts(defaults.GetMaxSurgeOrDefault(rollout), defaults.GetMaxUnavailableOrDefault(rollout), rolloutReplicas)
	if maxUnavailable > rolloutReplicas {
		return rolloutReplicas
	}
	return maxUnavailable
}

// MaxSurge returns the maximum surge pods a rolling deployment can take.
func MaxSurge(rollout *v1alpha1.Rollout) int32 {
	rolloutReplicas := defaults.GetRolloutReplicasOrDefault(rollout)
	if rollout.Spec.Strategy.CanaryStrategy == nil {
		return int32(0)
	}
	// Error caught by validation
	maxSurge, _, _ := resolveFenceposts(defaults.GetMaxSurgeOrDefault(rollout), defaults.GetMaxUnavailableOrDefault(rollout), rolloutReplicas)
	return maxSurge
}

// checkStepHashChange indicates if the rollout's step for the strategy have changed. This causes the rollout to reset the
// currentStepIndex to zero. If there is no previous pod spec to compare to the function defaults to false
func checkStepHashChange(rollout *v1alpha1.Rollout) bool {
	if rollout.Status.CurrentStepHash == "" {
		return false
	}
	// TODO: conditions.ComputeStepHash is not stable and will change
	stepsHash := conditions.ComputeStepHash(rollout)
	if rollout.Status.CurrentStepHash != conditions.ComputeStepHash(rollout) {
		logCtx := logutil.WithRollout(rollout)
		logCtx.Infof("Canary steps change detected (new: %s, old: %s)", stepsHash, rollout.Status.CurrentStepHash)
		return true
	}
	return false
}

// checkPodSpecChange indicates if the rollout spec has changed indicating that the rollout needs to reset the
// currentStepIndex to zero. If there is no previous pod spec to compare to the function defaults to false
func checkPodSpecChange(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet) bool {
	if rollout.Status.CurrentPodHash == "" {
		return false
	}
	podHash := controller.ComputeHash(&rollout.Spec.Template, rollout.Status.CollisionCount)
	if newRS != nil {
		podHash = GetPodTemplateHash(newRS)
	}
	if rollout.Status.CurrentPodHash != podHash {
		logCtx := logutil.WithRollout(rollout)
		logCtx.Infof("Pod template change detected (new: %s, old: %s)", podHash, rollout.Status.CurrentPodHash)
		return true
	}
	return false
}

// PodTemplateOrStepsChanged detects if there is a change in either the pod template, or canary steps
func PodTemplateOrStepsChanged(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet) bool {
	if checkStepHashChange(rollout) {
		return true
	}
	if checkPodSpecChange(rollout, newRS) {
		return true
	}
	return false
}

// ResetCurrentStepIndex resets the index back to zero unless there are no steps
func ResetCurrentStepIndex(rollout *v1alpha1.Rollout) *int32 {
	if len(rollout.Spec.Strategy.CanaryStrategy.Steps) > 0 {
		return pointer.Int32Ptr(0)
	}
	return nil
}

// PodTemplateEqualIgnoreHash returns true if two given podTemplateSpec are equal, ignoring the diff in value of Labels[pod-template-hash]
// We ignore pod-template-hash because:
// 1. The hash result would be different upon podTemplateSpec API changes
//    (e.g. the addition of a new field will cause the hash code to change)
// 2. The deployment template won't have hash labels
//
// NOTE: This is a modified version of deploymentutil.EqualIgnoreHash, but modified to perform
// defaulting on the desired spec. This is so that defaulted fields by the replicaset controller
// factor into the comparison. The reason this is necessary, is because unlike the deployment
// controller, the rollout controller does not benefit/operate on a completely defaulted
// rollout object.
func PodTemplateEqualIgnoreHash(live, desired *corev1.PodTemplateSpec) bool {
	live = live.DeepCopy()
	desired = desired.DeepCopy()
	// Remove hash labels from template.Labels before comparing
	delete(live.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)
	delete(desired.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)

	podTemplate := corev1.PodTemplate{
		Template: *desired,
	}
	corev1defaults.SetObjectDefaults_PodTemplate(&podTemplate)
	desired = &podTemplate.Template
	return apiequality.Semantic.DeepEqual(live, desired)
}

// GetPodTemplateHash returns the rollouts-pod-template-hash value from a ReplicaSet's labels
func GetPodTemplateHash(rs *appsv1.ReplicaSet) string {
	if rs.Labels == nil {
		return ""
	}
	return rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
}

// ReplicaSetsByRevisionNumber sorts a list of ReplicaSet by revision timestamp, using their creation timestamp as a tie breaker.
type ReplicaSetsByRevisionNumber []*appsv1.ReplicaSet

func (o ReplicaSetsByRevisionNumber) Len() int      { return len(o) }
func (o ReplicaSetsByRevisionNumber) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o ReplicaSetsByRevisionNumber) Less(i, j int) bool {
	iRevision, iErr := strconv.Atoi(o[i].Annotations[annotations.RevisionAnnotation])
	jRevision, jErr := strconv.Atoi(o[j].Annotations[annotations.RevisionAnnotation])
	if iErr != nil && jErr != nil {
		return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
	}
	if iErr != nil {
		return i > j
	}
	if jErr != nil {
		return i < j

	}
	if iRevision == jRevision {
		return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
	}
	return iRevision < jRevision
}
