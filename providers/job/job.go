package job

import (
	"errors"

	log "github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	batchlisters "k8s.io/client-go/listers/batch/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	ProviderType = "job"
	// JobNameKey is the measurement's metadata key holding the job name associated with the measurement
	JobNameKey = "job-name"
	// AnalysisRunLabelKey is the job's label key where we label the name of the AnalysisRun associated to it
	AnalysisRunLabelKey = "analysisruns.argoproj.io/name"
)

var (
	analysisRunGVK = v1alpha1.SchemeGroupVersion.WithKind("AnalysisRun")
)

type JobProvider struct {
	kubeclientset kubernetes.Interface
	jobLister     batchlisters.JobLister
	logCtx        log.Entry
}

func NewJobProvider(logCtx log.Entry, kubeclientset kubernetes.Interface, jobLister batchlisters.JobLister) *JobProvider {
	return &JobProvider{
		kubeclientset: kubeclientset,
		logCtx:        logCtx,
		jobLister:     jobLister,
	}
}

func (p *JobProvider) Type() string {
	return ProviderType
}

func (p *JobProvider) Run(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, args []v1alpha1.Argument) (v1alpha1.Measurement, error) {
	now := metav1.Now()
	measurement := v1alpha1.Measurement{
		StartedAt: &now,
		Status:    v1alpha1.AnalysisStatusRunning,
	}
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName:    run.Name + "-" + metric.Name + "-",
			Namespace:       run.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(run, analysisRunGVK)},
			Labels: map[string]string{
				AnalysisRunLabelKey: run.Name,
			},
		},
		Spec: metric.Provider.Job.Spec,
	}
	createdJob, err := p.kubeclientset.BatchV1().Jobs(run.Namespace).Create(&job)
	if err != nil {
		p.logCtx.Errorf("job create (generateName: %s) failed: %v", job.ObjectMeta.GenerateName, err)
		measurement.FinishedAt = &now
		measurement.Status = v1alpha1.AnalysisStatusError
		return measurement, err
	}
	measurement.Metadata = map[string]string{
		JobNameKey: createdJob.Name,
	}
	p.logCtx.Infof("job %s/%s created", createdJob.Namespace, createdJob.Name)
	return measurement, nil
}

func (p *JobProvider) Resume(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, args []v1alpha1.Argument, measurement v1alpha1.Measurement) (v1alpha1.Measurement, error) {
	jobName, err := getJobName(measurement)
	now := metav1.Now()
	if err != nil {
		measurement.FinishedAt = &now
		measurement.Status = v1alpha1.AnalysisStatusError
		return measurement, err
	}
	job, err := p.jobLister.Jobs(run.Namespace).Get(jobName)
	if err != nil {
		measurement.FinishedAt = &now
		measurement.Status = v1alpha1.AnalysisStatusError
		return measurement, err
	}
	for _, condition := range job.Status.Conditions {
		switch condition.Type {
		case batchv1.JobComplete:
			measurement.FinishedAt = &now
			measurement.Status = v1alpha1.AnalysisStatusSuccessful
		case batchv1.JobFailed:
			measurement.FinishedAt = &now
			measurement.Status = v1alpha1.AnalysisStatusFailed
		}
	}
	if measurement.Status.Completed() {
		p.logCtx.Infof("job %s/%s completed: %s", job.Namespace, job.Name, measurement.Status)
	}
	return measurement, nil
}

func (p *JobProvider) Terminate(run *v1alpha1.AnalysisRun, metric v1alpha1.Metric, args []v1alpha1.Argument, measurement v1alpha1.Measurement) (v1alpha1.Measurement, error) {
	jobName, err := getJobName(measurement)
	now := metav1.Now()
	if err != nil {
		measurement.FinishedAt = &now
		measurement.Status = v1alpha1.AnalysisStatusError
		return measurement, err
	}
	// TODO(jessesuen): retry
	foregroundDelete := metav1.DeletePropagationForeground
	deleteOpts := metav1.DeleteOptions{PropagationPolicy: &foregroundDelete}
	err = p.kubeclientset.BatchV1().Jobs(run.Namespace).Delete(jobName, &deleteOpts)
	if err != nil && !k8serrors.IsNotFound(err) {
		measurement.FinishedAt = &now
		measurement.Status = v1alpha1.AnalysisStatusError
		return measurement, err
	}
	p.logCtx.Infof("job %s/%s terminated", run.Namespace, jobName)
	measurement.FinishedAt = &now
	measurement.Status = v1alpha1.AnalysisStatusSuccessful
	return measurement, nil
}

func getJobName(measurement v1alpha1.Measurement) (string, error) {
	if measurement.Metadata != nil && measurement.Metadata[JobNameKey] != "" {
		return measurement.Metadata[JobNameKey], nil
	}
	return "", errors.New("job metadata reference missing")
}
