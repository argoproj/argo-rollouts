package analysis

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func timePtr(t metav1.Time) *metav1.Time {
	return &t
}

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
							StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-50 * time.Second))),
							FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-50 * time.Second))),
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
		successRate.Measurements[0].StartedAt = timePtr(metav1.NewTime(time.Now().Add(-61 * time.Second)))
		successRate.Measurements[0].FinishedAt = timePtr(metav1.NewTime(time.Now().Add(-61 * time.Second)))
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
							StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-50 * time.Second))),
							FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-50 * time.Second))),
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
							StartedAt: timePtr(metav1.NewTime(time.Now().Add(-50 * time.Second))),
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

func TestAssessRunStatus(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			AnalysisSpec: v1alpha1.AnalysisTemplateSpec{
				Metrics: []v1alpha1.Metric{
					{
						Name: "latency",
					},
					{
						Name: "success-rate",
					},
				},
			},
		},
	}
	{
		// ensure if one metric is still running, entire run is still running
		run.Status = &v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: map[string]v1alpha1.MetricResult{
				"latency": {
					Status: v1alpha1.AnalysisStatusSuccessful,
				},
				"success-rate": {
					Status: v1alpha1.AnalysisStatusRunning,
				},
			},
		}
		assert.Equal(t, v1alpha1.AnalysisStatusRunning, asssessRunStatus(run))
	}
	{
		// ensure we take the worst of the completed metrics
		run.Status = &v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: map[string]v1alpha1.MetricResult{
				"latency": {
					Status: v1alpha1.AnalysisStatusSuccessful,
				},
				"success-rate": {
					Status: v1alpha1.AnalysisStatusFailed,
				},
			},
		}
		assert.Equal(t, v1alpha1.AnalysisStatusFailed, asssessRunStatus(run))
	}
}

func TestAssessMetricStatusNoMeasurements(t *testing.T) {
	// no measurements yet taken
	metric := v1alpha1.Metric{
		Name: "success-rate",
	}
	result := v1alpha1.MetricResult{
		Measurements: nil,
	}
	assert.Equal(t, v1alpha1.AnalysisStatusPending, assessMetricStatus(metric, result, false))
	assert.Equal(t, v1alpha1.AnalysisStatusSuccessful, assessMetricStatus(metric, result, true))
}
func TestAssessMetricStatusInFlightMeasurement(t *testing.T) {
	// in-flight measurement
	metric := v1alpha1.Metric{
		Name: "success-rate",
	}
	result := v1alpha1.MetricResult{
		Measurements: []v1alpha1.Measurement{
			{
				Value:      "99",
				Status:     v1alpha1.AnalysisStatusSuccessful,
				StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
				FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
			},
			{
				Value:     "99",
				Status:    v1alpha1.AnalysisStatusRunning,
				StartedAt: timePtr(metav1.NewTime(time.Now())),
			},
		},
	}
	assert.Equal(t, v1alpha1.AnalysisStatusRunning, assessMetricStatus(metric, result, false))
	assert.Equal(t, v1alpha1.AnalysisStatusRunning, assessMetricStatus(metric, result, true))
}
func TestAssessMetricStatusMaxFailures(t *testing.T) { // max failures
	metric := v1alpha1.Metric{
		Name:        "success-rate",
		MaxFailures: 2,
	}
	result := v1alpha1.MetricResult{
		Failed: 3,
		Measurements: []v1alpha1.Measurement{
			{
				Value:      "99",
				Status:     v1alpha1.AnalysisStatusFailed,
				StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
				FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
			},
		},
	}
	assert.Equal(t, v1alpha1.AnalysisStatusFailed, assessMetricStatus(metric, result, false))
	assert.Equal(t, v1alpha1.AnalysisStatusFailed, assessMetricStatus(metric, result, true))
}
func TestAssessMetricStatusConsecutiveErrors(t *testing.T) {
	metric := v1alpha1.Metric{
		Name: "success-rate",
	}
	result := v1alpha1.MetricResult{
		Measurements: []v1alpha1.Measurement{
			{
				Status:     v1alpha1.AnalysisStatusError,
				StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
				FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
			},
			{
				Status:     v1alpha1.AnalysisStatusError,
				StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-50 * time.Second))),
				FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-50 * time.Second))),
			},
			{
				Status:     v1alpha1.AnalysisStatusError,
				StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-40 * time.Second))),
				FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-40 * time.Second))),
			},
			{
				Status:     v1alpha1.AnalysisStatusError,
				StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-30 * time.Second))),
				FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-30 * time.Second))),
			},
			{
				Status:     v1alpha1.AnalysisStatusError,
				StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-20 * time.Second))),
				FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-20 * time.Second))),
			},
		},
	}
	assert.Equal(t, v1alpha1.AnalysisStatusError, assessMetricStatus(metric, result, false))
	assert.Equal(t, v1alpha1.AnalysisStatusError, assessMetricStatus(metric, result, true))
	result.Measurements[2] = v1alpha1.Measurement{
		Status:     v1alpha1.AnalysisStatusSuccessful,
		StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-40 * time.Second))),
		FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-40 * time.Second))),
	}
	assert.Equal(t, v1alpha1.AnalysisStatusSuccessful, assessMetricStatus(metric, result, true))
	assert.Equal(t, v1alpha1.AnalysisStatusRunning, assessMetricStatus(metric, result, false))
}

func TestAssessMetricStatusCountReached(t *testing.T) {
	metric := v1alpha1.Metric{
		Name:  "success-rate",
		Count: 10,
	}
	result := v1alpha1.MetricResult{
		Successful: 10,
		Count:      10,
		Measurements: []v1alpha1.Measurement{
			{
				Value:      "99",
				Status:     v1alpha1.AnalysisStatusSuccessful,
				StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
				FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
			},
		},
	}
	assert.Equal(t, v1alpha1.AnalysisStatusSuccessful, assessMetricStatus(metric, result, false))
	result.Successful = 5
	result.Inconclusive = 5
	assert.Equal(t, v1alpha1.AnalysisStatusInconclusive, assessMetricStatus(metric, result, false))
}

func TestCalculateNextReconcileTimeInterval(t *testing.T) {
	now := metav1.Now()
	nowMinus30 := metav1.NewTime(now.Add(time.Second * -30))
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
							StartedAt:  &nowMinus30,
							FinishedAt: &nowMinus30,
						},
					},
				},
			},
		},
	}
	// ensure we requeue at correct interval
	assert.Equal(t, now.Add(time.Second*30), *calculateNextReconcileTime(run))
	// when in-flight is not set, we do not requeue
	run.Status.MetricResults["success-rate"].Measurements[0].FinishedAt = nil
	run.Status.MetricResults["success-rate"].Measurements[0].Status = v1alpha1.AnalysisStatusRunning
	assert.Nil(t, calculateNextReconcileTime(run))
	// do not queue completed metrics
	nowMinus120 := metav1.NewTime(now.Add(time.Second * -120))
	run.Status.MetricResults["success-rate"] = v1alpha1.MetricResult{
		Status: v1alpha1.AnalysisStatusSuccessful,
		Measurements: []v1alpha1.Measurement{
			{
				Value:      "99",
				Status:     v1alpha1.AnalysisStatusSuccessful,
				StartedAt:  &nowMinus120,
				FinishedAt: &nowMinus120,
			},
		},
	}
	assert.Nil(t, calculateNextReconcileTime(run))
}

func TestCalculateNextReconcileTimeNoInterval(t *testing.T) {
	now := metav1.Now()
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			AnalysisSpec: v1alpha1.AnalysisTemplateSpec{
				Metrics: []v1alpha1.Metric{
					{
						Name:  "success-rate",
						Count: 1,
					},
				},
			},
		},
		Status: &v1alpha1.AnalysisRunStatus{
			Status: v1alpha1.AnalysisStatusRunning,
			MetricResults: map[string]v1alpha1.MetricResult{
				"success-rate": {
					Status: v1alpha1.AnalysisStatusSuccessful,
					Measurements: []v1alpha1.Measurement{
						{
							Value:      "99",
							Status:     v1alpha1.AnalysisStatusSuccessful,
							StartedAt:  &now,
							FinishedAt: &now,
						},
					},
				},
			},
		},
	}
	assert.Nil(t, calculateNextReconcileTime(run))
}

func TestCalculateNextReconcileEarliestMetric(t *testing.T) {
	now := metav1.Now()
	nowMinus30 := metav1.NewTime(now.Add(time.Second * -30))
	nowMinus50 := metav1.NewTime(now.Add(time.Second * -50))
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			AnalysisSpec: v1alpha1.AnalysisTemplateSpec{
				Metrics: []v1alpha1.Metric{
					{
						Name:     "success-rate",
						Interval: pointer.Int32Ptr(60),
					},
					{
						Name:     "latency",
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
							StartedAt:  &nowMinus30,
							FinishedAt: &nowMinus30,
						},
					},
				},
				"latency": {
					Status: v1alpha1.AnalysisStatusRunning,
					Measurements: []v1alpha1.Measurement{
						{
							Value:      "1",
							Status:     v1alpha1.AnalysisStatusSuccessful,
							StartedAt:  &nowMinus50,
							FinishedAt: &nowMinus50,
						},
					},
				},
			},
		},
	}
	// ensure we requeue at correct interval
	assert.Equal(t, now.Add(time.Second*10), *calculateNextReconcileTime(run))
}
