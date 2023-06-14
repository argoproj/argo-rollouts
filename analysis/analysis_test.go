package analysis

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/metric"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
)

func timePtr(t metav1.Time) *metav1.Time {
	return &t
}

func newMeasurement(status v1alpha1.AnalysisPhase) v1alpha1.Measurement {
	now := metav1.Now()
	return v1alpha1.Measurement{
		Phase:      status,
		Value:      "100",
		StartedAt:  &now,
		FinishedAt: &now,
	}
}

func newRun() *v1alpha1.AnalysisRun {
	return &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:     "metric1",
					Interval: "60s",
					Provider: v1alpha1.MetricProvider{
						Job: &v1alpha1.JobMetric{},
					},
				},
				{
					Name:     "metric2",
					Interval: "60s",
					Provider: v1alpha1.MetricProvider{
						Job: &v1alpha1.JobMetric{},
					},
				},
			},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:  "metric1",
					Phase: v1alpha1.AnalysisPhaseRunning,
					Measurements: []v1alpha1.Measurement{{
						Value:      "1",
						Phase:      v1alpha1.AnalysisPhaseSuccessful,
						StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
						FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
					}},
				},
				{
					Name: "metric2",
					Measurements: []v1alpha1.Measurement{
						{
							Value:      "2",
							Phase:      v1alpha1.AnalysisPhaseSuccessful,
							StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
							FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
						},
						{
							Value:      "3",
							Phase:      v1alpha1.AnalysisPhaseSuccessful,
							StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-30 * time.Second))),
							FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-30 * time.Second))),
						},
					},
				},
			},
		},
	}
}

// newTerminatingRun returns a run which is terminating because of the given status
func newTerminatingRun(status v1alpha1.AnalysisPhase, isDryRun bool) *v1alpha1.AnalysisRun {
	var dryRunArray []v1alpha1.DryRun
	if isDryRun {
		dryRunArray = append(dryRunArray, v1alpha1.DryRun{MetricName: "run-forever"})
		dryRunArray = append(dryRunArray, v1alpha1.DryRun{MetricName: "failed-metric"})
	}
	run := v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "run-forever",
					Provider: v1alpha1.MetricProvider{
						Job: &v1alpha1.JobMetric{},
					},
				},
				{
					Name: "failed-metric",
					Provider: v1alpha1.MetricProvider{
						Job: &v1alpha1.JobMetric{},
					},
				},
			},
			DryRun: dryRunArray,
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:   "run-forever",
					DryRun: isDryRun,
					Phase:  v1alpha1.AnalysisPhaseRunning,
					Measurements: []v1alpha1.Measurement{{
						Phase:     v1alpha1.AnalysisPhaseRunning,
						StartedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
					}},
				},
				{
					Name:   "failed-metric",
					Count:  1,
					DryRun: isDryRun,
					Measurements: []v1alpha1.Measurement{{
						Phase:      status,
						StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
						FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
					}},
				},
			},
		},
	}
	run.Status.MetricResults[1].Phase = status
	switch status {
	case v1alpha1.AnalysisPhaseFailed:
		run.Status.MetricResults[1].Failed = 1
	case v1alpha1.AnalysisPhaseInconclusive:
		run.Status.MetricResults[1].Inconclusive = 1
	case v1alpha1.AnalysisPhaseError:
		run.Status.MetricResults[1].Error = 1
		run.Status.MetricResults[1].Measurements = []v1alpha1.Measurement{{
			Phase:      v1alpha1.AnalysisPhaseError,
			StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-120 * time.Second))),
			FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-120 * time.Second))),
		}}
	}
	return &run
}

func TestGenerateMetricTasksInterval(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{{
				Name:     "success-rate",
				Interval: "60s",
			}},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{{
				Name:  "success-rate",
				Phase: v1alpha1.AnalysisPhaseRunning,
				Measurements: []v1alpha1.Measurement{{
					Value:      "99",
					Phase:      v1alpha1.AnalysisPhaseSuccessful,
					StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-50 * time.Second))),
					FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-50 * time.Second))),
				}},
			}},
		},
	}
	{
		// ensure we don't take measurements when within the interval
		tasks := generateMetricTasks(run, run.Spec.Metrics)
		assert.Equal(t, 0, len(tasks))
	}
	{
		// ensure we do take measurements when outside interval
		successRate := run.Status.MetricResults[0]
		successRate.Measurements[0].StartedAt = timePtr(metav1.NewTime(time.Now().Add(-61 * time.Second)))
		successRate.Measurements[0].FinishedAt = timePtr(metav1.NewTime(time.Now().Add(-61 * time.Second)))
		run.Status.MetricResults[0] = successRate
		tasks := generateMetricTasks(run, run.Spec.Metrics)
		assert.Equal(t, 1, len(tasks))
	}
}

func TestGenerateMetricTasksFailing(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "success-rate",
				},
				{
					Name: "latency",
				},
			},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{{
				Name:  "latency",
				Phase: v1alpha1.AnalysisPhaseFailed,
			}},
		},
	}
	// ensure we don't perform more measurements when one result already failed
	tasks := generateMetricTasks(run, run.Spec.Metrics)
	assert.Equal(t, 0, len(tasks))
	run.Status.MetricResults = nil
	// ensure we schedule tasks when no results are failed
	tasks = generateMetricTasks(run, run.Spec.Metrics)
	assert.Equal(t, 2, len(tasks))
}

func TestGenerateMetricTasksNoIntervalOrCount(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{{
				Name: "success-rate",
			}},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{{
				Name:  "success-rate",
				Count: 1,
				Measurements: []v1alpha1.Measurement{{
					Value:      "99",
					Phase:      v1alpha1.AnalysisPhaseSuccessful,
					StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-50 * time.Second))),
					FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-50 * time.Second))),
				}},
			}},
		},
	}
	{
		// ensure we don't take measurement when result count indicates we completed
		tasks := generateMetricTasks(run, run.Spec.Metrics)
		assert.Equal(t, 0, len(tasks))
	}
	{
		// ensure we do take measurements when measurement has not been taken
		successRate := run.Status.MetricResults[0]
		successRate.Measurements = nil
		successRate.Count = 0
		run.Status.MetricResults[0] = successRate
		tasks := generateMetricTasks(run, run.Spec.Metrics)
		assert.Equal(t, 1, len(tasks))
	}
}

func TestGenerateMetricTasksIncomplete(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{{
				Name: "success-rate",
			}},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{{
				Name:  "success-rate",
				Phase: v1alpha1.AnalysisPhaseRunning,
				Measurements: []v1alpha1.Measurement{{
					Value:     "99",
					Phase:     v1alpha1.AnalysisPhaseSuccessful,
					StartedAt: timePtr(metav1.NewTime(time.Now().Add(-50 * time.Second))),
				}},
			}},
		},
	}
	{
		// ensure we don't take measurement when interval is not specified and we already took measurement
		tasks := generateMetricTasks(run, run.Spec.Metrics)
		assert.Equal(t, 1, len(tasks))
		assert.NotNil(t, tasks[0].incompleteMeasurement)
	}
}

func TestGenerateMetricTasksHonorInitialDelay(t *testing.T) {
	now := metav1.Now()
	nowMinus10 := metav1.NewTime(now.Add(-10 * time.Second))
	nowMinus20 := metav1.NewTime(now.Add(-20 * time.Second))
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:         "success-rate",
					InitialDelay: "20s",
				},
			},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
		},
	}
	{
		// ensure we don't take measurement for metrics with start delays when no startAt is set
		tasks := generateMetricTasks(run, run.Spec.Metrics)
		assert.Equal(t, 0, len(tasks))
	}
	{
		run.Status.StartedAt = &nowMinus10
		// ensure we don't take measurement for metrics with start delays where we haven't waited the start delay
		tasks := generateMetricTasks(run, run.Spec.Metrics)
		assert.Equal(t, 0, len(tasks))
	}
	{
		run.Status.StartedAt = &nowMinus20
		// ensure we do take measurement for metrics with start delays where we have waited the start delay
		tasks := generateMetricTasks(run, run.Spec.Metrics)
		assert.Equal(t, 1, len(tasks))
	}
	{
		run.Spec.Metrics[0].InitialDelay = "invalid-start-delay"
		// ensure we don't take measurement for metrics with invalid start delays
		tasks := generateMetricTasks(run, run.Spec.Metrics)
		assert.Equal(t, 0, len(tasks))
	}
}

func TestGenerateMetricTasksHonorResumeAt(t *testing.T) {
	now := metav1.Now()
	nowMinus50 := metav1.NewTime(now.Add(-50 * time.Second))
	nowMinus10 := metav1.NewTime(now.Add(-10 * time.Second))
	nowPlus10 := metav1.NewTime(now.Add(10 * time.Second))
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "success-rate",
				},
				{
					Name: "success-rate2",
				},
			},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:  "success-rate",
					Phase: v1alpha1.AnalysisPhaseRunning,
					Measurements: []v1alpha1.Measurement{{
						Value:     "99",
						Phase:     v1alpha1.AnalysisPhaseSuccessful,
						StartedAt: &nowMinus50,
						ResumeAt:  &nowPlus10,
					}},
				}, {
					Name:  "success-rate2",
					Phase: v1alpha1.AnalysisPhaseRunning,
					Measurements: []v1alpha1.Measurement{{
						Value:     "99",
						Phase:     v1alpha1.AnalysisPhaseSuccessful,
						StartedAt: &nowMinus50,
						ResumeAt:  &nowMinus10,
					}},
				},
			},
		},
	}
	{
		// ensure we don't take measurement when resumeAt has not passed
		tasks := generateMetricTasks(run, run.Spec.Metrics)
		assert.Equal(t, 1, len(tasks))
		assert.Equal(t, "success-rate2", tasks[0].metric.Name)
	}
}

// TestGenerateMetricTasksError ensures we generate a task when have a measurement which was errored

func TestGenerateMetricTasksError(t *testing.T) {
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{{
				Name: "success-rate",
			}},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{{
				Name:  "success-rate",
				Phase: v1alpha1.AnalysisPhaseRunning,
				Error: 1,
				Measurements: []v1alpha1.Measurement{{
					Phase:      v1alpha1.AnalysisPhaseError,
					StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-120 * time.Second))),
					FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-120 * time.Second))),
				}},
			}},
		},
	}
	{
		run := run.DeepCopy()
		tasks := generateMetricTasks(run, run.Spec.Metrics)
		assert.Equal(t, 1, len(tasks))
	}
	{
		run := run.DeepCopy()
		run.Spec.Metrics[0].Interval = "5m"
		tasks := generateMetricTasks(run, run.Spec.Metrics)
		assert.Equal(t, 1, len(tasks))
	}
}

func TestAssessRunStatus(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "latency",
				},
				{
					Name: "success-rate",
				},
			},
		},
	}
	{
		// ensure if one metric is still running, entire run is still running
		run.Status = v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:  "latency",
					Phase: v1alpha1.AnalysisPhaseSuccessful,
				},
				{
					Name:  "success-rate",
					Phase: v1alpha1.AnalysisPhaseRunning,
				},
			},
		}
		status, message := c.assessRunStatus(run, run.Spec.Metrics, map[string]bool{})
		assert.Equal(t, v1alpha1.AnalysisPhaseRunning, status)
		assert.Equal(t, "", message)
	}
	{
		// ensure we take the worst of the completed metrics
		run.Status = v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:  "latency",
					Phase: v1alpha1.AnalysisPhaseSuccessful,
				},
				{
					Name:  "success-rate",
					Phase: v1alpha1.AnalysisPhaseFailed,
				},
			},
		}
		status, message := c.assessRunStatus(run, run.Spec.Metrics, map[string]bool{})
		assert.Equal(t, v1alpha1.AnalysisPhaseFailed, status)
		assert.Equal(t, "", message)
	}
}

// TestAssessRunStatusUpdateResult ensures we update the metricresult status properly
// based on latest measurements
func TestAssessRunStatusUpdateResult(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "sleep-infinity",
					Provider: v1alpha1.MetricProvider{
						Job: &v1alpha1.JobMetric{},
					},
				},
				{
					Name: "fail-after-30",
					Provider: v1alpha1.MetricProvider{
						Job: &v1alpha1.JobMetric{},
					},
				},
			},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:  "sleep-infinity",
					Phase: v1alpha1.AnalysisPhaseRunning,
					Measurements: []v1alpha1.Measurement{{
						Phase:     v1alpha1.AnalysisPhaseRunning,
						StartedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
					}},
				},
				{
					Name:   "fail-after-30",
					Count:  1,
					Failed: 1,
					Phase:  v1alpha1.AnalysisPhaseRunning, // This should flip to Failed
					Measurements: []v1alpha1.Measurement{{
						Phase:      v1alpha1.AnalysisPhaseFailed,
						StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
						FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
					}},
				},
			},
		},
	}
	status, message := c.assessRunStatus(run, run.Spec.Metrics, map[string]bool{})
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, status)
	assert.Equal(t, "", message)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, run.Status.MetricResults[1].Phase)
}

func TestAssessMetricStatusNoMeasurements(t *testing.T) {
	// no measurements yet taken
	metric := v1alpha1.Metric{
		Name: "success-rate",
	}
	result := v1alpha1.MetricResult{
		Measurements: nil,
	}
	assert.Equal(t, v1alpha1.AnalysisPhasePending, assessMetricStatus(metric, result, false))
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, assessMetricStatus(metric, result, true))
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
				Phase:      v1alpha1.AnalysisPhaseSuccessful,
				StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
				FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
			},
			{
				Value:     "99",
				Phase:     v1alpha1.AnalysisPhaseRunning,
				StartedAt: timePtr(metav1.NewTime(time.Now())),
			},
		},
	}
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, assessMetricStatus(metric, result, false))
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, assessMetricStatus(metric, result, true))
}
func TestAssessMetricStatusFailureLimit(t *testing.T) { // max failures
	failureLimit := intstr.FromInt(2)
	metric := v1alpha1.Metric{
		Name:         "success-rate",
		FailureLimit: &failureLimit,
		Interval:     "60s",
	}
	result := v1alpha1.MetricResult{
		Failed: 3,
		Count:  3,
		Measurements: []v1alpha1.Measurement{{
			Value:      "99",
			Phase:      v1alpha1.AnalysisPhaseFailed,
			StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
			FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
		}},
	}
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, assessMetricStatus(metric, result, false))
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, assessMetricStatus(metric, result, true))
	newFailureLimit := intstr.FromInt(3)
	metric.FailureLimit = &newFailureLimit
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, assessMetricStatus(metric, result, false))
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, assessMetricStatus(metric, result, true))
}

func TestAssessMetricStatusInconclusiveLimit(t *testing.T) {
	inconclusiveLimit := intstr.FromInt(2)
	metric := v1alpha1.Metric{
		Name:              "success-rate",
		InconclusiveLimit: &inconclusiveLimit,
		Interval:          "60s",
	}
	result := v1alpha1.MetricResult{
		Inconclusive: 3,
		Count:        3,
		Measurements: []v1alpha1.Measurement{{
			Value:      "99",
			Phase:      v1alpha1.AnalysisPhaseInconclusive,
			StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
			FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
		}},
	}
	assert.Equal(t, v1alpha1.AnalysisPhaseInconclusive, assessMetricStatus(metric, result, false))
	assert.Equal(t, v1alpha1.AnalysisPhaseInconclusive, assessMetricStatus(metric, result, true))
	newInconclusiveLimit := intstr.FromInt(3)
	metric.InconclusiveLimit = &newInconclusiveLimit
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, assessMetricStatus(metric, result, false))
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, assessMetricStatus(metric, result, true))
}

func TestAssessMetricStatusConsecutiveErrors(t *testing.T) {
	metric := v1alpha1.Metric{
		Name:     "success-rate",
		Interval: "60s",
	}
	result := v1alpha1.MetricResult{
		ConsecutiveError: 5,
		Count:            5,
		Measurements: []v1alpha1.Measurement{{
			Phase:      v1alpha1.AnalysisPhaseError,
			StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
			FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
		}},
	}
	assert.Equal(t, v1alpha1.AnalysisPhaseError, assessMetricStatus(metric, result, false))
	assert.Equal(t, v1alpha1.AnalysisPhaseError, assessMetricStatus(metric, result, true))
	result.ConsecutiveError = 4
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, assessMetricStatus(metric, result, true))
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, assessMetricStatus(metric, result, false))
}

func TestAssessMetricStatusCountReached(t *testing.T) {
	count := intstr.FromInt(10)
	metric := v1alpha1.Metric{
		Name:  "success-rate",
		Count: &count,
	}
	result := v1alpha1.MetricResult{
		Successful: 10,
		Count:      10,
		Measurements: []v1alpha1.Measurement{{
			Value:      "99",
			Phase:      v1alpha1.AnalysisPhaseSuccessful,
			StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
			FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
		}},
	}
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, assessMetricStatus(metric, result, false))
	result.Successful = 5
	result.Inconclusive = 5
	assert.Equal(t, v1alpha1.AnalysisPhaseInconclusive, assessMetricStatus(metric, result, false))
}

func TestCalculateNextReconcileTimeInterval(t *testing.T) {
	now := metav1.Now()
	nowMinus30 := metav1.NewTime(now.Add(time.Second * -30))
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{{
				Name:     "success-rate",
				Interval: "60s",
			}},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{{
				Name:  "success-rate",
				Phase: v1alpha1.AnalysisPhaseRunning,
				Measurements: []v1alpha1.Measurement{{
					Value:      "99",
					Phase:      v1alpha1.AnalysisPhaseSuccessful,
					StartedAt:  &nowMinus30,
					FinishedAt: &nowMinus30,
				}},
			}},
		},
	}
	// ensure we requeue at correct interval
	assert.Equal(t, now.Add(time.Second*30), *calculateNextReconcileTime(run, run.Spec.Metrics))
	// when in-flight is not set, we do not requeue
	run.Status.MetricResults[0].Measurements[0].FinishedAt = nil
	run.Status.MetricResults[0].Measurements[0].Phase = v1alpha1.AnalysisPhaseRunning
	assert.Nil(t, calculateNextReconcileTime(run, run.Spec.Metrics))
	// do not queue completed metrics
	nowMinus120 := metav1.NewTime(now.Add(time.Second * -120))
	run.Status.MetricResults[0] = v1alpha1.MetricResult{
		Phase: v1alpha1.AnalysisPhaseSuccessful,
		Measurements: []v1alpha1.Measurement{{
			Value:      "99",
			Phase:      v1alpha1.AnalysisPhaseSuccessful,
			StartedAt:  &nowMinus120,
			FinishedAt: &nowMinus120,
		}},
	}
	assert.Nil(t, calculateNextReconcileTime(run, run.Spec.Metrics))
}

func TestCalculateNextReconcileTimeInitialDelay(t *testing.T) {
	now := metav1.Now()
	nowMinus30 := metav1.NewTime(now.Add(time.Second * -30))
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:     "success-rate",
					Interval: "60s",
				},
				{
					Name:         "start-delay",
					Interval:     "60s",
					InitialDelay: "40s",
				},
			},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase:     v1alpha1.AnalysisPhaseRunning,
			StartedAt: &nowMinus30,
			MetricResults: []v1alpha1.MetricResult{{
				Name:  "success-rate",
				Phase: v1alpha1.AnalysisPhaseRunning,
				Measurements: []v1alpha1.Measurement{{
					Value:      "99",
					Phase:      v1alpha1.AnalysisPhaseSuccessful,
					StartedAt:  &nowMinus30,
					FinishedAt: &nowMinus30,
				}},
			}},
		},
	}
	// ensure we requeue after start delay
	assert.Equal(t, now.Add(time.Second*10), *calculateNextReconcileTime(run, run.Spec.Metrics))
	run.Spec.Metrics[1].InitialDelay = "not-valid-start-delay"
	// skip invalid start delay and use the other metrics next reconcile time
	assert.Equal(t, now.Add(time.Second*30), *calculateNextReconcileTime(run, run.Spec.Metrics))

}

func TestCalculateNextReconcileTimeNoInterval(t *testing.T) {
	now := metav1.Now()
	count := intstr.FromInt(1)
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{{
				Name:  "success-rate",
				Count: &count,
			}},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{{
				Name:  "success-rate",
				Phase: v1alpha1.AnalysisPhaseSuccessful,
				Measurements: []v1alpha1.Measurement{{
					Value:      "99",
					Phase:      v1alpha1.AnalysisPhaseSuccessful,
					StartedAt:  &now,
					FinishedAt: &now,
				}},
			}},
		},
	}
	assert.Nil(t, calculateNextReconcileTime(run, run.Spec.Metrics))
}

func TestCalculateNextReconcileEarliestMetric(t *testing.T) {
	now := metav1.Now()
	nowMinus30 := metav1.NewTime(now.Add(time.Second * -30))
	nowMinus50 := metav1.NewTime(now.Add(time.Second * -50))
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:     "success-rate",
					Interval: "60s",
				},
				{
					Name:     "latency",
					Interval: "60s",
				},
			},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:  "success-rate",
					Phase: v1alpha1.AnalysisPhaseRunning,
					Measurements: []v1alpha1.Measurement{{
						Value:      "99",
						Phase:      v1alpha1.AnalysisPhaseSuccessful,
						StartedAt:  &nowMinus30,
						FinishedAt: &nowMinus30,
					}},
				},
				{
					Name:  "latency",
					Phase: v1alpha1.AnalysisPhaseRunning,
					Measurements: []v1alpha1.Measurement{{
						Value:      "1",
						Phase:      v1alpha1.AnalysisPhaseSuccessful,
						StartedAt:  &nowMinus50,
						FinishedAt: &nowMinus50,
					}},
				},
			},
		},
	}
	// ensure we requeue at correct interval
	assert.Equal(t, now.Add(time.Second*10), *calculateNextReconcileTime(run, run.Spec.Metrics))
}

func TestCalculateNextReconcileHonorResumeAt(t *testing.T) {
	now := metav1.Now()
	nowMinus30 := metav1.NewTime(now.Add(time.Second * -30))
	nowPlus10 := metav1.NewTime(now.Add(time.Second * 10))
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{{
				Name:     "success-rate",
				Interval: "60s",
			}},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{{
				Name:  "success-rate",
				Phase: v1alpha1.AnalysisPhaseRunning,
				Measurements: []v1alpha1.Measurement{{
					Value:     "99",
					Phase:     v1alpha1.AnalysisPhaseSuccessful,
					StartedAt: &nowMinus30,
					ResumeAt:  &nowPlus10,
				}},
			}},
		},
	}
	// ensure we requeue at correct interval
	assert.Equal(t, now.Add(time.Second*10), *calculateNextReconcileTime(run, run.Spec.Metrics))
}

// TestCalculateNextReconcileUponError ensure we requeue at error interval when we error

func TestCalculateNextReconcileUponError(t *testing.T) {
	now := metav1.Now()
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{{
				Name: "success-rate",
			}},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{{
				Name:  "success-rate",
				Phase: v1alpha1.AnalysisPhaseRunning,
				Error: 1,
				Measurements: []v1alpha1.Measurement{{
					Value:      "99",
					Phase:      v1alpha1.AnalysisPhaseError,
					StartedAt:  &now,
					FinishedAt: &now,
				}},
			}},
		},
	}
	{
		run := run.DeepCopy()
		assert.Equal(t, now.Add(DefaultErrorRetryInterval), *calculateNextReconcileTime(run, run.Spec.Metrics))
	}
	{
		run := run.DeepCopy()
		run.Spec.Metrics[0].Interval = "5m"
		assert.Equal(t, now.Add(DefaultErrorRetryInterval), *calculateNextReconcileTime(run, run.Spec.Metrics))
	}
}

func TestReconcileAnalysisRunInitial(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{{
				Name:     "success-rate",
				Interval: "60s",
				Provider: v1alpha1.MetricProvider{
					Prometheus: &v1alpha1.PrometheusMetric{},
				},
			}},
		},
	}
	f.provider.On("Run", mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseSuccessful), nil)
	f.provider.On("GetMetadata", mock.Anything, mock.Anything).Return(map[string]string{}, nil)
	{
		newRun := c.reconcileAnalysisRun(run)
		assert.Equal(t, v1alpha1.AnalysisPhaseRunning, newRun.Status.MetricResults[0].Phase)
		assert.Equal(t, v1alpha1.AnalysisPhaseRunning, newRun.Status.Phase)
		assert.Equal(t, 1, len(newRun.Status.MetricResults[0].Measurements))
		assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.MetricResults[0].Measurements[0].Phase)
	}
	{
		// now set count to one and run should be completed immediately
		newCount := intstr.FromInt(1)
		run.Spec.Metrics[0].Count = &newCount
		newRun := c.reconcileAnalysisRun(run)
		assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.MetricResults[0].Phase)
		assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.Phase)
		assert.Equal(t, 1, len(newRun.Status.MetricResults[0].Measurements))
		assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.MetricResults[0].Measurements[0].Phase)
	}
	{
		// run should complete immediately if both count and interval are omitted
		count := intstr.FromInt(0)
		run.Spec.Metrics[0].Count = &count
		run.Spec.Metrics[0].Interval = ""
		newRun := c.reconcileAnalysisRun(run)
		assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.MetricResults[0].Phase)
		assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.Phase)
		assert.Equal(t, 1, len(newRun.Status.MetricResults[0].Measurements))
		assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.MetricResults[0].Measurements[0].Phase)
	}
}

func TestReconcileAnalysisRunInvalid(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{{
				Name: "success-rate",
			}},
		},
	}
	newRun := c.reconcileAnalysisRun(run)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, newRun.Status.Phase)
}

// TestReconcileAnalysisRunTerminateSiblingAfterFail verifies we terminate a metric when we assess
// a sibling has already Failed
func TestReconcileAnalysisRunTerminateSiblingAfterFail(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	// mocks terminate to cancel the in-progress measurement
	f.provider.On("Terminate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseSuccessful), nil)

	for _, status := range []v1alpha1.AnalysisPhase{v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseInconclusive, v1alpha1.AnalysisPhaseError} {
		run := newTerminatingRun(status, false)
		newRun := c.reconcileAnalysisRun(run)

		assert.Equal(t, status, newRun.Status.Phase)
		assert.Equal(t, status, newRun.Status.MetricResults[1].Phase)
		assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.MetricResults[0].Phase)
		// ensure the in-progress measurement is now terminated
		assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.MetricResults[0].Measurements[0].Phase)
		assert.NotNil(t, newRun.Status.MetricResults[0].Measurements[0].FinishedAt)
		assert.Equal(t, "Metric Terminated", newRun.Status.MetricResults[0].Message)
		assert.Equal(t, "Metric Terminated", newRun.Status.MetricResults[0].Measurements[0].Message)
	}
}

func TestReconcileAnalysisRunResumeInProgress(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	run := v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{{
				Name: "test",
				Provider: v1alpha1.MetricProvider{
					Job: &v1alpha1.JobMetric{},
				},
			}},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{{
				Name:  "test",
				Phase: v1alpha1.AnalysisPhaseRunning,
				Measurements: []v1alpha1.Measurement{{
					Phase:     v1alpha1.AnalysisPhaseRunning,
					StartedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
				}},
			}},
		},
	}

	// mocks resume to complete the in-progress measurement
	f.provider.On("Resume", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseSuccessful), nil)

	newRun := c.reconcileAnalysisRun(&run)

	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.Phase)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.MetricResults[0].Phase)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.MetricResults[0].Measurements[0].Phase)
	assert.NotNil(t, newRun.Status.MetricResults[0].Measurements[0].FinishedAt)
}

// TestRunMeasurementsResetConsecutiveErrorCounter verifies we reset the metric consecutiveError counter
// when metric measures success, failed, or inconclusive.
func TestRunMeasurementsResetConsecutiveErrorCounter(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	//for _, status := range []v1alpha1.AnalysisPhase{v1alpha1.AnalysisPhaseSuccessful, v1alpha1.AnalysisPhaseInconclusive, v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseError} {
	for _, status := range []v1alpha1.AnalysisPhase{v1alpha1.AnalysisPhaseError} {
		run := v1alpha1.AnalysisRun{
			Spec: v1alpha1.AnalysisRunSpec{
				Metrics: []v1alpha1.Metric{{
					Name: "test",
					Provider: v1alpha1.MetricProvider{
						Job: &v1alpha1.JobMetric{},
					},
				}},
			},
			Status: v1alpha1.AnalysisRunStatus{
				Phase: v1alpha1.AnalysisPhaseRunning,
				MetricResults: []v1alpha1.MetricResult{{
					Name:             "test",
					Phase:            v1alpha1.AnalysisPhaseRunning,
					ConsecutiveError: 4,
					Count:            4,
					Error:            4,
				}},
			},
		}
		f.provider.On("Run", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(status), nil)

		newRun := c.reconcileAnalysisRun(&run)
		if status == v1alpha1.AnalysisPhaseError {
			assert.Equal(t, int32(5), newRun.Status.MetricResults[0].ConsecutiveError)
			assert.Equal(t, int32(5), newRun.Status.MetricResults[0].Error)
			assert.Equal(t, int32(4), newRun.Status.MetricResults[0].Count)
		} else {
			assert.Equal(t, int32(0), newRun.Status.MetricResults[0].ConsecutiveError)
			assert.Equal(t, int32(4), newRun.Status.MetricResults[0].Error)
			assert.Equal(t, int32(5), newRun.Status.MetricResults[0].Count)
		}
	}
}

// TestTrimMeasurementHistory verifies we trim the measurement list appropriately to the correct length
// and retain the newest measurements
func TestTrimMeasurementHistory(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	f.provider.On("GarbageCollect", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	{
		run := newRun()
		err := c.garbageCollectMeasurements(run, map[string]*v1alpha1.MeasurementRetention{}, 2)
		assert.Nil(t, err)
		assert.Len(t, run.Status.MetricResults[0].Measurements, 1)
		assert.Equal(t, "1", run.Status.MetricResults[0].Measurements[0].Value)
		assert.Len(t, run.Status.MetricResults[1].Measurements, 2)
		assert.Equal(t, "2", run.Status.MetricResults[1].Measurements[0].Value)
		assert.Equal(t, "3", run.Status.MetricResults[1].Measurements[1].Value)
	}
	{
		run := newRun()
		err := c.garbageCollectMeasurements(run, map[string]*v1alpha1.MeasurementRetention{}, 1)
		assert.Nil(t, err)
		assert.Len(t, run.Status.MetricResults[0].Measurements, 1)
		assert.Equal(t, "1", run.Status.MetricResults[0].Measurements[0].Value)
		assert.Len(t, run.Status.MetricResults[1].Measurements, 1)
		assert.Equal(t, "3", run.Status.MetricResults[1].Measurements[0].Value)
	}
	{
		run := newRun()
		var measurementRetentionMetricsMap = map[string]*v1alpha1.MeasurementRetention{}
		measurementRetentionMetricsMap["metric2"] = &v1alpha1.MeasurementRetention{MetricName: "*", Limit: 2}
		err := c.garbageCollectMeasurements(run, measurementRetentionMetricsMap, 1)
		assert.Nil(t, err)
		assert.Len(t, run.Status.MetricResults[0].Measurements, 1)
		assert.Equal(t, "1", run.Status.MetricResults[0].Measurements[0].Value)
		assert.Len(t, run.Status.MetricResults[1].Measurements, 2)
		assert.Equal(t, "2", run.Status.MetricResults[1].Measurements[0].Value)
		assert.Equal(t, "3", run.Status.MetricResults[1].Measurements[1].Value)
	}
	{
		run := newRun()
		var measurementRetentionMetricsMap = map[string]*v1alpha1.MeasurementRetention{}
		measurementRetentionMetricsMap["metric2"] = &v1alpha1.MeasurementRetention{MetricName: "metric2", Limit: 2}
		err := c.garbageCollectMeasurements(run, measurementRetentionMetricsMap, 1)
		assert.Nil(t, err)
		assert.Len(t, run.Status.MetricResults[0].Measurements, 1)
		assert.Equal(t, "1", run.Status.MetricResults[0].Measurements[0].Value)
		assert.Len(t, run.Status.MetricResults[1].Measurements, 2)
		assert.Equal(t, "2", run.Status.MetricResults[1].Measurements[0].Value)
		assert.Equal(t, "3", run.Status.MetricResults[1].Measurements[1].Value)
	}
}

func TestGarbageCollectArgResolution(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	c.newProvider = func(logCtx log.Entry, metric v1alpha1.Metric) (metric.Provider, error) {
		assert.Equal(t, "https://prometheus.kubeaddons:8080", metric.Provider.Prometheus.Address)
		return f.provider, nil
	}

	f.provider.On("GarbageCollect", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name:     "metric1",
					Interval: "60s",
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{
							Address: "https://prometheus.kubeaddons:{{args.port}}",
						},
					},
				},
				{
					Name:     "metric2",
					Interval: "60s",
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{
							Address: "https://prometheus.kubeaddons:{{args.port}}",
						},
					},
				},
			},
		},
		Status: v1alpha1.AnalysisRunStatus{
			Phase: v1alpha1.AnalysisPhaseRunning,
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:  "metric1",
					Phase: v1alpha1.AnalysisPhaseRunning,
					Measurements: []v1alpha1.Measurement{
						{
							Value:      "1",
							Phase:      v1alpha1.AnalysisPhaseSuccessful,
							StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
							FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
						},
						{
							Value:      "2",
							Phase:      v1alpha1.AnalysisPhaseSuccessful,
							StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
							FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
						},
					},
				},
				{
					Name: "metric2",
					Measurements: []v1alpha1.Measurement{
						{
							Value:      "2",
							Phase:      v1alpha1.AnalysisPhaseSuccessful,
							StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
							FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
						},
						{
							Value:      "3",
							Phase:      v1alpha1.AnalysisPhaseSuccessful,
							StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-30 * time.Second))),
							FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-30 * time.Second))),
						},
						{
							Value:      "4",
							Phase:      v1alpha1.AnalysisPhaseSuccessful,
							StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-30 * time.Second))),
							FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-30 * time.Second))),
						},
					},
				},
			},
		},
	}
	run.Spec.Args = append(run.Spec.Args, v1alpha1.Argument{
		Name:  "port",
		Value: pointer.String("8080"),
	})
	var measurementRetentionMetricsMap = map[string]*v1alpha1.MeasurementRetention{}
	measurementRetentionMetricsMap["metric2"] = &v1alpha1.MeasurementRetention{MetricName: "metric2", Limit: 2}
	err := c.garbageCollectMeasurements(run, measurementRetentionMetricsMap, 1)
	assert.Nil(t, err)
	assert.Len(t, run.Status.MetricResults[0].Measurements, 1)
	assert.Equal(t, "2", run.Status.MetricResults[0].Measurements[0].Value)
	assert.Len(t, run.Status.MetricResults[1].Measurements, 2)
	assert.Equal(t, "3", run.Status.MetricResults[1].Measurements[0].Value)
	assert.Equal(t, "4", run.Status.MetricResults[1].Measurements[1].Value)
}

func TestResolveMetricArgsUnableToSubstitute(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)
	// Dry-Run or not if the args resolution fails then we should fail the analysis
	for _, isDryRun := range [3]bool{false, true, false} {
		var dryRunArray []v1alpha1.DryRun
		if isDryRun {
			dryRunArray = append(dryRunArray, v1alpha1.DryRun{MetricName: "*"})
		}
		run := &v1alpha1.AnalysisRun{
			Spec: v1alpha1.AnalysisRunSpec{
				Metrics: []v1alpha1.Metric{{
					Name:             "rate",
					SuccessCondition: "{{args.does-not-exist}}",
					Provider: v1alpha1.MetricProvider{
						Prometheus: &v1alpha1.PrometheusMetric{
							Query: "{{args.metric-name}}",
						},
					},
				}},
				DryRun: dryRunArray,
			},
		}
		newRun := c.reconcileAnalysisRun(run)
		assert.Equal(t, v1alpha1.AnalysisPhaseError, newRun.Status.Phase)
		assert.Equal(t, "Unable to resolve metric arguments: failed to resolve {{args.metric-name}}", newRun.Status.Message)
	}
}

func TestGetMetadataIsCalled(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)
	arg := "success-rate"
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Args: []v1alpha1.Argument{
				{
					Name:  "metric-name",
					Value: &arg,
				},
			},
			Metrics: []v1alpha1.Metric{{
				Name:             "rate",
				SuccessCondition: "result[0] > 0",
				Provider: v1alpha1.MetricProvider{
					Prometheus: &v1alpha1.PrometheusMetric{
						Query: "{{args.metric-name}}",
					},
				},
			}},
		},
	}
	metricMetadata := map[string]string{"foo": "bar"}
	f.provider.On("Run", mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseSuccessful), nil)
	f.provider.On("GetMetadata", mock.Anything, mock.Anything).Return(metricMetadata, nil)
	newRun := c.reconcileAnalysisRun(run)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.Phase)
	assert.Equal(t, metricMetadata, newRun.Status.MetricResults[0].Metadata)
}

// TestSecretContentReferenceSuccess verifies that secret arguments are properly resolved
func TestSecretContentReferenceSuccess(t *testing.T) {
	f := newFixture(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "web-metric-secret",
			Namespace: metav1.NamespaceDefault,
		},
		Data: map[string][]byte{
			"apikey": []byte("12345"),
		},
	}
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)
	f.kubeclient.CoreV1().Secrets(metav1.NamespaceDefault).Create(context.TODO(), secret, metav1.CreateOptions{})
	argName := "apikey"
	run := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.AnalysisRunSpec{
			Args: []v1alpha1.Argument{{
				Name: argName,
				ValueFrom: &v1alpha1.ValueFrom{
					SecretKeyRef: &v1alpha1.SecretKeyRef{
						Name: "web-metric-secret",
						Key:  "apikey",
					},
				},
			}},
			Metrics: []v1alpha1.Metric{{
				Name: "rate",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						Headers: []v1alpha1.WebMetricHeader{{
							Key:   "apikey",
							Value: "{{args.apikey}}",
						}},
					},
				},
			}},
		},
	}
	f.provider.On("Run", mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseSuccessful), nil)
	f.provider.On("GetMetadata", mock.Anything, mock.Anything).Return(map[string]string{}, nil)
	newRun := c.reconcileAnalysisRun(run)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.Phase)
}

// TestSecretContentReferenceProviderError verifies that secret values are redacted in logs
func TestSecretContentReferenceProviderError(t *testing.T) {
	buf := bytes.NewBufferString("")
	log.SetOutput(buf)
	f := newFixture(t)
	secretName, secretKey, secretValue := "web-metric-secret", "apikey", "12345"
	arg := "success-rate"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: metav1.NamespaceDefault,
		},
		Data: map[string][]byte{
			secretKey: []byte(secretValue),
		},
	}
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)
	f.kubeclient.CoreV1().Secrets(metav1.NamespaceDefault).Create(context.TODO(), secret, metav1.CreateOptions{})
	run := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.AnalysisRunSpec{
			Args: []v1alpha1.Argument{
				{
					Name: "secret",
					ValueFrom: &v1alpha1.ValueFrom{
						SecretKeyRef: &v1alpha1.SecretKeyRef{
							Name: secretName,
							Key:  secretKey,
						},
					},
				},
				{
					Name:  "metric-name",
					Value: &arg,
				},
			},
			Metrics: []v1alpha1.Metric{{
				Name:             "rate",
				SuccessCondition: "result > {{args.metric-name}}",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						Headers: []v1alpha1.WebMetricHeader{{
							Key:   "apikey",
							Value: "{{args.secret}}",
						}},
					},
				},
			}},
		},
	}

	error := fmt.Errorf("Error with Header Value: %v", secretValue)
	expectedValue := "Error with Header Value: *****"
	measurement := newMeasurement(v1alpha1.AnalysisPhaseError)
	measurement.Message = error.Error()

	f.provider.On("Run", mock.Anything, mock.Anything, mock.Anything).Return(measurement)
	f.provider.On("GetMetadata", mock.Anything, mock.Anything).Return(map[string]string{}, nil)
	newRun := c.reconcileAnalysisRun(run)
	logMessage := buf.String()

	assert.Equal(t, expectedValue, newRun.Status.MetricResults[0].Measurements[0].Message)
	assert.False(t, strings.Contains(logMessage, "12345"))
	assert.True(t, strings.Contains(logMessage, "*****"))
}

// TestSecretContentReferenceAndMultipleArgResolutionSuccess verifies that both secret and non-secret arguments are resolved properly
func TestSecretContentReferenceAndMultipleArgResolutionSuccess(t *testing.T) {
	f := newFixture(t)
	secretName, secretKey, secretValue := "web-metric-secret", "apikey", "12345"
	arg := "success-rate"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: metav1.NamespaceDefault,
		},
		Data: map[string][]byte{
			secretKey: []byte(secretValue),
		},
	}
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)
	f.kubeclient.CoreV1().Secrets(metav1.NamespaceDefault).Create(context.TODO(), secret, metav1.CreateOptions{})
	run := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.AnalysisRunSpec{
			Args: []v1alpha1.Argument{
				{
					Name: "secret",
					ValueFrom: &v1alpha1.ValueFrom{
						SecretKeyRef: &v1alpha1.SecretKeyRef{
							Name: secretName,
							Key:  secretKey,
						},
					},
				},
				{
					Name:  "metric-name",
					Value: &arg,
				},
			},
			Metrics: []v1alpha1.Metric{{
				Name:             "secret",
				SuccessCondition: "result > {{args.metric-name}}",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{
						Headers: []v1alpha1.WebMetricHeader{{
							Key:   "apikey",
							Value: "{{args.secret}}",
						}},
					},
				},
			}},
		},
	}

	f.provider.On("Run", mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseSuccessful), nil)
	f.provider.On("GetMetadata", mock.Anything, mock.Anything).Return(map[string]string{}, nil)
	newRun := c.reconcileAnalysisRun(run)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.Phase)
}

func TestSecretNotFound(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	args := []v1alpha1.Argument{{
		Name: "secret-does-not-exist",
		ValueFrom: &v1alpha1.ValueFrom{
			SecretKeyRef: &v1alpha1.SecretKeyRef{
				Name: "secret-does-not-exist",
			},
			//SecretKeyRef: nil,
		},
	}}
	tasks := []metricTask{{
		metric: v1alpha1.Metric{
			Name:             "metric-name",
			SuccessCondition: "{{args.secret-does-not-exist}}",
		},
		incompleteMeasurement: nil,
	}}
	_, _, err := c.resolveArgs(tasks, args, metav1.NamespaceDefault)
	assert.Equal(t, "secrets \"secret-does-not-exist\" not found", err.Error())
}

func TestKeyNotInSecret(t *testing.T) {
	f := newFixture(t)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret-name",
			Namespace: metav1.NamespaceDefault,
		},
	}
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)
	f.kubeclient.CoreV1().Secrets(metav1.NamespaceDefault).Create(context.TODO(), secret, metav1.CreateOptions{})

	args := []v1alpha1.Argument{{
		Name: "secret-wrong-key",
		ValueFrom: &v1alpha1.ValueFrom{
			SecretKeyRef: &v1alpha1.SecretKeyRef{
				Name: "secret-name",
				Key:  "key-name",
			},
		},
	}}
	tasks := []metricTask{{
		metric: v1alpha1.Metric{
			Name:             "metric-name",
			SuccessCondition: "{{args.secret-wrong-key}}",
		},
		incompleteMeasurement: nil,
	}}
	_, _, err := c.resolveArgs(tasks, args, metav1.NamespaceDefault)
	assert.Equal(t, "key 'key-name' does not exist in secret 'secret-name'", err.Error())
}

func TestSecretResolution(t *testing.T) {
	f := newFixture(t)
	secretName, secretKey, secretData := "web-metric-secret", "apikey", "12345"
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: metav1.NamespaceDefault,
		},
		Data: map[string][]byte{
			secretKey: []byte(secretData),
		},
	}
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)
	f.kubeclient.CoreV1().Secrets(metav1.NamespaceDefault).Create(context.TODO(), secret, metav1.CreateOptions{})

	args := []v1alpha1.Argument{{
		Name: "secret",
		ValueFrom: &v1alpha1.ValueFrom{
			SecretKeyRef: &v1alpha1.SecretKeyRef{
				Name: secretName,
				Key:  secretKey,
			},
		},
	}}
	tasks := []metricTask{{
		metric: v1alpha1.Metric{
			Name:             "metric-name",
			SuccessCondition: "{{args.secret}}",
		},
		incompleteMeasurement: nil,
	}}
	metricTaskList, secretList, _ := c.resolveArgs(tasks, args, metav1.NamespaceDefault)

	assert.Equal(t, secretData, metricTaskList[0].metric.SuccessCondition)
	assert.Contains(t, secretList, secretData)
}

// TestAssessMetricFailureInconclusiveOrError verifies that assessMetricFailureInconclusiveOrError returns the correct phases and messages
// for Failed, Inconclusive, and Error metrics respectively
func TestAssessMetricFailureInconclusiveOrError(t *testing.T) {
	metric := v1alpha1.Metric{}
	result := v1alpha1.MetricResult{
		Failed: 1,
		Measurements: []v1alpha1.Measurement{{
			Phase: v1alpha1.AnalysisPhaseFailed,
		}},
	}
	phase, msg := assessMetricFailureInconclusiveOrError(metric, result)
	expectedMsg := fmt.Sprintf("failed (%d) > failureLimit (%d)", result.Failed, 0)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, phase)
	assert.Equal(t, expectedMsg, msg)
	assert.Equal(t, phase, assessMetricStatus(metric, result, true))

	result = v1alpha1.MetricResult{
		Inconclusive: 1,
		Measurements: []v1alpha1.Measurement{{
			Phase: v1alpha1.AnalysisPhaseInconclusive,
		}},
	}
	phase, msg = assessMetricFailureInconclusiveOrError(metric, result)
	expectedMsg = fmt.Sprintf("inconclusive (%d) > inconclusiveLimit (%d)", result.Inconclusive, 0)
	assert.Equal(t, v1alpha1.AnalysisPhaseInconclusive, phase)
	assert.Equal(t, expectedMsg, msg)
	assert.Equal(t, phase, assessMetricStatus(metric, result, true))

	result = v1alpha1.MetricResult{
		ConsecutiveError: 5, //default ConsecutiveErrorLimit for Metrics is 4
		Measurements: []v1alpha1.Measurement{{
			Phase: v1alpha1.AnalysisPhaseError,
		}},
	}
	phase, msg = assessMetricFailureInconclusiveOrError(metric, result)
	expectedMsg = fmt.Sprintf("consecutiveErrors (%d) > consecutiveErrorLimit (%d)", result.ConsecutiveError, defaults.DefaultConsecutiveErrorLimit)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, phase)
	assert.Equal(t, expectedMsg, msg)
	assert.Equal(t, phase, assessMetricStatus(metric, result, true))
}

func StartAssessRunStatusErrorMessageAnalysisPhaseFail(t *testing.T, isDryRun bool) (v1alpha1.AnalysisPhase, string, *v1alpha1.RunSummary) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	run := newTerminatingRun(v1alpha1.AnalysisPhaseFailed, isDryRun)
	run.Status.MetricResults[0].Phase = v1alpha1.AnalysisPhaseSuccessful
	status, message := c.assessRunStatus(run, run.Spec.Metrics, map[string]bool{"run-forever": isDryRun, "failed-metric": isDryRun})
	return status, message, run.Status.DryRunSummary
}

func TestAssessRunStatusErrorMessageAnalysisPhaseFail(t *testing.T) {
	status, message, dryRunSummary := StartAssessRunStatusErrorMessageAnalysisPhaseFail(t, false)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, status)
	assert.Equal(t, "Metric \"failed-metric\" assessed Failed due to failed (1) > failureLimit (0)", message)
	expectedDryRunSummary := v1alpha1.RunSummary{
		Count:        0,
		Successful:   0,
		Failed:       0,
		Inconclusive: 0,
		Error:        0,
	}
	assert.Equal(t, &expectedDryRunSummary, dryRunSummary)
}

func TestAssessRunStatusErrorMessageAnalysisPhaseFailInDryRunMode(t *testing.T) {
	status, message, dryRunSummary := StartAssessRunStatusErrorMessageAnalysisPhaseFail(t, true)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
	assert.Equal(t, "", message)
	expectedDryRunSummary := v1alpha1.RunSummary{
		Count:        2,
		Successful:   1,
		Failed:       1,
		Inconclusive: 0,
		Error:        0,
	}
	assert.Equal(t, &expectedDryRunSummary, dryRunSummary)
}

func StartAssessRunStatusErrorMessageFromProvider(t *testing.T, providerMessage string, isDryRun bool) (v1alpha1.AnalysisPhase, string, *v1alpha1.RunSummary) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	run := newTerminatingRun(v1alpha1.AnalysisPhaseFailed, isDryRun)
	run.Status.MetricResults[0].Phase = v1alpha1.AnalysisPhaseSuccessful // All metrics must complete, or assessRunStatus will not return message
	run.Status.MetricResults[1].Message = providerMessage
	status, message := c.assessRunStatus(run, run.Spec.Metrics, map[string]bool{"run-forever": isDryRun, "failed-metric": isDryRun})
	return status, message, run.Status.DryRunSummary
}

// TestAssessRunStatusErrorMessageFromProvider verifies that the message returned by assessRunStatus
// includes the error message from the provider
func TestAssessRunStatusErrorMessageFromProvider(t *testing.T) {
	providerMessage := "Provider Error"
	status, message, dryRunSummary := StartAssessRunStatusErrorMessageFromProvider(t, providerMessage, false)
	expectedMessage := fmt.Sprintf("Metric \"failed-metric\" assessed Failed due to failed (1) > failureLimit (0): \"Error Message: %s\"", providerMessage)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, status)
	assert.Equal(t, expectedMessage, message)
	expectedDryRunSummary := v1alpha1.RunSummary{
		Count:        0,
		Successful:   0,
		Failed:       0,
		Inconclusive: 0,
		Error:        0,
	}
	assert.Equal(t, &expectedDryRunSummary, dryRunSummary)
}

func TestAssessRunStatusErrorMessageFromProviderInDryRunMode(t *testing.T) {
	providerMessage := "Provider Error"
	status, message, dryRunSummary := StartAssessRunStatusErrorMessageFromProvider(t, providerMessage, true)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
	assert.Equal(t, "", message)
	expectedDryRunSummary := v1alpha1.RunSummary{
		Count:        2,
		Successful:   1,
		Failed:       1,
		Inconclusive: 0,
		Error:        0,
	}
	assert.Equal(t, &expectedDryRunSummary, dryRunSummary)
}

func StartAssessRunStatusMultipleFailures(t *testing.T, isDryRun bool) (v1alpha1.AnalysisPhase, string, *v1alpha1.RunSummary) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	run := newTerminatingRun(v1alpha1.AnalysisPhaseFailed, isDryRun)
	run.Status.MetricResults[0].Phase = v1alpha1.AnalysisPhaseFailed
	run.Status.MetricResults[0].Failed = 1
	status, message := c.assessRunStatus(run, run.Spec.Metrics, map[string]bool{"run-forever": isDryRun, "failed-metric": isDryRun})
	return status, message, run.Status.DryRunSummary
}

// TestAssessRunStatusMultipleFailures verifies that if there are multiple failed metrics, assessRunStatus returns the message
// from the first failed metric
func TestAssessRunStatusMultipleFailures(t *testing.T) {
	status, message, dryRunSummary := StartAssessRunStatusMultipleFailures(t, false)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, status)
	assert.Equal(t, "Metric \"run-forever\" assessed Failed due to failed (1) > failureLimit (0)", message)
	expectedDryRunSummary := v1alpha1.RunSummary{
		Count:        0,
		Successful:   0,
		Failed:       0,
		Inconclusive: 0,
		Error:        0,
	}
	assert.Equal(t, &expectedDryRunSummary, dryRunSummary)
}

func TestAssessRunStatusMultipleFailuresInDryRunMode(t *testing.T) {
	status, message, dryRunSummary := StartAssessRunStatusMultipleFailures(t, true)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
	assert.Equal(t, "", message)
	expectedDryRunSummary := v1alpha1.RunSummary{
		Count:        2,
		Successful:   0,
		Failed:       2,
		Inconclusive: 0,
		Error:        0,
	}
	assert.Equal(t, &expectedDryRunSummary, dryRunSummary)
}

func StartAssessRunStatusWorstMessageInReconcileAnalysisRun(t *testing.T, isDryRun bool) *v1alpha1.AnalysisRun {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	run := newTerminatingRun(v1alpha1.AnalysisPhaseFailed, isDryRun)
	run.Status.MetricResults[0].Phase = v1alpha1.AnalysisPhaseFailed
	run.Status.MetricResults[0].Failed = 1

	f.provider.On("Run", mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseFailed), nil)

	return c.reconcileAnalysisRun(run)
}

// TestAssessRunStatusWithOnlyDryRunMetrics verifies that if only dry-run metrics are getting evaluated then, the final
// status of the analysis run is always successful.
func TestAssessRunStatusWithOnlyDryRunMetrics(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	run := v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "success-metric",
					Provider: v1alpha1.MetricProvider{
						Job: &v1alpha1.JobMetric{},
					},
				},
			},
			DryRun: []v1alpha1.DryRun{{
				MetricName: "success-metric",
			}},
		},
		Status: v1alpha1.AnalysisRunStatus{
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:       "success-metric",
					Count:      1,
					Successful: 1,
					DryRun:     true,
					Phase:      v1alpha1.AnalysisPhaseSuccessful,
					Measurements: []v1alpha1.Measurement{{
						Phase:      v1alpha1.AnalysisPhaseSuccessful,
						StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
						FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
					}},
				},
			},
		},
	}

	f.provider.On("Run", mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseFailed), nil)
	f.provider.On("Resume", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseSuccessful), nil)

	newRun := c.reconcileAnalysisRun(&run)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.Phase)
}

// TestAssessRunStatusWithMixedMetrics verifies that the status of dry-run metrics doesn't impact the final state of the
// analysis run.
func TestAssessRunStatusWithMixedMetrics(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	run := v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "run-forever",
					Provider: v1alpha1.MetricProvider{
						Job: &v1alpha1.JobMetric{},
					},
				},
				{
					Name: "success-metric",
					Provider: v1alpha1.MetricProvider{
						Job: &v1alpha1.JobMetric{},
					},
				},
			},
			DryRun: []v1alpha1.DryRun{{
				MetricName: "success-metric",
			}},
		},
		Status: v1alpha1.AnalysisRunStatus{
			MetricResults: []v1alpha1.MetricResult{
				{
					Name:         "run-forever",
					Inconclusive: 1,
					DryRun:       false,
					Phase:        v1alpha1.AnalysisPhaseRunning,
					Measurements: []v1alpha1.Measurement{{
						Phase:     v1alpha1.AnalysisPhaseRunning,
						StartedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
					}},
				},
				{
					Name:   "success-metric",
					Count:  1,
					Failed: 1,
					DryRun: true,
					Phase:  v1alpha1.AnalysisPhaseSuccessful,
					Measurements: []v1alpha1.Measurement{{
						Phase:      v1alpha1.AnalysisPhaseFailed,
						StartedAt:  timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
						FinishedAt: timePtr(metav1.NewTime(time.Now().Add(-60 * time.Second))),
					}},
				},
			},
		},
	}

	f.provider.On("Run", mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseFailed), nil)
	f.provider.On("Resume", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseSuccessful), nil)

	newRun := c.reconcileAnalysisRun(&run)
	assert.Equal(t, v1alpha1.AnalysisPhaseInconclusive, newRun.Status.Phase)
	assert.Equal(t, "Metric \"run-forever\" assessed Inconclusive due to inconclusive (1) > inconclusiveLimit (0)", newRun.Status.Message)
}

// TestAssessRunStatusWorstMessageInReconcileAnalysisRun verifies that the worstMessage returned by assessRunStatus is set as the
// status of the AnalysisRun returned by reconcileAnalysisRun
func TestAssessRunStatusWorstMessageInReconcileAnalysisRun(t *testing.T) {
	newRun := StartAssessRunStatusWorstMessageInReconcileAnalysisRun(t, false)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, newRun.Status.Phase)
	assert.Equal(t, "Metric \"run-forever\" assessed Failed due to failed (1) > failureLimit (0)", newRun.Status.Message)
}

func TestAssessRunStatusWorstMessageInReconcileAnalysisRunInDryRunMode(t *testing.T) {
	newRun := StartAssessRunStatusWorstMessageInReconcileAnalysisRun(t, true)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.Phase)
	assert.Equal(t, "", newRun.Status.Message)
	expectedDryRunSummary := v1alpha1.RunSummary{
		Count:        2,
		Successful:   0,
		Failed:       2,
		Inconclusive: 0,
		Error:        0,
	}
	assert.Equal(t, &expectedDryRunSummary, newRun.Status.DryRunSummary)
	assert.Equal(t, "Metric assessed Failed due to failed (1) > failureLimit (0)", newRun.Status.MetricResults[0].Message)
	assert.Equal(t, "Metric assessed Failed due to failed (1) > failureLimit (0)", newRun.Status.MetricResults[1].Message)
}

func StartTerminatingAnalysisRun(t *testing.T, isDryRun bool) *v1alpha1.AnalysisRun {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	f.provider.On("Run", mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseError), nil)

	now := metav1.Now()
	var dryRunArray []v1alpha1.DryRun
	if isDryRun {
		dryRunArray = append(dryRunArray, v1alpha1.DryRun{MetricName: "success-rate"})
	}
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Terminate: true,
			Args: []v1alpha1.Argument{
				{
					Name:  "service",
					Value: pointer.StringPtr("rollouts-demo-canary.default.svc.cluster.local"),
				},
			},
			Metrics: []v1alpha1.Metric{{
				Name:             "success-rate",
				InitialDelay:     "20s",
				Interval:         "20s",
				SuccessCondition: "result[0] > 0.90",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{},
				},
			}},
			DryRun: dryRunArray,
		},
		Status: v1alpha1.AnalysisRunStatus{
			StartedAt: &now,
			Phase:     v1alpha1.AnalysisPhaseRunning,
		},
	}
	return c.reconcileAnalysisRun(run)
}

func TestTerminateAnalysisRun(t *testing.T) {
	newRun := StartTerminatingAnalysisRun(t, false)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.Phase)
	assert.Equal(t, "Run Terminated", newRun.Status.Message)
}

func TestTerminateAnalysisRunInDryRunMode(t *testing.T) {
	newRun := StartTerminatingAnalysisRun(t, true)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, newRun.Status.Phase)
	assert.Equal(t, "Run Terminated", newRun.Status.Message)
	expectedDryRunSummary := v1alpha1.RunSummary{
		Count:        1,
		Successful:   0,
		Failed:       0,
		Inconclusive: 0,
		Error:        0,
	}
	assert.Equal(t, &expectedDryRunSummary, newRun.Status.DryRunSummary)
}

func TestInvalidDryRunConfigThrowsError(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	// Mocks terminate to cancel the in-progress measurement
	f.provider.On("Terminate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseSuccessful), nil)

	var dryRunArray []v1alpha1.DryRun
	dryRunArray = append(dryRunArray, v1alpha1.DryRun{MetricName: "error-rate"})
	now := metav1.Now()
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Terminate: true,
			Args: []v1alpha1.Argument{
				{
					Name:  "service",
					Value: pointer.StringPtr("rollouts-demo-canary.default.svc.cluster.local"),
				},
			},
			Metrics: []v1alpha1.Metric{{
				Name:             "success-rate",
				InitialDelay:     "20s",
				Interval:         "20s",
				SuccessCondition: "result[0] > 0.90",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{},
				},
			}},
			DryRun: dryRunArray,
		},
		Status: v1alpha1.AnalysisRunStatus{
			StartedAt: &now,
			Phase:     v1alpha1.AnalysisPhaseRunning,
		},
	}
	newRun := c.reconcileAnalysisRun(run)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, newRun.Status.Phase)
	assert.Equal(t, "Analysis spec invalid: dryRun[0]: Rule didn't match any metric name(s)", newRun.Status.Message)
}

func TestInvalidMeasurementsRetentionConfigThrowsError(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)

	// Mocks terminate to cancel the in-progress measurement
	f.provider.On("Terminate", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(newMeasurement(v1alpha1.AnalysisPhaseSuccessful), nil)

	var measurementsRetentionArray []v1alpha1.MeasurementRetention
	measurementsRetentionArray = append(measurementsRetentionArray, v1alpha1.MeasurementRetention{MetricName: "error-rate"})
	now := metav1.Now()
	run := &v1alpha1.AnalysisRun{
		Spec: v1alpha1.AnalysisRunSpec{
			Terminate: true,
			Args: []v1alpha1.Argument{
				{
					Name:  "service",
					Value: pointer.StringPtr("rollouts-demo-canary.default.svc.cluster.local"),
				},
			},
			Metrics: []v1alpha1.Metric{{
				Name:             "success-rate",
				InitialDelay:     "20s",
				Interval:         "20s",
				SuccessCondition: "result[0] > 0.90",
				Provider: v1alpha1.MetricProvider{
					Web: &v1alpha1.WebMetric{},
				},
			}},
			MeasurementRetention: measurementsRetentionArray,
		},
		Status: v1alpha1.AnalysisRunStatus{
			StartedAt: &now,
			Phase:     v1alpha1.AnalysisPhaseRunning,
		},
	}
	newRun := c.reconcileAnalysisRun(run)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, newRun.Status.Phase)
	assert.Equal(t, "Analysis spec invalid: measurementRetention[0]: Rule didn't match any metric name(s)", newRun.Status.Message)
}
