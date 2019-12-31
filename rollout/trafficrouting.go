package rollout

import (
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

// TrafficRoutingReconciler common function across all TrafficRouting implementation
type TrafficRoutingReconciler interface {
	Reconcile() error
	Type() string
}

// NewTrafficRoutingReconciler identifies return the TrafficRouting Plugin that the rollout wants to modify
func (c *RolloutController) NewTrafficRoutingReconciler(roCtx rolloutContext, desiredWeight int32) TrafficRoutingReconciler {
	rollout := roCtx.Rollout()
	if rollout.Spec.Strategy.Canary.TrafficRouting == nil {
		return nil
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.Istio != nil {
		return istio.NewReconciler(rollout, desiredWeight, c.dynamicclientset, c.recorder, c.defaultIstioVersion)
	}
	return nil
}

func (c *RolloutController) reconcileTrafficRouting(roCtx *canaryContext) error {
	//TODO(dthomson) base setWeight on previous value if not at desired
	rollout := roCtx.Rollout()
	if rollout.Spec.Strategy.Canary.TrafficRouting == nil {
		return nil
	}
	newRS := roCtx.NewRS()
	stableRS := roCtx.StableRS()
	olderRS := roCtx.OlderRSs()

	atDesiredReplicaCount := replicasetutil.AtDesiredReplicaCountsForCanary(rollout, newRS, stableRS, olderRS)
	_, index := replicasetutil.GetCurrentCanaryStep(rollout)
	desiredWeight := int32(0)
	if !atDesiredReplicaCount {
		//Use the previous weight since the new RS is not ready for neww weight
		for i := *index - 1; i >= 0; i-- {
			step := rollout.Spec.Strategy.Canary.Steps[i]
			if step.SetWeight != nil {
				desiredWeight = *step.SetWeight
				break
			}
		}
		// This if statement prevents the desiredWeight being set to 100
		// when the rollout has progressed through all the steps. The rollout
		// should send all traffic to the stable service by using a weight of 0
	} else if *index != int32(len(rollout.Spec.Strategy.Canary.Steps)) {
		desiredWeight = replicasetutil.GetCurrentSetWeight(rollout)
	}
	reconciler := c.newTrafficRoutingReconciler(roCtx, desiredWeight)
	if reconciler == nil {
		return nil
	}
	roCtx.Log().Infof("Reconciling TrafficRouting with type '%s'", reconciler.Type())
	return reconciler.Reconcile()
}
