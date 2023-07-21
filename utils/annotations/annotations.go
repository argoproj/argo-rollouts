package annotations

import (
	"fmt"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const (
	// RolloutLabel key value for the label in the annotations and selector
	RolloutLabel = "rollout.argoproj.io"
	// RevisionAnnotation is the revision annotation of a rollout's replica sets which records its rollout sequence
	RevisionAnnotation = RolloutLabel + "/revision"
	// RevisionHistoryAnnotation maintains the history of all old revisions that a replica set has served for a rollout.
	RevisionHistoryAnnotation = RolloutLabel + "/revision-history"
	// DesiredReplicasAnnotation is the desired replicas for a rollout recorded as an annotation
	// in its replica sets. Helps in separating scaling events from the rollout process and for
	// determining if the new replica set for a rollout is really saturated.
	DesiredReplicasAnnotation = RolloutLabel + "/desired-replicas"
	// WorkloadGenerationAnnotation is the generation of the referenced workload
	WorkloadGenerationAnnotation = RolloutLabel + "/workload-generation"
)

// GetDesiredReplicasAnnotation returns the number of desired replicas
func GetDesiredReplicasAnnotation(rs *appsv1.ReplicaSet) (int32, bool) {
	return getIntFromAnnotation(rs, DesiredReplicasAnnotation)
}

// GetWorkloadGenerationAnnotation returns generation of referenced workload
func GetWorkloadGenerationAnnotation(ro *v1alpha1.Rollout) (int32, bool) {
	if ro == nil {
		return 0, false
	}
	annotationValue, ok := ro.Annotations[WorkloadGenerationAnnotation]
	if !ok {
		return int32(0), false
	}
	intValue, err := strconv.ParseInt(annotationValue, 10, 32)
	if err != nil {
		log.Warnf("Cannot convert the value %q with annotation key %q for the replica set %q", annotationValue, WorkloadGenerationAnnotation, ro.Name)
		return int32(0), false
	}
	return int32(intValue), true
}

// GetRevisionAnnotation returns revision of rollout
func GetRevisionAnnotation(ro *v1alpha1.Rollout) (int32, bool) {
	if ro == nil {
		return 0, false
	}
	annotationValue, ok := ro.Annotations[RevisionAnnotation]
	if !ok {
		return int32(0), false
	}
	intValue, err := strconv.ParseInt(annotationValue, 10, 32)
	if err != nil {
		log.Warnf("Cannot convert the value %q with annotation key %q for the replica set %q", annotationValue, RevisionAnnotation, ro.Name)
		return int32(0), false
	}
	return int32(intValue), true
}

func getIntFromAnnotation(rs *appsv1.ReplicaSet, annotationKey string) (int32, bool) {
	if rs == nil {
		return 0, false
	}
	annotationValue, ok := rs.Annotations[annotationKey]
	if !ok {
		return int32(0), false
	}
	intValue, err := strconv.ParseInt(annotationValue, 10, 32)
	if err != nil {
		log.Warnf("Cannot convert the value %q with annotation key %q for the replica set %q", annotationValue, annotationKey, rs.Name)
		return int32(0), false
	}
	return int32(intValue), true
}

// SetRolloutRevision updates the revision for a rollout.
func SetRolloutRevision(rollout *v1alpha1.Rollout, revision string) bool {
	if rollout.Annotations == nil {
		rollout.Annotations = make(map[string]string)
	}
	if rollout.Annotations[RevisionAnnotation] != revision {
		rollout.Annotations[RevisionAnnotation] = revision
		return true
	}
	return false
}

// SetRolloutWorkloadRefGeneration updates the workflow generation annotation for a rollout.
func SetRolloutWorkloadRefGeneration(rollout *v1alpha1.Rollout, workloadGeneration string) bool {
	if rollout.Annotations == nil {
		rollout.Annotations = make(map[string]string)
	}
	if rollout.Annotations[WorkloadGenerationAnnotation] != workloadGeneration {
		rollout.Annotations[WorkloadGenerationAnnotation] = workloadGeneration
		return true
	}
	return false
}

// RemoveRolloutWorkloadRefGeneration remove the annotation of workload ref generation
func RemoveRolloutWorkloadRefGeneration(rollout *v1alpha1.Rollout) {
	if rollout.Annotations == nil {
		return
	}
	delete(rollout.Annotations, WorkloadGenerationAnnotation)
}

// SetReplicasAnnotations sets the desiredReplicas into the annotations
func SetReplicasAnnotations(rs *appsv1.ReplicaSet, desiredReplicas int32) bool {
	if rs.Annotations == nil {
		rs.Annotations = make(map[string]string)
	}
	desiredString := fmt.Sprintf("%d", desiredReplicas)
	if hasString := rs.Annotations[DesiredReplicasAnnotation]; hasString != desiredString {
		rs.Annotations[DesiredReplicasAnnotation] = desiredString
		return true
	}
	return false
}

// ReplicasAnnotationsNeedUpdate return true if ReplicasAnnotations need to be updated
func ReplicasAnnotationsNeedUpdate(rs *appsv1.ReplicaSet, desiredReplicas int32) bool {
	if rs.Annotations == nil {
		return true
	}
	desiredString := fmt.Sprintf("%d", desiredReplicas)
	if hasString := rs.Annotations[DesiredReplicasAnnotation]; hasString != desiredString {
		return true
	}
	hasScaleDownDelay := rs.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]
	if desiredReplicas == int32(0) && hasScaleDownDelay != "" {
		return true
	}
	return false
}

// SetNewReplicaSetAnnotations sets new replica set's annotations appropriately by updating its revision and
// copying required rollout annotations to it; it returns true if replica set's annotation is changed.
func SetNewReplicaSetAnnotations(rollout *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, newRevision string, exists bool) bool {
	logCtx := logutil.WithRollout(rollout)
	// First, copy rollout's annotations (except for apply and revision annotations)
	annotationChanged := copyRolloutAnnotationsToReplicaSet(rollout, newRS)
	// Then, update replica set's revision annotation
	if newRS.Annotations == nil {
		newRS.Annotations = make(map[string]string)
	}
	oldRevision, ok := newRS.Annotations[RevisionAnnotation]
	// The newRS's revision should be the greatest among all RSes. Usually, its revision number is newRevision (the max revision number
	// of all old RSes + 1). However, it's possible that some of the old RSes are deleted after the newRS revision being updated, and
	// newRevision becomes smaller than newRS's revision. We should only update newRS revision when it's smaller than newRevision.

	oldRevisionInt, err := strconv.ParseInt(oldRevision, 10, 64)
	if err != nil {
		if oldRevision != "" {
			logCtx.Warnf("Updating replica set '%s' revision: OldRevision not int '%s'", newRS.Name, err)
			return false
		}
		//If the RS annotation is empty then initialise it to 0
		oldRevisionInt = 0
	}
	newRevisionInt, err := strconv.ParseInt(newRevision, 10, 64)
	if err != nil {
		logCtx.Warnf("Updating replica set '%s' revision: NewRevision not int %s", newRS.Name, err)
		return false
	}
	if oldRevisionInt < newRevisionInt {
		newRS.Annotations[RevisionAnnotation] = newRevision
		annotationChanged = true
		logCtx.Infof("Updating replica set '%s' revision from %d to %d", newRS.Name, oldRevisionInt, newRevisionInt)
	}
	// If a revision annotation already existed and this replica set was updated with a new revision
	// then that means we are rolling back to this replica set. We need to preserve the old revisions
	// for historical information.
	if ok && annotationChanged {
		revisionHistoryAnnotation := newRS.Annotations[RevisionHistoryAnnotation]
		oldRevisions := strings.Split(revisionHistoryAnnotation, ",")
		if len(oldRevisions[0]) == 0 {
			newRS.Annotations[RevisionHistoryAnnotation] = oldRevision
		} else {
			oldRevisions = append(oldRevisions, oldRevision)
			newRS.Annotations[RevisionHistoryAnnotation] = strings.Join(oldRevisions, ",")
		}
	}
	// If the new replica set is about to be created, we need to add replica annotations to it.
	//TODO: look at implementation due to surge
	if !exists && SetReplicasAnnotations(newRS, defaults.GetReplicasOrDefault(rollout.Spec.Replicas)) {
		annotationChanged = true
	}
	return annotationChanged
}

var annotationsToSkip = map[string]bool{
	corev1.LastAppliedConfigAnnotation: true,
	RevisionAnnotation:                 true,
	RevisionHistoryAnnotation:          true,
	DesiredReplicasAnnotation:          true,
}

// skipCopyAnnotation returns true if we should skip copying the annotation with the given annotation key
func skipCopyAnnotation(key string) bool {
	return annotationsToSkip[key]
}

// copyRolloutAnnotationsToReplicaSet copies rollout's annotations to replica set's annotations,
// and returns true if replica set's annotation is changed.
// Note that apply and revision annotations are not copied.
func copyRolloutAnnotationsToReplicaSet(rollouts *v1alpha1.Rollout, rs *appsv1.ReplicaSet) bool {
	rsAnnotationsChanged := false
	if rs.Annotations == nil {
		rs.Annotations = make(map[string]string)
	}
	for k, v := range rollouts.Annotations {
		// newRS revision is updated automatically in getNewReplicaSet, and the rollout's revision number is then updated
		// by copying its newRS revision number. We should not copy rollout's revision to its newRS, since the update of
		// rollout revision number may fail (revision becomes stale) and the revision number in newRS is more reliable.
		if skipCopyAnnotation(k) || rs.Annotations[k] == v {
			continue
		}
		rs.Annotations[k] = v
		rsAnnotationsChanged = true
	}
	return rsAnnotationsChanged
}

// IsSaturated checks if the new replica set is saturated by comparing its size with its rollout size.
// Both the rollout and the replica set have to believe this replica set can own all of the desired
// replicas in the rollout and the annotation helps in achieving that. All pods of the ReplicaSet
// need to be available.
func IsSaturated(rollout *v1alpha1.Rollout, rs *appsv1.ReplicaSet) bool {
	if rs == nil {
		return false
	}
	desiredString := rs.Annotations[DesiredReplicasAnnotation]
	desired, err := strconv.ParseInt(desiredString, 10, 32)
	if err != nil {
		return false
	}
	rolloutReplicas := defaults.GetReplicasOrDefault(rollout.Spec.Replicas)
	return *(rs.Spec.Replicas) == rolloutReplicas &&
		int32(desired) == rolloutReplicas &&
		rs.Status.AvailableReplicas == rolloutReplicas
}
