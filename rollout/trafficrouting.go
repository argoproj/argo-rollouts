package rollout

import (
	"time"

	"github.com/argoproj/argo-rollouts/rollout/trafficrouting"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/alb"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/ambassador"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/nginx"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/smi"

	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
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
	weightDestinations := make([]trafficrouting.WeightDestination, 0)
	if c.rollout.Status.StableRS == c.rollout.Status.CurrentPodHash {
		// when we are fully promoted. desired canary weight should be 0
	} else if c.pauseContext.IsAborted() {
		// when promote aborted. desired canary weight should be 0
	} else if c.newRS == nil || c.newRS.Status.AvailableReplicas == 0 {
		// when newRS is not available or replicas num is 0. never weight to canary
	} else if index != nil {
		atDesiredReplicaCount := replicasetutil.AtDesiredReplicaCountsForCanary(c.rollout, c.newRS, c.stableRS, c.otherRSs)
		if !atDesiredReplicaCount {
			// Use the previous weight since the new RS is not ready for a new weight
			for i := *index - 1; i >= 0; i-- {
				step := c.rollout.Spec.Strategy.Canary.Steps[i]
				if step.SetWeight != nil {
					desiredWeight = *step.SetWeight
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

		exStep := replicasetutil.GetCurrentExperimentStep(c.rollout)
		if exStep != nil {
			getTemplateWeight := func(name string) *int32 {
				for _, tmpl := range exStep.Templates {
					if tmpl.Name == name {
						return tmpl.Weight
					}
				}
				return nil
			}
			// TODO: Check if Experiment is running
			for _, templateStatus := range c.currentEx.Status.TemplateStatuses {
				templateWeight := getTemplateWeight(templateStatus.Name)
				weightDestinations = append(weightDestinations, trafficrouting.WeightDestination{
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

	// If we are at a setWeight step, also perform weight verification. Note that we don't do this
	// every reconciliation because weight verification typically involves API calls to the cloud
	// provider which could incur rate limiting
	if currentStep != nil && currentStep.SetWeight != nil {
		weightVerified, err := reconciler.VerifyWeight(desiredWeight)
		if err != nil {
			return err
		}
		if !weightVerified {
			c.log.Infof("Desired weight (stepIdx: %d) %d not yet verified", *index, desiredWeight)
			c.enqueueRolloutAfter(c.rollout, 10*time.Second)
		} else {
			c.log.Infof("Desired weight (stepIdx: %d) %d verified", *index, desiredWeight)
		}
		c.weightVerified = &weightVerified
	}

	return nil
}
