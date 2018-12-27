package controller

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
)

var (
	alwaysReady        = func() bool { return true }
	noResyncPeriodFunc = func() time.Duration { return 0 }
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
	kubeobjects []runtime.Object
	objects     []runtime.Object
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{}
	f.t = t
	f.objects = []runtime.Object{}
	f.kubeobjects = []runtime.Object{}
	return f
}

func newRollout(name string, replicas int, revisionHistoryLimit *int32, selector map[string]string, activeSvc string, previewSvc string) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			UID:       uuid.NewUUID(),
			Name:      name,
			Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{
				annotations.RevisionAnnotation: "1",
			},
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Type: v1alpha1.BlueGreenRolloutStrategyType,
				BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{
					ActiveService:  activeSvc,
					PreviewService: previewSvc,
				},
			},
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
	}
}

func newReplicaSetWithStatus(r *v1alpha1.Rollout, name string, replicas int, availableReplicas int) *appsv1.ReplicaSet {
	rs := newReplicaSet(r, name, replicas)
	rs.Status.AvailableReplicas = int32(availableReplicas)
	return rs
}

func newReplicaSet(r *v1alpha1.Rollout, name string, replicas int) *appsv1.ReplicaSet {
	newRSTemplate := *r.Spec.Template.DeepCopy()
	rsLabels := map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: controller.ComputeHash(&newRSTemplate, r.Status.CollisionCount),
	}
	for k, v := range r.Spec.Selector.MatchLabels {
		rsLabels[k] = v
	}
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			UID:             uuid.NewUUID(),
			Namespace:       metav1.NamespaceDefault,
			Labels:          rsLabels,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(r, controllerKind)},
			Annotations: map[string]string{
				annotations.DesiredReplicasAnnotation: strconv.Itoa(replicas),
				annotations.RevisionAnnotation:        r.Annotations[annotations.RevisionAnnotation],
			},
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: r.Spec.Selector,
			Replicas: func() *int32 { i := int32(replicas); return &i }(),
			Template: r.Spec.Template,
		},
	}
}

func newImage(rs *appsv1.ReplicaSet, newImage string) *appsv1.ReplicaSet {
	rsCopy := rs.DeepCopy()
	rsCopy.Spec.Template.Spec.Containers[0].Image = newImage
	rsCopy.ObjectMeta.Name = controller.ComputeHash(&rsCopy.Spec.Template, nil)
	return rsCopy
}

func getKey(rollout *v1alpha1.Rollout, t *testing.T) string {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(rollout)
	if err != nil {
		t.Errorf("Unexpected error getting key for rollout %v: %v", rollout.Name, err)
		return ""
	}
	return key
}

func (f *fixture) newController() (*Controller, informers.SharedInformerFactory, kubeinformers.SharedInformerFactory) {
	f.client = fake.NewSimpleClientset(f.objects...)
	f.kubeclient = k8sfake.NewSimpleClientset(f.kubeobjects...)

	i := informers.NewSharedInformerFactory(f.client, noResyncPeriodFunc())
	k8sI := kubeinformers.NewSharedInformerFactory(f.kubeclient, noResyncPeriodFunc())

	c := NewController(f.kubeclient, f.client,
		k8sI.Apps().V1().ReplicaSets(),
		k8sI.Core().V1().Services(),
		i.Argoproj().V1alpha1().Rollouts())

	c.rolloutsSynced = alwaysReady
	c.replicaSetSynced = alwaysReady
	c.serviceSynced = alwaysReady
	c.recorder = &record.FakeRecorder{}
	c.enqueueRollout = c.enqueue

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
	f.runController(rolloutName, true, false)
}

func (f *fixture) runExpectError(rolloutName string, startInformers bool) {
	f.runController(rolloutName, startInformers, true)
}

func (f *fixture) runController(rolloutName string, startInformers bool, expectError bool) {
	c, i, k8sI := f.newController()
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
}

// checkAction verifies that expected and actual actions are equal
func checkAction(expected, actual core.Action, t *testing.T) {
	if !(expected.Matches(actual.GetVerb(), actual.GetResource().Resource) && actual.GetSubresource() == expected.GetSubresource()) {
		t.Errorf("Expected\n\t%#v\ngot\n\t%#v", expected, actual)
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

func (f *fixture) expectPatchServiceAction(s *corev1.Service, rs *appsv1.ReplicaSet) {
	patch := corev1.Service{
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]},
		},
	}
	patchBytes, _ := json.Marshal(patch)
	serviceSchema := schema.GroupVersionResource{
		Resource: "services",
		Version:  "v1",
	}
	f.kubeactions = append(f.kubeactions, core.NewPatchAction(serviceSchema, s.Namespace, s.Name, patchBytes))
}

func (f *fixture) expectCreateReplicaSetAction(r *appsv1.ReplicaSet) {
	f.kubeactions = append(f.kubeactions, core.NewCreateAction(schema.GroupVersionResource{Resource: "replicasets"}, r.Namespace, r))
}

func (f *fixture) expectUpdateReplicaSetAction(r *appsv1.ReplicaSet) {
	f.kubeactions = append(f.kubeactions, core.NewUpdateAction(schema.GroupVersionResource{Resource: "replicasets"}, r.Namespace, r))
}

func (f *fixture) expectGetRolloutAction(rollout *v1alpha1.Rollout) {
	f.kubeactions = append(f.kubeactions, core.NewGetAction(schema.GroupVersionResource{Resource: "rollouts"}, rollout.Namespace, rollout.Name))
}

func (f *fixture) expectUpdateRolloutAction(rollout *v1alpha1.Rollout) {
	action := core.NewUpdateAction(schema.GroupVersionResource{Resource: "rollouts"}, rollout.Namespace, rollout)
	f.actions = append(f.actions, action)
}

func (f *fixture) expectUpdateRolloutStatusAction(rollout *v1alpha1.Rollout) {
	action := core.NewUpdateAction(schema.GroupVersionResource{Resource: "rollouts"}, rollout.Namespace, rollout)
	action.Subresource = "status"
	f.actions = append(f.actions, action)
}

func TestSyncRolloutCreatesReplicaSet(t *testing.T) {
	f := newFixture(t)

	r := newRollout("foo", 1, nil, map[string]string{"foo": "bar"}, "bar", "")
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	rs := newReplicaSet(r, "foo-895c6c4f9", 1)

	f.expectCreateReplicaSetAction(rs)
	f.run(getKey(r, t))
}

func TestDontSyncRolloutsWithEmptyPodSelector(t *testing.T) {
	f := newFixture(t)

	r := newRollout("foo", 1, nil, nil, "", "")
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	f.run(getKey(r, t))
}

func TestSyncRolloutsScaleDownOldRS(t *testing.T) {
	f := newFixture(t)

	r1 := newRollout("foo", 1, nil, map[string]string{"foo": "bar"}, "bar", "")

	r2 := r1.DeepCopy()
	annotations.SetRolloutRevision(r2, "2")
	r2.Spec.Template.Spec.Containers[0].Image = "foo/bar2.0"
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	rs1 := newReplicaSetWithStatus(r1, "foo-895c6c4f9", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	rs2 := newReplicaSetWithStatus(r2, "foo-6479c8f85c", 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)

	expRS := rs2.DeepCopy()
	expRS.Annotations[annotations.DesiredReplicasAnnotation] = "0"
	f.expectUpdateReplicaSetAction(expRS)

	f.run(getKey(r2, t))
}
