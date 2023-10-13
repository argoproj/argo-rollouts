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

		arInfo.SpecAndStatus = &rollout.AnalysisRunSpecAndStatus{
			Spec:   &run.Spec,
			Status: &run.Status,
		}

		if run.Spec.Metrics != nil {
			for _, metric := range run.Spec.Metrics {

				metrics := rollout.Metrics{
					Name:             metric.Name,
					SuccessCondition: metric.SuccessCondition,
				}

				if metric.InconclusiveLimit != nil {
					metrics.InconclusiveLimit = metric.InconclusiveLimit.IntVal
				} else {
					metrics.InconclusiveLimit = 0
				}

				if metric.Count != nil {
					metrics.Count = metric.Count.IntVal
				} else {
					metrics.Count = 0
				}

				if metric.FailureLimit != nil {
					metrics.FailureLimit = metric.FailureLimit.IntVal
				} else {
					metrics.FailureLimit = 0
				}

				arInfo.Metrics = append(arInfo.Metrics, &metrics)
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
							Icon:       analysisIcon(measurement.Phase),
							Status:     string(measurement.Phase),
							StartedAt:  measurement.StartedAt,
							MetricName: mr.Name,
						}
						if measurement.StartedAt != nil {
							jobInfo.ObjectMeta.CreationTimestamp = *measurement.StartedAt
						}
						arInfo.Jobs = append(arInfo.Jobs, &jobInfo)
					}
				} else {
					nonJobInfo := rollout.NonJobInfo{
						Value:      measurement.Value,
						Status:     string(measurement.Phase),
						StartedAt:  measurement.StartedAt,
						MetricName: mr.Name,
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
