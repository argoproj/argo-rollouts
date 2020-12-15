package info

import (
	"sort"

	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/metricproviders/job"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
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
	Jobs         []JobInfo
}

type JobInfo struct {
	Metadata
	Status string
	Icon   string
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
			lastMeasurement := analysisutil.LastMeasurement(run, mr.Name)
			if lastMeasurement != nil && lastMeasurement.Metadata != nil {
				if jobName, ok := lastMeasurement.Metadata[job.JobNameKey]; ok {
					jobInfo := JobInfo{
						Metadata: Metadata{
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
