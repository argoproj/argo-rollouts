package replicaset

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	intstrutil "k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	corev1defaults "k8s.io/kubernetes/pkg/apis/core/v1"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/hash"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
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
	// First, attempt to find the ReplicaSet by pod-template-hash label. The label was computed
	// from the same desired template by this controller, so a match is authoritative even if the
	// live template has since diverged from the desired one (e.g. apiserver defaulting of fields
	// unknown to our vendored Kubernetes libraries, or mutating admission on ReplicaSets).
	podHash := hash.ComputePodTemplateHash(&rollout.Spec.Template, rollout.Status.CollisionCount)
	if rs := searchRsByHash(rsList, podHash); rs != nil {
		return rs
	}
	// Otherwise, fall back to semantic pod template equality, like the upstream Deployment
	// controller. This covers ReplicaSets whose hash label was written by a different hashing
	// algorithm (e.g. after an upgrade of the controller or its Kubernetes libraries); adopting
	// them avoids an unwarranted redeploy.
	//
	// Prefer the ReplicaSet recorded in status.currentPodHash when it still matches the desired
	// template. After a hashing-algorithm change, several retained ReplicaSets can carry the same
	// template (e.g. the revision spuriously created by a controller version whose hash had
	// changed); preferring the rollout's own record of its current ReplicaSet keeps the upgrade a
	// no-op instead of resurrecting the oldest twin. On a genuine template change the current
	// ReplicaSet no longer matches, so this is a no-op and the oldest matching ReplicaSet is
	// reused, like the upstream Deployment controller.
	if rollout.Status.CurrentPodHash != "" && rollout.Status.CurrentPodHash != podHash {
		if rs := searchRsByHash(rsList, rollout.Status.CurrentPodHash); rs != nil && ReplicaSetTemplateMatchesRollout(rollout, rs) {
			logCtx := logutil.WithRollout(rollout)
			logCtx.Infof("Adopted ReplicaSet %s recorded in status.currentPodHash by pod template equality (hash label: %s, computed hash: %s)", rs.Name, rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], podHash)
			return rs
		}
	}
	for _, rs := range rsList {
		if ReplicaSetTemplateMatchesRollout(rollout, rs) {
			logCtx := logutil.WithRollout(rollout)
			logCtx.Infof("Adopted ReplicaSet %s by pod template equality (hash label: %s, computed hash: %s)", rs.Name, rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], podHash)
			return rs
		}
	}
	return nil
}

func searchRsByHash(rsList []*appsv1.ReplicaSet, hash string) *appsv1.ReplicaSet {
	for _, rs := range rsList {
		if rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] == hash {
			return rs
		}
	}
	return nil
}

// ReplicaSetTemplateMatchesRollout returns whether the live ReplicaSet's pod template is
// semantically equal to the rollout's desired pod template, after stripping the fields this
// controller injects into ReplicaSet templates (ephemeral canary/stable metadata and the
// injected anti-affinity rule) from the live template.
func ReplicaSetTemplateMatchesRollout(rollout *v1alpha1.Rollout, rs *appsv1.ReplicaSet) bool {
	// Remove injected canary/stable metadata from spec.template.metadata before comparing
	rsCopy, _ := SyncReplicaSetEphemeralPodMetadata(rs, nil)
	// Remove anti-affinity from template.Spec.Affinity before comparing
	live := &rsCopy.Spec.Template
	live.Spec.Affinity = RemoveInjectedAntiAffinityRule(live.Spec.Affinity, *rollout)
	return PodTemplateEqualIgnoreHash(live, &rollout.Spec.Template)
}

func GetRolloutAffinity(rollout v1alpha1.Rollout) *v1alpha1.AntiAffinity {
	var antiAffinityStrategy *v1alpha1.AntiAffinity
	if rollout.Spec.Strategy.BlueGreen != nil && rollout.Spec.Strategy.BlueGreen.AntiAffinity != nil {
		antiAffinityStrategy = rollout.Spec.Strategy.BlueGreen.AntiAffinity
	}
	if rollout.Spec.Strategy.Canary != nil && rollout.Spec.Strategy.Canary.AntiAffinity != nil {
		antiAffinityStrategy = rollout.Spec.Strategy.Canary.AntiAffinity
	}
	if antiAffinityStrategy != nil {
		if antiAffinityStrategy.PreferredDuringSchedulingIgnoredDuringExecution != nil || antiAffinityStrategy.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			return antiAffinityStrategy
		}
	}
	return nil
}

// GenerateReplicaSetAffinity injects an anti-affinity rule (against the stable ReplicaSet) into the
// rollout's desired pod affinity while a rollout is in progress, i.e. when the ReplicaSet identified
// by podHash is not the stable one. podHash is the pod-template-hash label of the ReplicaSet the
// affinity is being generated for (the new RS being created, or the existing RS being updated). It is
// compared against status.StableRS — both are stored label values of the same provenance — so the
// decision is stable across a hashing-algorithm change and is not skewed by recomputing a hash that
// may differ in format from the stored stable hash.
func GenerateReplicaSetAffinity(rollout v1alpha1.Rollout, podHash string) *corev1.Affinity {
	antiAffinityStrategy := GetRolloutAffinity(rollout)
	affinitySpec := rollout.Spec.Template.Spec.Affinity.DeepCopy()
	if antiAffinityStrategy != nil && rollout.Status.StableRS != "" && rollout.Status.StableRS != podHash {
		antiAffinityRule := CreateInjectedAntiAffinityRule(rollout)
		if affinitySpec == nil {
			affinitySpec = &corev1.Affinity{}
		}
		if affinitySpec.PodAntiAffinity == nil {
			affinitySpec.PodAntiAffinity = &corev1.PodAntiAffinity{}
		}
		podAntiAffinitySpec := affinitySpec.PodAntiAffinity
		if antiAffinityStrategy.PreferredDuringSchedulingIgnoredDuringExecution != nil {
			weightedPodAffinityTerm := corev1.WeightedPodAffinityTerm{
				Weight:          antiAffinityStrategy.PreferredDuringSchedulingIgnoredDuringExecution.Weight,
				PodAffinityTerm: antiAffinityRule,
			}
			if podAntiAffinitySpec.PreferredDuringSchedulingIgnoredDuringExecution == nil {
				podAntiAffinitySpec.PreferredDuringSchedulingIgnoredDuringExecution = []corev1.WeightedPodAffinityTerm{weightedPodAffinityTerm}
			} else {
				podAntiAffinitySpec.PreferredDuringSchedulingIgnoredDuringExecution = append(podAntiAffinitySpec.PreferredDuringSchedulingIgnoredDuringExecution, weightedPodAffinityTerm)
			}
		} else {
			if podAntiAffinitySpec.RequiredDuringSchedulingIgnoredDuringExecution == nil {
				podAntiAffinitySpec.RequiredDuringSchedulingIgnoredDuringExecution = []corev1.PodAffinityTerm{antiAffinityRule}
			} else {
				podAntiAffinitySpec.RequiredDuringSchedulingIgnoredDuringExecution = append(podAntiAffinitySpec.RequiredDuringSchedulingIgnoredDuringExecution, antiAffinityRule)
			}
		}
	}
	return affinitySpec
}

func CreateInjectedAntiAffinityRule(rollout v1alpha1.Rollout) corev1.PodAffinityTerm {
	// Create anti-affinity rule for last stable rollout
	antiAffinityRule := corev1.PodAffinityTerm{
		LabelSelector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key:      v1alpha1.DefaultRolloutUniqueLabelKey,
				Operator: metav1.LabelSelectorOpIn,
				// Most recent stable ReplicaSet
				Values: []string{rollout.Status.StableRS},
			}},
		},
		Namespaces:  []string{rollout.Namespace},
		TopologyKey: "kubernetes.io/hostname",
	}
	return antiAffinityRule
}

func HasInjectedAntiAffinityRule(affinity *corev1.Affinity, rollout v1alpha1.Rollout) (int, *corev1.PodAffinityTerm) {
	antiAffinityStrategy := GetRolloutAffinity(rollout)
	if antiAffinityStrategy != nil && affinity != nil && affinity.PodAntiAffinity != nil {
		podAntiAffinitySpec := affinity.PodAntiAffinity
		if antiAffinityStrategy.PreferredDuringSchedulingIgnoredDuringExecution != nil {
			for i := range podAntiAffinitySpec.PreferredDuringSchedulingIgnoredDuringExecution {
				podAffinityTerm := podAntiAffinitySpec.PreferredDuringSchedulingIgnoredDuringExecution[i].PodAffinityTerm
				for _, labelSelectorRequirement := range podAffinityTerm.LabelSelector.MatchExpressions {
					if labelSelectorRequirement.Key == v1alpha1.DefaultRolloutUniqueLabelKey {
						return i, &podAffinityTerm
					}
				}

			}
		} else {
			for i := range podAntiAffinitySpec.RequiredDuringSchedulingIgnoredDuringExecution {
				podAffinityTerm := podAntiAffinitySpec.RequiredDuringSchedulingIgnoredDuringExecution[i]
				for _, labelSelectorRequirement := range podAffinityTerm.LabelSelector.MatchExpressions {
					if labelSelectorRequirement.Key == v1alpha1.DefaultRolloutUniqueLabelKey {
						return i, &podAffinityTerm
					}
				}
			}
		}

	}
	return -1, nil
}

func RemoveInjectedAntiAffinityRule(affinity *corev1.Affinity, rollout v1alpha1.Rollout) *corev1.Affinity {
	i, _ := HasInjectedAntiAffinityRule(affinity, rollout)
	affinitySpec := affinity.DeepCopy()
	if i >= 0 {
		antiAffinityStrategy := GetRolloutAffinity(rollout)
		if antiAffinityStrategy.PreferredDuringSchedulingIgnoredDuringExecution != nil {
			antiAffinityTerms := affinitySpec.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			affinitySpec.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(antiAffinityTerms[:i], antiAffinityTerms[i+1:]...)
		}
		if antiAffinityStrategy.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			antiAffinityTerms := affinitySpec.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution
			affinitySpec.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution = append(antiAffinityTerms[:i], antiAffinityTerms[i+1:]...)
		}
		if len(affinitySpec.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) == 0 {
			affinitySpec.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution = nil
		}
		if affinitySpec.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil && affinitySpec.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution == nil {
			affinitySpec.PodAntiAffinity = nil
		}
		if affinitySpec.PodAntiAffinity == nil && affinitySpec.PodAffinity == nil && affinitySpec.NodeAffinity == nil {
			affinitySpec = nil
		}
	}
	return affinitySpec
}

// IfInjectedAntiAffinityRuleNeedsUpdate reports whether the injected anti-affinity rule on the
// ReplicaSet identified by podHash points at a stale stable ReplicaSet and therefore needs updating.
// podHash is the ReplicaSet's pod-template-hash label, compared against status.StableRS (see
// GenerateReplicaSetAffinity for why a stored label is used instead of a freshly computed hash).
func IfInjectedAntiAffinityRuleNeedsUpdate(affinity *corev1.Affinity, rollout v1alpha1.Rollout, podHash string) bool {
	_, podAffinityTerm := HasInjectedAntiAffinityRule(affinity, rollout)
	if podAffinityTerm != nil && rollout.Status.StableRS != podHash {
		for _, labelSelectorRequirement := range podAffinityTerm.LabelSelector.MatchExpressions {
			if labelSelectorRequirement.Key == v1alpha1.DefaultRolloutUniqueLabelKey && labelSelectorRequirement.Values[0] != rollout.Status.StableRS {
				return true
			}
		}
	}
	return false
}

func NeedsRestart(rollout *v1alpha1.Rollout) bool {
	now := timeutil.MetaNow().UTC()
	if rollout.Spec.RestartAt == nil {
		return false
	}
	if rollout.Status.RestartedAt != nil && rollout.Spec.RestartAt.Equal(rollout.Status.RestartedAt) {
		return false
	}
	return now.After(rollout.Spec.RestartAt.Time)
}

// FindOldReplicaSets returns the old replica sets targeted by the given Rollout, with the given slice of RSes.
func FindOldReplicaSets(rollout *v1alpha1.Rollout, rsList []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet) []*appsv1.ReplicaSet {
	var allRSs []*appsv1.ReplicaSet
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
func NewRSNewReplicas(rollout *v1alpha1.Rollout, allRSs []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet, weights *v1alpha1.TrafficWeights) (int32, error) {
	if rollout.Spec.Strategy.BlueGreen != nil {
		desiredReplicas := defaults.GetReplicasOrDefault(rollout.Spec.Replicas)
		if rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount != nil {
			activeRS, _ := GetReplicaSetByTemplateHash(allRSs, rollout.Status.BlueGreen.ActiveSelector)
			if activeRS == nil || activeRS.Name == newRS.Name {
				// the active RS is our desired RS. we are already past the blue-green promote step
				return desiredReplicas, nil
			}
			if rollout.Status.PromoteFull {
				// we are doing a full promotion. ignore previewReplicaCount
				return desiredReplicas, nil
			}
			if newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] != rollout.Status.CurrentPodHash {
				// the desired RS is not equal to our previously recorded current RS.
				// This must be a new update, so return previewReplicaCount
				return *rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount, nil
			}
			isNotPaused := !rollout.Spec.Paused && len(rollout.Status.PauseConditions) == 0
			if isNotPaused && rollout.Status.BlueGreen.ScaleUpPreviewCheckPoint {
				// We are not paused, but we are already past our preview scale up checkpoint.
				// If we get here, we were resumed after the pause, but haven't yet flipped the
				// active service switch to the desired RS.
				return desiredReplicas, nil
			}
			return *rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount, nil
		}
		return desiredReplicas, nil
	}
	if rollout.Spec.Strategy.Canary != nil {
		stableRS := GetStableRS(rollout, newRS, allRSs)
		var newRSReplicaCount int32
		if rollout.Spec.Strategy.Canary.TrafficRouting == nil {
			otherRSs := GetOtherRSs(rollout, newRS, stableRS, allRSs)
			newRSReplicaCount, _ = CalculateReplicaCountsForBasicCanary(rollout, newRS, stableRS, otherRSs)
		} else {
			newRSReplicaCount, _ = CalculateReplicaCountsForTrafficRoutedCanary(rollout, newRS, stableRS, weights)
		}
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

// IsActive returns if replica set is active (has, or at least ought to have pods).
func IsActive(rs *appsv1.ReplicaSet) bool {
	if rs == nil {
		return false
	}

	return len(controller.FilterActiveReplicaSets([]*appsv1.ReplicaSet{rs})) > 0
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
	rolloutReplicas := defaults.GetReplicasOrDefault(rollout.Spec.Replicas)
	if rolloutReplicas == 0 {
		return int32(0)
	}

	// Error caught by validation
	var maxUnavailable int32
	if rollout.Spec.Strategy.Canary != nil {
		_, maxUnavailable, _ = resolveFenceposts(defaults.GetMaxSurgeOrDefault(rollout), defaults.GetMaxUnavailableOrDefault(rollout), rolloutReplicas)
	} else {
		unavailable, _ := intstrutil.GetValueFromIntOrPercent(intstrutil.ValueOrDefault(defaults.GetMaxUnavailableOrDefault(rollout), intstrutil.FromInt(0)), int(rolloutReplicas), false)
		maxUnavailable = int32(unavailable)
	}

	if maxUnavailable > rolloutReplicas {
		return rolloutReplicas
	}
	return maxUnavailable
}

// MaxSurge returns the maximum surge pods a rolling deployment can take.
func MaxSurge(rollout *v1alpha1.Rollout) int32 {
	rolloutReplicas := defaults.GetReplicasOrDefault(rollout.Spec.Replicas)
	if rollout.Spec.Strategy.Canary == nil {
		return int32(0)
	}
	// Error caught by validation
	maxSurge, _, _ := resolveFenceposts(defaults.GetMaxSurgeOrDefault(rollout), defaults.GetMaxUnavailableOrDefault(rollout), rolloutReplicas)
	return maxSurge
}

// checkStepHashChange indicates if the rollout's step for the strategy have changed. This causes the rollout to reset the
// currentStepIndex to zero. If there was no previously recorded step hash to compare to the function defaults to true
func checkStepHashChange(rollout *v1alpha1.Rollout) bool {
	if rollout.Status.CurrentStepHash == "" {
		return true
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

// CheckPodSpecChange indicates if the rollout spec has changed indicating that the rollout needs to reset the
// currentStepIndex to zero. If there is no previous pod spec to compare to the function defaults to false.
//
// When newRS is non-nil it was adopted by FindNewReplicaSet via semantic template equality (like the
// upstream Deployment controller), so we compare status.currentPodHash against the adopted ReplicaSet's
// existing pod-template-hash label. This keeps detection stable across a hashing-algorithm change: an
// adopted ReplicaSet retains its original label, so an upgrade alone is not seen as a pod spec change.
// When newRS is nil we fall back to comparing against the freshly computed hash of the desired template.
func CheckPodSpecChange(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet) bool {
	if rollout.Status.CurrentPodHash == "" {
		return false
	}
	podHash := hash.ComputePodTemplateHash(&rollout.Spec.Template, rollout.Status.CollisionCount)
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
	if CheckPodSpecChange(rollout, newRS) {
		return true
	}
	return false
}

// ResetCurrentStepIndex resets the index back to zero unless there are no steps
func ResetCurrentStepIndex(rollout *v1alpha1.Rollout) *int32 {
	if rollout.Spec.Strategy.Canary != nil && len(rollout.Spec.Strategy.Canary.Steps) > 0 {
		return ptr.To[int32](0)
	}
	return nil
}

// PodTemplateEqualIgnoreHash returns true if two given podTemplateSpec are equal, ignoring the diff in value of Labels[pod-template-hash]
// We ignore pod-template-hash because:
//  1. The hash result would be different upon podTemplateSpec API changes
//     (e.g. the addition of a new field will cause the hash code to change)
//  2. The deployment template won't have hash labels
//
// NOTE: This is a modified version of deploymentutil.EqualIgnoreHash. Both templates are run
// through the current library's defaulting before comparison. Unlike the deployment controller,
// the rollout controller does not operate on a completely defaulted rollout object, so we
// default both sides to keep comparisons stable across Kubernetes library upgrades.
func PodTemplateEqualIgnoreHash(live, desired *corev1.PodTemplateSpec) bool {
	live = defaultPodTemplateSpec(live)
	desired = defaultPodTemplateSpec(desired)
	// Remove hash labels from template.Labels before comparing
	delete(live.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)
	delete(desired.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)

	// Do not allow the deprecated spec.serviceAccount to factor into the equality check. In live
	// ReplicaSet pod template, this field will be populated, but in the desired pod template
	// it will be missing (even after defaulting), causing us to believe there is a diff
	// (when there really wasn't), and hence causing an unsolicited update to be triggered.
	// See: https://github.com/argoproj/argo-rollouts/issues/1356
	desired.Spec.DeprecatedServiceAccount = ""
	live.Spec.DeprecatedServiceAccount = ""

	return apiequality.Semantic.DeepEqual(live, desired)
}

func defaultPodTemplateSpec(template *corev1.PodTemplateSpec) *corev1.PodTemplateSpec {
	podTemplate := corev1.PodTemplate{
		Template: *template.DeepCopy(),
	}
	corev1defaults.SetObjectDefaults_PodTemplate(&podTemplate)
	return &podTemplate.Template
}

// GetPodTemplateHash returns the rollouts-pod-template-hash value from a ReplicaSet's labels
func GetPodTemplateHash(rs *appsv1.ReplicaSet) string {
	if rs == nil || rs.Labels == nil {
		return ""
	}
	return rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
}

func GetReplicaSetRevision(ro *v1alpha1.Rollout, rs *appsv1.ReplicaSet) int {
	logCtx := logutil.WithRollout(ro).WithField("ReplicaSet", rs.Name)
	revisionStr, ok := rs.Annotations[annotations.RevisionAnnotation]
	if !ok {
		logCtx.Warn("ReplicaSet has no revision")
		return -1
	}
	revision, err := strconv.Atoi(revisionStr)
	if err != nil {
		logCtx.Warnf("Unable to convert ReplicaSet revision to int: %s", err.Error())
		return -1
	}
	return revision
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

// HasScaleDownDeadline returns whether or not the given ReplicaSet is annotated with a scale-down delay
func HasScaleDownDeadline(rs *appsv1.ReplicaSet) bool {
	if rs == nil || rs.Annotations == nil {
		return false
	}
	return rs.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] != ""
}

func GetTimeRemainingBeforeScaleDownDeadline(rs *appsv1.ReplicaSet) (*time.Duration, error) {
	if HasScaleDownDeadline(rs) {
		scaleDownAtStr := rs.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]
		scaleDownAtTime, err := time.Parse(time.RFC3339, scaleDownAtStr)
		if err != nil {
			return nil, fmt.Errorf("unable to read scaleDownAt label on rs '%s'", rs.Name)
		}
		now := timeutil.MetaNow()
		scaleDownAt := metav1.NewTime(scaleDownAtTime)
		if scaleDownAt.After(now.Time) {
			remainingTime := scaleDownAt.Sub(now.Time)
			return &remainingTime, nil
		}
	}
	return nil, nil
}

// GetPodsOwnedByReplicaSet returns a list of pods owned by the given replicaset
func GetPodsOwnedByReplicaSet(ctx context.Context, client kubernetes.Interface, rs *appsv1.ReplicaSet) ([]*corev1.Pod, error) {
	pods, err := client.CoreV1().Pods(rs.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(rs.Spec.Selector),
	})
	if err != nil {
		return nil, err
	}
	var podOwnedByRS []*corev1.Pod
	for i := range pods.Items {
		pod := pods.Items[i]
		if metav1.IsControlledBy(&pod, rs) {
			podOwnedByRS = append(podOwnedByRS, &pod)
		}
	}
	return podOwnedByRS, nil
}

// IsReplicaSetAvailable returns if a ReplicaSet is scaled up and its ready count is >= desired count
func IsReplicaSetAvailable(rs *appsv1.ReplicaSet) bool {
	if rs == nil {
		return false
	}
	replicas := rs.Spec.Replicas
	availableReplicas := rs.Status.AvailableReplicas
	return replicas != nil && *replicas != 0 && availableReplicas != 0 && *replicas <= availableReplicas
}

// IsReplicaSetPartiallyAvailable returns if a ReplicaSet is scaled up and has at least 1 pod available
func IsReplicaSetPartiallyAvailable(rs *appsv1.ReplicaSet) bool {
	return rs.Status.AvailableReplicas > 0
}
