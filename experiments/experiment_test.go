package experiments

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

func TestSetExperimentToRunning(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, nil, nil)
	cond := newCondition(conditions.ReplicaSetUpdatedReason, e)

	f.experimentLister = append(f.experimentLister, e)
	f.objects = append(f.objects, e)

	f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))
	patch := f.getPatchedExperiment(0)
	templateStatus := []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 0, 0),
	}
	expectedPatch := calculatePatch(e, `{
		"status":{
			"running": true
		}
	}`, templateStatus, cond)
	assert.Equal(t, expectedPatch, patch)
}

func TestScaleDownRSAfterFinish(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, nil, pointer.BoolPtr(true))
	e.Status.Running = pointer.BoolPtr(false)
	cond := newCondition(conditions.ExperimentCompleteReason, e)

	f.experimentLister = append(f.experimentLister, e)
	f.objects = append(f.objects, e)
	rs1 := templateToRS(e, templates[0], 0)
	rs2 := templateToRS(e, templates[1], 0)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)

	updateRs1Index := f.expectUpdateReplicaSetAction(rs1)
	updateRs2Index := f.expectUpdateReplicaSetAction(rs2)
	patchIndex := f.expectPatchExperimentAction(e)

	f.run(getKey(e, t))
	updatedRs1 := f.getUpdatedReplicaSet(updateRs1Index)
	assert.NotNil(t, updatedRs1)
	assert.Equal(t, int32(0), *updatedRs1.Spec.Replicas)

	updatedRs2 := f.getUpdatedReplicaSet(updateRs2Index)
	assert.NotNil(t, updatedRs2)
	assert.Equal(t, int32(0), *updatedRs2.Spec.Replicas)

	patch := f.getPatchedExperiment(patchIndex)
	expectedPatch := `{"status":{}}`
	templateStatuses := []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 0, 0),
		generateTemplatesStatus("baz", 0, 0),
	}
	assert.Equal(t, calculatePatch(e, expectedPatch, templateStatuses, cond), patch)
}

func TestSetAvailableAt(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, nil, pointer.BoolPtr(true))
	e.Status.Running = pointer.BoolPtr(true)
	cond := newCondition(conditions.ReplicaSetUpdatedReason, e)
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 0),
		generateTemplatesStatus("baz", 1, 0),
	}

	f.experimentLister = append(f.experimentLister, e)
	f.objects = append(f.objects, e)
	rs1 := templateToRS(e, templates[0], 1)
	rs2 := templateToRS(e, templates[1], 1)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)

	patchIndex := f.expectPatchExperimentAction(e)

	f.run(getKey(e, t))

	patch := f.getPatchedExperiment(patchIndex)
	templateStatuses := []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1),
		generateTemplatesStatus("baz", 1, 1),
	}
	validatePatch(t, patch, nil, Set, templateStatuses, []v1alpha1.ExperimentCondition{*cond})
}

func TestNoPatch(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, nil, pointer.BoolPtr(true))
	e.Status.Conditions = []v1alpha1.ExperimentCondition{{
		Type:               v1alpha1.ExperimentProgressing,
		Reason:             conditions.NewRSAvailableReason,
		Message:            fmt.Sprintf(conditions.ExperimentRunningMessage, e.Name),
		LastTransitionTime: metav1.Now(),
		Status:             corev1.ConditionTrue,
		LastUpdateTime:     metav1.Now(),
	}}

	now := metav1.Now()
	e.Status.AvailableAt = &now
	e.Status.Running = pointer.BoolPtr(true)
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1),
		generateTemplatesStatus("baz", 1, 1),
	}

	f.experimentLister = append(f.experimentLister, e)
	f.objects = append(f.objects, e)
	rs1 := templateToRS(e, templates[0], 1)
	rs2 := templateToRS(e, templates[1], 1)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)

	f.run(getKey(e, t))
}

func TestDisableRunningAfterDurationPasses(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, pointer.Int32Ptr(5), pointer.BoolPtr(true))

	now := metav1.Now().Add(-10 * time.Second)
	e.Status.AvailableAt = &metav1.Time{Time: now}
	e.Status.Running = pointer.BoolPtr(true)
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1),
		generateTemplatesStatus("baz", 1, 1),
	}

	f.experimentLister = append(f.experimentLister, e)
	f.objects = append(f.objects, e)
	rs1 := templateToRS(e, templates[0], 1)
	rs2 := templateToRS(e, templates[1], 1)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)

	i := f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))
	patch := f.getPatchedExperiment(i)
	expectedPatch := calculatePatch(e, `{
		"status":{
			"running": false
		}
	}`, nil, nil)
	assert.Equal(t, expectedPatch, patch)
}
