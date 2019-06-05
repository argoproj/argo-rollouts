package controller

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	log "github.com/sirupsen/logrus"
)

const (
	serviceIndexName    = "byService"
	switchSelectorPatch = `{
	"spec": {
		"selector": {
			"%s": "%s"
		}
	}
}`
	removeSelectorPatch = `[{ "op": "remove", "path": "/spec/selector/%s" }]`
)

// switchSelector switch the selector on an existing service to a new value
func (c Controller) switchServiceSelector(service *corev1.Service, newRolloutUniqueLabelValue string, r *v1alpha1.Rollout) error {
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

func (c *Controller) reconcilePreviewService(r *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, previewSvc *corev1.Service, activeSvc *corev1.Service) (bool, error) {
	logCtx := logutil.WithRollout(r)
	if previewSvc == nil {
		return false, nil
	}
	logCtx.Infof("Reconciling preview service '%s'", previewSvc.Name)

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
		previewSvc, err = c.servicesLister.Services(r.Namespace).Get(r.Spec.Strategy.BlueGreenStrategy.PreviewService)
		if err != nil {
			if errors.IsNotFound(err) {
				msg := fmt.Sprintf(conditions.ServiceNotFoundMessage, r.Spec.Strategy.BlueGreenStrategy.PreviewService)
				c.recorder.Event(r, corev1.EventTypeWarning, conditions.ServiceNotFoundReason, msg)
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
	activeSvc, err = c.servicesLister.Services(r.Namespace).Get(r.Spec.Strategy.BlueGreenStrategy.ActiveService)
	if err != nil {
		if errors.IsNotFound(err) {
			msg := fmt.Sprintf(conditions.ServiceNotFoundMessage, r.Spec.Strategy.BlueGreenStrategy.ActiveService)
			c.recorder.Event(r, corev1.EventTypeWarning, conditions.ServiceNotFoundReason, msg)
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

func (c *Controller) enqueueService(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		runtime.HandleError(err)
		return
	}
	c.serviceWorkqueue.AddRateLimited(key)
}

func (c *Controller) syncService(key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	svc, err := c.servicesLister.Services(namespace).Get(name)
	if errors.IsNotFound(err) {
		log.WithField(logutil.ServiceKey, key).Infof("Service %v has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	if rollouts, err := c.getRolloutsByService(svc.Namespace, svc.Name); err == nil {
		for i := range rollouts {
			c.enqueueRollout(rollouts[i])
		}

		if _, hasHashSelector := svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]; hasHashSelector && len(rollouts) == 0 {
			updatedSvc := svc.DeepCopy()
			delete(updatedSvc.Spec.Selector, v1alpha1.DefaultRolloutUniqueLabelKey)
			patch := fmt.Sprintf(removeSelectorPatch, v1alpha1.DefaultRolloutUniqueLabelKey)
			_, err := c.kubeclientset.CoreV1().Services(updatedSvc.Namespace).Patch(updatedSvc.Name, patchtypes.JSONPatchType, []byte(patch))
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}
	}
	return nil
}

// getRolloutsByService returns all rollouts which are referencing specified service
func (c *Controller) getRolloutsByService(namespace string, serviceName string) ([]*v1alpha1.Rollout, error) {
	objs, err := c.rolloutsIndexer.ByIndex(serviceIndexName, fmt.Sprintf("%s/%s", namespace, serviceName))
	if err != nil {
		return nil, err
	}
	var rollouts []*v1alpha1.Rollout
	for i := range objs {
		if r, ok := objs[i].(*v1alpha1.Rollout); ok {
			rollouts = append(rollouts, r)
		}
	}
	return rollouts, nil
}

// getRolloutServiceKeys returns services keys (namespace/serviceName) which are referenced by specified rollout
func getRolloutServiceKeys(rollout *v1alpha1.Rollout) []string {
	servicesSet := make(map[string]bool)
	if rollout.Spec.Strategy.BlueGreenStrategy != nil {
		if rollout.Spec.Strategy.BlueGreenStrategy.ActiveService != "" {
			servicesSet[fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.BlueGreenStrategy.ActiveService)] = true
		}
		if rollout.Spec.Strategy.BlueGreenStrategy.PreviewService != "" {
			servicesSet[fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Spec.Strategy.BlueGreenStrategy.PreviewService)] = true
		}
	}
	var services []string
	for svc := range servicesSet {
		services = append(services, svc)
	}
	return services
}
