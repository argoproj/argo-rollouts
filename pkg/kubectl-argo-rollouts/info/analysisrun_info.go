package info

import (
	"sort"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/metricproviders/job"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
)

type AnalysisRunInfo v1alpha1.AnalysisRunInfo
type JobInfo v1alpha1.JobInfo

func getAnalysisRunInfo(ownerUID types.UID, allAnalysisRuns []*v1alpha1.AnalysisRun) []v1alpha1.AnalysisRunInfo {
	var arInfos []v1alpha1.AnalysisRunInfo
	for _, run := range allAnalysisRuns {
		if ownerRef(run.OwnerReferences, []types.UID{ownerUID}) == nil {
			continue
		}
		arInfo := v1alpha1.AnalysisRunInfo{
			ObjectMeta: v1.ObjectMeta{
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
					jobInfo := v1alpha1.JobInfo{
						ObjectMeta: v1.ObjectMeta{
							Name: jobName,
						},
						Icon:   analysisIcon(lastMeasurement.Phase),
						Status: string(lastMeasurement.Phase),
					}
					if lastMeasurement.StartedAt != nil {
						jobInfo.CreationTimestamp = *lastMeasurement.StartedAt
					}
					arInfo.Jobs = append(arInfo.Jobs, jobInfo)
				}
			}
		}
		arInfo.Icon = analysisIcon(run.Status.Phase)
		arInfo.Revision = int32(parseRevision(run.ObjectMeta.Annotations))

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
