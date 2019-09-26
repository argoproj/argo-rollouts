package analysis

import (
	"sync"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

// metricTask holds the metric which need to be measured during this reconciliation along with
// an in-progress measurement
type metricTask struct {
	metric                *v1alpha1.Metric
	incompleteMeasurement *v1alpha1.Measurement
}

func (c *AnalysisController) reconcileAnalysisRun(origRun *v1alpha1.AnalysisRun) error {
	if origRun.Status.Status.Completed() {
		return nil
	}
	log := logutil.WithAnalysisRun(origRun)
	run := origRun.DeepCopy()
	if run.Status.Status == "" {
		err := analysisutil.ValidateAnalysisTemplateSpec(run.Spec.AnalysisSpec)
		if err != nil {
			log.Warnf("analysis spec invalid: %v", err)
			run.Status.Status = v1alpha1.AnalysisStatusError
		}
		return nil
	}

	tasks := generateMetricTasks(run)

	runMeasurements(run, tasks)

	asssessRunStatus(run)

	nextReconcileTime := calculateNextReconcileTime(run)
	if nextReconcileTime != nil {
		enqueueSeconds := nextReconcileTime.Sub(time.Now())
		c.enqueueAnalysisAfter(run, enqueueSeconds)
	}
	return nil
}

// generateMetricTasks generates a list of metrics tasks needed to be measured as part of this
// sync, based on the last completion times of measurement was taken (if ever). If any metrics
// are failed, will not perform further measurements on other metrics (fast fail).
func generateMetricTasks(run *v1alpha1.AnalysisRun) []metricTask {
	log := logutil.WithAnalysisRun(run)
	isFailing := analysisutil.IsFailing(run)
	var tasks []metricTask
	for _, metric := range run.Spec.AnalysisSpec.Metrics {
		if analysisutil.MetricCompleted(run, metric.Name) {
			continue
		}
		lastMeasurement := analysisutil.LastMeasurement(run, metric.Name)
		if lastMeasurement != nil && lastMeasurement.FinishedAt.IsZero() {
			// last measurement is still in-progress. need to complete it
			tasks = append(tasks, metricTask{
				metric:                &metric,
				incompleteMeasurement: lastMeasurement,
			})
			continue
		}
		if isFailing {
			log.WithField("metric", metric.Name).Infof("skipping measurement: run is failing")
			continue
		}
		if lastMeasurement == nil {
			// measurement never taken
			tasks = append(tasks, metricTask{metric: &metric})
			continue
		}
		if metric.Interval == nil {
			// a measurement was already taken, and reoccurrence was not desired
			continue
		}
		if time.Now().After(lastMeasurement.FinishedAt.Add(time.Duration(*metric.Interval) * time.Second)) {
			// we are due for a measurement
			tasks = append(tasks, metricTask{metric: &metric})
			continue
		}
	}
	return tasks
}

// runMeasurements iterates a list of metric tasks, and runs or resumes measurements
func runMeasurements(run *v1alpha1.AnalysisRun, tasks []metricTask) {
	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Add(1)
		//var provider provider.MetricProvider
		//provider = provider.NewProvider(task.metric)

		//go func(p provider.Provider, t metricTask) {
		go func(p interface{}, t metricTask) {
			defer wg.Done()
			var newMeasurement v1alpha1.Measurement
			metricResult := run.Status.MetricResults[t.metric.Name]
			if t.incompleteMeasurement == nil {
				// newMeasurement = p.Run(metric)
				metricResult.Measurements = append(metricResult.Measurements, newMeasurement)
			} else {
				// newMeasurement = p.Resume(metric, measurement)
				metricResult.Measurements[len(metricResult.Measurements)-1] = newMeasurement
			}
			if newMeasurement.Status.Completed() {
				metricResult.Count++
				switch newMeasurement.Status {
				case v1alpha1.AnalysisStatusSuccessful:
					metricResult.Successful++
				case v1alpha1.AnalysisStatusFailed:
					metricResult.Failed++
				case v1alpha1.AnalysisStatusError:
					metricResult.Error++
				case v1alpha1.AnalysisStatusInconclusive:
					metricResult.Inconclusive++
				}
			}
		}(nil, task)
		//}(provider, task)
	}
	wg.Wait()
}

// asssessRunStatus assesses the overall status of this analysis run
// If any metric is still not yet completed, the AnalysisRun is still considered running
// Once all metrics are complete, the worst status is used as the overall AnalysisRun status
func asssessRunStatus(run *v1alpha1.AnalysisRun) v1alpha1.AnalysisStatus {
	var worstStatus v1alpha1.AnalysisStatus
	for _, metric := range run.Spec.AnalysisSpec.Metrics {
		if result, ok := run.Status.MetricResults[metric.Name]; ok {
			metricStatus := assessMetricStatus(&metric, &result)
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

// assessMetricStatus assesses the status of a single metric based on current/latest measurements
// and the parameters given by the metric (maxFailures, count, etc...)
func assessMetricStatus(metric *v1alpha1.Metric, result *v1alpha1.MetricResult) v1alpha1.AnalysisStatus {
	if result.Status.Completed() {
		return result.Status
	}
	if result.Count == int32(0) || len(result.Measurements) == 0 {
		return v1alpha1.AnalysisStatusPending
	}
	lastMeasurement := result.Measurements[len(result.Measurements)-1]
	if !lastMeasurement.Status.Completed() {
		return v1alpha1.AnalysisStatusRunning
	}
	if result.Failed > metric.MaxFailures {
		return v1alpha1.AnalysisStatusFailed
	}
	// TODO(jessesuen):
	// We need a way mark metrics as error if the last N number of measurements were measured
	// error. NOTE that we shouldn't base this on result.Errors since that number is cumulative,
	// and failing a metric due to errors should happen when N number of errors happens in sequence.

	// If a count was specified, and we reached that count, then we assess the status based on
	// most recent measurement. Note that any Failures are ignored because failure check is already
	// taken into account above, and we do not want to fail the metric if failures < maxFailures.
	if metric.Count > 0 && result.Count >= metric.Count {
		for i := len(result.Measurements) - 1; i >= 0; i-- {
			measurement := result.Measurements[i]
			switch measurement.Status {
			case v1alpha1.AnalysisStatusSuccessful, v1alpha1.AnalysisStatusInconclusive:
				return measurement.Status
			}
		}
		// if we get here, it means no measurements were Successful or Inconclusive and everything
		// was either Failed, Error.
		for i := len(result.Measurements) - 1; i >= 0; i-- {
			measurement := result.Measurements[i]
			switch measurement.Status {
			case v1alpha1.AnalysisStatusSuccessful, v1alpha1.AnalysisStatusInconclusive:
				return measurement.Status
			}
		}
	}

	return v1alpha1.AnalysisStatusRunning
}

// calculateNextReconcileTime calculates the next time that this AnalysisRun should be reconciled,
// based on the earliest time of all metrics intervals and their finishedAt timestamps
func calculateNextReconcileTime(run *v1alpha1.AnalysisRun) *time.Time {
	log := logutil.WithAnalysisRun(run)
	var reconcileTime *time.Time
	for _, metric := range run.Spec.AnalysisSpec.Metrics {
		if analysisutil.MetricCompleted(run, metric.Name) {
			continue
		}
		lastMeasurement := analysisutil.LastMeasurement(run, metric.Name)
		if lastMeasurement == nil {
			// no measurement was not started. we should not get here
			log.WithField("metric", metric.Name).Warnf("metric never started. not factored into enqueue time")
			continue
		}
		if lastMeasurement.FinishedAt.IsZero() {
			continue
		}
		if metric.Interval == nil {
			// a measurement was already taken, and reoccurrence was not desired. no need to re-enqueue
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
