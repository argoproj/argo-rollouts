package rollout

import (
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
		analysisContext := NewAnalysisContext(ars, r)
		assert.Len(t, analysisContext.otherArs, 1)
		assert.Equal(t, analysisContext.CanaryStep.AnalysisRun(), ars[0])
		assert.Equal(t, analysisContext.CanaryBackground.AnalysisRun(), ars[1])
		assert.Nil(t, analysisContext.BlueGreenPostPromotion.AnalysisRun())
		assert.Nil(t, analysisContext.BlueGreenPrePromotion.AnalysisRun())
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
		analysisContext := NewAnalysisContext(ars, r)
		assert.Len(t, analysisContext.otherArs, 1)
		assert.Equal(t, analysisContext.BlueGreenPrePromotion.AnalysisRun(), ars[0])
		assert.Equal(t, analysisContext.BlueGreenPostPromotion.AnalysisRun(), ars[1])
		assert.Nil(t, analysisContext.CanaryStep.AnalysisRun())
		assert.Nil(t, analysisContext.CanaryStep.AnalysisRun())
	})
}
