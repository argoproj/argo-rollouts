package info

import (
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/metricproviders/job"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
)

type AnalysisRunInfo struct {
	metav1.TypeMeta `json:",inline"`
	Metadata        `json:"metadata,omitempty"`
	Spec            AnalysisRunInfoSpec `json:"spec,omitempty"`
}

type AnalysisRunInfoSpec struct {
	Icon         string    `json:"-"`
	Revision     int       `json:"revision"`
	Status       string    `json:"status"`
	Successful   int32     `json:"successful"`
	Failed       int32     `json:"failed"`
	Inconclusive int32     `json:"inconclusive"`
	Error        int32     `json:"error"`
	Jobs         []JobInfo `json:"jobs,omitempty"`
}

type JobInfo struct {
	Metadata
	Status string `json:"status"`
	Icon   string `json:"-"`
}

func getAnalysisRunInfo(ownerUID types.UID, allAnalysisRuns []*v1alpha1.AnalysisRun) []AnalysisRunInfo {
	var arInfos []AnalysisRunInfo
	for _, run := range allAnalysisRuns {
		if ownerRef(run.OwnerReferences, []types.UID{ownerUID}) == nil {
			continue
		}
		arInfo := AnalysisRunInfo{
			Metadata: Metadata{
				ObjectMeta: v1.ObjectMeta{
					Name:              run.Name,
					Namespace:         run.Namespace,
					CreationTimestamp: run.CreationTimestamp,
					UID:               run.UID,
				},
			},
		}
		arInfo.Spec.Status = string(run.Status.Phase)
		for _, mr := range run.Status.MetricResults {
			arInfo.Spec.Successful += mr.Successful
			arInfo.Spec.Failed += mr.Failed
			arInfo.Spec.Inconclusive += mr.Inconclusive
			arInfo.Spec.Error += mr.Error
			lastMeasurement := analysisutil.LastMeasurement(run, mr.Name)
			if lastMeasurement != nil && lastMeasurement.Metadata != nil {
				if jobName, ok := lastMeasurement.Metadata[job.JobNameKey]; ok {
					jobInfo := JobInfo{
						Metadata: Metadata{
							ObjectMeta: v1.ObjectMeta{
								Name: jobName,
							},
						},
						Icon:   analysisIcon(lastMeasurement.Phase),
						Status: string(lastMeasurement.Phase),
					}
					if lastMeasurement.StartedAt != nil {
						jobInfo.CreationTimestamp = *lastMeasurement.StartedAt
					}
					arInfo.Spec.Jobs = append(arInfo.Spec.Jobs, jobInfo)
				}
			}
		}
		arInfo.Spec.Icon = analysisIcon(run.Status.Phase)
		arInfo.Spec.Revision = parseRevision(run.ObjectMeta.Annotations)

		arInfos = append(arInfos, arInfo)
	}
	sort.Slice(arInfos[:], func(i, j int) bool {
		if arInfos[i].Spec.Revision != arInfos[j].Spec.Revision {
			return arInfos[i].Spec.Revision > arInfos[j].Spec.Revision
		}
		if arInfos[i].CreationTimestamp != arInfos[j].CreationTimestamp {
			return arInfos[i].CreationTimestamp.Before(&arInfos[j].CreationTimestamp)
		}
		return arInfos[i].Name > arInfos[j].Name
	})
	return arInfos
}
