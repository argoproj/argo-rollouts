package rollout

import (
	"github.com/argoproj/argo-rollouts/rollout/networking/istio"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

// NetworkingReconciler common function across all networking implementation
type NetworkingReconciler interface {
	Reconcile() error
	Type() string
}

// NewNetworkingReconciler identifies return the networking Plugin that the rollout wants to modify
func (c *RolloutController) NewNetworkingReconciler(roCtx rolloutContext, desiredWeight int32) NetworkingReconciler {
	rollout := roCtx.Rollout()
	if rollout.Spec.Strategy.Canary.Networking == nil {
		return nil
	}
	if rollout.Spec.Strategy.Canary.Networking.Istio != nil {
		return istio.NewReconciler(rollout, desiredWeight, c.dynamicclientset, c.recorder)
	}
	return nil
}

func (c *RolloutController) reconcileNetworking(roCtx *canaryContext) error {
	//TODO(dthomson) base setWeight on previous value if not at desired
	rollout := roCtx.Rollout()
	if rollout.Spec.Strategy.Canary.Networking == nil {
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
	reconciler := c.newNetworkingReconciler(roCtx, desiredWeight)
	if reconciler == nil {
		return nil
	}
	roCtx.Log().Infof("Reconciling Networking with type '%s'", reconciler.Type())
	return reconciler.Reconcile()
}
