package rollout

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	patchtypes "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const (
	switchSelectorPatch = `{
	"spec": {
		"selector": {
			"%s": "%s"
		}
	}
}`
)

// switchSelector switch the selector on an existing service to a new value
func (c RolloutController) switchServiceSelector(service *corev1.Service, newRolloutUniqueLabelValue string, r *v1alpha1.Rollout) error {
	patch := fmt.Sprintf(switchSelectorPatch, v1alpha1.DefaultRolloutUniqueLabelKey, newRolloutUniqueLabelValue)
	msg := fmt.Sprintf("Switching selector for service '%s' to value '%s'", service.Name, newRolloutUniqueLabelValue)
	logutil.WithRollout(r).Info(msg)
	c.recorder.Event(r, corev1.EventTypeNormal, "SwitchService", msg)
	_, err := c.kubeclientset.CoreV1().Services(service.Namespace).Patch(service.Name, patchtypes.StrategicMergePatchType, []byte(patch))
	if service.Spec.Selector == nil {
		service.Spec.Selector = make(map[string]string)
	}
	service.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] = newRolloutUniqueLabelValue
	return err
}

func (c *RolloutController) reconcilePreviewService(roCtx *blueGreenContext, previewSvc *corev1.Service) (bool, error) {
	r := roCtx.Rollout()
	logCtx := roCtx.Log()
	newRS := roCtx.NewRS()
	if previewSvc == nil {
		return false, nil
	}
	logCtx.Infof("Reconciling preview service '%s'", previewSvc.Name)

	newPodHash := newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	// If preview service already points to the new RS, skip the next steps
	if previewSvc.Spec.Selector != nil {
		currentSelectorValue, ok := previewSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		if ok && currentSelectorValue == newPodHash {
			return false, nil
		}
	}

	err := c.switchServiceSelector(previewSvc, newPodHash, r)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (c *RolloutController) reconcileActiveService(roCtx *blueGreenContext, activeSvc *corev1.Service) (bool, error) {
	r := roCtx.Rollout()
	newRS := roCtx.NewRS()
	newPodHash := newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	if activeSvc.Spec.Selector != nil {
		currentSelectorValue, ok := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		if ok && currentSelectorValue == newPodHash {
			return false, nil
		}
	}

	err := c.switchServiceSelector(activeSvc, newPodHash, r)
	if err != nil {
		return false, err
	}
	return true, nil
}

// getReferencedService returns service references in rollout spec and sets warning condition if service does not exist
func (c *RolloutController) getReferencedService(r *v1alpha1.Rollout, serviceName string) (*corev1.Service, error) {
	svc, err := c.servicesLister.Services(r.Namespace).Get(serviceName)
	if err != nil {
		if errors.IsNotFound(err) {
			msg := fmt.Sprintf(conditions.ServiceNotFoundMessage, serviceName)
			c.recorder.Event(r, corev1.EventTypeWarning, conditions.ServiceNotFoundReason, msg)
			newStatus := r.Status.DeepCopy()
			cond := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionFalse, conditions.ServiceNotFoundReason, msg)
			c.patchCondition(r, newStatus, cond)
		}
		return nil, err
	}
	return svc, nil
}

func (c *RolloutController) getPreviewAndActiveServices(r *v1alpha1.Rollout) (*corev1.Service, *corev1.Service, error) {
	var previewSvc *corev1.Service
	var activeSvc *corev1.Service
	var err error

	if r.Spec.Strategy.BlueGreen.PreviewService != "" {
		previewSvc, err = c.getReferencedService(r, r.Spec.Strategy.BlueGreen.PreviewService)
		if err != nil {
			return nil, nil, err
		}
	}
	if r.Spec.Strategy.BlueGreen.ActiveService == "" {
		return nil, nil, fmt.Errorf("Invalid Spec: Rollout missing field ActiveService")
	}
	activeSvc, err = c.getReferencedService(r, r.Spec.Strategy.BlueGreen.ActiveService)
	if err != nil {
		return nil, nil, err
	}
	return previewSvc, activeSvc, nil
}

func (c *RolloutController) reconcileStableAndCanaryService(roCtx *canaryContext) error {
	r := roCtx.Rollout()
	newRS := roCtx.NewRS()
	stableRS := roCtx.StableRS()
	if r.Spec.Strategy.Canary == nil {
		return nil
	}
	if r.Spec.Strategy.Canary.StableService != "" && stableRS != nil {
		svc, err := c.getReferencedService(r, r.Spec.Strategy.Canary.StableService)
		if err != nil {
			return err
		}

		err = c.switchServiceSelector(svc, stableRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], r)
		if err != nil {
			return err
		}
	}
	if r.Spec.Strategy.Canary.CanaryService != "" && newRS != nil {
		svc, err := c.getReferencedService(r, r.Spec.Strategy.Canary.CanaryService)
		if err != nil {
			return err
		}

		err = c.switchServiceSelector(svc, newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], r)
		if err != nil {
			return err
		}
	}
	return nil
}
