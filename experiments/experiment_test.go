package experiments

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"

	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"github.com/argoproj/argo-rollouts/utils/conditions"
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

	return newExperimentContext(
		ex,
		make(map[string]*appsv1.ReplicaSet),
		kubeclient,
		rolloutclient,
		rsLister,
		analysisTemplateLister,
		analysisRunLister,
		&record.FakeRecorder{},
		func(obj interface{}, duration time.Duration) {},
	)
}
func TestSetExperimentToPending(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, nil)
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
			"status": "Pending"
		}
	}`, templateStatus, cond)
	assert.Equal(t, expectedPatch, patch)
}

func TestScaleDownRSAfterFinish(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, nil)
	e.Status.AvailableAt = now()
	e.Status.Status = v1alpha1.AnalysisStatusRunning
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusSuccessful, now()),
		generateTemplatesStatus("baz", 1, 1, v1alpha1.TemplateStatusSuccessful, now()),
	}
	cond := conditions.NewExperimentConditions(v1alpha1.ExperimentProgressing, corev1.ConditionTrue, conditions.NewRSAvailableReason, "Experiment \"foo\" is running.")
	e.Status.Conditions = append(e.Status.Conditions, *cond)
	rs1 := templateToRS(e, templates[0], 1)
	rs2 := templateToRS(e, templates[1], 1)

	f := newFixture(t, e, rs1, rs2)
	defer f.Close()

	updateRs1Index := f.expectUpdateReplicaSetAction(rs1)
	updateRs2Index := f.expectUpdateReplicaSetAction(rs2)

	f.run(getKey(e, t))
	updatedRs1 := f.getUpdatedReplicaSet(updateRs1Index)
	assert.NotNil(t, updatedRs1)
	assert.Equal(t, int32(0), *updatedRs1.Spec.Replicas)

	updatedRs2 := f.getUpdatedReplicaSet(updateRs2Index)
	assert.NotNil(t, updatedRs2)
	assert.Equal(t, int32(0), *updatedRs2.Spec.Replicas)
}

func TestSetAvailableAt(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, nil)
	e.Status.Status = v1alpha1.AnalysisStatusPending
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
	validatePatch(t, patch, v1alpha1.AnalysisStatusRunning, Set, templateStatuses, []v1alpha1.ExperimentCondition{*cond})
}

func TestNoPatch(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, nil)
	e.Status.Conditions = []v1alpha1.ExperimentCondition{{
		Type:               v1alpha1.ExperimentProgressing,
		Reason:             conditions.NewRSAvailableReason,
		Message:            fmt.Sprintf(conditions.ExperimentRunningMessage, e.Name),
		LastTransitionTime: metav1.Now(),
		Status:             corev1.ConditionTrue,
		LastUpdateTime:     metav1.Now(),
	}}

	e.Status.AvailableAt = now()
	e.Status.Status = v1alpha1.AnalysisStatusRunning
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
	e := newExperiment("foo", templates, pointer.Int32Ptr(5))

	tenSecondsAgo := metav1.Now().Add(-10 * time.Second)
	e.Status.AvailableAt = &metav1.Time{Time: tenSecondsAgo}
	e.Status.Status = v1alpha1.AnalysisStatusRunning
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

	expectedPatch := calculatePatch(e, `{
		"status":{
			"status": "Successful"
		}
	}`, templateStatuses, nil)
	assert.Equal(t, expectedPatch, patch)
}

// TestDontRequeueWithoutDuration verifies we don't enter a hot loop because we keep requeuing
func TestDontRequeueWithoutDuration(t *testing.T) {
	templates := generateTemplates("bar")
	ex := newExperiment("foo", templates, nil)
	ex.Status.AvailableAt = &metav1.Time{Time: metav1.Now().Add(-10 * time.Second)}
	exCtx := newTestContext(ex)
	enqueueCalled := false
	exCtx.enqueueExperimentAfter = func(obj interface{}, duration time.Duration) {
		enqueueCalled = true
	}
	exCtx.reconcile()
	assert.False(t, enqueueCalled)
}

func TestFailReplicaSetCreation(t *testing.T) {
	templates := generateTemplates("good", "bad")
	e := newExperiment("foo", templates, nil)

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
	assert.Equal(t, newStatus.Status, v1alpha1.AnalysisStatusError)
}
