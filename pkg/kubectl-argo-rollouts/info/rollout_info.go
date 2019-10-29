package info

import (
	"fmt"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

type RolloutInfo struct {
	Metadata
	Namespace string

	Status       string
	Icon         string
	Strategy     string
	Step         string
	SetWeight    string
	ActualWeight string

	Ready     int32
	Current   int32
	Desired   int32
	Updated   int32
	Available int32

	ReplicaSets  []ReplicaSetInfo
	Experiments  []ExperimentInfo
	AnalysisRuns []AnalysisRunInfo
}

func NewRolloutInfo(
	ro *v1alpha1.Rollout,
	allReplicaSets []*appsv1.ReplicaSet,
	allPods []*corev1.Pod,
	allExperiments []*v1alpha1.Experiment,
	allARs []*v1alpha1.AnalysisRun,
) *RolloutInfo {

	roInfo := RolloutInfo{
		Metadata: Metadata{
			Name:              ro.Name,
			UID:               ro.UID,
			CreationTimestamp: ro.CreationTimestamp,
		},
		Namespace: ro.Namespace,
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
		} else {
			for _, rs := range roInfo.ReplicaSets {
				if rs.Canary {
					roInfo.ActualWeight = fmt.Sprintf("%d", (rs.Available*100)/ro.Status.AvailableReplicas)
				}
			}
		}
	} else if ro.Spec.Strategy.BlueGreen != nil {
		roInfo.Strategy = "BlueGreen"
	}
	roInfo.Status = RolloutStatusString(ro)
	roInfo.Icon = rolloutIcon(roInfo.Status)

	roInfo.Desired = defaults.GetReplicasOrDefault(ro.Spec.Replicas)
	roInfo.Ready = ro.Status.ReadyReplicas
	roInfo.Current = ro.Status.Replicas
	roInfo.Updated = ro.Status.UpdatedReplicas
	roInfo.Available = ro.Status.AvailableReplicas
	return &roInfo
}

// RolloutStatusString returns a status string to print in the STATUS column
func RolloutStatusString(ro *v1alpha1.Rollout) string {
	for _, cond := range ro.Status.Conditions {
		if cond.Type == v1alpha1.InvalidSpec {
			return string(cond.Type)
		}
		if cond.Type == v1alpha1.RolloutProgressing && cond.Reason == "ProgressDeadlineExceeded" {
			return "Degraded"
		}
	}
	if ro.Spec.Paused || len(ro.Status.PauseConditions) > 0 {
		return "Paused"
	}
	if ro.Status.UpdatedReplicas < ro.Status.Replicas {
		// more replicas need to be updated
		return "Progressing"
	}
	if ro.Status.Replicas > ro.Status.UpdatedReplicas {
		// old replicas are pending termination
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
		if ro.Status.Canary.StableRS != "" && ro.Status.Canary.StableRS == ro.Status.CurrentPodHash {
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

func (r *RolloutInfo) Images() []string {
	uniqueImages := make(map[string]bool)
	var images []string
	for _, rsInfo := range r.ReplicaSets {
		if rsInfo.Available == 0 {
			continue
		}
		for _, image := range rsInfo.Images {
			if !uniqueImages[image] {
				images = append(images, image)
				uniqueImages[image] = true
				//fmt.Println(podInfo.Name, image, rsInfo.Name, rsInfo.Available, podInfo.Ready)
			}
		}
	}
	sort.Strings(images)
	return images
}

func (r *RolloutInfo) Revisions() []int {
	revisionMap := make(map[int]bool)
	for _, rsInfo := range r.ReplicaSets {
		revisionMap[rsInfo.Revision] = true
	}
	for _, expInfo := range r.Experiments {
		revisionMap[expInfo.Revision] = true
	}
	for _, arInfo := range r.AnalysisRuns {
		revisionMap[arInfo.Revision] = true
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
	for _, rs := range r.ReplicaSets {
		if rs.Revision == rev {
			replicaSets = append(replicaSets, rs)
		}
	}
	return replicaSets
}

func (r *RolloutInfo) ExperimentsByRevision(rev int) []ExperimentInfo {
	var experiments []ExperimentInfo
	for _, e := range r.Experiments {
		if e.Revision == rev {
			experiments = append(experiments, e)
		}
	}
	return experiments
}

func (r *RolloutInfo) AnalysisRunsByRevision(rev int) []AnalysisRunInfo {
	var runs []AnalysisRunInfo
	for _, run := range r.AnalysisRuns {
		if run.Revision == rev {
			runs = append(runs, run)
		}
	}
	return runs
}
