package analysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestGetCurrentAnalysisRunByKind(t *testing.T) {
	arsWithSteps := []*v1alpha1.AnalysisRun{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
				Labels: map[string]string{
					v1alpha1.RolloutTypeLabel: v1alpha1.RolloutTypeStepLabel,
				},
			},
		},
	}
	currAr := GetCurrentAnalysisRunByType(arsWithSteps, v1alpha1.RolloutTypeStepLabel)
	assert.Equal(t, arsWithSteps[0], currAr)
	arsWithNoSteps := []*v1alpha1.AnalysisRun{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
				Labels: map[string]string{
					v1alpha1.RolloutTypeLabel: v1alpha1.RolloutTypeBackgroundRunLabel,
				},
			},
		},
	}
	currAr = GetCurrentAnalysisRunByType(arsWithNoSteps, v1alpha1.RolloutTypeStepLabel)
	assert.Nil(t, currAr)
}

func TestFilterCurrentRolloutAnalysisRuns(t *testing.T) {
	ars := []*v1alpha1.AnalysisRun{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "baz",
			},
		},
		nil,
	}
	t.Run("Canary", func(t *testing.T) {
		r := &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				Canary: v1alpha1.CanaryStatus{
					CurrentStepAnalysisRunStatus: &v1alpha1.RolloutAnalysisRunStatus{
						Name: "foo",
					},
					CurrentBackgroundAnalysisRunStatus: &v1alpha1.RolloutAnalysisRunStatus{
						Name: "bar",
					},
				},
			},
		}
		currentArs, nonCurrentArs := FilterCurrentRolloutAnalysisRuns(ars, r)
		assert.Len(t, nonCurrentArs, 1)
		assert.Equal(t, currentArs.CanaryStep, ars[0])
		assert.Equal(t, currentArs.CanaryBackground, ars[1])
		assert.Nil(t, currentArs.BlueGreenPostPromotion)
		assert.Nil(t, currentArs.BlueGreenPrePromotion)

	})
	t.Run("BlueGreen", func(t *testing.T) {
		r := &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				BlueGreen: v1alpha1.BlueGreenStatus{
					PrePromotionAnalysisRunStatus: &v1alpha1.RolloutAnalysisRunStatus{
						Name: "foo",
					},
					PostPromotionAnalysisRunStatus: &v1alpha1.RolloutAnalysisRunStatus{
						Name: "bar",
					},
				},
			},
		}
		currentArs, nonCurrentArs := FilterCurrentRolloutAnalysisRuns(ars, r)
		assert.Len(t, nonCurrentArs, 1)
		assert.Equal(t, currentArs.BlueGreenPrePromotion, ars[0])
		assert.Equal(t, currentArs.BlueGreenPostPromotion, ars[1])
		assert.Nil(t, currentArs.CanaryBackground)
		assert.Nil(t, currentArs.CanaryStep)
	})
}

func TestFilterAnalysisRunsByName(t *testing.T) {
	ars := []*v1alpha1.AnalysisRun{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
			},
		},
	}
	filteredArs := FilterAnalysisRunsByName(ars, "foo")
	assert.Nil(t, filteredArs)
	filteredArs = FilterAnalysisRunsByName(ars, "bar")
	assert.Equal(t, ars[2].Name, filteredArs.Name)
}

func TestFilterAnalysisRunsByRolloutType(t *testing.T) {
	ars := []*v1alpha1.AnalysisRun{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "foo",
				Labels: map[string]string{
					v1alpha1.RolloutTypeLabel: "not-a-type",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
				Labels: map[string]string{
					v1alpha1.RolloutTypeLabel: v1alpha1.RolloutTypeStepLabel,
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "baz",
				Labels: map[string]string{
					v1alpha1.RolloutTypeLabel: v1alpha1.RolloutTypeBackgroundRunLabel,
				},
			},
		},
	}
	filteredArs := FilterAnalysisRunsByRolloutType(ars, v1alpha1.RolloutTypeBackgroundRunLabel)
	assert.Len(t, filteredArs, 1)
	assert.Equal(t, ars[2].Name, filteredArs[0].Name)
}

func TestFilterAnalysisRunsToDelete(t *testing.T) {
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
	ar := func(podHash string) *v1alpha1.AnalysisRun {
		return &v1alpha1.AnalysisRun{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: podHash},
			},
		}
	}

	arWithNoPodHash := &v1alpha1.AnalysisRun{}
	arNoDeletion := ar("a")
	arWithDeletedRS := ar("b")
	arWithNoMatchingRS := ar("c")

	validRS := rs("a", false)
	deletedRS := rs("b", true)
	ars := []*v1alpha1.AnalysisRun{
		arNoDeletion,
		arWithNoPodHash,
		arWithDeletedRS,
		arWithNoMatchingRS,
	}
	olderRSs := []*appsv1.ReplicaSet{
		validRS,
		deletedRS,
		nil,
	}
	filteredArs := FilterAnalysisRunsToDelete(ars, olderRSs)
	assert.Len(t, filteredArs, 3)
	assert.NotContains(t, filteredArs, arNoDeletion)
	assert.Contains(t, filteredArs, arWithNoPodHash)
	assert.Contains(t, filteredArs, arWithDeletedRS)
	assert.Contains(t, filteredArs, arWithNoMatchingRS)
}

func TestSortAnalysisRunByPodHash(t *testing.T) {
	emptyMap := SortAnalysisRunByPodHash(nil)
	assert.NotNil(t, 0)
	assert.Len(t, emptyMap, 0)
	name := "a"
	ar := func(podHash *string) *v1alpha1.AnalysisRun {
		labels := map[string]string{}
		if podHash != nil {
			labels[v1alpha1.DefaultRolloutUniqueLabelKey] = *podHash
		}
		a := &v1alpha1.AnalysisRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: labels,
			},
		}
		name = name + "a"
		return a
	}
	ars := []*v1alpha1.AnalysisRun{
		ar(nil),
		ar(pointer.StringPtr("ab")),
		ar(pointer.StringPtr("abc")),
		ar(pointer.StringPtr("abc")),
	}
	arMap := SortAnalysisRunByPodHash(ars)
	assert.Len(t, arMap, 2)
	assert.Len(t, arMap["ab"], 1)
	assert.Len(t, arMap["abc"], 2)

}
