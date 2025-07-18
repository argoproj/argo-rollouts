package job

import (
	"context"
	"errors"
	"fmt"
	"sort"

	log "github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	batchlisters "k8s.io/client-go/listers/batch/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	metricutil "github.com/argoproj/argo-rollouts/utils/metric"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

const (
	ProviderType = "Job"
	// JobNameKey is the measurement's metadata key holding the job name associated with the measurement
	JobNameKey = "job-name"
	// JobNamespaceKey is the measurement's metadata key holding the job namespace associated with the measurement
	JobNamespaceKey = "job-namespace"
	// AnalysisRunNameAnnotationKey is the job's annotation key containing the name of the controller AnalysisRun
	AnalysisRunNameAnnotationKey = "analysisrun.argoproj.io/name"
	// AnalysisRunNamespaceAnnotationKey is the job's annotation key containing the namespace of the controller AnalysisRun
	AnalysisRunNamespaceAnnotationKey = "analysisrun.argoproj.io/namespace"
	// AnalysisRunMetricLabelKey is the job's annotation key containing the name of the associated AnalysisRun metric
	AnalysisRunMetricAnnotationKey = "analysisrun.argoproj.io/metric-name"
	// AnalysisRunUIDLabelKey is the job's label key containing the uid of the associated AnalysisRun
	// Also used to filter the job informer
	AnalysisRunUIDLabelKey = "analysisrun.argoproj.io/uid"
)

var (
	analysisRunGVK = v1alpha1.SchemeGroupVersion.WithKind("AnalysisRun")
)

type JobProvider struct {
	kubeclientset       kubernetes.Interface
	jobLister           batchlisters.JobLister
	logCtx              log.Entry
	jobNamespace        string
	customJobKubeconfig bool
}

func NewJobProvider(logCtx log.Entry, kubeclientset kubernetes.Interface, jobLister batchlisters.JobLister, jobNS string, customJobKubeconfig bool) *JobProvider {
	return &JobProvider{
		kubeclientset:       kubeclientset,
		logCtx:              logCtx,
		jobLister:           jobLister,
		jobNamespace:        jobNS,
		customJobKubeconfig: customJobKubeconfig,
	}
}

func (p *JobProvider) Type() string {
	return ProviderType
}

// GetMetadata returns any additional metadata which needs to be stored & displayed as part of the metrics result.
func (p *JobProvider) GetMetadata(metric v1alpha1.Metric) map[string]string {
	return nil
}

// newJobName returns a new job name for the run and metric. Names must be shortened so that it can
// fit into a 63 character label, since the k8s job controller incorporates the job name into the
// pod spec labels.
func newJobName(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) string {

	jobID := getJobIDSuffix(run, metric.Name)
	jobName := fmt.Sprintf("%s.%s.%d", run.UID, metric.Name, jobID)

	// Kubernetes can accept this job name without any issues
	if len(jobName) <= defaults.Kubernetes_DNS_Limit {
		return jobName
	}

	//We are over 63 characters so Kubernetes will reject this job name. We need to truncate it to 63 characters.
	charactersOverLimit := len(jobName) - defaults.Kubernetes_DNS_Limit
	truncateTo := len(metric.Name) - charactersOverLimit
	return fmt.Sprintf("%s.%s.%d", run.UID, metric.Name[:truncateTo], jobID)
}

// getJobIDSuffix returns a numeric id which will be used as part of the job name. This is equal
// to the total number of measurements ever taken + 1.
func getJobIDSuffix(run *v1alpha1.AnalysisRun, metricName string) int {
	res := analysisutil.GetResult(run, metricName)
	if res == nil {
		return 1
	}
	return int(res.Count + res.Error + 1)
}

func newMetricJob(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, jobNS string, customJobKubeconfig bool) (*batchv1.Job, error) {
	ns := run.Namespace
	if jobNS != "" {
		ns = jobNS
	}
	jobAnnotations := metric.Provider.Job.Metadata.GetAnnotations()
	jobLabels := metric.Provider.Job.Metadata.GetLabels()
	if jobAnnotations == nil {
		jobAnnotations = make(map[string]string)
	}
	if jobLabels == nil {
		jobLabels = make(map[string]string)
	}
	jobLabels[AnalysisRunUIDLabelKey] = string(run.UID)
	jobAnnotations[AnalysisRunNameAnnotationKey] = run.Name
	jobAnnotations[AnalysisRunNamespaceAnnotationKey] = run.Namespace
	jobAnnotations[AnalysisRunMetricAnnotationKey] = metric.Name

	ownerRef := []metav1.OwnerReference{*metav1.NewControllerRef(run, analysisRunGVK)}

	if ns != run.Namespace || customJobKubeconfig {
		ownerRef = nil
	}
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:            newJobName(run, metric),
			Namespace:       ns,
			OwnerReferences: ownerRef,
			Annotations:     jobAnnotations,
			Labels:          jobLabels,
		},
		Spec: metric.Provider.Job.Spec,
	}
	return &job, nil
}

func (p *JobProvider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric) v1alpha1.Measurement {
	ctx := context.TODO()
	now := timeutil.MetaNow()
	measurement := v1alpha1.Measurement{
		StartedAt: &now,
		Phase:     v1alpha1.AnalysisPhaseRunning,
	}
	job, err := newMetricJob(run, metric, p.jobNamespace, p.customJobKubeconfig)
	if err != nil {
		p.logCtx.Errorf("job initialization failed: %v", err)
		return metricutil.MarkMeasurementError(measurement, err)
	}
	jobIf := p.kubeclientset.BatchV1().Jobs(job.Namespace)
	createdJob, createErr := jobIf.Create(ctx, job, metav1.CreateOptions{})
	if createErr != nil {
		if !k8serrors.IsAlreadyExists(createErr) {
			p.logCtx.Errorf("job create %s failed: %v", job.Name, createErr)
			return metricutil.MarkMeasurementError(measurement, createErr)
		}
		existingJob, err := jobIf.Get(ctx, job.Name, metav1.GetOptions{})
		if err != nil {
			p.logCtx.Errorf("job create (verify) %s failed: %v", job.Name, createErr)
			return metricutil.MarkMeasurementError(measurement, createErr)
		}
		ownerUID := ""
		// if custom kubeconfig or different namespace is used owner ref is absent,
		// use run uid label to get owner analysis run uid
		if p.customJobKubeconfig || job.Namespace != run.Namespace {
			ownerUID = job.Labels[AnalysisRunUIDLabelKey]
		} else {
			controllerRef := metav1.GetControllerOf(existingJob)
			ownerUID = string(controllerRef.UID)
		}

		if string(run.UID) != ownerUID {
			// NOTE: we don't bother to check for semantic equality. UID is good enough
			p.logCtx.Errorf("job create (uid check) %s failed: %v", job.Name, createErr)
			return metricutil.MarkMeasurementError(measurement, createErr)
		}
		p.logCtx.Infof("duplicate job create detected %s", job.Name)
		createdJob = existingJob
	}
	measurement.Metadata = map[string]string{
		JobNameKey:      createdJob.Name,
		JobNamespaceKey: createdJob.Namespace,
	}
	p.logCtx.Infof("job %s/%s created", createdJob.Namespace, createdJob.Name)
	return measurement
}

func (p *JobProvider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	jobName, err := getJobNamespacedName(measurement, run.Namespace)
	now := timeutil.MetaNow()
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}
	job, err := p.jobLister.Jobs(jobName.Namespace).Get(jobName.Name)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}
	for _, condition := range job.Status.Conditions {
		switch condition.Type {
		case batchv1.JobComplete:
			measurement.FinishedAt = &now
			measurement.Phase = v1alpha1.AnalysisPhaseSuccessful
		case batchv1.JobFailed:
			measurement.FinishedAt = &now
			measurement.Phase = v1alpha1.AnalysisPhaseFailed
		}
	}
	if measurement.Phase.Completed() {
		p.logCtx.Infof("job %s/%s completed: %s", job.Namespace, job.Name, measurement.Phase)
	}
	return measurement
}

func (p *JobProvider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, measurement v1alpha1.Measurement) v1alpha1.Measurement {
	jobName, err := getJobNamespacedName(measurement, run.Namespace)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}
	err = p.deleteJob(jobName.Namespace, jobName.Name)
	if err != nil {
		return metricutil.MarkMeasurementError(measurement, err)
	}
	now := timeutil.MetaNow()
	measurement.FinishedAt = &now
	measurement.Phase = v1alpha1.AnalysisPhaseSuccessful
	p.logCtx.Infof("job %s/%s terminated", jobName.Namespace, jobName.Name)
	return measurement
}

func getJobNamespacedName(measurement v1alpha1.Measurement, defaultNS string) (types.NamespacedName, error) {
	name := types.NamespacedName{
		Namespace: defaultNS,
		Name:      "",
	}
	if measurement.Metadata != nil {
		if measurement.Metadata[JobNameKey] != "" {
			name.Name = measurement.Metadata[JobNameKey]
		} else {
			return name, errors.New("job metadata reference missing")
		}
		if measurement.Metadata[JobNamespaceKey] != "" {
			name.Namespace = measurement.Metadata[JobNamespaceKey]
		}
	} else {
		return name, errors.New("job metadata reference missing")
	}
	return name, nil
}

func (p *JobProvider) deleteJob(namespace, jobName string) error {
	foregroundDelete := metav1.DeletePropagationForeground
	deleteOpts := metav1.DeleteOptions{PropagationPolicy: &foregroundDelete}

	// TODO(jessesuen): retry
	err := p.kubeclientset.BatchV1().Jobs(namespace).Delete(context.TODO(), jobName, deleteOpts)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	return nil
}

// GarbageCollect deletes an old jobs
func (p *JobProvider) GarbageCollect(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, limit int) error {
	set := labels.Set(map[string]string{
		AnalysisRunUIDLabelKey: string(run.UID),
	})
	selector := labels.SelectorFromSet(set)
	jobs, err := p.jobLister.List(selector)
	if err != nil {
		return err
	}
	sort.Slice(jobs[:], func(i, j int) bool {
		return jobs[i].CreationTimestamp.Before(&jobs[j].CreationTimestamp)
	})
	totalJobs := len(jobs)
	if totalJobs > limit {
		for i := 0; i < totalJobs-limit; i++ {
			err = p.deleteJob(jobs[i].Namespace, jobs[i].Name)
			if err != nil {
				return err
			}
			p.logCtx.Infof("job %s/%s garbage collected", jobs[i].Namespace, jobs[i].Name)
		}
	}
	return nil
}
