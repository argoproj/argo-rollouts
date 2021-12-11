package experiments

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
)

func TestUpdateProgressingLastUpdateTime(t *testing.T) {

	templates := generateTemplates("bar")
	templates[0].Replicas = pointer.Int32Ptr(2)
	e := newExperiment("foo", templates, "")
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{{
		Name: "bar",
	}}
	prevCond := newCondition(conditions.ReplicaSetUpdatedReason, e)
	prevTime := metav1.NewTime(timeutil.Now().Add(-10 * time.Second))
	prevCond.LastUpdateTime = prevTime
	prevCond.LastTransitionTime = prevTime
	e.Status.Conditions = []v1alpha1.ExperimentCondition{
		*prevCond,
	}

	rs := templateToRS(e, templates[0], 1)

	f := newFixture(t, e, rs)
	defer f.Close()

	patchIndex := f.expectPatchExperimentAction(e)

	f.run(getKey(e, t))

	patch := f.getPatchedExperiment(patchIndex)
	cond := []v1alpha1.ExperimentCondition{*newCondition(conditions.ReplicaSetUpdatedReason, e)}
	cond[0].LastTransitionTime = prevTime.Rfc3339Copy()
	templateStatuses := []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1, v1alpha1.TemplateStatusProgressing, now()),
	}
	validatePatch(t, patch, "", NoChange, templateStatuses, cond)
}

func TestEnterTimeoutDegradedState(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, "")
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{{
		Name:   "bar",
		Status: v1alpha1.TemplateStatusProgressing,
	}}
	e.Spec.ProgressDeadlineSeconds = pointer.Int32Ptr(30)
	prevTime := metav1.NewTime(timeutil.Now().Add(-1 * time.Minute).Truncate(time.Second))
	e.Status.TemplateStatuses[0].LastTransitionTime = &prevTime

	rs := templateToRS(e, templates[0], 0)
	f := newFixture(t, e, rs)
	defer f.Close()

	patchIndex := f.expectPatchExperimentAction(e)

	f.run(getKey(e, t))

	patch := f.getPatchedExperiment(patchIndex)

	ts := generateTemplatesStatus("bar", 0, 0, v1alpha1.TemplateStatusFailed, now())
	ts.LastTransitionTime = &prevTime
	ts.Message = "Template 'bar' exceeded its progressDeadlineSeconds (30)"
	templateStatuses := []v1alpha1.TemplateStatus{
		ts,
	}
	validatePatch(t, patch, v1alpha1.AnalysisPhaseFailed, NoChange, templateStatuses, nil)
}
