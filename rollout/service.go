package rollout

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
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
func (c Controller) switchServiceSelector(service *corev1.Service, newRolloutUniqueLabelValue string, r *v1alpha1.Rollout) error {
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
	logutil.WithRollout(r).Info(msg)
	c.recorder.Event(r, corev1.EventTypeNormal, "SwitchService", msg)
	service.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] = newRolloutUniqueLabelValue
	return err
}

func (c *Controller) reconcilePreviewService(roCtx *blueGreenContext, previewSvc *corev1.Service) error {
	r := roCtx.Rollout()
	logCtx := roCtx.Log()
	newRS := roCtx.NewRS()
	if previewSvc == nil {
		return nil
	}
	logCtx.Infof("Reconciling preview service '%s'", previewSvc.Name)

	newPodHash := newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	err := c.switchServiceSelector(previewSvc, newPodHash, r)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) reconcileActiveService(roCtx *blueGreenContext, previewSvc, activeSvc *corev1.Service) error {
	r := roCtx.Rollout()
	newRS := roCtx.NewRS()
	allRSs := roCtx.AllRSs()

	if !replicasetutil.ReadyForPause(r, newRS, allRSs) || !annotations.IsSaturated(r, newRS) {
		roCtx.log.Infof("New RS '%s' is not fully saturated", newRS.Name)
		return nil
	}

	newPodHash := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
	if skipPause(roCtx, activeSvc) {
		newPodHash = newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	}
	if roCtx.PauseContext().CompletedBlueGreenPause() && completedPrePromotionAnalysis(roCtx) {
		newPodHash = newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	}

	if r.Status.Abort {
		newPodHash = r.Status.StableRS
	}

	err := c.switchServiceSelector(activeSvc, newPodHash, r)
	if err != nil {
		return err
	}
	return nil
}

func (c *Controller) getPreviewAndActiveServices(r *v1alpha1.Rollout) (*corev1.Service, *corev1.Service, error) {
	var previewSvc *corev1.Service
	var activeSvc *corev1.Service
	var err error

	if r.Spec.Strategy.BlueGreen.PreviewService != "" {
		previewSvc, err = c.servicesLister.Services(r.Namespace).Get(r.Spec.Strategy.BlueGreen.PreviewService)
		if err != nil {
			return nil, nil, err
		}
	}
	activeSvc, err = c.servicesLister.Services(r.Namespace).Get(r.Spec.Strategy.BlueGreen.ActiveService)
	if err != nil {
		return nil, nil, err
	}
	return previewSvc, activeSvc, nil
}

func (c *Controller) reconcileStableAndCanaryService(roCtx *canaryContext) error {
	r := roCtx.Rollout()
	newRS := roCtx.NewRS()
	stableRS := roCtx.StableRS()
	if r.Spec.Strategy.Canary == nil {
		return nil
	}
	if r.Spec.Strategy.Canary.StableService != "" && stableRS != nil {
		svc, err := c.servicesLister.Services(r.Namespace).Get(r.Spec.Strategy.Canary.StableService)
		if err != nil {
			return err
		}
		if svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] != stableRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
			err = c.switchServiceSelector(svc, stableRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], r)
			if err != nil {
				return err
			}
		}

	}
	if r.Spec.Strategy.Canary.CanaryService != "" && newRS != nil {
		svc, err := c.servicesLister.Services(r.Namespace).Get(r.Spec.Strategy.Canary.CanaryService)
		if err != nil {
			return err
		}
		if svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] != newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
			err = c.switchServiceSelector(svc, newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], r)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
