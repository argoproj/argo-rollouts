package aws

import (
	"context"
	"testing"

	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	testutil "github.com/argoproj/argo-rollouts/test/util"
	"github.com/argoproj/argo-rollouts/utils/aws/mocks"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

func newFakeClient() (*mocks.ELBv2APIClient, Client) {
	fakeELB := mocks.ELBv2APIClient{}
	awsClient, _ := FakeNewClientFunc(&fakeELB)()
	return &fakeELB, awsClient
}

func TestFindLoadBalancerByDNSName(t *testing.T) {
	// LoadBalancer not found
	{
		fakeELB, c := newFakeClient()
		fakeELB.On("DescribeLoadBalancers", mock.Anything, mock.Anything).Return(&elbv2.DescribeLoadBalancersOutput{}, nil)
		lb, err := c.FindLoadBalancerByDNSName(context.TODO(), "doesnt-exist")
		assert.NoError(t, err)
		assert.Nil(t, lb)
	}

	// LoadBalancer found
	{
		fakeELB, c := newFakeClient()
		// Mock output
		expectedLB := elbv2types.LoadBalancer{
			LoadBalancerArn: pointer.StringPtr("lb-abc123"),
			DNSName:         pointer.StringPtr("find-loadbalancer-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		lbOut := elbv2.DescribeLoadBalancersOutput{
			LoadBalancers: []elbv2types.LoadBalancer{
				expectedLB,
			},
		}
		fakeELB.On("DescribeLoadBalancers", mock.Anything, mock.Anything).Return(&lbOut, nil)

		lb, err := c.FindLoadBalancerByDNSName(context.TODO(), "find-loadbalancer-test-abc-123.us-west-2.elb.amazonaws.com")
		assert.NoError(t, err)
		assert.Equal(t, expectedLB, *lb)

	}
}

func TestGetNumericTargetPort(t *testing.T) {
	tgb := TargetGroupBinding{
		Spec: TargetGroupBindingSpec{
			ServiceRef: ServiceReference{
				Port: intstr.FromString("web"),
			},
		},
	}
	svc := corev1.Service{
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:       "web",
					TargetPort: intstr.FromString("http"),
				},
			},
		},
	}
	eps := corev1.Endpoints{
		Subsets: []corev1.EndpointSubset{
			{
				Ports: []corev1.EndpointPort{
					{
						Name: "asdf",
						Port: 1234,
					},
					{
						Name: "http",
						Port: 4567,
					},
				},
			},
		},
	}
	assert.Equal(t, int32(4567), getNumericTargetPort(tgb, svc, eps))
}

func TestGetTargetGroupMetadata(t *testing.T) {
	defaults.SetDescribeTagsLimit(2)
	fakeELB, c := newFakeClient()

	// mock the output
	tgOut := elbv2.DescribeTargetGroupsOutput{
		TargetGroups: []elbv2types.TargetGroup{
			{
				TargetGroupArn: pointer.StringPtr("tg-abc123"),
			},
			{
				TargetGroupArn: pointer.StringPtr("tg-def456"),
			},
			{
				TargetGroupArn: pointer.StringPtr("tg-ghi789"),
			},
		},
	}
	fakeELB.On("DescribeTargetGroups", mock.Anything, mock.Anything).Return(&tgOut, nil)

	mockDescribeTagsCall := fakeELB.On("DescribeTags", mock.Anything, mock.Anything)
	mockDescribeTagsCall.RunFn = func(args mock.Arguments) {
		tagsIn := args[1].(*elbv2.DescribeTagsInput)
		assert.LessOrEqual(t, len(tagsIn.ResourceArns), defaults.GetDescribeTagsLimit())

		tagsOut := elbv2.DescribeTagsOutput{
			TagDescriptions: []elbv2types.TagDescription{},
		}
		for _, arn := range tagsIn.ResourceArns {
			tagsOut.TagDescriptions = append(tagsOut.TagDescriptions, elbv2types.TagDescription{
				ResourceArn: pointer.StringPtr(arn),
				Tags: []elbv2types.Tag{
					{
						Key:   pointer.StringPtr("foo"),
						Value: pointer.StringPtr("bar"),
					},
				},
			})
		}

		mockDescribeTagsCall.ReturnArguments = mock.Arguments{&tagsOut, nil}
	}

	listenersOut := elbv2.DescribeListenersOutput{
		Listeners: []elbv2types.Listener{
			{
				ListenerArn:     pointer.StringPtr("lst-abc123"),
				LoadBalancerArn: pointer.StringPtr("lb-abc123"),
			},
		},
	}
	fakeELB.On("DescribeListeners", mock.Anything, mock.Anything).Return(&listenersOut, nil)

	rulesOut := elbv2.DescribeRulesOutput{
		Rules: []elbv2types.Rule{
			{
				Actions: []elbv2types.Action{
					{
						ForwardConfig: &elbv2types.ForwardActionConfig{
							TargetGroups: []elbv2types.TargetGroupTuple{
								{
									TargetGroupArn: pointer.StringPtr("tg-abc123"),
									Weight:         pointer.Int32Ptr(10),
								},
							},
						},
					},
				},
			},
		},
	}
	fakeELB.On("DescribeRules", mock.Anything, mock.Anything).Return(&rulesOut, nil)

	// Test
	tgMeta, err := c.GetTargetGroupMetadata(context.TODO(), "lb-abc123")
	assert.NoError(t, err)
	assert.Len(t, tgMeta, 3)
	assert.Equal(t, "tg-abc123", *tgMeta[0].TargetGroup.TargetGroupArn)
	assert.Equal(t, "bar", tgMeta[0].Tags["foo"])
	assert.Equal(t, int32(10), *tgMeta[0].Weight)

	assert.Equal(t, "tg-def456", *tgMeta[1].TargetGroup.TargetGroupArn)
	assert.Equal(t, "bar", tgMeta[1].Tags["foo"])
	assert.Nil(t, tgMeta[1].Weight)

	assert.Equal(t, "tg-ghi789", *tgMeta[2].TargetGroup.TargetGroupArn)
	assert.Equal(t, "bar", tgMeta[2].Tags["foo"])
	assert.Nil(t, tgMeta[2].Weight)
}

func TestBuildTargetGroupResourceID(t *testing.T) {
	assert.Equal(t, "default/ingress-svc:80", BuildTargetGroupResourceID("default", "ingress", "svc", 80))
}

func TestGetTargetGroupHealth(t *testing.T) {
	fakeELB, c := newFakeClient()
	expectedHealth := elbv2.DescribeTargetHealthOutput{
		TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
			{
				HealthCheckPort: pointer.StringPtr("80"),
				Target:          &elbv2types.TargetDescription{},
				TargetHealth: &elbv2types.TargetHealth{
					State: elbv2types.TargetHealthStateEnumHealthy,
				},
			},
		},
	}
	fakeELB.On("DescribeTargetHealth", mock.Anything, mock.Anything).Return(&expectedHealth, nil)

	// Test
	health, err := c.GetTargetGroupHealth(context.TODO(), "tg-abc123")
	assert.NoError(t, err)
	assert.Equal(t, expectedHealth.TargetHealthDescriptions, health)
}

var testTargetGroupBinding = `
apiVersion: elbv2.k8s.aws/v1beta1
kind: TargetGroupBinding
metadata:
  name: active
  namespace: default
spec:
  serviceRef:
    name: active
    port: 80
  targetGroupARN: arn::1234
  targetType: ip
`

func TestGetTargetGroupBindingsByService(t *testing.T) {
	{
		svc1 := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "active",
				Namespace: metav1.NamespaceDefault,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{},
				Ports: []corev1.ServicePort{{
					Protocol:   "TCP",
					Port:       int32(80),
					TargetPort: intstr.FromInt(80),
				}},
			},
		}
		obj := unstructuredutil.StrToUnstructuredUnsafe(testTargetGroupBinding)
		dynamicClientSet := testutil.NewFakeDynamicClient(obj)
		tgbs, err := GetTargetGroupBindingsByService(context.TODO(), dynamicClientSet, svc1)
		assert.NoError(t, err)
		assert.Equal(t, 80, tgbs[0].Spec.ServiceRef.Port.IntValue())
		assert.Equal(t, "arn::1234", tgbs[0].Spec.TargetGroupARN)
		assert.Equal(t, "ip", string(*tgbs[0].Spec.TargetType))
	}
	{
		svc2 := corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "active",
				Namespace: metav1.NamespaceDefault,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{},
				Ports: []corev1.ServicePort{{
					Protocol:   "TCP",
					Name:       "foo",
					TargetPort: intstr.FromInt(80),
				}},
			},
		}
		obj := unstructuredutil.StrToUnstructuredUnsafe(`
apiVersion: elbv2.k8s.aws/v1beta1
kind: TargetGroupBinding
metadata:
  name: active
  namespace: default
spec:
  serviceRef:
    name: active
    port: foo
  targetGroupARN: arn::1234
  targetType: instance
`)
		dynamicClientSet := testutil.NewFakeDynamicClient(obj)
		tgbs, err := GetTargetGroupBindingsByService(context.TODO(), dynamicClientSet, svc2)
		assert.NoError(t, err)
		assert.Equal(t, "foo", tgbs[0].Spec.ServiceRef.Port.StrVal)
		assert.Equal(t, "arn::1234", tgbs[0].Spec.TargetGroupARN)
		assert.Equal(t, "instance", string(*tgbs[0].Spec.TargetType))
	}
}

func TestVerifyTargetGroupBindingIgnoreInstanceMode(t *testing.T) {
	logCtx := log.NewEntry(log.New())
	_, awsClnt := newFakeClient()
	tgb := TargetGroupBinding{
		Spec: TargetGroupBindingSpec{
			TargetType: (*TargetType)(pointer.StringPtr("instance")),
		},
	}
	res, err := VerifyTargetGroupBinding(context.TODO(), logCtx, awsClnt, tgb, nil, nil)
	assert.Nil(t, res)
	assert.NoError(t, err)
}

func TestVerifyTargetGroupBinding(t *testing.T) {
	logCtx := log.NewEntry(log.New())
	tgb := TargetGroupBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: TargetGroupBindingSpec{
			TargetType:     (*TargetType)(pointer.StringPtr("ip")),
			TargetGroupARN: "arn::1234",
			ServiceRef: ServiceReference{
				Name: "active",
				Port: intstr.FromInt(80),
			},
		},
	}
	ep := corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active",
			Namespace: metav1.NamespaceDefault,
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP: "1.2.3.4", // registered
					},
					{
						IP: "5.6.7.8", // registered
					},
					{
						IP: "2.4.6.8", // not registered
					},
				},
				Ports: []corev1.EndpointPort{
					{
						Port:     8080,
						Protocol: "TCP",
					},
				},
			},
		},
	}
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "active",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{},
			Ports: []corev1.ServicePort{{
				Protocol:   "TCP",
				Port:       int32(80),
				TargetPort: intstr.FromInt(8080),
			}},
		},
	}
	fakeELB, awsClnt := newFakeClient()
	thOut := elbv2.DescribeTargetHealthOutput{
		TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.StringPtr("1.2.3.4"),
					Port: pointer.Int32Ptr(8080),
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.StringPtr("5.6.7.8"),
					Port: pointer.Int32Ptr(8080),
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.StringPtr("2.4.6.8"), // irrelevant
					Port: pointer.Int32Ptr(8081),       // wrong port
				},
			},
			{
				Target: &elbv2types.TargetDescription{
					Id:   pointer.StringPtr("9.8.7.6"), // irrelevant ip
					Port: pointer.Int32Ptr(8080),
				},
			},
		},
	}
	fakeELB.On("DescribeTargetHealth", mock.Anything, mock.Anything).Return(&thOut, nil)
	res, err := VerifyTargetGroupBinding(context.TODO(), logCtx, awsClnt, tgb, &ep, &svc)
	expectedRes := TargetGroupVerifyResult{
		Service:             "active",
		Verified:            false,
		EndpointsRegistered: 2,
		EndpointsTotal:      3,
	}
	assert.Equal(t, expectedRes, *res)
	assert.NoError(t, err)
}
