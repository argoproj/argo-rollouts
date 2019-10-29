package info

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

type ExperimentInfo struct {
	Metadata
	Icon         string
	Revision     int
	Status       string
	ReplicaSets  []ReplicaSetInfo
	AnalysisRuns []AnalysisRunInfo
}

func getExperimentInfo(
	ro *v1alpha1.Rollout,
	allExperiments []*v1alpha1.Experiment,
	allReplicaSets []*appsv1.ReplicaSet,
	allAnalysisRuns []*v1alpha1.AnalysisRun,
	allPods []*corev1.Pod,
) []ExperimentInfo {

	var expInfos []ExperimentInfo
	for _, exp := range allExperiments {
		if ownerRef(exp.OwnerReferences, []types.UID{ro.UID}) == nil {
			continue
		}
		expInfo := ExperimentInfo{
			Metadata: Metadata{
				Name:              exp.Name,
				CreationTimestamp: exp.CreationTimestamp,
				UID:               exp.UID,
			},
			Status: string(exp.Status.Status),
		}
		expInfo.Icon = analysisIcon(exp.Status.Status)
		expInfo.Revision = parseRevision(exp.ObjectMeta.Annotations)
		expInfo.ReplicaSets = getReplicaSetInfo(exp.UID, nil, allReplicaSets, allPods)

		expInfos = append(expInfos, expInfo)
	}
	sort.Slice(expInfos[:], func(i, j int) bool {
		if expInfos[i].Revision > expInfos[j].Revision {
			return true
		}
		return expInfos[i].CreationTimestamp.Before(&expInfos[j].CreationTimestamp)
	})
	return expInfos
}

func analysisIcon(status v1alpha1.AnalysisStatus) string {
	switch status {
	case v1alpha1.AnalysisStatusSuccessful:
		return IconOK
	case v1alpha1.AnalysisStatusInconclusive:
		return IconUnknown
	case v1alpha1.AnalysisStatusFailed:
		return IconBad
	case v1alpha1.AnalysisStatusError:
		return IconWarning
	case v1alpha1.AnalysisStatusRunning:
		return IconProgressing
	case v1alpha1.AnalysisStatusPending:
		return IconWaiting
	}
	return " "
}
