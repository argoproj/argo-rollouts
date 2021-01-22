package aws

import (
	"context"
	"testing"

	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/utils/aws/mocks"
)

func newFakeClient() (*mocks.ELBv2APIClient, Client) {
	fakeELB := mocks.ELBv2APIClient{}
	c := client{
		ELBV2:                &fakeELB,
		loadBalancerDNStoARN: make(map[string]string),
	}
	return &fakeELB, &c
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
		expectedLB := types.LoadBalancer{
			LoadBalancerArn: pointer.StringPtr("lb-abc123"),
			DNSName:         pointer.StringPtr("find-loadbalancer-test-abc-123.us-west-2.elb.amazonaws.com"),
		}
		lbOut := elbv2.DescribeLoadBalancersOutput{
			LoadBalancers: []types.LoadBalancer{
				expectedLB,
			},
		}
		fakeELB.On("DescribeLoadBalancers", mock.Anything, mock.Anything).Return(&lbOut, nil)

		lb, err := c.FindLoadBalancerByDNSName(context.TODO(), "find-loadbalancer-test-abc-123.us-west-2.elb.amazonaws.com")
		assert.NoError(t, err)
		assert.Equal(t, expectedLB, *lb)

	}
}

func TestGetTargetGroupMetadata(t *testing.T) {
	fakeELB, c := newFakeClient()

	// mock the output
	tgOut := elbv2.DescribeTargetGroupsOutput{
		TargetGroups: []types.TargetGroup{
			{
				TargetGroupArn: pointer.StringPtr("tg-abc123"),
			},
			{
				TargetGroupArn: pointer.StringPtr("tg-def456"),
			},
		},
	}
	fakeELB.On("DescribeTargetGroups", mock.Anything, mock.Anything).Return(&tgOut, nil)

	tagsOut := elbv2.DescribeTagsOutput{
		TagDescriptions: []types.TagDescription{
			{
				ResourceArn: pointer.StringPtr("tg-abc123"),
				Tags: []types.Tag{
					{
						Key:   pointer.StringPtr("foo"),
						Value: pointer.StringPtr("bar"),
					},
				},
			},
		},
	}
	fakeELB.On("DescribeTags", mock.Anything, mock.Anything).Return(&tagsOut, nil)

	listenersOut := elbv2.DescribeListenersOutput{
		Listeners: []types.Listener{
			{
				ListenerArn:     pointer.StringPtr("lst-abc123"),
				LoadBalancerArn: pointer.StringPtr("lb-abc123"),
			},
		},
	}
	fakeELB.On("DescribeListeners", mock.Anything, mock.Anything).Return(&listenersOut, nil)

	rulesOut := elbv2.DescribeRulesOutput{
		Rules: []types.Rule{
			{
				Actions: []types.Action{
					{
						ForwardConfig: &types.ForwardActionConfig{
							TargetGroups: []types.TargetGroupTuple{
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
	assert.Len(t, tgMeta, 2)
	assert.Equal(t, "tg-abc123", *tgMeta[0].TargetGroup.TargetGroupArn)
	assert.Equal(t, "bar", tgMeta[0].Tags["foo"])
	assert.Equal(t, int32(10), *tgMeta[0].Weight)

	assert.Equal(t, "tg-def456", *tgMeta[1].TargetGroup.TargetGroupArn)
	assert.Len(t, tgMeta[1].Tags, 0)
	assert.Nil(t, tgMeta[1].Weight)
}

func TestBuildV2TargetGroupID(t *testing.T) {
	assert.Equal(t, "default/ingress-svc:80", BuildV2TargetGroupID("default", "ingress", "svc", 80))
}
