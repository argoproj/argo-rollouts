package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	log "github.com/sirupsen/logrus"
)

// AWSLoadBalancerV2TagKeyResourceID is the tag applied to an AWS resource by the AWS Load Balancer
// controller, to associate it to the corresponding kubernetes resource. This is used by the rollout
// controller to identify the correct TargetGroups associated with the LoadBalancer. For AWS
// target group service references, the format is: <namespace>/<ingress-name>-<service-name>:<port>
// Example: ingress.k8s.aws/resource: default/alb-rollout-ingress-alb-rollout-stable:80
// See: https://kubernetes-sigs.github.io/aws-load-balancer-controller/guide/ingress/annotations/#resource-tags
// https://github.com/kubernetes-sigs/aws-load-balancer-controller/blob/2e51fbdc5dc978d66e36b376d6dc56b0ae146d8f/internal/alb/generator/tag.go#L125-L128
const AWSLoadBalancerV2TagKeyResourceID = "ingress.k8s.aws/resource"

type Client interface {
	GetTargetGroupMetadata(ctx context.Context, loadBalancerARN string) ([]TargetGroupMeta, error)
	FindLoadBalancerByDNSName(ctx context.Context, dnsName string) (*elbv2types.LoadBalancer, error)
}

// ELBv2APIClient is an interface that enables mocking of the ELBv2 API
type ELBv2APIClient interface {
	elbv2.DescribeTargetGroupsAPIClient
	elbv2.DescribeLoadBalancersAPIClient
	elbv2.DescribeListenersAPIClient
	DescribeRules(ctx context.Context, params *elbv2.DescribeRulesInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeRulesOutput, error)
	DescribeTags(ctx context.Context, params *elbv2.DescribeTagsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error)
}

type client struct {
	ELBV2 ELBv2APIClient

	// loadBalancerDNStoARN is a cache that maps a LoadBalancer DNSName to an ARN
	loadBalancerDNStoARN map[string]string
}

// TargetGroupMeta is a data type which combines the AWS TargetGroup information along with its
// tags, and weights
type TargetGroupMeta struct {
	elbv2types.TargetGroup
	Tags   map[string]string
	Weight *int32
}

func NewClient() (Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}
	c := client{
		ELBV2:                elbv2.NewFromConfig(cfg),
		loadBalancerDNStoARN: make(map[string]string),
	}
	return &c, nil
}

func (c *client) FindLoadBalancerByDNSName(ctx context.Context, dnsName string) (*elbv2types.LoadBalancer, error) {
	lbOutput, err := c.ELBV2.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{})
	if err != nil {
		return nil, err
	}
	for _, lb := range lbOutput.LoadBalancers {
		if lb.DNSName != nil && *lb.DNSName == dnsName {
			return &lb, nil
		}
	}
	return nil, nil
}

// GetTargetGroupMetadata is a convenience to retrieve the target groups of a load balancer along
// with relevant metadata (tags, and traffic weights).
func (c *client) GetTargetGroupMetadata(ctx context.Context, loadBalancerARN string) ([]TargetGroupMeta, error) {
	// Get target groups associated with LoadBalancer
	tgIn := elbv2.DescribeTargetGroupsInput{
		LoadBalancerArn: &loadBalancerARN,
	}
	tgOut, err := c.ELBV2.DescribeTargetGroups(ctx, &tgIn)
	if err != nil {
		return nil, err
	}
	var tgARNs []string
	// tgMetaMap is a map from TargetGroup ARN, to TargetGroupMeta objects we want to return
	tgMetaMap := make(map[string]*TargetGroupMeta)
	for _, tg := range tgOut.TargetGroups {
		tgARNs = append(tgARNs, *tg.TargetGroupArn)
		tgMetaMap[*tg.TargetGroupArn] = &TargetGroupMeta{
			TargetGroup: tg,
			Tags:        make(map[string]string),
		}
	}

	// Enrich TargetGroups with tag information
	tagsIn := elbv2.DescribeTagsInput{
		ResourceArns: tgARNs,
	}
	tagsOut, err := c.ELBV2.DescribeTags(ctx, &tagsIn)
	if err != nil {
		return nil, err
	}
	for _, tagDesc := range tagsOut.TagDescriptions {
		for _, tag := range tagDesc.Tags {
			if _, ok := tgMetaMap[*tagDesc.ResourceArn]; ok {
				tgMetaMap[*tagDesc.ResourceArn].Tags[*tag.Key] = *tag.Value
			}
		}
	}

	// Add Weight information to TargetGroups
	listIn := elbv2.DescribeListenersInput{
		LoadBalancerArn: &loadBalancerARN,
	}
	listOut, err := c.ELBV2.DescribeListeners(ctx, &listIn)
	if err != nil {
		return nil, err
	}

	// NOTE: listeners is typically a single element array
	for _, list := range listOut.Listeners {
		rulesIn := elbv2.DescribeRulesInput{
			ListenerArn: list.ListenerArn,
		}
		rulesOut, err := c.ELBV2.DescribeRules(ctx, &rulesIn)
		if err != nil {
			return nil, err
		}
		// NOTE: rules is typically a two element array containing:
		// 1. a forwarder rule which splits traffic between canary/stable target groups
		// 2. a default rule which returns 404
		for _, rule := range rulesOut.Rules {
			for _, action := range rule.Actions {
				if action.ForwardConfig != nil {
					for _, tgTuple := range action.ForwardConfig.TargetGroups {
						tgMeta, ok := tgMetaMap[*tgTuple.TargetGroupArn]
						if !ok {
							log.Warnf("Found ForwardConfig to TargetGroup for unknown target group: %s", *tgTuple.TargetGroupArn)
							continue
						}
						tgMeta.Weight = tgTuple.Weight
					}
				}
			}
		}
	}
	var tgMeta []TargetGroupMeta
	for _, tgARN := range tgARNs {
		tgMeta = append(tgMeta, *tgMetaMap[tgARN])
	}
	return tgMeta, nil
}

// BuildV2TargetGroupID returns the AWS targetGroup ResourceID that compatible with V2 version.
// Copied from https://github.com/kubernetes-sigs/aws-load-balancer-controller/blob/2e51fbdc5dc978d66e36b376d6dc56b0ae146d8f/internal/alb/generator/tag.go#L125-L128
func BuildV2TargetGroupID(namespace string, ingressName string, serviceName string, servicePort int32) string {
	return fmt.Sprintf("%s/%s-%s:%d", namespace, ingressName, serviceName, servicePort)
}
