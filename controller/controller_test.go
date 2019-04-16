package controller

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/uuid"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/equality"
)

var (
	alwaysReady        = func() bool { return true }
	noResyncPeriodFunc = func() time.Duration { return 0 }
)

const (
	OnlyObservedGenerationPatch = `{
			"status" : {
				"observedGeneration": ""
			}
	}`
)

type fixture struct {
	t *testing.T

	client     *fake.Clientset
	kubeclient *k8sfake.Clientset
	// Objects to put in the store.
	rolloutLister    []*v1alpha1.Rollout
	replicaSetLister []*appsv1.ReplicaSet
	serviceLister    []*corev1.Service
	// Actions expected to happen on the client.
	kubeactions []core.Action
	actions     []core.Action
	// Objects from here preloaded into NewSimpleFake.
	kubeobjects     []runtime.Object
	objects         []runtime.Object
	enqueuedObjects map[string]int
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{}
	f.t = t
	f.objects = []runtime.Object{}
	f.kubeobjects = []runtime.Object{}
	f.enqueuedObjects = make(map[string]int)
	return f
}

func newRollout(name string, replicas int, revisionHistoryLimit *int32, selector map[string]string) *v1alpha1.Rollout {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			UID:       uuid.NewUUID(),
			Name:      name,
			Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{
				annotations.RevisionAnnotation: "1",
			},
		},
		Spec: v1alpha1.RolloutSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: selector,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: "foo/bar",
						},
					},
				},
			},
			RevisionHistoryLimit: revisionHistoryLimit,
			Replicas:             func() *int32 { i := int32(replicas); return &i }(),
			Selector:             &metav1.LabelSelector{MatchLabels: selector},
		},
		Status: v1alpha1.RolloutStatus{},
	}
	return ro
}

func newReplicaSetWithStatus(r *v1alpha1.Rollout, name string, replicas int, availableReplicas int) *appsv1.ReplicaSet {
	rs := newReplicaSet(r, name, replicas)
	rs.Status.Replicas = int32(replicas)
	rs.Status.AvailableReplicas = int32(availableReplicas)
	return rs
}

func updateBlueGreenRolloutStatus(r *v1alpha1.Rollout, preview, active string, availableReplicas, updatedReplicas, hpaReplicas int32, pause bool, available bool) *v1alpha1.Rollout {
	newRollout := updateBaseRolloutStatus(r, availableReplicas, updatedReplicas, hpaReplicas, pause)
	selector := newRollout.Spec.Selector.DeepCopy()
	if active != "" {
		selector.MatchLabels[v1alpha1.DefaultRolloutUniqueLabelKey] = active
	}
	newRollout.Status.Selector = metav1.FormatLabelSelector(selector)
	newRollout.Status.BlueGreen.ActiveSelector = active
	newRollout.Status.BlueGreen.PreviewSelector = preview
	cond, _ := newAvailableCondition(available)
	newRollout.Status.Conditions = cond
	return newRollout
}
func updateCanaryRolloutStatus(r *v1alpha1.Rollout, stableRS string, availableReplicas, updatedReplicas, hpaReplicas int32, pause bool) *v1alpha1.Rollout {
	newRollout := updateBaseRolloutStatus(r, availableReplicas, updatedReplicas, hpaReplicas, pause)
	newRollout.Status.Canary.StableRS = stableRS
	return newRollout
}

func updateBaseRolloutStatus(r *v1alpha1.Rollout, availableReplicas, updatedReplicas, hpaReplicas int32, pause bool) *v1alpha1.Rollout {
	newRollout := r.DeepCopy()
	newRollout.Status.Replicas = availableReplicas
	newRollout.Status.AvailableReplicas = availableReplicas
	newRollout.Status.UpdatedReplicas = updatedReplicas
	newRollout.Status.HPAReplicas = hpaReplicas
	if pause {
		newRollout.Spec.Paused = pause
		now := metav1.Now()
		newRollout.Status.PauseStartTime = &now
	}
	return newRollout
}

func newReplicaSet(r *v1alpha1.Rollout, name string, replicas int) *appsv1.ReplicaSet {
	newRSTemplate := *r.Spec.Template.DeepCopy()
	rsLabels := map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: controller.ComputeHash(&newRSTemplate, r.Status.CollisionCount),
	}
	for k, v := range r.Spec.Selector.MatchLabels {
		rsLabels[k] = v
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			UID:             uuid.NewUUID(),
			Namespace:       metav1.NamespaceDefault,
			Labels:          rsLabels,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(r, controllerKind)},
			Annotations: map[string]string{
				annotations.DesiredReplicasAnnotation: strconv.Itoa(int(*r.Spec.Replicas)),
				annotations.RevisionAnnotation:        r.Annotations[annotations.RevisionAnnotation],
			},
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: metav1.SetAsLabelSelector(rsLabels),
			Replicas: func() *int32 { i := int32(replicas); return &i }(),
			Template: r.Spec.Template,
		},
	}
	rs.Spec.Template.ObjectMeta.Labels = rsLabels
	return rs
}

func calculatePatch(ro *v1alpha1.Rollout, patch string) string {
	origBytes, err := json.Marshal(ro)
	if err != nil {
		panic(err)
	}
	newBytes, err := strategicpatch.StrategicMergePatch(origBytes, []byte(patch), v1alpha1.Rollout{})
	if err != nil {
		panic(err)
	}
	newRO := &v1alpha1.Rollout{}
	json.Unmarshal(newBytes, newRO)
	newObservedGen := conditions.ComputeGenerationHash(newRO.Spec)

	newPatch := make(map[string]interface{})
	json.Unmarshal([]byte(patch), &newPatch)
	newStatus := newPatch["status"].(map[string]interface{})
	newStatus["observedGeneration"] = newObservedGen
	newPatch["status"] = newStatus
	newPatchBytes, _ := json.Marshal(newPatch)
	return string(newPatchBytes)
}

func getKey(rollout *v1alpha1.Rollout, t *testing.T) string {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(rollout)
	if err != nil {
		t.Errorf("Unexpected error getting key for rollout %v: %v", rollout.Name, err)
		return ""
	}
	return key
}

type resyncFunc func() time.Duration

func (f *fixture) newController(resync resyncFunc) (*Controller, informers.SharedInformerFactory, kubeinformers.SharedInformerFactory) {
	f.client = fake.NewSimpleClientset(f.objects...)
	f.kubeclient = k8sfake.NewSimpleClientset(f.kubeobjects...)

	i := informers.NewSharedInformerFactory(f.client, resync())
	k8sI := kubeinformers.NewSharedInformerFactory(f.kubeclient, resync())

	c := NewController(f.kubeclient, f.client,
		k8sI.Apps().V1().ReplicaSets(),
		i.Argoproj().V1alpha1().Rollouts(),
		resync())

	c.rolloutsSynced = alwaysReady
	c.replicaSetSynced = alwaysReady
	c.recorder = &record.FakeRecorder{}
	c.enqueueRollout = func(obj interface{}) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
			panic(err)
		}
		count, ok := f.enqueuedObjects[key]
		if !ok {
			count = 0
		}
		count++
		f.enqueuedObjects[key] = count
		c.enqueue(obj)
	}
	c.enqueueRolloutAfter = func(obj interface{}, duration time.Duration) {
		c.enqueueRollout(obj)
	}

	for _, r := range f.rolloutLister {
		i.Argoproj().V1alpha1().Rollouts().Informer().GetIndexer().Add(r)
	}

	for _, r := range f.replicaSetLister {
		k8sI.Apps().V1().ReplicaSets().Informer().GetIndexer().Add(r)
	}
	for _, s := range f.serviceLister {
		k8sI.Core().V1().Services().Informer().GetIndexer().Add(s)
	}

	return c, i, k8sI
}

func (f *fixture) run(rolloutName string) {
	c, i, k8sI := f.newController(noResyncPeriodFunc)
	f.runController(rolloutName, true, false, c, i, k8sI)
}

func (f *fixture) runExpectError(rolloutName string, startInformers bool) {
	c, i, k8sI := f.newController(noResyncPeriodFunc)
	f.runController(rolloutName, startInformers, true, c, i, k8sI)
}

func (f *fixture) runController(rolloutName string, startInformers bool, expectError bool, c *Controller, i informers.SharedInformerFactory, k8sI kubeinformers.SharedInformerFactory) *Controller {
	if startInformers {
		stopCh := make(chan struct{})
		defer close(stopCh)
		i.Start(stopCh)
		k8sI.Start(stopCh)
	}

	err := c.syncHandler(rolloutName)
	if !expectError && err != nil {
		f.t.Errorf("error syncing rollout: %v", err)
	} else if expectError && err == nil {
		f.t.Error("expected error syncing rollout, got nil")
	}

	actions := filterInformerActions(f.client.Actions())
	for i, action := range actions {
		if len(f.actions) < i+1 {
			f.t.Errorf("%d unexpected actions: %+v", len(actions)-len(f.actions), actions[i:])
			break
		}

		expectedAction := f.actions[i]
		checkAction(expectedAction, action, f.t)
	}

	if len(f.actions) > len(actions) {
		f.t.Errorf("%d additional expected actions:%+v", len(f.actions)-len(actions), f.actions[len(actions):])
	}

	k8sActions := filterInformerActions(f.kubeclient.Actions())
	for i, action := range k8sActions {
		if len(f.kubeactions) < i+1 {
			f.t.Errorf("%d unexpected actions: %+v", len(k8sActions)-len(f.kubeactions), k8sActions[i:])
			break
		}

		expectedAction := f.kubeactions[i]
		checkAction(expectedAction, action, f.t)
	}

	if len(f.kubeactions) > len(k8sActions) {
		f.t.Errorf("%d additional expected actions:%+v", len(f.kubeactions)-len(k8sActions), f.kubeactions[len(k8sActions):])
	}
	return c
}

// checkAction verifies that expected and actual actions are equal
func checkAction(expected, actual core.Action, t *testing.T) {
	if !(expected.Matches(actual.GetVerb(), actual.GetResource().Resource) && actual.GetSubresource() == expected.GetSubresource()) {
		t.Errorf("Expected\n\t%#v\ngot\n\t%#v", expected, actual)
		if patch, ok := actual.(core.PatchAction); ok {
			patchBytes := patch.GetPatch()
			t.Errorf("Patch Received: %s", string(patchBytes))
		}
		if patch, ok := expected.(core.PatchAction); ok {
			patchBytes := patch.GetPatch()
			t.Errorf("Expected Patch: %s", string(patchBytes))
		}
		return
	}

	if reflect.TypeOf(actual) != reflect.TypeOf(expected) {
		t.Errorf("Action has wrong type. Expected: %t. Got: %t", expected, actual)
		return
	}
}

// filterInformerActions filters list, and watch actions for testing resources.
// Since list, and watch don't change resource state we can filter it to lower
// nose level in our tests.
func filterInformerActions(actions []core.Action) []core.Action {
	ret := []core.Action{}
	for _, action := range actions {
		if action.Matches("list", "rollouts") ||
			action.Matches("watch", "rollouts") ||
			action.Matches("list", "replicaSets") ||
			action.Matches("watch", "replicaSets") ||
			action.Matches("list", "services") ||
			action.Matches("watch", "services") {
			continue
		}
		ret = append(ret, action)
	}

	return ret
}

func (f *fixture) expectGetServiceAction(s *corev1.Service) {
	serviceSchema := schema.GroupVersionResource{
		Resource: "services",
		Version:  "v1",
	}
	f.kubeactions = append(f.kubeactions, core.NewGetAction(serviceSchema, s.Namespace, s.Name))
}

func (f *fixture) expectPatchServiceAction(s *corev1.Service, newLabel string) int {
	patch := fmt.Sprintf(switchSelectorPatch, v1alpha1.DefaultRolloutUniqueLabelKey, newLabel)
	serviceSchema := schema.GroupVersionResource{
		Resource: "services",
		Version:  "v1",
	}
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, core.NewPatchAction(serviceSchema, s.Namespace, s.Name, []byte(patch)))
	return len
}

func (f *fixture) expectCreateReplicaSetAction(r *appsv1.ReplicaSet) int {
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, core.NewCreateAction(schema.GroupVersionResource{Resource: "replicasets"}, r.Namespace, r))
	return len
}

func (f *fixture) expectUpdateReplicaSetAction(r *appsv1.ReplicaSet) int {
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, core.NewUpdateAction(schema.GroupVersionResource{Resource: "replicasets"}, r.Namespace, r))
	return len
}

func (f *fixture) expectGetRolloutAction(rollout *v1alpha1.Rollout) int {
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, core.NewGetAction(schema.GroupVersionResource{Resource: "rollouts"}, rollout.Namespace, rollout.Name))
	return len
}

func (f *fixture) expectUpdateRolloutAction(rollout *v1alpha1.Rollout) int {
	action := core.NewUpdateAction(schema.GroupVersionResource{Resource: "rollouts"}, rollout.Namespace, rollout)
	len := len(f.actions)
	f.actions = append(f.actions, action)
	return len
}

func (f *fixture) expectPatchRolloutAction(rollout *v1alpha1.Rollout) int {
	serviceSchema := schema.GroupVersionResource{
		Resource: "rollouts",
		Version:  "v1alpha1",
	}
	len := len(f.actions)
	f.actions = append(f.actions, core.NewPatchAction(serviceSchema, rollout.Namespace, rollout.Name, nil))
	return len
}

func (f *fixture) expectPatchRolloutActionWithPatch(rollout *v1alpha1.Rollout, patch string) int {
	expectedPatch := calculatePatch(rollout, patch)
	serviceSchema := schema.GroupVersionResource{
		Resource: "rollouts",
		Version:  "v1alpha1",
	}
	len := len(f.actions)
	f.actions = append(f.actions, core.NewPatchAction(serviceSchema, rollout.Namespace, rollout.Name, []byte(expectedPatch)))
	return len
}

func (f *fixture) getCreatedReplicaSet(index int) *appsv1.ReplicaSet {
	action := filterInformerActions(f.kubeclient.Actions())[index]
	createAction, ok := action.(core.CreateAction)
	if !ok {
		assert.Fail(f.t, "Expected Created action, not %s", action.GetVerb())
	}
	obj := createAction.GetObject()
	rs := &appsv1.ReplicaSet{}
	converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ := converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, rs)
	return rs
}

func (f *fixture) getUpdatedReplicaSet(index int) *appsv1.ReplicaSet {
	action := filterInformerActions(f.kubeclient.Actions())[index]
	updateAction, ok := action.(core.UpdateAction)
	if !ok {
		assert.Fail(f.t, "Expected Update action, not %s", action.GetVerb())
	}
	obj := updateAction.GetObject()
	rs := &appsv1.ReplicaSet{}
	converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ := converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, rs)
	return rs
}

func (f *fixture) verifyPatchedService(index int, newPodHash string) bool {
	action := filterInformerActions(f.kubeclient.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		assert.Fail(f.t, "Expected Patch action, not %s", action.GetVerb())
	}
	patch := fmt.Sprintf(switchSelectorPatch, v1alpha1.DefaultRolloutUniqueLabelKey, newPodHash)
	return string(patchAction.GetPatch()) == patch
}

func (f *fixture) getUpdatedRollout(index int) *v1alpha1.Rollout {
	action := filterInformerActions(f.client.Actions())[index]
	updateAction, ok := action.(core.UpdateAction)
	if !ok {
		assert.Fail(f.t, "Expected Update action, not %s", action.GetVerb())
	}
	obj := updateAction.GetObject()
	rollout := &v1alpha1.Rollout{}
	converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ := converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, rollout)
	return rollout
}

func (f *fixture) getPatchedRollout(index int) string {
	action := filterInformerActions(f.client.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		assert.Fail(f.t, "Expected Patch action, not %s", action.GetVerb())
	}
	return string(patchAction.GetPatch())
}

func TestDontSyncRolloutsWithEmptyPodSelector(t *testing.T) {
	f := newFixture(t)

	r := newBlueGreenRollout("foo", 1, nil, "", "")
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
}
