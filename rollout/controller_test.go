package rollout

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/bouk/monkey"
	"github.com/ghodss/yaml"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/uuid"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	corev1defaults "k8s.io/kubernetes/pkg/apis/core/v1"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
)

var (
	noResyncPeriodFunc = func() time.Duration { return 0 }
)

const (
	OnlyObservedGenerationPatch = `{
			"status" : {
				"observedGeneration": ""
			}
	}`
	MockGeneratedNameSuffix = "abc123"
)

type fixture struct {
	t *testing.T

	client     *fake.Clientset
	kubeclient *k8sfake.Clientset
	// Objects to put in the store.
	rolloutLister          []*v1alpha1.Rollout
	experimentLister       []*v1alpha1.Experiment
	analysisRunLister      []*v1alpha1.AnalysisRun
	analysisTemplateLister []*v1alpha1.AnalysisTemplate
	replicaSetLister       []*appsv1.ReplicaSet
	serviceLister          []*corev1.Service
	// Actions expected to happen on the client.
	kubeactions []core.Action
	actions     []core.Action
	// Objects from here preloaded into NewSimpleFake.
	kubeobjects     []runtime.Object
	objects         []runtime.Object
	enqueuedObjects map[string]int
	unfreezeTime    func()
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{}
	f.t = t
	f.objects = []runtime.Object{}
	f.kubeobjects = []runtime.Object{}
	f.enqueuedObjects = make(map[string]int)
	now := time.Now()
	patch := monkey.Patch(time.Now, func() time.Time { return now })
	f.unfreezeTime = patch.Unpatch
	return f
}

func (f *fixture) Close() {
	f.unfreezeTime()
}

const (
	defaultTestPodHash = "78574f5b57"
)

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
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, ro)
	conditions.SetRolloutCondition(&ro.Status, progressingCondition)
	return ro
}

func newReplicaSetWithStatus(r *v1alpha1.Rollout, replicas int, availableReplicas int) *appsv1.ReplicaSet {
	rs := newReplicaSet(r, replicas)
	rs.Status.Replicas = int32(replicas)
	rs.Status.AvailableReplicas = int32(availableReplicas)
	return rs
}

func newProgressingCondition(reason string, resourceObj runtime.Object) (v1alpha1.RolloutCondition, string) {
	status := corev1.ConditionTrue
	msg := ""
	switch resource := resourceObj.(type) {
	case *appsv1.ReplicaSet:
		if reason == conditions.ReplicaSetUpdatedReason {
			msg = fmt.Sprintf(conditions.ReplicaSetProgressingMessage, resource.Name)
		}
		if reason == conditions.ReplicaSetUpdatedReason {
			msg = fmt.Sprintf(conditions.ReplicaSetProgressingMessage, resource.Name)
		}
		if reason == conditions.NewReplicaSetReason {
			msg = fmt.Sprintf(conditions.NewReplicaSetMessage, resource.Name)
		}
		if reason == conditions.NewRSAvailableReason {
			msg = fmt.Sprintf(conditions.ReplicaSetCompletedMessage, resource.Name)
		}
	case *v1alpha1.Rollout:
		if reason == conditions.ReplicaSetUpdatedReason {
			msg = fmt.Sprintf(conditions.RolloutProgressingMessage, resource.Name)
		}
		if reason == conditions.RolloutExperimentFailedReason {
			msg = fmt.Sprintf(conditions.RolloutExperimentFailedMessage, experimentutil.ExperimentNameFromRollout(resource), resource.Name)
			status = corev1.ConditionFalse
		}
	case *corev1.Service:
		if reason == conditions.ServiceNotFoundReason {
			msg = fmt.Sprintf(conditions.ServiceNotFoundMessage, resource.Name)
			status = corev1.ConditionFalse
		}
	}

	if reason == conditions.PausedRolloutReason {
		msg = conditions.PausedRolloutMessage
		status = corev1.ConditionUnknown
	}
	if reason == conditions.ResumedRolloutReason {
		msg = conditions.ResumeRolloutMessage
		status = corev1.ConditionUnknown
	}

	condition := v1alpha1.RolloutCondition{
		LastTransitionTime: metav1.Now(),
		LastUpdateTime:     metav1.Now(),
		Message:            msg,
		Reason:             reason,
		Status:             status,
		Type:               v1alpha1.RolloutProgressing,
	}
	conditionBytes, err := json.Marshal(condition)
	if err != nil {
		panic(err)
	}
	return condition, string(conditionBytes)

}

func newAvailableCondition(available bool) (v1alpha1.RolloutCondition, string) {
	message := conditions.NotAvailableMessage
	status := corev1.ConditionFalse
	if available {
		message = conditions.AvailableMessage
		status = corev1.ConditionTrue

	}
	condition := v1alpha1.RolloutCondition{
		LastTransitionTime: metav1.Now(),
		LastUpdateTime:     metav1.Now(),
		Message:            message,
		Reason:             conditions.AvailableReason,
		Status:             status,
		Type:               v1alpha1.RolloutAvailable,
	}
	conditionBytes, _ := json.Marshal(condition)
	return condition, string(conditionBytes)
}

func generateConditionsPatch(available bool, progressingReason string, progressingResource runtime.Object, availableConditionFirst bool) string {
	_, availableCondition := newAvailableCondition(available)
	_, progressingConditon := newProgressingCondition(progressingReason, progressingResource)
	if availableConditionFirst {
		return fmt.Sprintf("[%s, %s]", availableCondition, progressingConditon)
	}
	return fmt.Sprintf("[%s, %s]", progressingConditon, availableCondition)
}

// func updateBlueGreenRolloutStatus(r *v1alpha1.Rollout, preview, active string, availableReplicas, updatedReplicas, hpaReplicas int32, pause bool, available bool, progressingStatus string) *v1alpha1.Rollout {
func updateBlueGreenRolloutStatus(r *v1alpha1.Rollout, preview, active string, availableReplicas, updatedReplicas, totalReplicas, hpaReplicas int32, pause bool, available bool) *v1alpha1.Rollout {
	newRollout := updateBaseRolloutStatus(r, availableReplicas, updatedReplicas, totalReplicas, hpaReplicas, pause)
	selector := newRollout.Spec.Selector.DeepCopy()
	if active != "" {
		selector.MatchLabels[v1alpha1.DefaultRolloutUniqueLabelKey] = active
	}
	newRollout.Status.Selector = metav1.FormatLabelSelector(selector)
	newRollout.Status.BlueGreen.ActiveSelector = active
	newRollout.Status.BlueGreen.PreviewSelector = preview
	cond, _ := newAvailableCondition(available)
	newRollout.Status.Conditions = append(newRollout.Status.Conditions, cond)
	return newRollout
}
func updateCanaryRolloutStatus(r *v1alpha1.Rollout, stableRS string, availableReplicas, updatedReplicas, hpaReplicas int32, pause bool) *v1alpha1.Rollout {
	newRollout := updateBaseRolloutStatus(r, availableReplicas, updatedReplicas, availableReplicas, hpaReplicas, pause)
	newRollout.Status.Canary.StableRS = stableRS
	return newRollout
}

func updateBaseRolloutStatus(r *v1alpha1.Rollout, availableReplicas, updatedReplicas, totalReplicas, hpaReplicas int32, pause bool) *v1alpha1.Rollout {
	newRollout := r.DeepCopy()
	newRollout.Status.Replicas = totalReplicas
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

func newReplicaSet(r *v1alpha1.Rollout, replicas int) *appsv1.ReplicaSet {
	newRSTemplate := *r.Spec.Template.DeepCopy()
	podHash := controller.ComputeHash(&newRSTemplate, r.Status.CollisionCount)
	rsLabels := map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
	}
	for k, v := range r.Spec.Selector.MatchLabels {
		rsLabels[k] = v
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("%s-%s", r.Name, podHash),
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

func cleanPatch(expectedPatch string) string {
	patch := make(map[string]interface{})
	json.Unmarshal([]byte(expectedPatch), &patch)
	patchStr, _ := json.Marshal(patch)
	return string(patchStr)
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

func (f *fixture) newController(resync resyncFunc) (*RolloutController, informers.SharedInformerFactory, kubeinformers.SharedInformerFactory) {
	f.client = fake.NewSimpleClientset(f.objects...)
	f.kubeclient = k8sfake.NewSimpleClientset(f.kubeobjects...)

	i := informers.NewSharedInformerFactory(f.client, resync())
	k8sI := kubeinformers.NewSharedInformerFactory(f.kubeclient, resync())

	rolloutWorkqueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Rollouts")
	serviceWorkqueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Services")

	c := NewRolloutController(f.kubeclient, f.client,
		i.Argoproj().V1alpha1().Experiments(),
		i.Argoproj().V1alpha1().AnalysisRuns(),
		i.Argoproj().V1alpha1().AnalysisTemplates(),
		k8sI.Apps().V1().ReplicaSets(),
		k8sI.Core().V1().Services(),
		i.Argoproj().V1alpha1().Rollouts(),
		resync(),
		rolloutWorkqueue,
		serviceWorkqueue,
		metrics.NewMetricsServer("localhost:8080", i.Argoproj().V1alpha1().Rollouts().Lister()),
		&record.FakeRecorder{})

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
		c.rolloutWorkqueue.Add(obj)
	}
	c.enqueueRolloutAfter = func(obj interface{}, duration time.Duration) {
		c.enqueueRollout(obj)
	}

	for _, r := range f.rolloutLister {
		i.Argoproj().V1alpha1().Rollouts().Informer().GetIndexer().Add(r)
	}

	for _, e := range f.experimentLister {
		i.Argoproj().V1alpha1().Experiments().Informer().GetIndexer().Add(e)
	}

	for _, r := range f.replicaSetLister {
		k8sI.Apps().V1().ReplicaSets().Informer().GetIndexer().Add(r)
	}
	for _, s := range f.serviceLister {
		k8sI.Core().V1().Services().Informer().GetIndexer().Add(s)
	}
	for _, at := range f.analysisTemplateLister {
		i.Argoproj().V1alpha1().AnalysisTemplates().Informer().GetIndexer().Add(at)
	}
	for _, ar := range f.analysisRunLister {
		i.Argoproj().V1alpha1().AnalysisRuns().Informer().GetIndexer().Add(ar)
	}

	f.client.PrependReactor("create", "analysisruns", func(action core.Action) (bool, runtime.Object, error) {
		createAction, ok := action.(core.CreateAction)
		if !ok {
			assert.Fail(f.t, "Expected Created action, not %s", action.GetVerb())
		}
		ar := &v1alpha1.AnalysisRun{}
		converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
		objMap, _ := converter.ToUnstructured(createAction.GetObject())
		runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ar)
		ar.Name = ar.GenerateName + "-" + MockGeneratedNameSuffix
		return true, ar.DeepCopyObject(), nil
	})

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

func (f *fixture) runController(rolloutName string, startInformers bool, expectError bool, c *RolloutController, i informers.SharedInformerFactory, k8sI kubeinformers.SharedInformerFactory) *RolloutController {
	if startInformers {
		stopCh := make(chan struct{})
		defer close(stopCh)
		i.Start(stopCh)
		k8sI.Start(stopCh)

		assert.True(f.t, cache.WaitForCacheSync(stopCh, c.replicaSetSynced, c.rolloutsSynced))
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
		if action.Matches("list", "experiments") ||
			action.Matches("watch", "experiments") ||
			action.Matches("list", "analysisruns") ||
			action.Matches("watch", "analysisruns") ||
			action.Matches("list", "analysistemplates") ||
			action.Matches("watch", "analysistemplates") ||
			action.Matches("list", "rollouts") ||
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

func (f *fixture) expectPatchServiceAction(s *corev1.Service, newLabel string) int {
	patch := fmt.Sprintf(switchSelectorPatch, v1alpha1.DefaultRolloutUniqueLabelKey, newLabel)
	serviceSchema := schema.GroupVersionResource{
		Resource: "services",
		Version:  "v1",
	}
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, core.NewPatchAction(serviceSchema, s.Namespace, s.Name, types.MergePatchType, []byte(patch)))
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

func (f *fixture) expectPatchReplicaSetAction(rs *appsv1.ReplicaSet) int {
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, core.NewPatchAction(schema.GroupVersionResource{Resource: "replicasets"}, rs.Namespace, rs.Name, types.MergePatchType, nil))
	return len
}

func (f *fixture) expectGetRolloutAction(rollout *v1alpha1.Rollout) int {
	len := len(f.actions)
	f.kubeactions = append(f.actions, core.NewGetAction(schema.GroupVersionResource{Resource: "rollouts"}, rollout.Namespace, rollout.Name))
	return len
}

func (f *fixture) expectCreateExperimentAction(ex *v1alpha1.Experiment) int {
	action := core.NewCreateAction(schema.GroupVersionResource{Resource: "experiments"}, ex.Namespace, ex)
	len := len(f.actions)
	f.actions = append(f.actions, action)
	return len
}

func (f *fixture) expectUpdateExperimentAction(ex *v1alpha1.Experiment) int {
	action := core.NewUpdateAction(schema.GroupVersionResource{Resource: "experiments"}, ex.Namespace, ex)
	len := len(f.actions)
	f.actions = append(f.actions, action)
	return len
}

func (f *fixture) expectPatchAnalysisRunAction(ar *v1alpha1.AnalysisRun) int {
	analysisRunSchema := schema.GroupVersionResource{
		Resource: "analysisruns",
		Version:  "v1alpha1",
	}
	len := len(f.actions)
	f.actions = append(f.actions, core.NewPatchAction(analysisRunSchema, ar.Namespace, ar.Name, types.MergePatchType, nil))
	return len
}

func (f *fixture) expectCreateAnalysisRunAction(ar *v1alpha1.AnalysisRun) int {
	action := core.NewCreateAction(schema.GroupVersionResource{Resource: "analysisruns"}, ar.Namespace, ar)
	len := len(f.actions)
	f.actions = append(f.actions, action)
	return len
}

func (f *fixture) expectUpdateRolloutAction(rollout *v1alpha1.Rollout) int {
	action := core.NewUpdateAction(schema.GroupVersionResource{Resource: "rollouts"}, rollout.Namespace, rollout)
	len := len(f.actions)
	f.actions = append(f.actions, action)
	return len
}

func (f *fixture) expectPatchExperimentAction(ex *v1alpha1.Experiment) int {
	experimentSchema := schema.GroupVersionResource{
		Resource: "experiments",
		Version:  "v1alpha1",
	}
	len := len(f.actions)
	f.actions = append(f.actions, core.NewPatchAction(experimentSchema, ex.Namespace, ex.Name, types.MergePatchType, nil))
	return len
}

func (f *fixture) expectPatchRolloutAction(rollout *v1alpha1.Rollout) int {
	serviceSchema := schema.GroupVersionResource{
		Resource: "rollouts",
		Version:  "v1alpha1",
	}
	len := len(f.actions)
	f.actions = append(f.actions, core.NewPatchAction(serviceSchema, rollout.Namespace, rollout.Name, types.MergePatchType, nil))
	return len
}

func (f *fixture) expectPatchRolloutActionWithPatch(rollout *v1alpha1.Rollout, patch string) int {
	expectedPatch := calculatePatch(rollout, patch)
	serviceSchema := schema.GroupVersionResource{
		Resource: "rollouts",
		Version:  "v1alpha1",
	}
	len := len(f.actions)
	f.actions = append(f.actions, core.NewPatchAction(serviceSchema, rollout.Namespace, rollout.Name, types.MergePatchType, []byte(expectedPatch)))
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

func (f *fixture) verifyPatchedReplicaSet(index int, scaleDownDelaySeconds int32) bool {
	action := filterInformerActions(f.kubeclient.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		assert.Fail(f.t, "Expected Patch action, not %s", action.GetVerb())
	}
	now := metav1.Now().Add(time.Duration(scaleDownDelaySeconds) * time.Second).UTC().Format(time.RFC3339)
	patch := fmt.Sprintf(addScaleDownAtAnnotationsPatch, v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey, now)
	return string(patchAction.GetPatch()) == patch
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

func (f *fixture) getPatchedAnalysisRun(index int) *v1alpha1.AnalysisRun {
	action := filterInformerActions(f.client.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	ar := v1alpha1.AnalysisRun{}
	err := json.Unmarshal(patchAction.GetPatch(), &ar)
	if err != nil {
		panic(err)
	}
	return &ar
}

func (f *fixture) getCreatedAnalysisRun(index int) *v1alpha1.AnalysisRun {
	action := filterInformerActions(f.client.Actions())[index]
	createAction, ok := action.(core.CreateAction)
	if !ok {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	obj := createAction.GetObject()
	ar := &v1alpha1.AnalysisRun{}
	converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ := converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ar)
	return ar
}

func (f *fixture) getPatchedExperiment(index int) *v1alpha1.Experiment {
	action := filterInformerActions(f.client.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	e := v1alpha1.Experiment{}
	err := json.Unmarshal(patchAction.GetPatch(), &e)
	if err != nil {
		panic(err)
	}
	return &e
}

func (f *fixture) getPatchedRollout(index int) string {
	action := filterInformerActions(f.client.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	return string(patchAction.GetPatch())
}

func TestDontSyncRolloutsWithEmptyPodSelector(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "", "")
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
}

func TestAdoptReplicaSet(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r.Status.Conditions = []v1alpha1.RolloutCondition{}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	previewSvc := newService("preview", 80, nil)
	activeSvc := newService("active", 80, nil)

	rs := newReplicaSet(r, 1)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)
	f.serviceLister = append(f.serviceLister, previewSvc, activeSvc)

	updatedRolloutIndex := f.expectUpdateRolloutAction(r)
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	updatedRollout := f.getUpdatedRollout(updatedRolloutIndex)
	progressingCondition := conditions.GetRolloutCondition(updatedRollout.Status, v1alpha1.RolloutProgressing)
	assert.NotNil(t, progressingCondition)
	assert.Equal(t, fmt.Sprintf(conditions.FoundNewRSMessage, rs.Name), progressingCondition.Message)
	assert.Equal(t, conditions.FoundNewRSReason, progressingCondition.Reason)
}

func TestRequeueStuckRollout(t *testing.T) {
	rollout := func(progressingConditionReason string, rolloutCompleted bool, rolloutPaused bool, progessDeadlineSeconds *int32) *v1alpha1.Rollout {
		r := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Replicas:                pointer.Int32Ptr(0),
				Paused:                  rolloutPaused,
				ProgressDeadlineSeconds: progessDeadlineSeconds,
			},
		}
		if rolloutCompleted {
			r.Status.ObservedGeneration = conditions.ComputeGenerationHash(r.Spec)
		}

		if progressingConditionReason != "" {
			lastUpdated := metav1.Time{
				Time: metav1.Now().Add(-10 * time.Second),
			}
			r.Status.Conditions = []v1alpha1.RolloutCondition{{
				Type:           v1alpha1.RolloutProgressing,
				Reason:         progressingConditionReason,
				LastUpdateTime: lastUpdated,
			}}
		}

		return r
	}

	tests := []struct {
		name               string
		rollout            *v1alpha1.Rollout
		requeueImmediately bool
		noRequeue          bool
	}{
		{
			name:      "No Progressing Condition",
			rollout:   rollout("", false, false, nil),
			noRequeue: true,
		},
		{
			name:      "Rollout Completed",
			rollout:   rollout(conditions.ReplicaSetUpdatedReason, true, false, nil),
			noRequeue: true,
		},
		{
			name:      "Rollout Timed out",
			rollout:   rollout(conditions.TimedOutReason, false, false, nil),
			noRequeue: true,
		},
		{
			name:      "Rollout Paused",
			rollout:   rollout(conditions.ReplicaSetUpdatedReason, false, true, nil),
			noRequeue: true,
		},
		{
			name:               "Less than a second",
			rollout:            rollout(conditions.ReplicaSetUpdatedReason, false, false, pointer.Int32Ptr(10)),
			requeueImmediately: true,
		},
		{
			name:    "More than a second",
			rollout: rollout(conditions.ReplicaSetUpdatedReason, false, false, pointer.Int32Ptr(20)),
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			f := newFixture(t)
			defer f.Close()
			c, _, _ := f.newController(noResyncPeriodFunc)
			duration := c.requeueStuckRollout(test.rollout, test.rollout.Status)
			if test.noRequeue {
				assert.Equal(t, time.Duration(-1), duration)
			} else if test.requeueImmediately {
				assert.Equal(t, time.Duration(0), duration)
			} else {
				assert.NotEqual(t, time.Duration(-1), duration)
				assert.NotEqual(t, time.Duration(0), duration)
			}
		})
	}
}

func TestSetReplicaToDefault(t *testing.T) {
	f := newFixture(t)
	r := newCanaryRollout("foo", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
	r.Spec.Replicas = nil
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	updateIndex := f.expectUpdateRolloutAction(r)
	f.run(getKey(r, t))
	updatedRollout := f.getUpdatedRollout(updateIndex)
	assert.Equal(t, defaults.DefaultReplicas, *updatedRollout.Spec.Replicas)
}

func TestSwitchInvalidSpecMessage(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "", "")
	r.Spec.Selector = &metav1.LabelSelector{}
	cond := conditions.NewRolloutCondition(v1alpha1.InvalidSpec, corev1.ConditionTrue, conditions.InvalidSpecReason, conditions.RolloutSelectAllMessage)
	conditions.SetRolloutCondition(&r.Status, *cond)

	r.Spec.Selector = nil
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	expectedPatchWithoutSub := `{
		"status": {
			"conditions": [%s,%s]
		}
	}`
	_, progressingCond := newProgressingCondition(conditions.ReplicaSetUpdatedReason, r)
	invalidSpecCond := conditions.NewRolloutCondition(v1alpha1.InvalidSpec, corev1.ConditionTrue, conditions.InvalidSpecReason, fmt.Sprintf(conditions.MissingFieldMessage, ".Spec.Selector"))
	invalidSpecBytes, _ := json.Marshal(invalidSpecCond)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutSub, progressingCond, string(invalidSpecBytes))

	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, calculatePatch(r, expectedPatch), patch)
}

// TestPodTemplateHashEquivalence verifies the hash is computed consistently when there are slight
// variations made to the pod template in equivalent ways.
func TestPodTemplateHashEquivalence(t *testing.T) {
	var err error
	expectedReplicaSetName := "guestbook-75fc5957d4"

	r1 := newBlueGreenRollout("guestbook", 1, nil, "active", "")
	r1Resources := `
limits:
  cpu: 150m
  memory: 8192M
requests:
  cpu: 2000m
  memory: 8192M
`
	err = yaml.Unmarshal([]byte(r1Resources), &r1.Spec.Template.Spec.Containers[0].Resources)
	assert.NoError(t, err)

	r2 := newBlueGreenRollout("guestbook", 1, nil, "active", "")
	r2Resources := `
  limits:
    cpu: 0.15
    memory: 8192M
  requests:
    cpu: '2'
    memory: 8192M
`
	err = yaml.Unmarshal([]byte(r2Resources), &r2.Spec.Template.Spec.Containers[0].Resources)
	assert.NoError(t, err)

	for _, r := range []*v1alpha1.Rollout{r1, r2} {
		f := newFixture(t)
		defer f.Close()
		activeSvc := newService("active", 80, nil)
		f.kubeobjects = append(f.kubeobjects, activeSvc)
		f.rolloutLister = append(f.rolloutLister, r)
		f.serviceLister = append(f.serviceLister, activeSvc)
		f.objects = append(f.objects, r)

		_ = f.expectUpdateRolloutAction(r)
		f.expectPatchRolloutAction(r)
		rs := newReplicaSet(r, 1)
		rsIdx := f.expectCreateReplicaSetAction(rs)
		f.run(getKey(r, t))
		rs = f.getCreatedReplicaSet(rsIdx)
		assert.Equal(t, expectedReplicaSetName, rs.Name)
	}
}

func TestNoReconcileForDeletedRollout(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newRollout("foo", 1, nil, nil)
	now := metav1.Now()
	r.DeletionTimestamp = &now

	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	f.run(getKey(r, t))
}

// TestComputeHashChangeTolerationBlueGreen verifies that we can tolerate a change in
// controller.ComputeHash() for the blue-green strategy and do not redeploy any replicasets
func TestComputeHashChangeTolerationBlueGreen(t *testing.T) {
	f := newFixture(t)

	r := newBlueGreenRollout("foo", 1, nil, "active", "")
	r.Status.CurrentPodHash = "fakepodhash"
	r.Status.AvailableReplicas = 1
	r.Status.ReadyReplicas = 1
	r.Status.BlueGreen.ActiveSelector = "fakepodhash"
	r.Status.ObservedGeneration = "fakeobservedgeneration"
	rs := newReplicaSet(r, 1)
	rs.Name = "foo-fakepodhash"
	rs.Status.AvailableReplicas = 1
	rs.Status.ReadyReplicas = 1
	rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = "fakepodhash"

	selector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			v1alpha1.DefaultRolloutUniqueLabelKey: "fakepodhash",
			"foo":                                 "bar",
		},
	}
	r.Status.Selector = metav1.FormatLabelSelector(&selector)
	rs.Spec.Selector = &selector
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r.Status, availableCondition)
	progressingConditon, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs)
	conditions.SetRolloutCondition(&r.Status, progressingConditon)

	podTemplate := corev1.PodTemplate{
		Template: rs.Spec.Template,
	}
	corev1defaults.SetObjectDefaults_PodTemplate(&podTemplate)
	rs.Spec.Template = podTemplate.Template

	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	activeSvc := newService("active", 80, selector.MatchLabels)

	f.kubeobjects = append(f.kubeobjects, activeSvc, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)
	f.serviceLister = append(f.serviceLister, activeSvc)

	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
	// this should only update observedGeneration and nothing else
	expectedPatch := `{"status":{"observedGeneration":"779b98dbc5"}}`
	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, expectedPatch, patch)
}

// TestComputeHashChangeTolerationCanary verifies that we can tolerate a change in
// controller.ComputeHash() for the canary strategy and do not redeploy any replicasets
func TestComputeHashChangeTolerationCanary(t *testing.T) {
	f := newFixture(t)

	r := newCanaryRollout("foo", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))

	r.Status.CurrentPodHash = "fakepodhash"
	r.Status.Canary.StableRS = "fakepodhash"
	r.Status.AvailableReplicas = 1
	r.Status.ReadyReplicas = 1
	r.Status.ObservedGeneration = "fakeobservedgeneration"
	rs := newReplicaSet(r, 1)
	rs.Name = "foo-fakepodhash"
	rs.Status.AvailableReplicas = 1
	rs.Status.ReadyReplicas = 1
	rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = "fakepodhash"
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r.Status, availableCondition)
	progressingConditon, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs)
	conditions.SetRolloutCondition(&r.Status, progressingConditon)

	podTemplate := corev1.PodTemplate{
		Template: rs.Spec.Template,
	}
	corev1defaults.SetObjectDefaults_PodTemplate(&podTemplate)
	rs.Spec.Template = podTemplate.Template

	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	f.kubeobjects = append(f.kubeobjects, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)

	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
	// this should only update observedGeneration and nothing else
	expectedPatch := `{"status":{"observedGeneration":"6479797d56"}}`
	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, expectedPatch, patch)
}
