package analysis

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	templateutil "github.com/argoproj/argo-rollouts/utils/template"
)

const (
	// DefaultMeasurementHistoryLimit is the default maximum number of measurements to retain per metric,
	// before trimming the list.
	DefaultMeasurementHistoryLimit = 10
	// DefaultConsecutiveErrorLimit is the default number times a metric can error in sequence before
	// erroring the entire metric.
	DefaultConsecutiveErrorLimit int32 = 4
	// DefaultErrorRetryInterval is the default interval to retry a measurement upon error, in the
	// event an interval was not specified
	DefaultErrorRetryInterval time.Duration = 10 * time.Second
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
	if origRun.Status.Phase.Completed() {
		return origRun
	}
	log := logutil.WithAnalysisRun(origRun)
	run := origRun.DeepCopy()

	if run.Status.MetricResults == nil {
		run.Status.MetricResults = make([]v1alpha1.MetricResult, 0)
		err := analysisutil.ValidateMetrics(run.Spec.Metrics)
		if err != nil {
			message := fmt.Sprintf("analysis spec invalid: %v", err)
			log.Warn(message)
			run.Status.Phase = v1alpha1.AnalysisPhaseError
			run.Status.Message = message
			c.recorder.Eventf(run, corev1.EventTypeWarning, EventReasonStatusFailed, "analysis completed %s", run.Status.Phase)
			return run
		}
	}

	tasks := generateMetricTasks(run)
	log.Infof("taking %d measurements", len(tasks))
	err := c.runMeasurements(run, tasks)
	if err != nil {
		message := fmt.Sprintf("unable to resolve metric arguments: %v", err)
		log.Warn(message)
		run.Status.Phase = v1alpha1.AnalysisPhaseError
		run.Status.Message = message
		c.recorder.Eventf(run, corev1.EventTypeWarning, EventReasonStatusFailed, "analysis completed %s", run.Status.Phase)
		return run
	}

	newStatus := c.asssessRunStatus(run)
	if newStatus != run.Status.Phase {
		message := fmt.Sprintf("analysis transitioned from %s -> %s", run.Status.Phase, newStatus)
		if newStatus.Completed() {
			switch newStatus {
			case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
				c.recorder.Eventf(run, corev1.EventTypeWarning, EventReasonStatusFailed, "analysis completed %s", newStatus)
			default:
				c.recorder.Eventf(run, corev1.EventTypeNormal, EventReasonStatusCompleted, "analysis completed %s", newStatus)
			}
		}
		log.Info(message)
		run.Status.Phase = newStatus
	}

	err = c.garbageCollectMeasurements(run, DefaultMeasurementHistoryLimit)
	if err != nil {
		// TODO(jessesuen): surface errors to controller so they can be retried
		log.Warnf("Failed to garbage collect measurements: %v", err)
	}

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

// Resolves args for single metric in AnalysisRun
// Returns resolved metric
// Uses ResolveQuotedArgs to handle escaped quotes
func (c *AnalysisController) resolveMetricArgs(metric v1alpha1.Metric, args []v1alpha1.Argument) (*v1alpha1.Metric, error) {
	metricBytes, err := json.Marshal(metric) //run.Spec.Metrics)
	if err != nil {
		return nil, err
	}
	var newMetricStr string
	newMetricStr, err = templateutil.ResolveQuotedArgs(string(metricBytes), args) //run.Spec.Args)
	if err != nil {
		return nil, err
	}
	var newMetric v1alpha1.Metric
	err = json.Unmarshal([]byte(newMetricStr), &newMetric)
	if err != nil {
		return nil, err
	}
	return &newMetric, nil
}

// generateMetricTasks generates a list of metrics tasks needed to be measured as part of this
// sync, based on the last completion times that metric was measured (if ever). If the run is
// terminating (e.g. due to manual termination or failing metric), will not schedule further
// measurements other than to resume any in-flight measurements.
func generateMetricTasks(run *v1alpha1.AnalysisRun) []metricTask {
	log := logutil.WithAnalysisRun(run)
	var tasks []metricTask
	terminating := analysisutil.IsTerminating(run)
	for _, metric := range run.Spec.Metrics {
		if analysisutil.MetricCompleted(run, metric.Name) {
			continue
		}
		logCtx := log.WithField("metric", metric.Name)
		lastMeasurement := analysisutil.LastMeasurement(run, metric.Name)
		if lastMeasurement != nil && lastMeasurement.FinishedAt == nil {
			now := metav1.Now()
			if lastMeasurement.ResumeAt != nil && lastMeasurement.ResumeAt.After(now.Time) {
				continue
			}
			// last measurement is still in-progress. need to complete it
			logCtx.Infof("resuming in-progress measurement")
			tasks = append(tasks, metricTask{
				metric:                metric,
				incompleteMeasurement: lastMeasurement,
			})
			continue
		}
		if terminating {
			logCtx.Infof("skipping measurement: run is terminating")
			continue
		}
		if lastMeasurement == nil {
			if metric.InitialDelay != "" {
				if run.Status.StartedAt == nil {
					continue
				}
				duration, err := metric.InitialDelay.Duration()
				if err != nil {
					logCtx.Warnf("failed to parse duration: %v", err)
					continue
				}
				if run.Status.StartedAt.Add(duration).After(time.Now()) {
					logCtx.Infof("waiting until start delay duration passes")
					continue
				}
			}
			// measurement never taken
			tasks = append(tasks, metricTask{metric: metric})
			logCtx.Infof("running initial measurement")
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
		if metric.Interval != "" {
			metricInterval, err := metric.Interval.Duration()
			if err != nil {
				logCtx.Warnf("failed to parse interval: %v", err)
				continue
			}
			interval = metricInterval
		}
		if time.Now().After(lastMeasurement.FinishedAt.Add(interval)) {
			tasks = append(tasks, metricTask{metric: metric})
			logCtx.Infof("running overdue measurement")
			continue
		}
	}
	return tasks
}

// runMeasurements iterates a list of metric tasks, and runs, resumes, or terminates measurements
func (c *AnalysisController) runMeasurements(run *v1alpha1.AnalysisRun, tasks []metricTask) error {
	var wg sync.WaitGroup
	// resultsLock should be held whenever we are accessing or setting status.metricResults since
	// we are performing queries in parallel
	var resultsLock sync.Mutex
	terminating := analysisutil.IsTerminating(run)

	//check args
	//if secret specified in valueFrom, replace value with secret value
	//error if arg has both value and valueFrom
	//create set of secrets for redaction
	secretSet := map[string]bool{}
	args := make([]v1alpha1.Argument, len(run.Spec.Args))
	for i, arg := range run.Spec.Args {
		if arg.ValueFrom != nil { // if arg contains secret
			if arg.Value != nil {
				err := fmt.Errorf("arg %s has both Value and ValueFrom fields", arg.Name)
				return err
			}
			name := arg.ValueFrom.SecretKeyRef.LocalObjectReference.Name
			mySecret, err := c.secretLister.Secrets(run.Namespace).Get(name)
			if err != nil {
				return err
			}

			superSecretContent := string(mySecret.Data[arg.ValueFrom.SecretKeyRef.Key])
			secretSet[superSecretContent] = true
			newArg := arg.DeepCopy()
			newArg.Value = &superSecretContent
			args[i] = *newArg
		} else {
			args[i] = arg
		}
	}

	// creates list of secrets from secretSet for RedactorFormatter
	secrets := make([]string, 0, len(secretSet))
	for k := range secretSet {
		secrets = append(secrets, k)
	}

	// resolves arguments in each metric task
	for i, task := range tasks {
		newMetric, err := c.resolveMetricArgs(task.metric, args)
		if err != nil {
			return err
		}
		tasks[i].metric = *newMetric
	}

	for _, task := range tasks {
		wg.Add(1)

		go func(t metricTask) {
			defer wg.Done()
			//redact secrets from logs
			log := logutil.WithRedactor(*logutil.WithAnalysisRun(run).WithField("metric", t.metric.Name), secrets)

			resultsLock.Lock()
			metricResult := analysisutil.GetResult(run, t.metric.Name)
			resultsLock.Unlock()

			if metricResult == nil {
				metricResult = &v1alpha1.MetricResult{
					Name:  t.metric.Name,
					Phase: v1alpha1.AnalysisPhaseRunning,
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
				newMeasurement.Phase = v1alpha1.AnalysisPhaseError
				newMeasurement.Message = err.Error()
			} else {
				if t.incompleteMeasurement == nil {
					newMeasurement = provider.Run(run, t.metric)
				} else {
					// metric is incomplete. either terminate or resume it
					if terminating {
						log.Infof("terminating in-progress measurement")
						newMeasurement = provider.Terminate(run, t.metric, *t.incompleteMeasurement)
						if newMeasurement.Phase == v1alpha1.AnalysisPhaseSuccessful {
							newMeasurement.Message = "metric terminated"
						}
					} else {
						newMeasurement = provider.Resume(run, t.metric, *t.incompleteMeasurement)
					}
				}
			}

			if newMeasurement.Phase.Completed() {
				log.Infof("measurement completed %s", newMeasurement.Phase)
				if newMeasurement.FinishedAt == nil {
					finishedAt := metav1.Now()
					newMeasurement.FinishedAt = &finishedAt
				}
				switch newMeasurement.Phase {
				case v1alpha1.AnalysisPhaseSuccessful:
					metricResult.Successful++
					metricResult.Count++
					metricResult.ConsecutiveError = 0
				case v1alpha1.AnalysisPhaseFailed:
					metricResult.Failed++
					metricResult.Count++
					metricResult.ConsecutiveError = 0
				case v1alpha1.AnalysisPhaseInconclusive:
					metricResult.Inconclusive++
					metricResult.Count++
					metricResult.ConsecutiveError = 0
				case v1alpha1.AnalysisPhaseError:
					metricResult.Error++
					metricResult.ConsecutiveError++
					log.Warnf("measurement had error: %s", newMeasurement.Message)
				}
			}

			//redact secrets from measurement message
			for _, secret := range secrets {
				newMeasurement.Message = strings.ReplaceAll(newMeasurement.Message, secret, "*****")
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

	return nil
}

// asssessRunStatus assesses the overall status of this AnalysisRun
// If any metric is not yet completed, the AnalysisRun is still considered Running
// Once all metrics are complete, the worst status is used as the overall AnalysisRun status
func (c *AnalysisController) asssessRunStatus(run *v1alpha1.AnalysisRun) v1alpha1.AnalysisPhase {
	var worstStatus v1alpha1.AnalysisPhase
	terminating := analysisutil.IsTerminating(run)
	everythingCompleted := true

	if run.Status.StartedAt == nil {
		now := metav1.Now()
		run.Status.StartedAt = &now
	}

	// Iterate all metrics and update MetricResult.Phase fields based on lastest measurement(s)
	for _, metric := range run.Spec.Metrics {
		if result := analysisutil.GetResult(run, metric.Name); result != nil {
			log := logutil.WithAnalysisRun(run).WithField("metric", metric.Name)
			metricStatus := assessMetricStatus(metric, *result, terminating)
			if result.Phase != metricStatus {
				log.Infof("metric transitioned from %s -> %s", result.Phase, metricStatus)
				if metricStatus.Completed() {
					switch metricStatus {
					case v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseFailed:
						c.recorder.Eventf(run, corev1.EventTypeWarning, EventReasonStatusFailed, "metric '%s' completed %s", metric.Name, metricStatus)
					default:
						c.recorder.Eventf(run, corev1.EventTypeNormal, EventReasonStatusCompleted, "metric '%s' completed %s", metric.Name, metricStatus)
					}
				}
				if lastMeasurement := analysisutil.LastMeasurement(run, metric.Name); lastMeasurement != nil {
					result.Message = lastMeasurement.Message
				}
				result.Phase = metricStatus
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
		return v1alpha1.AnalysisPhaseRunning
	}
	return worstStatus
}

// assessMetricStatus assesses the status of a single metric based on:
// * current/latest measurement status
// * parameters given by the metric (failureLimit, count, etc...)
// * whether or not we are terminating (e.g. due to failing run, or termination request)
func assessMetricStatus(metric v1alpha1.Metric, result v1alpha1.MetricResult, terminating bool) v1alpha1.AnalysisPhase {
	if result.Phase.Completed() {
		return result.Phase
	}
	log := log.WithField("metric", metric.Name)
	if len(result.Measurements) == 0 {
		if terminating {
			// we have yet to take a single measurement, but have already been instructed to stop
			log.Infof("metric assessed %s: run terminated", v1alpha1.AnalysisPhaseSuccessful)
			return v1alpha1.AnalysisPhaseSuccessful
		}
		return v1alpha1.AnalysisPhasePending
	}
	lastMeasurement := result.Measurements[len(result.Measurements)-1]
	if !lastMeasurement.Phase.Completed() {
		// we still have a in-flight measurement
		return v1alpha1.AnalysisPhaseRunning
	}
	if result.Failed > metric.FailureLimit {
		log.Infof("metric assessed %s: failed (%d) > failureLimit (%d)", v1alpha1.AnalysisPhaseFailed, result.Failed, metric.FailureLimit)
		return v1alpha1.AnalysisPhaseFailed
	}
	if result.Inconclusive > metric.InconclusiveLimit {
		log.Infof("metric assessed %s: inconclusive (%d) > inconclusiveLimit (%d)", v1alpha1.AnalysisPhaseInconclusive, result.Inconclusive, metric.InconclusiveLimit)
		return v1alpha1.AnalysisPhaseInconclusive
	}
	consecutiveErrorLimit := DefaultConsecutiveErrorLimit
	if metric.ConsecutiveErrorLimit != nil {
		consecutiveErrorLimit = *metric.ConsecutiveErrorLimit
	}
	if result.ConsecutiveError > consecutiveErrorLimit {
		log.Infof("metric assessed %s: consecutiveErrors (%d) > consecutiveErrorLimit (%d)", v1alpha1.AnalysisPhaseError, result.ConsecutiveError, consecutiveErrorLimit)
		return v1alpha1.AnalysisPhaseError
	}
	// If a count was specified, and we reached that count, then metric is considered Successful.
	// The Error, Failed, Inconclusive counters are ignored because those checks have already been
	// taken into consideration above, and we do not want to fail if failures < failureLimit.
	effectiveCount := metric.EffectiveCount()
	if effectiveCount != nil && result.Count >= *effectiveCount {
		log.Infof("metric assessed %s: count (%d) reached", v1alpha1.AnalysisPhaseSuccessful, *effectiveCount)
		return v1alpha1.AnalysisPhaseSuccessful
	}
	// if we get here, this metric runs indefinitely
	if terminating {
		log.Infof("metric assessed %s: run terminated", v1alpha1.AnalysisPhaseSuccessful)
		return v1alpha1.AnalysisPhaseSuccessful
	}
	return v1alpha1.AnalysisPhaseRunning
}

// calculateNextReconcileTime calculates the next time that this AnalysisRun should be reconciled,
// based on the earliest time of all metrics intervals, counts, and their finishedAt timestamps
func calculateNextReconcileTime(run *v1alpha1.AnalysisRun) *time.Time {
	var reconcileTime *time.Time
	for _, metric := range run.Spec.Metrics {
		if analysisutil.MetricCompleted(run, metric.Name) {
			// NOTE: this also covers the case where metric.Count is reached
			continue
		}
		logCtx := logutil.WithAnalysisRun(run).WithField("metric", metric.Name)
		lastMeasurement := analysisutil.LastMeasurement(run, metric.Name)
		if lastMeasurement == nil {
			if metric.InitialDelay != "" {
				startTime := metav1.Now()
				if run.Status.StartedAt != nil {
					startTime = *run.Status.StartedAt
				}
				duration, err := metric.InitialDelay.Duration()
				if err != nil {
					logCtx.Warnf("failed to parse interval: %v", err)
					continue
				}
				endInitialDelay := startTime.Add(duration)
				if reconcileTime == nil || reconcileTime.After(endInitialDelay) {
					reconcileTime = &endInitialDelay
				}
				continue
			}
			// no measurement was started . we should never get here
			logCtx.Warnf("metric never started. not factored into enqueue time")
			continue
		}
		if lastMeasurement.FinishedAt == nil {
			// unfinished in-flight measurement.
			if lastMeasurement.ResumeAt != nil {
				if reconcileTime == nil || reconcileTime.After(lastMeasurement.ResumeAt.Time) {
					reconcileTime = &lastMeasurement.ResumeAt.Time
				}
			}
			continue
		}
		metricResult := analysisutil.GetResult(run, metric.Name)
		effectiveCount := metric.EffectiveCount()
		if effectiveCount != nil && metricResult.Count >= *effectiveCount {
			// we have reached desired count
			continue
		}
		var interval time.Duration
		if metric.Interval != "" {
			metricInterval, err := metric.Interval.Duration()
			if err != nil {
				logCtx.Warnf("failed to parse interval: %v", err)
				continue
			}
			interval = metricInterval
		} else if lastMeasurement.Phase == v1alpha1.AnalysisPhaseError {
			interval = DefaultErrorRetryInterval
		} else {
			// if we get here, an interval was not set (meaning reoccurrence was not desired), and
			// there was no error (meaning we don't need to retry). no need to requeue this metric.
			// NOTE: we shouldn't ever get here since it means we are not doing proper bookkeeping
			// of count.
			logCtx.Warnf("skipping requeue. no interval or error (count: %d, effectiveCount: %d)", metricResult.Count, metric.EffectiveCount())
			continue
		}
		// Take the earliest time of all metrics
		metricReconcileTime := lastMeasurement.FinishedAt.Add(interval)
		if reconcileTime == nil || reconcileTime.After(metricReconcileTime) {
			reconcileTime = &metricReconcileTime
		}
	}
	return reconcileTime
}

// garbageCollectMeasurements trims the measurement history to the specified limit and GCs old measurements
func (c *AnalysisController) garbageCollectMeasurements(run *v1alpha1.AnalysisRun, limit int) error {
	var errors []error

	metricsByName := make(map[string]v1alpha1.Metric)
	for _, metric := range run.Spec.Metrics {
		metricsByName[metric.Name] = metric
	}

	for i, result := range run.Status.MetricResults {
		length := len(result.Measurements)
		if length > limit {
			metric, ok := metricsByName[result.Name]
			if !ok {
				continue
			}
			log := logutil.WithAnalysisRun(run).WithField("metric", metric.Name)
			provider, err := c.newProvider(*log, metric)
			if err != nil {
				errors = append(errors, err)
				continue
			}
			err = provider.GarbageCollect(run, metric, limit)
			if err != nil {
				return err
			}
			result.Measurements = result.Measurements[length-limit : length]
		}
		run.Status.MetricResults[i] = result
	}
	if len(errors) > 0 {
		return errors[0]
	}
	return nil
}
