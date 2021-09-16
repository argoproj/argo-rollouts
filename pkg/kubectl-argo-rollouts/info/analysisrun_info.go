package info

import (
	"sort"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/metricproviders/job"
	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
)

func getAnalysisRunInfo(ownerUID types.UID, allAnalysisRuns []*v1alpha1.AnalysisRun) []*rollout.AnalysisRunInfo {
	var arInfos []*rollout.AnalysisRunInfo
	for _, run := range allAnalysisRuns {
		if ownerRef(run.OwnerReferences, []types.UID{ownerUID}) == nil {
			continue
		}
		arInfo := rollout.AnalysisRunInfo{
			ObjectMeta: &v1.ObjectMeta{
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
			lastMeasurement := analysisutil.LastMeasurement(run, mr.Name)
			if lastMeasurement != nil && lastMeasurement.Metadata != nil {
				if jobName, ok := lastMeasurement.Metadata[job.JobNameKey]; ok {
					jobInfo := rollout.JobInfo{
						ObjectMeta: &v1.ObjectMeta{
							Name: jobName,
						},
						Icon:   analysisIcon(lastMeasurement.Phase),
						Status: string(lastMeasurement.Phase),
					}
					if lastMeasurement.StartedAt != nil {
						jobInfo.ObjectMeta.CreationTimestamp = *lastMeasurement.StartedAt
					}
					arInfo.Jobs = append(arInfo.Jobs, &jobInfo)
				}
			}
		}
		arInfo.Icon = analysisIcon(run.Status.Phase)
		arInfo.Revision = int32(parseRevision(run.ObjectMeta.Annotations))

		arInfos = append(arInfos, &arInfo)
	}
	sort.Slice(arInfos[:], func(i, j int) bool {
		if arInfos[i].Revision != arInfos[j].Revision {
			return arInfos[i].Revision > arInfos[j].Revision
		}
		if arInfos[i].ObjectMeta.CreationTimestamp != arInfos[j].ObjectMeta.CreationTimestamp {
			return arInfos[i].ObjectMeta.CreationTimestamp.Before(&arInfos[j].ObjectMeta.CreationTimestamp)
		}
		return arInfos[i].ObjectMeta.Name > arInfos[j].ObjectMeta.Name
	})
	return arInfos
}
