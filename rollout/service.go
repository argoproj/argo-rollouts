package rollout

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	serviceutil "github.com/argoproj/argo-rollouts/utils/service"
)

const (
	switchSelectorPatch = `{
	"spec": {
		"selector": {
			"` + v1alpha1.DefaultRolloutUniqueLabelKey + `": "%s"
		}
	}
}`
	switchSelectorAndAddManagedByPatch = `{
	"metadata": {
		"annotations": {
			"` + v1alpha1.ManagedByRolloutsKey + `": "%s"
		}
	},
	"spec": {
		"selector": {
			"` + v1alpha1.DefaultRolloutUniqueLabelKey + `": "%s"
		}
	}
}`
)

func generatePatch(service *corev1.Service, newRolloutUniqueLabelValue string, r *v1alpha1.Rollout) string {
	if _, ok := service.Annotations[v1alpha1.ManagedByRolloutsKey]; !ok {
		return fmt.Sprintf(switchSelectorAndAddManagedByPatch, r.Name, newRolloutUniqueLabelValue)
	}
	return fmt.Sprintf(switchSelectorPatch, newRolloutUniqueLabelValue)
}

// switchSelector switch the selector on an existing service to a new value
func (c rolloutContext) switchServiceSelector(service *corev1.Service, newRolloutUniqueLabelValue string, r *v1alpha1.Rollout) error {
	if service.Spec.Selector == nil {
		service.Spec.Selector = make(map[string]string)
	}
	_, hasManagedRollout := serviceutil.HasManagedByAnnotation(service)
	oldPodHash, ok := service.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
	if ok && oldPodHash == newRolloutUniqueLabelValue && hasManagedRollout {
		return nil
	}
	patch := generatePatch(service, newRolloutUniqueLabelValue, r)
	_, err := c.kubeclientset.CoreV1().Services(service.Namespace).Patch(service.Name, patchtypes.StrategicMergePatchType, []byte(patch))
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("Switched selector for service '%s' to value '%s'", service.Name, newRolloutUniqueLabelValue)
	c.log.Info(msg)
	c.recorder.Event(r, corev1.EventTypeNormal, "SwitchService", msg)
	service.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] = newRolloutUniqueLabelValue
	return err
}

func (c *rolloutContext) reconcilePreviewService(previewSvc *corev1.Service) error {
	if previewSvc == nil {
		return nil
	}
	c.log.Infof("Reconciling preview service '%s'", previewSvc.Name)

	newPodHash := c.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	err := c.switchServiceSelector(previewSvc, newPodHash, c.rollout)
	if err != nil {
		return err
	}

	return nil
}

func (c *rolloutContext) reconcileActiveService(previewSvc, activeSvc *corev1.Service) error {
	if !replicasetutil.ReadyForPause(c.rollout, c.newRS, c.allRSs) || !annotations.IsSaturated(c.rollout, c.newRS) {
		c.log.Infof("New RS '%s' is not fully saturated", c.newRS.Name)
		return nil
	}

	newPodHash := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
	if c.skipPause(activeSvc) {
		newPodHash = c.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	}
	if c.pauseContext.CompletedBlueGreenPause() && c.completedPrePromotionAnalysis() {
		newPodHash = c.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	}

	if c.rollout.Status.Abort {
		newPodHash = c.rollout.Status.StableRS
	}

	err := c.switchServiceSelector(activeSvc, newPodHash, c.rollout)
	if err != nil {
		return err
	}
	return nil
}

func (c *rolloutContext) getPreviewAndActiveServices() (*corev1.Service, *corev1.Service, error) {
	var previewSvc *corev1.Service
	var activeSvc *corev1.Service
	var err error

	if c.rollout.Spec.Strategy.BlueGreen.PreviewService != "" {
		previewSvc, err = c.servicesLister.Services(c.rollout.Namespace).Get(c.rollout.Spec.Strategy.BlueGreen.PreviewService)
		if err != nil {
			return nil, nil, err
		}
	}
	activeSvc, err = c.servicesLister.Services(c.rollout.Namespace).Get(c.rollout.Spec.Strategy.BlueGreen.ActiveService)
	if err != nil {
		return nil, nil, err
	}
	return previewSvc, activeSvc, nil
}

func (c *rolloutContext) reconcileStableAndCanaryService() error {
	if c.rollout.Spec.Strategy.Canary == nil {
		return nil
	}
	if c.rollout.Spec.Strategy.Canary.StableService != "" && c.stableRS != nil {
		svc, err := c.servicesLister.Services(c.rollout.Namespace).Get(c.rollout.Spec.Strategy.Canary.StableService)
		if err != nil {
			return err
		}
		if svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] != c.stableRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
			err = c.switchServiceSelector(svc, c.stableRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], c.rollout)
			if err != nil {
				return err
			}
		}

	}
	if c.rollout.Spec.Strategy.Canary.CanaryService != "" && c.newRS != nil {
		svc, err := c.servicesLister.Services(c.rollout.Namespace).Get(c.rollout.Spec.Strategy.Canary.CanaryService)
		if err != nil {
			return err
		}
		if svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] != c.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
			err = c.switchServiceSelector(svc, c.newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], c.rollout)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
