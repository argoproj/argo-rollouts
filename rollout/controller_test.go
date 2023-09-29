package rollout

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
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
	"k8s.io/utils/pointer"
	"sigs.k8s.io/yaml"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/validation"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	"github.com/argoproj/argo-rollouts/rollout/mocks"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/hash"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	"github.com/argoproj/argo-rollouts/utils/queue"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
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
	ingressLister                 []*ingressutil.Ingress
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
	fakeTrafficRouting *mocks.TrafficRoutingReconciler
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{}
	f.t = t
	f.objects = []runtime.Object{}
	f.kubeobjects = []runtime.Object{}
	f.enqueuedObjects = make(map[string]int)
	now := time.Now()
	timeutil.Now = func() time.Time { return now }
	f.unfreezeTime = func() error {
		timeutil.Now = time.Now
		return nil
	}

	f.fakeTrafficRouting = newFakeSingleTrafficRoutingReconciler()
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
		LastTransitionTime: timeutil.MetaNow(),
		LastUpdateTime:     timeutil.MetaNow(),
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

func newHealthyCondition(isHealthy bool) (v1alpha1.RolloutCondition, string) {
	status := corev1.ConditionTrue
	msg := conditions.RolloutHealthyMessage
	if !isHealthy {
		status = corev1.ConditionFalse
		msg = conditions.RolloutNotHealthyMessage
	}
	condition := v1alpha1.RolloutCondition{
		LastTransitionTime: timeutil.MetaNow(),
		LastUpdateTime:     timeutil.MetaNow(),
		Message:            msg,
		Reason:             conditions.RolloutHealthyReason,
		Status:             status,
		Type:               v1alpha1.RolloutHealthy,
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
		LastTransitionTime: timeutil.MetaNow(),
		LastUpdateTime:     timeutil.MetaNow(),
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
		if reason == conditions.ReplicaSetNotAvailableReason {
			msg = conditions.NotAvailableMessage
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
		LastTransitionTime: timeutil.MetaNow(),
		LastUpdateTime:     timeutil.MetaNow(),
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
		LastTransitionTime: timeutil.MetaNow(),
		LastUpdateTime:     timeutil.MetaNow(),
		Message:            message,
		Reason:             conditions.AvailableReason,
		Status:             status,
		Type:               v1alpha1.RolloutAvailable,
	}
	conditionBytes, _ := json.Marshal(condition)
	return condition, string(conditionBytes)
}

func generateConditionsPatch(available bool, progressingReason string, progressingResource runtime.Object, availableConditionFirst bool, progressingMessage string, isCompleted bool) string {
	_, availableCondition := newAvailableCondition(available)
	_, progressingCondition := newProgressingCondition(progressingReason, progressingResource, progressingMessage)
	_, completedCondition := newCompletedCondition(isCompleted)
	if availableConditionFirst {
		return fmt.Sprintf("[%s, %s, %s]", availableCondition, progressingCondition, completedCondition)
	}
	return fmt.Sprintf("[%s, %s, %s]", progressingCondition, availableCondition, completedCondition)
}

func generateConditionsPatchWithPause(available bool, progressingReason string, progressingResource runtime.Object, availableConditionFirst bool, progressingMessage string, isPaused bool, isCompleted bool) string {
	_, availableCondition := newAvailableCondition(available)
	_, progressingCondition := newProgressingCondition(progressingReason, progressingResource, progressingMessage)
	_, pauseCondition := newPausedCondition(isPaused)
	_, completedCondition := newCompletedCondition(isCompleted)
	if availableConditionFirst {
		return fmt.Sprintf("[%s, %s, %s, %s]", availableCondition, completedCondition, progressingCondition, pauseCondition)
	}
	return fmt.Sprintf("[%s, %s, %s, %s]", progressingCondition, pauseCondition, availableCondition, completedCondition)
}

func generateConditionsPatchWithHealthy(available bool, progressingReason string, progressingResource runtime.Object, availableConditionFirst bool, progressingMessage string, isHealthy bool, isCompleted bool) string {
	_, availableCondition := newAvailableCondition(available)
	_, progressingCondition := newProgressingCondition(progressingReason, progressingResource, progressingMessage)
	_, healthyCondition := newHealthyCondition(isHealthy)
	_, completedCondition := newCompletedCondition(isCompleted)
	if availableConditionFirst {
		return fmt.Sprintf("[%s, %s, %s, %s]", availableCondition, completedCondition, healthyCondition, progressingCondition)
	}
	return fmt.Sprintf("[%s, %s, %s, %s]", completedCondition, healthyCondition, progressingCondition, availableCondition)
}

func generateConditionsPatchWithCompleted(available bool, progressingReason string, progressingResource runtime.Object, availableConditionFirst bool, progressingMessage string, isCompleted bool) string {
	_, availableCondition := newAvailableCondition(available)
	_, progressingCondition := newProgressingCondition(progressingReason, progressingResource, progressingMessage)
	_, completeCondition := newCompletedCondition(isCompleted)
	if availableConditionFirst {
		return fmt.Sprintf("[%s, %s, %s]", availableCondition, progressingCondition, completeCondition)
	}
	return fmt.Sprintf("[%s, %s, %s]", progressingCondition, availableCondition, completeCondition)
}

func generateConditionsPatchWithCompletedHealthy(available bool, progressingReason string, progressingResource runtime.Object, availableConditionFirst bool, progressingMessage string, isHealthy bool, isCompleted bool) string {
	_, completedCondition := newCompletedCondition(isCompleted)
	_, availableCondition := newAvailableCondition(available)
	_, progressingCondition := newProgressingCondition(progressingReason, progressingResource, progressingMessage)
	_, healthyCondition := newHealthyCondition(isHealthy)
	if availableConditionFirst {
		return fmt.Sprintf("[%s, %s, %s, %s]", availableCondition, healthyCondition, completedCondition, progressingCondition)
	}
	return fmt.Sprintf("[%s, %s, %s, %s]", healthyCondition, completedCondition, progressingCondition, availableCondition)
}

func updateConditionsPatch(r v1alpha1.Rollout, newCondition v1alpha1.RolloutCondition) string {
	conditions.SetRolloutCondition(&r.Status, newCondition)
	conditionsBytes, _ := json.Marshal(r.Status.Conditions)
	return string(conditionsBytes)
}

// func updateBlueGreenRolloutStatus(r *v1alpha1.Rollout, preview, active string, availableReplicas, updatedReplicas, hpaReplicas int32, pause bool, available bool, progressingStatus string) *v1alpha1.Rollout {
func updateBlueGreenRolloutStatus(r *v1alpha1.Rollout, preview, active, stable string, availableReplicas, updatedReplicas, totalReplicas, hpaReplicas int32, pause bool, available bool, isCompleted bool) *v1alpha1.Rollout {
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
	completeCond, _ := newCompletedCondition(isCompleted)
	newRollout.Status.Conditions = append(newRollout.Status.Conditions, completeCond)
	if pause {
		now := timeutil.MetaNow()
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
	podHash := hash.ComputePodTemplateHash(&r.Spec.Template, r.Status.CollisionCount)
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
	f.t.Helper()
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
	vsvcGVR := istioutil.GetIstioVirtualServiceGVR()
	destGVR := istioutil.GetIstioDestinationRuleGVR()
	listMapping := map[schema.GroupVersionResource]string{
		tgbGVR:  "TargetGroupBindingList",
		vsvcGVR: vsvcGVR.Resource + "List",
		destGVR: destGVR.Resource + "List",
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

	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, f.kubeclient, k8sI)
	if err != nil {
		f.t.Fatal(err)
	}

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
		IngressWrapper:                  ingressWrapper,
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
	c.newTrafficRoutingReconciler = func(roCtx *rolloutContext) ([]trafficrouting.TrafficRoutingReconciler, error) {
		if roCtx.rollout.Spec.Strategy.Canary == nil || roCtx.rollout.Spec.Strategy.Canary.TrafficRouting == nil {
			return nil, nil
		}
		var reconcilers = []trafficrouting.TrafficRoutingReconciler{}
		reconcilers = append(reconcilers, f.fakeTrafficRouting)
		return reconcilers, nil
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
		ing, err := i.GetExtensionsIngress()
		if err != nil {
			log.Fatal(err)
		}
		k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(ing)
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

	err := c.syncHandler(context.Background(), rolloutName)
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

func (f *fixture) expectGetRolloutAction(rollout *v1alpha1.Rollout) int { //nolint:unused
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

func (f *fixture) expectUpdateExperimentAction(ex *v1alpha1.Experiment) int { //nolint:unused
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
	now := timeutil.Now().Add(time.Duration(scaleDownDelaySeconds) * time.Second).UTC().Format(time.RFC3339)
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

func (f *fixture) getPatchedAnalysisRun(index int) *v1alpha1.AnalysisRun { //nolint:unused
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

func (f *fixture) getDeletedReplicaSet(index int) string { //nolint:unused
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
				Time: timeutil.MetaNow().Add(-10 * time.Second),
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
	assert.JSONEq(t, calculatePatch(r, expectedPatch), patch)
}

// TestPodTemplateHashEquivalence verifies the hash is computed consistently when there are slight
// variations made to the pod template in equivalent ways.
func TestPodTemplateHashEquivalence(t *testing.T) {
	var err error
	// NOTE: This test will fail on every k8s library upgrade.
	// To fix it, update expectedReplicaSetName to match the new hash.
	expectedReplicaSetName := "guestbook-6c5667f666"

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
	completedCondition, _ := newCompletedCondition(true)
	conditions.SetRolloutCondition(&r.Status, completedCondition)
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
	completedCondition, _ := newCompletedCondition(true)
	conditions.SetRolloutCondition(&r.Status, completedCondition)

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
	r = updateBlueGreenRolloutStatus(r, "", rsPodHash, rsPodHash, 1, 1, 1, 1, false, true, false)
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

	addedConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs, true, "", true)
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
	assert.JSONEq(t, calculatePatch(r, expectedPatch), patch)
}

func newInvalidSpecCondition(reason string, resourceObj runtime.Object, optionalMessage string) (v1alpha1.RolloutCondition, string) {
	status := corev1.ConditionTrue
	msg := ""
	if optionalMessage != "" {
		msg = optionalMessage
	}

	condition := v1alpha1.RolloutCondition{
		LastTransitionTime: timeutil.MetaNow(),
		LastUpdateTime:     timeutil.MetaNow(),
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
		f.ingressLister = append(f.ingressLister, ingressutil.NewLegacyIngress(ingress))
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		i, err := roCtx.getReferencedIngresses()
		assert.NoError(t, err)
		assert.NotNil(t, i)
	})
}

func TestGetReferencedIngressesALBMultiIngress(t *testing.T) {
	primaryIngress := "alb-ingress-name"
	addIngress := "alb-ingress-additional"
	ingresses := []string{primaryIngress, addIngress}
	f := newFixture(t)
	defer f.Close()
	r := newCanaryRollout("rollout", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
	r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		ALB: &v1alpha1.ALBTrafficRouting{
			Ingresses: ingresses,
		},
	}
	r.Namespace = metav1.NamespaceDefault
	defer f.Close()

	tests := []struct {
		name        string
		ingresses   []*ingressutil.Ingress
		expectedErr *field.Error
	}{
		{
			"get referenced ALB ingress - fail first ingress when both missing",
			[]*ingressutil.Ingress{},
			field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "alb", "ingresses"), ingresses, fmt.Sprintf("ingress.extensions \"%s\" not found", primaryIngress)),
		},
		{
			"get referenced ALB ingress - fail on primary when additional present",
			[]*ingressutil.Ingress{
				ingressutil.NewLegacyIngress(&extensionsv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      addIngress,
						Namespace: metav1.NamespaceDefault,
					},
				}),
			},
			field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "alb", "ingresses"), ingresses, fmt.Sprintf("ingress.extensions \"%s\" not found", primaryIngress)),
		},
		{
			"get referenced ALB ingress - fail on secondary when only secondary missing",
			[]*ingressutil.Ingress{
				ingressutil.NewLegacyIngress(&extensionsv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      primaryIngress,
						Namespace: metav1.NamespaceDefault,
					},
				}),
			},
			field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "alb", "ingresses"), ingresses, fmt.Sprintf("ingress.extensions \"%s\" not found", addIngress)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// clear fixture
			f.ingressLister = []*ingressutil.Ingress{}
			for _, ing := range test.ingresses {
				f.ingressLister = append(f.ingressLister, ing)
			}
			c, _, _ := f.newController(noResyncPeriodFunc)
			roCtx, err := c.newRolloutContext(r)
			assert.NoError(t, err)
			_, err = roCtx.getReferencedIngresses()
			assert.Equal(t, test.expectedErr.Error(), err.Error())
		})
	}

	t.Run("get referenced ALB ingress - success", func(t *testing.T) {
		// clear fixture
		f.ingressLister = []*ingressutil.Ingress{}
		ingress := &extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      primaryIngress,
				Namespace: metav1.NamespaceDefault,
			},
		}
		ingressAdditional := &extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addIngress,
				Namespace: metav1.NamespaceDefault,
			},
		}
		f.ingressLister = append(f.ingressLister, ingressutil.NewLegacyIngress(ingress))
		f.ingressLister = append(f.ingressLister, ingressutil.NewLegacyIngress(ingressAdditional))
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		ingresses, err := roCtx.getReferencedIngresses()
		assert.NoError(t, err)
		assert.Len(t, *ingresses, 2, "Should find the main ingress and the additional ingress")
	})
}

func TestGetReferencedIngressesNginx(t *testing.T) {
	primaryIngress := "nginx-ingress-name"
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
		// clear fixture
		f.ingressLister = []*ingressutil.Ingress{}
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getReferencedIngresses()
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "nginx", "stableIngress"), primaryIngress, fmt.Sprintf("ingress.extensions \"%s\" not found", primaryIngress))
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("get referenced Nginx ingress - success", func(t *testing.T) {
		// clear fixture
		f.ingressLister = []*ingressutil.Ingress{}
		ingress := &extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      primaryIngress,
				Namespace: metav1.NamespaceDefault,
			},
		}
		f.ingressLister = append(f.ingressLister, ingressutil.NewLegacyIngress(ingress))
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getReferencedIngresses()
		assert.NoError(t, err)
	})
}
func TestGetReferencedIngressesNginxMultiIngress(t *testing.T) {
	primaryIngress := "nginx-ingress-name"
	addIngress := "nginx-ingress-additional"
	ingresses := []string{primaryIngress, addIngress}
	f := newFixture(t)
	defer f.Close()
	r := newCanaryRollout("rollout", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
	r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		Nginx: &v1alpha1.NginxTrafficRouting{
			StableIngresses: ingresses,
		},
	}
	r.Namespace = metav1.NamespaceDefault
	defer f.Close()

	tests := []struct {
		name        string
		ingresses   []*ingressutil.Ingress
		expectedErr *field.Error
	}{
		{
			"get referenced Nginx ingress - fail first ingress when both missing",
			[]*ingressutil.Ingress{},
			field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "nginx", "StableIngresses"), ingresses, fmt.Sprintf("ingress.extensions \"%s\" not found", primaryIngress)),
		},
		{
			"get referenced Nginx ingress - fail on primary when additional present",
			[]*ingressutil.Ingress{
				ingressutil.NewLegacyIngress(&extensionsv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      addIngress,
						Namespace: metav1.NamespaceDefault,
					},
				}),
			},
			field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "nginx", "StableIngresses"), ingresses, fmt.Sprintf("ingress.extensions \"%s\" not found", primaryIngress)),
		},
		{
			"get referenced Nginx ingress - fail on secondary when only secondary missing",
			[]*ingressutil.Ingress{
				ingressutil.NewLegacyIngress(&extensionsv1beta1.Ingress{
					ObjectMeta: metav1.ObjectMeta{
						Name:      primaryIngress,
						Namespace: metav1.NamespaceDefault,
					},
				}),
			},
			field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "nginx", "StableIngresses"), ingresses, fmt.Sprintf("ingress.extensions \"%s\" not found", addIngress)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// clear fixture
			f.ingressLister = []*ingressutil.Ingress{}
			for _, ing := range test.ingresses {
				f.ingressLister = append(f.ingressLister, ing)
			}
			c, _, _ := f.newController(noResyncPeriodFunc)
			roCtx, err := c.newRolloutContext(r)
			assert.NoError(t, err)
			_, err = roCtx.getReferencedIngresses()
			assert.Equal(t, test.expectedErr.Error(), err.Error())
		})
	}

	t.Run("get referenced Nginx ingress - success", func(t *testing.T) {
		// clear fixture
		f.ingressLister = []*ingressutil.Ingress{}
		ingress := &extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      primaryIngress,
				Namespace: metav1.NamespaceDefault,
			},
		}
		ingressAdditional := &extensionsv1beta1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addIngress,
				Namespace: metav1.NamespaceDefault,
			},
		}
		f.ingressLister = append(f.ingressLister, ingressutil.NewLegacyIngress(ingress))
		f.ingressLister = append(f.ingressLister, ingressutil.NewLegacyIngress(ingressAdditional))
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		ingresses, err := roCtx.getReferencedIngresses()
		assert.NoError(t, err)
		assert.Len(t, *ingresses, 2, "Should find the main ingress and the additional ingress")
	})
}

func TestGetReferencedAppMeshResources(t *testing.T) {
	r := newCanaryRollout("rollout", 1, nil, nil, nil, intstr.FromInt(0), intstr.FromInt(1))
	r.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{
		AppMesh: &v1alpha1.AppMeshTrafficRouting{
			VirtualService: &v1alpha1.AppMeshVirtualService{
				Name:   "mysvc",
				Routes: []string{"primary"},
			},
			VirtualNodeGroup: &v1alpha1.AppMeshVirtualNodeGroup{
				CanaryVirtualNodeRef: &v1alpha1.AppMeshVirtualNodeReference{
					Name: "mysvc-canary-vn",
				},
				StableVirtualNodeRef: &v1alpha1.AppMeshVirtualNodeReference{
					Name: "mysvc-stable-vn",
				},
			},
		},
	}
	r.Namespace = "default"

	t.Run("should return error when virtual-service is not defined on rollout", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		c, _, _ := f.newController(noResyncPeriodFunc)
		rCopy := r.DeepCopy()
		rCopy.Spec.Strategy.Canary.TrafficRouting.AppMesh.VirtualService = nil
		roCtx, err := c.newRolloutContext(rCopy)
		assert.NoError(t, err)
		_, err = roCtx.getRolloutReferencedResources()
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "appmesh", "virtualService"), "null", "must provide virtual-service")
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("should return error when virtual-service is not-found", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getRolloutReferencedResources()
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "appmesh", "virtualService"), "mysvc.default", "virtualservices.appmesh.k8s.aws \"mysvc\" not found")
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("should return error when virtual-router is not-found", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		vsvc := `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualService
metadata:
  name: mysvc
  namespace: default
spec:
  provider:
    virtualRouter:
      virtualRouterRef:
        name: mysvc-vrouter
`
		uVsvc := unstructuredutil.StrToUnstructuredUnsafe(vsvc)
		f.objects = append(f.objects, uVsvc)
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		_, err = roCtx.getRolloutReferencedResources()
		expectedErr := field.Invalid(field.NewPath("spec", "strategy", "canary", "trafficRouting", "appmesh", "virtualService"), "mysvc.default", "virtualrouters.appmesh.k8s.aws \"mysvc-vrouter\" not found")
		assert.Equal(t, expectedErr.Error(), err.Error())
	})

	t.Run("get referenced App Mesh - success", func(t *testing.T) {
		f := newFixture(t)
		defer f.Close()

		vsvc := `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualService
metadata:
  name: mysvc
  namespace: default
spec:
  provider:
    virtualRouter:
      virtualRouterRef:
        name: mysvc-vrouter
`

		vrouter := `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualRouter
metadata:
  name: mysvc-vrouter
  namespace: default
spec:
  listeners:
    - portMapping:
        port: 8080
        protocol: http
  routes:
    - name: primary
      httpRoute:
        match:
          prefix: /
        action:
          weightedTargets:
            - virtualNodeRef:
                name: mysvc-canary-vn
              weight: 0
            - virtualNodeRef:
                name: mysvc-stable-vn
              weight: 100
`

		uVsvc := unstructuredutil.StrToUnstructuredUnsafe(vsvc)
		uVrouter := unstructuredutil.StrToUnstructuredUnsafe(vrouter)
		f.objects = append(f.objects, uVsvc, uVrouter)
		c, _, _ := f.newController(noResyncPeriodFunc)
		roCtx, err := c.newRolloutContext(r)
		assert.NoError(t, err)
		refRsources, err := roCtx.getRolloutReferencedResources()
		assert.NoError(t, err)
		assert.Len(t, refRsources.AppMeshResources, 1)
		assert.Equal(t, refRsources.AppMeshResources[0].GetKind(), "VirtualRouter")
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
	obj, exists, err := c.rolloutsIndexer.GetByKey(roKey)
	assert.NoError(t, err)
	assert.True(t, exists)

	// The type returned from c.rolloutsIndexer.GetByKey is not always the same type it switches between
	// *unstructured.Unstructured and *v1alpha1.Rollout the underlying cause is not fully known. We use the
	// runtime.DefaultUnstructuredConverter to account for this.
	unObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	assert.NoError(t, err)

	stableRS, _, _ := unstructured.NestedString(unObj, "status", "stableRS")
	assert.NotEmpty(t, stableRS)
	assert.Equal(t, rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], stableRS)
}

func TestRun(t *testing.T) {
	f := newFixture(t)
	defer f.Close()
	// make sure we can start and top the controller
	c, _, _ := f.newController(noResyncPeriodFunc)
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()
	go func() {
		time.Sleep(1000 * time.Millisecond)
		c.rolloutWorkqueue.ShutDownWithDrain()
		cancel()
	}()
	c.Run(ctx, 1)
}
