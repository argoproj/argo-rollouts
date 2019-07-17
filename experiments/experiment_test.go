package experiments

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestSetExperimentToRunning(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, 0, nil)

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
	}`, templateStatus)
	assert.Equal(t, expectedPatch, patch)
}

func TestScaleDownRSAfterFinish(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, 0, pointer.BoolPtr(true))
	e.Status.Running = pointer.BoolPtr(false)

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
	assert.Equal(t, calculatePatch(e, expectedPatch, templateStatuses), patch)
}

func TestFailureToCreateRS(t *testing.T) {
}

func TestSetAvailableAt(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, 0, pointer.BoolPtr(true))
	e.Status.Running = pointer.BoolPtr(true)

	f.experimentLister = append(f.experimentLister, e)
	f.objects = append(f.objects, e)
	rs1 := templateToRS(e, templates[0], 1)
	rs2 := templateToRS(e, templates[1], 1)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)

	patchIndex := f.expectPatchExperimentAction(e)

	f.run(getKey(e, t))

	patch := f.getPatchedExperiment(patchIndex)
	expectedPatch := fmt.Sprintf(`{
		"status":{
			"availableAt": "%s"
		}
	}`, metav1.Now().UTC().Format(time.RFC3339))
	templateStatuses := []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 1, 1),
		generateTemplatesStatus("baz", 1, 1),
	}
	assert.Equal(t, calculatePatch(e, expectedPatch, templateStatuses), patch)
}

func TestNoPatch(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, 0, pointer.BoolPtr(true))

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