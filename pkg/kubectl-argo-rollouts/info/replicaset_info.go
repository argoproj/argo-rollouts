package info

import (
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/duration"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

func GetReplicaSetInfo(ownerUID types.UID, ro *v1alpha1.Rollout, allReplicaSets []*appsv1.ReplicaSet, allPods []*corev1.Pod) []*rollout.ReplicaSetInfo {
	var rsInfos []*rollout.ReplicaSetInfo
	for _, rs := range allReplicaSets {
		// if owned by replicaset
		if ownerRef(rs.OwnerReferences, []types.UID{ownerUID}) == nil {
			continue
		}
		rsInfo := &rollout.ReplicaSetInfo{
			ObjectMeta: &v1.ObjectMeta{
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
		rsInfo.Revision = int64(parseRevision(rs.ObjectMeta.Annotations))
		rsInfo.Template = parseExperimentTemplateName(rs.ObjectMeta.Annotations)
		rsInfo.ScaleDownDeadline = parseScaleDownDeadline(rs.ObjectMeta.Annotations)

		if ro != nil {
			podTemplateHash := replicasetutil.GetPodTemplateHash(rs)
			if ro.Spec.Strategy.Canary != nil {
				stableRsIsPing := trafficrouting.IsStablePing(ro)
				if ro.Status.StableRS == podTemplateHash {
					rsInfo.Stable = true
					if trafficrouting.IsPingPongEnabled(ro) {
						rsInfo.Ping = stableRsIsPing
						rsInfo.Pong = !stableRsIsPing
					}
				} else if ro.Status.CurrentPodHash == podTemplateHash {
					rsInfo.Canary = true
					if trafficrouting.IsPingPongEnabled(ro) {
						rsInfo.Ping = !stableRsIsPing
						rsInfo.Pong = stableRsIsPing
					}
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
		if rsInfos[i].ObjectMeta.CreationTimestamp != rsInfos[j].ObjectMeta.CreationTimestamp {
			rsInfos[i].ObjectMeta.CreationTimestamp.Before(&rsInfos[j].ObjectMeta.CreationTimestamp)
		}
		return rsInfos[i].ObjectMeta.Name < rsInfos[j].ObjectMeta.Name
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

func ScaleDownDelay(rs rollout.ReplicaSetInfo) string {
	if deadline, err := time.Parse(time.RFC3339, rs.ScaleDownDeadline); err == nil {
		now := timeutil.MetaNow().Time
		if deadline.Before(now) {
			return "passed"
		}
		return duration.HumanDuration(deadline.Sub(now))
	}
	return ""
}
