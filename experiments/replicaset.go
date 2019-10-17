package experiments

import (
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

const (
	CollisionCountPatch = `{
	"status" : {
		"templateStatuses" : %s
	}
}`
)

var controllerKind = v1alpha1.SchemeGroupVersion.WithKind("Experiment")

func (c *ExperimentController) getReplicaSetsForExperiment(experiment *v1alpha1.Experiment) (map[string]*appsv1.ReplicaSet, error) {
	// List all ReplicaSets to find those we own but that no longer match our
	// selector. They will be orphaned by ClaimReplicaSets().
	rsList, err := c.replicaSetLister.ReplicaSets(experiment.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	// If any adoptions are attempted, we should first recheck for deletion with
	// an uncached quorum read sometime after listing ReplicaSets (see #42639).
	canAdoptFunc := controller.RecheckDeletionTimestamp(func() (metav1.Object, error) {
		fresh, err := c.argoProjClientset.ArgoprojV1alpha1().Experiments(experiment.Namespace).Get(experiment.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if fresh.UID != experiment.UID {
			return nil, fmt.Errorf("original Experiment %v/%v is gone: got uid %v, wanted %v", experiment.Namespace, experiment.Name, fresh.UID, experiment.UID)
		}
		return fresh, nil
	})

	templateToRS := make(map[string]*appsv1.ReplicaSet)
	for _, template := range experiment.Spec.Templates {
		replicaSetSelector, err := metav1.LabelSelectorAsSelector(template.Selector)
		if err != nil {
			return nil, fmt.Errorf("experiment %s/%s has invalid label selector: %v", experiment.Namespace, experiment.Name, err)
		}
		templateRSs := make([]*appsv1.ReplicaSet, 0)
		for i := range rsList {
			rs := rsList[i]
			if replicaSetSelector.Matches(labels.Set(rs.ObjectMeta.Labels)) {
				templateRSs = append(templateRSs, rs)
			}
		}

		cm := controller.NewReplicaSetControllerRefManager(c.replicaSetControl, experiment, replicaSetSelector, controllerKind, canAdoptFunc)
		templateRSs, err = cm.ClaimReplicaSets(templateRSs)
		if err != nil {
			return nil, err
		}
		matches := len(templateRSs)
		if matches > 1 {
			return nil, fmt.Errorf("multiple ReplicaSets match single experiment template")
		} else if matches == 1 {
			templateToRS[template.Name] = templateRSs[0]
		}
	}
	return templateToRS, nil
}

func (ec *experimentContext) reconcileReplicaSet(template v1alpha1.TemplateSpec, templateStatus v1alpha1.TemplateStatus) (*appsv1.ReplicaSet, error) {
	newRSTemplate := *template.Template.DeepCopy()

	podTemplateSpecHash := controller.ComputeHash(&newRSTemplate, templateStatus.CollisionCount)
	newRS := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            fmt.Sprintf("%s-%s-%s", ec.ex.Name, template.Name, podTemplateSpecHash),
			Namespace:       ec.ex.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ec.ex, controllerKind)},
			Labels:          newRSTemplate.Labels,
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas:        new(int32),
			MinReadySeconds: template.MinReadySeconds,
			Selector:        template.Selector,
			Template:        newRSTemplate,
		},
	}

	newReplicasCount := experimentutil.CalculateTemplateReplicasCount(ec.ex, template)
	*(newRS.Spec.Replicas) = newReplicasCount

	// Create the new ReplicaSet. If it already exists, then we need to check for possible
	// hash collisions. If there is any other error, we need to report it in the status of
	// the Experiment.
	alreadyExists := false
	createdRS, err := ec.kubeclientset.AppsV1().ReplicaSets(ec.ex.Namespace).Create(&newRS)
	switch {
	// We may end up hitting this due to a slow cache or a fast resync of the Experiment.
	case errors.IsAlreadyExists(err):
		alreadyExists = true

		// Fetch a copy of the ReplicaSet.
		rs, rsErr := ec.replicaSetLister.ReplicaSets(newRS.Namespace).Get(newRS.Name)
		if rsErr != nil {
			return nil, rsErr
		}

		// If the Experiment owns the ReplicaSet and the ReplicaSet's PodTemplateSpec is semantically
		// deep equal to the PodTemplateSpec of the Experiment, it's the Experiment's new ReplicaSet.
		// Otherwise, this is a hash collision and we need to increment the collisionCount field in
		// the status of the Experiment and requeue to try the creation in the next sync.
		controllerRef := metav1.GetControllerOf(rs)
		if controllerRef != nil && controllerRef.UID == ec.ex.UID && replicasetutil.PodTemplateEqualIgnoreHash(&rs.Spec.Template, &template.Template) {
			createdRS = rs
			err = nil
			break
		}

		// Since the replicaset is a collision, the experiment will not have a status for that rs and
		// the controller needs to create a new template status for it
		newTemplate := v1alpha1.TemplateStatus{
			Name:           template.Name,
			CollisionCount: new(int32),
		}

		// Matching ReplicaSet is not equal - increment the collisionCount in the ExperimentStatus
		// and requeue the Experiment.
		preCollisionCount := *newTemplate.CollisionCount
		*newTemplate.CollisionCount++

		statusCpy := ec.ex.Status.DeepCopy()
		statusCpy.TemplateStatuses = append(statusCpy.TemplateStatuses, newTemplate)
		templateStatusBytes, marshelErr := json.Marshal(statusCpy.TemplateStatuses)
		if marshelErr != nil {
			return nil, marshelErr
		}

		patch := fmt.Sprintf(CollisionCountPatch, string(templateStatusBytes))
		_, patchErr := ec.argoProjClientset.ArgoprojV1alpha1().Experiments(ec.ex.Namespace).Patch(ec.ex.Name, patchtypes.MergePatchType, []byte(patch))
		ec.log.WithField("patch", patch).Debug("Applied Patch")
		if patchErr != nil {
			ec.log.Errorf("Error patching service %s", err.Error())
		}
		ec.log.Warnf("Found a hash collision - bumped collisionCount (%d->%d) to resolve it", preCollisionCount, *newTemplate.CollisionCount)
		return nil, err
	case err != nil:
		msg := fmt.Sprintf(conditions.FailedRSCreateMessage, newRS.Name, err)
		ec.recorder.Event(ec.ex, corev1.EventTypeWarning, conditions.FailedRSCreateReason, msg)
		// TODO(jessesuen): this didn't seem like it did anything so commented it out
		// newStatus := ec.ex.Status.DeepCopy()
		// ec.persistExperimentStatus(ec.ex, newStatus)
		return nil, err
	}

	if !alreadyExists && newReplicasCount > int32(0) {
		ec.recorder.Eventf(ec.ex, corev1.EventTypeNormal, "ScalingReplicaSet", "Scaled up replica set %s to %d", createdRS.Name, newReplicasCount)
	}

	return nil, err
}

func (ec *experimentContext) scaleReplicaSetAndRecordEvent(rs *appsv1.ReplicaSet, newScale int32) (bool, *appsv1.ReplicaSet, error) {
	// No need to scale
	if *(rs.Spec.Replicas) == newScale {
		return false, rs, nil
	}
	var scalingOperation string
	if *(rs.Spec.Replicas) < newScale {
		scalingOperation = "up"
	} else {
		scalingOperation = "down"
	}
	scaled, newRS, err := ec.scaleReplicaSet(rs, newScale, scalingOperation)
	return scaled, newRS, err
}

func (ec *experimentContext) scaleReplicaSet(rs *appsv1.ReplicaSet, newScale int32, scalingOperation string) (bool, *appsv1.ReplicaSet, error) {
	sizeNeedsUpdate := *(rs.Spec.Replicas) != newScale
	scaled := false
	var err error
	if sizeNeedsUpdate {
		rsCopy := rs.DeepCopy()
		*(rsCopy.Spec.Replicas) = newScale
		rs, err = ec.kubeclientset.AppsV1().ReplicaSets(rsCopy.Namespace).Update(rsCopy)
		if err == nil && sizeNeedsUpdate {
			scaled = true
			ec.recorder.Eventf(ec.ex, corev1.EventTypeNormal, "ScalingReplicaSet", "Scaled %s replica set %s to %d", scalingOperation, rs.Name, newScale)
		}
	}
	return scaled, rs, err
}
