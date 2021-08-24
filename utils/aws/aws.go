package aws

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/aws/aws-sdk-go-v2/config"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
)

// AWSLoadBalancerV2TagKeyResourceID is the tag applied to an AWS resource by the AWS Load Balancer
// controller, to associate it to the corresponding kubernetes resource. This is used by the rollout
// controller to identify the correct TargetGroups associated with the LoadBalancer. For AWS
// target group service references, the format is: <namespace>/<ingress-name>-<service-name>:<port>
// Example: ingress.k8s.aws/resource: default/alb-rollout-ingress-alb-rollout-stable:80
// See: https://kubernetes-sigs.github.io/aws-load-balancer-controller/v2.2/guide/ingress/annotations/#resource-tags
// https://github.com/kubernetes-sigs/aws-load-balancer-controller/blob/da8951f80521651e0a1ffe1361c011d6baad7706/pkg/deploy/tracking/provider.go#L19
const AWSLoadBalancerV2TagKeyResourceID = "ingress.k8s.aws/resource"

// TargetType is the targetType of your ELBV2 TargetGroup.
//
// * with `instance` TargetType, nodes with nodePort for your service will be registered as targets
// * with `ip` TargetType, Pods with containerPort for your service will be registered as targets
type TargetType string

const (
	TargetTypeInstance TargetType = "instance"
	TargetTypeIP       TargetType = "ip"
)

type Client interface {
	GetTargetGroupTargets(ctx context.Context, targetGroupARN string) ([]elbv2types.TargetHealthDescription, error)
	GetTargetGroupMetadata(ctx context.Context, loadBalancerARN string) ([]TargetGroupMeta, error)
	FindLoadBalancerByDNSName(ctx context.Context, dnsName string) (*elbv2types.LoadBalancer, error)
}

// ELBv2APIClient is an interface that enables mocking of the ELBv2 API
type ELBv2APIClient interface {
	elbv2.DescribeTargetGroupsAPIClient
	elbv2.DescribeLoadBalancersAPIClient
	elbv2.DescribeListenersAPIClient
	DescribeTargetHealth(ctx context.Context, params *elbv2.DescribeTargetHealthInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTargetHealthOutput, error)
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

// TargetGroupBinding is the Schema for the TargetGroupBinding API
// This is a subset of actual type definition and should only be used for readonly operations
// https://github.com/kubernetes-sigs/aws-load-balancer-controller/blob/v2.2.1/apis/elbv2/v1beta1/targetgroupbinding_types.go
type TargetGroupBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TargetGroupBindingSpec `json:"spec,omitempty"`
}

// TargetGroupBindingSpec defines the desired state of TargetGroupBinding
type TargetGroupBindingSpec struct {
	// targetGroupARN is the Amazon Resource Name (ARN) for the TargetGroup.
	TargetGroupARN string `json:"targetGroupARN"`

	// targetType is the TargetType of TargetGroup. If unspecified, it will be automatically inferred.
	// +optional
	TargetType *TargetType `json:"targetType,omitempty"`

	// serviceRef is a reference to a Kubernetes Service and ServicePort.
	ServiceRef ServiceReference `json:"serviceRef"`
}

// ServiceReference defines reference to a Kubernetes Service and its ServicePort.
type ServiceReference struct {
	// Name is the name of the Service.
	Name string `json:"name"`

	// Port is the port of the ServicePort.
	Port intstr.IntOrString `json:"port"`
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

func (c *client) GetTargetGroupTargets(ctx context.Context, targetGroupARN string) ([]elbv2types.TargetHealthDescription, error) {
	// Get target groups associated with LoadBalancer
	thIn := elbv2.DescribeTargetHealthInput{
		TargetGroupArn: &targetGroupARN,
	}
	thOut, err := c.ELBV2.DescribeTargetHealth(ctx, &thIn)
	if err != nil {
		return nil, err
	}
	return thOut.TargetHealthDescriptions, nil
}

// BuildTargetGroupResourceID returns the AWS TargetGroup ResourceID
// Adapted from https://github.com/kubernetes-sigs/aws-load-balancer-controller/blob/57c8ce344fe09089fa2e20b0aa9fc0972696bc05/pkg/ingress/model_build_target_group.go#L398-L400
func BuildTargetGroupResourceID(namespace string, ingressName string, serviceName string, servicePort int32) string {
	return fmt.Sprintf("%s/%s-%s:%d", namespace, ingressName, serviceName, servicePort)
}

func GetTargetGroupBindingsGVR() (schema.GroupVersionResource, error) {
	gv, err := schema.ParseGroupVersion(defaults.GetTargetGroupBindingAPIVersion())
	if err != nil {
		return schema.GroupVersionResource{}, err
	}
	return schema.GroupVersionResource{
		Group:    gv.Group,
		Version:  gv.Version,
		Resource: "targetgroupbindings",
	}, nil
}

func GetTargetGroupBindingsByService(ctx context.Context, dynamicClient dynamic.Interface, svc corev1.Service) ([]TargetGroupBinding, error) {
	gvr, err := GetTargetGroupBindingsGVR()
	if err != nil {
		return nil, err
	}
	tgbList, err := dynamicClient.Resource(gvr).Namespace(svc.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var tgbs []TargetGroupBinding
	for _, tgbUn := range tgbList.Items {
		tgb, err := toTargetGroupBinding(tgbUn.Object)
		if err != nil {
			return nil, err
		}
		if tgb.Spec.ServiceRef.Name != svc.Name {
			continue
		}
		for _, port := range svc.Spec.Ports {
			if tgb.Spec.ServiceRef.Port.Type == intstr.Int && port.Port == tgb.Spec.ServiceRef.Port.IntVal {
				tgbs = append(tgbs, *tgb)
				break
			} else if tgb.Spec.ServiceRef.Port.StrVal != "" && port.Name == tgb.Spec.ServiceRef.Port.StrVal {
				tgbs = append(tgbs, *tgb)
				break
			}
		}
	}
	return tgbs, nil
}

func toTargetGroupBinding(obj map[string]interface{}) (*TargetGroupBinding, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	var tgb TargetGroupBinding
	err = json.Unmarshal(data, &tgb)
	if err != nil {
		return nil, err
	}
	return &tgb, nil
}

// getNumericPort resolves the numeric port which a AWS TargetGroup targets.
// This is needed in case the TargetGroupBinding's spec.serviceRef.Port is a string and not a number
// Returns 0 if unable to find matching port in given service.
func getNumericPort(tgb TargetGroupBinding, svc corev1.Service) int32 {
	if portInt := tgb.Spec.ServiceRef.Port.IntValue(); portInt > 0 {
		return int32(portInt)
	}
	// port is a string and not a num
	for _, svcPort := range svc.Spec.Ports {
		if tgb.Spec.ServiceRef.Port.StrVal == svcPort.Name {
			return svcPort.Port
		}
	}
	return 0
}

// VerifyTargetGroupBinding verifies if the underlying AWS TargetGroup:
// 1. targets all the Pod IPs and port in the given service
// 2. those targets are in a healthy state
func VerifyTargetGroupBinding(ctx context.Context, logCtx *log.Entry, awsClnt Client, tgb TargetGroupBinding, endpoints *corev1.Endpoints, svc *corev1.Service) (bool, error) {
	if tgb.Spec.TargetType == nil || *tgb.Spec.TargetType != TargetTypeIP {
		// We only need to verify target groups using AWS CNI (spec.targetType: ip)
		return true, nil
	}
	port := getNumericPort(tgb, *svc)
	if port == 0 {
		logCtx.Warn("Unable to match TargetGroupBinding spec.serviceRef.port to Service spec.ports")
		return false, nil
	}
	logCtx = logCtx.WithFields(map[string]interface{}{
		"service":            svc.Name,
		"targetgroupbinding": tgb.Name,
		"tg":                 tgb.Spec.TargetGroupARN,
		"port":               port,
	})
	targets, err := awsClnt.GetTargetGroupTargets(ctx, tgb.Spec.TargetGroupARN)
	if err != nil {
		return false, err
	}

	// Remember all of the ip:port of the endpoints list
	endpointIPs := make(map[string]bool)
	for _, subset := range endpoints.Subsets {
		for _, addr := range subset.Addresses {
			endpointIPs[fmt.Sprintf("%s:%d", addr.IP, port)] = false
		}
	}

	logCtx.Infof("verifying %d endpoint addresses (of %d targets)", len(endpointIPs), len(targets))

	// Iterate all targets in AWS TargetGroup. Mark all endpoint IPs which are healthy
	for _, target := range targets {
		if target.Target == nil || target.Target.Id == nil || target.Target.Port == nil || target.TargetHealth == nil {
			logCtx.Warnf("Invalid target in TargetGroup: %v", target)
			continue
		}
		targetStr := fmt.Sprintf("%s:%d", *target.Target.Id, *target.Target.Port)
		_, isEndpointTarget := endpointIPs[targetStr]
		if !isEndpointTarget {
			// this is a target for something not in the endpoint list (e.g. old endpoint entry). Ignore it
			continue
		}
		// Mark the endpoint IP as healthy or not
		endpointIPs[targetStr] = bool(target.TargetHealth.State == elbv2types.TargetHealthStateEnumHealthy)
	}

	// Check if any of our desired endpoints are not yet healthy
	for epIP, healthy := range endpointIPs {
		if !healthy {
			logCtx.Infof("Service endpoint IP %s not yet targeted or healthy", epIP)
			return false, nil
		}
	}
	logCtx.Info("TargetGroupBinding verified")
	return true, nil
}
