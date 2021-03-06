package info

import (
	"fmt"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

type RolloutInfo v1alpha1.RolloutInfo

func NewRolloutInfo(
	ro *v1alpha1.Rollout,
	allReplicaSets []*appsv1.ReplicaSet,
	allPods []*corev1.Pod,
	allExperiments []*v1alpha1.Experiment,
	allARs []*v1alpha1.AnalysisRun,
) *v1alpha1.RolloutInfo {

	roInfo := v1alpha1.RolloutInfo{
		ObjectMeta: v1.ObjectMeta{
			Name:              ro.Name,
			Namespace:         ro.Namespace,
			UID:               ro.UID,
			CreationTimestamp: ro.CreationTimestamp,
		},
	}

	roInfo.ReplicaSets = getReplicaSetInfo(ro.UID, ro, allReplicaSets, allPods)
	roInfo.Experiments = getExperimentInfo(ro, allExperiments, allReplicaSets, allARs, allPods)
	roInfo.AnalysisRuns = getAnalysisRunInfo(ro.UID, allARs)

	if ro.Spec.Strategy.Canary != nil {
		roInfo.Strategy = "Canary"
		if ro.Status.CurrentStepIndex != nil && len(ro.Spec.Strategy.Canary.Steps) > 0 {
			roInfo.Step = fmt.Sprintf("%d/%d", *ro.Status.CurrentStepIndex, len(ro.Spec.Strategy.Canary.Steps))
		}
		// NOTE that this is desired weight, not the actual current weight
		roInfo.SetWeight = strconv.Itoa(int(replicasetutil.GetCurrentSetWeight(ro)))

		roInfo.ActualWeight = "0"
		currentStep, _ := replicasetutil.GetCurrentCanaryStep(ro)

		if currentStep == nil {
			roInfo.ActualWeight = "100"
		} else if ro.Status.AvailableReplicas > 0 {
			if ro.Spec.Strategy.Canary.TrafficRouting == nil {
				for _, rs := range roInfo.ReplicaSets {
					if rs.Canary {
						roInfo.ActualWeight = fmt.Sprintf("%d", (rs.Available*100)/ro.Status.AvailableReplicas)
					}
				}
			} else {
				roInfo.ActualWeight = roInfo.SetWeight
			}
		}
	} else if ro.Spec.Strategy.BlueGreen != nil {
		roInfo.Strategy = "BlueGreen"
	}
	roInfo.Status, roInfo.Message = RolloutStatusString(ro)
	roInfo.Icon = rolloutIcon(roInfo.Status)

	if (ro.Status.RestartedAt != nil) {
		roInfo.RestartedAt = ro.Status.RestartedAt.String()
	} else {
		roInfo.RestartedAt = "Never"
	}

	roInfo.Generation = ro.Status.ObservedGeneration
	

	roInfo.Desired = defaults.GetReplicasOrDefault(ro.Spec.Replicas)
	roInfo.Ready = ro.Status.ReadyReplicas
	roInfo.Current = ro.Status.Replicas
	roInfo.Updated = ro.Status.UpdatedReplicas
	roInfo.Available = ro.Status.AvailableReplicas
	return &roInfo
}

func RolloutErrorConditions(ro *v1alpha1.Rollout) []string {
	var errorConditions []string
	for _, status := range ro.Status.Conditions {
		if status.Type == v1alpha1.InvalidSpec {
			errorConditions = append(errorConditions, status.Message)
		}
	}
	arStatuses := []*v1alpha1.RolloutAnalysisRunStatus{
		ro.Status.Canary.CurrentStepAnalysisRunStatus,
		ro.Status.Canary.CurrentBackgroundAnalysisRunStatus,
		ro.Status.BlueGreen.PrePromotionAnalysisRunStatus,
		ro.Status.BlueGreen.PostPromotionAnalysisRunStatus,
	}
	if ro.Status.Abort {
		for _, arStatus := range arStatuses {
			if arStatus == nil {
				continue
			}
			if arStatus.Status.Completed() &&
				arStatus.Status != v1alpha1.AnalysisPhaseSuccessful &&
				arStatus.Message != "" {
				errorConditions = append(errorConditions, arStatus.Message)
			}
		}
	}
	return errorConditions
}

// isGenerationObserved determines if the rollout spec has been observed by the controller. This
// only applies to v0.10 rollout which uses a numeric status.observedGeneration. For v0.9 rollouts
// and below this function always returns true.
func isGenerationObserved(ro *v1alpha1.Rollout) bool {
	observedGen, err := strconv.Atoi(ro.Status.ObservedGeneration)
	if err != nil {
		return true
	}
	// It's still possible for a v0.9 rollout to have an all numeric hash, this covers that corner case
	if int64(observedGen) > ro.Generation {
		return true
	}
	return int64(observedGen) == ro.Generation
}

// RolloutStatusString returns a status and message for a rollout
// This logic is more or less the same as the Argo CD rollouts health.lua check
// Any changes to this function should also be changed there
func RolloutStatusString(ro *v1alpha1.Rollout) (string, string) {
	if !isGenerationObserved(ro) {
		return "Progressing", "waiting for rollout spec update to be observed"
	}
	for _, cond := range ro.Status.Conditions {
		if cond.Type == v1alpha1.InvalidSpec {
			return "Degraded", fmt.Sprintf("%s: %s", v1alpha1.InvalidSpec, cond.Message)
		}
		switch cond.Reason {
		case conditions.RolloutAbortedReason, conditions.TimedOutReason:
			return "Degraded", fmt.Sprintf("%s: %s", cond.Reason, cond.Message)
		}
	}
	if ro.Spec.Paused {
		return "Paused", "manually paused"
	}
	for _, pauseCond := range ro.Status.PauseConditions {
		return "Paused", string(pauseCond.Reason)
	}
	if ro.Status.UpdatedReplicas < defaults.GetReplicasOrDefault(ro.Spec.Replicas) {
		return "Progressing", "more replicas need to be updated"
	}
	if ro.Status.AvailableReplicas < ro.Status.UpdatedReplicas {
		return "Progressing", "updated replicas are still becoming available"
	}
	if ro.Spec.Strategy.BlueGreen != nil {
		if ro.Status.BlueGreen.ActiveSelector == "" || ro.Status.BlueGreen.ActiveSelector != ro.Status.CurrentPodHash {
			return "Progressing", "active service cutover pending"
		}
		if ro.Status.StableRS == "" || ro.Status.StableRS != ro.Status.CurrentPodHash {
			return "Progressing", "waiting for analysis to complete"
		}
	} else if ro.Spec.Strategy.Canary != nil {
		if ro.Status.Replicas > ro.Status.UpdatedReplicas {
			// This check should only be done for canary and not blue-green since blue-green has the
			// scaleDownDelay feature which leaves the old stack of replicas running for a long time
			return "Progressing", "old replicas are pending termination"
		}
		if ro.Status.StableRS == "" || ro.Status.StableRS != ro.Status.CurrentPodHash {
			return "Progressing", "waiting for all steps to complete"
		}
	}
	return "Healthy", ""
}

func rolloutIcon(status string) string {
	switch status {
	case "Progressing":
		return IconProgressing
	case "Error", "InvalidSpec":
		return IconWarning
	case "Paused":
		return IconPaused
	case "Healthy":
		return IconOK
	case "Degraded":
		return IconBad
	case "Unknown":
		return IconUnknown
	case "ScaledDown":
		return IconNeutral
	}
	return " "
}

// Images returns a list of images that are currently running along with informational tags about
// 1. which stack they belong to (canary, stable, active, preview)
// 2. which experiment template they are part of
func Images(r *v1alpha1.RolloutInfo) []ImageInfo {
	var images []ImageInfo
	for _, rsInfo := range r.ReplicaSets {
		if rsInfo.Replicas > 0 {
			for _, image := range rsInfo.Images {
				newImage := ImageInfo{
					Image: image,
				}
				if rsInfo.Canary {
					newImage.Tags = append(newImage.Tags, InfoTagCanary)
				}
				if rsInfo.Stable {
					newImage.Tags = append(newImage.Tags, InfoTagStable)
				}
				if rsInfo.Active {
					newImage.Tags = append(newImage.Tags, InfoTagActive)
				}
				if rsInfo.Preview {
					newImage.Tags = append(newImage.Tags, InfoTagPreview)
				}
				images = mergeImageAndTags(newImage, images)
			}
		}
	}
	for _, expInfo := range r.Experiments {
		for _, expImage := range ExperimentImages(&expInfo) {
			images = mergeImageAndTags(expImage, images)
		}
	}
	return images
}

// mergeImageAndTags updates or appends the given image, and merges the tags
func mergeImageAndTags(image ImageInfo, images []ImageInfo) []ImageInfo {
	foundIdx := -1
	for i, img := range images {
		if img.Image == image.Image {
			foundIdx = i
			break
		}
	}
	if foundIdx == -1 {
		images = append(images, image)
	} else {
		existing := images[foundIdx]
		existing.Tags = mergeTags(image.Tags, existing.Tags)
		images[foundIdx] = existing
	}
	sort.Slice(images[:], func(i, j int) bool {
		return images[i].Image < images[j].Image
	})
	return images
}

func mergeTags(newTags []string, existingTags []string) []string {
	newTagMap := make(map[string]bool)
	for _, tag := range newTags {
		newTagMap[tag] = true
	}
	for _, tag := range existingTags {
		newTagMap[tag] = true
	}
	var tags []string
	for tag := range newTagMap {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags
}

func Revisions(r *v1alpha1.RolloutInfo) []int {
	revisionMap := make(map[int]bool)
	for _, rsInfo := range r.ReplicaSets {
		revisionMap[int(rsInfo.Revision)] = true
	}
	for _, expInfo := range r.Experiments {
		revisionMap[int(expInfo.Revision)] = true
	}
	for _, arInfo := range r.AnalysisRuns {
		revisionMap[int(arInfo.Revision)] = true
	}
	revisions := make([]int, 0, len(revisionMap))
	for k := range revisionMap {
		revisions = append(revisions, k)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(revisions)))
	return revisions
}

func ReplicaSetsByRevision(r *v1alpha1.RolloutInfo, rev int) []v1alpha1.ReplicaSetInfo {
	var replicaSets []v1alpha1.ReplicaSetInfo
	for _, rs := range r.ReplicaSets {
		if rs.Revision == int32(rev) {
			replicaSets = append(replicaSets, rs)
		}
	}
	return replicaSets
}

func ExperimentsByRevision(r *v1alpha1.RolloutInfo, rev int) []v1alpha1.ExperimentInfo {
	var experiments []v1alpha1.ExperimentInfo
	for _, e := range r.Experiments {
		if int(e.Revision) == rev {
			experiments = append(experiments, e)
		}
	}
	return experiments
}

func AnalysisRunsByRevision(r *v1alpha1.RolloutInfo, rev int) []v1alpha1.AnalysisRunInfo {
	var runs []v1alpha1.AnalysisRunInfo
	for _, run := range r.AnalysisRuns {
		if int(run.Revision) == rev {
			runs = append(runs, run)
		}
	}
	return runs
}
