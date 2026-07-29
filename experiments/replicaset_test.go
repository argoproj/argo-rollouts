package experiments

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	appslisters "k8s.io/client-go/listers/apps/v1"
	k8stesting "k8s.io/client-go/testing"
	corev1defaults "k8s.io/kubernetes/pkg/apis/core/v1"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
)

// errReplicaSetLister is a fake ReplicaSetLister that always returns a fixed error from Get.
type errReplicaSetLister struct{ err error }

func (l *errReplicaSetLister) List(_ labels.Selector) ([]*appsv1.ReplicaSet, error) {
	return nil, l.err
}
func (l *errReplicaSetLister) ReplicaSets(_ string) appslisters.ReplicaSetNamespaceLister {
	return &errReplicaSetNamespaceLister{err: l.err}
}
func (l *errReplicaSetLister) GetPodReplicaSets(_ *corev1.Pod) ([]*appsv1.ReplicaSet, error) {
	return nil, l.err
}

type errReplicaSetNamespaceLister struct{ err error }

func (l *errReplicaSetNamespaceLister) List(_ labels.Selector) ([]*appsv1.ReplicaSet, error) {
	return nil, l.err
}
func (l *errReplicaSetNamespaceLister) Get(_ string) (*appsv1.ReplicaSet, error) {
	return nil, l.err
}

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
	}`, templateStatus, cond, nil, "")
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
	assert.JSONEq(t, calculatePatch(e, expectedPatch, templateStatuses, cond, nil, ""), patch)
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
		templateStatuses[0].CollisionCount = ptr.To[int32](1)
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
		templateStatuses[0].CollisionCount = ptr.To[int32](1)
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

// TestReplicaSetAlreadyExistsMissingFromCacheFallsBackToAPI verifies that when a ReplicaSet already
// exists in the API but not in the informer cache (stale lister), the AlreadyExists handler falls
// back to a direct API Get instead of propagating a "not found" error.
func TestReplicaSetAlreadyExistsMissingFromCacheFallsBackToAPI(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, "")

	// Build the RS exactly as createReplicaSet would, then apply kubernetes defaults to
	// the pod template so that the "live" side has the same defaults that
	// PodTemplateEqualIgnoreHash applies to the "desired" side during comparison.
	rs := newReplicaSetFromTemplate(e, templates[0], nil)
	pt := corev1.PodTemplate{Template: rs.Spec.Template}
	corev1defaults.SetObjectDefaults_PodTemplate(&pt)
	rs.Spec.Template = pt.Template

	// Create a context with an empty lister (informer never started/synced). Pre-load the
	// RS directly into the kubeclientset to simulate it existing in the API but not yet
	// visible in the informer cache — the stale-lister race.
	exCtx := newTestContext(e)
	_, err := exCtx.kubeclientset.AppsV1().ReplicaSets(e.Namespace).Create(context.TODO(), &rs, metav1.CreateOptions{})
	assert.NoError(t, err)

	// createReplicaSet: Create → AlreadyExists → lister.Get → NotFound → kubeclientset.Get → found → success
	createdRS, createErr := exCtx.createReplicaSet(templates[0], nil)
	assert.NoError(t, createErr)
	assert.NotNil(t, createdRS)
	assert.Equal(t, rs.Name, createdRS.Name)
}

// TestReplicaSetAlreadyExistsListerNonNotFoundError verifies that when the lister returns a
// non-NotFound error (e.g. indexer failure), it is propagated directly without an API fallback.
func TestReplicaSetAlreadyExistsListerNonNotFoundError(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, "")
	rs := newReplicaSetFromTemplate(e, templates[0], nil)

	exCtx := newTestContext(e)
	// Pre-create RS so Create returns AlreadyExists.
	_, err := exCtx.kubeclientset.AppsV1().ReplicaSets(e.Namespace).Create(context.TODO(), &rs, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Replace lister with one that returns a non-NotFound error.
	listerErr := fmt.Errorf("indexer failure")
	exCtx.replicaSetLister = &errReplicaSetLister{err: listerErr}

	createdRS, createErr := exCtx.createReplicaSet(templates[0], nil)
	assert.ErrorIs(t, createErr, listerErr)
	assert.Nil(t, createdRS)
}

// TestReplicaSetAlreadyExistsAPIGetFailure verifies that when the lister cache is stale and the
// direct API Get also fails, the error is propagated to the caller.
func TestReplicaSetAlreadyExistsAPIGetFailure(t *testing.T) {
	templates := generateTemplates("bar")
	e := newExperiment("foo", templates, "")

	rs := newReplicaSetFromTemplate(e, templates[0], nil)

	exCtx := newTestContext(e)
	// Pre-create RS so Create returns AlreadyExists (lister remains empty/stale).
	_, err := exCtx.kubeclientset.AppsV1().ReplicaSets(e.Namespace).Create(context.TODO(), &rs, metav1.CreateOptions{})
	assert.NoError(t, err)

	// Inject an error for all subsequent Get operations on replicasets.
	exCtx.kubeclientset.(*k8sfake.Clientset).PrependReactor("get", "replicasets", func(_ k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, fmt.Errorf("API server unavailable")
	})

	// createReplicaSet: Create → AlreadyExists → lister.Get → miss → kubeclientset.Get → error
	createdRS, createErr := exCtx.createReplicaSet(templates[0], nil)
	assert.Error(t, createErr)
	assert.Nil(t, createdRS)
	assert.Contains(t, createErr.Error(), "API server unavailable")
}

// TestNewReplicaSetFromTemplate tests the creation of a new ReplicaSet from a given template.
// It verifies that the ReplicaSet is correctly initialized with the expected name, namespace,
// annotations, labels, and container specifications based on the provided experiment and template.
// The test ensures that:
// - The ReplicaSet name is a combination of the experiment name and template name.
// - The ReplicaSet namespace matches the experiment namespace.
// - The ReplicaSet annotations include the experiment name and template name.
// - The ReplicaSet labels include the default rollout unique label key and a specific key from the template.
// - The ReplicaSet selector and template labels include the default rollout unique label key.
// - The ReplicaSet container specifications match those defined in the template.
func TestNewReplicaSetFromTemplate(t *testing.T) {

	templates := generateTemplates("bar")
	template := templates[0]
	experiment := newExperiment("foo", templates, "")
	collisionCount := int32(0)
	rs := newReplicaSetFromTemplate(experiment, template, &collisionCount)

	assert.Equal(t, fmt.Sprintf("%s-%s", experiment.Name, template.Name), rs.Name)
	assert.Equal(t, experiment.Namespace, rs.Namespace)
	assert.Equal(t, experiment.Name, rs.Annotations[v1alpha1.ExperimentNameAnnotationKey])
	assert.NotNil(t, rs.ObjectMeta.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
	assert.NotNil(t, rs.ObjectMeta.Labels["key"])
	assert.Equal(t, template.Template.ObjectMeta.Labels["key"], rs.ObjectMeta.Labels["key"])
	assert.Equal(t, template.Name, rs.Annotations[v1alpha1.ExperimentTemplateNameAnnotationKey])
	assert.NotNil(t, rs.Spec.Selector.MatchLabels[v1alpha1.DefaultRolloutUniqueLabelKey])
	assert.NotNil(t, rs.Spec.Template.ObjectMeta.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
	assert.Equal(t, template.Template.Labels["key"], rs.Spec.Template.Labels["key"])
	assert.Equal(t, template.Template.Spec.Containers[0].Name, rs.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, template.Template.Spec.Containers[0].Image, rs.Spec.Template.Spec.Containers[0].Image)
}
