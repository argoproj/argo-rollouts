package controller

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func (c *Controller) getRolloutsForService(service *corev1.Service) ([]*v1alpha1.Rollout, error) {
	allROs, err := c.rolloutsclientset.ArgoprojV1alpha1().Rollouts(service.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	rollouts := []*v1alpha1.Rollout{}
	for _, rollout := range allROs.Items {
		if rollout.Spec.Strategy.BlueGreenStrategy.ActiveService == service.Name {
			copyRO := rollout.DeepCopy()
			rollouts = append(rollouts, copyRO)
		}
	}
	if len(rollouts) > 1 {
		klog.V(4).Infof("user error! more than one rollout is selecting replica set %s/%s with labels: %#v",
			service.Namespace, service.Name, service.Labels, rollouts[0].Namespace, rollouts[0].Name)
	}
	return rollouts, nil
}

func (c *Controller) handleService(obj interface{}) {
	service := obj.(*corev1.Service)
	rollouts, err := c.getRolloutsForService(service)
	if err != nil {
		return
	}
	for i := range rollouts {
		c.enqueueRollout(rollouts[i])
	}

}

func (c *Controller) updateService(old, cur interface{}) {
	curSvc := cur.(*corev1.Service)
	oldSvc := old.(*corev1.Service)
	if curSvc.ResourceVersion == oldSvc.ResourceVersion {
		// Periodic resync will send update events for all known services.
		// Two different versions of the same replica set will always have different RVs.
		return
	}
	c.handleService(cur)
}
