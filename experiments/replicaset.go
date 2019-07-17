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
	labelsutil "k8s.io/kubernetes/pkg/util/labels"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	"github.com/argoproj/argo-rollouts/utils/log"
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
		fresh, err := c.arogProjClientset.ArgoprojV1alpha1().Experiments(experiment.Namespace).Get(experiment.Name, metav1.GetOptions{})
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

func (ec *ExperimentController) reconcileReplicaSet(experiment *v1alpha1.Experiment, template v1alpha1.TemplateSpec) (*appsv1.ReplicaSet, error) {
	logCtx := log.WithExperiment(experiment)
	newRSTemplate := *template.Template.DeepCopy()

	podTemplateSpecHash := controller.ComputeHash(&newRSTemplate, experimentutil.GetCollisionCountForTemplate(experiment, template))
	newRSTemplate.Labels = labelsutil.CloneAndAddLabel(template.Template.Labels, v1alpha1.DefaultRolloutUniqueLabelKey, podTemplateSpecHash)
	//Add podTemplateHash label to selector.
	newRSSelector := labelsutil.CloneSelectorAndAddLabel(template.Selector, v1alpha1.DefaultRolloutUniqueLabelKey, podTemplateSpecHash)

	newRS := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            experimentutil.ReplicasetNameFromExperiment(experiment, template),
			Namespace:       experiment.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(experiment, controllerKind)},
			Labels:          newRSTemplate.Labels,
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas:        new(int32),
			MinReadySeconds: template.MinReadySeconds,
			Selector:        newRSSelector,
			Template:        newRSTemplate,
		},
	}

	newReplicasCount := experimentutil.CalculateTemplateReplicasCount(experiment, template)
	*(newRS.Spec.Replicas) = newReplicasCount

	// Create the new ReplicaSet. If it already exists, then we need to check for possible
	// hash collisions. If there is any other error, we need to report it in the status of
	// the Rollout.
	alreadyExists := false
	createdRS, err := ec.kubeclientset.AppsV1().ReplicaSets(experiment.Namespace).Create(&newRS)
	switch {
	// We may end up hitting this due to a slow cache or a fast resync of the Rollout.
	case errors.IsAlreadyExists(err):
		alreadyExists = true

		// Fetch a copy of the ReplicaSet.
		rs, rsErr := ec.replicaSetLister.ReplicaSets(newRS.Namespace).Get(newRS.Name)
		if rsErr != nil {
			return nil, rsErr
		}

		// If the Rollout owns the ReplicaSet and the ReplicaSet's PodTemplateSpec is semantically
		// deep equal to the PodTemplateSpec of the Rollout, it's the Rollout's new ReplicaSet.
		// Otherwise, this is a hash collision and we need to increment the collisionCount field in
		// the status of the Rollout and requeue to try the creation in the next sync.
		controllerRef := metav1.GetControllerOf(rs)
		if controllerRef != nil && controllerRef.UID == experiment.UID && replicasetutil.PodTemplateEqualIgnoreHash(&rs.Spec.Template, &template.Template) {
			createdRS = rs
			err = nil
			break
		}

		templateStatusPtr, statusIndex := experimentutil.GetTemplateStatus(experiment, template)
		templateStatus := v1alpha1.TemplateStatus{
			Name: template.Name,
		}
		if templateStatusPtr != nil {
			templateStatus = *templateStatusPtr
		}

		// Matching ReplicaSet is not equal - increment the collisionCount in the RolloutStatus
		// and requeue the Rollout.
		if templateStatus.CollisionCount == nil {
			templateStatus.CollisionCount = new(int32)
		}
		preCollisionCount := *templateStatus.CollisionCount
		*templateStatus.CollisionCount++

		statusCpy := experiment.Status.DeepCopy()
		templateStatuses := statusCpy.TemplateStatuses
		if statusIndex != nil {
			templateStatuses[*statusIndex] = templateStatus
		} else {
			templateStatuses = append(templateStatuses, templateStatus)
		}

		templateStatusBytes, err := json.Marshal(templateStatuses)
		if err != nil {
			return nil, err
		}

		patch := fmt.Sprintf(CollisionCountPatch, string(templateStatusBytes))
		_, err = ec.arogProjClientset.ArgoprojV1alpha1().Experiments(experiment.Namespace).Patch(experiment.Name, patchtypes.MergePatchType, []byte(patch))
		if err == nil {
			logCtx.Warnf("Found a hash collision - bumped collisionCount (%d->%d) to resolve it", preCollisionCount, *templateStatus.CollisionCount)
		}
		return nil, err
	case err != nil:
		msg := fmt.Sprintf(conditions.FailedRSCreateMessage, newRS.Name, err)
		ec.recorder.Event(experiment, corev1.EventTypeWarning, conditions.FailedRSCreateReason, msg)
		newStatus := experiment.Status.DeepCopy()
		ec.persistExperimentStatus(experiment, newStatus)
		return nil, err
	}

	if !alreadyExists && newReplicasCount > int32(0) {
		ec.recorder.Eventf(experiment, corev1.EventTypeNormal, "ScalingReplicaSet", "Scaled up replica set %s to %d", createdRS.Name, newReplicasCount)
	}

	return nil, err
}

func (ec *ExperimentController) scaleReplicaSetAndRecordEvent(rs *appsv1.ReplicaSet, newScale int32, experiment *v1alpha1.Experiment) (bool, *appsv1.ReplicaSet, error) {
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
	scaled, newRS, err := ec.scaleReplicaSet(rs, newScale, experiment, scalingOperation)
	return scaled, newRS, err
}

func (ec *ExperimentController) scaleReplicaSet(rs *appsv1.ReplicaSet, newScale int32, experiment *v1alpha1.Experiment, scalingOperation string) (bool, *appsv1.ReplicaSet, error) {
	sizeNeedsUpdate := *(rs.Spec.Replicas) != newScale
	scaled := false
	var err error
	if sizeNeedsUpdate {
		rsCopy := rs.DeepCopy()
		*(rsCopy.Spec.Replicas) = newScale
		rs, err = ec.kubeclientset.AppsV1().ReplicaSets(rsCopy.Namespace).Update(rsCopy)
		if err == nil && sizeNeedsUpdate {
			scaled = true
			ec.recorder.Eventf(experiment, corev1.EventTypeNormal, "ScalingReplicaSet", "Scaled %s replica set %s to %d", scalingOperation, rs.Name, newScale)
		}
	}
	return scaled, rs, err
}
