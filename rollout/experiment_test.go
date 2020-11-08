package rollout

import (
	"fmt"
	"testing"
	"time"

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

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{
			Templates: []v1alpha1.RolloutExperimentTemplate{{
				Name:     "stable-template",
				SpecRef:  v1alpha1.StableSpecRef,
				Replicas: pointer.Int32Ptr(1),
			}},
			Analyses: []v1alpha1.RolloutExperimentStepAnalysisTemplateRef{{
				Name:         "test",
				TemplateName: at.Name,
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
	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	createExIndex := f.expectCreateExperimentAction(ex)
	patchIndex := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdEx := f.getCreatedExperiment(createExIndex)
	assert.Equal(t, createdEx.Name, ex.Name)
	assert.Equal(t, createdEx.Spec.Analyses[0].TemplateName, at.Name)
	assert.Equal(t, createdEx.Spec.Analyses[0].Name, "test")
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentExperiment": "%s"
			},
			"conditions": %s
		}
	}`
	conds := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false, "")
	assert.Equal(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, ex.Name, conds)), patch)
}

func TestRolloutCreateClusterTemplateExperiment(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	cat := clusterAnalysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{
			Templates: []v1alpha1.RolloutExperimentTemplate{{
				Name:     "stable-template",
				SpecRef:  v1alpha1.StableSpecRef,
				Replicas: pointer.Int32Ptr(1),
			}},
			Analyses: []v1alpha1.RolloutExperimentStepAnalysisTemplateRef{{
				Name:         "test",
				TemplateName: cat.Name,
				ClusterScope: true,
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
	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	createExIndex := f.expectCreateExperimentAction(ex)
	patchIndex := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdEx := f.getCreatedExperiment(createExIndex)
	assert.Equal(t, createdEx.Name, ex.Name)
	assert.Equal(t, createdEx.Spec.Analyses[0].TemplateName, cat.Name)
	assert.True(t, createdEx.Spec.Analyses[0].ClusterScope)
	assert.Equal(t, createdEx.Spec.Analyses[0].Name, "test")
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentExperiment": "%s"
			},
			"conditions": %s
		}
	}`
	conds := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false, "")
	assert.Equal(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, ex.Name, conds)), patch)
}

func TestCreateExperimentWithCollision(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{
			Templates: []v1alpha1.RolloutExperimentTemplate{{
				Name:     "stable-template",
				SpecRef:  v1alpha1.StableSpecRef,
				Replicas: pointer.Int32Ptr(1),
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
	ex.Status.Phase = v1alpha1.AnalysisPhaseFailed
	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)

	f.experimentLister = append(f.experimentLister, ex)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2, ex)

	f.expectCreateExperimentAction(ex)                  // create fails
	f.expectGetExperimentAction(ex)                     // get existing
	createExIndex := f.expectCreateExperimentAction(ex) // create with a new name
	patchIndex := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdEx := f.getCreatedExperiment(createExIndex)
	assert.Equal(t, ex.Name+".1", createdEx.Name)
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentExperiment": "%s"
			},
			"conditions": %s
		}
	}`
	conds := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false, "")
	assert.Equal(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, createdEx.Name, conds)), patch)
}

func TestCreateExperimentWithCollisionAndSemanticEquality(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{
			Templates: []v1alpha1.RolloutExperimentTemplate{{
				Name:     "stable-template",
				SpecRef:  v1alpha1.StableSpecRef,
				Replicas: pointer.Int32Ptr(1),
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
	ex.Status.Phase = v1alpha1.AnalysisPhaseRunning
	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)

	f.experimentLister = append(f.experimentLister, ex)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2, ex)

	createExIndex := f.expectCreateExperimentAction(ex)
	f.expectGetExperimentAction(ex) // get existing to verify semantic equality
	patchIndex := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdEx := f.getCreatedExperiment(createExIndex)
	assert.Equal(t, ex.Name, createdEx.Name)
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentExperiment": "%s"
			},
			"conditions": %s
		}
	}`
	conds := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false, "")
	assert.Equal(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, ex.Name, conds)), patch)
}

func TestRolloutExperimentProcessingDoNothing(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{},
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
	r2.Status.Canary.CurrentExperiment = ex.Name
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex)
	f.objects = append(f.objects, r2, ex)

	patchIndex := f.expectPatchRolloutAction(r1)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)

}

func TestAbortRolloutAfterFailedExperiment(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{},
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
	ex.Status.Phase = v1alpha1.AnalysisPhaseFailed
	r2.Status.Canary.CurrentExperiment = ex.Name

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex)
	f.objects = append(f.objects, r2, ex)

	patchIndex := f.expectPatchRolloutAction(r1)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"abort": true,
			"abortedAt": "%s",
			"conditions": %s,
			"canary": {
				"currentExperiment": null
			}
		}
	}`
	now := metav1.Now().UTC().Format(time.RFC3339)
	generatedConditions := generateConditionsPatch(true, conditions.RolloutAbortedReason, r2, false, "")
	assert.Equal(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, now, generatedConditions)), patch)
}

func TestPauseRolloutAfterInconclusiveExperiment(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{},
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
	ex.Status.Phase = v1alpha1.AnalysisPhaseInconclusive
	r2.Status.Canary.CurrentExperiment = ex.Name

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex)
	f.objects = append(f.objects, r2, ex)

	patchIndex := f.expectPatchRolloutAction(r1)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatchFmt := `{
		"status": {
			"canary": {
				"currentExperiment": null
			},
			"pauseConditions": [{
				"reason": "%s",
				"startTime": "%s"
			}],
			"conditions": %s,
			"controllerPause": true
		}
	}`
	now := metav1.Now().UTC().Format(time.RFC3339)
	conditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false, "")
	expectedPatch := calculatePatch(r2, fmt.Sprintf(expectedPatchFmt, v1alpha1.PauseReasonInconclusiveExperiment, now, conditions))
	assert.Equal(t, expectedPatch, patch)
}

func TestRolloutExperimentScaleDownExperimentFromPreviousStep(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{Experiment: &v1alpha1.RolloutExperimentStep{}},
		{SetWeight: pointer.Int32Ptr(1)},
	}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 1, 1, false)
	ex, _ := GetExperimentFromTemplate(r2, rs1, rs2)
	r2.Status.Canary.CurrentExperiment = ex.Name

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex)
	f.objects = append(f.objects, r2, ex)

	exPatchIndex := f.expectPatchExperimentAction(ex)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	exPatch := f.getPatchedExperiment(exPatchIndex)
	assert.True(t, exPatch.Spec.Terminate)
}

func TestRolloutExperimentScaleDownExtraExperiment(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{},
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
	r2.Status.Canary.CurrentExperiment = ex.Name
	extraExp := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "extraExp",
			Namespace:       r2.Namespace,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(r2, controllerKind)},
			UID:             uuid.NewUUID(),
			Labels:          map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash},
		},
		Status: v1alpha1.ExperimentStatus{
			Phase: v1alpha1.AnalysisPhasePending,
		},
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex, extraExp)
	f.objects = append(f.objects, r2, ex, extraExp)

	exPatchIndex := f.expectPatchExperimentAction(extraExp)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	exPatch := f.getPatchedExperiment(exPatchIndex)
	assert.True(t, exPatch.Spec.Terminate)
}

func TestRolloutExperimentFinishedIncrementStep(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{
			Templates: []v1alpha1.RolloutExperimentTemplate{{
				Name:     "stable-template",
				SpecRef:  v1alpha1.StableSpecRef,
				Replicas: pointer.Int32Ptr(1),
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
	ex.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
	now := metav1.Now()
	ex.Status.AvailableAt = &now
	r2.Status.Canary.CurrentExperiment = ex.Name

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex)
	f.objects = append(f.objects, r2, ex)

	patchIndex := f.expectPatchRolloutAction(r1)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentExperiment":null
			},
			"currentStepIndex": 1,
			"conditions": %s
		}
	}`
	generatedConditions := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs2, false, "")

	assert.Equal(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, generatedConditions)), patch)
}

func TestRolloutDoNotCreateExperimentWithoutStableRS(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{
			Templates: []v1alpha1.RolloutExperimentTemplate{{
				Name:     "stable-template",
				SpecRef:  v1alpha1.StableSpecRef,
				Replicas: pointer.Int32Ptr(1),
			}},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs2 := newReplicaSetWithStatus(r2, 1, 1)

	r2 = updateCanaryRolloutStatus(r2, "", 1, 1, 1, false)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	f.expectCreateReplicaSetAction(rs2)
	f.expectUpdateRolloutAction(r2)       // update revision
	f.expectUpdateRolloutStatusAction(r2) // update progressing condition
	f.expectPatchRolloutAction(r1)
	f.run(getKey(r2, t))
}

func TestGetExperimentFromTemplate(t *testing.T) {
	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{
			Templates: []v1alpha1.RolloutExperimentTemplate{{
				Name:     "stable-template",
				SpecRef:  v1alpha1.StableSpecRef,
				Replicas: pointer.Int32Ptr(1),
			}},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2.Status.CurrentStepIndex = pointer.Int32Ptr(0)
	r2.Status.StableRS = rs1PodHash

	stable, err := GetExperimentFromTemplate(r2, rs1, rs2)
	assert.Nil(t, err)
	assert.Equal(t, rs1.Spec.Template, stable.Spec.Templates[0].Template)
	assert.Equal(t, rs1.Spec.Selector, stable.Spec.Templates[0].Selector)

	newSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"foo": "bar",
		},
	}
	r2.Spec.Strategy.Canary.Steps[0].Experiment.Templates[0].Selector = &newSelector
	modifiedSelector, err := GetExperimentFromTemplate(r2, rs1, rs2)
	assert.Nil(t, err)
	assert.Equal(t, newSelector, *modifiedSelector.Spec.Templates[0].Selector)

	r2.Spec.Strategy.Canary.Steps[0].Experiment.Templates[0].SpecRef = v1alpha1.CanarySpecRef
	canary, err := GetExperimentFromTemplate(r2, rs1, rs2)
	assert.Nil(t, err)
	assert.Equal(t, rs2.Spec.Template, canary.Spec.Templates[0].Template)

	r2.Spec.Strategy.Canary.Steps[0].Experiment.Templates[0].Metadata.Annotations = map[string]string{"abc": "def"}
	r2.Spec.Strategy.Canary.Steps[0].Experiment.Templates[0].Metadata.Labels = map[string]string{"123": "456"}
	modifiedLabelAndAnnotations, err := GetExperimentFromTemplate(r2, rs1, rs2)
	assert.Nil(t, err)
	assert.Equal(t, modifiedLabelAndAnnotations.Spec.Templates[0].Template.ObjectMeta.Annotations["abc"], "def")
	assert.Equal(t, modifiedLabelAndAnnotations.Spec.Templates[0].Template.ObjectMeta.Labels["123"], "456")

	r2.Spec.Strategy.Canary.Steps[0].Experiment.Templates[0].SpecRef = v1alpha1.ReplicaSetSpecRef("test")
	invalidRef, err := GetExperimentFromTemplate(r2, rs1, rs2)
	assert.Nil(t, invalidRef)
	assert.NotNil(t, err)
	assert.Error(t, err, "Invalid template step SpecRef: must be canary or stable")

	r2.Spec.Strategy.Canary.Steps[0].Experiment = nil
	noStep, err := GetExperimentFromTemplate(r2, rs1, rs2)
	assert.Nil(t, noStep)
	assert.Nil(t, err)
}

func TestDeleteExperimentWithNoMatchingRS(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	ex, _ := GetExperimentFromTemplate(r2, rs1, rs2)
	ex.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
	r2.Status.Canary.CurrentExperiment = ex.Name
	exWithNoMatchingPodHash := ex.DeepCopy()
	exWithNoMatchingPodHash.UID = uuid.NewUUID()
	exWithNoMatchingPodHash.Name = "no-matching-rs"
	exWithNoMatchingPodHash.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = "abc123"

	exWithDeletionTimeStamp := exWithNoMatchingPodHash.DeepCopy()
	exWithDeletionTimeStamp.Name = "has-deletion-timestamp"
	now := metav1.Now()
	exWithDeletionTimeStamp.DeletionTimestamp = &now

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex, exWithNoMatchingPodHash, exWithDeletionTimeStamp)
	f.objects = append(f.objects, r2, ex, exWithNoMatchingPodHash, exWithDeletionTimeStamp)

	deletedIndex := f.expectDeleteExperimentAction(exWithNoMatchingPodHash)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	deletedAr := f.getDeletedExperiment(deletedIndex)
	assert.Equal(t, deletedAr, exWithNoMatchingPodHash.Name)
}

func TestDeleteExperimentsAfterRSDelete(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	r3 := bumpVersion(r2)
	r3.Spec.RevisionHistoryLimit = pointer.Int32Ptr(0)

	rs1 := newReplicaSetWithStatus(r1, 0, 0)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs3 := newReplicaSetWithStatus(r3, 0, 0)

	ex, _ := GetExperimentFromTemplate(r3, rs2, rs1)
	ex.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, rs3)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2, rs3)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	exToDelete := ex.DeepCopy()
	exToDelete.Name = "older-experiment"
	exToDelete.UID = uuid.NewUUID()
	exToDelete.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = rs1PodHash

	r3 = updateCanaryRolloutStatus(r3, rs2PodHash, 1, 0, 1, false)
	r3.Status.Canary.CurrentExperiment = ex.Name

	f.rolloutLister = append(f.rolloutLister, r3)
	f.experimentLister = append(f.experimentLister, ex, exToDelete)
	f.objects = append(f.objects, r3, ex, exToDelete)

	f.expectDeleteReplicaSetAction(rs1)
	deletedIndex := f.expectDeleteExperimentAction(exToDelete)
	f.expectPatchRolloutAction(r3)
	f.run(getKey(r3, t))

	deletedEx := f.getDeletedExperiment(deletedIndex)
	assert.Equal(t, deletedEx, exToDelete.Name)
}

func TestCancelExperimentWhenAborted(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	ex, _ := GetExperimentFromTemplate(r2, rs1, rs2)
	ex.Name = "test"
	ex.Status.Phase = v1alpha1.AnalysisPhaseRunning

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	r2.Status.Abort = true
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.experimentLister = append(f.experimentLister, ex)
	f.objects = append(f.objects, r2, ex)

	f.expectPatchExperimentAction(ex)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
}

func TestRolloutCreateExperimentWithInstanceID(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Experiment: &v1alpha1.RolloutExperimentStep{
			Templates: []v1alpha1.RolloutExperimentTemplate{{
				Name:     "stable-template",
				SpecRef:  v1alpha1.StableSpecRef,
				Replicas: pointer.Int32Ptr(1),
			}},
			Analyses: []v1alpha1.RolloutExperimentStepAnalysisTemplateRef{{
				Name:         "test",
				TemplateName: at.Name,
			}},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	r2.Labels = map[string]string{v1alpha1.LabelKeyControllerInstanceID: "instance-id-test"}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	ex, _ := GetExperimentFromTemplate(r2, rs1, rs2)
	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)

	createExIndex := f.expectCreateExperimentAction(ex)
	f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdEx := f.getCreatedExperiment(createExIndex)
	assert.Equal(t, createdEx.Name, ex.Name)
	assert.Equal(t, "instance-id-test", createdEx.Labels[v1alpha1.LabelKeyControllerInstanceID])
}
