package analysis

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func GetCurrentAnalysisRunByType(currentArs []*v1alpha1.AnalysisRun, kind string) *v1alpha1.AnalysisRun {
	for i := range currentArs {
		ar := currentArs[i]
		rolloutType, ok := ar.Labels[v1alpha1.RolloutTypeLabel]
		if ok && rolloutType == kind {
			return ar
		}
	}
	return nil
}

// FilterCurrentRolloutAnalysisRuns returns analysisRuns that match the analysisRuns listed in the rollout status
func FilterCurrentRolloutAnalysisRuns(analysisRuns []*v1alpha1.AnalysisRun, r *v1alpha1.Rollout) (CurrentAnalysisRuns, []*v1alpha1.AnalysisRun) {
	currArs := CurrentAnalysisRuns{}
	otherArs := []*v1alpha1.AnalysisRun{}
	getArName := func(s *v1alpha1.RolloutAnalysisRunStatus) string {
		if s == nil {
			return ""
		}
		return s.Name
	}
	for i := range analysisRuns {
		ar := analysisRuns[i]
		if ar != nil {
			switch ar.Name {
			case getArName(r.Status.Canary.CurrentStepAnalysisRunStatus):
				currArs.CanaryStep = ar
			case getArName(r.Status.Canary.CurrentBackgroundAnalysisRunStatus):
				currArs.CanaryBackground = ar
			case getArName(r.Status.BlueGreen.PrePromotionAnalysisRunStatus):
				currArs.BlueGreenPrePromotion = ar
			case getArName(r.Status.BlueGreen.PostPromotionAnalysisRunStatus):
				currArs.BlueGreenPostPromotion = ar
			default:
				otherArs = append(otherArs, ar)
			}
		}
	}
	return currArs, otherArs
}

// FilterAnalysisRunsByRolloutType returns a list of analysisRuns that have the rollout-type of the typeFilter
func FilterAnalysisRunsByRolloutType(analysisRuns []*v1alpha1.AnalysisRun, typeFilter string) []*v1alpha1.AnalysisRun {
	analysisRunsByType, _ := FilterAnalysisRuns(analysisRuns, func(ar *v1alpha1.AnalysisRun) bool {
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
	analysisRunsByName, _ := FilterAnalysisRuns(analysisRuns, func(ar *v1alpha1.AnalysisRun) bool {
		return ar.Name == name
	})
	if len(analysisRunsByName) == 1 {
		return analysisRunsByName[0]
	}
	return nil
}

func FilterAnalysisRuns(ars []*v1alpha1.AnalysisRun, cond func(ar *v1alpha1.AnalysisRun) bool) ([]*v1alpha1.AnalysisRun, []*v1alpha1.AnalysisRun) {
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

// SortAnalysisRunByPodHash returns map with a podHash as a key and an array of analysisRuns with that pod hash
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

// AnalysisRunByCreationTimestamp sorts a list of AnalysisRun by creation timestamp
type AnalysisRunByCreationTimestamp []*v1alpha1.AnalysisRun

func (o AnalysisRunByCreationTimestamp) Len() int      { return len(o) }
func (o AnalysisRunByCreationTimestamp) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o AnalysisRunByCreationTimestamp) Less(i, j int) bool {
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}

// FilterAnalysisRunsToDelete returns a list of analysis runs that should be deleted in the cases where:
// 1. The analysis run has no pod hash label,
// 2. There is no ReplicaSet with the same pod hash as the analysis run
// 3. The ReplicaSet that has the same pod hash as the analysis run has a deletiontimestamp.
// Note: It is okay to use pod hash for filtering since the analysis run's pod hash is originally derived from the new RS.
// Even if there is a library change during the lifetime of the analysis run, the ReplicaSet's pod hash that the analysis
// run references does not change.
func FilterAnalysisRunsToDelete(ars []*v1alpha1.AnalysisRun, allRSs []*appsv1.ReplicaSet, limitSuccessful int32, limitUnsuccessful int32) []*v1alpha1.AnalysisRun {
	olderRsPodHashes := map[string]bool{}
	for i := range allRSs {
		rs := allRSs[i]
		if rs == nil {
			continue
		}
		if podHash, ok := rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]; ok {
			olderRsPodHashes[podHash] = rs.DeletionTimestamp != nil
		}
	}
	sort.Sort(sort.Reverse(AnalysisRunByCreationTimestamp(ars)))

	var retainedSuccessful int32 = 0
	var retainedUnsuccessful int32 = 0
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

		if ar.Status.Phase == v1alpha1.AnalysisPhaseSuccessful {
			if retainedSuccessful < limitSuccessful {
				retainedSuccessful++
			} else {
				arsToDelete = append(arsToDelete, ar)
			}
		} else if ar.Status.Phase == v1alpha1.AnalysisPhaseFailed ||
			ar.Status.Phase == v1alpha1.AnalysisPhaseError ||
			ar.Status.Phase == v1alpha1.AnalysisPhaseInconclusive {
			if retainedUnsuccessful < limitUnsuccessful {
				retainedUnsuccessful++
			} else {
				arsToDelete = append(arsToDelete, ar)
			}
		}
	}
	return arsToDelete
}
