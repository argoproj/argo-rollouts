package rollout

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *rolloutContext) scaleDeployment(targetScale *int32) error {
	deploymentName := c.rollout.Spec.WorkloadRef.Name
	namespace := c.rollout.Namespace
	deployment, err := c.kubeclientset.AppsV1().Deployments(namespace).Get(context.TODO(), deploymentName, metav1.GetOptions{})
	if err != nil {
		c.log.Warnf("Failed to fetch deployment %s: %s", deploymentName, err.Error())
		return err
	}

	var newReplicasCount int32
	if *targetScale < 0 {
		newReplicasCount = 0
	} else {
		newReplicasCount = *targetScale
	}
	if newReplicasCount == *deployment.Spec.Replicas {
		return nil
	}
	c.log.Infof("Scaling deployment %s to %d replicas", deploymentName, newReplicasCount)
	*deployment.Spec.Replicas = newReplicasCount

	_, err = c.kubeclientset.AppsV1().Deployments(namespace).Update(context.TODO(), deployment, metav1.UpdateOptions{})
	if err != nil {
		c.log.Warnf("Failed to update deployment %s: %s", deploymentName, err.Error())
		return err
	}
	return nil
}
