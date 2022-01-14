package alb

import (
	"context"
	"encoding/json"
	"fmt"
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
	managedByValue := fmt.Sprintf("%s:%s", managedBy, albActionAnnotation(actionService))
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
				albActionAnnotation(actionService):   string(jsonutil.MustMarshal(a)),
				ingressutil.ManagedActionsAnnotation: managedByValue,
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

func TestErrorOnInvalidManagedBy(t *testing.T) {
	ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
	i := ingress("ingress", STABLE_SVC, CANARY_SVC, STABLE_SVC, 443, 5, ro.Name, false)
	i.Annotations[ingressutil.ManagedActionsAnnotation] = "test"
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

func TestSetWeightPingPong(t *testing.T) {
	pp := &v1alpha1.PingPongSpec{PingService: PING_SVC, PongService: PONG_SVC}
	ro := fakeRollout("", "", pp, "ingress", 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ALB.RootService = "root-service"
	ro.Status.Canary.StablePingPong = PONG_SVC
	i := ingress("ingress", PING_SVC, PONG_SVC, "root-service", 443, 10, ro.Name, false)
	//i.Spec.
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

type fakeAWSClient struct {
	targetGroups             []aws.TargetGroupMeta
	loadBalancer             *elbv2types.LoadBalancer
	targetHealthDescriptions []elbv2types.TargetHealthDescription
}

func (f *fakeAWSClient) GetTargetGroupMetadata(ctx context.Context, loadBalancerARN string) ([]aws.TargetGroupMeta, error) {
	return f.targetGroups, nil
}

func (f *fakeAWSClient) FindLoadBalancerByDNSName(ctx context.Context, dnsName string) (*elbv2types.LoadBalancer, error) {
	return f.loadBalancer, nil
}

func (f *fakeAWSClient) GetTargetGroupHealth(ctx context.Context, targetGroupARN string) ([]elbv2types.TargetHealthDescription, error) {
	return f.targetHealthDescriptions, nil
}

func (f *fakeAWSClient) getAlbStatus() *v1alpha1.ALBStatus {
	return &v1alpha1.ALBStatus{
		LoadBalancer: v1alpha1.AwsResourceRef{
			Name: *f.loadBalancer.LoadBalancerName,
			ARN:  *f.loadBalancer.LoadBalancerArn,
		},
		CanaryTargetGroup: v1alpha1.AwsResourceRef{
			Name: *f.targetGroups[0].TargetGroupName,
			ARN:  *f.targetGroups[0].TargetGroupArn,
		},
		StableTargetGroup: v1alpha1.AwsResourceRef{
			Name: *f.targetGroups[len(f.targetGroups)-1].TargetGroupName,
			ARN:  *f.targetGroups[len(f.targetGroups)-1].TargetGroupArn,
		},
	}
}

func TestVerifyWeight(t *testing.T) {
	newFakeReconciler := func(status *v1alpha1.RolloutStatus) (*Reconciler, *fakeAWSClient) {
		ro := fakeRollout(STABLE_SVC, CANARY_SVC, nil, "ingress", 443)
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
	}

	// LoadBalancer found, not at weight
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancer = &elbv2types.LoadBalancer{
			LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
			LoadBalancerArn:  pointer.StringPtr("lb-abc123-arn"),
			DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("canary-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("canary-tg-abc123-arn"),
				},
				Weight: pointer.Int32Ptr(11),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("stable-tg-abc123-arn"),
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
		assert.Equal(t, *status.ALB, *fakeClient.getAlbStatus())
	}

	// LoadBalancer found, at weight
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancer = &elbv2types.LoadBalancer{
			LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
			LoadBalancerArn:  pointer.StringPtr("lb-abc123-arn"),
			DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
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
		assert.Equal(t, *status.ALB, *fakeClient.getAlbStatus())
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
		fakeClient.loadBalancer = &elbv2types.LoadBalancer{
			LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
			LoadBalancerArn:  pointer.StringPtr("lb-abc123-arn"),
			DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
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
				Weight: pointer.Int32Ptr(90),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-stable-svc:443",
				},
			},
		}

		weightVerified, err := r.VerifyWeight(10, weightDestinations...)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
		assert.Equal(t, *status.ALB, *fakeClient.getAlbStatus())
	}

	// LoadBalancer found, with incorrect weights
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancer = &elbv2types.LoadBalancer{
			LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
			LoadBalancerArn:  pointer.StringPtr("lb-abc123-arn"),
			DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
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
					TargetGroupName: pointer.StringPtr("ex-svc-1-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("ex-svc-1-tg-abc123-arn"),
				},
				Weight: pointer.Int32Ptr(100),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-1:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("ex-svc-2-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("ex-svc-2-tg-abc123-arn"),
				},
				Weight: pointer.Int32Ptr(100),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-2:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("stable-tg-abc123-arn"),
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
		assert.Equal(t, *status.ALB, *fakeClient.getAlbStatus())
	}

	// LoadBalancer found, with all correct weights
	{
		var status v1alpha1.RolloutStatus
		r, fakeClient := newFakeReconciler(&status)
		fakeClient.loadBalancer = &elbv2types.LoadBalancer{
			LoadBalancerName: pointer.StringPtr("lb-abc123-name"),
			LoadBalancerArn:  pointer.StringPtr("lb-abc123-arn"),
			DNSName:          pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
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
					TargetGroupName: pointer.StringPtr("ex-svc-1-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("ex-svc-1-tg-abc123-arn"),
				},
				Weight: &weightDestinations[0].Weight,
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-1:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("ex-svc-2-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("ex-svc-2-tg-abc123-arn"),
				},
				Weight: &weightDestinations[1].Weight,
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-2:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupName: pointer.StringPtr("stable-tg-abc123-name"),
					TargetGroupArn:  pointer.StringPtr("stable-tg-abc123-arn"),
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
		assert.Equal(t, *status.ALB, *fakeClient.getAlbStatus())
	}
}
