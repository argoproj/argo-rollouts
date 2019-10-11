package analysis

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
)

//GetCurrentStepAnalysisRun filters the currentArs and returns the step based analysis run
func GetCurrentStepAnalysisRun(currentArs []*v1alpha1.AnalysisRun) *v1alpha1.AnalysisRun {
	for i := range currentArs {
		ar := currentArs[i]
		rolloutType, ok := ar.Labels[v1alpha1.RolloutTypeLabel]
		if ok && rolloutType == v1alpha1.RolloutTypeStepLabel {
			return ar
		}
	}
	return nil
}

//GetCurrentBackgroundAnalysisRun filters the currentArs and returns the background based analysis run
func GetCurrentBackgroundAnalysisRun(currentArs []*v1alpha1.AnalysisRun) *v1alpha1.AnalysisRun {
	for i := range currentArs {
		ar := currentArs[i]
		rolloutType, ok := ar.Labels[v1alpha1.RolloutTypeLabel]
		if ok && rolloutType == v1alpha1.RolloutTypeBackgroundRunLabel {
			return ar
		}
	}
	return nil
}

// FilterCurrentRolloutAnalysisRuns returns analysisRuns that match the analysisRuns listed in the rollout status
func FilterCurrentRolloutAnalysisRuns(analysisRuns []*v1alpha1.AnalysisRun, r *v1alpha1.Rollout) ([]*v1alpha1.AnalysisRun, []*v1alpha1.AnalysisRun) {
	return filterAnalysisRuns(analysisRuns, func(ar *v1alpha1.AnalysisRun) bool {
		if ar.Name == r.Status.Canary.CurrentStepAnalysisRun {
			return true
		}
		if ar.Name == r.Status.Canary.CurrentBackgroundAnalysisRun {
			return true
		}
		return false
	})
}

// FilterAnalysisRunsByRolloutType returns a list of analysisRuns that have the rollout-type of the typeFilter
func FilterAnalysisRunsByRolloutType(analysisRuns []*v1alpha1.AnalysisRun, typeFilter string) []*v1alpha1.AnalysisRun {
	analysisRunsByType, _ := filterAnalysisRuns(analysisRuns, func(ar *v1alpha1.AnalysisRun) bool {
		analysisRunType, ok := ar.Labels[v1alpha1.RolloutTypeLabel]
		if !ok || analysisRunType != typeFilter {
			return false
		}
		return true
	})
	return analysisRunsByType
}

// FilterAnalysisRunsByName returns the analysisRuns with the name provided
func FilterAnalysisRunsByName(analysisRuns []*v1alpha1.AnalysisRun, name string) *v1alpha1.AnalysisRun {
	analysisRunsByName, _ := filterAnalysisRuns(analysisRuns, func(ar *v1alpha1.AnalysisRun) bool {
		return ar.Name == name
	})
	if len(analysisRunsByName) == 1 {
		return analysisRunsByName[0]
	}
	return nil
}

func filterAnalysisRuns(ars []*v1alpha1.AnalysisRun, cond func(ar *v1alpha1.AnalysisRun) bool) ([]*v1alpha1.AnalysisRun, []*v1alpha1.AnalysisRun) {
	condTrue := []*v1alpha1.AnalysisRun{}
	condFalse := []*v1alpha1.AnalysisRun{}
	for i := range ars {
		if ars[i] == nil {
			continue
		}
		if cond(ars[i]) {
			condTrue = append(condTrue, ars[i])
		} else {
			condFalse = append(condFalse, ars[i])
		}
	}
	return condTrue, condFalse
}

// SortAnalysisRunByPodHash returns map with podHash as a key and an array of analysis runs as the value
// and an array of all the analysisRuns without the podHash label
func SortAnalysisRunByPodHash(ars []*v1alpha1.AnalysisRun) map[string][]*v1alpha1.AnalysisRun {
	podHashToAr := map[string][]*v1alpha1.AnalysisRun{}
	if ars == nil {
		return podHashToAr
	}
	for i := range ars {
		ar := ars[i]
		podHash, ok := ar.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		if !ok {
			continue
		}
		podHashArray, ok := podHashToAr[podHash]
		if !ok {
			podHashArray = []*v1alpha1.AnalysisRun{}
		}
		podHashArray = append(podHashArray, ar)
		podHashToAr[podHash] = podHashArray
	}
	return podHashToAr
}

// FilterAnalysisRunsToDelete returns a list of analysis runs that should be deleted in the cases of the analysis run
// has no pod hash, the analysis run has no matching replicaSet, or the rs has a deletiontimestamp
func FilterAnalysisRunsToDelete(ars []*v1alpha1.AnalysisRun, olderRSs []*appsv1.ReplicaSet) []*v1alpha1.AnalysisRun {
	olderRsPodHashes := map[string]bool{}
	for i := range olderRSs {
		rs := olderRSs[i]
		if podHash, ok := rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]; ok {
			olderRsPodHashes[podHash] = rs.DeletionTimestamp != nil
		}
	}
	arsToDelete := []*v1alpha1.AnalysisRun{}
	for i := range ars {
		ar := ars[i]
		podHash, ok := ar.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		//AnalysisRun does not have podHash Label
		if !ok {
			arsToDelete = append(arsToDelete, ar)
			continue
		}
		hasDeletionTimeStamp, ok := olderRsPodHashes[podHash]

		//AnalysisRun does not have matching rs
		if !ok {
			arsToDelete = append(arsToDelete, ar)
			continue
		}

		//AnalysisRun has matching rs but rs has deletiontimestamp
		if ok && hasDeletionTimeStamp {
			arsToDelete = append(arsToDelete, ar)
			continue
		}
	}
	return arsToDelete
}
