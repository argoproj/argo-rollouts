package ingress

import (
	json2 "encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/client-go/discovery"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/diff"
	"github.com/argoproj/argo-rollouts/utils/json"
)

const (
	// CanaryIngressSuffix is the name suffix all canary ingresses created by the rollouts controller will have
	CanaryIngressSuffix = "-canary"
	// ManagedActionsAnnotation holds list of ALB actions that are managed by rollouts
	// DEPRECATED in favor of ManagedAnnotations
	ManagedActionsAnnotation = "rollouts.argoproj.io/managed-alb-actions"
	// ManagedAnnotations holds list of ALB annotations that are managed by rollouts supports multiple annotations
	ManagedAnnotations = "rollouts.argoproj.io/managed-alb-annotations"
	//ALBIngressAnnotation is the prefix annotation that is used by the ALB Ingress controller to configure an ALB
	ALBIngressAnnotation = "alb.ingress.kubernetes.io"
	// ALBActionPrefix the prefix to specific actions within an ALB ingress.
	ALBActionPrefix = "/actions."
	// ALBConditionPrefix the prefix to specific conditions within an ALB ingress.
	ALBConditionPrefix = "/conditions."
)

// ALBAction describes an ALB action that configure the behavior of an ALB. This struct is marshaled into a string
// that is added to the Ingress's annotations.
type ALBAction struct {
	Type          string           `json:"Type"`
	ForwardConfig ALBForwardConfig `json:"ForwardConfig"`
}

// ALBCondition describes an ALB action condition that configure the behavior of an ALB. This struct is marshaled into a string
// that is added to the Ingress's annotations.
type ALBCondition struct {
	Field            string           `json:"field"`
	HttpHeaderConfig HttpHeaderConfig `json:"httpHeaderConfig"`
}

// HttpHeaderConfig describes header config for the ALB action condition
type HttpHeaderConfig struct {
	HttpHeaderName string   `json:"httpHeaderName"`
	Values         []string `json:"values"`
}

// ALBForwardConfig describes a list of target groups that the ALB should route traffic towards
type ALBForwardConfig struct {
	TargetGroups                []ALBTargetGroup                `json:"TargetGroups"`
	TargetGroupStickinessConfig *ALBTargetGroupStickinessConfig `json:"TargetGroupStickinessConfig,omitempty"`
}

// ALBTargetGroupStickinessConfig describes settings for the listener to apply to all forwards
type ALBTargetGroupStickinessConfig struct {
	Enabled         bool  `json:"Enabled"`
	DurationSeconds int64 `json:"DurationSeconds"`
}

// ALBTargetGroup holds the weight to send to a specific destination consisting of a K8s service and port or ARN
type ALBTargetGroup struct {
	// the K8s service Name
	ServiceName string `json:"ServiceName,omitempty"`
	// the K8s service port
	ServicePort string `json:"ServicePort,omitempty"`
	// The weight. The range is 0 to 999.
	Weight *int64 `json:"Weight,omitempty"`
}

func MultipleNginxIngressesConfigured(rollout *v1alpha1.Rollout) bool {
	return rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngresses != nil
}

func SingleNginxIngressConfigured(rollout *v1alpha1.Rollout) bool {
	return rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress != ""
}

func MultipleAlbIngressesConfigured(rollout *v1alpha1.Rollout) bool {
	return rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingresses != nil
}

func SingleAlbIngressConfigured(rollout *v1alpha1.Rollout) bool {
	return rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress != ""
}

// GetRolloutIngressKeys returns ingresses keys (namespace/ingressName) which are referenced by specified rollout
func GetRolloutIngressKeys(rollout *v1alpha1.Rollout) []string {
	var ingresses []string
	if rollout.Spec.Strategy.Canary != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress != "" {

		stableIngress := rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress
		// Also start watcher for `-canary` ingress which is created by the trafficmanagement controller
		ingresses = append(
			ingresses,
			fmt.Sprintf("%s/%s", rollout.Namespace, stableIngress),
			fmt.Sprintf("%s/%s", rollout.Namespace, GetCanaryIngressName(rollout.GetName(), stableIngress)),
		)
	}

	// Scenario where one rollout is managing multiple Ngnix ingresses.
	if rollout.Spec.Strategy.Canary != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngresses != nil {

		for _, stableIngress := range rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngresses {
			// Also start watcher for `-canary` ingress which is created by the trafficmanagement controller
			ingresses = append(
				ingresses,
				fmt.Sprintf("%s/%s", rollout.Namespace, stableIngress),
				fmt.Sprintf("%s/%s", rollout.Namespace, GetCanaryIngressName(rollout.GetName(), stableIngress)),
			)
		}
	}

	if rollout.Spec.Strategy.Canary != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.ALB != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress != "" {
		ingresses = append(
			ingresses,
			fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress),
		)
	}

	// Scenario where one rollout is managing multiple ALB ingresses.
	if rollout.Spec.Strategy.Canary != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.ALB != nil &&
		rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingresses != nil {

		for _, ingress := range rollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingresses {
			ingresses = append(
				ingresses,
				fmt.Sprintf("%s/%s", rollout.Namespace, ingress),
			)
		}
	}

	return ingresses
}

// GetCanaryIngressName constructs the name to use for the canary ingress resource from a given Rollout
func GetCanaryIngressName(rolloutName, stableIngressName string) string {
	// names limited to 253 characters
	if stableIngressName != "" {
		prefix := fmt.Sprintf("%s-%s", rolloutName, stableIngressName)
		if len(prefix) > 253-len(CanaryIngressSuffix) {
			// trim prefix
			prefix = prefix[0 : 253-len(CanaryIngressSuffix)]
		}
		return fmt.Sprintf("%s%s", prefix, CanaryIngressSuffix)
	}
	return ""
}

// HasRuleWithService check if an Ingress has a service in one of it's rules
func HasRuleWithService(i *Ingress, svc string) bool {
	switch i.mode {
	case IngressModeNetworking:
		return hasIngressRuleWithService(i.ingress, svc)
	case IngressModeExtensions:
		return hasLegacyIngressRuleWithService(i.legacyIngress, svc)
	default:
		return false
	}

}

func hasIngressRuleWithService(ingress *networkingv1.Ingress, svc string) bool {
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP != nil {
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service.Name == svc {
					return true
				}
			}
		}
	}
	return false
}

func hasLegacyIngressRuleWithService(ingress *extensionsv1beta1.Ingress, svc string) bool {
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP != nil {
			for _, path := range rule.HTTP.Paths {
				if path.Backend.ServiceName == svc {
					return true
				}
			}
		}
	}
	return false
}

// ManagedALBActions a mapping of Rollout names to the ALB action that the Rollout manages
type ManagedALBActions map[string]string

type ManagedALBAnnotations map[string]ManagedALBAnnotation

type ManagedALBAnnotation []string

// String outputs a string of all the managed ALB annotations that is stored in the Ingress's annotations
func (m ManagedALBAnnotations) String() string {
	return string(json.MustMarshal(m))
}

func NewManagedALBAnnotations(json string) (ManagedALBAnnotations, error) {
	res := ManagedALBAnnotations{}
	if json == "" {
		return res, nil
	}
	if err := json2.Unmarshal([]byte(json), &res); err != nil {
		return nil, err
	}
	return res, nil
}

// String outputs a string of all the managed ALB actions that is stored in the Ingress's annotations
func (m ManagedALBActions) String() string {
	str := ""
	for key, value := range m {
		str = fmt.Sprintf("%s%s:%s,", str, key, value)
	}
	if len(str) == 0 {
		return ""
	}
	return str[:len(str)-1]
}

// NewManagedALBActions converts a string into a mapping of the rollouts to managed ALB actions
func NewManagedALBActions(annotation string) (ManagedALBActions, error) {
	m := ManagedALBActions{}
	if len(annotation) == 0 {
		return m, nil
	}
	keys := strings.Split(annotation, ",")
	for _, key := range keys {
		values := strings.Split(key, ":")
		if len(values) != 2 {
			return nil, fmt.Errorf("incorrectly formatted managed actions annotation")
		}
		m[values[0]] = values[1]
	}
	return m, nil
}

// ALBActionAnnotationKey returns the annotation key for a specific action
func ALBActionAnnotationKey(r *v1alpha1.Rollout) string {
	actionService := defaults.GetStringOrDefault(r.Spec.Strategy.Canary.TrafficRouting.ALB.RootService, r.Spec.Strategy.Canary.StableService)
	return albIngressKubernetesIoKey(r, ALBActionPrefix, actionService)
}

// ALBHeaderBasedActionAnnotationKey returns the annotation key for a specific action
func ALBHeaderBasedActionAnnotationKey(r *v1alpha1.Rollout, action string) string {
	return albIngressKubernetesIoKey(r, ALBActionPrefix, action)
}

// ALBHeaderBasedConditionAnnotationKey returns the annotation key for a specific condition
func ALBHeaderBasedConditionAnnotationKey(r *v1alpha1.Rollout, action string) string {
	return albIngressKubernetesIoKey(r, ALBConditionPrefix, action)
}

func albIngressKubernetesIoKey(r *v1alpha1.Rollout, action, service string) string {
	prefix := defaults.GetStringOrDefault(r.Spec.Strategy.Canary.TrafficRouting.ALB.AnnotationPrefix, ALBIngressAnnotation)
	return fmt.Sprintf("%s%s%s", prefix, action, service)
}

type patchConfig struct {
	withAnnotations bool
	withLabels      bool
	withSpec        bool
}

type PatchOption func(p *patchConfig)

func WithAnnotations() PatchOption {
	return func(p *patchConfig) {
		p.withAnnotations = true
	}
}

func WithLabels() PatchOption {
	return func(p *patchConfig) {
		p.withLabels = true
	}
}

func WithSpec() PatchOption {
	return func(p *patchConfig) {
		p.withSpec = true
	}
}

func BuildIngressPatch(mode IngressMode, current, desired *Ingress, opts ...PatchOption) ([]byte, bool, error) {
	cfg := &patchConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	switch mode {
	case IngressModeNetworking:
		return buildIngressPatch(current.ingress, desired.ingress, cfg)
	case IngressModeExtensions:
		return buildIngressPatchLegacy(current.legacyIngress, desired.legacyIngress, cfg)
	default:
		return nil, false, errors.New("error building annotations patch: undefined ingress mode")
	}
}

func buildIngressPatch(current, desired *networkingv1.Ingress, cfg *patchConfig) ([]byte, bool, error) {
	cur := &networkingv1.Ingress{}
	des := &networkingv1.Ingress{}
	if cfg.withAnnotations {
		cur.Annotations = current.Annotations
		des.Annotations = desired.Annotations
	}
	if cfg.withLabels {
		cur.Labels = current.Labels
		des.Labels = desired.Labels
	}
	if cfg.withSpec {
		cur.Spec = current.Spec
		des.Spec = desired.Spec
	}
	return diff.CreateTwoWayMergePatch(cur, des, networkingv1.Ingress{})
}

func buildIngressPatchLegacy(current, desired *extensionsv1beta1.Ingress, cfg *patchConfig) ([]byte, bool, error) {
	cur := &extensionsv1beta1.Ingress{}
	des := &extensionsv1beta1.Ingress{}
	if cfg.withAnnotations {
		cur.Annotations = current.Annotations
		des.Annotations = desired.Annotations
	}
	if cfg.withLabels {
		cur.Labels = current.Labels
		des.Labels = desired.Labels
	}
	if cfg.withSpec {
		cur.Spec = current.Spec
		des.Spec = desired.Spec
	}
	return diff.CreateTwoWayMergePatch(cur, des, extensionsv1beta1.Ingress{})
}

// DetermineIngressMode will first attempt to determine the ingress mode by checking
// the given apiVersion. If it is "extensions/v1beta1" will return IngressModeExtensions.
// If it is "networking/v1" will return IngressModeNetworking. Otherwise it will check
// the kubernetes server version to determine the ingress mode.
func DetermineIngressMode(apiVersion string, d discovery.ServerVersionInterface) (IngressMode, error) {
	if apiVersion == "extensions/v1beta1" {
		return IngressModeExtensions, nil
	}
	if apiVersion == "networking/v1" {
		return IngressModeNetworking, nil
	}

	ver, err := d.ServerVersion()
	if err != nil {
		return 0, err
	}
	major, err := strconv.Atoi(ver.Major)
	if err != nil {
		return 0, err
	}
	verMinor := ver.Minor
	if strings.HasSuffix(ver.Minor, "+") {
		verMinor = ver.Minor[0 : len(ver.Minor)-1]
	}
	minor, err := strconv.Atoi(verMinor)
	if err != nil {
		return 0, err
	}
	if major > 1 {
		return IngressModeNetworking, nil
	}
	if major == 1 && minor >= 19 {
		return IngressModeNetworking, nil
	}
	return IngressModeExtensions, nil
}
