package job

import (
	"context"
	"fmt"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

var noResyncPeriodFunc = func() time.Duration { return 0 }

func newTestJobProvider(objects ...runtime.Object) *JobProvider {
	logCtx := log.NewEntry(log.New())
	kubeclient := k8sfake.NewSimpleClientset(objects...)
	k8sI := kubeinformers.NewSharedInformerFactory(kubeclient, noResyncPeriodFunc())
	jobInformer := k8sI.Batch().V1().Jobs().Informer()

	ctx, cancel := context.WithCancel(context.Background())
	go jobInformer.Run(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), jobInformer.HasSynced)
	cancel()

	jobLister := k8sI.Batch().V1().Jobs().Lister()
	return NewJobProvider(*logCtx, kubeclient, jobLister)
}

func newRunWithJobMetric() *v1alpha1.AnalysisRun {
	run := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dummyrun",
			Namespace: "dummynamespace",
		},
		Spec: v1alpha1.AnalysisRunSpec{
			AnalysisSpec: v1alpha1.AnalysisTemplateSpec{
				Metrics: []v1alpha1.Metric{
					{
						Name: "dummymetric",
						Provider: v1alpha1.MetricProvider{
							Job: &v1alpha1.JobMetric{
								Spec: batchv1.JobSpec{},
							},
						},
					},
				},
			},
		},
	}
	return &run
}

func newJob(jobType batchv1.JobConditionType) *batchv1.Job {
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dummyrun-metric-abc123",
			Namespace: "dummynamespace",
		},
		Status: batchv1.JobStatus{},
	}
	if jobType != "" {
		job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
			Type: jobType,
		})
	}
	return &job
}

func newRunningMeasurement(jobName string) v1alpha1.Measurement {
	now := metav1.Now()
	measurement := v1alpha1.Measurement{
		StartedAt: &now,
		Status:    v1alpha1.AnalysisStatusRunning,
		Metadata: map[string]string{
			JobNameKey: jobName,
		},
	}
	return measurement
}

func TestType(t *testing.T) {
	p := newTestJobProvider()
	assert.Equal(t, ProviderType, p.Type())
}

func TestRun(t *testing.T) {
	p := newTestJobProvider()
	run := newRunWithJobMetric()
	metric := run.Spec.AnalysisSpec.Metrics[0]
	measurement := p.Run(run, metric, nil)

	assert.Equal(t, v1alpha1.AnalysisStatusRunning, measurement.Status)
	assert.NotNil(t, measurement.StartedAt)
	assert.Nil(t, measurement.FinishedAt)

	// NOTE: fake clientsets don't generate names from generateName, so we don't check for the
	// expected job name here, just the fact that we set something in measurement metadata.
	_, ok := measurement.Metadata[JobNameKey]
	assert.True(t, ok)

	// Ensure the job was created with the right generateName in the right namespace with
	// right ownership reference and right label
	jobs, err := p.kubeclientset.BatchV1().Jobs(run.Namespace).List(metav1.ListOptions{})
	assert.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%s-%s-", run.Name, metric.Name), jobs.Items[0].GenerateName)
	assert.Equal(t, run.Name, jobs.Items[0].ObjectMeta.Labels[AnalysisRunLabelKey])
	expectedOwnerRef := []metav1.OwnerReference{*metav1.NewControllerRef(run, analysisRunGVK)}
	assert.Equal(t, expectedOwnerRef, jobs.Items[0].ObjectMeta.OwnerReferences)
}

func TestRunCreateFail(t *testing.T) {
	p := newTestJobProvider()
	run := newRunWithJobMetric()
	errMsg := "random create failure"

	// The following causes the Create call to fail
	fakeClient := p.kubeclientset.(*k8sfake.Clientset)
	fakeClient.PrependReactor("create", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, fmt.Errorf(errMsg)
	})

	measurement := p.Run(run, run.Spec.AnalysisSpec.Metrics[0], nil)
	assert.Equal(t, v1alpha1.AnalysisStatusError, measurement.Status)
	assert.Contains(t, errMsg, measurement.Message)
	assert.NotNil(t, measurement.FinishedAt)
}

func TestResumeCompletedJob(t *testing.T) {
	job := newJob(batchv1.JobComplete)
	p := newTestJobProvider(job)
	run := newRunWithJobMetric()
	measurement := newRunningMeasurement(job.Name)
	measurement = p.Resume(run, run.Spec.AnalysisSpec.Metrics[0], nil, measurement)
	assert.Equal(t, v1alpha1.AnalysisStatusSuccessful, measurement.Status)
	assert.NotNil(t, measurement.FinishedAt)
}

func TestResumeFailedJob(t *testing.T) {
	job := newJob(batchv1.JobFailed)
	p := newTestJobProvider(job)
	run := newRunWithJobMetric()
	measurement := newRunningMeasurement(job.Name)
	measurement = p.Resume(run, run.Spec.AnalysisSpec.Metrics[0], nil, measurement)
	assert.Equal(t, v1alpha1.AnalysisStatusFailed, measurement.Status)
	assert.NotNil(t, measurement.FinishedAt)
}

func TestResumeErrorJob(t *testing.T) {
	p := newTestJobProvider()
	run := newRunWithJobMetric()
	measurement := newRunningMeasurement("job-which-does-not-exist")
	measurement = p.Resume(run, run.Spec.AnalysisSpec.Metrics[0], nil, measurement)
	assert.Equal(t, "job.batch \"job-which-does-not-exist\" not found", measurement.Message)
	assert.Equal(t, v1alpha1.AnalysisStatusError, measurement.Status)
	assert.NotNil(t, measurement.FinishedAt)
}

func TestResumeMeasurementNoMetadata(t *testing.T) {
	p := newTestJobProvider()
	run := newRunWithJobMetric()
	measurement := newRunningMeasurement("")
	measurement.Metadata = nil
	measurement = p.Resume(run, run.Spec.AnalysisSpec.Metrics[0], nil, measurement)
	assert.Equal(t, "job metadata reference missing", measurement.Message)
	assert.Equal(t, v1alpha1.AnalysisStatusError, measurement.Status)
	assert.NotNil(t, measurement.FinishedAt)
}

func TestTerminateMeasurement(t *testing.T) {
	job := newJob("")

	providerWithJob := newTestJobProvider(job)
	providerWithoutJob := newTestJobProvider()

	for _, p := range []*JobProvider{providerWithJob, providerWithoutJob} {
		run := newRunWithJobMetric()
		measurement := newRunningMeasurement(job.Name)
		measurement = p.Terminate(run, run.Spec.AnalysisSpec.Metrics[0], nil, measurement)
		assert.Equal(t, v1alpha1.AnalysisStatusSuccessful, measurement.Status)
		assert.NotNil(t, measurement.FinishedAt)
	}
}

func TestTerminateError(t *testing.T) {
	p := newTestJobProvider()
	run := newRunWithJobMetric()
	measurement := newRunningMeasurement("doesnt-matter")

	// cause delete to fail
	errMsg := "random delete error"
	fakeClient := p.kubeclientset.(*k8sfake.Clientset)
	fakeClient.PrependReactor("delete", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, fmt.Errorf(errMsg)
	})

	measurement = p.Terminate(run, run.Spec.AnalysisSpec.Metrics[0], nil, measurement)
	assert.Equal(t, v1alpha1.AnalysisStatusError, measurement.Status)
	assert.Contains(t, measurement.Message, errMsg)
	assert.NotNil(t, measurement.FinishedAt)
}

func TestTerminateMeasurementNoMetadata(t *testing.T) {
	run := newRunWithJobMetric()
	p := newTestJobProvider()
	measurement := newRunningMeasurement("")
	measurement.Metadata = nil
	measurement = p.Terminate(run, run.Spec.AnalysisSpec.Metrics[0], nil, measurement)
	assert.Equal(t, "job metadata reference missing", measurement.Message)
	assert.Equal(t, v1alpha1.AnalysisStatusError, measurement.Status)
	assert.NotNil(t, measurement.FinishedAt)
}
