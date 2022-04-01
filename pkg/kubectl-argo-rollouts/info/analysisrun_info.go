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
		if run.Spec.Metrics != nil {
			arInfo.SuccessCondition = run.Spec.Metrics[0].SuccessCondition

			if run.Spec.Metrics[0].Count != nil {
				arInfo.Count = run.Spec.Metrics[0].Count.IntVal
			}

			if run.Spec.Metrics[0].InconclusiveLimit != nil {
				arInfo.InconclusiveLimit = run.Spec.Metrics[0].InconclusiveLimit.IntVal
			}
			if run.Spec.Metrics[0].FailureLimit != nil {
				arInfo.FailureLimit = run.Spec.Metrics[0].FailureLimit.IntVal
			}
		}
		arInfo.Status = string(run.Status.Phase)
		for _, mr := range run.Status.MetricResults {
			arInfo.Successful += mr.Successful
			arInfo.Failed += mr.Failed
			arInfo.Inconclusive += mr.Inconclusive
			arInfo.Error += mr.Error
			for _, measurement := range analysisutil.ArrayMeasurement(run, mr.Name) {
				if measurement.Metadata != nil {
					if jobName, ok := measurement.Metadata[job.JobNameKey]; ok {
						jobInfo := rollout.JobInfo{
							ObjectMeta: &v1.ObjectMeta{
								Name: jobName,
							},
							Icon:      analysisIcon(measurement.Phase),
							Status:    string(measurement.Phase),
							StartedAt: measurement.StartedAt,
						}
						if measurement.StartedAt != nil {
							jobInfo.ObjectMeta.CreationTimestamp = *measurement.StartedAt
						}
						arInfo.Jobs = append(arInfo.Jobs, &jobInfo)
					}
				} else {
					nonJobInfo := rollout.NonJobInfo{
						Value:     measurement.Value,
						Status:    string(measurement.Phase),
						StartedAt: measurement.StartedAt,
					}
					arInfo.NonJobInfo = append(arInfo.NonJobInfo, &nonJobInfo)
				}

			}
		}
		arInfo.Icon = analysisIcon(run.Status.Phase)
		arInfo.Revision = int64(parseRevision(run.ObjectMeta.Annotations))
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
