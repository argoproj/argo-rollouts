package experiment

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// GetCurrentExperiment grabs the experiment that matches the current rollout
func GetCurrentExperiment(rollout *v1alpha1.Rollout, exList []*v1alpha1.Experiment) *v1alpha1.Experiment {
	var newExList []*v1alpha1.Experiment
	for i := range exList {
		ex := exList[i].DeepCopy()
		if ex != nil {
			newExList = append(newExList, ex)
		}
	}
	for i := range newExList {
		ex := newExList[i]
		if ex.Name == rollout.Status.Canary.CurrentExperiment {
			return ex
		}

	}
	// new Experiment does not exist.
	return nil
}

// GetOldExperiments returns the old experiments from list of experiments.
func GetOldExperiments(rollout *v1alpha1.Rollout, exList []*v1alpha1.Experiment) []*v1alpha1.Experiment {
	var allExs []*v1alpha1.Experiment
	currentEx := GetCurrentExperiment(rollout, exList)
	for i := range exList {
		ex := exList[i]
		// Filter out new experiment
		if currentEx != nil && ex.UID == currentEx.UID {
			continue
		}
		allExs = append(allExs, ex)
	}
	return allExs
}

// SortExperimentsByPodHash returns map with a podHash as a key and an array of experiments with that pod hash
func SortExperimentsByPodHash(exs []*v1alpha1.Experiment) map[string][]*v1alpha1.Experiment {
	podHashToEx := map[string][]*v1alpha1.Experiment{}
	if exs == nil {
		return podHashToEx
	}
	for i := range exs {
		ex := exs[i]
		podHash, ok := ex.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		if !ok {
			continue
		}
		podHashArray, ok := podHashToEx[podHash]
		if !ok {
			podHashArray = []*v1alpha1.Experiment{}
		}
		podHashArray = append(podHashArray, ex)
		podHashToEx[podHash] = podHashArray
	}
	return podHashToEx
}

// FilterExperimentsToDelete returns a list of experiments that should be deleted in the cases where:
// 1. The experiments has no pod hash label,
// 2. There is no ReplicaSet with the same pod hash as the experiments
// 3. The ReplicaSet that has the same pod hash as the experiments has a deletiontimestamp.
// Note: It is okay to use pod hash for filtering since the experiments's pod hash is originally derived from the new RS.
// Even if there is a library change during the lifetime of the experiments, the ReplicaSet's pod hash that the
// experiments references does not change.
func FilterExperimentsToDelete(exs []*v1alpha1.Experiment, olderRSs []*appsv1.ReplicaSet, limitSuccessful int32, limitUnsuccessful int32) []*v1alpha1.Experiment {
	olderRsPodHashes := map[string]bool{}
	for i := range olderRSs {
		rs := olderRSs[i]
		if podHash, ok := rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]; ok {
			olderRsPodHashes[podHash] = rs.DeletionTimestamp != nil
		}
	}
	sort.Sort(sort.Reverse(ExperimentByCreationTimestamp(exs)))

	var retainedSuccessful int32 = 0
	var retainedUnsuccessful int32 = 0
	exsToDelete := []*v1alpha1.Experiment{}
	for i := range exs {
		ex := exs[i]
		podHash, ok := ex.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		//Experiment does not have podHash Label
		if !ok {
			exsToDelete = append(exsToDelete, ex)
			continue
		}
		hasDeletionTimeStamp, ok := olderRsPodHashes[podHash]

		//Experiment does not have matching rs
		if !ok {
			exsToDelete = append(exsToDelete, ex)
			continue
		}

		//Experiment has matching rs but rs has deletiontimestamp
		if ok && hasDeletionTimeStamp {
			exsToDelete = append(exsToDelete, ex)
			continue
		}

		if ex.Status.Phase == v1alpha1.AnalysisPhaseSuccessful {
			if retainedSuccessful < limitSuccessful {
				retainedSuccessful++
			} else {
				exsToDelete = append(exsToDelete, ex)
			}
		} else if ex.Status.Phase == v1alpha1.AnalysisPhaseFailed ||
			ex.Status.Phase == v1alpha1.AnalysisPhaseError ||
			ex.Status.Phase == v1alpha1.AnalysisPhaseInconclusive {
			if retainedUnsuccessful < limitUnsuccessful {
				retainedUnsuccessful++
			} else {
				exsToDelete = append(exsToDelete, ex)
			}
		}
	}
	return exsToDelete
}
