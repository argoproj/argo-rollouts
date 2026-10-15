package promotion

import (
	"context"
	"fmt"
	"strconv"

	"github.com/argoproj/argo-rollouts/controller/promotion/traffic"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// Translate the old API at the boundary. The execution path only sees providers.
func trafficConfig(target Target) *v1alpha1.RolloutPromotionTraffic {
	if target.Traffic != nil {
		return target.Traffic.DeepCopy()
	}
	ws := target.WeightSwitch
	if ws == nil || !ws.Enabled {
		return nil
	}
	apply := !ws.ApplyOnPromotionSet || ws.ApplyOnPromotion
	return &v1alpha1.RolloutPromotionTraffic{
		Provider: traffic.PodAnnotation, ApplyOnPromotion: &apply,
		Config: map[string]string{
			"annotationKey": ws.AnnotationKey, "baseSelector": ws.BaseSelector,
			"activeWeight": ws.ActiveWeight, "inactiveWeight": ws.InactiveWeight,
			"reapplyDelaySeconds": strconv.Itoa(ws.ReapplyDelaySeconds),
		},
	}
}

func (c *Controller) trafficRegistry() traffic.Registry {
	if c.trafficProviders != nil {
		return c.trafficProviders
	}
	return traffic.NewRegistry(c.kubeClient)
}

func (c *Controller) trafficTarget(ctx context.Context, namespace string, target Target, hash string) (traffic.Provider, traffic.Target, error) {
	config := trafficConfig(target)
	if config == nil {
		return nil, traffic.Target{}, fmt.Errorf("rollout %s requires a traffic provider", target.Name)
	}
	provider, err := c.trafficRegistry().Resolve(config.Provider)
	if err != nil {
		return nil, traffic.Target{}, err
	}
	ro, err := c.getTypedRollout(ctx, namespace, target.Name)
	if err != nil {
		return nil, traffic.Target{}, err
	}
	if target.ExpectedRolloutUID != "" && target.ExpectedRolloutUID != string(ro.UID) {
		return nil, traffic.Target{}, fmt.Errorf("Rollout UID changed before traffic operation")
	}
	input := traffic.Target{Rollout: ro, Hash: hash, Config: config.Config}
	if err := provider.Validate(input); err != nil {
		return nil, input, err
	}
	return provider, input, nil
}

func (c *Controller) adoptionProvider() traffic.Adoption {
	// Legacy adoption is an explicit, annotation-only compatibility contract.
	// Ordinary promotion and other adapters never enter this path.
	provider, _ := c.trafficRegistry().Resolve(traffic.PodAnnotation)
	adoption, _ := provider.(traffic.Adoption)
	return adoption
}

func reapplyDelay(target Target) int {
	config := trafficConfig(target)
	if config == nil {
		return 0
	}
	value, _ := strconv.Atoi(config.Config["reapplyDelaySeconds"])
	return value
}
