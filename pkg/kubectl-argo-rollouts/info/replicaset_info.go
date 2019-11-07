package info

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

type ReplicaSetInfo struct {
	Metadata
	Status    string
	Icon      string
	Revision  int
	Stable    bool
	Canary    bool
	Active    bool
	Preview   bool
	Replicas  int32
	Available int32
	Template  string
	Images    []string
	Pods      []PodInfo
}

func getReplicaSetInfo(ownerUID types.UID, ro *v1alpha1.Rollout, allReplicaSets []*appsv1.ReplicaSet, allPods []*corev1.Pod) []ReplicaSetInfo {
	var rsInfos []ReplicaSetInfo
	for _, rs := range allReplicaSets {
		// if owned by replicaset
		if ownerRef(rs.OwnerReferences, []types.UID{ownerUID}) == nil {
			continue
		}
		rsInfo := ReplicaSetInfo{
			Metadata: Metadata{
				Name:              rs.Name,
				Namespace:         rs.Namespace,
				CreationTimestamp: rs.CreationTimestamp,
				UID:               rs.UID,
			},
			Status:    getReplicaSetHealth(rs),
			Replicas:  rs.Status.Replicas,
			Available: rs.Status.AvailableReplicas,
		}
		rsInfo.Icon = replicaSetIcon(rsInfo.Status)
		rsInfo.Revision = parseRevision(rs.ObjectMeta.Annotations)
		rsInfo.Template = parseExperimentTemplateName(rs.ObjectMeta.Annotations)

		if ro != nil {
			if ro.Spec.Strategy.Canary != nil && rs.Labels != nil {
				if ro.Status.Canary.StableRS == rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
					rsInfo.Stable = true
				} else if ro.Status.CurrentPodHash == rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
					rsInfo.Canary = true
				}
			}
			if ro.Spec.Strategy.BlueGreen != nil {
				if ro.Status.BlueGreen.ActiveSelector == rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
					rsInfo.Active = true
				} else if ro.Status.BlueGreen.PreviewSelector == rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
					rsInfo.Preview = true
				}
			}
		}

		for _, ctr := range rs.Spec.Template.Spec.Containers {
			rsInfo.Images = append(rsInfo.Images, ctr.Image)
		}
		rsInfos = append(rsInfos, rsInfo)
	}
	sort.Slice(rsInfos[:], func(i, j int) bool {
		if rsInfos[i].Revision != rsInfos[j].Revision {
			return rsInfos[i].Revision > rsInfos[j].Revision
		}
		if rsInfos[i].CreationTimestamp != rsInfos[j].CreationTimestamp {
			rsInfos[i].CreationTimestamp.Before(&rsInfos[j].CreationTimestamp)
		}
		return rsInfos[i].Name < rsInfos[j].Name
	})
	return addPodInfos(rsInfos, allPods)
}

func getReplicaSetHealth(rs *appsv1.ReplicaSet) string {
	if rs.Generation <= rs.Status.ObservedGeneration {
		cond := getReplicaSetCondition(rs.Status, appsv1.ReplicaSetReplicaFailure)
		if cond != nil && cond.Status == corev1.ConditionTrue {
			return string(appsv1.ReplicaSetReplicaFailure)
		} else if rs.Spec.Replicas != nil && rs.Status.AvailableReplicas < *rs.Spec.Replicas {
			return "Progressing"
		}
	} else {
		return "Progressing"
	}
	if rs.Spec.Replicas != nil && *rs.Spec.Replicas == 0 {
		return "ScaledDown"
	}
	return "Healthy"
}

func replicaSetIcon(status string) string {
	switch status {
	case "Progressing":
		return IconProgressing
	case "Healthy":
		return IconOK
	case string(appsv1.ReplicaSetReplicaFailure):
		return IconBad
	case "ScaledDown":
		return IconNeutral
	}
	return " "
}

func getReplicaSetCondition(status appsv1.ReplicaSetStatus, condType appsv1.ReplicaSetConditionType) *appsv1.ReplicaSetCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}
