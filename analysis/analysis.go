package analysis

import (
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const (
	// DefaultMaxConsecutiveErrors is the default number times a metric can error in sequence before
	// erroring the entire metric.
	DefaultMaxConsecutiveErrors = 4
)

// metricTask holds the metric which need to be measured during this reconciliation along with
// an in-progress measurement
type metricTask struct {
	metric                v1alpha1.Metric
	incompleteMeasurement *v1alpha1.Measurement
}

func (c *AnalysisController) reconcileAnalysisRun(origRun *v1alpha1.AnalysisRun) *v1alpha1.AnalysisRun {
	if origRun.Status != nil && origRun.Status.Status.Completed() {
		return origRun
	}
	log := logutil.WithAnalysisRun(origRun)
	run := origRun.DeepCopy()

	if run.Status == nil {
		run.Status = &v1alpha1.AnalysisRunStatus{
			MetricResults: make([]v1alpha1.MetricResult, 0),
		}
		err := analysisutil.ValidateAnalysisTemplateSpec(run.Spec.AnalysisSpec)
		if err != nil {
			message := fmt.Sprintf("analysis spec invalid: %v", err)
			log.Warn(message)
			run.Status.Status = v1alpha1.AnalysisStatusError
			run.Status.Message = message
			return run
		}
	}
	tasks := generateMetricTasks(run)
	log.Infof("taking %d measurements", len(tasks))
	c.runMeasurements(run, tasks)

	newStatus := asssessRunStatus(run)
	if newStatus != run.Status.Status {
		log.Infof("analysis transitioned from %s -> %s", run.Status.Status, newStatus)
		run.Status.Status = newStatus
	}

	nextReconcileTime := calculateNextReconcileTime(run)
	if nextReconcileTime != nil {
		enqueueSeconds := nextReconcileTime.Sub(time.Now())
		if enqueueSeconds < 0 {
			enqueueSeconds = 0
		}
		c.enqueueAnalysisAfter(run, enqueueSeconds)
	}
	return run
}

// generateMetricTasks generates a list of metrics tasks needed to be measured as part of this
// sync, based on the last completion times that metric was measured (if ever). If the run is
// terminating (e.g. due to manual termination or failing metric), will not schedule further
// measurements other than to resume any in-flight measurements.
func generateMetricTasks(run *v1alpha1.AnalysisRun) []metricTask {
	log := logutil.WithAnalysisRun(run)
	var tasks []metricTask
	terminating := analysisutil.IsTerminating(run)
	for _, metric := range run.Spec.AnalysisSpec.Metrics {
		if analysisutil.MetricCompleted(run, metric.Name) {
			continue
		}
		lastMeasurement := analysisutil.LastMeasurement(run, metric.Name)
		if lastMeasurement != nil && lastMeasurement.FinishedAt == nil {
			// last measurement is still in-progress. need to complete it
			log.WithField("metric", metric.Name).Infof("resuming in-progress measurement")
			tasks = append(tasks, metricTask{
				metric:                metric,
				incompleteMeasurement: lastMeasurement,
			})
			continue
		}
		if terminating {
			log.WithField("metric", metric.Name).Infof("skipping measurement: run is terminating")
			continue
		}
		if lastMeasurement == nil {
			// measurement never taken
			tasks = append(tasks, metricTask{metric: metric})
			log.WithField("metric", metric.Name).Infof("running initial measurement")
			continue
		}
		metricResult := analysisutil.MetricResult(run, metric.Name)
		effectiveCount := metric.EffectiveCount()
		if effectiveCount != nil && metricResult.Count >= *effectiveCount {
			// we have reached desired count
			continue
		}
		// if we get here, we know we need to take a measurement (eventually). check last measurement
		// to decide if it should be taken now. metric.Interval can be null because we may be
		// retrying a metric due to error.
		interval := int32(10)
		if metric.Interval != nil {
			interval = *metric.Interval
		}
		if time.Now().After(lastMeasurement.FinishedAt.Add(time.Duration(interval) * time.Second)) {
			tasks = append(tasks, metricTask{metric: metric})
			log.WithField("metric", metric.Name).Infof("running overdue measurement")
			continue
		}
	}
	return tasks
}

// runMeasurements iterates a list of metric tasks, and runs or resumes measurements
func (c *AnalysisController) runMeasurements(run *v1alpha1.AnalysisRun, tasks []metricTask) {
	var wg sync.WaitGroup
	var resultsLock sync.Mutex
	for _, task := range tasks {
		wg.Add(1)

		go func(t metricTask) {
			defer wg.Done()

			log := logutil.WithAnalysisRun(run).WithField("metric", t.metric.Name)

			resultsLock.Lock()
			metricResult := analysisutil.MetricResult(run, t.metric.Name)
			if metricResult == nil {
				metricResult = &v1alpha1.MetricResult{}
			}
			resultsLock.Unlock()

			var newMeasurement v1alpha1.Measurement
			provider, err := c.newProvider(*log, t.metric)
			if err != nil {
				if t.incompleteMeasurement != nil {
					newMeasurement = *t.incompleteMeasurement
				} else {
					startedAt := metav1.Now()
					newMeasurement.StartedAt = &startedAt
				}
				newMeasurement.Status = v1alpha1.AnalysisStatusError
			} else {
				if t.incompleteMeasurement == nil {
					newMeasurement, err = provider.Run(t.metric, run.Spec.Arguments)
				} else {
					newMeasurement, err = provider.Resume(t.metric, run.Spec.Arguments, *t.incompleteMeasurement)
				}
			}

			if newMeasurement.Status.Completed() {
				if newMeasurement.FinishedAt == nil {
					finishedAt := metav1.Now()
					newMeasurement.FinishedAt = &finishedAt
				}
				switch newMeasurement.Status {
				case v1alpha1.AnalysisStatusSuccessful:
					metricResult.Successful++
					metricResult.Count++
				case v1alpha1.AnalysisStatusFailed:
					metricResult.Failed++
					metricResult.Count++
				case v1alpha1.AnalysisStatusInconclusive:
					metricResult.Inconclusive++
					metricResult.Count++
				case v1alpha1.AnalysisStatusError:
					metricResult.Error++
				}
			}
			if t.incompleteMeasurement == nil {
				metricResult.Measurements = append(metricResult.Measurements, newMeasurement)
			} else {
				metricResult.Measurements[len(metricResult.Measurements)-1] = newMeasurement
			}
			if err != nil {
				metricResult.Message = err.Error()
			} else {
				metricResult.Message = ""
			}
			metricResult.Name = t.metric.Name
			resultsLock.Lock()
			analysisutil.SetResult(run, *metricResult)
			resultsLock.Unlock()

		}(task)
	}
	wg.Wait()
}

// asssessRunStatus assesses the overall status of this AnalysisRun
// If any metric is not yet completed, the AnalysisRun is still considered Running
// Once all metrics are complete, the worst status is used as the overall AnalysisRun status
func asssessRunStatus(run *v1alpha1.AnalysisRun) v1alpha1.AnalysisStatus {
	var worstStatus v1alpha1.AnalysisStatus
	terminating := analysisutil.IsTerminating(run)

	for _, metric := range run.Spec.AnalysisSpec.Metrics {
		if result := analysisutil.MetricResult(run, metric.Name); result != nil {
			log := logutil.WithAnalysisRun(run).WithField("metric", metric.Name)
			metricStatus := assessMetricStatus(metric, *result, terminating)
			if result.Status != metricStatus {
				log.Infof("metric transitioned from %s -> %s", result.Status, metricStatus)
				result.Status = metricStatus
				analysisutil.SetResult(run, *result)
			}
			if !metricStatus.Completed() {
				// if any metric is not completed, then entire analysis run is considered running
				return v1alpha1.AnalysisStatusRunning
			}
			if worstStatus == "" {
				worstStatus = metricStatus
			} else {
				if analysisutil.IsWorse(worstStatus, metricStatus) {
					worstStatus = metricStatus
				}
			}
		}
	}
	return worstStatus
}

// assessMetricStatus assesses the status of a single metric based on:
// * current/latest measurement status
// * parameters given by the metric (maxFailures, count, etc...)
// * whether or not we are terminating (e.g. due to failing run, or termination request)
func assessMetricStatus(metric v1alpha1.Metric, result v1alpha1.MetricResult, terminating bool) v1alpha1.AnalysisStatus {
	if result.Status.Completed() {
		return result.Status
	}
	log := log.WithField("metric", metric.Name)
	if len(result.Measurements) == 0 {
		if terminating {
			// we have yet to take a single measurement, but have already been instructed to stop
			log.Infof("metric assessed %s: run terminated", v1alpha1.AnalysisStatusSuccessful)
			return v1alpha1.AnalysisStatusSuccessful
		}
		return v1alpha1.AnalysisStatusPending
	}
	lastMeasurement := result.Measurements[len(result.Measurements)-1]
	if !lastMeasurement.Status.Completed() {
		// we still have a in-flight measurement
		return v1alpha1.AnalysisStatusRunning
	}
	if result.Failed > metric.MaxFailures {
		log.Infof("metric assessed %s: failed (%d) > maxFailures (%d)", v1alpha1.AnalysisStatusFailed, result.Failed, metric.MaxFailures)
		return v1alpha1.AnalysisStatusFailed
	}
	consecutiveErrors := analysisutil.ConsecutiveErrors(result)
	maxConsecutiveErrors := DefaultMaxConsecutiveErrors
	if metric.MaxConsecutiveErrors != nil {
		maxConsecutiveErrors = int(*metric.MaxConsecutiveErrors)
	}
	if consecutiveErrors > maxConsecutiveErrors {
		log.Infof("metric assessed %s: consecutiveErrors (%d) > maxConsecutiveErrors (%d)", v1alpha1.AnalysisStatusError, consecutiveErrors, maxConsecutiveErrors)
		return v1alpha1.AnalysisStatusError
	}
	// If a count was specified, and we reached that count, then we assess the status based on
	// the greater of the Successful & Inconclusive status counters.
	// Error and Failed counters are ignored because those checks have already been taken into
	// consideration above, and we do not want to fail the metric if failures < maxFailures.
	// TODO(jessesuen): may need to tweak this logic
	effectiveCount := metric.EffectiveCount()
	if effectiveCount != nil && result.Count >= *effectiveCount {
		var status v1alpha1.AnalysisStatus
		if result.Successful > result.Inconclusive {
			status = v1alpha1.AnalysisStatusSuccessful
		} else {
			status = v1alpha1.AnalysisStatusInconclusive
		}
		log.Infof("metric assessed %s: count %d reached, successful: %d, inconclusive: %d, errors: %d, failures: %d",
			status, result.Count, result.Successful, result.Inconclusive, result.Error, result.Failed)
		return status
	}
	// if we get here, this metric runs indefinitely
	if terminating {
		log.Infof("metric assessed %s: run terminated", v1alpha1.AnalysisStatusSuccessful)
		return v1alpha1.AnalysisStatusSuccessful
	}
	return v1alpha1.AnalysisStatusRunning
}

// calculateNextReconcileTime calculates the next time that this AnalysisRun should be reconciled,
// based on the earliest time of all metrics intervals, counts, and their finishedAt timestamps
func calculateNextReconcileTime(run *v1alpha1.AnalysisRun) *time.Time {
	log := logutil.WithAnalysisRun(run)
	var reconcileTime *time.Time
	for _, metric := range run.Spec.AnalysisSpec.Metrics {
		if analysisutil.MetricCompleted(run, metric.Name) {
			// NOTE: this also covers the case where metric.Count is reached
			continue
		}
		lastMeasurement := analysisutil.LastMeasurement(run, metric.Name)
		if lastMeasurement == nil {
			// no measurement was started. we should never get here
			log.WithField("metric", metric.Name).Warnf("metric never started. not factored into enqueue time")
			continue
		}
		if lastMeasurement.FinishedAt == nil {
			// unfinished in-flight measurement.
			// TODO(jessesuen) perhaps ask provider for an appropriate time to poll?
			continue
		}
		if metric.Interval == nil {
			// a measurement was already taken, and reoccurrence was not desired
			continue
		}
		// Take the earliest time of all metrics
		metricReconcileTime := lastMeasurement.FinishedAt.Add(time.Duration(*metric.Interval) * time.Second)
		if reconcileTime == nil || reconcileTime.After(metricReconcileTime) {
			reconcileTime = &metricReconcileTime
		}
	}
	return reconcileTime
}
