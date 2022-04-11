package info

import (
	"fmt"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
)

func NewRolloutInfo(
	ro *v1alpha1.Rollout,
	allReplicaSets []*appsv1.ReplicaSet,
	allPods []*corev1.Pod,
	allExperiments []*v1alpha1.Experiment,
	allARs []*v1alpha1.AnalysisRun,
	workloadRef *appsv1.Deployment,
) *rollout.RolloutInfo {

	roInfo := rollout.RolloutInfo{
		ObjectMeta: &v1.ObjectMeta{
			Name:              ro.Name,
			Namespace:         ro.Namespace,
			UID:               ro.UID,
			CreationTimestamp: ro.CreationTimestamp,
			ResourceVersion:   ro.ObjectMeta.ResourceVersion,
		},
	}

	roInfo.ReplicaSets = GetReplicaSetInfo(ro.UID, ro, allReplicaSets, allPods)
	roInfo.Experiments = getExperimentInfo(ro, allExperiments, allReplicaSets, allARs, allPods)
	roInfo.AnalysisRuns = getAnalysisRunInfo(ro.UID, allARs)

	if ro.Spec.Strategy.Canary != nil {
		roInfo.Strategy = "Canary"
		if ro.Status.CurrentStepIndex != nil && len(ro.Spec.Strategy.Canary.Steps) > 0 {
			roInfo.Step = fmt.Sprintf("%d/%d", *ro.Status.CurrentStepIndex, len(ro.Spec.Strategy.Canary.Steps))
			var steps []*v1alpha1.CanaryStep
			for i := range ro.Spec.Strategy.Canary.Steps {
				steps = append(steps, &ro.Spec.Strategy.Canary.Steps[i])
			}
			roInfo.Steps = steps
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
				if ro.Status.Canary.Weights != nil {
					roInfo.ActualWeight = fmt.Sprintf("%d", ro.Status.Canary.Weights.Canary.Weight)
				} else {
					roInfo.ActualWeight = roInfo.SetWeight
				}
			}
		}
	} else if ro.Spec.Strategy.BlueGreen != nil {
		roInfo.Strategy = "BlueGreen"
	}
	phase, message := rolloututil.GetRolloutPhase(ro)
	roInfo.Status = string(phase)
	roInfo.Message = message
	roInfo.Icon = rolloutIcon(roInfo.Status)
	roInfo.Containers = []*rollout.ContainerInfo{}

	var containerList []corev1.Container
	if workloadRef != nil {
		containerList = workloadRef.Spec.Template.Spec.Containers
	} else {
		containerList = ro.Spec.Template.Spec.Containers
	}

	for _, c := range containerList {
		roInfo.Containers = append(roInfo.Containers, &rollout.ContainerInfo{Name: c.Name, Image: c.Image})
	}

	if ro.Status.RestartedAt != nil {
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
func Images(r *rollout.RolloutInfo) []ImageInfo {
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
				if rsInfo.Ping {
					newImage.Tags = append(newImage.Tags, InfoTagPing)
				}
				if rsInfo.Pong {
					newImage.Tags = append(newImage.Tags, InfoTagPong)
				}
				images = mergeImageAndTags(newImage, images)
			}
		}
	}
	for _, expInfo := range r.Experiments {
		for _, expImage := range ExperimentImages(expInfo) {
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

func Revisions(r *rollout.RolloutInfo) []int {
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

func ReplicaSetsByRevision(r *rollout.RolloutInfo, rev int) []*rollout.ReplicaSetInfo {
	var replicaSets []*rollout.ReplicaSetInfo
	for _, rs := range r.ReplicaSets {
		if rs.Revision == int64(rev) {
			replicaSets = append(replicaSets, rs)
		}
	}
	return replicaSets
}

func ExperimentsByRevision(r *rollout.RolloutInfo, rev int) []*rollout.ExperimentInfo {
	var experiments []*rollout.ExperimentInfo
	for _, e := range r.Experiments {
		if int(e.Revision) == rev {
			experiments = append(experiments, e)
		}
	}
	return experiments
}

func AnalysisRunsByRevision(r *rollout.RolloutInfo, rev int) []*rollout.AnalysisRunInfo {
	var runs []*rollout.AnalysisRunInfo
	for _, run := range r.AnalysisRuns {
		if int(run.Revision) == rev {
			runs = append(runs, run)
		}
	}
	return runs
}
