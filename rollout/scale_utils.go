package rollout

import (
	"context"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

const legacyDeploymentTrafficTarget = "__legacy_deployment__"

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

// scaleDownPromotedLegacyDeploymentForNewRevision retires the source
// Deployment only after first-adoption traffic has been explicitly promoted
// and a genuinely new revision is ready to start. This preserves every legacy
// Pod during adoption while avoiding legacy + stable + canary triple capacity
// on subsequent releases.
func (c *rolloutContext) scaleDownPromotedLegacyDeploymentForNewRevision() (bool, error) {
	if c.rollout.Spec.WorkloadRef == nil || c.rollout.Spec.WorkloadRef.ScaleDown != v1alpha1.ScaleDownNever {
		return false, nil
	}
	if c.rollout.Annotations == nil || strings.TrimSpace(c.rollout.Annotations[annotations.LegacyAdoptionPromotedAnnotation]) != "true" {
		return false, nil
	}
	if strings.TrimSpace(c.rollout.Annotations[annotations.TrafficOverrideHashAnnotation]) == legacyDeploymentTrafficTarget {
		return false, nil
	}
	if c.newRS == nil || c.stableRS == nil {
		return false, nil
	}
	newHash := replicasetutil.GetPodTemplateHash(c.newRS)
	stableHash := replicasetutil.GetPodTemplateHash(c.stableRS)
	if newHash == "" || stableHash == "" || newHash == stableHash {
		return false, nil
	}

	deployment, err := c.kubeclientset.AppsV1().Deployments(c.rollout.Namespace).Get(context.TODO(), c.rollout.Spec.WorkloadRef.Name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas == 0 {
		return false, nil
	}

	targetScale := int32(0)
	if err := c.scaleDeployment(&targetScale); err != nil {
		return false, err
	}
	return true, nil
}
