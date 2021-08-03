package experiment

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestGetExperiments(t *testing.T) {
	r := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
	}
	r.Status.Canary.CurrentExperiment = "foo-exp"
	ex1 := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo-exp",
			UID:  uuid.NewUUID(),
		},
	}
	ex2 := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo-2",
			UID:  uuid.NewUUID(),
		},
	}
	allExperiments := []*v1alpha1.Experiment{ex1, ex2}

	assert.Equal(t, GetCurrentExperiment(r, allExperiments), ex1)
	assert.Nil(t, GetCurrentExperiment(r, []*v1alpha1.Experiment{ex2}), ex1)
	assert.Equal(t, GetOldExperiments(r, allExperiments), []*v1alpha1.Experiment{ex2})

}

func TestSortExperimentsByPodHash(t *testing.T) {
	emptyMap := SortExperimentsByPodHash(nil)
	assert.NotNil(t, 0)
	assert.Len(t, emptyMap, 0)
	name := "a"
	ex := func(podHash *string) *v1alpha1.Experiment {
		labels := map[string]string{}
		if podHash != nil {
			labels[v1alpha1.DefaultRolloutUniqueLabelKey] = *podHash
		}
		a := &v1alpha1.Experiment{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: labels,
			},
		}
		name = name + "a"
		return a
	}
	exs := []*v1alpha1.Experiment{
		ex(nil),
		ex(pointer.StringPtr("ab")),
		ex(pointer.StringPtr("abc")),
		ex(pointer.StringPtr("abc")),
	}
	exMap := SortExperimentsByPodHash(exs)
	assert.Len(t, exMap, 2)
	assert.Len(t, exMap["ab"], 1)
	assert.Len(t, exMap["abc"], 2)

}

func TestFilterExperimentsToDelete(t *testing.T) {
	rs := func(podHash string, hasDeletionTimeStamp bool) *appsv1.ReplicaSet {
		var time *metav1.Time
		if hasDeletionTimeStamp {
			now := metav1.Now()
			time = &now
		}
		return &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Labels:            map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: podHash},
				DeletionTimestamp: time,
			},
		}
	}
	ex := func(podHash string) *v1alpha1.Experiment {
		return &v1alpha1.Experiment{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: podHash},
			},
		}
	}

	exWithNoPodHash := &v1alpha1.Experiment{}
	exNoDeletion := ex("a")
	exWithDeletedRS := ex("b")
	exWithNoMatchingRS := ex("c")

	validRS := rs("a", false)
	deletedRS := rs("b", true)
	exs := []*v1alpha1.Experiment{
		exNoDeletion,
		exWithNoPodHash,
		exWithDeletedRS,
		exWithNoMatchingRS,
	}
	olderRSs := []*appsv1.ReplicaSet{
		validRS,
		deletedRS,
	}
	filteredArs := FilterExperimentsToDelete(exs, olderRSs, 5, 5)
	assert.Len(t, filteredArs, 3)
	assert.NotContains(t, filteredArs, exNoDeletion)
	assert.Contains(t, filteredArs, exWithNoPodHash)
	assert.Contains(t, filteredArs, exWithDeletedRS)
	assert.Contains(t, filteredArs, exWithNoMatchingRS)
}

func TestFilterExperimentsToDeleteByLimit(t *testing.T) {
	rs := func(podHash string) *appsv1.ReplicaSet {
		return &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: podHash},
			},
		}
	}
	ex := func(podHash string, phase v1alpha1.AnalysisPhase) *v1alpha1.Experiment {
		return &v1alpha1.Experiment{
			ObjectMeta: metav1.ObjectMeta{
				Labels:            map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: podHash},
				CreationTimestamp: metav1.Now(),
			},
			Status: v1alpha1.ExperimentStatus{
				Phase: phase,
			},
		}
	}

	arS0 := ex("a", v1alpha1.AnalysisPhaseSuccessful)
	arS1 := ex("a", v1alpha1.AnalysisPhaseSuccessful)
	arS2 := ex("a", v1alpha1.AnalysisPhaseSuccessful)
	arS3 := ex("a", v1alpha1.AnalysisPhaseSuccessful)
	arS4 := ex("a", v1alpha1.AnalysisPhaseSuccessful)
	arF0 := ex("a", v1alpha1.AnalysisPhaseFailed)
	arF1 := ex("a", v1alpha1.AnalysisPhaseError)
	arF2 := ex("a", v1alpha1.AnalysisPhaseInconclusive)
	arF3 := ex("a", v1alpha1.AnalysisPhaseFailed)
	arF4 := ex("a", v1alpha1.AnalysisPhaseFailed)

	validRS := rs("a")
	ars := []*v1alpha1.Experiment{
		arS0, arF0, arS1, arF1, arS2, arF2, arS3, arF3, arS4, arF4,
	}
	olderRSs := []*appsv1.ReplicaSet{
		validRS,
	}

	assert.Len(t, FilterExperimentsToDelete(ars, olderRSs, 1, 0), 9)
	assert.Len(t, FilterExperimentsToDelete(ars, olderRSs, 5, 0), 5)
	assert.Len(t, FilterExperimentsToDelete(ars, olderRSs, 10, 0), 5)

	assert.Len(t, FilterExperimentsToDelete(ars, olderRSs, 0, 1), 9)
	assert.Len(t, FilterExperimentsToDelete(ars, olderRSs, 0, 5), 5)
	assert.Len(t, FilterExperimentsToDelete(ars, olderRSs, 0, 10), 5)

	assert.Len(t, FilterExperimentsToDelete(ars, olderRSs, 0, 0), 10)
	assert.Len(t, FilterExperimentsToDelete(ars, olderRSs, 4, 4), 2)
	assert.Len(t, FilterExperimentsToDelete(ars, olderRSs, 10, 10), 0)
}
