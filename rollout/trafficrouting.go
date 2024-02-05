package rollout

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/argoproj/argo-rollouts/utils/annotations"

	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/alb"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/ambassador"
	a6 "github.com/argoproj/argo-rollouts/rollout/trafficrouting/apisix"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/appmesh"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/nginx"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/smi"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/traefik"
	a6util "github.com/argoproj/argo-rollouts/utils/apisix"
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
		}))
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting.ALB != nil {
		alb_reconcilier, err := alb.NewReconciler(alb.ReconcilerConfig{
			Rollout:        rollout,
			Client:         c.kubeclientset,
			Recorder:       c.recorder,
			ControllerKind: controllerKind,
			IngressWrapper: c.ingressWrapper,
			Status:         &roCtx.newStatus,
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
	if rollout.Spec.Strategy.Canary.TrafficRouting.Traefik != nil {
		dynamicClient := traefik.NewDynamicClient(c.dynamicclientset, rollout.GetNamespace())
		trafficReconcilers = append(trafficReconcilers, traefik.NewReconciler(&traefik.ReconcilerConfig{
			Rollout:  rollout,
			Client:   dynamicClient,
			Recorder: c.recorder,
		}))
	}

	if rollout.Spec.Strategy.Canary.TrafficRouting.Apisix != nil {
		dynamicClient := a6util.NewDynamicClient(c.dynamicclientset, rollout.GetNamespace())
		trafficReconcilers = append(trafficReconcilers, a6.NewReconciler(&a6.ReconcilerConfig{
			Rollout:  rollout,
			Client:   dynamicClient,
			Recorder: c.recorder,
		}))
	}

	if rollout.Spec.Strategy.Canary.TrafficRouting.Plugins != nil {
		for pluginName := range rollout.Spec.Strategy.Canary.TrafficRouting.Plugins {
			pluginReconciler, err := plugin.NewReconciler(&plugin.ReconcilerConfig{
				Rollout:    rollout,
				Client:     c.kubeclientset,
				Recorder:   c.recorder,
				PluginName: pluginName,
			})
			if err != nil {
				return trafficReconcilers, err
			}
			trafficReconcilers = append(trafficReconcilers, pluginReconciler)
		}
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

		if dynamicallyRollingBackToStable, prevDesiredHash := isDynamicallyRollingBackToStable(c.rollout, c.newRS); dynamicallyRollingBackToStable {
			desiredWeight = c.calculateDesiredWeightOnAbortOrStableRollback()
			// Since stableRS == desiredRS, we must balance traffic between the
			// *previous desired* vs. stable (as opposed to current desired vs. stable).
			// The previous desired is remembered in Status.Canary.Weights.Canary.PodTemplateHash.
			// See: https://github.com/argoproj/argo-rollouts/issues/3020
			canaryHash = prevDesiredHash
		} else if rolloututil.IsFullyPromoted(c.rollout) {
			err := reconciler.RemoveManagedRoutes()
			if err != nil {
				return err
			}
		} else if c.pauseContext.IsAborted() {
			desiredWeight = c.calculateDesiredWeightOnAbortOrStableRollback()
			if (c.rollout.Spec.Strategy.Canary.DynamicStableScale && desiredWeight == 0) || !c.rollout.Spec.Strategy.Canary.DynamicStableScale {
				// If we are using dynamic stable scale we need to also make sure that desiredWeight=0 aka we are completely
				// done with aborting before resetting the canary service selectors back to stable. For non-dynamic scale we do not check for availability because we are
				// fully aborted and stable pods will be there, if we check for availability it causes issues with ALB readiness gates if all stable pods
				// have the desired readiness gate on them during an abort we get stuck in a loop because all the stable go unready and rollouts won't be able
				// to switch the desired services because there is no ready pods which causes pods to get stuck progressing forever waiting for readiness.
				err = c.ensureSVCTargets(c.rollout.Spec.Strategy.Canary.CanaryService, c.stableRS, false)
				if err != nil {
					return err
				}
			}
			err := reconciler.RemoveManagedRoutes()
			if err != nil {
				return err
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

			err := reconciler.RemoveManagedRoutes()
			if err != nil {
				return err
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
		// We need to check for revision > 1 because when we first install the rollout we run step 0 this prevents that.
		// There is a bigger fix needed for the reasons on why we run step 0 on rollout install, that needs to be explored.
		revision, revisionFound := annotations.GetRevisionAnnotation(c.rollout)
		if currentStep != nil && (revisionFound && revision > 1) {
			if currentStep.SetHeaderRoute != nil {
				if err = reconciler.SetHeaderRoute(currentStep.SetHeaderRoute); err != nil {
					return err
				}
			}
			if currentStep.SetMirrorRoute != nil {
				if err = reconciler.SetMirrorRoute(currentStep.SetMirrorRoute); err != nil {
					return err
				}
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

		weightVerified, err := reconciler.VerifyWeight(desiredWeight, weightDestinations...)
		c.newStatus.Canary.Weights.Verified = weightVerified
		if err != nil {
			c.recorder.Warnf(c.rollout, record.EventOptions{EventReason: conditions.WeightVerifyErrorReason}, conditions.WeightVerifyErrorMessage, err)
			return nil // return nil instead of error since we want to continue with normal reconciliation
		}

		var indexString string
		if index != nil {
			indexString = strconv.FormatInt(int64(*index), 10)
		} else {
			indexString = "n/a"
		}

		if weightVerified != nil {
			if *weightVerified {
				c.log.Infof("Desired weight (stepIdx: %s) %d verified", indexString, desiredWeight)
			} else {
				c.log.Infof("Desired weight (stepIdx: %s) %d not yet verified", indexString, desiredWeight)
				c.enqueueRolloutAfter(c.rollout, defaults.GetRolloutVerifyRetryInterval())
			}
		}
	}
	return nil
}

// calculateDesiredWeightOnAbortOrStableRollback returns the desired weight to use when we are either
// aborting, or rolling back to stable RS.
func (c *rolloutContext) calculateDesiredWeightOnAbortOrStableRollback() int32 {
	if !c.rollout.Spec.Strategy.Canary.DynamicStableScale {
		// When aborting or rolling back to stable RS and dynamicStableScaling is disabled,
		// then desired canary weight should immediately be 0 (100% to stable) since we can trust
		// that it is fully scaled up
		return 0
	}
	// When using dynamic stable scaling, we must dynamically decreasing the weight to the canary
	// according to the availability of the stable (whatever it can support).
	desiredWeight := 100 - ((100 * c.stableRS.Status.AvailableReplicas) / *c.rollout.Spec.Replicas)
	if c.rollout.Status.Canary.Weights != nil {
		// This ensures that if we are already at a lower weight, then we will not
		// increase the weight because stable availability is flapping (e.g. pod restarts)
		desiredWeight = minInt(desiredWeight, c.rollout.Status.Canary.Weights.Canary.Weight)
	}
	return desiredWeight
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
