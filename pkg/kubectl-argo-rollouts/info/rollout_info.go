package info

import (
	"fmt"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

type RolloutInfo struct {
	metav1.TypeMeta `json:",inline"`
	Metadata        `json:"metadata,omitempty"`

	Spec RolloutInfoSpec `json:"spec"`
}

type RolloutInfoSpec struct {
	Status       string `json:"status"`
	Icon         string `json:"-"`
	Strategy     string `json:"strategy,omitempty"`
	Step         string `json:"step,omitempty"`
	SetWeight    string `json:"setWeight,omitempty"`
	ActualWeight string `json:"actualWeight,omitempty"`

	Ready     int32 `json:"ready"`
	Current   int32 `json:"current"`
	Desired   int32 `json:"desired"`
	Updated   int32 `json:"updated"`
	Available int32 `json:"available"`

	ReplicaSets  []ReplicaSetInfo  `json:"replicaSets,omitempty"`
	Experiments  []ExperimentInfo  `json:"experiments,omitempty"`
	AnalysisRuns []AnalysisRunInfo `json:"analysisRuns,omitempty"`
}

func NewRolloutInfo(
	ro *v1alpha1.Rollout,
	allReplicaSets []*appsv1.ReplicaSet,
	allPods []*corev1.Pod,
	allExperiments []*v1alpha1.Experiment,
	allARs []*v1alpha1.AnalysisRun,
) *RolloutInfo {

	roInfo := RolloutInfo{
		Spec: RolloutInfoSpec{},
	}
	roInfo.Kind = "RolloutInfo"
	roInfo.APIVersion = "argoproj.io/v1alpha1"
	roInfo.Name = ro.Name
	roInfo.Namespace = ro.Namespace
	roInfo.UID = ro.UID
	roInfo.Labels = ro.Labels
	roInfo.Annotations = ro.Annotations
	roInfo.CreationTimestamp = ro.CreationTimestamp
	roInfo.Spec.ReplicaSets = getReplicaSetInfo(ro.UID, ro, allReplicaSets, allPods)
	roInfo.Spec.Experiments = getExperimentInfo(ro, allExperiments, allReplicaSets, allARs, allPods)
	roInfo.Spec.AnalysisRuns = getAnalysisRunInfo(ro.UID, allARs)

	if ro.Spec.Strategy.Canary != nil {
		roInfo.Spec.Strategy = "Canary"
		if ro.Status.CurrentStepIndex != nil && len(ro.Spec.Strategy.Canary.Steps) > 0 {
			roInfo.Spec.Step = fmt.Sprintf("%d/%d", *ro.Status.CurrentStepIndex, len(ro.Spec.Strategy.Canary.Steps))
		}
		// NOTE that this is desired weight, not the actual current weight
		roInfo.Spec.SetWeight = strconv.Itoa(int(replicasetutil.GetCurrentSetWeight(ro)))

		roInfo.Spec.ActualWeight = "0"
		currentStep, _ := replicasetutil.GetCurrentCanaryStep(ro)

		if currentStep == nil {
			roInfo.Spec.ActualWeight = "100"
		} else if ro.Status.AvailableReplicas > 0 {
			if ro.Spec.Strategy.Canary.TrafficRouting == nil {
				for _, rs := range roInfo.Spec.ReplicaSets {
					if rs.Spec.Canary {
						roInfo.Spec.ActualWeight = fmt.Sprintf("%d", (rs.Spec.Available*100)/ro.Status.AvailableReplicas)
					}
				}
			} else {
				roInfo.Spec.ActualWeight = roInfo.Spec.SetWeight
			}
		}
	} else if ro.Spec.Strategy.BlueGreen != nil {
		roInfo.Spec.Strategy = "BlueGreen"
	}
	roInfo.Spec.Status = RolloutStatusString(ro)
	roInfo.Spec.Icon = rolloutIcon(roInfo.Spec.Status)

	roInfo.Spec.Desired = defaults.GetReplicasOrDefault(ro.Spec.Replicas)
	roInfo.Spec.Ready = ro.Status.ReadyReplicas
	roInfo.Spec.Current = ro.Status.Replicas
	roInfo.Spec.Updated = ro.Status.UpdatedReplicas
	roInfo.Spec.Available = ro.Status.AvailableReplicas
	return &roInfo
}

// RolloutStatusString returns a status string to print in the STATUS column
func RolloutStatusString(ro *v1alpha1.Rollout) string {
	for _, cond := range ro.Status.Conditions {
		if cond.Type == v1alpha1.InvalidSpec {
			return string(cond.Type)
		}
		switch cond.Reason {
		case conditions.RolloutAbortedReason, conditions.TimedOutReason:
			return "Degraded"
		}
	}
	if ro.Spec.Paused || len(ro.Status.PauseConditions) > 0 {
		return "Paused"
	}
	if ro.Status.UpdatedReplicas < defaults.GetReplicasOrDefault(ro.Spec.Replicas) {
		// not enough updated replicas
		return "Progressing"
	}
	if ro.Status.UpdatedReplicas < ro.Status.Replicas {
		// more replicas need to be updated
		return "Progressing"
	}
	if ro.Status.AvailableReplicas < ro.Status.UpdatedReplicas {
		// updated replicas are still becoming available
		return "Progressing"
	}
	if ro.Spec.Strategy.BlueGreen != nil {
		if ro.Status.BlueGreen.ActiveSelector != "" && ro.Status.BlueGreen.ActiveSelector == ro.Status.CurrentPodHash {
			return "Healthy"
		}
		// service cutover pending
		return "Progressing"
	} else if ro.Spec.Strategy.Canary != nil {
		if ro.Status.Replicas > ro.Status.UpdatedReplicas {
			// old replicas are pending termination
			return "Progressing"
		}
		stableRS := ro.Status.StableRS
		//TODO(dthomson) Remove canary.stableRS for v0.9
		if ro.Status.Canary.StableRS != "" {
			stableRS = ro.Status.Canary.StableRS
		}
		if stableRS != "" && stableRS == ro.Status.CurrentPodHash {
			return "Healthy"
		}
		// Waiting for rollout to finish steps
		return "Progressing"
	}
	return "Unknown"
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
func (r *RolloutInfo) Images() []ImageInfo {
	var images []ImageInfo
	for _, rsInfo := range r.Spec.ReplicaSets {
		if rsInfo.Spec.Replicas > 0 {
			for _, image := range rsInfo.Spec.Images {
				newImage := ImageInfo{
					Image: image,
				}
				if rsInfo.Spec.Canary {
					newImage.Tags = append(newImage.Tags, InfoTagCanary)
				}
				if rsInfo.Spec.Stable {
					newImage.Tags = append(newImage.Tags, InfoTagStable)
				}
				if rsInfo.Spec.Active {
					newImage.Tags = append(newImage.Tags, InfoTagActive)
				}
				if rsInfo.Spec.Preview {
					newImage.Tags = append(newImage.Tags, InfoTagPreview)
				}
				images = mergeImageAndTags(newImage, images)
			}
		}
	}
	for _, expInfo := range r.Spec.Experiments {
		for _, expImage := range expInfo.Spec.Images() {
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

func (r *RolloutInfo) Revisions() []int {
	revisionMap := make(map[int]bool)
	for _, rsInfo := range r.Spec.ReplicaSets {
		revisionMap[rsInfo.Spec.Revision] = true
	}
	for _, expInfo := range r.Spec.Experiments {
		revisionMap[expInfo.Spec.Revision] = true
	}
	for _, arInfo := range r.Spec.AnalysisRuns {
		revisionMap[arInfo.Spec.Revision] = true
	}
	revisions := make([]int, 0, len(revisionMap))
	for k := range revisionMap {
		revisions = append(revisions, k)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(revisions)))
	return revisions
}

func (r *RolloutInfo) ReplicaSetsByRevision(rev int) []ReplicaSetInfo {
	var replicaSets []ReplicaSetInfo
	for _, rs := range r.Spec.ReplicaSets {
		if rs.Spec.Revision == rev {
			replicaSets = append(replicaSets, rs)
		}
	}
	return replicaSets
}

func (r *RolloutInfo) ExperimentsByRevision(rev int) []ExperimentInfo {
	var experiments []ExperimentInfo
	for _, e := range r.Spec.Experiments {
		if e.Spec.Revision == rev {
			experiments = append(experiments, e)
		}
	}
	return experiments
}

func (r *RolloutInfo) AnalysisRunsByRevision(rev int) []AnalysisRunInfo {
	var runs []AnalysisRunInfo
	for _, run := range r.Spec.AnalysisRuns {
		if run.Spec.Revision == rev {
			runs = append(runs, run)
		}
	}
	return runs
}

func (r *RolloutInfo) DeepCopyObject() runtime.Object {
	out := new(RolloutInfo)
	*out = *r
	out.TypeMeta = r.TypeMeta
	return out
}
