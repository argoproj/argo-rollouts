package alb

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/pointer"

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

const actionTemplate = `{
	"Type":"forward",
	"ForwardConfig":{
		"TargetGroups":[
			{
				"ServiceName":"%s",
				"ServicePort":"%d",
				"Weight":%d
			},{
				"ServiceName":"%s",
				"ServicePort":"%d",
				"Weight":%d
			}
		]
	}
}`

const actionTemplateWithStickyConfig = `{
	"Type":"forward",
	"ForwardConfig":{
		"TargetGroups":[
			{
				"ServiceName":"%s",
				"ServicePort":"%d",
				"Weight":%d
			},{
				"ServiceName":"%s",
				"ServicePort":"%d",
				"Weight":%d
			}
		],
		"TargetGroupStickinessConfig":{
		  "DurationSeconds" : 300,
		  "Enabled" : true
		}
	}
}`

const actionTemplateWithExperiments = `{"Type":"forward","ForwardConfig":{"TargetGroups":[{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d}]}}`

func albActionAnnotation(stable string) string {
	return fmt.Sprintf("%s%s%s", ingressutil.ALBIngressAnnotation, ingressutil.ALBActionPrefix, stable)
}

func ingress(name, stableSvc, canarySvc, actionService string, port, weight int32, managedBy string, includeStickyConfig bool) *extensionsv1beta1.Ingress {
	managedByValue := ingressutil.ManagedALBAnnotations{
		managedBy: ingressutil.ManagedALBAnnotation{albActionAnnotation(actionService)},
	}
	action := fmt.Sprintf(actionTemplate, canarySvc, port, weight, stableSvc, port, 100-weight)
	if includeStickyConfig {
		action = fmt.Sprintf(actionTemplateWithStickyConfig, canarySvc, port, weight, stableSvc, port, 100-weight)
	}
	var a ingressutil.ALBAction
	err := json.Unmarshal([]byte(action), &a)
	if err != nil {
		panic(err)
	}

	i := &extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{
				albActionAnnotation(actionService): string(jsonutil.MustMarshal(a)),
				ingressutil.ManagedAnnotations:     managedByValue.String(),
			},
		},
		Spec: extensionsv1beta1.IngressSpec{
			Rules: []extensionsv1beta1.IngressRule{
				{
					IngressRuleValue: extensionsv1beta1.IngressRuleValue{
						HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
							Paths: []extensionsv1beta1.HTTPIngressPath{
								{
									Backend: extensionsv1beta1.IngressBackend{
										ServiceName: actionService,
										ServicePort: intstr.Parse("use-annotation"),
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

func TestIngressNotFound(t *testing.T) {
	ro := fakeRollout("stable-service", "canary-service", nil, "stable-ingress", 443)
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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

func TestIngressNotFoundMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress("stable-service", "canary-service", nil, []string{"stable-ingress", "multi-ingress"}, 443)
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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

func TestServiceNotFoundInIngress(t *testing.T) {
	ro := fakeRollout("stable-stable", "canary-service", nil, "ingress", 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ALB.RootService = "invalid-svc"
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 50, ro.Name, false)
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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

func TestServiceNotFoundInMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress("stable-service", "canary-service", nil, []string{"stable-ingress", "multi-ingress"}, 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ALB.RootService = "invalid-svc"
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 50, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 50, ro.Name, false)
	client := fake.NewSimpleClientset(i, mi)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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

func TestNoChanges(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 10, ro.Name, false)
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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

func TestNoChangesMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 10, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 10, ro.Name, false)
	client := fake.NewSimpleClientset(i, mi)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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

func TestErrorOnInvalidManagedBy(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
	i.Annotations[ingressutil.ManagedAnnotations] = "test"
	client := fake.NewSimpleClientset(i)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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

func TestErrorOnInvalidManagedByMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress("stable-service", "canary-service", nil, []string{"stable-ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
	mi.Annotations[ingressutil.ManagedAnnotations] = "test"
	client := fake.NewSimpleClientset(i, mi)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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

func TestSetInitialDesiredWeight(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
	i.Annotations = map[string]string{}
	client := fake.NewSimpleClientset(i)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	assert.Len(t, client.Actions(), 1)
}

func TestSetInitialDesiredWeightMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
	i.Annotations = map[string]string{}
	client := fake.NewSimpleClientset(i, mi)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	assert.Len(t, client.Actions(), 2)
}

func TestSetWeightPingPong(t *testing.T) {
	pp := &v1alpha1.PingPongSpec{PingService: PING_SVC, PongService: PONG_SVC}
	ro := fakeRollout("", "", pp, "ingress", 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ALB.RootService = "root-service"
	ro.Status.Canary.StablePingPong = PONG_SVC
	i := ingress("ingress", PING_SVC, PONG_SVC, "root-service", 443, 10, ro.Name, false)
	i.Annotations = map[string]string{}
	client := fake.NewSimpleClientset(i)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	actions := client.Actions()
	assert.Len(t, actions, 1)
}

func TestSetWeightPingPongMultiIngress(t *testing.T) {
	pp := &v1alpha1.PingPongSpec{PingService: PING_SVC, PongService: PONG_SVC}
	ro := fakeRolloutWithMultiIngress("", "", pp, []string{"ingress", "multi-ingress"}, 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ALB.RootService = "root-service"
	ro.Status.Canary.StablePingPong = PONG_SVC
	i := ingress("ingress", PING_SVC, PONG_SVC, "root-service", 443, 10, ro.Name, false)
	mi := ingress("multi-ingress", PING_SVC, PONG_SVC, "root-service", 443, 10, ro.Name, false)
	i.Annotations = map[string]string{}
	client := fake.NewSimpleClientset(i, mi)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	actions := client.Actions()
	assert.Len(t, actions, 2)
}

func TestUpdateDesiredWeightWithStickyConfig(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, true)
	client := fake.NewSimpleClientset(i)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	assert.Len(t, client.Actions(), 1)
}

func TestUpdateDesiredWeightWithStickyConfigMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, true)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, true)
	client := fake.NewSimpleClientset(i, mi)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	assert.Len(t, client.Actions(), 2)
}

func TestUpdateDesiredWeight(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
	client := fake.NewSimpleClientset(i)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	assert.Len(t, client.Actions(), 1)
}

func TestUpdateDesiredWeightMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
	client := fake.NewSimpleClientset(i, mi)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	assert.Len(t, client.Actions(), 2)
}

// TestGetForwardActionStringMarshalsZeroCorrectly ensures that the annotation does not omit default value zero when marshalling
// the forward action
func TestGetForwardActionStringMarshalsZeroCorrectly(t *testing.T) {
	r := fakeRollout("stable", "canary", nil, "ingress", 443)
	forwardAction, err := getForwardActionString(r, 443, 0)
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
	forwardAction, err := getForwardActionString(r, 443, 0)
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
	forwardAction, err := getForwardActionString(r, 443, 0)

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
	forwardAction, err := getForwardActionString(r, 443, 0)

	assert.NotNilf(t, forwardAction, "There should be no forwardAction being generated: %v", forwardAction)
	expectedErrorMsg := "TargetGroupStickinessConfig's duration must be between 1 and 604800 seconds (7 days)!"
	assert.EqualErrorf(t, err, expectedErrorMsg, "Error should be: %v, got: %v", expectedErrorMsg, err)
}

func TestErrorPatching(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
	client := fake.NewSimpleClientset(i)
	client.ReactionChain = nil
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
		return true, nil, fmt.Errorf(errMessage)
	})

	err = r.SetWeight(10)
	assert.Error(t, err, "some error occurred")
	assert.Len(t, client.Actions(), 1)
}

func TestErrorPatchingMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
	client := fake.NewSimpleClientset(i, mi)
	client.ReactionChain = nil
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
		return true, nil, fmt.Errorf(errMessage)
	})

	err = r.SetWeight(10)
	assert.Error(t, err, "some error occurred")
	assert.Len(t, client.Actions(), 1)
}

type fakeAWSClient struct {
	Ingresses                []string
	targetGroups             []aws.TargetGroupMeta
	loadBalancers            []*elbv2types.LoadBalancer
	targetHealthDescriptions []elbv2types.TargetHealthDescription
}

func (f *fakeAWSClient) GetTargetGroupMetadata(ctx context.Context, loadBalancerARN string) ([]aws.TargetGroupMeta, error) {
	return f.targetGroups, nil
}

func (f *fakeAWSClient) FindLoadBalancerByDNSName(ctx context.Context, dnsName string) (*elbv2types.LoadBalancer, error) {
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
			SetWeight: pointer.Int32Ptr(10),
		}}
		i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
		i.Status.LoadBalancer = corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{
				{
					Hostname: "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com",
				},
			},
		}

		client := fake.NewSimpleClientset(i)
		k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
		k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
		ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
			{
				LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-abc123-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			},
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(11),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/stable-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(89),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-stable-svc:443",
				},
			},
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
			{
				LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-abc123-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			},
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/stable-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(11),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-stable-svc:443",
				},
			},
		}

		weightVerified, err := r.VerifyWeight(10)
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
					TargetGroupName: pointer.StringPtr("canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("canary-tg-abc123-arn"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("stable-tg-abc123-arn"),
				},
				Weight: pointer.Int32Ptr(11),
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

func TestVerifyWeightMultiIngress(t *testing.T) {
	newFakeReconciler := func(status *v1alpha1.RolloutStatus) (*Reconciler, *fakeAWSClient) {
		ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
		ro.Status.StableRS = "a45fe23"
		ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{
			SetWeight: pointer.Int32Ptr(10),
		}}
		i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
		mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
		i.Status.LoadBalancer = corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{
				{
					Hostname: "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com",
				},
			},
		}
		mi.Status.LoadBalancer = corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{
				{
					Hostname: "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com",
				},
			},
		}

		client := fake.NewSimpleClientset(i, mi)
		k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
		k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
		k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
		ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
			{
				LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-abc123-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			},
			{
				LoadBalancerName: pointer.StringPtr("lb-multi-ingress-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-multi-ingress-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
			},
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(11),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/stable-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(89),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-stable-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("multi-ingress-canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/multi-ingress-canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(11),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/multi-ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("multi-ingress-stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/multi-ingress-stable-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(89),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/multi-ingress-stable-svc:443",
				},
			},
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
			{
				LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-abc123-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			},
			{
				LoadBalancerName: pointer.StringPtr("lb-multi-ingress-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-multi-ingress-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
			},
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/stable-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(90),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-stable-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("multi-ingress-canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/multi-ingress-canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/multi-ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("multi-ingress-stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/multi-ingress-stable-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(90),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/multi-ingress-stable-svc:443",
				},
			},
		}

		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.True(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *fakeClient.getAlbStatusMultiIngress("ingress", 0, 0))
		assert.Equal(t, status.ALBs[1], *fakeClient.getAlbStatusMultiIngress("multi-ingress", 1, 2))
	}
}

func TestSetWeightWithMultipleBackends(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 0, ro.Name, false)
	client := fake.NewSimpleClientset(i)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	assert.Len(t, client.Actions(), 1)
	assert.Equal(t, "patch", actions[0].GetVerb())

	patchedI := extensionsv1beta1.Ingress{}
	err = json.Unmarshal(actions[0].(k8stesting.PatchAction).GetPatch(), &patchedI)
	assert.Nil(t, err)

	servicePort := 443
	expectedAction := fmt.Sprintf(actionTemplateWithExperiments, CANARY_SVC, servicePort, 10, weightDestinations[0].ServiceName, servicePort, weightDestinations[0].Weight, weightDestinations[1].ServiceName, servicePort, weightDestinations[1].Weight, STABLE_SVC, servicePort, 85)
	assert.Equal(t, expectedAction, patchedI.Annotations["alb.ingress.kubernetes.io/actions.stable-svc"])
}

func TestSetWeightWithMultipleBackendsMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 0, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 0, ro.Name, false)
	client := fake.NewSimpleClientset(i, mi)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	assert.Len(t, client.Actions(), 2)
	assert.Equal(t, "patch", actions[0].GetVerb())

	patchedI := extensionsv1beta1.Ingress{}
	err = json.Unmarshal(actions[0].(k8stesting.PatchAction).GetPatch(), &patchedI)
	assert.Nil(t, err)

	servicePort := 443
	expectedAction := fmt.Sprintf(actionTemplateWithExperiments, CANARY_SVC, servicePort, 10, weightDestinations[0].ServiceName, servicePort, weightDestinations[0].Weight, weightDestinations[1].ServiceName, servicePort, weightDestinations[1].Weight, STABLE_SVC, servicePort, 85)
	assert.Equal(t, expectedAction, patchedI.Annotations["alb.ingress.kubernetes.io/actions.stable-svc"])
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
			SetWeight: pointer.Int32Ptr(10),
		}}
		i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 0, ro.Name, false)
		i.Annotations["alb.ingress.kubernetes.io/actions.stable-svc"] = fmt.Sprintf(actionTemplateWithExperiments, CANARY_SVC, 443, 10, weightDestinations[0].ServiceName, 443, weightDestinations[0].Weight, weightDestinations[1].ServiceName, 443, weightDestinations[1].Weight, STABLE_SVC, 443, 85)

		i.Status.LoadBalancer = corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{
				{
					Hostname: "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com",
				},
			},
		}

		client := fake.NewSimpleClientset(i)
		k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
		k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
		ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
			{
				LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-abc123-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			},
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/stable-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(90),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-stable-svc:443",
				},
			},
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
			{
				LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-abc123-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			},
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("ex-svc-1-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/ex-svc-1-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(100),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-1:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("ex-svc-2-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/ex-svc-2-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(100),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-2:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/stable-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(85),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-stable-svc:443",
				},
			},
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
			{
				LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-abc123-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			},
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("ex-svc-1-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/ex-svc-1-tg-abc123-name/1234567890123456"),
				},
				Weight: &weightDestinations[0].Weight,
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-1:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("ex-svc-2-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/ex-svc-2-tg-abc123-name/1234567890123456"),
				},
				Weight: &weightDestinations[1].Weight,
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-2:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/stable-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(85),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-stable-svc:443",
				},
			},
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
			SetWeight: pointer.Int32Ptr(10),
		}}
		i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 0, ro.Name, false)
		mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
		i.Annotations["alb.ingress.kubernetes.io/actions.stable-svc"] = fmt.Sprintf(actionTemplateWithExperiments, CANARY_SVC, 443, 10, weightDestinations[0].ServiceName, 443, weightDestinations[0].Weight, weightDestinations[1].ServiceName, 443, weightDestinations[1].Weight, STABLE_SVC, 443, 85)
		mi.Annotations["alb.ingress.kubernetes.io/actions.stable-svc"] = fmt.Sprintf(actionTemplateWithExperiments, CANARY_SVC, 443, 10, weightDestinations[0].ServiceName, 443, weightDestinations[0].Weight, weightDestinations[1].ServiceName, 443, weightDestinations[1].Weight, STABLE_SVC, 443, 85)

		i.Status.LoadBalancer = corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{
				{
					Hostname: "verify-weight-test-abc-123.us-west-2.elb.amazonaws.com",
				},
			},
		}
		mi.Status.LoadBalancer = corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{
				{
					Hostname: "verify-weight-multi-ingress.us-west-2.elb.amazonaws.com",
				},
			},
		}

		client := fake.NewSimpleClientset(i, mi)
		k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
		k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
		k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
		ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
			{
				LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-abc123-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			},
			{
				LoadBalancerName: pointer.StringPtr("lb-multi-ingress-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-multi-ingress-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
			},
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/stable-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(90),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-stable-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("multi-ingress-canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/multi-ingress-canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/multi-ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("multi-ingress-stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/multi-ingress-stable-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(90),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/multi-ingress-stable-svc:443",
				},
			},
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
			{
				LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-abc123-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			},
			{
				LoadBalancerName: pointer.StringPtr("lb-multi-ingress-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-multi-ingress-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
			},
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/stable-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(85),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-stable-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("multi-ingress-canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/multi-ingress-canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/multi-ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("multi-ingress-stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/app/multi-ingress-stable-tg-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(85),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/multi-ingress-stable-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("ex-svc-1-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/ex-svc-1-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(100),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-1:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("ex-svc-2-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/ex-svc-2-tg-abc123-name/123456789012345"),
				},
				Weight: pointer.Int32Ptr(100),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-2:443",
				},
			},
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
			{
				LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-abc123-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
			},
			{
				LoadBalancerName: pointer.StringPtr("lb-multi-ingress-name"),
				LoadBalancerArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:loadbalancer/app/lb-multi-ingress-name/1234567890123456"),
				DNSName:          pointer.StringPtr("verify-weight-multi-ingress.us-west-2.elb.amazonaws.com"),
			},
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/canary-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/stable-tg-abc123-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(85),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-stable-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("multi-ingress-canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/app/multi-ingress-canary-tg-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/multi-ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("multi-ingress-stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/app/multi-ingress-stable-tg-name/1234567890123456"),
				},
				Weight: pointer.Int32Ptr(85),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/multi-ingress-stable-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("ex-svc-1-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/ex-svc-1-tg-abc123-name/1234567890123456"),
				},
				Weight: &weightDestinations[0].Weight,
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-1:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("ex-svc-2-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("arn:aws:elasticloadbalancing:us-east-2:123456789012:targetgroup/ex-svc-2-tg-abc123-name/123456789012345"),
				},
				Weight: &weightDestinations[1].Weight,
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-2:443",
				},
			},
		}

		weightVerified, err := r.VerifyWeight(10, weightDestinations...)
		assert.NoError(t, err)
		assert.True(t, *weightVerified)
		assert.Equal(t, status.ALBs[0], *fakeClient.getAlbStatusMultiIngress("ingress", 0, 0))
	}
}

func TestSetHeaderRoute(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = []v1alpha1.MangedRoutes{
		{Name: "header-route"},
	}
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, "action1", 443, 10, ro.Name, false)
	client := fake.NewSimpleClientset(i)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	assert.Len(t, client.Actions(), 1)

	// no managed routes, no changes expected
	err = r.RemoveManagedRoutes()
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 1)
}

func TestSetHeaderRouteMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = []v1alpha1.MangedRoutes{
		{Name: "header-route"},
	}
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, "action1", 443, 10, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, "action2", 443, 10, ro.Name, false)
	client := fake.NewSimpleClientset(i, mi)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	assert.Len(t, client.Actions(), 2)

	// no managed routes, no changes expected
	err = r.RemoveManagedRoutes()
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 2)
}

func TestRemoveManagedRoutes(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = []v1alpha1.MangedRoutes{
		{Name: "header-route"},
	}
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, "action1", 443, 10, ro.Name, false)
	managedByValue := ingressutil.ManagedALBAnnotations{
		ro.Name: ingressutil.ManagedALBAnnotation{
			"alb.ingress.kubernetes.io/actions.action1",
			"alb.ingress.kubernetes.io/actions.header-route",
			"alb.ingress.kubernetes.io/conditions.header-route",
		},
	}
	i.Annotations["alb.ingress.kubernetes.io/actions.header-route"] = "{}"
	i.Annotations["alb.ingress.kubernetes.io/conditions.header-route"] = "{}"
	i.Annotations[ingressutil.ManagedAnnotations] = managedByValue.String()
	i.Spec.Rules = []extensionsv1beta1.IngressRule{
		{
			IngressRuleValue: extensionsv1beta1.IngressRuleValue{
				HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
					Paths: []extensionsv1beta1.HTTPIngressPath{
						{
							Backend: extensionsv1beta1.IngressBackend{
								ServiceName: "action1",
								ServicePort: intstr.Parse("use-annotation"),
							},
						},
					},
				},
			},
		},
		{
			IngressRuleValue: extensionsv1beta1.IngressRuleValue{
				HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
					Paths: []extensionsv1beta1.HTTPIngressPath{
						{
							Backend: extensionsv1beta1.IngressBackend{
								ServiceName: "header-route",
								ServicePort: intstr.Parse("use-annotation"),
							},
						},
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(i)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	assert.Len(t, client.Actions(), 1)

	err = r.RemoveManagedRoutes()
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 2)
}

func TestRemoveManagedRoutesMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ManagedRoutes = []v1alpha1.MangedRoutes{
		{Name: "header-route"},
	}
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, "action1", 443, 10, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, "action1", 443, 10, ro.Name, false)
	managedByValue := ingressutil.ManagedALBAnnotations{
		ro.Name: ingressutil.ManagedALBAnnotation{
			"alb.ingress.kubernetes.io/actions.action1",
			"alb.ingress.kubernetes.io/actions.header-route",
			"alb.ingress.kubernetes.io/conditions.header-route",
		},
	}
	i.Annotations["alb.ingress.kubernetes.io/actions.header-route"] = "{}"
	i.Annotations["alb.ingress.kubernetes.io/conditions.header-route"] = "{}"
	i.Annotations[ingressutil.ManagedAnnotations] = managedByValue.String()
	i.Spec.Rules = []extensionsv1beta1.IngressRule{
		{
			IngressRuleValue: extensionsv1beta1.IngressRuleValue{
				HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
					Paths: []extensionsv1beta1.HTTPIngressPath{
						{
							Backend: extensionsv1beta1.IngressBackend{
								ServiceName: "action1",
								ServicePort: intstr.Parse("use-annotation"),
							},
						},
					},
				},
			},
		},
		{
			IngressRuleValue: extensionsv1beta1.IngressRuleValue{
				HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
					Paths: []extensionsv1beta1.HTTPIngressPath{
						{
							Backend: extensionsv1beta1.IngressBackend{
								ServiceName: "header-route",
								ServicePort: intstr.Parse("use-annotation"),
							},
						},
					},
				},
			},
		},
	}

	mi.Annotations["alb.ingress.kubernetes.io/actions.header-route"] = "{}"
	mi.Annotations["alb.ingress.kubernetes.io/conditions.header-route"] = "{}"
	mi.Annotations[ingressutil.ManagedAnnotations] = managedByValue.String()
	mi.Spec.Rules = []extensionsv1beta1.IngressRule{
		{
			IngressRuleValue: extensionsv1beta1.IngressRuleValue{
				HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
					Paths: []extensionsv1beta1.HTTPIngressPath{
						{
							Backend: extensionsv1beta1.IngressBackend{
								ServiceName: "action1",
								ServicePort: intstr.Parse("use-annotation"),
							},
						},
					},
				},
			},
		},
		{
			IngressRuleValue: extensionsv1beta1.IngressRuleValue{
				HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
					Paths: []extensionsv1beta1.HTTPIngressPath{
						{
							Backend: extensionsv1beta1.IngressBackend{
								ServiceName: "header-route",
								ServicePort: intstr.Parse("use-annotation"),
							},
						},
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(i, mi)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
	assert.Len(t, client.Actions(), 2)

	err = r.RemoveManagedRoutes()
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 4)
}

func TestSetMirrorRoute(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 10, ro.Name, false)
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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

func TestSetMirrorRouteMultiIngress(t *testing.T) {
	ro := fakeRolloutWithMultiIngress(STABLE_SVC, CANARY_SVC, nil, []string{"ingress", "multi-ingress"}, 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 10, ro.Name, false)
	mi := ingress("multi-ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 10, ro.Name, false)
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(mi)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
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
