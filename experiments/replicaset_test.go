package experiments

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

func TestCreateMultipleRS(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, "")

	f := newFixture(t, e)
	defer f.Close()

	createFirstRSIndex := f.expectCreateReplicaSetAction(templateToRS(e, templates[0], 0))
	createSecondRSIndex := f.expectCreateReplicaSetAction(templateToRS(e, templates[1], 0))
	patchIndex := f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))
	patch := f.getPatchedExperiment(patchIndex)
	firstRS := f.getCreatedReplicaSet(createFirstRSIndex)
	assert.NotNil(t, firstRS)
	assert.Equal(t, generateRSName(e, templates[0]), firstRS.Name)

	secondRS := f.getCreatedReplicaSet(createSecondRSIndex)
	assert.NotNil(t, secondRS)
	assert.Equal(t, generateRSName(e, templates[1]), secondRS.Name)

	templateStatus := []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 0, 0, v1alpha1.TemplateStatusProgressing, now()),
		generateTemplatesStatus("baz", 0, 0, v1alpha1.TemplateStatusProgressing, now()),
	}
	cond := newCondition(conditions.ReplicaSetUpdatedReason, e)

	expectedPatch := calculatePatch(e, `{
		"status":{
		}
	}`, templateStatus, cond)
	assert.JSONEq(t, expectedPatch, patch)
}

func TestCreateMissingRS(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, "")
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{{
		Name:               "bar",
		LastTransitionTime: now(),
	}}

	rs := templateToRS(e, templates[0], 0)
	f := newFixture(t, e, rs)
	defer f.Close()

	createRsIndex := f.expectCreateReplicaSetAction(templateToRS(e, templates[1], 0))
	patchIndex := f.expectPatchExperimentAction(e)

	f.run(getKey(e, t))
	secondRS := f.getCreatedReplicaSet(createRsIndex)
	assert.NotNil(t, secondRS)
	assert.Equal(t, generateRSName(e, templates[1]), secondRS.Name)

	patch := f.getPatchedExperiment(patchIndex)
	expectedPatch := `{"status":{}}`
	cond := newCondition(conditions.ReplicaSetUpdatedReason, e)
	templateStatuses := []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 0, 0, v1alpha1.TemplateStatusProgressing, now()),
		generateTemplatesStatus("baz", 0, 0, v1alpha1.TemplateStatusProgressing, now()),
	}
	assert.JSONEq(t, calculatePatch(e, expectedPatch, templateStatuses, cond), patch)
}

func TestTemplateHasMultipleRS(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, "")

	rs := templateToRS(e, templates[0], 0)
	rs2 := rs.DeepCopy()
	rs2.Name = "rs2"

	f := newFixture(t, e, rs, rs2)
	defer f.Close()

	f.runExpectError(getKey(e, t), true)
}

func TestNameCollision(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, "")
	e.Status.Phase = v1alpha1.AnalysisPhasePending

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "deploy",
		},
	}
	rs := templateToRS(e, templates[0], 0)
	rs.ObjectMeta.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(deploy, controllerKind)}

	f := newFixture(t, e, rs)
	defer f.Close()

	f.expectCreateReplicaSetAction(rs)
	collisionCountPatchIndex := f.expectPatchExperimentAction(e) // update collision count
	statusUpdatePatchIndex := f.expectPatchExperimentAction(e)   // updates status
	f.run(getKey(e, t))

	{
		patch := f.getPatchedExperiment(collisionCountPatchIndex)
		templateStatuses := []v1alpha1.TemplateStatus{
			generateTemplatesStatus("bar", 0, 0, "", nil),
		}
		templateStatuses[0].CollisionCount = pointer.Int32Ptr(1)
		validatePatch(t, patch, "", NoChange, templateStatuses, nil)
	}
	{
		patch := f.getPatchedExperiment(statusUpdatePatchIndex)
		templateStatuses := []v1alpha1.TemplateStatus{
			generateTemplatesStatus("bar", 0, 0, v1alpha1.TemplateStatusProgressing, nil),
		}
		cond := []v1alpha1.ExperimentCondition{*newCondition(conditions.ReplicaSetUpdatedReason, e)}
		validatePatch(t, patch, "", NoChange, templateStatuses, cond)
	}
}

// TestNameCollisionWithEquivalentPodTemplateAndControllerUID verifies we consider the annotations
//
//	of the replicaset when encountering name collisions
func TestNameCollisionWithEquivalentPodTemplateAndControllerUID(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, "")
	e.Status.Phase = v1alpha1.AnalysisPhasePending

	rs := templateToRS(e, templates[0], 0)
	rs.ObjectMeta.Annotations[v1alpha1.ExperimentTemplateNameAnnotationKey] = "something-different" // change this to something different

	f := newFixture(t, e, rs)
	defer f.Close()

	f.expectCreateReplicaSetAction(rs)
	collisionCountPatchIndex := f.expectPatchExperimentAction(e) // update collision count
	statusUpdatePatchIndex := f.expectPatchExperimentAction(e)   // updates status
	f.run(getKey(e, t))

	{
		patch := f.getPatchedExperiment(collisionCountPatchIndex)
		templateStatuses := []v1alpha1.TemplateStatus{
			generateTemplatesStatus("bar", 0, 0, "", nil),
		}
		templateStatuses[0].CollisionCount = pointer.Int32Ptr(1)
		validatePatch(t, patch, "", NoChange, templateStatuses, nil)
	}
	{
		patch := f.getPatchedExperiment(statusUpdatePatchIndex)
		templateStatuses := []v1alpha1.TemplateStatus{
			generateTemplatesStatus("bar", 0, 0, v1alpha1.TemplateStatusProgressing, nil),
		}
		cond := []v1alpha1.ExperimentCondition{*newCondition(conditions.ReplicaSetUpdatedReason, e)}
		validatePatch(t, patch, "", NoChange, templateStatuses, cond)
	}
}
