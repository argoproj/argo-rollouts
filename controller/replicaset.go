package controller

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

var controllerKind = v1alpha1.SchemeGroupVersion.WithKind("Rollouts")

func (c *Controller) getReplicaSetsForRollouts(r *v1alpha1.Rollout) ([]*appsv1.ReplicaSet, error) {
	// List all ReplicaSets to find those we own but that no longer match our
	// selector. They will be orphaned by ClaimReplicaSets().
	rsList, err := c.replicaSetLister.ReplicaSets(r.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	replicaSetSelector, err := metav1.LabelSelectorAsSelector(r.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("rollout %s/%s has invalid label selector: %v", r.Namespace, r.Name, err)
	}
	// If any adoptions are attempted, we should first recheck for deletion with
	// an uncached quorum read sometime after listing ReplicaSets (see #42639).
	canAdoptFunc := controller.RecheckDeletionTimestamp(func() (metav1.Object, error) {
		fresh, err := c.rolloutsclientset.ArgoprojV1alpha1().Rollouts(r.Namespace).Get(r.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if fresh.UID != r.UID {
			return nil, fmt.Errorf("original Rollout %v/%v is gone: got uid %v, wanted %v", r.Namespace, r.Name, fresh.UID, r.UID)
		}
		return fresh, nil
	})
	cm := controller.NewReplicaSetControllerRefManager(c.replicaSetControl, r, replicaSetSelector, controllerKind, canAdoptFunc)
	return cm.ClaimReplicaSets(rsList)
}
