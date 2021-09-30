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

func fakeRollout(stableSvc, canarySvc, stableIng string, port int32) *v1alpha1.Rollout {
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

const actionTemplateWithExperiments = `{"Type":"forward","ForwardConfig":{"TargetGroups":[{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d},{"ServiceName":"%s","ServicePort":"%d","Weight":%d}]}}`

func albActionAnnotation(stable string) string {
	return fmt.Sprintf("%s%s%s", ingressutil.ALBIngressAnnotation, ingressutil.ALBActionPrefix, stable)
}

func ingress(name string, stableSvc, canarySvc string, port, weight int32, managedBy string) *extensionsv1beta1.Ingress {
	managedByValue := fmt.Sprintf("%s:%s", managedBy, albActionAnnotation(stableSvc))
	action := fmt.Sprintf(actionTemplate, canarySvc, port, weight, stableSvc, port, 100-weight)
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
				albActionAnnotation(stableSvc):       string(jsonutil.MustMarshal(a)),
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
										ServiceName: stableSvc,
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
	rollout := fakeRollout("stable-service", "canary-service", "stable-ingress", 443)
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
	ro := fakeRollout("stable-service", "canary-service", "stable-ingress", 443)
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
	ro := fakeRollout("stable-stable", "canary-service", "ingress", 443)
	ro.Spec.Strategy.Canary.TrafficRouting.ALB.RootService = "invalid-svc"
	i := ingress("ingress", "stable-service", "canary-svc", 443, 50, ro.Name)
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
	ro := fakeRollout("stable-svc", "canary-svc", "ingress", 443)
	i := ingress("ingress", "stable-svc", "canary-svc", 443, 10, ro.Name)
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
	ro := fakeRollout("stable-svc", "canary-svc", "ingress", 443)
	i := ingress("ingress", "stable-svc", "canary-svc", 443, 5, ro.Name)
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
	ro := fakeRollout("stable-svc", "canary-svc", "ingress", 443)
	i := ingress("ingress", "stable-svc", "canary-svc", 443, 5, ro.Name)
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

func TestUpdateDesiredWeight(t *testing.T) {
	ro := fakeRollout("stable-svc", "canary-svc", "ingress", 443)
	i := ingress("ingress", "stable-svc", "canary-svc", 443, 5, ro.Name)
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
	r := fakeRollout("stable", "canary", "ingress", 443)
	forwardAction := getForwardActionString(r, 443, 0)
	assert.Contains(t, forwardAction, `"Weight":0`)
}

func TestErrorPatching(t *testing.T) {
	ro := fakeRollout("stable-svc", "canary-svc", "ingress", 443)
	i := ingress("ingress", "stable-svc", "canary-svc", 443, 5, ro.Name)
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

func TestVerifyWeight(t *testing.T) {
	newFakeReconciler := func() (*Reconciler, *fakeAWSClient) {
		ro := fakeRollout("stable-svc", "canary-svc", "ingress", 443)
		i := ingress("ingress", "stable-svc", "canary-svc", 443, 5, ro.Name)
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
		})
		assert.NoError(t, err)
		fakeAWS := fakeAWSClient{}
		r.aws = &fakeAWS
		return r, &fakeAWS
	}

	// LoadBalancer not found
	{
		r, _ := newFakeReconciler()
		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
	}

	// LoadBalancer found, not at weight
	{
		r, fakeClient := newFakeReconciler()
		fakeClient.loadBalancer = &elbv2types.LoadBalancer{
			LoadBalancerArn: pointer.StringPtr("lb-abc123"),
			DNSName:         pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupArn: pointer.StringPtr("tg-abc123"),
				},
				Weight: pointer.Int32Ptr(11),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
		}

		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
	}

	// LoadBalancer found, at weight
	{
		r, fakeClient := newFakeReconciler()
		fakeClient.loadBalancer = &elbv2types.LoadBalancer{
			LoadBalancerArn: pointer.StringPtr("lb-abc123"),
			DNSName:         pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupArn: pointer.StringPtr("tg-abc123"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
		}

		weightVerified, err := r.VerifyWeight(10)
		assert.NoError(t, err)
		assert.True(t, *weightVerified)
	}
}

func TestSetWeightWithMultipleBackends(t *testing.T) {
	ro := fakeRollout("stable-svc", "canary-svc", "ingress", 443)
	i := ingress("ingress", "stable-svc", "canary-svc", 443, 0, ro.Name)
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
	expectedAction := fmt.Sprintf(actionTemplateWithExperiments, "canary-svc", servicePort, 10, weightDestinations[0].ServiceName, servicePort, weightDestinations[0].Weight, weightDestinations[1].ServiceName, servicePort, weightDestinations[1].Weight, "stable-svc", servicePort, 85)
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
	newFakeReconciler := func() (*Reconciler, *fakeAWSClient) {
		ro := fakeRollout("stable-svc", "canary-svc", "ingress", 443)
		i := ingress("ingress", "stable-svc", "canary-svc", 443, 0, ro.Name)
		i.Annotations["alb.ingress.kubernetes.io/actions.stable-svc"] = fmt.Sprintf(actionTemplateWithExperiments, "canary-svc", 443, 10, weightDestinations[0].ServiceName, 443, weightDestinations[0].Weight, weightDestinations[1].ServiceName, 443, weightDestinations[1].Weight, "stable-svc", 443, 85)

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
		})
		assert.NoError(t, err)
		fakeAWS := fakeAWSClient{}
		r.aws = &fakeAWS
		return r, &fakeAWS
	}

	// LoadBalancer found, but experiment weights not present
	{
		r, fakeClient := newFakeReconciler()
		fakeClient.loadBalancer = &elbv2types.LoadBalancer{
			LoadBalancerArn: pointer.StringPtr("lb-abc123"),
			DNSName:         pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupArn: pointer.StringPtr("tg-abc123"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
		}

		weightVerified, err := r.VerifyWeight(10, weightDestinations...)
		assert.NoError(t, err)
		assert.False(t, *weightVerified)
	}

	// LoadBalancer found, with incorrect weights
	{
		r, fakeClient := newFakeReconciler()
		fakeClient.loadBalancer = &elbv2types.LoadBalancer{
			LoadBalancerArn: pointer.StringPtr("lb-abc123"),
			DNSName:         pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupArn: pointer.StringPtr("tg-abc123"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupArn: pointer.StringPtr("tg-abc123"),
				},
				Weight: pointer.Int32Ptr(100),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-1:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupArn: pointer.StringPtr("tg-abc123"),
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
	}

	// LoadBalancer found, with all correct weights
	{
		r, fakeClient := newFakeReconciler()
		fakeClient.loadBalancer = &elbv2types.LoadBalancer{
			LoadBalancerArn: pointer.StringPtr("lb-abc123"),
			DNSName:         pointer.StringPtr("verify-weight-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		fakeClient.targetGroups = []aws.TargetGroupMeta{
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupArn: pointer.StringPtr("tg-abc123"),
				},
				Weight: pointer.Int32Ptr(10),
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-canary-svc:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupArn: pointer.StringPtr("tg-abc123"),
				},
				Weight: &weightDestinations[0].Weight,
				Tags: map[string]string{
					aws.AWSLoadBalancerV2TagKeyResourceID: "default/ingress-ex-svc-1:443",
				},
			},
			{
				TargetGroup: elbv2types.TargetGroup{
					TargetGroupArn: pointer.StringPtr("tg-abc123"),
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
	}
}
