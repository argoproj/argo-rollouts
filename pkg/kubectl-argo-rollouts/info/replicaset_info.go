package info

import (
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/duration"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

type ReplicaSetInfo struct {
	metav1.TypeMeta `json:",inline"`
	Metadata        `json:"metadata,omitempty"`
	Spec            ReplicaSetInfoSpec `json:"spec,omitempty"`
}

type ReplicaSetInfoSpec struct {
	Status            string    `json:"status"`
	Icon              string    `json:"-"`
	Revision          int       `json:"revision"`
	Stable            bool      `json:"stable"`
	Canary            bool      `json:"canary"`
	Active            bool      `json:"active"`
	Preview           bool      `json:"preview"`
	Replicas          int32     `json:"replicas"`
	Available         int32     `json:"available"`
	Template          string    `json:"template,omitempty"`
	ScaleDownDeadline string    `json:"scaleDownDeadline,omitempty"`
	Images            []string  `json:"images,omitempty"`
	Pods              []PodInfo `json:"pods,omitempty"`
}

func getReplicaSetInfo(ownerUID types.UID, ro *v1alpha1.Rollout, allReplicaSets []*appsv1.ReplicaSet, allPods []*corev1.Pod) []ReplicaSetInfo {
	var rsInfos []ReplicaSetInfo
	for _, rs := range allReplicaSets {
		// if owned by replicaset
		if ownerRef(rs.OwnerReferences, []types.UID{ownerUID}) == nil {
			continue
		}
		rsStatus := getReplicaSetHealth(rs)
		rsInfo := ReplicaSetInfo{
			Metadata: Metadata{
				ObjectMeta: metav1.ObjectMeta{
					Name:              rs.Name,
					Namespace:         rs.Namespace,
					CreationTimestamp: rs.CreationTimestamp,
					UID:               rs.UID,
				},
			},
			Spec: ReplicaSetInfoSpec{
				Status:            rsStatus,
				Replicas:          rs.Status.Replicas,
				Available:         rs.Status.AvailableReplicas,
				Icon:              replicaSetIcon(rsStatus),
				Revision:          parseRevision(rs.ObjectMeta.Annotations),
				Template:          parseExperimentTemplateName(rs.ObjectMeta.Annotations),
				ScaleDownDeadline: parseScaleDownDeadline(rs.ObjectMeta.Annotations),
			},
		}

		if ro != nil {
			if ro.Spec.Strategy.Canary != nil && rs.Labels != nil {
				if ro.Status.StableRS == rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
					rsInfo.Spec.Stable = true
				} else if ro.Status.CurrentPodHash == rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
					rsInfo.Spec.Canary = true
				}
			}
			if ro.Spec.Strategy.BlueGreen != nil {
				if ro.Status.BlueGreen.ActiveSelector == rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
					rsInfo.Spec.Active = true
				} else if ro.Status.BlueGreen.PreviewSelector == rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
					rsInfo.Spec.Preview = true
				}
			}
		}

		for _, ctr := range rs.Spec.Template.Spec.Containers {
			rsInfo.Spec.Images = append(rsInfo.Spec.Images, ctr.Image)
		}
		rsInfos = append(rsInfos, rsInfo)
	}
	sort.Slice(rsInfos[:], func(i, j int) bool {
		if rsInfos[i].Spec.Revision != rsInfos[j].Spec.Revision {
			return rsInfos[i].Spec.Revision > rsInfos[j].Spec.Revision
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

func (rs ReplicaSetInfoSpec) ScaleDownDelay() string {
	if deadline, err := time.Parse(time.RFC3339, rs.ScaleDownDeadline); err == nil {
		now := metav1.Now().Time
		if deadline.Before(now) {
			return "passed"
		}
		return duration.HumanDuration(deadline.Sub(now))
	}
	return ""
}
