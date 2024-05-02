package alb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	networkingv1 "k8s.io/api/networking/v1"

	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/stretchr/testify/assert"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"
	"k8s.io/utils/strings/slices"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/aws"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	jsonutil "github.com/argoproj/argo-rollouts/utils/json"
	"github.com/argoproj/argo-rollouts/utils/record"
)

const STABLE_SVC = "stable-svc"
const CANARY_SVC = "canary-svc"
const PING_SVC = "ping-service"
const PONG_SVC = "pong-service"

func fakeRollout(stableSvc, canarySvc string, pingPong *v1alpha1.PingPongSpec, stableIng string, port int32) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: stableSvc,
					CanaryService: canarySvc,
					PingPong:      pingPong,
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						ALB: &v1alpha1.ALBTrafficRouting{
							Ingress:     stableIng,
							ServicePort: port,
						},
					},
				},
			},
		},
	}
}

func fakeRolloutWithMultiIngress(stableSvc, canarySvc string, pingPong *v1alpha1.PingPongSpec, stableIngresses []string, port int32) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: stableSvc,
					CanaryService: canarySvc,
					PingPong:      pingPong,
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						ALB: &v1alpha1.ALBTrafficRouting{
							Ingresses:   stableIngresses,
							ServicePort: port,
						},
					},
				},
			},
		},
	}
}

func fakeRolloutWithServicePorts(stableSvc, canarySvc string, pingPong *v1alpha1.PingPongSpec, stableIngresses []string) *v1alpha1.Rollout {
	result := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: stableSvc,
					CanaryService: canarySvc,
					PingPong:      pingPong,
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						ALB: &v1alpha1.ALBTrafficRouting{},
					},
				},
			},
		},
	}

	for _, ingress := range stableIngresses {
		result.Spec.Strategy.Canary.TrafficRouting.ALB.Ingresses = append(
			result.Spec.Strategy.Canary.TrafficRouting.ALB.Ingresses,
			ingress,
		)
		result.Spec.Strategy.Canary.TrafficRouting.ALB.ServicePorts = append(
			result.Spec.Strategy.Canary.TrafficRouting.ALB.ServicePorts,
			v1alpha1.ALBIngressWithPorts{Ingress: ingress, ServicePorts: []int32{80, 443}},
		)
	}

	return result
}

const actionTemplateWithExperiments = `{"Type":"forward","ForwardConfig":{"TargetGroups":[{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d}]}}`

func albActionAnnotation(stable string) string {
	return fmt.Sprintf("%s%s%s", ingressutil.ALBIngressAnnotation, ingressutil.ALBActionPrefix, stable)
}

func targetGroup(name string, port int32, weight int32) ingressutil.ALBTargetGroup {
	return ingressutil.ALBTargetGroup{
		ServiceName: name,
		ServicePort: strconv.Itoa(int(port)),
		Weight:      pointer.Int64Ptr(int64(weight)),
	}
}

func albActionForwardFromDestinations(includeStickyConfig bool, ports []int32, destinations ...v1alpha1.WeightDestination) string {
	targetGroups := make([]ingressutil.ALBTargetGroup, 0)
	for _, dest := range destinations {
		for _, port := range ports {
			targetGroups = append(targetGroups, targetGroup(dest.ServiceName, port, dest.Weight))
		}
	}
	action := ingressutil.ALBAction{
		Type:          "forward",
		ForwardConfig: ingressutil.ALBForwardConfig{TargetGroups: targetGroups},
	}
	if includeStickyConfig {
		action.ForwardConfig.TargetGroupStickinessConfig = &ingressutil.ALBTargetGroupStickinessConfig{
			DurationSeconds: 300,
			Enabled:         true,
		}
	}
	return string(jsonutil.MustMarshal(action))
}
func albActionForward(stableSvc, canarySvc string, ports []int32, weight int32, includeStickyConfig bool) string {
	targetGroups := make([]ingressutil.ALBTargetGroup, 0)
	for _, port := range ports {
		targetGroups = append(targetGroups, targetGroup(canarySvc, port, weight))
	}
	for _, port := range ports {
		targetGroups = append(targetGroups, targetGroup(stableSvc, port, 100-weight))
	}
	action := ingressutil.ALBAction{
		Type:          "forward",
		ForwardConfig: ingressutil.ALBForwardConfig{TargetGroups: targetGroups},
	}
	if includeStickyConfig {
		action.ForwardConfig.TargetGroupStickinessConfig = &ingressutil.ALBTargetGroupStickinessConfig{
			DurationSeconds: 300,
			Enabled:         true,
		}
	}
	return string(jsonutil.MustMarshal(action))
}

func ingress(name, stableSvc, canarySvc, actionService string, ports []int32, weight int32, managedBy string, includeStickyConfig bool) *networkingv1.Ingress {
	managedByValue := ingressutil.ManagedALBAnnotations{
		managedBy: ingressutil.ManagedALBAnnotation{albActionAnnotation(actionService)},
	}

	i := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{
				albActionAnnotation(actionService): albActionForward(stableSvc, canarySvc, ports, weight, includeStickyConfig),
				ingressutil.ManagedAnnotations:     managedByValue.String(),
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: actionService,
											Port: networkingv1.ServiceBackendPort{
												Name:   "use-annotation",
												Number: 0,
											},
										},
										Resource: nil,
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return i
}

func ingressesToObjects(ingresses []*networkingv1.Ingress) []runtime.Object {
	result := make([]runtime.Object, len(ingresses))
	for i, ingress := range ingresses {
		result[i] = ingress
	}
	return result
}

func TestType(t *testing.T) {
	client := fake.NewSimpleClientset()
	rollout := fakeRollout("stable-service", "canary-service", nil, "stable-ingress", 443)
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
	})
	assert.Equal(t, Type, r.Type())
	assert.NoError(t, err)
}

func TestTypeMultiIngress(t *testing.T) {
	client := fake.NewSimpleClientset()
	rollout := fakeRolloutWithMultiIngress("stable-service", "canary-service", nil, []string{"stable-ingress", "multi-ingress"}, 443)
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
	})
	assert.Equal(t, Type, r.Type())
	assert.NoError(t, err)
}

func TestAddManagedAnnotation(t *testing.T) {
	annotations, _ := modifyManagedAnnotation(map[string]string{}, "argo-rollouts", true, "alb.ingress.kubernetes.io/actions.action1", "alb.ingress.kubernetes.io/conditions.action1")
	assert.Equal(t, annotations[ingressutil.ManagedAnnotations], "{\"argo-rollouts\":[\"alb.ingress.kubernetes.io/actions.action1\",\"alb.ingress.kubernetes.io/conditions.action1\"]}")
	_, err := modifyManagedAnnotation(map[string]string{ingressutil.ManagedAnnotations: "invalid, non-json value"}, "some-rollout", false)
	assert.Error(t, err)
}

func testIngressNotFound(t *testing.T, ro *v1alpha1.Rollout) {
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})
	assert.NoError(t, err)
	err = r.SetWeight(10)
	assert.True(t, k8serrors.IsNotFound(err))
}
func TestIngressNotFound(t *testing.T) {
	ro := fakeRollout("stable-service", "canary-service", nil, "stable-ingress", 443)
	testIngressNotFound(t, ro)
}

func TestIngressNotFoundMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress("stable-service", "canary-service", nil, []string{"stable-ingress", "multi-ingress"}, 443)
	testIngressNotFound(t, ro)
}
func TestIngressNotFoundServicePorts(t *testing.T) {
	ro := fakeRolloutWithServicePorts("stable-service", "canary-service", nil, []string{"stable-ingress", "multi-ingress"})
	testIngressNotFound(t, ro)
}

func testServiceNotFoundInIngress(t *testing.T, ro *v1alpha1.Rollout, ingresses []*networkingv1.Ingress) {
	client := fake.NewSimpleClientset(ingressesToObjects(ingresses)...)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	for _, ingress := range ingresses {
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(ingress)
	}
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})
	assert.NoError(t, err)
	err = r.SetWeight(10)
	assert.Errorf(t, err, "ingress does not use the stable service")
}

func TestServiceNotFoundInIngress(t *testing.T) {
	ro := fakeRollout("stable-stable", "canary-service", nil, "ingress", 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ALB.RootService = "invalid-svc"
	ingresses := []*networkingv1.Ingress{
		ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 50, ro.Name, false),
	}
	testServiceNotFoundInIngress(t, ro, ingresses)
}

func TestServiceNotFoundInMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress("stable-service", "canary-service", nil, []string{"stable-ingress", "multi-ingress"}, 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ALB.RootService = "invalid-svc"
	ingresses := []*networkingv1.Ingress{
		ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 50, ro.Name, false),
		ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 50, ro.Name, false),
	}

	testServiceNotFoundInIngress(t, ro, ingresses)
}
func TestServiceNotFoundInServicePorts(t *testing.T) {
	ro := fakeRolloutWithServicePorts("stable-service", "canary-service", nil, []string{"stable-ingress", "multi-ingress"})
	ro.Spec.Strategy.Canary.TrafficRouting.ALB.RootService = "invalid-svc"
	ingresses := []*networkingv1.Ingress{
		ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 50, ro.Name, false),
		ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 50, ro.Name, false),
	}

	testServiceNotFoundInIngress(t, ro, ingresses)
}

func testNoChanges(t *testing.T, ro *v1alpha1.Rollout, ingresses []*networkingv1.Ingress) {
	client := fake.NewSimpleClientset(ingressesToObjects(ingresses)...)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	for _, ingress := range ingresses {
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(ingress)
	}
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})
	assert.NoError(t, err)
	err = r.SetWeight(10)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 0)
}

func TestNoChanges(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	ingresses := []*networkingv1.Ingress{
		ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 10, ro.Name, false),
	}
	testNoChanges(t, ro, ingresses)
}

func TestNoChangesMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	ingresses := []*networkingv1.Ingress{
		ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 10, ro.Name, false),
		ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 10, ro.Name, false),
	}
	testNoChanges(t, ro, ingresses)
}

func TestNoChangesServicePorts(t *testing.T) {
	ro := fakeRolloutWithServicePorts(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"})
	ingresses := []*networkingv1.Ingress{
		ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 10, ro.Name, false),
		ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 10, ro.Name, false),
	}
	testNoChanges(t, ro, ingresses)
}

func testErrorOnInvalidManagedBy(t *testing.T, ro *v1alpha1.Rollout, ingresses []*networkingv1.Ingress) {
	client := fake.NewSimpleClientset(ingressesToObjects(ingresses)...)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	for _, ingress := range ingresses {
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(ingress)
	}

	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})
	assert.NoError(t, err)
	err = r.SetWeight(10)
	assert.Errorf(t, err, "incorrectly formatted managed actions annotation")
}

func TestErrorOnInvalidManagedBy(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
	i.Annotations[ingressutil.ManagedAnnotations] = "test"
	testErrorOnInvalidManagedBy(t, ro, []*networkingv1.Ingress{i})
}

func TestErrorOnInvalidManagedByMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress("stable-service", "canary-service", nil, []string{"stable-ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
	mi.Annotations[ingressutil.ManagedAnnotations] = "test"

	testErrorOnInvalidManagedBy(t, ro, []*networkingv1.Ingress{i, mi})
}

func TestErrorOnInvalidManagedByServicePorts(t *testing.T) {
	ro := fakeRolloutWithServicePorts("stable-service", "canary-service", nil, []string{"stable-ingress", "multi-ingress"})
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
	mi.Annotations[ingressutil.ManagedAnnotations] = "test"

	testErrorOnInvalidManagedBy(t, ro, []*networkingv1.Ingress{i, mi})
}

func testSetInitialDesiredWeight(t *testing.T, ro *v1alpha1.Rollout, ingresses []*networkingv1.Ingress, numActions int) {
	client := fake.NewSimpleClientset(ingressesToObjects(ingresses)...)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	for _, ingress := range ingresses {
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(ingress)
	}
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})
	assert.NoError(t, err)
	err = r.SetWeight(10)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), numActions)
}

func TestSetInitialDesiredWeight(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
	i.Annotations = map[string]string{}

	testSetInitialDesiredWeight(t, ro, []*networkingv1.Ingress{i}, 1)
}

func TestSetInitialDesiredWeightMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
	i.Annotations = map[string]string{}

	testSetInitialDesiredWeight(t, ro, []*networkingv1.Ingress{i, mi}, 2)
}
func TestSetInitialDesiredWeightServicePorts(t *testing.T) {
	ro := fakeRolloutWithServicePorts(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"})
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 5, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 5, ro.Name, false)
	i.Annotations = map[string]string{}

	testSetInitialDesiredWeight(t, ro, []*networkingv1.Ingress{i, mi}, 2)
}

func testSetWeightPingPong(t *testing.T, ro *v1alpha1.Rollout, ingresses []*networkingv1.Ingress, numActions int) {
	client := fake.NewSimpleClientset(ingressesToObjects(ingresses)...)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	for _, ingress := range ingresses {
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(ingress)
	}
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})
	assert.NoError(t, err)
	err = r.SetWeight(10)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), numActions)
}
func TestSetWeightPingPong(t *testing.T) {
	pp := &v1alpha1.PingPongSpec{PingService: PING_SVC, PongService: PONG_SVC}
	ro := fakeRollout("", "", pp, "ingress", 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ALB.RootService = "root-service"
	ro.Status.Canary.StablePingPong = PONG_SVC
	i := ingress("ingress", PING_SVC, PONG_SVC, "root-service", []int32{443}, 10, ro.Name, false)
	i.Annotations = map[string]string{}
	testSetWeightPingPong(t, ro, []*networkingv1.Ingress{i}, 1)
}

func TestSetWeightPingPongMultiIngress(t *testing.T) {
	pp := &v1alpha1.PingPongSpec{PingService: PING_SVC, PongService: PONG_SVC}
	ro := fakeRolloutWithMultiIngress("", "", pp, []string{"ingress", "multi-ingress"}, 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ALB.RootService = "root-service"
	ro.Status.Canary.StablePingPong = PONG_SVC
	i := ingress("ingress", PING_SVC, PONG_SVC, "root-service", []int32{443}, 10, ro.Name, false)
	mi := ingress("multi-ingress", PING_SVC, PONG_SVC, "root-service", []int32{443}, 10, ro.Name, false)
	i.Annotations = map[string]string{}

	testSetWeightPingPong(t, ro, []*networkingv1.Ingress{i, mi}, 2)
}
func TestSetWeightPingPongServicePorts(t *testing.T) {
	pp := &v1alpha1.PingPongSpec{PingService: PING_SVC, PongService: PONG_SVC}
	ro := fakeRolloutWithMultiIngress("", "", pp, []string{"ingress", "multi-ingress"}, 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ALB.RootService = "root-service"
	ro.Status.Canary.StablePingPong = PONG_SVC
	i := ingress("ingress", PING_SVC, PONG_SVC, "root-service", []int32{80, 443}, 10, ro.Name, false)
	mi := ingress("multi-ingress", PING_SVC, PONG_SVC, "root-service", []int32{80, 443}, 10, ro.Name, false)
	i.Annotations = map[string]string{}

	testSetWeightPingPong(t, ro, []*networkingv1.Ingress{i, mi}, 2)
}

func testUpdateDesiredWeightWithStickyConfig(t *testing.T, ro *v1alpha1.Rollout, ingresses []*networkingv1.Ingress) {
	client := fake.NewSimpleClientset(ingressesToObjects(ingresses)...)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	for _, ingress := range ingresses {
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(ingress)
	}
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
	assert.Nil(t, err)
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})
	assert.NoError(t, err)
	err = r.SetWeight(10)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), len(ingresses))
}
func TestUpdateDesiredWeightWithStickyConfig(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, true)
	testUpdateDesiredWeightWithStickyConfig(t, ro, []*networkingv1.Ingress{i})
}

func TestUpdateDesiredWeightWithStickyConfigMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, true)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, true)
	testUpdateDesiredWeightWithStickyConfig(t, ro, []*networkingv1.Ingress{i, mi})
}

func TestUpdateDesiredWeightWithStickyConfigServicePorts(t *testing.T) {
	ro := fakeRolloutWithServicePorts(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"})
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 5, ro.Name, true)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 5, ro.Name, true)
	testUpdateDesiredWeightWithStickyConfig(t, ro, []*networkingv1.Ingress{i, mi})
}

func testUpdateDesiredWeight(t *testing.T, ro *v1alpha1.Rollout, ingresses []*networkingv1.Ingress) {
	client := fake.NewSimpleClientset(ingressesToObjects(ingresses)...)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	for _, ingress := range ingresses {
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(ingress)
	}
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})
	assert.NoError(t, err)
	err = r.SetWeight(10)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), len(ingresses))
}
func TestUpdateDesiredWeight(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
	testUpdateDesiredWeight(t, ro, []*networkingv1.Ingress{i})
}

func TestUpdateDesiredWeightMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
	testUpdateDesiredWeight(t, ro, []*networkingv1.Ingress{i, mi})
}
func TestUpdateDesiredWeightServicePorts(t *testing.T) {
	ro := fakeRolloutWithServicePorts(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"})
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 5, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 5, ro.Name, false)
	testUpdateDesiredWeight(t, ro, []*networkingv1.Ingress{i, mi})
}

// TestGetForwardActionStringMarshalsZeroCorrectly ensures that the annotation does not omit default value zero when marshalling
// the forward action
func TestGetForwardActionStringMarshalsZeroCorrectly(t *testing.T) {
	r := fakeRollout("stable", "canary", nil, "ingress", 443)
	forwardAction, err := getForwardActionString(r, []int32{443}, 0)
	if err != nil {
		t.Fatal(err)
	}
	assert.Contains(t, forwardAction, `"Weight":0`)
}

func TestGetForwardActionStringMarshalsDisabledStickyConfigCorrectly(t *testing.T) {
	r := fakeRollout("stable", "canary", nil, "ingress", 443)
	stickinessConfig := v1alpha1.StickinessConfig{
		Enabled:         false,
		DurationSeconds: 0,
	}
	r.Spec.Strategy.Canary.TrafficRouting.ALB.StickinessConfig = &stickinessConfig
	forwardAction, err := getForwardActionString(r, []int32{443}, 0)
	if err != nil {
		t.Fatal(err)
	}
	assert.Contains(t, forwardAction, `"Weight":0`)
}

func TestGetForwardActionStringDetectsNegativeStickyConfigDuration(t *testing.T) {
	r := fakeRollout("stable", "canary", nil, "ingress", 443)
	stickinessConfig := v1alpha1.StickinessConfig{
		Enabled:         true,
		DurationSeconds: 0,
	}
	r.Spec.Strategy.Canary.TrafficRouting.ALB.StickinessConfig = &stickinessConfig
	forwardAction, err := getForwardActionString(r, []int32{443}, 0)

	assert.NotNilf(t, forwardAction, "There should be no forwardAction being generated: %v", forwardAction)
	expectedErrorMsg := "TargetGroupStickinessConfig's duration must be between 1 and 604800 seconds (7 days)!"
	assert.EqualErrorf(t, err, expectedErrorMsg, "Error should be: %v, got: %v", expectedErrorMsg, err)
}

func TestGetForwardActionStringDetectsTooLargeStickyConfigDuration(t *testing.T) {
	r := fakeRollout("stable", "canary", nil, "ingress", 443)
	stickinessConfig := v1alpha1.StickinessConfig{
		Enabled:         true,
		DurationSeconds: 604800 + 1,
	}
	r.Spec.Strategy.Canary.TrafficRouting.ALB.StickinessConfig = &stickinessConfig
	forwardAction, err := getForwardActionString(r, []int32{443}, 0)

	assert.NotNilf(t, forwardAction, "There should be no forwardAction being generated: %v", forwardAction)
	expectedErrorMsg := "TargetGroupStickinessConfig's duration must be between 1 and 604800 seconds (7 days)!"
	assert.EqualErrorf(t, err, expectedErrorMsg, "Error should be: %v, got: %v", expectedErrorMsg, err)
}

func testErrorPatching(t *testing.T, ro *v1alpha1.Rollout, ingresses []*networkingv1.Ingress) {
	client := fake.NewSimpleClientset(ingressesToObjects(ingresses)...)
	client.ReactionChain = nil
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	for _, ingress := range ingresses {
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(ingress)
	}
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})
	assert.NoError(t, err)

	errMessage := "some error occurred"
	r.cfg.Client.(*fake.Clientset).Fake.AddReactor("patch", "ingresses", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New(errMessage)
	})

	err = r.SetWeight(10)
	assert.Error(t, err, "some error occurred")
	assert.Len(t, client.Actions(), 1)
}
func TestErrorPatching(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)

	testErrorPatching(t, ro, []*networkingv1.Ingress{i})
}

func TestErrorPatchingMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
	testErrorPatching(t, ro, []*networkingv1.Ingress{i, mi})
}

func TestErrorPatchingServicePorts(t *testing.T) {
	ro := fakeRolloutWithServicePorts(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"})
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 5, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 5, ro.Name, false)
	testErrorPatching(t, ro, []*networkingv1.Ingress{i, mi})
}

type fakeAWSClientBalancerFetchError struct {
	Error   error
	DNSName string
}
type fakeAWSTargetGroupFetchError struct {
	Error           error
	LoadBalancerARN string
}
type fakeAWSClient struct {
	Ingresses                []string
	targetGroups             []aws.TargetGroupMeta
	targetGroupsErrors       []fakeAWSTargetGroupFetchError
	loadBalancers            []*elbv2types.LoadBalancer
	loadBalancerErrors       []fakeAWSClientBalancerFetchError
	targetHealthDescriptions []elbv2types.TargetHealthDescription
}

func (f *fakeAWSClient) GetTargetGroupMetadata(ctx context.Context, loadBalancerARN string) ([]aws.TargetGroupMeta, error) {
	result := []aws.TargetGroupMeta{}
	for _, lb := range f.targetGroupsErrors {
		if lb.LoadBalancerARN == loadBalancerARN {
			return nil, lb.Error
		}
	}
	for _, tg := range f.targetGroups {
		if slices.Contains(tg.LoadBalancerArns, loadBalancerARN) {
			result = append(result, tg)
		}
	}
	return result, nil
}

func (f *fakeAWSClient) FindLoadBalancerByDNSName(ctx context.Context, dnsName string) (*elbv2types.LoadBalancer, error) {
	for _, lb := range f.loadBalancerErrors {
		if lb.DNSName == dnsName {
			return nil, lb.Error
		}
	}
	for _, lb := range f.loadBalancers {
		if lb.DNSName != nil && *lb.DNSName == dnsName {
			return lb, nil
		}
	}
	return nil, nil
}

func (f *fakeAWSClient) GetTargetGroupHealth(ctx context.Context, targetGroupARN string) ([]elbv2types.TargetHealthDescription, error) {
	return f.targetHealthDescriptions, nil
}

func (f *fakeAWSClient) getAlbStatus(ingress string) *v1alpha1.ALBStatus {
	LoadBalancerFullName := ""
	if lbArnParts := strings.Split(*f.loadBalancers[0].LoadBalancerArn, "/"); len(lbArnParts) > 2 {
		LoadBalancerFullName = strings.Join(lbArnParts[2:], "/")
	}
	CanaryTargetGroupFullName := ""
	if tgArnParts := strings.Split(*f.targetGroups[0].TargetGroupArn, "/"); len(tgArnParts) > 1 {
		CanaryTargetGroupFullName = strings.Join(tgArnParts[1:], "/")
	}
	StableTargetGroupFullName := ""
	if tgArnParts := strings.Split(*f.targetGroups[len(f.targetGroups)-1].TargetGroupArn, "/"); len(tgArnParts) > 1 {
		StableTargetGroupFullName = strings.Join(tgArnParts[1:], "/")
	}
	return &v1alpha1.ALBStatus{
		Ingress: ingress,
		LoadBalancer: v1alpha1.AwsResourceRef{
			Name:     *f.loadBalancers[0].LoadBalancerName,
			ARN:      *f.loadBalancers[0].LoadBalancerArn,
			FullName: LoadBalancerFullName,
		},
		CanaryTargetGroup: v1alpha1.AwsResourceRef{
			Name:     *f.targetGroups[0].TargetGroupName,
			ARN:      *f.targetGroups[0].TargetGroupArn,
			FullName: CanaryTargetGroupFullName,
		},
		StableTargetGroup: v1alpha1.AwsResourceRef{
			Name:     *f.targetGroups[len(f.targetGroups)-1].TargetGroupName,
			ARN:      *f.targetGroups[len(f.targetGroups)-1].TargetGroupArn,
			FullName: StableTargetGroupFullName,
		},
	}
}

func (f *fakeAWSClient) getAlbStatusMultiIngress(ingress string, lbIdx int32, tgIdx int32) *v1alpha1.ALBStatus {
	LoadBalancerFullName := ""
	if lbArnParts := strings.Split(*f.loadBalancers[lbIdx].LoadBalancerArn, "/"); len(lbArnParts) > 2 {
		LoadBalancerFullName = strings.Join(lbArnParts[2:], "/")
	}
	CanaryTargetGroupFullName := ""
	if tgArnParts := strings.Split(*f.targetGroups[tgIdx].TargetGroupArn, "/"); len(tgArnParts) > 1 {
		CanaryTargetGroupFullName = strings.Join(tgArnParts[1:], "/")
	}
	StableTargetGroupFullName := ""
	if tgArnParts := strings.Split(*f.targetGroups[tgIdx+1].TargetGroupArn, "/"); len(tgArnParts) > 1 {
		StableTargetGroupFullName = strings.Join(tgArnParts[1:], "/")
	}
	return &v1alpha1.ALBStatus{
		Ingress: ingress,
		LoadBalancer: v1alpha1.AwsResourceRef{
			Name:     *f.loadBalancers[lbIdx].LoadBalancerName,
			ARN:      *f.loadBalancers[lbIdx].LoadBalancerArn,
			FullName: LoadBalancerFullName,
		},
		CanaryTargetGroup: v1alpha1.AwsResourceRef{
			Name:     *f.targetGroups[tgIdx].TargetGroupName,
			ARN:      *f.targetGroups[tgIdx].TargetGroupArn,
			FullName: CanaryTargetGroupFullName,
		},
		StableTargetGroup: v1alpha1.AwsResourceRef{
			Name:     *f.targetGroups[tgIdx+1].TargetGroupName,
			ARN:      *f.targetGroups[tgIdx+1].TargetGroupArn,
			FullName: StableTargetGroupFullName,
		},
	}
}

func TestVerifyWeight(t *testing.T) {
	newFakeReconciler := func(status *v1alpha1.RolloutStatus) (*Reconciler, *fakeAWSClient) {
		ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
		ro.Status.StableRS = "a45fe23"
		ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetWeight: pointer.Int32(10),
		}}
		i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
		i.Status.LoadBalancer = networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{
				{
					Hostname: "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com",
				},
			},
		}

		client := fake.NewSimpleClientset(i)
		k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(i)
		ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
		if err != nil {
			t.Fatal(err)
		}
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
			IngressWrapper: ingressWrapper,
			VerifyWeight:   pointer.BoolPtr(true),
			Status:         status,
		})
		assert.NoError(t, err)
		fakeAWS := fakeAWSClient{}
		r.aws = &fakeAWS
		return r, &fakeAWS
	}

	// LoadBalancer not found
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.NotNil(t, status.ALB)
		assert.Len(t, status.ALBs, 1)
	}

	// VerifyWeight not needed
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		status.StableRS = ""
		r.cfg.Rollout.Status.StableRS = ""
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.NotNil(t, status.ALB)
		assert.Len(t, status.ALBs, 1)
	}

	// VerifyWeight that we do not need to verify weight and status.ALB is already set
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		r.cfg.Rollout.Status.ALB = &v1alpha1.ALBStatus{}
		r.cfg.Rollout.Status.CurrentStepIndex = nil
		r.cfg.Rollout.Spec.Strategy.Canary.Steps = nil
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.Nil(t, weightVerified)
	}

	// LoadBalancer found, not at weight
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 11, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 89, "default/ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *status.ALB)
		assert.Equal(t, *status.ALB, *fakeClient.getAlbStatus("ingress"))
	}

	// LoadBalancer found, at weight
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)

		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 10, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 90, "default/ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.True(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *status.ALB)
		assert.Equal(t, *status.ALB, *fakeClient.getAlbStatus("ingress"))
	}

	// LoadBalancer found, at max weight, end of rollout
	{
		var status v1alpha1.RolloutStatus
		status.CurrentStepIndex = pointer.Int32Ptr(2)
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 100, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 0, "default/ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(100)
		assert.NoError(t, err)
		assert.True(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *status.ALB)
		assert.Equal(t, *status.ALB, *fakeClient.getAlbStatus("ingress"))
	}

	// LoadBalancer found, but ARNs are unparsable
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			{
				LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
				LoadBalancerArn:  pointer.StringPtr("lb-abc123-arn"),
				DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			},
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					LoadBalancerArns: []string{"lb-abc123-arn"},
					TargetGroupName:  pointer.StringPtr("canary-tg-abc123-name"),
					TargetGroupArn:   pointer.StringPtr("canary-tg-abc123-arn"),
				},
				Weight: pointer.Int32(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					LoadBalancerArns: []string{"lb-abc123-arn"},
					TargetGroupName:  pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:   pointer.StringPtr("stable-tg-abc123-arn"),
				},
				Weight: pointer.Int32(11),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-stable-svc:443",
				},
			},
		}

		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.True(t, *weightVerified)
		albStatus := *fakeClient.getAlbStatus("ingress")
		assert.Equal(t, albStatus.LoadBalancer.FullName, "")
		assert.Equal(t, albStatus.CanaryTargetGroup.FullName, "")
		assert.Equal(t, albStatus.StableTargetGroup.FullName, "")
	}
}

func TestVerifyWeightSingleIngressMultiplePorts(t *testing.T) {
	newFakeReconciler := func(status *v1alpha1.RolloutStatus) (*Reconciler, *fakeAWSClient) {
		ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
		// set multiple service ports
		ro.Spec.Strategy.Canary.TrafficRouting.ALB.ServicePorts = []v1alpha1.ALBIngressWithPorts{
			{Ingress: "ingress", ServicePorts: []int32{80, 443}},
		}
		ro.Status.StableRS = "a45fe23"
		ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetWeight: pointer.Int32(10),
		}}
		i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 5, ro.Name, false)
		i.Status.LoadBalancer = networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{
				{
					Hostname: "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com",
				},
			},
		}

		client := fake.NewSimpleClientset(i)
		k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(i)
		ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
		if err != nil {
			t.Fatal(err)
		}
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
			IngressWrapper: ingressWrapper,
			VerifyWeight:   pointer.BoolPtr(true),
			Status:         status,
		})
		assert.NoError(t, err)
		fakeAWS := fakeAWSClient{}
		r.aws = &fakeAWS
		return r, &fakeAWS
	}

	// LoadBalancer not found
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.NotNil(t, status.ALB)
		assert.Len(t, status.ALBs, 1)
	}

	// VerifyWeight not needed
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		status.StableRS = ""
		r.cfg.Rollout.Status.StableRS = ""
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.NotNil(t, status.ALB)
		assert.Len(t, status.ALBs, 1)
	}

	// VerifyWeight that we do not need to verify weight and status.ALB is already set
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		r.cfg.Rollout.Status.ALB = &v1alpha1.ALBStatus{}
		r.cfg.Rollout.Status.CurrentStepIndex = nil
		r.cfg.Rollout.Spec.Strategy.Canary.Steps = nil
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.Nil(t, weightVerified)
	}

	// LoadBalancer found, not at weight
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 80, 11, "default/ingress-canary-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 11, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 80, 89, "default/ingress-stable-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 89, "default/ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *status.ALB)
		assert.Equal(t, *status.ALB, *fakeClient.getAlbStatus("ingress"))
	}

	// LoadBalancer found, at weight
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 80, 10, "default/ingress-canary-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 10, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 80, 90, "default/ingress-stable-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 90, "default/ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.True(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *status.ALB)
		assert.Equal(t, *status.ALB, *fakeClient.getAlbStatus("ingress"))
	}
}

func TestVerifyWeightMultiIngress(t *testing.T) {
	newFakeReconciler := func(status *v1alpha1.RolloutStatus) (*Reconciler, *fakeAWSClient) {
		ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
		ro.Status.StableRS = "a45fe23"
		ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetWeight: pointer.Int32(10),
		}}
		i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
		mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
		i.Status.LoadBalancer = networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{
				{
					Hostname: "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com",
				},
			},
		}
		mi.Status.LoadBalancer = networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{
				{
					Hostname: "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com",
				},
			},
		}

		client := fake.NewSimpleClientset(i, mi)
		k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(i)
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(mi)
		ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
		if err != nil {
			t.Fatal(err)
		}
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
			IngressWrapper: ingressWrapper,
			VerifyWeight:   pointer.BoolPtr(true),
			Status:         status,
		})
		assert.NoError(t, err)
		fakeAWS := fakeAWSClient{}
		r.aws = &fakeAWS
		return r, &fakeAWS
	}

	// LoadBalancer not found
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.NotNil(t, status.ALB)
		assert.Len(t, status.ALBs, 2)
	}

	// VerifyWeight not needed
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		status.StableRS = ""
		r.cfg.Rollout.Status.StableRS = ""
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.NotNil(t, status.ALB)
		assert.Len(t, status.ALBs, 2)
	}

	// VerifyWeight that we do not need to verify weight and status.ALB is already set
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		r.cfg.Rollout.Status.ALBs = []v1alpha1.ALBStatus{}
		r.cfg.Rollout.Status.CurrentStepIndex = nil
		r.cfg.Rollout.Spec.Strategy.Canary.Steps = nil
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.Nil(t, weightVerified)
	}

	// status.ALBs already set, len not match
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		r.cfg.Status.ALBs = []v1alpha1.ALBStatus{{}}
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.Len(t, status.ALBs, 2)
	}

	// LoadBalancer found, not at weight
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			makeLoadBalancer("lb-multi-ingress-name", "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 11, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 89, "default/ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 11, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 443, 89, "default/multi-ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *fakeClient.getAlbStatusMultiIngress("ingress", 0, 0))
		assert.Equal(t, status.ALBs[1], *fakeClient.getAlbStatusMultiIngress("multi-ingress", 1, 2))
	}

	// LoadBalancer found, at weight
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			makeLoadBalancer("lb-multi-ingress-name", "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 10, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 90, "default/ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 10, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 443, 90, "default/multi-ingress-stable-svc:443"),
		}
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.True(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *fakeClient.getAlbStatusMultiIngress("ingress", 0, 0))
		assert.Equal(t, status.ALBs[1], *fakeClient.getAlbStatusMultiIngress("multi-ingress", 1, 2))
	}

	// LoadBalancer found, dns-mismatch
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "broken-dns-verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			makeLoadBalancer("lb-multi-ingress-name", "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 11, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 443, 89, "default/multi-ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
	}

	// LoadBalancer found, but balancer fetch failed with error
	{
		expectedError := k8serrors.NewBadRequest("failed to fetch load balancer")
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancerErrors = []fakeAWSClientBalancerFetchError{{
			DNSName: "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com",
			Error:   expectedError,
		}}
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-multi-ingress-name", "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 11, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 443, 89, "default/multi-ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(10)
		assert.Equal(t, err, expectedError)
		assert.False(t, *weightVerified)
	}

	// LoadBalancer found, but target group fetch failed with error
	{
		expectedError := k8serrors.NewBadRequest("failed to fetch target group")
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			makeLoadBalancer("lb-multi-ingress-name", "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 11, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 443, 89, "default/multi-ingress-stable-svc:443"),
		}
		fakeClient.targetGroupsErrors = []fakeAWSTargetGroupFetchError{{
			LoadBalancerARN: *fakeClient.loadBalancers[0].LoadBalancerArn,
			Error:           expectedError,
		}}

		weightVerified, err := r.VerifyWeight(10)
		assert.Equal(t, err, expectedError)
		assert.False(t, *weightVerified)
	}
}

func TestVerifyWeightServicePorts(t *testing.T) {
	newFakeReconciler := func(status *v1alpha1.RolloutStatus) (*Reconciler, *fakeAWSClient) {
		ro := fakeRolloutWithServicePorts(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"})
		ro.Status.StableRS = "a45fe23"
		ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetWeight: pointer.Int32(10),
		}}
		i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 5, ro.Name, false)
		mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 5, ro.Name, false)
		i.Status.LoadBalancer = networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{
				{
					Hostname: "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com",
				},
			},
		}
		mi.Status.LoadBalancer = networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{
				{
					Hostname: "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com",
				},
			},
		}

		client := fake.NewSimpleClientset(i, mi)
		k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(i)
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(mi)
		ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
		if err != nil {
			t.Fatal(err)
		}
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
			IngressWrapper: ingressWrapper,
			VerifyWeight:   pointer.BoolPtr(true),
			Status:         status,
		})
		assert.NoError(t, err)
		fakeAWS := fakeAWSClient{}
		r.aws = &fakeAWS
		return r, &fakeAWS
	}

	// LoadBalancer not found
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.NotNil(t, status.ALB)
		assert.Len(t, status.ALBs, 2)
	}

	// VerifyWeight not needed
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		status.StableRS = ""
		r.cfg.Rollout.Status.StableRS = ""
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.NotNil(t, status.ALB)
		assert.Len(t, status.ALBs, 2)
	}

	// VerifyWeight that we do not need to verify weight and status.ALB is already set
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		r.cfg.Rollout.Status.ALBs = []v1alpha1.ALBStatus{}
		r.cfg.Rollout.Status.CurrentStepIndex = nil
		r.cfg.Rollout.Spec.Strategy.Canary.Steps = nil
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.Nil(t, weightVerified)
	}

	// status.ALBs already set, len not match
	{
		var status v1alpha1.RolloutStatus
		r, _ := newFakeReconciler(&status)
		r.cfg.Status.ALBs = []v1alpha1.ALBStatus{{}}
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.Len(t, status.ALBs, 2)
	}

	// LoadBalancer found, not at weight
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			makeLoadBalancer("lb-multi-ingress-name", "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 80, 11, "default/ingress-canary-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 80, 89, "default/ingress-stable-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 11, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 89, "default/ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 80, 11, "default/multi-ingress-canary-svc:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 80, 89, "default/multi-ingress-stable-svc:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 11, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 443, 89, "default/multi-ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *fakeClient.getAlbStatusMultiIngress("ingress", 0, 0))
		assert.Equal(t, status.ALBs[1], *fakeClient.getAlbStatusMultiIngress("multi-ingress", 1, 4))
	}

	// LoadBalancer found, at weight
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			makeLoadBalancer("lb-multi-ingress-name", "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 80, 10, "default/ingress-canary-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 80, 90, "default/ingress-stable-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 10, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 90, "default/ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 80, 10, "default/multi-ingress-canary-svc:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 80, 90, "default/multi-ingress-stable-svc:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 10, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 443, 90, "default/multi-ingress-stable-svc:443"),
		}
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.True(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *fakeClient.getAlbStatusMultiIngress("ingress", 0, 0))
		assert.Equal(t, status.ALBs[1], *fakeClient.getAlbStatusMultiIngress("multi-ingress", 1, 4))
	}
}

func testSetWeightWithMultipleBackends(t *testing.T, ro *v1alpha1.Rollout, ingresses []*networkingv1.Ingress, ports []int32) {
	client := fake.NewSimpleClientset(ingressesToObjects(ingresses)...)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	for _, ingress := range ingresses {
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(ingress)
	}
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})
	assert.NoError(t, err)

	weightDestinations := []v1alpha1.WeightDestination{
		{
			ServiceName:     "ex-svc-1",
			PodTemplateHash: "",
			Weight:          2,
		},
		{
			ServiceName:     "ex-svc-2",
			PodTemplateHash: "",
			Weight:          3,
		},
	}
	err = r.SetWeight(10, weightDestinations...)
	assert.Nil(t, err)

	actions := client.Actions()
	assert.Len(t, client.Actions(), len(ingresses))
	assert.Equal(t, "patch", actions[0].GetVerb())

	for i := range ingresses {
		patchedI := networkingv1.Ingress{}
		err = json.Unmarshal(actions[i].(k8stesting.PatchAction).GetPatch(), &patchedI)
		assert.Nil(t, err)

		dest := make([]v1alpha1.WeightDestination, 0, 2+len(weightDestinations))
		dest = append(dest, v1alpha1.WeightDestination{ServiceName: CANARY_SVC, Weight: 10})
		dest = append(dest, weightDestinations...)
		dest = append(dest, v1alpha1.WeightDestination{ServiceName: STABLE_SVC, Weight: 85})
		expectedAction := albActionForwardFromDestinations(false, ports, dest...)
		assert.Equal(t, expectedAction, patchedI.Annotations["alb.ingress.kubernetes.io/actions.stable-svc"])
	}
}
func TestSetWeightWithMultipleBackends(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 0, ro.Name, false)
	testSetWeightWithMultipleBackends(t, ro, []*networkingv1.Ingress{i}, []int32{443})
}

func TestSetWeightWithMultipleBackendsMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 0, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 0, ro.Name, false)
	testSetWeightWithMultipleBackends(t, ro, []*networkingv1.Ingress{i, mi}, []int32{443})
}

func TestSetWeightWithMultipleBackendsServicePorts(t *testing.T) {
	ports := []int32{80, 443}
	ro := fakeRolloutWithServicePorts(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"})
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, ports, 0, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, ports, 0, ro.Name, false)
	testSetWeightWithMultipleBackends(t, ro, []*networkingv1.Ingress{i, mi}, ports)
}

func makeLoadBalancer(name string, dnsName string) *elbv2types.LoadBalancer {
	lbName := strings.Clone(name)
	lbDNS := strings.Clone(dnsName)
	return &elbv2types.LoadBalancer{
		LoadBalancerName: pointer.StringPtr(lbName),
		LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/" + name + "/1234567890123456"),
		DNSName:          pointer.StringPtr(lbDNS),
	}
}
func makeTargetGroup(name string, port int32, weight int32, lbResourceId string) aws.TargetGroupMeta {
	tgName := strings.Clone(name)
	tgPort := port
	tgWeight := weight
	return aws.TargetGroupMeta{
		TargetGroup: elbv2types.TargetGroup{
			LoadBalancerArns: []string{},
			Port:             pointer.Int32(tgPort),
			TargetGroupName:  pointer.StringPtr(tgName),
			TargetGroupArn:   pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/" + tgName + "/1234567890123456"),
		},
		Weight: pointer.Int32(tgWeight),
		Tags: map[string]string{
			aws.AWSLoadBalancerV2TagKeyResourceID: lbResourceId,
		},
	}
}
func makeTargetGroupForBalancer(lbName string, tgName string, port int32, weight int32, lbResourceId string) aws.TargetGroupMeta {
	result := makeTargetGroup(tgName, port, weight, lbResourceId)
	result.TargetGroup.LoadBalancerArns = append(
		result.TargetGroup.LoadBalancerArns,
		"arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/"+lbName+"/1234567890123456",
	)

	return result
}

func TestVerifyWeightWithAdditionalDestinations(t *testing.T) {
	weightDestinations := []v1alpha1.WeightDestination{
		{
			ServiceName:     "ex-svc-1",
			PodTemplateHash: "",
			Weight:          2,
		},
		{
			ServiceName:     "ex-svc-2",
			PodTemplateHash: "",
			Weight:          3,
		},
	}
	newFakeReconciler := func(status *v1alpha1.RolloutStatus) (*Reconciler, *fakeAWSClient) {
		ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
		ro.Status.StableRS = "a45fe23"
		ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetWeight: pointer.Int32(10),
		}}
		i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 0, ro.Name, false)
		i.Annotations["alb.ingress.kubernetes.io/actions.stable-svc"] = fmt.Sprintf(actionTemplateWithExperiments, CANARY_SVC, 443, 10, weightDestinations[0].ServiceName, 443, weightDestinations[0].Weight, weightDestinations[1].ServiceName, 443, weightDestinations[1].Weight, STABLE_SVC, []int32{443}, 85)

		i.Status.LoadBalancer = networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{
				{
					Hostname: "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com",
				},
			},
		}

		client := fake.NewSimpleClientset(i)
		k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(i)
		ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
		if err != nil {
			t.Fatal(err)
		}
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
			IngressWrapper: ingressWrapper,
			VerifyWeight:   pointer.BoolPtr(true),
			Status:         status,
		})
		assert.NoError(t, err)
		fakeAWS := fakeAWSClient{}
		r.aws = &fakeAWS
		return r, &fakeAWS
	}

	// LoadBalancer found, but experiment weights not present
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 10, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 90, "default/ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(10, weightDestinations...)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.Equal(t, *status.ALB, *fakeClient.getAlbStatus("ingress"))
	}

	// LoadBalancer found, with incorrect weights
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 10, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 85, "default/ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 10, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-1-tg-abc123-name", 443, 100, "default/ingress-ex-svc-1:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-2-tg-abc123-name", 443, 100, "default/ingress-ex-svc-2:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 90, "default/ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(10, weightDestinations...)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.Equal(t, *status.ALB, *fakeClient.getAlbStatus("ingress"))
	}

	// LoadBalancer found, with all correct weights
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 10, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-1-tg-abc123-name", 443, weightDestinations[0].Weight, "default/ingress-ex-svc-1:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-2-tg-abc123-name", 443, weightDestinations[1].Weight, "default/ingress-ex-svc-2:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 85, "default/ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(10, weightDestinations...)
		assert.NoError(t, err)
		assert.True(t, *weightVerified)
		assert.Equal(t, *status.ALB, *fakeClient.getAlbStatus("ingress"))
	}
}

func TestVerifyWeightWithAdditionalDestinationsMultiIngress(t *testing.T) {
	weightDestinations := []v1alpha1.WeightDestination{
		{
			ServiceName:     "ex-svc-1",
			PodTemplateHash: "",
			Weight:          2,
		},
		{
			ServiceName:     "ex-svc-2",
			PodTemplateHash: "",
			Weight:          3,
		},
	}
	newFakeReconciler := func(status *v1alpha1.RolloutStatus) (*Reconciler, *fakeAWSClient) {
		ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
		ro.Status.StableRS = "a45fe23"
		ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetWeight: pointer.Int32(10),
		}}
		i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 0, ro.Name, false)
		mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 5, ro.Name, false)
		i.Annotations["alb.ingress.kubernetes.io/actions.stable-svc"] = fmt.Sprintf(actionTemplateWithExperiments, CANARY_SVC, 443, 10, weightDestinations[0].ServiceName, 443, weightDestinations[0].Weight, weightDestinations[1].ServiceName, 443, weightDestinations[1].Weight, STABLE_SVC, []int32{443}, 85)
		mi.Annotations["alb.ingress.kubernetes.io/actions.stable-svc"] = fmt.Sprintf(actionTemplateWithExperiments, CANARY_SVC, 443, 10, weightDestinations[0].ServiceName, 443, weightDestinations[0].Weight, weightDestinations[1].ServiceName, 443, weightDestinations[1].Weight, STABLE_SVC, []int32{443}, 85)

		i.Status.LoadBalancer = networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{
				{
					Hostname: "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com",
				},
			},
		}
		mi.Status.LoadBalancer = networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{
				{
					Hostname: "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com",
				},
			},
		}

		client := fake.NewSimpleClientset(i, mi)
		k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(i)
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(mi)
		ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
		if err != nil {
			t.Fatal(err)
		}
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
			IngressWrapper: ingressWrapper,
			VerifyWeight:   pointer.BoolPtr(true),
			Status:         status,
		})
		assert.NoError(t, err)
		fakeAWS := fakeAWSClient{}
		r.aws = &fakeAWS
		return r, &fakeAWS
	}

	// LoadBalancer found, but experiment weights not present
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			makeLoadBalancer("lb-multi-ingress-name", "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 10, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 90, "default/ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 10, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 443, 90, "default/multi-ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(10, weightDestinations...)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *fakeClient.getAlbStatusMultiIngress("ingress", 0, 0))
	}

	// LoadBalancer found, with incorrect weights
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			makeLoadBalancer("lb-multi-ingress-name", "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 10, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 85, "default/ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 10, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 443, 85, "default/multi-ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-1-tg-abc123-name", 443, 100, "default/ingress-ex-svc-1:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-2-tg-abc123-name", 443, 100, "default/ingress-ex-svc-2:443"),
		}

		weightVerified, err := r.VerifyWeight(10, weightDestinations...)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *fakeClient.getAlbStatusMultiIngress("ingress", 0, 0))
		assert.Equal(t, status.ALBs[1], *fakeClient.getAlbStatusMultiIngress("multi-ingress", 1, 2))
	}

	// LoadBalancer found, with all correct weights
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			makeLoadBalancer("lb-multi-ingress-name", "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 10, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 85, "default/ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 10, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 443, 85, "default/multi-ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-1-tg-abc123-name", 443, weightDestinations[0].Weight, "default/ingress-ex-svc-1:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-2-tg-abc123-name", 443, weightDestinations[1].Weight, "default/ingress-ex-svc-2:443"),
			// because both balancers should have experiment target groups
			makeTargetGroupForBalancer("lb-multi-ingress-name", "ex-svc-1-tg-abc123-name", 443, weightDestinations[0].Weight, "default/multi-ingress-ex-svc-1:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "ex-svc-2-tg-abc123-name", 443, weightDestinations[1].Weight, "default/multi-ingress-ex-svc-2:443"),
		}

		weightVerified, err := r.VerifyWeight(10, weightDestinations...)
		assert.NoError(t, err)
		assert.True(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *fakeClient.getAlbStatusMultiIngress("ingress", 0, 0))
	}
}

func TestVerifyWeightWithAdditionalDestinationsServicePorts(t *testing.T) {
	weightDestinations := []v1alpha1.WeightDestination{
		{
			ServiceName:     "ex-svc-1",
			PodTemplateHash: "",
			Weight:          2,
		},
		{
			ServiceName:     "ex-svc-2",
			PodTemplateHash: "",
			Weight:          3,
		},
	}
	newFakeReconciler := func(status *v1alpha1.RolloutStatus) (*Reconciler, *fakeAWSClient) {
		ro := fakeRolloutWithServicePorts(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"})
		ro.Status.StableRS = "a45fe23"
		ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetWeight: pointer.Int32(10),
		}}
		i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 0, ro.Name, false)
		mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 5, ro.Name, false)
		i.Annotations["alb.ingress.kubernetes.io/actions.stable-svc"] = fmt.Sprintf(actionTemplateWithExperiments, CANARY_SVC, 443, 10, weightDestinations[0].ServiceName, 443, weightDestinations[0].Weight, weightDestinations[1].ServiceName, 443, weightDestinations[1].Weight, STABLE_SVC, []int32{443}, 85)
		mi.Annotations["alb.ingress.kubernetes.io/actions.stable-svc"] = fmt.Sprintf(actionTemplateWithExperiments, CANARY_SVC, 443, 10, weightDestinations[0].ServiceName, 443, weightDestinations[0].Weight, weightDestinations[1].ServiceName, 443, weightDestinations[1].Weight, STABLE_SVC, []int32{443}, 85)

		i.Status.LoadBalancer = networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{
				{
					Hostname: "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com",
				},
			},
		}
		mi.Status.LoadBalancer = networkingv1.IngressLoadBalancerStatus{
			Ingress: []networkingv1.IngressLoadBalancerIngress{
				{
					Hostname: "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com",
				},
			},
		}

		client := fake.NewSimpleClientset(i, mi)
		k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(i)
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(mi)
		ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
		if err != nil {
			t.Fatal(err)
		}
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
			IngressWrapper: ingressWrapper,
			VerifyWeight:   pointer.BoolPtr(true),
			Status:         status,
		})
		assert.NoError(t, err)
		fakeAWS := fakeAWSClient{}
		r.aws = &fakeAWS
		return r, &fakeAWS
	}

	// LoadBalancer found, but experiment weights not present
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			makeLoadBalancer("lb-multi-ingress-name", "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 80, 10, "default/ingress-canary-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 80, 90, "default/ingress-stable-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 10, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 90, "default/ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 80, 10, "default/multi-ingress-canary-svc:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 80, 90, "default/multi-ingress-stable-svc:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 10, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 443, 90, "default/multi-ingress-stable-svc:443"),
		}

		weightVerified, err := r.VerifyWeight(10, weightDestinations...)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *fakeClient.getAlbStatusMultiIngress("ingress", 0, 0))
	}

	// LoadBalancer found, with incorrect weights
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			makeLoadBalancer("lb-multi-ingress-name", "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 80, 10, "default/ingress-canary-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 80, 85, "default/ingress-stable-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 10, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 85, "default/ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 80, 10, "default/multi-ingress-canary-svc:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 80, 85, "default/multi-ingress-stable-svc:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 10, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 443, 85, "default/multi-ingress-stable-svc:443"),
			// first one has incorrect weights
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-1-tg-abc123-name", 80, 100, "default/ingress-ex-svc-1:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-2-tg-abc123-name", 80, 100, "default/ingress-ex-svc-2:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-1-tg-abc123-name", 443, 100, "default/ingress-ex-svc-1:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-2-tg-abc123-name", 443, 100, "default/ingress-ex-svc-2:443"),
			// second one has proper weights
			makeTargetGroupForBalancer("lb-multi-ingress-name", "ex-svc-1-tg-abc123-name", 80, weightDestinations[0].Weight, "default/multi-ingress-ex-svc-1:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "ex-svc-2-tg-abc123-name", 80, weightDestinations[1].Weight, "default/multi-ingress-ex-svc-2:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "ex-svc-1-tg-abc123-name", 443, weightDestinations[0].Weight, "default/multi-ingress-ex-svc-1:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "ex-svc-2-tg-abc123-name", 443, weightDestinations[1].Weight, "default/multi-ingress-ex-svc-2:443"),
		}

		weightVerified, err := r.VerifyWeight(10, weightDestinations...)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *fakeClient.getAlbStatusMultiIngress("ingress", 0, 0))
		assert.Equal(t, status.ALBs[1], *fakeClient.getAlbStatusMultiIngress("multi-ingress", 1, 4))
	}

	// LoadBalancer found, with all correct weights
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancers = []*elbv2types.LoadBalancer{
			makeLoadBalancer("lb-abc123-name", "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			makeLoadBalancer("lb-multi-ingress-name", "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 80, 10, "default/ingress-canary-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 80, 85, "default/ingress-stable-svc:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "canary-tg-abc123-name", 443, 10, "default/ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "stable-tg-abc123-name", 443, 85, "default/ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 80, 10, "default/multi-ingress-canary-svc:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 80, 85, "default/multi-ingress-stable-svc:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-canary-tg-abc123-name", 443, 10, "default/multi-ingress-canary-svc:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "multi-ingress-stable-tg-abc123-name", 443, 85, "default/multi-ingress-stable-svc:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-1-tg-abc123-name", 80, weightDestinations[0].Weight, "default/ingress-ex-svc-1:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-2-tg-abc123-name", 80, weightDestinations[1].Weight, "default/ingress-ex-svc-2:80"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-1-tg-abc123-name", 443, weightDestinations[0].Weight, "default/ingress-ex-svc-1:443"),
			makeTargetGroupForBalancer("lb-abc123-name", "ex-svc-2-tg-abc123-name", 443, weightDestinations[1].Weight, "default/ingress-ex-svc-2:443"),
			// because both balancers should have experiment target groups
			makeTargetGroupForBalancer("lb-multi-ingress-name", "ex-svc-1-tg-abc123-name", 80, weightDestinations[0].Weight, "default/multi-ingress-ex-svc-1:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "ex-svc-2-tg-abc123-name", 80, weightDestinations[1].Weight, "default/multi-ingress-ex-svc-2:80"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "ex-svc-1-tg-abc123-name", 443, weightDestinations[0].Weight, "default/multi-ingress-ex-svc-1:443"),
			makeTargetGroupForBalancer("lb-multi-ingress-name", "ex-svc-2-tg-abc123-name", 443, weightDestinations[1].Weight, "default/multi-ingress-ex-svc-2:443"),
		}

		weightVerified, err := r.VerifyWeight(10, weightDestinations...)
		assert.NoError(t, err)
		assert.True(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *fakeClient.getAlbStatusMultiIngress("ingress", 0, 0))
	}
}

func testSetHeaderRoute(t *testing.T, ro *v1alpha1.Rollout, ingresses []*networkingv1.Ingress) {
	client := fake.NewSimpleClientset(ingressesToObjects(ingresses)...)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	for _, ingress := range ingresses {
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(ingress)
	}
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})
	assert.NoError(t, err)
	err = r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
		Name: "header-route",
		Match: []v1alpha1.HeaderRoutingMatch{{
			HeaderName: "Agent",
			HeaderValue: &v1alpha1.StringMatch{
				Prefix: "Chrome",
			},
		}},
	})
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), len(ingresses))

	// no managed routes, no changes expected
	err = r.RemoveManagedRoutes()
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), len(ingresses))
}

func TestSetHeaderRoute(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = []v1alpha1.MangedRoutes{
		{Name: "header-route"},
	}
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, "action1", []int32{443}, 10, ro.Name, false)
	testSetHeaderRoute(t, ro, []*networkingv1.Ingress{i})
}

func TestSetHeaderRouteMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = []v1alpha1.MangedRoutes{
		{Name: "header-route"},
	}
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, "action1", []int32{443}, 10, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, "action2", []int32{443}, 10, ro.Name, false)

	testSetHeaderRoute(t, ro, []*networkingv1.Ingress{i, mi})
}
func TestSetHeaderRouteServicePorts(t *testing.T) {
	ro := fakeRolloutWithServicePorts(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"})
	ro.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = []v1alpha1.MangedRoutes{
		{Name: "header-route"},
	}
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, "action1", []int32{80, 443}, 10, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, "action2", []int32{80, 443}, 10, ro.Name, false)

	testSetHeaderRoute(t, ro, []*networkingv1.Ingress{i, mi})
}

func testRemoveManagedRoutes(t *testing.T, ro *v1alpha1.Rollout, ingresses []*networkingv1.Ingress) {
	client := fake.NewSimpleClientset(ingressesToObjects(ingresses)...)
	managedByValue := ingressutil.ManagedALBAnnotations{
		ro.Name: ingressutil.ManagedALBAnnotation{
			"alb.ingress.kubernetes.io/actions.action1",
			"alb.ingress.kubernetes.io/actions.header-route",
			"alb.ingress.kubernetes.io/conditions.header-route",
		},
	}
	specRules := []networkingv1.IngressRule{
		{
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "action1",
									Port: networkingv1.ServiceBackendPort{
										Name:   "use-annotation",
										Number: 0,
									},
								},
								Resource: nil,
							},
						},
					},
				},
			},
		},
		{
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: "header-route",
									Port: networkingv1.ServiceBackendPort{
										Name:   "use-annotation",
										Number: 0,
									},
								},
								Resource: nil,
							},
						},
					},
				},
			},
		},
	}

	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	for _, ingress := range ingresses {
		ingress.Annotations["alb.ingress.kubernetes.io/actions.header-route"] = "{}"
		ingress.Annotations["alb.ingress.kubernetes.io/conditions.header-route"] = "{}"
		ingress.Annotations[ingressutil.ManagedAnnotations] = managedByValue.String()

		ingress.Spec.Rules = []networkingv1.IngressRule{}
		copy(ingress.Spec.Rules, specRules)

		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(ingress)
	}

	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})
	assert.NoError(t, err)

	err = r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
		Name: "header-route",
	})
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), len(ingresses))

	err = r.RemoveManagedRoutes()
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), len(ingresses)*2)
}

func TestRemoveManagedRoutes(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = []v1alpha1.MangedRoutes{
		{Name: "header-route"},
	}
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, "action1", []int32{443}, 10, ro.Name, false)
	testRemoveManagedRoutes(t, ro, []*networkingv1.Ingress{i})
}

func TestRemoveManagedRoutesMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = []v1alpha1.MangedRoutes{
		{Name: "header-route"},
	}
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, "action1", []int32{443}, 10, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, "action1", []int32{443}, 10, ro.Name, false)
	testRemoveManagedRoutes(t, ro, []*networkingv1.Ingress{i, mi})
}

func TestRemoveManagedRoutesServicePorts(t *testing.T) {
	ro := fakeRolloutWithServicePorts(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"})
	ro.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = []v1alpha1.MangedRoutes{
		{Name: "header-route"},
	}
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, "action1", []int32{80, 443}, 10, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, "action1", []int32{80, 443}, 10, ro.Name, false)
	testRemoveManagedRoutes(t, ro, []*networkingv1.Ingress{i, mi})
}

func testSetMirrorRoute(t *testing.T, ro *v1alpha1.Rollout, ingresses []*networkingv1.Ingress) {
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	for _, ingress := range ingresses {
		k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(ingress)
	}
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})
	assert.NoError(t, err)
	err = r.SetMirrorRoute(&v1alpha1.SetMirrorRoute{
		Name: "mirror-route",
		Match: []v1alpha1.RouteMatch{{
			Method: &v1alpha1.StringMatch{Exact: "GET"},
		}},
	})
	assert.Nil(t, err)
	err = r.RemoveManagedRoutes()
	assert.Nil(t, err)

	assert.Len(t, client.Actions(), 0)
}
func TestSetMirrorRoute(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 10, ro.Name, false)
	testSetMirrorRoute(t, ro, []*networkingv1.Ingress{i})
}

func TestSetMirrorRouteMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 10, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{443}, 10, ro.Name, false)

	testSetMirrorRoute(t, ro, []*networkingv1.Ingress{i, mi})
}

func TestSetMirrorRouteServicePorts(t *testing.T) {
	ro := fakeRolloutWithServicePorts(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"})
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 10, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, []int32{80, 443}, 10, ro.Name, false)

	testSetMirrorRoute(t, ro, []*networkingv1.Ingress{i, mi})
}
