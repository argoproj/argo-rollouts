package rollout

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/util/uuid"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

func TestRolloutCreateExperiment(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutCanaryExperimentStep{
			Templates: []v1alpha1.RolloutExperimentTemplate{{
				Name:     "stable-template",
				SpecRef:  v1alpha1.StableSpecRef,
				Replicas: int32(1),
			}},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	ex, _ := GetExperimentFromTemplate(r2, rs1, rs2)
	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 1, 1, false)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectCreateExperimentAction(ex)
	f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
}

func TestRolloutExperimentProcessingDoNothing(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutCanaryExperimentStep{},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	ex, _ := GetExperimentFromTemplate(r2, rs1, rs2)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex)
	f.objects = append(f.objects, r2, ex)

	f.expectPatchRolloutAction(r1)
	f.run(getKey(r2, t))
}

func TestRolloutDegradedExperimentEnterDegraded(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutCanaryExperimentStep{},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	ex, _ := GetExperimentFromTemplate(r2, rs2, rs1)
	ex.Status.Conditions = []v1alpha1.ExperimentCondition{{
		Type:   v1alpha1.ExperimentProgressing,
		Reason: conditions.TimedOutReason,
	}}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex)
	f.objects = append(f.objects, r2, ex)

	patchIndex := f.expectPatchRolloutAction(r1)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"conditions": %s,
			"canary": {
				"experimentFailed": true
			}
		}
	}`
	generatedConditons := generateConditionsPatch(true, conditions.RolloutExperimentFailedReason, r2, false)
	assert.Equal(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, generatedConditons)), patch)

}

func TestRolloutExperimentScaleDownExtraExperiment(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutCanaryExperimentStep{},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 1, 1, false)
	ex, _ := GetExperimentFromTemplate(r2, rs1, rs2)
	extraExp := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "extraExp",
			Namespace:       r2.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(r2, controllerKind)},
			UID:             uuid.NewUUID(),
		},
		Status: v1alpha1.ExperimentStatus{
			Running: pointer.BoolPtr(true),
		},
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex, extraExp)
	f.objects = append(f.objects, r2, ex, extraExp)

	exPatchIndex := f.expectPatchExperimentAction(extraExp)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	exPatch := f.getPatchedExperiment(exPatchIndex)
	assert.NotNil(t, exPatch.Status.Running)
	assert.False(t, *exPatch.Status.Running)

}

func TestRolloutExperimentFinishedIncrementStep(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutCanaryExperimentStep{
			Templates: []v1alpha1.RolloutExperimentTemplate{{
				Name:     "stable-template",
				SpecRef:  v1alpha1.StableSpecRef,
				Replicas: int32(1),
			}},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)

	ex, _ := GetExperimentFromTemplate(r2, rs1, rs2)
	ex.Status.Running = pointer.BoolPtr(false)
	now := metav1.Now()
	ex.Status.AvailableAt = &now

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex)
	f.objects = append(f.objects, r2, ex)

	patchIndex := f.expectPatchRolloutAction(r1)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"currentStepIndex": 1,
			"conditions": %s
		}
	}`
	generatedConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs2, false)

	assert.Equal(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, generatedConditions)), patch)
}

func TestRolloutDoNotCreateExperimentWithoutNewRS(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutCanaryExperimentStep{
			Templates: []v1alpha1.RolloutExperimentTemplate{{
				Name:     "canary-template",
				SpecRef:  v1alpha1.CanarySpecRef,
				Replicas: int32(1),
			}},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	ex, _ := GetExperimentFromTemplate(r2, rs1, rs2)

	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 1, 1, false)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex)
	f.objects = append(f.objects, r2, ex)

	f.expectCreateReplicaSetAction(rs2)
	f.expectUpdateRolloutAction(r2)
	f.expectPatchRolloutAction(r1)
	f.run(getKey(r2, t))
}

func TestRolloutDoNotCreateExperimentWithoutStableRS(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutCanaryExperimentStep{
			Templates: []v1alpha1.RolloutExperimentTemplate{{
				Name:     "stable-template",
				SpecRef:  v1alpha1.StableSpecRef,
				Replicas: int32(1),
			}},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	ex, _ := GetExperimentFromTemplate(r2, rs1, rs2)

	r2 = updateCanaryRolloutStatus(r2, "", 1, 1, 1, false)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex)
	f.objects = append(f.objects, r2, ex)

	f.expectCreateReplicaSetAction(rs2)
	f.expectUpdateRolloutAction(r2)
	f.expectPatchRolloutAction(r1)
	f.run(getKey(r2, t))
}

// func TestExperimentCancelCurrentExperimentOnStepChange(t *testing.T) {
// 	f := newFixture(t)
// 	defer f.Close()

// 	steps := []v1alpha1.CanaryStep{
// 		{
// 			Experiment: &v1alpha1.RolloutCanaryExperimentStep{},
// 		},
// 		{
// 			SetWeight: pointer.Int32Ptr(30),
// 		},
// 	}

// 	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(0), intstr.FromInt(1))
// 	r2 := bumpVersion(r1)

// 	rs1 := newReplicaSetWithStatus(r1, 1, 1)
// 	rs2 := newReplicaSetWithStatus(r2, 0, 0)
// 	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
// 	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
// 	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

// 	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 1, 1, false)
// 	exToCancel, _ := GetExperimentFromTemplate(r2, rs1, rs2)

// 	f.rolloutLister = append(f.rolloutLister, r2)
// 	f.experimentLister = append(f.experimentLister, exToCancel)
// 	f.objects = append(f.objects, r2, exToCancel)

// 	exPatchIndex := f.expectPatchExperimentAction(exToCancel)
// 	f.expectPatchRolloutAction(r2)
// 	f.run(getKey(r2, t))
// 	exPatch := f.getPatchedExperiment(exPatchIndex)
// 	assert.NotNil(t, exPatch.Status.Running)
// 	assert.False(t, *exPatch.Status.Running)
// }

func TestGetExperimentFromTemplate(t *testing.T) {
	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutCanaryExperimentStep{
			Templates: []v1alpha1.RolloutExperimentTemplate{{
				Name:     "stable-template",
				SpecRef:  v1alpha1.StableSpecRef,
				Replicas: int32(1),
			}},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2.Status.CurrentStepIndex = pointer.Int32Ptr(0)
	r2.Status.Canary.StableRS = rs1PodHash

	stable, err := GetExperimentFromTemplate(r2, rs1, rs2)
	assert.Nil(t, err)
	assert.Equal(t, rs1.Spec.Template, stable.Spec.Templates[0].Template)

	r2.Spec.Strategy.CanaryStrategy.Steps[0].Experiment.Templates[0].SpecRef = v1alpha1.CanarySpecRef
	canary, err := GetExperimentFromTemplate(r2, rs1, rs2)
	assert.Nil(t, err)
	assert.Equal(t, rs2.Spec.Template, canary.Spec.Templates[0].Template)

	r2.Spec.Strategy.CanaryStrategy.Steps[0].Experiment.Templates[0].Metadata.Annotations = map[string]string{"abc": "def"}
	r2.Spec.Strategy.CanaryStrategy.Steps[0].Experiment.Templates[0].Metadata.Labels = map[string]string{"123": "456"}
	modifiedLabelAndAnnonations, err := GetExperimentFromTemplate(r2, rs1, rs2)
	assert.Nil(t, err)
	assert.Equal(t, modifiedLabelAndAnnonations.Spec.Templates[0].Template.ObjectMeta.Annotations["abc"], "def")
	assert.Equal(t, modifiedLabelAndAnnonations.Spec.Templates[0].Template.ObjectMeta.Labels["123"], "456")

	r2.Spec.Strategy.CanaryStrategy.Steps[0].Experiment.Templates[0].SpecRef = v1alpha1.ReplicaSetSpecRef("test")
	invalidRef, err := GetExperimentFromTemplate(r2, rs1, rs2)
	assert.Nil(t, invalidRef)
	assert.NotNil(t, err)
	assert.Error(t, err, "Invalid template step SpecRef: must be canary or stable")

	r2.Spec.Strategy.CanaryStrategy.Steps[0].Experiment = nil
	noStep, err := GetExperimentFromTemplate(r2, rs1, rs2)
	assert.Nil(t, noStep)
	assert.Nil(t, err)

}

func TestRolloutExperimentNoCreateWithoutStableOrNewRs(t *testing.T) {
}
