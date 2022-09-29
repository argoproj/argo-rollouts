package istio

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VirtualService is an Istio VirtualService containing only the fields which we care about
type VirtualService struct {
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              VirtualServiceSpec `json:"spec,omitempty"`
}

type VirtualServiceSpec struct {
	HTTP []VirtualServiceHTTPRoute `json:"http,omitempty"`
	TLS  []VirtualServiceTLSRoute  `json:"tls,omitempty"`
	TCP  []VirtualServiceTCPRoute  `json:"tcp,omitempty"`
}

// VirtualServiceHTTPRoute is a HTTP route in a VirtualService
type VirtualServiceHTTPRoute struct {
	Name             string                           `json:"name,omitempty"`
	Match            []RouteMatch                     `json:"match,omitempty"`
	Route            []VirtualServiceRouteDestination `json:"route,omitempty"`
	Mirror           *VirtualServiceDestination       `json:"mirror,omitempty"`
	MirrorPercentage *Percent                         `json:"mirrorPercentage,omitempty"`
}

type RouteMatch struct {
	// Method What http methods should be mirrored
	// +optional
	Method *v1alpha1.StringMatch `json:"method,omitempty" protobuf:"bytes,1,opt,name=method"`
	// Uri What url paths should be mirrored
	// +optional
	Uri *v1alpha1.StringMatch `json:"uri,omitempty" protobuf:"bytes,2,opt,name=uri"`
	// Headers What request with matching headers should be mirrored
	// +optional
	Headers map[string]v1alpha1.StringMatch `json:"headers,omitempty" protobuf:"bytes,3,opt,name=headers"`
}

type Percent struct {
	Value float64 `json:"value,omitempty"`
}

// VirtualServiceTLSRoute is a TLS route in a VirtualService
type VirtualServiceTLSRoute struct {
	Match []TLSMatchAttributes             `json:"match,omitempty"`
	Route []VirtualServiceRouteDestination `json:"route,omitempty"`
}

// TLSMatchAttributes is the route matcher for a TLS route in a VirtualService
type TLSMatchAttributes struct {
	SNI                []string          `json:"sniHosts,omitempty"`
	DestinationSubnets []string          `json:"destinationSubnets,omitempty"`
	Port               int64             `json:"port,omitempty"`
	SourceLabels       map[string]string `json:"sourceLabels,omitempty"`
	Gateways           []string          `json:"gateways,omitempty"`
	SourceNamespace    string            `json:"sourceNamespace,omitempty"`
}

// VirtualServiceTCPRoute is a TLS route in a VirtualService
type VirtualServiceTCPRoute struct {
	Match []L4MatchAttributes              `json:"match,omitempty"`
	Route []VirtualServiceRouteDestination `json:"route,omitempty"`
}

// L4MatchAttributes is the route matcher for a TCP route in a VirtualService
type L4MatchAttributes struct {
	DestinationSubnets []string          `json:"destinationSubnets,omitempty"`
	Port               int64             `json:"port,omitempty"`
	SourceLabels       map[string]string `json:"sourceLabels,omitempty"`
	Gateways           []string          `json:"gateways,omitempty"`
	SourceNamespace    string            `json:"sourceNamespace,omitempty"`
}

// VirtualServiceRouteDestination is a destination within
// { VirtualServiceHTTPRoute, VirtualServiceTLSRoute }
type VirtualServiceRouteDestination struct {
	// Destination holds the destination struct of the virtual service
	Destination VirtualServiceDestination `json:"destination,omitempty"`
	// Weight holds the destination struct of the virtual service
	Weight int64 `json:"weight,omitempty"`
}

// Destination fields within the VirtualServiceDestination struct of the Virtual Service that the controller modifies
type VirtualServiceDestination struct {
	Host   string `json:"host,omitempty"`
	Subset string `json:"subset,omitempty"`
	Port   *Port  `json:"port,omitempty"`
}

type Port struct {
	Number uint32 `json:"number,omitempty"`
}

// DestinationRule is an Istio DestinationRule containing only the fields which we care about
type DestinationRule struct {
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DestinationRuleSpec `json:"spec,omitempty"`
}

type DestinationRuleSpec struct {
	Host    string   `json:"host,omitempty"`
	Subsets []Subset `json:"subsets,omitempty"`
}

type Subset struct {
	Name   string            `json:"name,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	// TrafficPolicy *json.RawMessage  `json:"trafficPolicy,omitempty"`
	Extra map[string]interface{} `json:",omitempty"`
}
