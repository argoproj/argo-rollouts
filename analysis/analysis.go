package analysis

import (
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const (
	// DefaultMeasurementHistoryLimit is the default maximum number of measurements to retain per metric,
	// before trimming the list.
	DefaultMeasurementHistoryLimit = 10
	// DefaultMaxConsecutiveErrors is the default number times a metric can error in sequence before
	// erroring the entire metric.
	DefaultMaxConsecutiveErrors int32 = 4
	// DefaultErrorRetryInterval is the default interval to retry a measurement upon error, in the
	// event an interval was not specified
	DefaultErrorRetryInterval int32 = 10
)

// Event reasons for analysis events
const (
	EventReasonStatusFailed    = "Failed"
	EventReasonStatusCompleted = "Complete"
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
			c.recorder.Eventf(run, corev1.EventTypeWarning, EventReasonStatusFailed, "analysis completed %s", run.Status.Status)
			return run
		}
	}
	tasks := generateMetricTasks(run)
	log.Infof("taking %d measurements", len(tasks))
	c.runMeasurements(run, tasks)

	newStatus := c.asssessRunStatus(run)
	if newStatus != run.Status.Status {
		message := fmt.Sprintf("analysis transitioned from %s -> %s", run.Status.Status, newStatus)
		if newStatus.Completed() {
			switch newStatus {
			case v1alpha1.AnalysisStatusError, v1alpha1.AnalysisStatusFailed:
				c.recorder.Eventf(run, corev1.EventTypeWarning, EventReasonStatusFailed, "analysis completed %s", newStatus)
			default:
				c.recorder.Eventf(run, corev1.EventTypeNormal, EventReasonStatusCompleted, "analysis completed %s", newStatus)
			}
		}
		log.Info(message)
		run.Status.Status = newStatus
	}

	trimMeasurementHistory(run, DefaultMeasurementHistoryLimit)

	nextReconcileTime := calculateNextReconcileTime(run)
	if nextReconcileTime != nil {
		enqueueSeconds := nextReconcileTime.Sub(time.Now())
		if enqueueSeconds < 0 {
			enqueueSeconds = 0
		}
		log.Infof("enqueuing analysis after %v", enqueueSeconds)
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
		metricResult := analysisutil.GetResult(run, metric.Name)
		effectiveCount := metric.EffectiveCount()
		if effectiveCount != nil && metricResult.Count >= *effectiveCount {
			// we have reached desired count
			continue
		}
		// if we get here, we know we need to take a measurement (eventually). check last measurement
		// to decide if it should be taken now. metric.Interval can be null because we may be
		// retrying a metric due to error.
		interval := DefaultErrorRetryInterval
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

// runMeasurements iterates a list of metric tasks, and runs, resumes, or terminates measurements
func (c *AnalysisController) runMeasurements(run *v1alpha1.AnalysisRun, tasks []metricTask) {
	var wg sync.WaitGroup
	// resultsLock should be held whenever we are accessing or setting status.metricResults since
	// we are performing queries in parallel
	var resultsLock sync.Mutex
	terminating := analysisutil.IsTerminating(run)

	for _, task := range tasks {
		wg.Add(1)

		go func(t metricTask) {
			defer wg.Done()
			log := logutil.WithAnalysisRun(run).WithField("metric", t.metric.Name)

			resultsLock.Lock()
			metricResult := analysisutil.GetResult(run, t.metric.Name)
			resultsLock.Unlock()

			if metricResult == nil {
				metricResult = &v1alpha1.MetricResult{
					Name:   t.metric.Name,
					Status: v1alpha1.AnalysisStatusRunning,
				}
			}

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
				newMeasurement.Message = err.Error()
			} else {
				if t.incompleteMeasurement == nil {
					newMeasurement = provider.Run(run, t.metric, run.Spec.Arguments)
				} else {
					// metric is incomplete. either terminate or resume it
					if terminating {
						log.Infof("terminating in-progress measurement")
						newMeasurement = provider.Terminate(run, t.metric, run.Spec.Arguments, *t.incompleteMeasurement)
						if newMeasurement.Status == v1alpha1.AnalysisStatusSuccessful {
							newMeasurement.Message = "metric terminated"
						}
					} else {
						newMeasurement = provider.Resume(run, t.metric, run.Spec.Arguments, *t.incompleteMeasurement)
					}
				}
			}

			if newMeasurement.Status.Completed() {
				log.Infof("measurement completed %s", newMeasurement.Status)
				if newMeasurement.FinishedAt == nil {
					finishedAt := metav1.Now()
					newMeasurement.FinishedAt = &finishedAt
				}
				switch newMeasurement.Status {
				case v1alpha1.AnalysisStatusSuccessful:
					metricResult.Successful++
					metricResult.Count++
					metricResult.ConsecutiveError = 0
				case v1alpha1.AnalysisStatusFailed:
					metricResult.Failed++
					metricResult.Count++
					metricResult.ConsecutiveError = 0
				case v1alpha1.AnalysisStatusInconclusive:
					metricResult.Inconclusive++
					metricResult.Count++
					metricResult.ConsecutiveError = 0
				case v1alpha1.AnalysisStatusError:
					metricResult.Error++
					metricResult.ConsecutiveError++
					log.Warnf("measurement had error: %s", newMeasurement.Message)
				}
			}
			if t.incompleteMeasurement == nil {
				metricResult.Measurements = append(metricResult.Measurements, newMeasurement)
			} else {
				metricResult.Measurements[len(metricResult.Measurements)-1] = newMeasurement
			}

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
func (c *AnalysisController) asssessRunStatus(run *v1alpha1.AnalysisRun) v1alpha1.AnalysisStatus {
	var worstStatus v1alpha1.AnalysisStatus
	terminating := analysisutil.IsTerminating(run)
	everythingCompleted := true

	// Iterate all metrics and update MetricResult.Status fields based on lastest measurement(s)
	for _, metric := range run.Spec.AnalysisSpec.Metrics {
		if result := analysisutil.GetResult(run, metric.Name); result != nil {
			log := logutil.WithAnalysisRun(run).WithField("metric", metric.Name)
			metricStatus := assessMetricStatus(metric, *result, terminating)
			if result.Status != metricStatus {
				log.Infof("metric transitioned from %s -> %s", result.Status, metricStatus)
				if metricStatus.Completed() {
					switch metricStatus {
					case v1alpha1.AnalysisStatusError, v1alpha1.AnalysisStatusFailed:
						c.recorder.Eventf(run, corev1.EventTypeWarning, EventReasonStatusFailed, "metric '%s' completed %s", metric.Name, metricStatus)
					default:
						c.recorder.Eventf(run, corev1.EventTypeNormal, EventReasonStatusCompleted, "metric '%s' completed %s", metric.Name, metricStatus)
					}
				}
				if lastMeasurement := analysisutil.LastMeasurement(run, metric.Name); lastMeasurement != nil {
					result.Message = lastMeasurement.Message
				}
				result.Status = metricStatus
				analysisutil.SetResult(run, *result)
			}
			if !metricStatus.Completed() {
				// if any metric is in-progress, then entire analysis run will be considered running
				everythingCompleted = false
			} else {
				// otherwise, remember the worst status of all completed metric results
				if worstStatus == "" {
					worstStatus = metricStatus
				} else {
					if analysisutil.IsWorse(worstStatus, metricStatus) {
						worstStatus = metricStatus
					}
				}
			}
		}
	}
	if !everythingCompleted || worstStatus == "" {
		return v1alpha1.AnalysisStatusRunning
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
	if result.Inconclusive > metric.MaxInconclusive {
		log.Infof("metric assessed %s: inconclusive (%d) > maxInconclusive (%d)", v1alpha1.AnalysisStatusInconclusive, result.Inconclusive, metric.MaxInconclusive)
		return v1alpha1.AnalysisStatusInconclusive
	}
	maxConsecutiveErrors := DefaultMaxConsecutiveErrors
	if metric.MaxConsecutiveErrors != nil {
		maxConsecutiveErrors = *metric.MaxConsecutiveErrors
	}
	if result.ConsecutiveError > maxConsecutiveErrors {
		log.Infof("metric assessed %s: consecutiveErrors (%d) > maxConsecutiveErrors (%d)", v1alpha1.AnalysisStatusError, result.ConsecutiveError, maxConsecutiveErrors)
		return v1alpha1.AnalysisStatusError
	}
	// If a count was specified, and we reached that count, then metric is considered Successful.
	// The Error, Failed, Inconclusive counters are ignored because those checks have already been
	// taken into consideration above, and we do not want to fail if failures < maxFailures.
	effectiveCount := metric.EffectiveCount()
	if effectiveCount != nil && result.Count >= *effectiveCount {
		log.Infof("metric assessed %s: count (%d) reached", v1alpha1.AnalysisStatusSuccessful, *effectiveCount)
		return v1alpha1.AnalysisStatusSuccessful
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
		metricResult := analysisutil.GetResult(run, metric.Name)
		effectiveCount := metric.EffectiveCount()
		if effectiveCount != nil && metricResult.Count >= *effectiveCount {
			// we have reached desired count
			continue
		}
		var interval int32
		if metric.Interval != nil {
			interval = *metric.Interval
		} else if lastMeasurement.Status == v1alpha1.AnalysisStatusError {
			interval = DefaultErrorRetryInterval
		} else {
			// if we get here, an interval was not set (meaning reoccurrence was not desired), and
			// there was no error (meaning we don't need to retry). no need to requeue this metric.
			// NOTE: we shouldn't ever get here since it means we are not doing proper bookkeeping
			// of count.
			log.WithField("metric", metric.Name).Warnf("skipping requeue. no interval or error (count: %d, effectiveCount: %d)", metricResult.Count, metric.EffectiveCount())
			continue
		}
		// Take the earliest time of all metrics
		metricReconcileTime := lastMeasurement.FinishedAt.Add(time.Duration(interval) * time.Second)
		if reconcileTime == nil || reconcileTime.After(metricReconcileTime) {
			reconcileTime = &metricReconcileTime
		}
	}
	return reconcileTime
}

// trimMeasurementHistory trims the measurement history to the specified limit
func trimMeasurementHistory(run *v1alpha1.AnalysisRun, limit int) {
	if run.Status == nil {
		return
	}
	for i, result := range run.Status.MetricResults {
		length := len(result.Measurements)
		if length > limit {
			result.Measurements = result.Measurements[length-limit : length]
		}
		run.Status.MetricResults[i] = result
	}
}
