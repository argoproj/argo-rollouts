package experiments

import (
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/record"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

func newTestContext(ex *v1alpha1.Experiment, objects ...runtime.Object) *experimentContext {
	exobjects := []runtime.Object{}
	kubeobjects := []runtime.Object{}
	for _, obj := range objects {
		switch obj.(type) {
		case *v1alpha1.Experiment:
			exobjects = append(exobjects, obj)
		case *appsv1.ReplicaSet:
			kubeobjects = append(kubeobjects, obj)
		}
	}
	rolloutclient := fake.NewSimpleClientset(exobjects...)
	kubeclient := k8sfake.NewSimpleClientset(kubeobjects...)

	k8sI := kubeinformers.NewSharedInformerFactory(kubeclient, noResyncPeriodFunc())
	rsLister := k8sI.Apps().V1().ReplicaSets().Lister()
	rolloutsI := informers.NewSharedInformerFactory(rolloutclient, noResyncPeriodFunc())
	analysisRunLister := rolloutsI.Argoproj().V1alpha1().AnalysisRuns().Lister()
	analysisTemplateLister := rolloutsI.Argoproj().V1alpha1().AnalysisTemplates().Lister()
	clusterAnalysisTemplateLister := rolloutsI.Argoproj().V1alpha1().ClusterAnalysisTemplates().Lister()
	serviceLister := k8sI.Core().V1().Services().Lister()

	return newExperimentContext(
		ex,
		make(map[string]*appsv1.ReplicaSet),
		make(map[string]*corev1.Service),
		kubeclient,
		rolloutclient,
		rsLister,
		analysisTemplateLister,
		clusterAnalysisTemplateLister,
		analysisRunLister,
		serviceLister,
		record.NewFakeEventRecorder(),
		noResyncPeriodFunc(),
		func(obj interface{}, duration time.Duration) {},
	)
}

func setExperimentService(template *v1alpha1.TemplateSpec) {
	template.Service = &v1alpha1.TemplateService{}
	template.Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
		{
			ContainerPort: 80,
			Protocol:      "TCP",
		},
	}
}

func TestSetExperimentToPending(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, "")
	e.Status = v1alpha1.ExperimentStatus{}
	cond := newCondition(conditions.ReplicaSetUpdatedReason, e)

	f := newFixture(t, e)
	defer f.Close()

	rs := templateToRS(e, templates[0], 0)
	f.expectCreateReplicaSetAction(rs)
	f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))
	patch := f.getPatchedExperiment(0)
	templateStatus := []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 0, 0, v1alpha1.TemplateStatusProgressing, now()),
	}
	expectedPatch := calculatePatch(e, `{
		"status":{
			"phase": "Pending"
		}
	}`, templateStatus, cond)
	assert.Equal(t, expectedPatch, patch)
}

// TestAddScaleDownDelayToRS verifies that we add a scale down delay to the ReplicaSet after experiment completes
func TestAddScaleDownDelayToRS(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, "")
	e.Status.AvailableAt = now()
	e.Status.Phase = v1alpha1.AnalysisPhaseRunning
	cond := conditions.NewExperimentConditions(v1alpha1.ExperimentProgressing, corev1.ConditionTrue, conditions.NewRSAvailableReason, "Experiment \"foo\" is running.")
	e.Status.Conditions = append(e.Status.Conditions, *cond)
	rs := templateToRS(e, templates[0], 1)
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusSuccessful, now()),
	}

	f := newFixture(t, e, rs)
	defer f.Close()

	f.expectPatchExperimentAction(e)
	patchRs1Index := f.expectPatchReplicaSetAction(rs) // Add scaleDownDelaySeconds
	f.expectGetReplicaSetAction(rs)                    // Get RS after patch to modify updated version
	f.run(getKey(e, t))

	f.verifyPatchedReplicaSetAddScaleDownDelay(patchRs1Index, 30)
}

// TestAddScaleDownDelayToRS verifies that we add a scale down delay to the ReplicaSet after experiment completes
func TestRemoveScaleDownDelayFromRS(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, "")
	e.Spec.ScaleDownDelaySeconds = pointer.Int32Ptr(0)
	e.Status.AvailableAt = now()
	e.Status.Phase = v1alpha1.AnalysisPhaseRunning
	cond := conditions.NewExperimentConditions(v1alpha1.ExperimentProgressing, corev1.ConditionTrue, conditions.NewRSAvailableReason, "Experiment \"foo\" is running.")
	e.Status.Conditions = append(e.Status.Conditions, *cond)
	rs := templateToRS(e, templates[0], 1)
	rs.ObjectMeta.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = timeutil.Now().Add(600 * time.Second).UTC().Format(time.RFC3339)
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusSuccessful, now()),
	}

	f := newFixture(t, e, rs)
	defer f.Close()

	f.expectPatchExperimentAction(e)
	patchRs1Index := f.expectPatchReplicaSetAction(rs) // Remove scaleDownDelaySeconds
	f.expectGetReplicaSetAction(rs)                    // Get RS after patch to modify updated version
	f.expectUpdateReplicaSetAction(rs)
	f.run(getKey(e, t))

	f.verifyPatchedReplicaSetRemoveScaleDownDelayAnnotation(patchRs1Index)
}

// TestScaleDownRSAfterFinish verifies that ScaleDownDelaySeconds annotation is added to ReplicaSet that is to be scaled down
func TestScaleDownRSAfterFinish(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	templates[0].Service = &v1alpha1.TemplateService{}

	e := newExperiment("foo", templates, "")
	rs1 := templateToRS(e, templates[0], 1)
	rs2 := templateToRS(e, templates[1], 1)
	s1 := templateToService(e, templates[0], *rs1)

	e.Status.AvailableAt = now()
	e.Status.Phase = v1alpha1.AnalysisPhaseRunning
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusSuccessful, now()),
		generateTemplatesStatus("baz", 1, 1, v1alpha1.TemplateStatusSuccessful, now()),
	}
	e.Status.TemplateStatuses[0].ServiceName = s1.Name
	cond := conditions.NewExperimentConditions(v1alpha1.ExperimentProgressing, corev1.ConditionTrue, conditions.NewRSAvailableReason, "Experiment \"foo\" is running.")
	e.Status.Conditions = append(e.Status.Conditions, *cond)

	inThePast := timeutil.Now().Add(-10 * time.Second).UTC().Format(time.RFC3339)
	rs1.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inThePast
	rs2.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey] = inThePast

	f := newFixture(t, e, rs1, rs2, s1)
	defer f.Close()

	updateRs1Index := f.expectUpdateReplicaSetAction(rs1)
	f.expectDeleteServiceAction(s1)
	updateRs2Index := f.expectUpdateReplicaSetAction(rs2)
	expPatchIndex := f.expectPatchExperimentAction(e)

	f.run(getKey(e, t))
	updatedRs1 := f.getUpdatedReplicaSet(updateRs1Index)
	assert.NotNil(t, updatedRs1)
	assert.Equal(t, int32(0), *updatedRs1.Spec.Replicas)

	updatedRs2 := f.getUpdatedReplicaSet(updateRs2Index)
	assert.NotNil(t, updatedRs2)
	assert.Equal(t, int32(0), *updatedRs2.Spec.Replicas)

	expPatchObj := f.getPatchedExperimentAsObj(expPatchIndex)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, expPatchObj.Status.Phase)
}

func TestSetAvailableAt(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, "")
	e.Status.Phase = v1alpha1.AnalysisPhasePending
	cond := newCondition(conditions.ReplicaSetUpdatedReason, e)
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 0, v1alpha1.TemplateStatusProgressing, now()),
		generateTemplatesStatus("baz", 1, 0, v1alpha1.TemplateStatusProgressing, now()),
	}

	rs1 := templateToRS(e, templates[0], 1)
	rs2 := templateToRS(e, templates[1], 1)
	f := newFixture(t, e, rs1, rs2)
	defer f.Close()

	patchIndex := f.expectPatchExperimentAction(e)

	f.run(getKey(e, t))

	patch := f.getPatchedExperiment(patchIndex)
	templateStatuses := []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusRunning, now()),
		generateTemplatesStatus("baz", 1, 1, v1alpha1.TemplateStatusRunning, now()),
	}
	validatePatch(t, patch, v1alpha1.AnalysisPhaseRunning, Set, templateStatuses, []v1alpha1.ExperimentCondition{*cond})
}

func TestNoPatch(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, "")
	e.Status.Conditions = []v1alpha1.ExperimentCondition{{
		Type:               v1alpha1.ExperimentProgressing,
		Reason:             conditions.NewRSAvailableReason,
		Message:            fmt.Sprintf(conditions.ExperimentRunningMessage, e.Name),
		LastTransitionTime: timeutil.MetaNow(),
		Status:             corev1.ConditionTrue,
		LastUpdateTime:     timeutil.MetaNow(),
	}}

	e.Status.AvailableAt = now()
	e.Status.Phase = v1alpha1.AnalysisPhaseRunning
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusRunning, now()),
		generateTemplatesStatus("baz", 1, 1, v1alpha1.TemplateStatusRunning, now()),
	}

	rs1 := templateToRS(e, templates[0], 1)
	rs2 := templateToRS(e, templates[1], 1)
	f := newFixture(t, e, rs1, rs2)
	defer f.Close()

	f.run(getKey(e, t))
}

func TestSuccessAfterDurationPasses(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, "5s")

	tenSecondsAgo := timeutil.Now().Add(-10 * time.Second)
	e.Status.AvailableAt = &metav1.Time{Time: tenSecondsAgo}
	e.Status.Phase = v1alpha1.AnalysisPhaseRunning
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusRunning, now()),
		generateTemplatesStatus("baz", 1, 1, v1alpha1.TemplateStatusRunning, now()),
	}

	rs1 := templateToRS(e, templates[0], 1)
	rs2 := templateToRS(e, templates[1], 1)
	f := newFixture(t, e, rs1, rs2)
	defer f.Close()

	i := f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))
	patch := f.getPatchedExperiment(i)

	templateStatuses := []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusSuccessful, now()),
		generateTemplatesStatus("baz", 1, 1, v1alpha1.TemplateStatusSuccessful, now()),
	}
	cond := newCondition(conditions.ExperimentCompleteReason, e)
	expectedPatch := calculatePatch(e, `{
		"status":{
			"phase": "Successful"
		}
	}`, templateStatuses, cond)
	assert.JSONEq(t, expectedPatch, patch)
}

// TestDontRequeueWithoutDuration verifies we don't requeue if an experiment does not have
// spec.duration set, and is running properly, since would cause a hot loop.
func TestDontRequeueWithoutDuration(t *testing.T) {
	templates := generateTemplates("bar")
	ex := newExperiment("foo", templates, "")
	ex.Status.AvailableAt = &metav1.Time{Time: timeutil.MetaNow().Add(-10 * time.Second)}
	ex.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusRunning, now()),
	}
	exCtx := newTestContext(ex)
	rs1 := templateToRS(ex, ex.Spec.Templates[0], 1)
	exCtx.templateRSs = map[string]*appsv1.ReplicaSet{
		"bar": rs1,
	}
	fakeClient := exCtx.kubeclientset.(*k8sfake.Clientset)
	fakeClient.Tracker().Add(rs1)
	enqueueCalled := false
	exCtx.enqueueExperimentAfter = func(obj interface{}, duration time.Duration) {
		enqueueCalled = true
	}
	newStatus := exCtx.reconcile()
	assert.False(t, enqueueCalled)
	assert.Equal(t, v1alpha1.AnalysisPhaseRunning, newStatus.Phase)
}

// TestRequeueAfterDuration verifies we requeue after an appropriate status.availableAt + spec.duration
func TestRequeueAfterDuration(t *testing.T) {
	templates := generateTemplates("bar")
	ex := newExperiment("foo", templates, "")
	ex.Spec.Duration = "30s"
	ex.Status.AvailableAt = &metav1.Time{Time: timeutil.MetaNow().Add(-10 * time.Second)}
	ex.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusRunning, now()),
	}
	exCtx := newTestContext(ex)
	rs1 := templateToRS(ex, ex.Spec.Templates[0], 1)
	exCtx.templateRSs = map[string]*appsv1.ReplicaSet{
		"bar": rs1,
	}
	enqueueCalled := false
	exCtx.enqueueExperimentAfter = func(obj interface{}, duration time.Duration) {
		enqueueCalled = true
		// ensures we are enqueued around ~20 seconds
		twentySeconds := time.Second * time.Duration(20)
		delta := math.Abs(float64(twentySeconds - duration))
		assert.True(t, delta < float64(150*time.Millisecond), "")
	}
	exCtx.reconcile()
	assert.True(t, enqueueCalled)
}

// TestRequeueAfterProgressDeadlineSeconds verifies we requeue at an appropriate
// lastTransitionTime + spec.progressDeadlineSeconds
func TestRequeueAfterProgressDeadlineSeconds(t *testing.T) {
	templates := generateTemplates("bar")
	ex := newExperiment("foo", templates, "")
	ex.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 0, 0, v1alpha1.TemplateStatusProgressing, now()),
	}
	now := timeutil.MetaNow()
	ex.Status.TemplateStatuses[0].LastTransitionTime = &now
	exCtx := newTestContext(ex)
	rs1 := templateToRS(ex, ex.Spec.Templates[0], 0)
	exCtx.templateRSs = map[string]*appsv1.ReplicaSet{
		"bar": rs1,
	}
	enqueueCalled := false
	exCtx.enqueueExperimentAfter = func(obj interface{}, duration time.Duration) {
		enqueueCalled = true
		// ensures we are enqueued around 10 minutes
		tenMinutes := time.Second * time.Duration(600)
		delta := math.Abs(float64(tenMinutes - duration))
		assert.True(t, delta < float64(150*time.Millisecond))
	}
	exCtx.reconcile()
	assert.True(t, enqueueCalled)
}

func TestFailReplicaSetCreation(t *testing.T) {
	templates := generateTemplates("good", "bad")
	e := newExperiment("foo", templates, "")

	exCtx := newTestContext(e)

	// Cause failure of the second replicaset
	calls := 0
	fakeClient := exCtx.kubeclientset.(*k8sfake.Clientset)
	fakeClient.PrependReactor("create", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		if calls == 0 {
			calls++
			return true, templateToRS(e, templates[0], 0), nil
		}
		return true, nil, errors.New("intentional error")
	})
	newStatus := exCtx.reconcile()
	assert.Equal(t, newStatus.TemplateStatuses[1].Status, v1alpha1.TemplateStatusError)
	assert.Equal(t, newStatus.Phase, v1alpha1.AnalysisPhaseError)
}

func TestFailServiceCreation(t *testing.T) {
	templates := generateTemplates("bad")
	setExperimentService(&templates[0])
	e := newExperiment("foo", templates, "")

	exCtx := newTestContext(e)
	rs := templateToRS(e, templates[0], 0)
	exCtx.templateRSs[templates[0].Name] = rs

	fakeClient := exCtx.kubeclientset.(*k8sfake.Clientset)
	fakeClient.PrependReactor("create", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("intentional error")
	})
	newStatus := exCtx.reconcile()
	assert.Equal(t, v1alpha1.TemplateStatusError, newStatus.TemplateStatuses[0].Status)
	assert.Contains(t, newStatus.TemplateStatuses[0].Message, "Failed to create Service foo-bad for template 'bad'")
	assert.Equal(t, v1alpha1.AnalysisPhaseError, newStatus.Phase)
}

func TestFailAddScaleDownDelay(t *testing.T) {
	templates := generateTemplates("bar")
	templates[0].Service = &v1alpha1.TemplateService{}
	ex := newExperiment("foo", templates, "")
	ex.Spec.ScaleDownDelaySeconds = pointer.Int32Ptr(0)
	ex.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusFailed, now()),
	}
	rs := templateToRS(ex, templates[0], 1)

	exCtx := newTestContext(ex)
	exCtx.templateRSs["bar"] = rs

	fakeClient := exCtx.kubeclientset.(*k8sfake.Clientset)
	fakeClient.PrependReactor("patch", "*", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("intentional error")
	})
	newStatus := exCtx.reconcile()
	assert.Equal(t, v1alpha1.TemplateStatusError, newStatus.TemplateStatuses[0].Status)
	assert.Contains(t, newStatus.TemplateStatuses[0].Message, "Unable to scale ReplicaSet for template 'bar' to desired replica count '0'")
	assert.Equal(t, newStatus.Phase, v1alpha1.AnalysisPhaseError)
}

func TestFailAddScaleDownDelayIsConflict(t *testing.T) {
	templates := generateTemplates("bar")
	ex := newExperiment("foo", templates, "")
	ex.Spec.ScaleDownDelaySeconds = pointer.Int32Ptr(0)
	ex.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusRunning, now()),
	}
	rs := templateToRS(ex, templates[0], 1)
	rs.Spec.Replicas = pointer.Int32(0)

	exCtx := newTestContext(ex, rs)
	exCtx.templateRSs["bar"] = rs

	fakeClient := exCtx.kubeclientset.(*k8sfake.Clientset)
	updateCalled := false
	fakeClient.PrependReactor("update", "replicasets", func(action kubetesting.Action) (handled bool, ret runtime.Object, err error) {
		updateCalled = true
		return true, nil, k8serrors.NewConflict(schema.GroupResource{}, "guestbook", errors.New("intentional-error"))
	})
	newStatus := exCtx.reconcile()
	assert.True(t, updateCalled)
	assert.Equal(t, v1alpha1.TemplateStatusRunning, newStatus.TemplateStatuses[0].Status)
	assert.Equal(t, "", newStatus.TemplateStatuses[0].Message)
	assert.Equal(t, newStatus.Phase, v1alpha1.AnalysisPhaseRunning)
}

// TestDeleteOutdatedService verifies that outdated service for Template in templateServices map is deleted and new service is created
func TestDeleteOutdatedService(t *testing.T) {
	templates := generateTemplates("bar")
	setExperimentService(&templates[0])
	ex := newExperiment("foo", templates, "")

	wrongService := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "wrong-service"}}
	ex.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusRunning, now()),
	}
	ex.Status.TemplateStatuses[0].ServiceName = wrongService.Name

	rs := templateToRS(ex, templates[0], 0)
	s := templateToService(ex, templates[0], *rs)

	exCtx := newTestContext(ex)

	exCtx.templateRSs = map[string]*appsv1.ReplicaSet{
		"bar": rs,
	}

	exCtx.templateServices = map[string]*corev1.Service{
		"bar": wrongService,
	}

	exStatus := exCtx.reconcile()
	assert.Equal(t, s.Name, exStatus.TemplateStatuses[0].ServiceName)
	assert.Equal(t, s.Name, exCtx.templateServices["bar"].Name)
	assert.NotContains(t, exCtx.templateServices, wrongService.Name)
}

func TestDeleteServiceIfServiceFieldNil(t *testing.T) {
	templates := generateTemplates("bar")
	templates[0].Replicas = pointer.Int32Ptr(0)
	ex := newExperiment("foo", templates, "")
	ex.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusRunning, now()),
	}

	svcToDelete := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "service-to-delete"}}
	ex.Status.TemplateStatuses[0].ServiceName = svcToDelete.Name

	exCtx := newTestContext(ex)

	rs := templateToRS(ex, templates[0], 0)

	exCtx.templateRSs["bar"] = rs

	exCtx.templateServices["bar"] = svcToDelete

	exStatus := exCtx.reconcile()

	assert.Equal(t, "", exStatus.TemplateStatuses[0].ServiceName)
	assert.Nil(t, exCtx.templateServices["bar"])
}

func TestServiceInheritPortsFromRS(t *testing.T) {
	templates := generateTemplates("bar")
	templates[0].Service = &v1alpha1.TemplateService{}
	templates[0].Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
		{
			Name:          "testport",
			ContainerPort: 80,
			Protocol:      "TCP",
		},
	}
	ex := newExperiment("foo", templates, "")

	exCtx := newTestContext(ex)
	rs := templateToRS(ex, templates[0], 0)
	exCtx.templateRSs["bar"] = rs

	exCtx.reconcile()

	assert.NotNil(t, exCtx.templateServices["bar"])
	assert.Equal(t, exCtx.templateServices["bar"].Name, "foo-bar")
	assert.Equal(t, exCtx.templateServices["bar"].Spec.Ports[0].Port, int32(80))
}

func TestServiceNameSet(t *testing.T) {
	templates := generateTemplates("bar")
	templates[0].Service = &v1alpha1.TemplateService{
		Name: "service-name",
	}
	templates[0].Template.Spec.Containers[0].Ports = []corev1.ContainerPort{
		{
			Name:          "testport",
			ContainerPort: 80,
			Protocol:      "TCP",
		},
	}
	ex := newExperiment("foo", templates, "")

	exCtx := newTestContext(ex)
	rs := templateToRS(ex, templates[0], 0)
	exCtx.templateRSs["bar"] = rs

	exCtx.reconcile()

	assert.NotNil(t, exCtx.templateServices["bar"])
	assert.Equal(t, exCtx.templateServices["bar"].Name, "service-name")
}
