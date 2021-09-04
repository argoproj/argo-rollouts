package rollout

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/undefinedlabs/go-mpatch"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	corev1defaults "k8s.io/kubernetes/pkg/apis/core/v1"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/validation"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"github.com/argoproj/argo-rollouts/rollout/mocks"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	"github.com/argoproj/argo-rollouts/utils/queue"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
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

type FakeWorkloadRefResolver struct {
}

func (f *FakeWorkloadRefResolver) Resolve(_ *v1alpha1.Rollout) error {
	return nil
}

func (f *FakeWorkloadRefResolver) Init() error {
	return nil
}

type fixture struct {
	t *testing.T

	client     *fake.Clientset
	kubeclient *k8sfake.Clientset
	// Objects to put in the store.
	rolloutLister                 []*v1alpha1.Rollout
	experimentLister              []*v1alpha1.Experiment
	analysisRunLister             []*v1alpha1.AnalysisRun
	clusterAnalysisTemplateLister []*v1alpha1.ClusterAnalysisTemplate
	analysisTemplateLister        []*v1alpha1.AnalysisTemplate
	replicaSetLister              []*appsv1.ReplicaSet
	serviceLister                 []*corev1.Service
	ingressLister                 []*extensionsv1beta1.Ingress
	// Actions expected to happen on the client.
	kubeactions []core.Action
	actions     []core.Action
	// Objects from here preloaded into NewSimpleFake.
	kubeobjects     []runtime.Object
	objects         []runtime.Object
	enqueuedObjects map[string]int
	unfreezeTime    func() error

	// events holds all the K8s Event Reasons emitted during the run
	events             []string
	fakeTrafficRouting *[]mocks.TrafficRoutingReconciler
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{}
	f.t = t
	f.objects = []runtime.Object{}
	f.kubeobjects = []runtime.Object{}
	f.enqueuedObjects = make(map[string]int)
	now := time.Now()
	patch, err := mpatch.PatchMethod(time.Now, func() time.Time { return now })
	assert.NoError(t, err)
	f.unfreezeTime = patch.Unpatch
	f.fakeTrafficRouting = newFakeTrafficRoutingReconciler()
	return f
}

func (f *fixture) Close() {
	f.unfreezeTime()
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
			Generation: 123,
		},
		Spec: v1alpha1.RolloutSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: selector,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container-name",
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
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, ro, "")
	conditions.SetRolloutCondition(&ro.Status, progressingCondition)
	return ro
}

func newReplicaSetWithStatus(r *v1alpha1.Rollout, replicas int, availableReplicas int) *appsv1.ReplicaSet {
	rs := newReplicaSet(r, replicas)
	rs.Status.Replicas = int32(replicas)
	rs.Status.AvailableReplicas = int32(availableReplicas)
	rs.Status.ReadyReplicas = int32(availableReplicas)
	return rs
}

func newPausedCondition(isPaused bool) (v1alpha1.RolloutCondition, string) {
	status := corev1.ConditionTrue
	if !isPaused {
		status = corev1.ConditionFalse
	}
	condition := v1alpha1.RolloutCondition{
		LastTransitionTime: metav1.Now(),
		LastUpdateTime:     metav1.Now(),
		Message:            conditions.RolloutPausedMessage,
		Reason:             conditions.RolloutPausedReason,
		Status:             status,
		Type:               v1alpha1.RolloutPaused,
	}
	conditionBytes, err := json.Marshal(condition)
	if err != nil {
		panic(err)
	}
	return condition, string(conditionBytes)
}

func newCompletedCondition(isCompleted bool) (v1alpha1.RolloutCondition, string) {
	status := corev1.ConditionTrue
	if !isCompleted {
		status = corev1.ConditionFalse
	}
	condition := v1alpha1.RolloutCondition{
		LastTransitionTime: metav1.Now(),
		LastUpdateTime:     metav1.Now(),
		Message:            conditions.RolloutCompletedReason,
		Reason:             conditions.RolloutCompletedReason,
		Status:             status,
		Type:               v1alpha1.RolloutCompleted,
	}
	conditionBytes, err := json.Marshal(condition)
	if err != nil {
		panic(err)
	}
	return condition, string(conditionBytes)
}

func newProgressingCondition(reason string, resourceObj runtime.Object, optionalMessage string) (v1alpha1.RolloutCondition, string) {
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
		if reason == conditions.RolloutAbortedReason {
			rev, _ := replicasetutil.Revision(resourceObj)
			msg = fmt.Sprintf(conditions.RolloutAbortedMessage, rev)
			status = corev1.ConditionFalse
		}
		if reason == conditions.RolloutExperimentFailedReason {
			// rollout-experiment-step-5668c9d57b-2-1
			exName := fmt.Sprintf("%s-%s-%s-%s", resource.Name, resource.Status.CurrentPodHash, "2", "0")
			msg = fmt.Sprintf(conditions.RolloutExperimentFailedMessage, exName, resource.Name)
			status = corev1.ConditionFalse
		}
		if reason == conditions.RolloutAnalysisRunFailedReason {
			// rollout-analysis-step-58bfdcfddd-4-random-fail
			atName := ""
			if resource.Spec.Strategy.Canary.Analysis != nil {
				atName = resource.Spec.Strategy.Canary.Analysis.Templates[0].TemplateName
			} else if resource.Spec.Strategy.Canary.Steps != nil && resource.Status.CurrentStepIndex != nil {
				atName = resource.Spec.Strategy.Canary.Steps[*resource.Status.CurrentStepIndex].Analysis.Templates[0].TemplateName
			}
			arName := fmt.Sprintf("%s-%s-%s-%s", resource.Name, resource.Status.CurrentPodHash, "10", atName)
			msg = fmt.Sprintf(conditions.RolloutAnalysisRunFailedMessage, arName, resource.Name)
			status = corev1.ConditionFalse
		}
		if reason == conditions.RolloutRetryReason {
			msg = conditions.RolloutRetryMessage
			status = corev1.ConditionUnknown
		}
	}

	if reason == conditions.RolloutPausedReason {
		msg = conditions.RolloutPausedMessage
		status = corev1.ConditionUnknown
	}
	if reason == conditions.RolloutResumedReason {
		msg = conditions.RolloutResumedMessage
		status = corev1.ConditionUnknown
	}

	if optionalMessage != "" {
		msg = optionalMessage
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

func generateConditionsPatch(available bool, progressingReason string, progressingResource runtime.Object, availableConditionFirst bool, progressingMessage string) string {
	_, availableCondition := newAvailableCondition(available)
	_, progressingCondition := newProgressingCondition(progressingReason, progressingResource, progressingMessage)
	if availableConditionFirst {
		return fmt.Sprintf("[%s, %s]", availableCondition, progressingCondition)
	}
	return fmt.Sprintf("[%s, %s]", progressingCondition, availableCondition)
}

func generateConditionsPatchWithPause(available bool, progressingReason string, progressingResource runtime.Object, availableConditionFirst bool, progressingMessage string, isPaused bool) string {
	_, availableCondition := newAvailableCondition(available)
	_, progressingCondition := newProgressingCondition(progressingReason, progressingResource, progressingMessage)
	_, pauseCondition := newPausedCondition(isPaused)
	if availableConditionFirst {
		return fmt.Sprintf("[%s, %s, %s]", availableCondition, progressingCondition, pauseCondition)
	}
	return fmt.Sprintf("[%s, %s, %s]", progressingCondition, pauseCondition, availableCondition)
}

func generateConditionsPatchWithComplete(available bool, progressingReason string, progressingResource runtime.Object, availableConditionFirst bool, progressingMessage string, isCompleted bool) string {
	_, availableCondition := newAvailableCondition(available)
	_, progressingCondition := newProgressingCondition(progressingReason, progressingResource, progressingMessage)
	_, completeCondition := newCompletedCondition(isCompleted)
	if availableConditionFirst {
		return fmt.Sprintf("[%s, %s, %s]", availableCondition, completeCondition, progressingCondition)
	}
	return fmt.Sprintf("[%s, %s, %s]", completeCondition, progressingCondition, availableCondition)
}

func updateConditionsPatch(r v1alpha1.Rollout, newCondition v1alpha1.RolloutCondition) string {
	conditions.SetRolloutCondition(&r.Status, newCondition)
	conditionsBytes, _ := json.Marshal(r.Status.Conditions)
	return string(conditionsBytes)
}

// func updateBlueGreenRolloutStatus(r *v1alpha1.Rollout, preview, active string, availableReplicas, updatedReplicas, hpaReplicas int32, pause bool, available bool, progressingStatus string) *v1alpha1.Rollout {
func updateBlueGreenRolloutStatus(r *v1alpha1.Rollout, preview, active, stable string, availableReplicas, updatedReplicas, totalReplicas, hpaReplicas int32, pause bool, available bool) *v1alpha1.Rollout {
	newRollout := updateBaseRolloutStatus(r, availableReplicas, updatedReplicas, totalReplicas, hpaReplicas)
	selector := newRollout.Spec.Selector.DeepCopy()
	if active != "" {
		selector.MatchLabels[v1alpha1.DefaultRolloutUniqueLabelKey] = active
	}
	newRollout.Status.Selector = metav1.FormatLabelSelector(selector)
	newRollout.Status.BlueGreen.ActiveSelector = active
	newRollout.Status.BlueGreen.PreviewSelector = preview
	newRollout.Status.StableRS = stable
	cond, _ := newAvailableCondition(available)
	newRollout.Status.Conditions = append(newRollout.Status.Conditions, cond)
	if pause {
		now := metav1.Now()
		cond := v1alpha1.PauseCondition{
			Reason:    v1alpha1.PauseReasonBlueGreenPause,
			StartTime: now,
		}
		newRollout.Status.ControllerPause = true
		newRollout.Status.PauseConditions = append(newRollout.Status.PauseConditions, cond)
	}
	newRollout.Status.Phase, newRollout.Status.Message = rolloututil.CalculateRolloutPhase(r.Spec, newRollout.Status)
	return newRollout
}
func updateCanaryRolloutStatus(r *v1alpha1.Rollout, stableRS string, availableReplicas, updatedReplicas, hpaReplicas int32, pause bool) *v1alpha1.Rollout {
	newRollout := updateBaseRolloutStatus(r, availableReplicas, updatedReplicas, availableReplicas, hpaReplicas)
	newRollout.Status.StableRS = stableRS
	if pause {
		now := metav1.Now()
		cond := v1alpha1.PauseCondition{
			Reason:    v1alpha1.PauseReasonCanaryPauseStep,
			StartTime: now,
		}
		newRollout.Status.ControllerPause = true
		newRollout.Status.PauseConditions = append(newRollout.Status.PauseConditions, cond)
	}
	newRollout.Status.Phase, newRollout.Status.Message = rolloututil.CalculateRolloutPhase(r.Spec, newRollout.Status)
	return newRollout
}

func updateBaseRolloutStatus(r *v1alpha1.Rollout, availableReplicas, updatedReplicas, totalReplicas, hpaReplicas int32) *v1alpha1.Rollout {
	newRollout := r.DeepCopy()
	newRollout.Status.Replicas = totalReplicas
	newRollout.Status.AvailableReplicas = availableReplicas
	newRollout.Status.ReadyReplicas = availableReplicas
	newRollout.Status.UpdatedReplicas = updatedReplicas
	newRollout.Status.HPAReplicas = hpaReplicas
	return newRollout
}

func newReplicaSet(r *v1alpha1.Rollout, replicas int) *appsv1.ReplicaSet {
	podHash := controller.ComputeHash(&r.Spec.Template, r.Status.CollisionCount)
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
		fmt.Println(patch)
		panic(err)
	}
	newRO := &v1alpha1.Rollout{}
	json.Unmarshal(newBytes, newRO)
	newObservedGen := strconv.Itoa(int(newRO.Generation))

	newPatch := make(map[string]interface{})
	err = json.Unmarshal([]byte(patch), &newPatch)
	if err != nil {
		panic(err)
	}
	newStatus := newPatch["status"].(map[string]interface{})
	newStatus["observedGeneration"] = newObservedGen
	newPatch["status"] = newStatus
	newPatchBytes, _ := json.Marshal(newPatch)
	return string(newPatchBytes)
}

func cleanPatch(expectedPatch string) string {
	patch := make(map[string]interface{})
	err := json.Unmarshal([]byte(expectedPatch), &patch)
	if err != nil {
		panic(err)
	}
	patchStr, err := json.Marshal(patch)
	if err != nil {
		panic(err)
	}
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

func (f *fixture) newController(resync resyncFunc) (*Controller, informers.SharedInformerFactory, kubeinformers.SharedInformerFactory) {
	f.client = fake.NewSimpleClientset(f.objects...)
	f.kubeclient = k8sfake.NewSimpleClientset(f.kubeobjects...)

	i := informers.NewSharedInformerFactory(f.client, resync())
	k8sI := kubeinformers.NewSharedInformerFactory(f.kubeclient, resync())

	// Pass in objects to to dynamicClient
	scheme := runtime.NewScheme()
	v1alpha1.AddToScheme(scheme)
	tgbGVR := schema.GroupVersionResource{
		Group:    "elbv2.k8s.aws",
		Version:  "v1beta1",
		Resource: "targetgroupbindings",
	}
	listMapping := map[schema.GroupVersionResource]string{
		tgbGVR: "TargetGroupBindingList",
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listMapping, f.objects...)
	dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)
	istioVirtualServiceInformer := dynamicInformerFactory.ForResource(istioutil.GetIstioVirtualServiceGVR()).Informer()
	istioDestinationRuleInformer := dynamicInformerFactory.ForResource(istioutil.GetIstioDestinationRuleGVR()).Informer()

	rolloutWorkqueue := workqueue.NewNamedRateLimitingQueue(workqueue.NewItemExponentialFailureRateLimiter(time.Millisecond, 10*time.Second), "Rollouts")
	serviceWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Services")
	ingressWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Ingresses")

	metricsServer := metrics.NewMetricsServer(metrics.ServerConfig{
		Addr:               "localhost:8080",
		K8SRequestProvider: &metrics.K8sRequestsCountProvider{},
	})

	c := NewController(ControllerConfig{
		Namespace:                       metav1.NamespaceAll,
		KubeClientSet:                   f.kubeclient,
		ArgoProjClientset:               f.client,
		DynamicClientSet:                dynamicClient,
		ExperimentInformer:              i.Argoproj().V1alpha1().Experiments(),
		AnalysisRunInformer:             i.Argoproj().V1alpha1().AnalysisRuns(),
		AnalysisTemplateInformer:        i.Argoproj().V1alpha1().AnalysisTemplates(),
		ClusterAnalysisTemplateInformer: i.Argoproj().V1alpha1().ClusterAnalysisTemplates(),
		ReplicaSetInformer:              k8sI.Apps().V1().ReplicaSets(),
		ServicesInformer:                k8sI.Core().V1().Services(),
		IngressInformer:                 k8sI.Extensions().V1beta1().Ingresses(),
		RolloutsInformer:                i.Argoproj().V1alpha1().Rollouts(),
		IstioPrimaryDynamicClient:       dynamicClient,
		IstioVirtualServiceInformer:     istioVirtualServiceInformer,
		IstioDestinationRuleInformer:    istioDestinationRuleInformer,
		ResyncPeriod:                    resync(),
		RolloutWorkQueue:                rolloutWorkqueue,
		ServiceWorkQueue:                serviceWorkqueue,
		IngressWorkQueue:                ingressWorkqueue,
		MetricsServer:                   metricsServer,
		Recorder:                        record.NewFakeEventRecorder(),
		RefResolver:                     &FakeWorkloadRefResolver{},
	})

	var enqueuedObjectsLock sync.Mutex
	c.enqueueRollout = func(obj interface{}) {
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
	for _, i := range f.ingressLister {
		k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	}
	for _, at := range f.analysisTemplateLister {
		i.Argoproj().V1alpha1().AnalysisTemplates().Informer().GetIndexer().Add(at)
	}
	for _, cat := range f.clusterAnalysisTemplateLister {
		i.Argoproj().V1alpha1().ClusterAnalysisTemplates().Informer().GetIndexer().Add(cat)
	}
	for _, ar := range f.analysisRunLister {
		i.Argoproj().V1alpha1().AnalysisRuns().Informer().GetIndexer().Add(ar)
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
			actionsBytes, _ := json.MarshalIndent(actions[i:], "", "  ")
			f.t.Errorf("%d unexpected actions: %+v", len(actions)-len(f.actions), string(actionsBytes))
			break
		}

		expectedAction := f.actions[i]
		checkAction(expectedAction, action, f.t)
	}

	if len(f.actions) > len(actions) {
		f.t.Errorf("%d expected actions did not happen:%+v", len(f.actions)-len(actions), f.actions[len(actions):])
	}

	k8sActions := filterInformerActions(f.kubeclient.Actions())
	for i, action := range k8sActions {
		if len(f.kubeactions) < i+1 {
			actionsBytes, _ := json.MarshalIndent(k8sActions[i:], "", "  ")
			f.t.Errorf("%d unexpected actions: %+v", len(k8sActions)-len(f.kubeactions), string(actionsBytes))
			break
		}

		expectedAction := f.kubeactions[i]
		checkAction(expectedAction, action, f.t)
	}

	if len(f.kubeactions) > len(k8sActions) {
		f.t.Errorf("%d expected actions did not happen:%+v", len(f.kubeactions)-len(k8sActions), f.kubeactions[len(k8sActions):])
	}
	fakeRecorder := c.recorder.(*record.FakeEventRecorder)
	f.events = fakeRecorder.Events
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
		if action.Matches("list", "experiments") ||
			action.Matches("watch", "experiments") ||
			action.Matches("list", "analysisruns") ||
			action.Matches("watch", "analysisruns") ||
			action.Matches("list", "analysistemplates") ||
			action.Matches("watch", "analysistemplates") ||
			action.Matches("list", "clusteranalysistemplates") ||
			action.Matches("watch", "clusteranalysistemplates") ||
			action.Matches("list", "rollouts") ||
			action.Matches("watch", "rollouts") ||
			action.Matches("list", "replicaSets") ||
			action.Matches("watch", "replicaSets") ||
			action.Matches("list", "services") ||
			action.Matches("watch", "services") ||
			action.Matches("list", "ingresses") ||
			action.Matches("watch", "ingresses") {
			continue
		}
		ret = append(ret, action)
	}

	return ret
}

func (f *fixture) expectPatchServiceAction(s *corev1.Service, newLabel string) int {
	patch := fmt.Sprintf(switchSelectorPatch, newLabel)
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

func (f *fixture) expectUpdatePodAction(p *corev1.Pod) int {
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, core.NewUpdateAction(schema.GroupVersionResource{Resource: "pods"}, p.Namespace, p))
	return len
}

func (f *fixture) expectListPodAction(namespace string) int {
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, core.NewListAction(schema.GroupVersionResource{Resource: "pods"}, schema.GroupVersionKind{Kind: "Pod", Version: "v1"}, namespace, metav1.ListOptions{}))
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

func (f *fixture) expectGetAnalysisRunAction(ar *v1alpha1.AnalysisRun) int {
	len := len(f.actions)
	f.actions = append(f.actions, core.NewGetAction(schema.GroupVersionResource{Resource: "analysisruns"}, ar.Namespace, ar.Name))
	return len
}

func (f *fixture) expectGetExperimentAction(ex *v1alpha1.Experiment) int {
	len := len(f.actions)
	f.actions = append(f.actions, core.NewGetAction(schema.GroupVersionResource{Resource: "experiments"}, ex.Namespace, ex.Name))
	return len
}

func (f *fixture) expectUpdateRolloutAction(rollout *v1alpha1.Rollout) int {
	action := core.NewUpdateAction(schema.GroupVersionResource{Resource: "rollouts"}, rollout.Namespace, rollout)
	len := len(f.actions)
	f.actions = append(f.actions, action)
	return len
}

func (f *fixture) expectUpdateRolloutStatusAction(rollout *v1alpha1.Rollout) int {
	action := core.NewUpdateSubresourceAction(schema.GroupVersionResource{Resource: "rollouts"}, "status", rollout.Namespace, rollout)
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
	f.actions = append(f.actions, core.NewPatchSubresourceAction(serviceSchema, rollout.Namespace, rollout.Name, types.MergePatchType, nil, "status"))
	return len
}

func (f *fixture) expectPatchRolloutActionWithPatch(rollout *v1alpha1.Rollout, patch string) int {
	expectedPatch := calculatePatch(rollout, patch)
	serviceSchema := schema.GroupVersionResource{
		Resource: "rollouts",
		Version:  "v1alpha1",
	}
	len := len(f.actions)
	f.actions = append(f.actions, core.NewPatchSubresourceAction(serviceSchema, rollout.Namespace, rollout.Name, types.MergePatchType, []byte(expectedPatch), "status"))
	return len
}

func (f *fixture) expectGetEndpointsAction(ep *corev1.Endpoints) int {
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, core.NewGetAction(schema.GroupVersionResource{Resource: "endpoints"}, ep.Namespace, ep.Name))
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

func (f *fixture) verifyPatchedReplicaSet(index int, scaleDownDelaySeconds int32) {
	action := filterInformerActions(f.kubeclient.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		assert.Fail(f.t, "Expected Patch action, not %s", action.GetVerb())
	}
	now := metav1.Now().Add(time.Duration(scaleDownDelaySeconds) * time.Second).UTC().Format(time.RFC3339)
	patch := fmt.Sprintf(addScaleDownAtAnnotationsPatch, v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey, now)
	assert.Equal(f.t, string(patchAction.GetPatch()), patch)
}

func (f *fixture) verifyPatchedService(index int, newPodHash string, managedBy string) {
	action := filterInformerActions(f.kubeclient.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		assert.Fail(f.t, "Expected Patch action, not %s", action.GetVerb())
	}
	patch := fmt.Sprintf(switchSelectorPatch, newPodHash)
	if managedBy != "" {
		patch = fmt.Sprintf(switchSelectorAndAddManagedByPatch, managedBy, newPodHash)
	}
	assert.Equal(f.t, patch, string(patchAction.GetPatch()))
}

func (f *fixture) verifyPatchedRolloutAborted(index int, rsName string) {
	action := filterInformerActions(f.kubeclient.Actions())[index]
	_, ok := action.(core.PatchAction)
	if !ok {
		assert.Fail(f.t, "Expected Patch action, not %s", action.GetVerb())
	}

	ro := f.getPatchedRolloutAsObject(index)
	assert.NotNil(f.t, ro)
	assert.True(f.t, ro.Status.Abort)
	assert.Equal(f.t, v1alpha1.RolloutPhaseDegraded, ro.Status.Phase)
	expectedMsg := fmt.Sprintf("ProgressDeadlineExceeded: ReplicaSet %q has timed out progressing.", rsName)
	assert.Equal(f.t, expectedMsg, ro.Status.Message)
}

func (f *fixture) verifyPatchedAnalysisRun(index int, ar *v1alpha1.AnalysisRun) bool {
	action := filterInformerActions(f.client.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		assert.Fail(f.t, "Expected Patch action, not %s", action.GetVerb())
	}
	return ar.Name == patchAction.GetName() && string(patchAction.GetPatch()) == cancelAnalysisRun
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

func (f *fixture) getCreatedExperiment(index int) *v1alpha1.Experiment {
	action := filterInformerActions(f.client.Actions())[index]
	createAction, ok := action.(core.CreateAction)
	if !ok {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	obj := createAction.GetObject()
	ex := &v1alpha1.Experiment{}
	converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ := converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ex)
	return ex
}

func (f *fixture) getPatchedExperiment(index int) *v1alpha1.Experiment {
	action := filterInformerActions(f.client.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	e := v1alpha1.Experiment{}
	log.Infof("patch: %s", patchAction.GetPatch())
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

func (f *fixture) getPatchedRolloutWithoutConditions(index int) string {
	action := filterInformerActions(f.client.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	ro := make(map[string]interface{})
	err := json.Unmarshal(patchAction.GetPatch(), &ro)
	if err != nil {
		f.t.Fatalf("Unable to unmarshal Patch")
	}
	unstructured.RemoveNestedField(ro, "status", "conditions")
	roBytes, err := json.Marshal(ro)
	if err != nil {
		f.t.Fatalf("Unable to marshal Patch")
	}
	return string(roBytes)
}

func (f *fixture) getPatchedRolloutAsObject(index int) *v1alpha1.Rollout {
	action := filterInformerActions(f.client.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	ro := v1alpha1.Rollout{}
	err := json.Unmarshal(patchAction.GetPatch(), &ro)
	if err != nil {
		panic(err)
	}
	return &ro
}

func (f *fixture) expectDeleteAnalysisRunAction(ar *v1alpha1.AnalysisRun) int {
	action := core.NewDeleteAction(schema.GroupVersionResource{Resource: "analysisruns"}, ar.Namespace, ar.Name)
	len := len(f.actions)
	f.actions = append(f.actions, action)
	return len
}

func (f *fixture) expectDeleteExperimentAction(ex *v1alpha1.Experiment) int {
	action := core.NewDeleteAction(schema.GroupVersionResource{Resource: "experiments"}, ex.Namespace, ex.Name)
	len := len(f.actions)
	f.actions = append(f.actions, action)
	return len
}

func (f *fixture) expectDeleteReplicaSetAction(rs *appsv1.ReplicaSet) int {
	action := core.NewDeleteAction(schema.GroupVersionResource{Resource: "replicasets"}, rs.Namespace, rs.Name)
	len := len(f.kubeactions)
	f.kubeactions = append(f.kubeactions, action)
	return len
}

func (f *fixture) getDeletedReplicaSet(index int) string {
	action := filterInformerActions(f.kubeclient.Actions())[index]
	deleteAction, ok := action.(core.DeleteAction)
	if !ok {
		assert.Fail(f.t, "Expected Delete action, not %s", action.GetVerb())
	}
	return deleteAction.GetName()
}

func (f *fixture) getDeletedAnalysisRun(index int) string {
	action := filterInformerActions(f.client.Actions())[index]
	deleteAction, ok := action.(core.DeleteAction)
	if !ok {
		assert.Fail(f.t, "Expected Delete action, not %s", action.GetVerb())
	}
	return deleteAction.GetName()
}

func (f *fixture) getDeletedExperiment(index int) string {
	action := filterInformerActions(f.client.Actions())[index]
	deleteAction, ok := action.(core.DeleteAction)
	if !ok {
		assert.Fail(f.t, "Expected Delete action, not %s", action.GetVerb())
	}
	return deleteAction.GetName()
}

func (f *fixture) getUpdatedPod(index int) *corev1.Pod {
	action := filterInformerActions(f.kubeclient.Actions())[index]
	updateAction, ok := action.(core.UpdateAction)
	if !ok {
		assert.Fail(f.t, "Expected Update action, not %s", action.GetVerb())
	}
	obj := updateAction.GetObject()
	pod := &corev1.Pod{}
	converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ := converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, pod)
	return pod
}

func (f *fixture) assertEvents(events []string) {
	f.t.Helper()
	assert.Equal(f.t, events, f.events)
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
	previewSvc := newService("preview", 80, nil, r)
	activeSvc := newService("active", 80, nil, r)

	rs := newReplicaSet(r, 1)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)
	f.serviceLister = append(f.serviceLister, previewSvc, activeSvc)

	updatedRolloutIndex := f.expectUpdateRolloutStatusAction(r) // update rollout progressing condition
	f.expectPatchServiceAction(previewSvc, "")
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	updatedRollout := f.getUpdatedRollout(updatedRolloutIndex)
	progressingCondition := conditions.GetRolloutCondition(updatedRollout.Status, v1alpha1.RolloutProgressing)
	assert.NotNil(t, progressingCondition)
	assert.Equal(t, fmt.Sprintf(conditions.FoundNewRSMessage, rs.Name), progressingCondition.Message)
	assert.Equal(t, conditions.FoundNewRSReason, progressingCondition.Reason)
}

func TestRequeueStuckRollout(t *testing.T) {
	rollout := func(progressingConditionReason string, rolloutCompleted bool, rolloutPaused bool, progressDeadlineSeconds *int32) *v1alpha1.Rollout {
		r := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Replicas:                pointer.Int32Ptr(0),
				ProgressDeadlineSeconds: progressDeadlineSeconds,
			},
		}
		r.Generation = 123
		if rolloutPaused {
			r.Status.PauseConditions = []v1alpha1.PauseCondition{{
				Reason: v1alpha1.PauseReasonBlueGreenPause,
			}}
		}
		if rolloutCompleted {
			r.Status.ObservedGeneration = strconv.Itoa(int(r.Generation))
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
			roCtx, err := c.newRolloutContext(test.rollout)
			assert.NoError(t, err)
			duration := roCtx.requeueStuckRollout(test.rollout.Status)
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
	defer f.Close()
	r := newCanaryRollout("foo", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
	r.Spec.Replicas = nil
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	updateIndex := f.expectUpdateRolloutAction(r)
	f.run(getKey(r, t))
	updatedRollout := f.getUpdatedRollout(updateIndex)
	assert.Equal(t, defaults.DefaultReplicas, *updatedRollout.Spec.Replicas)
}

// TestSwitchInvalidSpecMessage verifies message is updated when reason for InvalidSpec changes
func TestSwitchInvalidSpecMessage(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "", "")
	r.Spec.Selector = &metav1.LabelSelector{}
	cond := conditions.NewRolloutCondition(v1alpha1.InvalidSpec, corev1.ConditionTrue, conditions.InvalidSpecReason, conditions.RolloutSelectAllMessage)
	conditions.SetRolloutCondition(&r.Status, *cond)
	r.Status.Phase, r.Status.Message = rolloututil.CalculateRolloutPhase(r.Spec, r.Status)

	r.Spec.Selector = nil
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	expectedPatchWithoutSub := `{
		"status": {
			"conditions": [%s,%s],
			"message": "%s: %s"
		}
	}`
	errmsg := "The Rollout \"foo\" is invalid: spec.selector: Required value: Rollout has missing field '.spec.selector'"
	_, progressingCond := newProgressingCondition(conditions.ReplicaSetUpdatedReason, r, "")
	invalidSpecCond := conditions.NewRolloutCondition(v1alpha1.InvalidSpec, corev1.ConditionTrue, conditions.InvalidSpecReason, errmsg)
	invalidSpecBytes, _ := json.Marshal(invalidSpecCond)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutSub, progressingCond, string(invalidSpecBytes), conditions.InvalidSpecReason, strings.ReplaceAll(errmsg, "\"", "\\\""))

	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, calculatePatch(r, expectedPatch), patch)
}

// TestPodTemplateHashEquivalence verifies the hash is computed consistently when there are slight
// variations made to the pod template in equivalent ways.
func TestPodTemplateHashEquivalence(t *testing.T) {
	var err error
	// NOTE: This test will fail on every k8s library upgrade.
	// To fix it, update expectedReplicaSetName to match the new hash.
	expectedReplicaSetName := "guestbook-586d86c77b"

	r1 := newBlueGreenRollout("guestbook", 1, nil, "active", "")
	r1Resources := `
limits:
  cpu: 2000m
  memory: 8192M
requests:
  cpu: 150m
  memory: 8192M
`
	err = yaml.Unmarshal([]byte(r1Resources), &r1.Spec.Template.Spec.Containers[0].Resources)
	assert.NoError(t, err)

	r2 := newBlueGreenRollout("guestbook", 1, nil, "active", "")
	r2Resources := `
  limits:
    cpu: '2'
    memory: 8192M
  requests:
    cpu: 0.15
    memory: 8192M
`
	err = yaml.Unmarshal([]byte(r2Resources), &r2.Spec.Template.Spec.Containers[0].Resources)
	assert.NoError(t, err)

	for _, r := range []*v1alpha1.Rollout{r1, r2} {
		f := newFixture(t)
		activeSvc := newService("active", 80, nil, r)
		f.kubeobjects = append(f.kubeobjects, activeSvc)
		f.rolloutLister = append(f.rolloutLister, r)
		f.serviceLister = append(f.serviceLister, activeSvc)
		f.objects = append(f.objects, r)

		f.expectUpdateRolloutStatusAction(r)
		f.expectPatchRolloutAction(r)
		rs := newReplicaSet(r, 1)
		rsIdx := f.expectCreateReplicaSetAction(rs)
		f.run(getKey(r, t))
		rs = f.getCreatedReplicaSet(rsIdx)
		assert.Equal(t, expectedReplicaSetName, rs.Name)
		f.Close()
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
	defer f.Close()
	r := newBlueGreenRollout("foo", 1, nil, "active", "")
	r.Status.CurrentPodHash = "fakepodhash"
	r.Status.StableRS = "fakepodhash"
	r.Status.AvailableReplicas = 1
	r.Status.ReadyReplicas = 1
	r.Status.BlueGreen.ActiveSelector = "fakepodhash"
	r.Status.ObservedGeneration = "122"
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
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs, "")
	conditions.SetRolloutCondition(&r.Status, progressingCondition)
	r.Status.Phase, r.Status.Message = rolloututil.CalculateRolloutPhase(r.Spec, r.Status)

	podTemplate := corev1.PodTemplate{
		Template: rs.Spec.Template,
	}
	corev1defaults.SetObjectDefaults_PodTemplate(&podTemplate)
	rs.Spec.Template = podTemplate.Template

	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	activeSvc := newService("active", 80, selector.MatchLabels, r)

	f.kubeobjects = append(f.kubeobjects, activeSvc, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)
	f.serviceLister = append(f.serviceLister, activeSvc)

	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
	expectedPatch := `{"status":{"observedGeneration":"123"}}`
	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, expectedPatch, patch)
}

// TestComputeHashChangeTolerationCanary verifies that we can tolerate a change in
// controller.ComputeHash() for the canary strategy and do not redeploy any replicasets
func TestComputeHashChangeTolerationCanary(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	r := newCanaryRollout("foo", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))

	r.Status.CurrentPodHash = "fakepodhash"
	r.Status.StableRS = "fakepodhash"
	r.Status.AvailableReplicas = 1
	r.Status.ReadyReplicas = 1
	r.Status.ObservedGeneration = "122"
	rs := newReplicaSet(r, 1)
	rs.Name = "foo-fakepodhash"
	rs.Status.AvailableReplicas = 1
	rs.Status.ReadyReplicas = 1
	rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = "fakepodhash"
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r.Status, availableCondition)
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs, "")
	conditions.SetRolloutCondition(&r.Status, progressingCondition)

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
	expectedPatch := `{"status":{"observedGeneration":"123"}}`
	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, expectedPatch, patch)
}

func TestSwitchBlueGreenToCanary(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	activeSvc := newService("active", 80, nil, r)
	rs := newReplicaSetWithStatus(r, 1, 1)
	rsPodHash := rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r = updateBlueGreenRolloutStatus(r, "", rsPodHash, rsPodHash, 1, 1, 1, 1, false, true)
	// StableRS is set to avoid running the migration code. When .status.canary.stableRS is removed, the line below can be deleted
	//r.Status.Canary.StableRS = rsPodHash
	r.Spec.Strategy.BlueGreen = nil
	r.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{
		Steps: []v1alpha1.CanaryStep{{
			SetWeight: int32Ptr(1),
		}},
	}
	f.rolloutLister = append(f.rolloutLister, r)
	f.kubeobjects = append(f.kubeobjects, rs, activeSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs)

	i := f.expectPatchRolloutAction(r)
	f.objects = append(f.objects, r)
	f.run(getKey(r, t))
	patch := f.getPatchedRollout(i)

	addedConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs, true, "")
	expectedPatch := fmt.Sprintf(`{
			"status": {
				"blueGreen": {
					"activeSelector": null
				},
				"conditions": %s,
				"currentStepIndex": 1,
				"currentStepHash": "%s",
				"selector": "foo=bar"
			}
		}`, addedConditions, conditions.ComputeStepHash(r))
	assert.Equal(t, calculatePatch(r, expectedPatch), patch)
}

func newInvalidSpecCondition(reason string, resourceObj runtime.Object, optionalMessage string) (v1alpha1.RolloutCondition, string) {
	status := corev1.ConditionTrue
	msg := ""
	if optionalMessage != "" {
		msg = optionalMessage
	}

	condition := v1alpha1.RolloutCondition{
		LastTransitionTime: metav1.Now(),
		LastUpdateTime:     metav1.Now(),
		Message:            msg,
		Reason:             reason,
		Status:             status,
		Type:               v1alpha1.InvalidSpec,
	}
	conditionBytes, err := json.Marshal(condition)
	if err != nil {
		panic(err)
	}
	return condition, string(conditionBytes)
}

func TestGetReferencedAnalyses(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	rolloutAnalysisFail := v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: "does-not-exist",
			ClusterScope: false,
		}},
	}

	t.Run("blueGreen pre-promotion analysis - fail", func(t *testing.T) {
		r := newBlueGreenRollout("rollout", 1, nil, "active-service", "preview-service")
		r.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &rolloutAnalysisFail
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getReferencedRolloutAnalyses()
		assert.NotNil(t, err)
		msg := "spec.strategy.blueGreen.prePromotionAnalysis.templates: Invalid value: \"does-not-exist\": AnalysisTemplate 'does-not-exist' not found"
		assert.Equal(t, msg, err.Error())
	})

	t.Run("blueGreen post-promotion analysis - fail", func(t *testing.T) {
		r := newBlueGreenRollout("rollout", 1, nil, "active-service", "preview-service")
		r.Spec.Strategy.BlueGreen.PostPromotionAnalysis = &rolloutAnalysisFail
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getReferencedRolloutAnalyses()
		assert.NotNil(t, err)
		msg := "spec.strategy.blueGreen.postPromotionAnalysis.templates: Invalid value: \"does-not-exist\": AnalysisTemplate 'does-not-exist' not found"
		assert.Equal(t, msg, err.Error())
	})

	t.Run("canary analysis - fail", func(t *testing.T) {
		r := newCanaryRollout("rollout-canary", 1, nil, nil, int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
		r.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
			RolloutAnalysis: rolloutAnalysisFail,
		}
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getReferencedRolloutAnalyses()
		assert.NotNil(t, err)
		msg := "spec.strategy.canary.analysis.templates: Invalid value: \"does-not-exist\": AnalysisTemplate 'does-not-exist' not found"
		assert.Equal(t, msg, err.Error())
	})

	t.Run("canary step analysis - fail", func(t *testing.T) {
		canarySteps := []v1alpha1.CanaryStep{{
			Analysis: &rolloutAnalysisFail,
		}}
		r := newCanaryRollout("rollout-canary", 1, nil, canarySteps, int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getReferencedRolloutAnalyses()
		assert.NotNil(t, err)
		msg := "spec.strategy.canary.steps[0].analysis.templates: Invalid value: \"does-not-exist\": AnalysisTemplate 'does-not-exist' not found"
		assert.Equal(t, msg, err.Error())
	})
}

func TestGetReferencedAnalysisTemplate(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	r := newBlueGreenRollout("rollout", 1, nil, "active-service", "preview-service")
	roAnalysisTemplate := &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: "cluster-analysis-template-name",
			ClusterScope: true,
		}},
	}

	t.Run("get referenced analysisTemplate - fail", func(t *testing.T) {
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getReferencedAnalysisTemplates(r, roAnalysisTemplate, validation.PrePromotionAnalysis, 0)
		expectedErr := field.Invalid(validation.GetAnalysisTemplateWithTypeFieldPath(validation.PrePromotionAnalysis, 0), roAnalysisTemplate.Templates[0].TemplateName, "ClusterAnalysisTemplate 'cluster-analysis-template-name' not found")
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("get referenced analysisTemplate - success", func(t *testing.T) {
		f.clusterAnalysisTemplateLister = append(f.clusterAnalysisTemplateLister, clusterAnalysisTemplate("cluster-analysis-template-name"))
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getReferencedAnalysisTemplates(r, roAnalysisTemplate, validation.PrePromotionAnalysis, 0)
		assert.NoError(t, err)
	})
}

func TestGetReferencedIngressesALB(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	r := newCanaryRollout("rollout", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
	r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		ALB: &v1alpha1.ALBTrafficRouting{
			Ingress: "alb-ingress-name",
		},
	}
	r.Namespace = metav1.NamespaceDefault

	t.Run("get referenced ALB ingress - fail", func(t *testing.T) {
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getReferencedIngresses()
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "alb", "ingress"), "alb-ingress-name", "ingress.extensions \"alb-ingress-name\" not found")
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("get referenced ALB ingress - success", func(t *testing.T) {
		ingress := &extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "alb-ingress-name",
				Namespace: metav1.NamespaceDefault,
			},
		}
		f.ingressLister = append(f.ingressLister, ingress)
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getReferencedIngresses()
		assert.NoError(t, err)
	})
}

func TestGetReferencedIngressesNginx(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	r := newCanaryRollout("rollout", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
	r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		Nginx: &v1alpha1.NginxTrafficRouting{
			StableIngress: "nginx-ingress-name",
		},
	}
	r.Namespace = metav1.NamespaceDefault
	defer f.Close()

	t.Run("get referenced Nginx ingress - fail", func(t *testing.T) {
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getReferencedIngresses()
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "nginx", "stableIngress"), "nginx-ingress-name", "ingress.extensions \"nginx-ingress-name\" not found")
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("get referenced Nginx ingress - success", func(t *testing.T) {
		ingress := &extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nginx-ingress-name",
				Namespace: metav1.NamespaceDefault,
			},
		}
		f.ingressLister = append(f.ingressLister, ingress)
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getReferencedIngresses()
		assert.NoError(t, err)
	})
}

func TestGetAmbassadorMappings(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	c, _, _ := f.newController(noResyncPeriodFunc)
	schema := runtime.NewScheme()
	c.dynamicclientset = dynamicfake.NewSimpleDynamicClient(schema)

	t.Run("will get mappings successfully", func(t *testing.T) {
		// given
		t.Parallel()
		r := newCanaryRollout("rollout", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
		r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
			Ambassador: &v1alpha1.AmbassadorTrafficRouting{
				Mappings: []string{"some-mapping"},
			},
		}
		r.Namespace = metav1.NamespaceDefault
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)

		// when
		_, err = roCtx.getAmbassadorMappings()

		// then
		assert.Error(t, err)
	})
}

func TestRolloutStrategyNotSet(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r.Spec.Strategy.BlueGreen = nil
	r.Status.Conditions = []v1alpha1.RolloutCondition{}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	previewSvc := newService("preview", 80, nil, r)
	activeSvc := newService("active", 80, nil, r)

	rs := newReplicaSet(r, 1)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs)
	f.replicaSetLister = append(f.replicaSetLister, rs)
	f.serviceLister = append(f.serviceLister, previewSvc, activeSvc)

	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
	patchedRollout := f.getPatchedRollout(patchIndex)
	assert.Contains(t, patchedRollout, `Rollout has missing field '.spec.strategy.canary or .spec.strategy.blueGreen'`)
}

// TestWriteBackToInformer verifies that after a rollout reconciles, the new version of the rollout
// is written back to the informer
func TestWriteBackToInformer(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newCanaryRollout("foo", 10, nil, nil, int32Ptr(0), intstr.FromInt(1), intstr.FromInt(0))
	r1.Status.StableRS = ""
	rs1 := newReplicaSetWithStatus(r1, 10, 10)

	f.rolloutLister = append(f.rolloutLister, r1)
	f.objects = append(f.objects, r1)

	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	f.expectPatchRolloutAction(r1)

	c, i, k8sI := f.newController(noResyncPeriodFunc)
	roKey := getKey(r1, t)
	f.runController(roKey, true, false, c, i, k8sI)

	// Verify the informer was updated with the new unstructured object after reconciliation
	obj, _, _ := c.rolloutsIndexer.GetByKey(roKey)
	un := obj.(*unstructured.Unstructured)
	stableRS, _, _ := unstructured.NestedString(un.Object, "status", "stableRS")
	assert.NotEmpty(t, stableRS)
	assert.Equal(t, rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], stableRS)
}
