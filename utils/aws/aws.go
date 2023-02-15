package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"

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
	GetTargetGroupHealth(ctx context.Context, targetGroupARN string) ([]elbv2types.TargetHealthDescription, error)
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

// ClientAdapter implements the Client interface
type ClientAdapter struct {
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

// NewClient instantiates a new AWS Client. It is declared as a variable to allow mocking
var NewClient = DefaultNewClientFunc

func DefaultNewClientFunc() (Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		return nil, err
	}
	c := ClientAdapter{
		ELBV2:                elbv2.NewFromConfig(cfg),
		loadBalancerDNStoARN: make(map[string]string),
	}
	return &c, nil
}

func FakeNewClientFunc(elbClient ELBv2APIClient) func() (Client, error) {
	return func() (Client, error) {
		c := ClientAdapter{
			ELBV2:                elbClient,
			loadBalancerDNStoARN: make(map[string]string),
		}
		return &c, nil
	}
}

func (c *ClientAdapter) FindLoadBalancerByDNSName(ctx context.Context, dnsName string) (*elbv2types.LoadBalancer, error) {
	paginator := elbv2.NewDescribeLoadBalancersPaginator(c.ELBV2, &elbv2.DescribeLoadBalancersInput{
		PageSize: aws.Int32(defaults.DefaultAwsLoadBalancerPageSize),
	})
	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, lb := range output.LoadBalancers {
			if lb.DNSName != nil && *lb.DNSName == dnsName {
				return &lb, nil
			}
		}
	}
	return nil, nil
}

// GetTargetGroupMetadata is a convenience to retrieve the target groups of a load balancer along
// with relevant metadata (tags, and traffic weights).
func (c *ClientAdapter) GetTargetGroupMetadata(ctx context.Context, loadBalancerARN string) ([]TargetGroupMeta, error) {
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
	describeTagsLimit := defaults.GetDescribeTagsLimit()
	tgARNsCount := len(tgARNs)
	for i := 0; i < tgARNsCount; i += describeTagsLimit {
		j := i + describeTagsLimit
		if j > tgARNsCount {
			// last batch
			j = tgARNsCount
		}

		tagsIn := elbv2.DescribeTagsInput{
			ResourceArns: tgARNs[i:j],
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

// GetTargetGroupHealth returns health descriptions of registered targets in a target group.
// A TargetHealthDescription is an IP:port pair, along with its health status.
func (c *ClientAdapter) GetTargetGroupHealth(ctx context.Context, targetGroupARN string) ([]elbv2types.TargetHealthDescription, error) {
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

// getNumericTargetPort resolves the numeric port which a AWS TargetGroup targets.
// This is needed in case the TargetGroupBinding's spec.serviceRef.Port is a string and not a number
// and/or the Service's targetPort is a string and not a number
// Returns 0 if unable to find matching port in given service.
func getNumericTargetPort(tgb TargetGroupBinding, svc corev1.Service, endpoints corev1.Endpoints) int32 {
	var servicePortNum int32
	var servicePortName string
	if portInt := tgb.Spec.ServiceRef.Port.IntVal; portInt > 0 {
		servicePortNum = portInt
	} else {
		servicePortName = tgb.Spec.ServiceRef.Port.String()
	}
	for _, svcPort := range svc.Spec.Ports {
		if (servicePortName != "" && servicePortName == svcPort.Name) || (servicePortNum > 0 && servicePortNum == svcPort.Port) {
			if targetPortNum := svcPort.TargetPort.IntVal; targetPortNum > 0 {
				return targetPortNum
			}
			// targetPort is a string and not a num. Must resort to looking at endpoints
			targetPortName := svcPort.TargetPort.String()
			for _, subset := range endpoints.Subsets {
				for _, port := range subset.Ports {
					if port.Name == targetPortName {
						return port.Port
					}
				}
			}
		}
	}
	return 0
}

// TargetGroupVerifyResult returns metadata when a target group is verified.
type TargetGroupVerifyResult struct {
	Service             string
	Verified            bool
	EndpointsRegistered int
	EndpointsTotal      int
}

// VerifyTargetGroupBinding verifies if the underlying AWS TargetGroup has all Pod IPs and ports
// from the given service (the K8s Endpoints list) registered to the TargetGroup.
// NOTE: a previous version of this method used to additionally verify that all registered targets
// were "healthy" (in addition to registered), but the health of registered targets is actually
// irrelevant for our purposes of verifying the service label change was reflected in the LB.
// Returns nil if the verification is not applicable (e.g. target type is not IP)
func VerifyTargetGroupBinding(ctx context.Context, logCtx *log.Entry, awsClnt Client, tgb TargetGroupBinding, endpoints *corev1.Endpoints, svc *corev1.Service) (*TargetGroupVerifyResult, error) {
	if tgb.Spec.TargetType == nil || *tgb.Spec.TargetType != TargetTypeIP {
		// We only need to verify target groups using AWS CNI (spec.targetType: ip)
		return nil, nil
	}
	port := getNumericTargetPort(tgb, *svc, *endpoints)
	if port == 0 {
		logCtx.Warn("Unable to match TargetGroupBinding spec.serviceRef.port to Service spec.ports")
		return nil, nil
	}
	logCtx = logCtx.WithFields(map[string]interface{}{
		"service":            svc.Name,
		"targetgroupbinding": tgb.Name,
		"tg":                 tgb.Spec.TargetGroupARN,
		"port":               port,
	})
	targets, err := awsClnt.GetTargetGroupHealth(ctx, tgb.Spec.TargetGroupARN)
	if err != nil {
		return nil, err
	}

	// Remember/initialize all of the ip:port of the endpoints list that we expect to see registered
	endpointIPs := make(map[string]bool)
	for _, subset := range endpoints.Subsets {
		for _, addr := range subset.Addresses {
			endpointIPs[fmt.Sprintf("%s:%d", addr.IP, port)] = false
		}
	}

	logCtx.Infof("verifying %d endpoint addresses (of %d targets)", len(endpointIPs), len(targets))
	var ignored []string
	var verified []string
	var unverified []string

	// Iterate all registered targets in AWS TargetGroup. Mark all endpoint IPs which we see registered
	for _, target := range targets {
		if target.Target == nil || target.Target.Id == nil || target.Target.Port == nil {
			logCtx.Warnf("Invalid target in TargetGroup: %v", target)
			continue
		}
		targetStr := fmt.Sprintf("%s:%d", *target.Target.Id, *target.Target.Port)
		_, isEndpointTarget := endpointIPs[targetStr]
		if !isEndpointTarget {
			ignored = append(ignored, targetStr)
			// this is a target for something not in the endpoint list (e.g. old endpoint entry). Ignore it
			continue
		}
		// Verify we see the endpoint IP registered to the TargetGroup
		// NOTE: we used to check health here, but health is not relevant for verifying service label change
		endpointIPs[targetStr] = true
		verified = append(verified, targetStr)
	}

	tgvr := TargetGroupVerifyResult{
		Service:             svc.Name,
		EndpointsTotal:      len(endpointIPs),
		EndpointsRegistered: len(verified),
	}

	// Check if any of our desired endpoints are not yet registered
	for epIP, seen := range endpointIPs {
		if !seen {
			unverified = append(unverified, epIP)
		}
	}

	logCtx.Infof("Ignored targets: %s", strings.Join(ignored, ", "))
	logCtx.Infof("Verified targets: %s", strings.Join(verified, ", "))
	logCtx.Infof("Unregistered targets: %s", strings.Join(unverified, ", "))

	tgvr.Verified = bool(tgvr.EndpointsRegistered == tgvr.EndpointsTotal)
	return &tgvr, nil
}
