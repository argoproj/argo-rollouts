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

func (c *RolloutController) reconcileNetworking(roCtx rolloutContext) error {
	//TODO(dthomson) base setWeight on previous value if not at desired
	desiredWeight := replicasetutil.GetCurrentSetWeight(roCtx.Rollout())
	reconciler := c.NewNetworkingReconciler(roCtx, desiredWeight)
	if reconciler == nil {
		return nil
	}
	roCtx.Log().Infof("Reconciling Networking with type '%s'", reconciler.Type())
	return reconciler.Reconcile()
}
