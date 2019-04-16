package controller

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
func (c Controller) switchServiceSelector(service *corev1.Service, newRolloutUniqueLabelValue string, r *v1alpha1.Rollout) error {
	patch := fmt.Sprintf(switchSelectorPatch, v1alpha1.DefaultRolloutUniqueLabelKey, newRolloutUniqueLabelValue)
	logutil.WithRollout(r).Infof("Switching selector for service '%s' to value '%s'", service.Name, newRolloutUniqueLabelValue)
	_, err := c.kubeclientset.CoreV1().Services(service.Namespace).Patch(service.Name, patchtypes.StrategicMergePatchType, []byte(patch))
	if service.Spec.Selector == nil {
		service.Spec.Selector = make(map[string]string)
	}
	service.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] = newRolloutUniqueLabelValue
	return err
}

func (c *Controller) reconcilePreviewService(r *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, previewSvc *corev1.Service, activeSvc *corev1.Service) (bool, error) {
	if previewSvc == nil {
		return false, nil
	}

	//If the active service selector does not point to any RS,
	// we short-circuit changing the preview service.
	if activeSvc.Spec.Selector == nil {
		return false, nil
	}
	// If the active service selector points at the new RS, the
	// preview service should point at nothing
	curActiveSelector, ok := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
	if !ok || curActiveSelector == newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
		curPreviewSelector, ok := previewSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		if !ok || curPreviewSelector != "" {
			err := c.switchServiceSelector(previewSvc, "", r)
			if err != nil {
				return false, err
			}
		}
		return false, nil
	}

	// If preview service already points to the new RS, skip the next steps
	if previewSvc.Spec.Selector != nil {
		currentSelectorValue, ok := previewSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		if ok && currentSelectorValue == newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
			return false, nil
		}
	}

	err := c.switchServiceSelector(previewSvc, newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], r)
	if err != nil {
		return false, err
	}

	return true, nil
}

func (c *Controller) reconcileActiveService(r *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, previewSvc *corev1.Service, activeSvc *corev1.Service) (bool, error) {
	switchActiveSvc := true
	if activeSvc.Spec.Selector != nil {
		currentSelectorValue, ok := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		if ok && currentSelectorValue == newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] {
			switchActiveSvc = false
		}
	}
	if switchActiveSvc {
		err := c.switchServiceSelector(activeSvc, newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], r)
		if err != nil {
			return false, err
		}
		return true, nil
	}

	switchPreviewSvc := false
	if previewSvc != nil && previewSvc.Spec.Selector != nil {
		currentSelectorValue, ok := previewSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
		if !ok || currentSelectorValue != "" {
			switchPreviewSvc = true
		}
	}

	if switchPreviewSvc {
		err := c.switchServiceSelector(previewSvc, "", r)
		if err != nil {
			return false, err
		}
		return true, nil
	}

	return false, nil
}

func (c *Controller) getPreviewAndActiveServices(r *v1alpha1.Rollout) (*corev1.Service, *corev1.Service, error) {
	var previewSvc *corev1.Service
	var activeSvc *corev1.Service
	var err error
	if r.Spec.Strategy.BlueGreenStrategy.PreviewService != "" {
		previewSvc, err = c.kubeclientset.CoreV1().Services(r.Namespace).Get(r.Spec.Strategy.BlueGreenStrategy.PreviewService, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				msg := fmt.Sprintf(conditions.ServiceNotFoundMessage, r.Spec.Strategy.BlueGreenStrategy.PreviewService)
				c.recorder.Eventf(r, corev1.EventTypeWarning, conditions.ServiceNotFoundReason, msg)
				newStatus := r.Status.DeepCopy()
				cond := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionFalse, conditions.ServiceNotFoundReason, msg)
				conditions.SetRolloutCondition(newStatus, *cond)
				c.persistRolloutStatus(r, newStatus, &r.Spec.Paused)
			}
			return nil, nil, err
		}
	}
	if r.Spec.Strategy.BlueGreenStrategy.ActiveService == "" {
		return nil, nil, fmt.Errorf("Invalid Spec: Rollout missing field ActiveService")
	}
	activeSvc, err = c.kubeclientset.CoreV1().Services(r.Namespace).Get(r.Spec.Strategy.BlueGreenStrategy.ActiveService, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			msg := fmt.Sprintf(conditions.ServiceNotFoundMessage, r.Spec.Strategy.BlueGreenStrategy.ActiveService)
			c.recorder.Eventf(r, corev1.EventTypeWarning, conditions.ServiceNotFoundReason, msg)
			newStatus := r.Status.DeepCopy()
			cond := conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionFalse, conditions.ServiceNotFoundReason, msg)
			conditions.SetRolloutCondition(newStatus, *cond)
			c.persistRolloutStatus(r, newStatus, &r.Spec.Paused)
		}
		return nil, nil, err
	}
	return previewSvc, activeSvc, nil
}

func (c *Controller) getRolloutSelectorLabel(svc *corev1.Service) (string, bool) {
	if svc == nil {
		return "", false
	}
	if svc.Spec.Selector == nil {
		return "", false
	}
	currentSelectorValue, ok := svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]
	return currentSelectorValue, ok
}

// GetActiveReplicaSet finds the replicaset that is serving traffic from the active service or returns nil
func GetActiveReplicaSet(rollout *v1alpha1.Rollout, allRS []*appsv1.ReplicaSet) *appsv1.ReplicaSet {
	if rollout.Status.BlueGreen.ActiveSelector == "" {
		return nil
	}
	for _, rs := range allRS {
		if rs == nil {
			continue
		}
		if podHash, ok := rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]; ok {
			if podHash == rollout.Status.BlueGreen.ActiveSelector {
				return rs
			}
		}
	}
	return nil
}
