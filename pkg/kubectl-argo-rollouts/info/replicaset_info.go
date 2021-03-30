package info

import (
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/duration"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

type ReplicaSetInfo v1alpha1.ReplicaSetInfo
type PodInfo v1alpha1.PodInfo

func GetReplicaSetInfo(ownerUID types.UID, ro *v1alpha1.Rollout, allReplicaSets []*appsv1.ReplicaSet, allPods []*corev1.Pod) []v1alpha1.ReplicaSetInfo {
	var rsInfos []v1alpha1.ReplicaSetInfo
	for _, rs := range allReplicaSets {
		// if owned by replicaset
		if ownerRef(rs.OwnerReferences, []types.UID{ownerUID}) == nil {
			continue
		}
		rsInfo := v1alpha1.ReplicaSetInfo{
			ObjectMeta: v1.ObjectMeta{
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
		rsInfo.Revision = int32(parseRevision(rs.ObjectMeta.Annotations))
		rsInfo.Template = parseExperimentTemplateName(rs.ObjectMeta.Annotations)
		rsInfo.ScaleDownDeadline = parseScaleDownDeadline(rs.ObjectMeta.Annotations)

		if ro != nil {
			podTemplateHash := replicasetutil.GetPodTemplateHash(rs)
			if ro.Spec.Strategy.Canary != nil {
				if ro.Status.StableRS == podTemplateHash {
					rsInfo.Stable = true
				} else if ro.Status.CurrentPodHash == podTemplateHash {
					rsInfo.Canary = true
				}
			}
			if ro.Spec.Strategy.BlueGreen != nil {
				if ro.Status.StableRS == podTemplateHash {
					rsInfo.Stable = true
				}
				if ro.Status.BlueGreen.ActiveSelector == podTemplateHash {
					rsInfo.Active = true
				} else if ro.Status.BlueGreen.PreviewSelector == podTemplateHash {
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

func ScaleDownDelay(rs v1alpha1.ReplicaSetInfo) string {
	if deadline, err := time.Parse(time.RFC3339, rs.ScaleDownDeadline); err == nil {
		now := metav1.Now().Time
		if deadline.Before(now) {
			return "passed"
		}
		return duration.HumanDuration(deadline.Sub(now))
	}
	return ""
}
