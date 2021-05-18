package experiments

import (
	"context"
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/controller"
	labelsutil "k8s.io/kubernetes/pkg/util/labels"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	log "github.com/sirupsen/logrus"
)

const (
	CollisionCountPatch = `{
	"status" : {
		"templateStatuses" : %s
	}
}`
)

var controllerKind = v1alpha1.SchemeGroupVersion.WithKind("Experiment")

func (c *Controller) getReplicaSetsForExperiment(experiment *v1alpha1.Experiment) (map[string]*appsv1.ReplicaSet, error) {
	rsList, err := c.replicaSetLister.ReplicaSets(experiment.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	templateDefined := func(name string) bool {
		for _, tmpl := range experiment.Spec.Templates {
			if tmpl.Name == name {
				return true
			}
		}
		return false
	}
	templateToRS := make(map[string]*appsv1.ReplicaSet)
	for _, rs := range rsList {
		controllerRef := metav1.GetControllerOf(rs)
		if controllerRef == nil || controllerRef.UID != experiment.UID || rs.Annotations == nil || rs.Annotations[v1alpha1.ExperimentNameAnnotationKey] != experiment.Name {
			continue
		}
		if templateName := rs.Annotations[v1alpha1.ExperimentTemplateNameAnnotationKey]; templateName != "" {
			if _, ok := templateToRS[templateName]; ok {
				return nil, fmt.Errorf("multiple ReplicaSets match single experiment template: %s", templateName)
			}
			if templateDefined(templateName) {
				templateToRS[templateName] = rs
				logCtx := log.WithField(logutil.ExperimentKey, experiment.Name).WithField(logutil.NamespaceKey, experiment.Namespace)
				logCtx.Infof("Claimed ReplicaSet '%s' for template '%s'", rs.Name, templateName)
			}
		}
	}

	return templateToRS, nil
}

// createReplicaSet creates a new replicaset based on the template
func (ec *experimentContext) createReplicaSet(template v1alpha1.TemplateSpec, collisionCount *int32) (*appsv1.ReplicaSet, error) {
	ctx := context.TODO()
	newRS := newReplicaSetFromTemplate(ec.ex, template, collisionCount)

	newReplicasCount := experimentutil.CalculateTemplateReplicasCount(ec.ex, template)
	*(newRS.Spec.Replicas) = newReplicasCount

	// Create the new ReplicaSet. If it already exists, then we need to check for possible
	// hash collisions. If there is any other error, we need to report it in the status of
	// the Experiment.
	alreadyExists := false
	createdRS, err := ec.kubeclientset.AppsV1().ReplicaSets(ec.ex.Namespace).Create(ctx, &newRS, metav1.CreateOptions{})
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
		if ec.isReplicaSetSemanticallyEqual(&newRS, rs) {
			// NOTE: we should only get here when the informer cache is stale and we already
			// succeeded in creating this replicaset
			createdRS = rs
			err = nil
			ec.log.Warnf("Claimed existing ReplicaSet %s with equivalent template spec", createdRS.Name)
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
		templateStatusBytes, marshalErr := json.Marshal(statusCpy.TemplateStatuses)
		if marshalErr != nil {
			return nil, marshalErr
		}

		patch := fmt.Sprintf(CollisionCountPatch, string(templateStatusBytes))
		_, patchErr := ec.argoProjClientset.ArgoprojV1alpha1().Experiments(ec.ex.Namespace).Patch(ctx, ec.ex.Name, patchtypes.MergePatchType, []byte(patch), metav1.PatchOptions{})
		ec.log.WithField("patch", patch).Debug("Applied Patch")
		if patchErr != nil {
			ec.log.Errorf("Error patching service %s", err.Error())
			return nil, patchErr
		}
		ec.log.Warnf("Found a hash collision - bumped collisionCount (%d->%d) to resolve it", preCollisionCount, *newTemplate.CollisionCount)
		return nil, err
	case err != nil:
		msg := fmt.Sprintf(conditions.FailedRSCreateMessage, newRS.Name, err)
		ec.recorder.Eventf(ec.ex, record.EventOptions{EventReason: conditions.FailedRSCreateReason}, msg)
		return nil, err
	default:
		ec.log.Infof("Created ReplicaSet %s", createdRS.Name)
	}

	if !alreadyExists && newReplicasCount > int32(0) {
		ec.recorder.Eventf(ec.ex, record.EventOptions{EventReason: conditions.NewReplicaSetReason}, conditions.NewReplicaSetMessage+" with size %d", createdRS.Name, newReplicasCount)
	}

	return createdRS, nil
}

// newReplicaSetFromTemplate is a helper to formulate a replicaset from an experiment's template
func newReplicaSetFromTemplate(experiment *v1alpha1.Experiment, template v1alpha1.TemplateSpec, collisionCount *int32) appsv1.ReplicaSet {
	newRSTemplate := *template.Template.DeepCopy()
	replicaSetAnnotations := newReplicaSetAnnotations(experiment.Name, template.Name)
	if newRSTemplate.Labels != nil {
		if _, ok := newRSTemplate.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]; ok {
			delete(newRSTemplate.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)
		}
	}
	podHash := controller.ComputeHash(&newRSTemplate, collisionCount)

	newRSTemplate.Labels = labelsutil.CloneAndAddLabel(newRSTemplate.Labels, v1alpha1.DefaultRolloutUniqueLabelKey, podHash)
	// Add podTemplateHash label to selector.
	newRSSelector := labelsutil.CloneSelectorAndAddLabel(template.Selector, v1alpha1.DefaultRolloutUniqueLabelKey, podHash)

	// The annotations must be different for each template because annotations are used to match
	// replicasets to templates. We inject the experiment and template name in the replicaset
	// annotations to ensure uniqueness.
	rs := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", experiment.Name, template.Name),
			Namespace: experiment.Namespace,
			Labels: map[string]string{
				v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
			},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(experiment, controllerKind)},
			Annotations:     replicaSetAnnotations,
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas:        new(int32),
			MinReadySeconds: template.MinReadySeconds,
			Selector:        newRSSelector,
			Template:        newRSTemplate,
		},
	}
	return rs
}

// isReplicaSetSemanticallyEqual checks to see if an existing ReplicaSet is semantically equal
// to the ReplicaSet we are trying to create
func (ec *experimentContext) isReplicaSetSemanticallyEqual(newRS, existingRS *appsv1.ReplicaSet) bool {
	controllerRef := metav1.GetControllerOf(existingRS)
	podTemplatesEqual := replicasetutil.PodTemplateEqualIgnoreHash(&existingRS.Spec.Template, &newRS.Spec.Template)
	existingAnnotations := existingRS.GetAnnotations()
	newAnnotations := newRS.GetAnnotations()
	return controllerRef != nil &&
		controllerRef.UID == ec.ex.UID &&
		podTemplatesEqual &&
		existingAnnotations != nil &&
		existingAnnotations[v1alpha1.ExperimentNameAnnotationKey] == newAnnotations[v1alpha1.ExperimentNameAnnotationKey] &&
		existingAnnotations[v1alpha1.ExperimentTemplateNameAnnotationKey] == newAnnotations[v1alpha1.ExperimentTemplateNameAnnotationKey]
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
	if err != nil {
		// TODO(jessesuen): gracefully handle conflict issues
		msg := fmt.Sprintf("Failed to scale %s %s: %v", rs.Name, scalingOperation, err)
		ec.recorder.Warnf(ec.ex, record.EventOptions{EventReason: "ReplicaSetUpdateError"}, msg)
	} else {
		ec.log.Infof("Scaled %s ReplicaSet %s from %d to %d", scalingOperation, rs.Name, *(rs.Spec.Replicas), newScale)
	}
	return scaled, newRS, err
}

func (ec *experimentContext) scaleReplicaSet(rs *appsv1.ReplicaSet, newScale int32, scalingOperation string) (bool, *appsv1.ReplicaSet, error) {
	ctx := context.TODO()
	oldScale := *(rs.Spec.Replicas)
	sizeNeedsUpdate := oldScale != newScale
	scaled := false
	var err error
	if sizeNeedsUpdate {
		rsCopy := rs.DeepCopy()
		*(rsCopy.Spec.Replicas) = newScale
		rs, err = ec.kubeclientset.AppsV1().ReplicaSets(rsCopy.Namespace).Update(ctx, rsCopy, metav1.UpdateOptions{})
		if err == nil && sizeNeedsUpdate {
			scaled = true
			ec.recorder.Eventf(ec.ex, record.EventOptions{EventReason: conditions.ScalingReplicaSetReason}, "Scaled %s ReplicaSet %s from %d to %d", scalingOperation, rs.Name, oldScale, newScale)
		}
	}
	return scaled, rs, err
}

func newReplicaSetAnnotations(experimentName, templateName string) map[string]string {
	return map[string]string{
		v1alpha1.ExperimentNameAnnotationKey:         experimentName,
		v1alpha1.ExperimentTemplateNameAnnotationKey: templateName,
	}
}
