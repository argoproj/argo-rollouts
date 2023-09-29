package experiments

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	timeutil "github.com/argoproj/argo-rollouts/utils/time"

	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/utils/queue"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/uuid"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/record"
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
)

func now() *metav1.Time {
	now := metav1.Time{Time: timeutil.Now().Truncate(time.Second)}
	return &now
}

func secondsAgo(seconds int) *metav1.Time {
	ago := metav1.Time{Time: timeutil.Now().Add(-1 * time.Second * time.Duration(seconds)).Truncate(time.Second)}
	return &ago
}

type fixture struct {
	t *testing.T

	client     *fake.Clientset
	kubeclient *k8sfake.Clientset
	// Objects to put in the store.
	experimentLister              []*v1alpha1.Experiment
	replicaSetLister              []*appsv1.ReplicaSet
	analysisRunLister             []*v1alpha1.AnalysisRun
	analysisTemplateLister        []*v1alpha1.AnalysisTemplate
	clusterAnalysisTemplateLister []*v1alpha1.ClusterAnalysisTemplate
	serviceLister                 []*corev1.Service
	// Actions expected to happen on the client.
	kubeactions []core.Action
	actions     []core.Action
	// Objects from here preloaded into NewSimpleFake.
	kubeobjects     []runtime.Object
	objects         []runtime.Object
	enqueuedObjects map[string]int
	unfreezeTime    func() error
}

func newFixture(t *testing.T, objects ...runtime.Object) *fixture {
	f := &fixture{}
	f.t = t
	f.objects = []runtime.Object{}
	f.kubeobjects = []runtime.Object{}
	for _, obj := range objects {
		switch obj.(type) {
		case *v1alpha1.ClusterAnalysisTemplate:
			f.objects = append(f.objects, obj)
			f.clusterAnalysisTemplateLister = append(f.clusterAnalysisTemplateLister, obj.(*v1alpha1.ClusterAnalysisTemplate))
		case *v1alpha1.AnalysisTemplate:
			f.objects = append(f.objects, obj)
			f.analysisTemplateLister = append(f.analysisTemplateLister, obj.(*v1alpha1.AnalysisTemplate))
		case *v1alpha1.AnalysisRun:
			f.objects = append(f.objects, obj)
			f.analysisRunLister = append(f.analysisRunLister, obj.(*v1alpha1.AnalysisRun))
		case *v1alpha1.Experiment:
			f.objects = append(f.objects, obj)
			f.experimentLister = append(f.experimentLister, obj.(*v1alpha1.Experiment))
		case *appsv1.ReplicaSet:
			f.kubeobjects = append(f.kubeobjects, obj)
			f.replicaSetLister = append(f.replicaSetLister, obj.(*appsv1.ReplicaSet))
		case *corev1.Service:
			f.kubeobjects = append(f.kubeobjects, obj)
			f.serviceLister = append(f.serviceLister, obj.(*corev1.Service))
		}
	}
	f.client = fake.NewSimpleClientset(f.objects...)
	f.kubeclient = k8sfake.NewSimpleClientset(f.kubeobjects...)
	f.enqueuedObjects = make(map[string]int)
	now := time.Now()
	timeutil.Now = func() time.Time {
		return now
	}
	f.unfreezeTime = func() error {
		timeutil.Now = time.Now
		return nil
	}
	return f
}

func (f *fixture) Close() {
	f.unfreezeTime()
}

func generateTemplates(imageNames ...string) []v1alpha1.TemplateSpec {
	templates := make([]v1alpha1.TemplateSpec, 0)
	for _, imageName := range imageNames {
		selector := map[string]string{
			"key": imageName,
		}
		template := v1alpha1.TemplateSpec{
			Name: imageName,
			Selector: &metav1.LabelSelector{
				MatchLabels: selector,
			},
			Replicas: pointer.Int32Ptr(1),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: selector,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: imageName,
						},
					},
				},
			},
		}
		templates = append(templates, template)
	}
	return templates
}

func generateTemplatesStatus(name string, replica, availableReplicas int32, status v1alpha1.TemplateStatusCode, transitionTime *metav1.Time) v1alpha1.TemplateStatus {
	return v1alpha1.TemplateStatus{
		Name:               name,
		Replicas:           replica,
		UpdatedReplicas:    availableReplicas,
		ReadyReplicas:      availableReplicas,
		AvailableReplicas:  availableReplicas,
		Status:             status,
		LastTransitionTime: transitionTime,
	}
}

func newExperiment(name string, templates []v1alpha1.TemplateSpec, duration v1alpha1.DurationString) *v1alpha1.Experiment {
	ex := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			UID:       uuid.NewUUID(),
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.ExperimentSpec{
			Templates: templates,
			Duration:  duration,
		},
		Status: v1alpha1.ExperimentStatus{
			Phase: v1alpha1.AnalysisPhasePending,
		},
	}
	if duration != "" {
		// Ensure that the experiment created is valid by making the ProgressDeadlineSeconds smaller than the duration
		d, err := duration.Duration()
		if err != nil {
			panic(err)
		}
		pds := int32(d.Seconds() - 1)
		ex.Spec.ProgressDeadlineSeconds = &pds
	}
	return ex
}

func newCondition(reason string, experiment *v1alpha1.Experiment) *v1alpha1.ExperimentCondition {
	if reason == conditions.ReplicaSetUpdatedReason {
		return &v1alpha1.ExperimentCondition{
			Type:               v1alpha1.ExperimentProgressing,
			Status:             corev1.ConditionTrue,
			LastUpdateTime:     timeutil.MetaNow().Rfc3339Copy(),
			LastTransitionTime: timeutil.MetaNow().Rfc3339Copy(),
			Reason:             reason,
			Message:            fmt.Sprintf(conditions.ExperimentProgressingMessage, experiment.Name),
		}
	}
	if reason == conditions.ExperimentCompleteReason {
		return &v1alpha1.ExperimentCondition{
			Type:               v1alpha1.ExperimentProgressing,
			Status:             corev1.ConditionFalse,
			LastUpdateTime:     timeutil.MetaNow().Rfc3339Copy(),
			LastTransitionTime: timeutil.MetaNow().Rfc3339Copy(),
			Reason:             reason,
			Message:            fmt.Sprintf(conditions.ExperimentCompletedMessage, experiment.Name),
		}
	}
	if reason == conditions.ReplicaSetUpdatedReason {
		return &v1alpha1.ExperimentCondition{
			Type:               v1alpha1.ExperimentProgressing,
			Status:             corev1.ConditionFalse,
			LastUpdateTime:     timeutil.MetaNow().Rfc3339Copy(),
			LastTransitionTime: timeutil.MetaNow().Rfc3339Copy(),
			Reason:             reason,
			Message:            fmt.Sprintf(conditions.ExperimentRunningMessage, experiment.Name),
		}
	}
	if reason == conditions.InvalidSpecReason {
		return &v1alpha1.ExperimentCondition{
			Type:               v1alpha1.InvalidExperimentSpec,
			Status:             corev1.ConditionTrue,
			LastUpdateTime:     timeutil.MetaNow().Rfc3339Copy(),
			LastTransitionTime: timeutil.MetaNow().Rfc3339Copy(),
			Reason:             reason,
			Message:            fmt.Sprintf(conditions.ExperimentTemplateNameEmpty, experiment.Name, 0),
		}
	}

	return nil
}

func templateToService(ex *v1alpha1.Experiment, template v1alpha1.TemplateSpec, replicaSet appsv1.ReplicaSet) *corev1.Service {
	if template.Service != nil {
		service := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      replicaSet.Name,
				Namespace: replicaSet.Namespace,
				Annotations: map[string]string{
					v1alpha1.ExperimentNameAnnotationKey:         ex.Name,
					v1alpha1.ExperimentTemplateNameAnnotationKey: template.Name,
				},
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ex, experimentKind)},
			},
			Spec: corev1.ServiceSpec{
				Selector: replicaSet.Spec.Selector.MatchLabels,
				Ports: []corev1.ServicePort{{
					Protocol:   "TCP",
					Port:       int32(80),
					TargetPort: intstr.FromInt(8080),
				}},
			},
		}
		return service
	}
	return nil
}

func templateToRS(ex *v1alpha1.Experiment, template v1alpha1.TemplateSpec, availableReplicas int32) *appsv1.ReplicaSet {
	rsLabels := map[string]string{}
	for k, v := range template.Selector.MatchLabels {
		rsLabels[k] = v
	}
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("%s-%s", ex.Name, template.Name),
			UID:             uuid.NewUUID(),
			Namespace:       metav1.NamespaceDefault,
			Annotations:     newReplicaSetAnnotations(ex.Name, template.Name),
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ex, controllerKind)},
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: metav1.SetAsLabelSelector(rsLabels),
			Replicas: template.Replicas,
			Template: template.Template,
		},
		Status: appsv1.ReplicaSetStatus{
			Replicas:          availableReplicas,
			ReadyReplicas:     availableReplicas,
			AvailableReplicas: availableReplicas,
		},
	}
	return rs
}

func generateRSName(ex *v1alpha1.Experiment, template v1alpha1.TemplateSpec) string {
	return fmt.Sprintf("%s-%s", ex.Name, template.Name)
}

func calculatePatch(ex *v1alpha1.Experiment, patch string, templates []v1alpha1.TemplateStatus, condition *v1alpha1.ExperimentCondition) string {
	patchMap := make(map[string]interface{})
	err := json.Unmarshal([]byte(patch), &patchMap)
	if err != nil {
		panic(err)
	}
	newStatus := patchMap["status"].(map[string]interface{})
	if templates != nil {
		newStatus["templateStatuses"] = templates
		patchMap["status"] = newStatus
	}
	if condition != nil {
		newStatus["conditions"] = []v1alpha1.ExperimentCondition{*condition}
		patchMap["status"] = newStatus
	}

	patchBytes, err := json.Marshal(patchMap)
	if err != nil {
		panic(err)
	}

	origBytes, err := json.Marshal(ex)
	if err != nil {
		panic(err)
	}
	newBytes, err := strategicpatch.StrategicMergePatch(origBytes, patchBytes, v1alpha1.Experiment{})
	if err != nil {
		panic(err)
	}

	newEx := &v1alpha1.Experiment{}
	json.Unmarshal(newBytes, newEx)

	newPatch := make(map[string]interface{})
	json.Unmarshal(patchBytes, &newPatch)
	newPatchBytes, _ := json.Marshal(newPatch)
	return string(newPatchBytes)
}

func getKey(experiment *v1alpha1.Experiment, t *testing.T) string {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(experiment)
	if err != nil {
		t.Errorf("Unexpected error getting key for experiment %v: %v", experiment.Name, err)
		return ""
	}
	return key
}

type resyncFunc func() time.Duration

func (f *fixture) newController(resync resyncFunc) (*Controller, informers.SharedInformerFactory, kubeinformers.SharedInformerFactory) {
	i := informers.NewSharedInformerFactory(f.client, resync())
	k8sI := kubeinformers.NewSharedInformerFactory(f.kubeclient, resync())

	rolloutWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	experimentWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Experiments")

	metricsServer := metrics.NewMetricsServer(metrics.ServerConfig{
		Addr:               "localhost:8080",
		K8SRequestProvider: &metrics.K8sRequestsCountProvider{},
	})

	c := NewController(ControllerConfig{
		KubeClientSet:                   f.kubeclient,
		ArgoProjClientset:               f.client,
		ReplicaSetInformer:              k8sI.Apps().V1().ReplicaSets(),
		ExperimentsInformer:             i.Argoproj().V1alpha1().Experiments(),
		AnalysisRunInformer:             i.Argoproj().V1alpha1().AnalysisRuns(),
		AnalysisTemplateInformer:        i.Argoproj().V1alpha1().AnalysisTemplates(),
		ClusterAnalysisTemplateInformer: i.Argoproj().V1alpha1().ClusterAnalysisTemplates(),
		ServiceInformer:                 k8sI.Core().V1().Services(),
		ResyncPeriod:                    resync(),
		RolloutWorkQueue:                rolloutWorkqueue,
		ExperimentWorkQueue:             experimentWorkqueue,
		MetricsServer:                   metricsServer,
		Recorder:                        record.NewFakeEventRecorder(),
	})

	var enqueuedObjectsLock sync.Mutex
	c.enqueueExperiment = func(obj interface{}) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
			panic(err)
		}
		enqueuedObjectsLock.Lock()
		defer enqueuedObjectsLock.Unlock()
		count, ok := f.enqueuedObjects[key]
		if !ok {
			count = 0
		}
		count++
		f.enqueuedObjects[key] = count
		c.experimentWorkqueue.Add(obj)
	}
	c.enqueueExperimentAfter = func(obj interface{}, duration time.Duration) {
		c.enqueueExperiment(obj)
	}

	for _, e := range f.experimentLister {
		i.Argoproj().V1alpha1().Experiments().Informer().GetIndexer().Add(e)
	}

	for _, r := range f.replicaSetLister {
		k8sI.Apps().V1().ReplicaSets().Informer().GetIndexer().Add(r)
	}

	for _, r := range f.serviceLister {
		k8sI.Core().V1().Services().Informer().GetIndexer().Add(r)
	}

	for _, r := range f.analysisRunLister {
		i.Argoproj().V1alpha1().AnalysisRuns().Informer().GetIndexer().Add(r)
	}

	for _, r := range f.analysisTemplateLister {
		i.Argoproj().V1alpha1().AnalysisTemplates().Informer().GetIndexer().Add(r)
	}

	for _, r := range f.clusterAnalysisTemplateLister {
		i.Argoproj().V1alpha1().ClusterAnalysisTemplates().Informer().GetIndexer().Add(r)
	}
	return c, i, k8sI
}

func (f *fixture) run(experimentName string) {
	c, i, k8sI := f.newController(noResyncPeriodFunc)
	f.runController(experimentName, true, false, c, i, k8sI)
}

func (f *fixture) runExpectError(experimentName string, startInformers bool) {
	c, i, k8sI := f.newController(noResyncPeriodFunc)
	f.runController(experimentName, startInformers, true, c, i, k8sI)
}

func (f *fixture) runController(experimentName string, startInformers bool, expectError bool, c *Controller, i informers.SharedInformerFactory, k8sI kubeinformers.SharedInformerFactory) *Controller {
	if startInformers {
		stopCh := make(chan struct{})
		defer close(stopCh)
		i.Start(stopCh)
		k8sI.Start(stopCh)

		assert.True(f.t, cache.WaitForCacheSync(stopCh, c.replicaSetSynced, c.experimentSynced, c.analysisRunSynced, c.analysisTemplateSynced, c.clusterAnalysisTemplateSynced))
	}

	err := c.syncHandler(context.Background(), experimentName)
	if !expectError && err != nil {
		f.t.Errorf("error syncing experiment: %v", err)
	} else if expectError && err == nil {
		f.t.Error("expected error syncing experiment, got nil")
	}

	actions := filterInformerActions(f.client.Actions())
	for i, action := range actions {
		if len(f.actions) < i+1 {
			actionsBytes, _ := json.Marshal(actions[i:])
			f.t.Errorf("%d unexpected actions: %+v", len(actions)-len(f.actions), string(actionsBytes))
			break
		}

		expectedAction := f.actions[i]
		checkAction(expectedAction, action, f.t)
	}

	if len(f.actions) > len(actions) {
		f.t.Errorf("%d expected actions did not occur:%+v", len(f.actions)-len(actions), f.actions[len(actions):])
	}

	k8sActions := filterInformerActions(f.kubeclient.Actions())
	for i, action := range k8sActions {
		if len(f.kubeactions) < i+1 {
			actionsBytes, _ := json.Marshal(k8sActions[i:])
			f.t.Errorf("%d unexpected actions: %+v", len(k8sActions)-len(f.kubeactions), string(actionsBytes))
			break
		}

		expectedAction := f.kubeactions[i]
		checkAction(expectedAction, action, f.t)
	}

	if len(f.kubeactions) > len(k8sActions) {
		f.t.Errorf("%d expected actions did not occur:%+v", len(f.kubeactions)-len(k8sActions), f.kubeactions[len(k8sActions):])
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
// noise level in our tests.
func filterInformerActions(actions []core.Action) []core.Action {
	ret := []core.Action{}
	for _, action := range actions {
		if action.Matches("list", "rollouts") ||
			action.Matches("watch", "rollouts") ||
			action.Matches("list", "replicaSets") ||
			action.Matches("watch", "replicaSets") ||
			action.Matches("list", "experiments") ||
			action.Matches("watch", "experiments") ||
			action.Matches("list", "analysistemplates") ||
			action.Matches("watch", "analysistemplates") ||
			action.Matches("list", "clusteranalysistemplates") ||
			action.Matches("watch", "clusteranalysistemplates") ||
			action.Matches("list", "analysisruns") ||
			action.Matches("watch", "analysisruns") ||
			action.Matches("watch", "services") ||
			action.Matches("list", "services") {
			continue
		}
		ret = append(ret, action)
	}

	return ret
}

func (f *fixture) expectCreateServiceAction(service *corev1.Service) int { //nolint:unused
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, core.NewCreateAction(schema.GroupVersionResource{Resource: "services"}, service.Namespace, service))
	return len
}

func (f *fixture) expectDeleteServiceAction(service *corev1.Service) int {
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, core.NewDeleteAction(schema.GroupVersionResource{Resource: "services"}, service.Namespace, service.Name))
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

func (f *fixture) expectGetExperimentAction(experiment *v1alpha1.Experiment) int { //nolint:unused
	len := len(f.actions)
	f.actions = append(f.actions, core.NewGetAction(schema.GroupVersionResource{Resource: "experiments"}, experiment.Namespace, experiment.Name))
	return len
}

func (f *fixture) expectUpdateExperimentAction(experiment *v1alpha1.Experiment) int { //nolint:unused
	action := core.NewUpdateAction(schema.GroupVersionResource{Resource: "experiments"}, experiment.Namespace, experiment)
	len := len(f.actions)
	f.actions = append(f.actions, action)
	return len
}

func (f *fixture) expectGetReplicaSetAction(r *appsv1.ReplicaSet) int {
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, core.NewGetAction(schema.GroupVersionResource{Resource: "replicasets"}, r.Namespace, r.Name))
	return len
}

func (f *fixture) expectPatchReplicaSetAction(r *appsv1.ReplicaSet) int {
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, core.NewPatchAction(schema.GroupVersionResource{Resource: "replicasets"}, r.Namespace, r.Name, types.MergePatchType, nil))
	return len
}

func (f *fixture) expectPatchExperimentAction(experiment *v1alpha1.Experiment) int {
	serviceSchema := schema.GroupVersionResource{
		Resource: "experiments",
		Version:  "v1alpha1",
	}
	len := len(f.actions)
	f.actions = append(f.actions, core.NewPatchAction(serviceSchema, experiment.Namespace, experiment.Name, types.MergePatchType, nil))
	return len
}

func (f *fixture) expectCreateAnalysisRunAction(r *v1alpha1.AnalysisRun) int {
	len := len(f.actions)
	f.actions = append(f.actions, core.NewCreateAction(schema.GroupVersionResource{Resource: "analysisruns"}, r.Namespace, r))
	return len
}

func (f *fixture) expectGetAnalysisRunAction(r *v1alpha1.AnalysisRun) int {
	len := len(f.actions)
	f.actions = append(f.actions, core.NewGetAction(schema.GroupVersionResource{Resource: "analysisruns"}, r.Namespace, r.Name))
	return len
}

func (f *fixture) expectPatchAnalysisRunAction(r *v1alpha1.AnalysisRun) int {
	len := len(f.actions)
	f.actions = append(f.actions, core.NewPatchAction(schema.GroupVersionResource{Resource: "analysisruns"}, r.Namespace, r.Name, types.MergePatchType, nil))
	return len
}

func (f *fixture) getCreatedAnalysisRun(index int) *v1alpha1.AnalysisRun {
	action := filterInformerActions(f.client.Actions())[index]
	createAction, ok := action.(core.CreateAction)
	if !ok {
		assert.Failf(f.t, "Expected Created action, not %s", action.GetVerb())
	}
	obj := createAction.GetObject()
	ar := &v1alpha1.AnalysisRun{}
	converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ := converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ar)
	return ar
}

func (f *fixture) getCreatedReplicaSet(index int) *appsv1.ReplicaSet {
	action := filterInformerActions(f.kubeclient.Actions())[index]
	createAction, ok := action.(core.CreateAction)
	if !ok {
		assert.Failf(f.t, "Expected Created action, not %s", action.GetVerb())
	}
	obj := createAction.GetObject()
	rs := &appsv1.ReplicaSet{}
	converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ := converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, rs)
	return rs
}

func (f *fixture) verifyPatchedReplicaSetAddScaleDownDelay(index int, scaleDownDelaySeconds int32) {
	action := filterInformerActions(f.kubeclient.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		assert.Fail(f.t, "Expected Patch action, not %s", action.GetVerb())
	}
	now := timeutil.Now().Add(time.Duration(scaleDownDelaySeconds) * time.Second).UTC().Format(time.RFC3339)
	patch := fmt.Sprintf(addScaleDownAtAnnotationsPatch, v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey, now)
	assert.Equal(f.t, string(patchAction.GetPatch()), patch)
}

func (f *fixture) verifyPatchedReplicaSetRemoveScaleDownDelayAnnotation(index int) {
	action := filterInformerActions(f.kubeclient.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		assert.Fail(f.t, "Expected Patch action, not %s", action.GetVerb())
	}
	patch := fmt.Sprintf(removeScaleDownAtAnnotationsPatch, v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey)
	assert.Equal(f.t, string(patchAction.GetPatch()), patch)
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

func (f *fixture) getUpdatedExperiment(index int) *v1alpha1.Experiment { //nolint:unused
	action := filterInformerActions(f.client.Actions())[index]
	updateAction, ok := action.(core.UpdateAction)
	if !ok {
		assert.Fail(f.t, "Expected Update action, not %s", action.GetVerb())
	}
	obj := updateAction.GetObject()
	experiment := &v1alpha1.Experiment{}
	converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ := converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, experiment)
	return experiment
}

func (f *fixture) getPatchedExperiment(index int) string {
	action := filterInformerActions(f.client.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	return string(patchAction.GetPatch())
}

func (f *fixture) getPatchedExperimentAsObj(index int) *v1alpha1.Experiment {
	action := filterInformerActions(f.client.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	var ex v1alpha1.Experiment
	err := json.Unmarshal(patchAction.GetPatch(), &ex)
	if err != nil {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	return &ex
}

func (f *fixture) getPatchedAnalysisRunAsObj(index int) *v1alpha1.AnalysisRun {
	action := filterInformerActions(f.client.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	var run v1alpha1.AnalysisRun
	err := json.Unmarshal(patchAction.GetPatch(), &run)
	if err != nil {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	return &run
}

func TestNoReconcileForDeletedExperiment(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	e := newExperiment("foo", nil, "10s")
	now := timeutil.MetaNow()
	e.DeletionTimestamp = &now

	f.experimentLister = append(f.experimentLister, e)
	f.objects = append(f.objects, e)

	f.run(getKey(e, t))
}

type availableAtResults string

const (
	Set      availableAtResults = "Set"
	Nulled   availableAtResults = "NulledOut"
	NoChange availableAtResults = "NoChange"
)

func validatePatch(t *testing.T, patch string, statusCode v1alpha1.AnalysisPhase, availableAt availableAtResults, templateStatuses []v1alpha1.TemplateStatus, conditions []v1alpha1.ExperimentCondition) {
	e := v1alpha1.Experiment{}
	err := json.Unmarshal([]byte(patch), &e)
	if err != nil {
		panic(err)
	}
	actualStatus := e.Status
	if availableAt == Set {
		assert.NotNil(t, actualStatus.AvailableAt)
	} else if availableAt == Nulled {
		assert.Contains(t, patch, `"availableAt": null`)
	} else if availableAt == NoChange {
		assert.Nil(t, actualStatus.AvailableAt)
	}
	assert.Equal(t, statusCode, e.Status.Phase)
	assert.Len(t, actualStatus.TemplateStatuses, len(templateStatuses))
	for i := range templateStatuses {
		assert.Contains(t, actualStatus.TemplateStatuses, templateStatuses[i])
	}
	assert.Len(t, actualStatus.Conditions, len(conditions))
	for i := range conditions {
		expectedCond := conditions[i]
		for j := range actualStatus.Conditions {
			hasComparedConditions := false
			actualCond := conditions[j]
			if actualCond.Type == expectedCond.Type {
				assert.Equal(t, expectedCond.Status, actualCond.Status)
				assert.Equal(t, expectedCond.LastUpdateTime, actualCond.LastUpdateTime)
				assert.Equal(t, expectedCond.LastTransitionTime, actualCond.LastTransitionTime)
				assert.Equal(t, expectedCond.Reason, actualCond.Reason)
				assert.Equal(t, expectedCond.Message, actualCond.Message)
				hasComparedConditions = true
			}
			assert.True(t, hasComparedConditions)
		}
	}
}

func TestAddInvalidSpec(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, "")
	e.Spec.Templates[0].Name = ""

	f := newFixture(t, e)
	defer f.Close()

	patchIndex := f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))
	patch := f.getPatchedExperiment(patchIndex)

	cond := newCondition(conditions.InvalidSpecReason, e)

	expectedPatch := calculatePatch(e, `{
		"status":{
		}
	}`, nil, cond)
	assert.JSONEq(t, expectedPatch, patch)
}

func TestKeepInvalidSpec(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, "")
	e.Status.Conditions = []v1alpha1.ExperimentCondition{{
		Type:    v1alpha1.InvalidExperimentSpec,
		Status:  corev1.ConditionTrue,
		Reason:  conditions.InvalidSpecReason,
		Message: fmt.Sprintf(conditions.ExperimentTemplateNameEmpty, e.Name, 0),
	}}
	e.Spec.Templates[0].Name = ""

	f := newFixture(t, e)
	defer f.Close()

	f.run(getKey(e, t))

}

func TestUpdateInvalidSpec(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, "")

	e.Status.Conditions = []v1alpha1.ExperimentCondition{{
		Type:    v1alpha1.InvalidExperimentSpec,
		Status:  corev1.ConditionTrue,
		Reason:  conditions.InvalidSpecReason,
		Message: conditions.ExperimentSelectAllMessage,
	}}

	e.Spec.Templates[0].Name = ""

	f := newFixture(t, e)
	defer f.Close()

	patchIndex := f.expectPatchExperimentAction(e)
	f.run(getKey(e, t))
	patch := f.getPatchedExperiment(patchIndex)

	cond := newCondition(conditions.InvalidSpecReason, e)

	expectedPatch := calculatePatch(e, `{
		"status":{
		}
	}`, nil, cond)
	assert.JSONEq(t, expectedPatch, patch)

}

func TestRemoveInvalidSpec(t *testing.T) {
	templates := generateTemplates("bar", "baz")
	e := newExperiment("foo", templates, "")

	e.Status.Conditions = []v1alpha1.ExperimentCondition{{
		Type:   v1alpha1.InvalidExperimentSpec,
		Status: corev1.ConditionTrue,
		Reason: conditions.InvalidSpecReason,
	}}

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

func TestRun(t *testing.T) {
	f := newFixture(t, nil)
	defer f.Close()
	// make sure we can start and top the controller
	c, _, _ := f.newController(noResyncPeriodFunc)
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	go func() {
		time.Sleep(1000 * time.Millisecond)
		c.experimentWorkqueue.ShutDownWithDrain()
		cancel()
	}()
	c.Run(ctx, 1)
}
