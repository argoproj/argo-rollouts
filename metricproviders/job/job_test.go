package job

import (
	"context"
	"fmt"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
			UID:       types.UID("dummyuid"),
			Name:      "dummyrun",
			Namespace: "dummynamespace",
		},
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: []v1alpha1.Metric{
				{
					Name: "dummymetric",
					Provider: v1alpha1.MetricProvider{
						Job: &v1alpha1.JobMetric{
							Spec: batchv1.JobSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{
											{
												Name: "dummy",
												Command: []string{
													"dosomething",
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return &run
}

func newJob(run *v1alpha1.AnalysisRun, jobType batchv1.JobConditionType) *batchv1.Job {
	job := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dummyrun-metric-abc123",
			Namespace: "dummynamespace",
			Annotations: map[string]string{
				AnalysisRunNameAnnotationKey:   run.Name,
				AnalysisRunMetricAnnotationKey: run.Spec.Metrics[0].Name,
			},
			Labels: map[string]string{
				AnalysisRunUIDLabelKey: string(run.UID),
			},
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
		Phase:     v1alpha1.AnalysisPhaseRunning,
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
	ctx := context.Background()
	p := newTestJobProvider()
	run := newRunWithJobMetric()
	metric := run.Spec.Metrics[0]
	metricsMetadata := p.GetMetadata(metric)
	assert.Nil(t, metricsMetadata)
	providerJobMetadataLabels := map[string]string{
		"foo-label": "bar",
	}
	providerJobMetadataAnnotations := map[string]string{
		"foo-annotation": "bar",
	}
	metric.Provider.Job.Metadata = metav1.ObjectMeta{
		Labels:      providerJobMetadataLabels,
		Annotations: providerJobMetadataAnnotations,
	}

	measurement := p.Run(run, metric)

	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, measurement.Phase)
	assert.NotNil(t, measurement.StartedAt)
	assert.Nil(t, measurement.FinishedAt)

	expectedName := fmt.Sprintf("%s.%s.1", run.UID, metric.Name)
	assert.Equal(t, expectedName, measurement.Metadata[JobNameKey])

	// Ensure the job was created with the right name in the right namespace with
	// right ownership reference and right label
	jobs, err := p.kubeclientset.BatchV1().Jobs(run.Namespace).List(ctx, metav1.ListOptions{})
	assert.NoError(t, err)
	assert.Equal(t, expectedName, jobs.Items[0].Name)
	assert.Equal(t, string(run.UID), jobs.Items[0].ObjectMeta.Labels[AnalysisRunUIDLabelKey])
	for labelKey, labelVal := range providerJobMetadataLabels {
		assert.Equal(t, labelVal, jobs.Items[0].ObjectMeta.Labels[labelKey])
	}
	for annotationKey, annotationVal := range providerJobMetadataAnnotations {
		assert.Equal(t, annotationVal, jobs.Items[0].ObjectMeta.Annotations[annotationKey])
	}

	expectedOwnerRef := []metav1.OwnerReference{*metav1.NewControllerRef(run, analysisRunGVK)}
	assert.Equal(t, expectedOwnerRef, jobs.Items[0].ObjectMeta.OwnerReferences)

	// do it again, this time it should bump up the run ID
	run.Status.MetricResults = []v1alpha1.MetricResult{
		{
			Name:  metric.Name,
			Count: 1,
		},
	}
	measurement = p.Run(run, metric)
	expectedName = fmt.Sprintf("%s.%s.2", run.UID, metric.Name)
	assert.Equal(t, expectedName, measurement.Metadata[JobNameKey])
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
	metricsMetadata := p.GetMetadata(run.Spec.Metrics[0])
	assert.Nil(t, metricsMetadata)

	measurement := p.Run(run, run.Spec.Metrics[0])
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Contains(t, errMsg, measurement.Message)
	assert.NotNil(t, measurement.FinishedAt)
}

func TestRunCreateCollision(t *testing.T) {
	p := newTestJobProvider()
	run := newRunWithJobMetric()

	existingJob, err := newMetricJob(run, run.Spec.Metrics[0])
	assert.NoError(t, err)
	fakeClient := p.kubeclientset.(*k8sfake.Clientset)
	fakeClient.Tracker().Add(existingJob)

	measurement := p.Run(run, run.Spec.Metrics[0])
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, measurement.Phase)
	assert.Nil(t, measurement.FinishedAt)
}

func TestResumeCompletedJob(t *testing.T) {
	run := newRunWithJobMetric()
	job := newJob(run, batchv1.JobComplete)
	p := newTestJobProvider(job)
	measurement := newRunningMeasurement(job.Name)
	measurement = p.Resume(run, run.Spec.Metrics[0], measurement)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
	assert.NotNil(t, measurement.FinishedAt)
}

func TestResumeFailedJob(t *testing.T) {
	run := newRunWithJobMetric()
	job := newJob(run, batchv1.JobFailed)
	p := newTestJobProvider(job)
	measurement := newRunningMeasurement(job.Name)
	measurement = p.Resume(run, run.Spec.Metrics[0], measurement)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, measurement.Phase)
	assert.NotNil(t, measurement.FinishedAt)
}

func TestResumeErrorJob(t *testing.T) {
	p := newTestJobProvider()
	run := newRunWithJobMetric()
	measurement := newRunningMeasurement("job-which-does-not-exist")
	measurement = p.Resume(run, run.Spec.Metrics[0], measurement)
	assert.Equal(t, "job.batch \"job-which-does-not-exist\" not found", measurement.Message)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.NotNil(t, measurement.FinishedAt)
}

func TestResumeMeasurementNoMetadata(t *testing.T) {
	p := newTestJobProvider()
	run := newRunWithJobMetric()
	measurement := newRunningMeasurement("")
	measurement.Metadata = nil
	measurement = p.Resume(run, run.Spec.Metrics[0], measurement)
	assert.Equal(t, "job metadata reference missing", measurement.Message)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.NotNil(t, measurement.FinishedAt)
}

func TestTerminateMeasurement(t *testing.T) {
	run := newRunWithJobMetric()
	job := newJob(run, "")

	providerWithJob := newTestJobProvider(job)
	providerWithoutJob := newTestJobProvider()

	for _, p := range []*JobProvider{providerWithJob, providerWithoutJob} {

		measurement := newRunningMeasurement(job.Name)
		measurement = p.Terminate(run, run.Spec.Metrics[0], measurement)
		assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, measurement.Phase)
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

	measurement = p.Terminate(run, run.Spec.Metrics[0], measurement)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.Contains(t, measurement.Message, errMsg)
	assert.NotNil(t, measurement.FinishedAt)
}

func TestTerminateMeasurementNoMetadata(t *testing.T) {
	run := newRunWithJobMetric()
	p := newTestJobProvider()
	measurement := newRunningMeasurement("")
	measurement.Metadata = nil
	measurement = p.Terminate(run, run.Spec.Metrics[0], measurement)
	assert.Equal(t, "job metadata reference missing", measurement.Message)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, measurement.Phase)
	assert.NotNil(t, measurement.FinishedAt)
}

func TestGarbageCollect(t *testing.T) {
	ctx := context.Background()
	run := newRunWithJobMetric()
	run.Status.MetricResults = []v1alpha1.MetricResult{
		{
			Name: run.Spec.Metrics[0].Name,
		},
	}
	now := time.Now()
	var objs []runtime.Object
	for i := 0; i < 12; i++ {
		job := newJob(run, batchv1.JobComplete)
		job.Name = fmt.Sprintf("%s-%d", job.Name, i)
		job.CreationTimestamp = metav1.NewTime(now.Add(time.Second * time.Duration(i)))
		objs = append(objs, job)
	}
	p := newTestJobProvider(objs...)
	err := p.GarbageCollect(run, run.Spec.Metrics[0], 10)
	assert.NoError(t, err)
	allJobs, err := p.kubeclientset.BatchV1().Jobs(run.Namespace).List(ctx, metav1.ListOptions{})
	assert.NoError(t, err)
	assert.Len(t, allJobs.Items, 10)
	basename := newJob(run, "").Name

	for i := 0; i < 12; i++ {
		_, err := p.kubeclientset.BatchV1().Jobs(run.Namespace).Get(ctx, fmt.Sprintf("%s-%d", basename, i), metav1.GetOptions{})
		if i < 2 {
			// ensure we deleted the oldest
			assert.True(t, k8serrors.IsNotFound(err))
		} else {
			assert.NoError(t, err)
		}
	}
}
