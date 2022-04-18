package rollout

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/alb"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/ambassador"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/appmesh"
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
func (c *Controller) NewTrafficRoutingReconciler(roCtx *rolloutContext) ([]trafficrouting.TrafficRoutingReconciler, error) {
	rollout := roCtx.rollout
	// define an empty list of trafficReconcilers to be populated
	// by the ones declared in the rolloutContext
	trafficReconcilers := []trafficrouting.TrafficRoutingReconciler{}

	if rollout.Spec.Strategy.Canary.TrafficRouting == nil {
		return nil, nil
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.Istio != nil {
		if c.IstioController.VirtualServiceInformer.HasSynced() {
			trafficReconcilers = append(trafficReconcilers, istio.NewReconciler(rollout, c.IstioController.DynamicClientSet, c.recorder, c.IstioController.VirtualServiceLister, c.IstioController.DestinationRuleLister))
		} else {
			trafficReconcilers = append(trafficReconcilers, istio.NewReconciler(rollout, c.IstioController.DynamicClientSet, c.recorder, nil, nil))
		}
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.Nginx != nil {
		trafficReconcilers = append(trafficReconcilers, nginx.NewReconciler(nginx.ReconcilerConfig{
			Rollout:        rollout,
			Client:         c.kubeclientset,
			Recorder:       c.recorder,
			ControllerKind: controllerKind,
			IngressWrapper: c.ingressWrapper,
		}), nil
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.ALB != nil {
		alb_reconcilier, err := alb.NewReconciler(alb.ReconcilerConfig{
			Rollout:        rollout,
			Client:         c.kubeclientset,
			Recorder:       c.recorder,
			ControllerKind: controllerKind,
			IngressWrapper: c.ingressWrapper,
		})
		if err != nil {
			return trafficReconcilers, err
		}
		trafficReconcilers = append(trafficReconcilers, alb_reconcilier)
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.SMI != nil {
		smi_reconcilier, err := smi.NewReconciler(smi.ReconcilerConfig{
			Rollout:        rollout,
			Client:         c.smiclientset,
			Recorder:       c.recorder,
			ControllerKind: controllerKind,
		})
		if err != nil {
			return trafficReconcilers, err
		}
		trafficReconcilers = append(trafficReconcilers, smi_reconcilier)
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.Ambassador != nil {
		ac := ambassador.NewDynamicClient(c.dynamicclientset, rollout.GetNamespace())
		trafficReconcilers = append(trafficReconcilers, ambassador.NewReconciler(rollout, ac, c.recorder))
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.AppMesh != nil {
		trafficReconcilers = append(trafficReconcilers, appmesh.NewReconciler(appmesh.ReconcilerConfig{
			Rollout:  rollout,
			Client:   c.dynamicclientset,
			Recorder: c.recorder,
		}))
	}

	// ensure that the trafficReconcilers is a healthy list and its not empty
	if len(trafficReconcilers) > 0 {
		return trafficReconcilers, nil
	}

	return nil, nil
}

func (c *rolloutContext) reconcileTrafficRouting() error {
	reconcilers, err := c.newTrafficRoutingReconciler(c)
	// a return here does ensure that all trafficReconcilers are healthy
	// and same in syntax
	if err != nil {
		return err
	}
	// ensure that trafficReconcilers list is healthy
	if len(reconcilers) == 0 {
		c.log.Info("No TrafficRouting Reconcilers found")
		c.newStatus.Canary.Weights = nil
		return nil
	}
	if reconcilers == nil {
		// Not using traffic routing
		c.newStatus.Canary.Weights = nil
		return nil
	}
	c.log.Infof("Found %d TrafficRouting Reconcilers", len(reconcilers))
	// iterate over the list of trafficReconcilers
	for _, reconciler := range reconcilers {
		c.log.Infof("Reconciling TrafficRouting with type '%s'", reconciler.Type())

		currentStep, index := replicasetutil.GetCurrentCanaryStep(c.rollout)
		desiredWeight := int32(0)
		weightDestinations := make([]v1alpha1.WeightDestination, 0)

		var canaryHash, stableHash string
		if c.stableRS != nil {
			stableHash = c.stableRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		}
		if c.newRS != nil {
			canaryHash = c.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
		}

		if rolloututil.IsFullyPromoted(c.rollout) {
			// when we are fully promoted. desired canary weight should be 0
		} else if c.pauseContext.IsAborted() {
			// when aborted, desired canary weight should immediately be 0 (100% to stable), *unless*
			// we are using dynamic stable scaling. In that case, we are dynamically decreasing the
			// weight to the canary according to the availability of the stable (whatever it can support).
			if c.rollout.Spec.Strategy.Canary.DynamicStableScale {
				desiredWeight = 100 - ((100 * c.stableRS.Status.AvailableReplicas) / *c.rollout.Spec.Replicas)
				if c.rollout.Status.Canary.Weights != nil {
					// This ensures that if we are already at a lower weight, then we will not
					// increase the weight because stable availability is flapping (e.g. pod restarts)
					desiredWeight = minInt(desiredWeight, c.rollout.Status.Canary.Weights.Canary.Weight)
				}
			}
		} else if c.newRS == nil || c.newRS.Status.AvailableReplicas == 0 {
			// when newRS is not available or replicas num is 0. never weight to canary
			weightDestinations = append(weightDestinations, c.calculateWeightDestinationsFromExperiment()...)
		} else if c.rollout.Status.PromoteFull {
			// on a promote full, desired stable weight should be 0 (100% to canary),
			// But we can only increase canary weight according to available replica counts of the canary.
			// we will need to set the desiredWeight to 0 when the newRS is not available.
			if c.rollout.Spec.Strategy.Canary.DynamicStableScale {
				desiredWeight = (100 * c.newRS.Status.AvailableReplicas) / *c.rollout.Spec.Replicas
			} else if c.rollout.Status.Canary.Weights != nil {
				desiredWeight = c.rollout.Status.Canary.Weights.Canary.Weight
			}
		} else if index != nil {
			atDesiredReplicaCount := replicasetutil.AtDesiredReplicaCountsForCanary(c.rollout, c.newRS, c.stableRS, c.otherRSs, nil)
			if !atDesiredReplicaCount && !c.rollout.Status.PromoteFull {
				// Use the previous weight since the new RS is not ready for a new weight
				for i := *index - 1; i >= 0; i-- {
					step := c.rollout.Spec.Strategy.Canary.Steps[i]
					if step.SetWeight != nil {
						desiredWeight = *step.SetWeight
						break
					}
				}
				weightDestinations = append(weightDestinations, c.calculateWeightDestinationsFromExperiment()...)
			} else if *index != int32(len(c.rollout.Spec.Strategy.Canary.Steps)) {
				// If the rollout is progressing through the steps, the desired
				// weight of the traffic routing service should be at the value of the
				// last setWeight step, which is set by GetCurrentSetWeight.
				desiredWeight = replicasetutil.GetCurrentSetWeight(c.rollout)
				weightDestinations = append(weightDestinations, c.calculateWeightDestinationsFromExperiment()...)
			} else {
				desiredWeight = 100
			}
		}

		err = reconciler.UpdateHash(canaryHash, stableHash, weightDestinations...)
		if err != nil {
			return err
		}

		err = reconciler.SetWeight(desiredWeight, weightDestinations...)
		if err != nil {
			c.recorder.Warnf(c.rollout, record.EventOptions{EventReason: "TrafficRoutingError"}, err.Error())
			return err
		}
		if modified, newWeights := calculateWeightStatus(c.rollout, canaryHash, stableHash, desiredWeight, weightDestinations...); modified {
			c.log.Infof("Previous weights: %v", c.rollout.Status.Canary.Weights)
			c.log.Infof("New weights: %v", newWeights)
			c.recorder.Eventf(c.rollout, record.EventOptions{EventReason: conditions.TrafficWeightUpdatedReason}, trafficWeightUpdatedMessage(c.rollout.Status.Canary.Weights, newWeights))
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
	}
	return nil
}

// trafficWeightUpdatedMessage returns a message we emit for the kubernetes event whenever we adjust traffic weights
func trafficWeightUpdatedMessage(prev, new *v1alpha1.TrafficWeights) string {
	var details []string
	if prev == nil {
		details = append(details, fmt.Sprintf("to %d", new.Canary.Weight))
	} else if prev.Canary.Weight != new.Canary.Weight {
		details = append(details, fmt.Sprintf("from %d to %d", prev.Canary.Weight, new.Canary.Weight))
	}
	if prev != nil && new != nil && !reflect.DeepEqual(prev.Additional, new.Additional) {
		details = append(details, fmt.Sprintf("additional: %v", new.Additional))
	}
	return fmt.Sprintf(conditions.TrafficWeightUpdatedMessage, strings.Join(details, ", "))
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

// calculateWeightDestinationsFromExperiment checks for experiment step
// If current experiment exists, then create WeightDestinations for each experiment template
func (c *rolloutContext) calculateWeightDestinationsFromExperiment() []v1alpha1.WeightDestination {
	weightDestinations := make([]v1alpha1.WeightDestination, 0)
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
	return weightDestinations
}
