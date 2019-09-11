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
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, nil, pointer.BoolPtr(true))

	f.experimentLister = append(f.experimentLister, e)
	f.objects = append(f.objects, e)

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
		generateTemplatesStatus("bar", 0, 0),
		generateTemplatesStatus("baz", 0, 0),
	}
	cond := newCondition(conditions.ReplicaSetUpdatedReason, e)

	expectedPatch := calculatePatch(e, `{
		"status":{
		}
	}`, templateStatus, cond)
	assert.Equal(t, expectedPatch, patch)
}

func TestCreateMissingRS(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, nil, pointer.BoolPtr(true))
	e.Status.TemplateStatuses = []v1alpha1.TemplateStatus{{
		Name: "bar",
	}}

	f.experimentLister = append(f.experimentLister, e)
	f.objects = append(f.objects, e)
	rs := templateToRS(e, templates[0], 0)
	f.replicaSetLister = append(f.replicaSetLister, rs)
	f.kubeobjects = append(f.kubeobjects, rs)

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
		generateTemplatesStatus("bar", 0, 0),
		generateTemplatesStatus("baz", 0, 0),
	}
	assert.Equal(t, calculatePatch(e, expectedPatch, templateStatuses, cond), patch)
}

func TestFailCreateRSWithInvalidSelector(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar")
	templates[0].Selector.MatchLabels = map[string]string{}
	templates[0].Selector.MatchExpressions = []metav1.LabelSelectorRequirement{{}}
	e := newExperiment("foo", templates, nil, pointer.BoolPtr(true))

	f.experimentLister = append(f.experimentLister, e)
	f.objects = append(f.objects, e)

	f.runExpectError(getKey(e, t), true)
}

func TestTemplateHasMultipleRS(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, nil, pointer.BoolPtr(true))

	f.experimentLister = append(f.experimentLister, e)
	f.objects = append(f.objects, e)

	rs := templateToRS(e, templates[0], 0)
	rs2 := rs.DeepCopy()
	rs2.Name = "rs2"
	f.replicaSetLister = append(f.replicaSetLister, rs, rs2)
	f.kubeobjects = append(f.kubeobjects, rs, rs2)

	f.runExpectError(getKey(e, t), true)
}

func TestAdoptRS(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, nil, pointer.BoolPtr(true))
	e.Status.Running = pointer.BoolPtr(true)
	f.experimentLister = append(f.experimentLister, e)
	f.objects = append(f.objects, e)

	rs := templateToRS(e, templates[0], 0)
	rs.ObjectMeta.OwnerReferences = []metav1.OwnerReference{}
	f.replicaSetLister = append(f.replicaSetLister, rs)
	f.kubeobjects = append(f.kubeobjects, rs)

	f.expectGetExperimentAction(e)
	f.expectPatchReplicaSetAction(rs)
	patchIndex := f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))

	patch := f.getPatchedExperiment(patchIndex)
	templateStatus := []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 0, 0),
	}

	cond := newCondition(conditions.ReplicaSetUpdatedReason, e)

	expectedPatch := calculatePatch(e, `{
		"status":{
		}
	}`, templateStatus, cond)
	assert.Equal(t, expectedPatch, patch)
}

func TestNameCollision(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, nil, pointer.BoolPtr(true))
	e.Status.Running = pointer.BoolPtr(true)
	f.experimentLister = append(f.experimentLister, e)
	f.objects = append(f.objects, e)

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "deploy",
		},
	}
	rs := templateToRS(e, templates[0], 0)
	rs.ObjectMeta.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(deploy, controllerKind)}
	f.replicaSetLister = append(f.replicaSetLister, rs)
	f.kubeobjects = append(f.kubeobjects, rs)

	f.expectCreateReplicaSetAction(rs)
	patchIndex := f.expectPatchExperimentAction(e)
	f.runExpectError(getKey(e, t), true)

	patch := f.getPatchedExperiment(patchIndex)
	templateStatuses := []v1alpha1.TemplateStatus{
		generateTemplatesStatus("bar", 0, 0),
	}
	templateStatuses[0].CollisionCount = pointer.Int32Ptr(1)
	validatePatch(t, patch, nil, NoChange, templateStatuses, nil)
}
