package analysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestGetCurrentBackgroundAnalysisRun(t *testing.T) {
	arsWithBackground := []*v1alpha1.AnalysisRun{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
				Labels: map[string]string{
					v1alpha1.RolloutTypeLabel: v1alpha1.RolloutTypeBackgroundRunLabel,
				},
			},
		},
	}
	currAr := GetCurrentBackgroundAnalysisRun(arsWithBackground)
	assert.Equal(t, arsWithBackground[0], currAr)
	arsWithNoBackground := []*v1alpha1.AnalysisRun{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "bar",
				Labels: map[string]string{
					v1alpha1.RolloutTypeLabel: v1alpha1.RolloutTypeStepLabel,
				},
			},
		},
	}
	currAr = GetCurrentBackgroundAnalysisRun(arsWithNoBackground)
	assert.Nil(t, currAr)
}

func TestGetCurrentStepAnalysisRun(t *testing.T) {
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
	currAr := GetCurrentStepAnalysisRun(arsWithSteps)
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
	currAr = GetCurrentStepAnalysisRun(arsWithNoSteps)
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
	r := &v1alpha1.Rollout{
		Status: v1alpha1.RolloutStatus{
			Canary: v1alpha1.CanaryStatus{
				CurrentStepAnalysisRun:       "foo",
				CurrentBackgroundAnalysisRun: "bar",
			},
		},
	}
	currentArs, nonCurrentArs := FilterCurrentRolloutAnalysisRuns(ars, r)
	assert.Len(t, currentArs, 2)
	assert.Len(t, nonCurrentArs, 1)
	assert.Contains(t, currentArs, ars[0])
	assert.Contains(t, currentArs, ars[1])
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
