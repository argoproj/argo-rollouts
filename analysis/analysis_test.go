package analysis

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestGenerateMetricTasksInterval(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			AnalysisSpec: v1alpha1.AnalysisTemplateSpec{
				Metrics: []v1alpha1.Metric{
					{
						Name:     "success-rate",
						Interval: pointer.Int32Ptr(60),
					},
				},
			},
		},
		Status: &v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: map[string]v1alpha1.MetricResult{
				"success-rate": {
					Status: v1alpha1.AnalysisStatusRunning,
					Measurements: []v1alpha1.Measurement{
						{
							Value:      "99",
							Status:     v1alpha1.AnalysisStatusSuccessful,
							StartedAt:  metav1.NewTime(time.Now().Add(-50 * time.Second)),
							FinishedAt: metav1.NewTime(time.Now().Add(-50 * time.Second)),
						},
					},
				},
			},
		},
	}
	{
		// ensure we don't take measurements when within the interval
		tasks := generateMetricTasks(run)
		assert.Equal(t, 0, len(tasks))
	}
	{
		// ensure we do take measurements when outside interval
		successRate := run.Status.MetricResults["success-rate"]
		successRate.Measurements[0].StartedAt = metav1.NewTime(time.Now().Add(-61 * time.Second))
		successRate.Measurements[0].FinishedAt = metav1.NewTime(time.Now().Add(-61 * time.Second))
		run.Status.MetricResults["success-rate"] = successRate
		tasks := generateMetricTasks(run)
		assert.Equal(t, 1, len(tasks))
	}
}

func TestGenerateMetricTasksFailing(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			AnalysisSpec: v1alpha1.AnalysisTemplateSpec{
				Metrics: []v1alpha1.Metric{
					{
						Name: "success-rate",
					},
					{
						Name: "latency",
					},
				},
			},
		},
		Status: &v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: map[string]v1alpha1.MetricResult{
				"latency": {
					Status: v1alpha1.AnalysisStatusFailed,
				},
			},
		},
	}
	// ensure we don't perform more measurements when one result already failed
	tasks := generateMetricTasks(run)
	assert.Equal(t, 0, len(tasks))
	run.Status.MetricResults = nil
	// ensure we schedule tasks when no results are failed
	tasks = generateMetricTasks(run)
	assert.Equal(t, 2, len(tasks))
}

func TestGenerateMetricTasksNoInterval(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			AnalysisSpec: v1alpha1.AnalysisTemplateSpec{
				Metrics: []v1alpha1.Metric{
					{
						Name: "success-rate",
					},
				},
			},
		},
		Status: &v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: map[string]v1alpha1.MetricResult{
				"success-rate": {
					Status: v1alpha1.AnalysisStatusRunning,
					Measurements: []v1alpha1.Measurement{
						{
							Value:      "99",
							Status:     v1alpha1.AnalysisStatusSuccessful,
							StartedAt:  metav1.NewTime(time.Now().Add(-50 * time.Second)),
							FinishedAt: metav1.NewTime(time.Now().Add(-50 * time.Second)),
						},
					},
				},
			},
		},
	}
	{
		// ensure we don't take measurement when interval is not specified and we already took measurement
		tasks := generateMetricTasks(run)
		assert.Equal(t, 0, len(tasks))
	}
	{
		// ensure we do take measurements when measurment has not been taken
		successRate := run.Status.MetricResults["success-rate"]
		successRate.Measurements = nil
		run.Status.MetricResults["success-rate"] = successRate
		tasks := generateMetricTasks(run)
		assert.Equal(t, 1, len(tasks))
	}
}

func TestGenerateMetricTasksIncomplete(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			AnalysisSpec: v1alpha1.AnalysisTemplateSpec{
				Metrics: []v1alpha1.Metric{
					{
						Name: "success-rate",
					},
				},
			},
		},
		Status: &v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: map[string]v1alpha1.MetricResult{
				"success-rate": {
					Status: v1alpha1.AnalysisStatusRunning,
					Measurements: []v1alpha1.Measurement{
						{
							Value:     "99",
							Status:    v1alpha1.AnalysisStatusSuccessful,
							StartedAt: metav1.NewTime(time.Now().Add(-50 * time.Second)),
						},
					},
				},
			},
		},
	}
	{
		// ensure we don't take measurement when interval is not specified and we already took measurement
		tasks := generateMetricTasks(run)
		assert.Equal(t, 1, len(tasks))
		assert.NotNil(t, tasks[0].incompleteMeasurement)
	}
}
