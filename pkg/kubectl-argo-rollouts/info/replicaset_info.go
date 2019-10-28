package info

import (
	"sort"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
)

type ReplicaSetInfo struct {
	Name              string
	UID               types.UID
	CreationTimestamp metav1.Time
	Status            string
	Icon              string
	Revision          int
	Stable            bool
	Canary            bool
	Active            bool
	Preview           bool
	Available         int32
	Images            []string
	Pods              []PodInfo
}

func (r *ReplicaSetInfo) Age() time.Duration {
	return metav1.Now().Sub(r.CreationTimestamp.Time)
}

func getReplicaSetInfo(ro *v1alpha1.Rollout, allReplicaSets []*appsv1.ReplicaSet, allPods []*corev1.Pod) []ReplicaSetInfo {
	var rsInfos []ReplicaSetInfo
	for _, rs := range allReplicaSets {
		// if owned by replicaset
		if ownerRef(rs.OwnerReferences, []types.UID{ro.UID}) == nil {
			continue
		}
		rsInfo := ReplicaSetInfo{
			Name:              rs.Name,
			UID:               rs.UID,
			CreationTimestamp: rs.CreationTimestamp,
			Status:            getReplicaSetHealth(rs),
			Available:         rs.Status.AvailableReplicas,
		}
		rsInfo.Icon = replicaSetIcon(rsInfo.Status)
		if rs.Annotations != nil {
			if revision, err := strconv.Atoi(rs.Annotations[annotations.RevisionAnnotation]); err == nil {
				rsInfo.Revision = revision
			}
		}
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
		for _, ctr := range rs.Spec.Template.Spec.Containers {
			rsInfo.Images = append(rsInfo.Images, ctr.Image)
		}
		rsInfos = append(rsInfos, rsInfo)
	}
	sort.Slice(rsInfos[:], func(i, j int) bool {
		return rsInfos[i].Revision > rsInfos[j].Revision
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
