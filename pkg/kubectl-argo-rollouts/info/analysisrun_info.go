package info

import (
	"sort"

	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

type AnalysisRunInfo struct {
	Metadata
	Icon         string
	Revision     int
	Status       string
	Successful   int32
	Failed       int32
	Inconclusive int32
	Error        int32
}

func getAnalysisRunInfo(ownerUID types.UID, allAnalysisRuns []*v1alpha1.AnalysisRun) []AnalysisRunInfo {
	var arInfos []AnalysisRunInfo
	for _, run := range allAnalysisRuns {
		if ownerRef(run.OwnerReferences, []types.UID{ownerUID}) == nil {
			continue
		}
		arInfo := AnalysisRunInfo{
			Metadata: Metadata{
				Name:              run.Name,
				Namespace:         run.Namespace,
				CreationTimestamp: run.CreationTimestamp,
				UID:               run.UID,
			},
		}
		arInfo.Status = string(run.Status.Phase)
		for _, mr := range run.Status.MetricResults {
			arInfo.Successful += mr.Successful
			arInfo.Failed += mr.Failed
			arInfo.Inconclusive += mr.Inconclusive
			arInfo.Error += mr.Error
		}
		arInfo.Icon = analysisIcon(run.Status.Phase)
		arInfo.Revision = parseRevision(run.ObjectMeta.Annotations)

		arInfos = append(arInfos, arInfo)
	}
	sort.Slice(arInfos[:], func(i, j int) bool {
		if arInfos[i].Revision != arInfos[j].Revision {
			return arInfos[i].Revision > arInfos[j].Revision
		}
		if arInfos[i].CreationTimestamp != arInfos[j].CreationTimestamp {
			return arInfos[i].CreationTimestamp.Before(&arInfos[j].CreationTimestamp)
		}
		return arInfos[i].Name > arInfos[j].Name
	})
	return arInfos
}
