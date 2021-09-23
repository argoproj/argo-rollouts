package rollout

import (
	"reflect"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/alb"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/ambassador"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/nginx"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/smi"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
)

// NewTrafficRoutingReconciler identifies return the TrafficRouting Plugin that the rollout wants to modify
func (c *Controller) NewTrafficRoutingReconciler(roCtx *rolloutContext) (trafficrouting.TrafficRoutingReconciler, error) {
	rollout := roCtx.rollout
	if rollout.Spec.Strategy.Canary.TrafficRouting == nil {
		return nil, nil
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.Istio != nil {
		if c.IstioController.VirtualServiceInformer.HasSynced() {
			return istio.NewReconciler(rollout, c.IstioController.DynamicClientSet, c.recorder, c.IstioController.VirtualServiceLister, c.IstioController.DestinationRuleLister), nil
		} else {
			return istio.NewReconciler(rollout, c.IstioController.DynamicClientSet, c.recorder, nil, nil), nil
		}
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.Nginx != nil {
		return nginx.NewReconciler(nginx.ReconcilerConfig{
			Rollout:        rollout,
			Client:         c.kubeclientset,
			Recorder:       c.recorder,
			ControllerKind: controllerKind,
			IngressLister:  c.ingressesLister,
		}), nil
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.ALB != nil {
		return alb.NewReconciler(alb.ReconcilerConfig{
			Rollout:        rollout,
			Client:         c.kubeclientset,
			Recorder:       c.recorder,
			ControllerKind: controllerKind,
			IngressLister:  c.ingressesLister,
		})
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.SMI != nil {
		return smi.NewReconciler(smi.ReconcilerConfig{
			Rollout:        rollout,
			Client:         c.smiclientset,
			Recorder:       c.recorder,
			ControllerKind: controllerKind,
		})
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.Ambassador != nil {
		ac := ambassador.NewDynamicClient(c.dynamicclientset, rollout.GetNamespace())
		return ambassador.NewReconciler(rollout, ac, c.recorder), nil
	}
	return nil, nil
}

func (c *rolloutContext) reconcileTrafficRouting() error {
	reconciler, err := c.newTrafficRoutingReconciler(c)
	if err != nil {
		return err
	}
	if reconciler == nil {
		// Not using traffic routing
		c.newStatus.Canary.Weights = nil
		return nil
	}
	c.log.Infof("Reconciling TrafficRouting with type '%s'", reconciler.Type())

	var canaryHash, stableHash string
	if c.stableRS != nil {
		stableHash = c.stableRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	}
	if c.newRS != nil {
		canaryHash = c.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	}
	err = reconciler.UpdateHash(canaryHash, stableHash)
	if err != nil {
		return err
	}

	currentStep, index := replicasetutil.GetCurrentCanaryStep(c.rollout)
	desiredWeight := int32(0)
	weightDestinations := make([]v1alpha1.WeightDestination, 0)
	if rolloututil.IsFullyPromoted(c.rollout) {
		// when we are fully promoted. desired canary weight should be 0
	} else if c.pauseContext.IsAborted() {
		// when aborted, desired canary weight should be 0 (100% to stable), *unless* we
		// are using dynamic stable scaling. In that case, we can only decrease canary weight
		// according to available replica counts of the stable.
		if c.rollout.Spec.Strategy.Canary.DynamicStableScale {
			desiredWeight = 100 - ((100 * c.stableRS.Status.AvailableReplicas) / *c.rollout.Spec.Replicas)
		}
	} else if c.newRS == nil || c.newRS.Status.AvailableReplicas == 0 {
		// when newRS is not available or replicas num is 0. never weight to canary
	} else if index != nil {
		atDesiredReplicaCount := replicasetutil.AtDesiredReplicaCountsForCanary(c.rollout, c.newRS, c.stableRS, c.otherRSs, nil)
		if !atDesiredReplicaCount {
			// Use the previous weight since the new RS is not ready for a new weight
			for i := *index - 1; i >= 0; i-- {
				step := c.rollout.Spec.Strategy.Canary.Steps[i]
				if step.SetWeight != nil {
					desiredWeight = *step.SetWeight
					break
				}
			}
		} else if *index != int32(len(c.rollout.Spec.Strategy.Canary.Steps)) {
			// This if statement prevents the desiredWeight from being set to 100
			// when the rollout has progressed through all the steps. The rollout
			// should send all traffic to the stable service by using a weight of
			// 0. If the rollout is progressing through the steps, the desired
			// weight of the traffic routing service should be at the value of the
			// last setWeight step, which is set by GetCurrentSetWeight.
			desiredWeight = replicasetutil.GetCurrentSetWeight(c.rollout)
		}

		// Checks for experiment step
		// If current experiment exists, then create WeightDestinations for each experiment template
		exStep := replicasetutil.GetCurrentExperimentStep(c.rollout)
		if exStep != nil && c.currentEx != nil && c.currentEx.Status.Phase == v1alpha1.AnalysisPhaseRunning {
			getTemplateWeight := func(name string) *int32 {
				for _, tmpl := range exStep.Templates {
					if tmpl.Name == name {
						return tmpl.Weight
					}
				}
				return nil
			}
			for _, templateStatus := range c.currentEx.Status.TemplateStatuses {
				templateWeight := getTemplateWeight(templateStatus.Name)
				weightDestinations = append(weightDestinations, v1alpha1.WeightDestination{
					ServiceName:     templateStatus.ServiceName,
					PodTemplateHash: templateStatus.PodTemplateHash,
					Weight:          *templateWeight,
				})
			}
		}
	}

	err = reconciler.SetWeight(desiredWeight, weightDestinations...)
	if err != nil {
		c.recorder.Warnf(c.rollout, record.EventOptions{EventReason: "TrafficRoutingError"}, err.Error())
		return err
	}
	if modified, newWeights := calculateWeightStatus(c.rollout, canaryHash, stableHash, desiredWeight, weightDestinations...); modified {
		c.log.Infof("Previous weights: %v", c.rollout.Status.Canary.Weights)
		c.log.Infof("New weights: %v", newWeights)
		c.newStatus.Canary.Weights = newWeights
	}

	// If we are in the middle of an update at a setWeight step, also perform weight verification.
	// Note that we don't do this every reconciliation because weight verification typically involves
	// API calls to the cloud provider which could incur rate limiting
	shouldVerifyWeight := c.rollout.Status.StableRS != "" &&
		!rolloututil.IsFullyPromoted(c.rollout) &&
		currentStep != nil && currentStep.SetWeight != nil

	if shouldVerifyWeight {
		weightVerified, err := reconciler.VerifyWeight(desiredWeight, weightDestinations...)
		c.newStatus.Canary.Weights.Verified = weightVerified
		if err != nil {
			c.recorder.Warnf(c.rollout, record.EventOptions{EventReason: conditions.WeightVerifyErrorReason}, conditions.WeightVerifyErrorMessage, err)
			return nil // return nil instead of error since we want to continue with normal reconciliation
		}
		if weightVerified != nil {
			if *weightVerified {
				c.log.Infof("Desired weight (stepIdx: %d) %d verified", *index, desiredWeight)
			} else {
				c.log.Infof("Desired weight (stepIdx: %d) %d not yet verified", *index, desiredWeight)
				c.enqueueRolloutAfter(c.rollout, defaults.GetRolloutVerifyRetryInterval())
			}
		}
	}

	return nil
}

// calculateWeightStatus calculates the Rollout's `status.canary.weights` values. Returns true if
// it has changed from previous values (which indicates we should reset status.canary.weights.verified)
func calculateWeightStatus(ro *v1alpha1.Rollout, canaryHash, stableHash string, desiredWeight int32, weightDestinations ...v1alpha1.WeightDestination) (bool, *v1alpha1.TrafficWeights) {
	weights := v1alpha1.TrafficWeights{
		Canary: v1alpha1.WeightDestination{
			Weight:          desiredWeight,
			PodTemplateHash: canaryHash,
			ServiceName:     ro.Spec.Strategy.Canary.CanaryService,
		},
	}
	stableWeight := 100 - desiredWeight
	for _, weightDest := range weightDestinations {
		weights.Additional = append(weights.Additional, weightDest)
		stableWeight -= weightDest.Weight
	}
	weights.Stable.Weight = stableWeight
	weights.Stable.PodTemplateHash = stableHash
	weights.Stable.ServiceName = ro.Spec.Strategy.Canary.StableService

	prevWeights := ro.Status.Canary.Weights
	modified := prevWeights == nil ||
		prevWeights.Canary != weights.Canary ||
		prevWeights.Stable != weights.Stable ||
		!reflect.DeepEqual(prevWeights.Additional, weights.Additional)
	return modified, &weights
}
