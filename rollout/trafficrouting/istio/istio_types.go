package istio

import (
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
}

// VirtualServiceHTTPRoute is a HTTP route in a VirtualService
type VirtualServiceHTTPRoute struct {
	Name  string                           `json:"name,omitempty"`
	Route []VirtualServiceRouteDestination `json:"route,omitempty"`
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
