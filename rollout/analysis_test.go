package rollout

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/utils/hash"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
)

func analysisTemplate(name string) *v1alpha1.AnalysisTemplate {
	return &v1alpha1.AnalysisTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{{
				Name: "example",
			}},
			DryRun: []v1alpha1.DryRun{{
				MetricName: "example",
			}},
			MeasurementRetention: []v1alpha1.MeasurementRetention{{
				MetricName: "example",
			}},
		},
	}
}

func clusterAnalysisTemplate(name string) *v1alpha1.ClusterAnalysisTemplate {
	return &v1alpha1.ClusterAnalysisTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{{
				Name: "clusterexample",
			}},
		},
	}
}

func clusterAnalysisRun(cat *v1alpha1.ClusterAnalysisTemplate, analysisRunType string, r *v1alpha1.Rollout) *v1alpha1.AnalysisRun {
	labels := map[string]string{}
	podHash := hash.ComputePodTemplateHash(&r.Spec.Template, r.Status.CollisionCount)
	var name string
	if analysisRunType == v1alpha1.RolloutTypeStepLabel {
		labels = analysisutil.StepLabels(*r.Status.CurrentStepIndex, podHash, "")
		name = fmt.Sprintf("%s-%s-%s-%s", r.Name, podHash, "2", cat.Name)
	} else if analysisRunType == v1alpha1.RolloutTypeBackgroundRunLabel {
		labels = analysisutil.BackgroundLabels(podHash, "")
		name = fmt.Sprintf("%s-%s-%s", r.Name, podHash, "2")
	} else if analysisRunType == v1alpha1.RolloutTypePrePromotionLabel {
		labels = analysisutil.PrePromotionLabels(podHash, "")
		name = fmt.Sprintf("%s-%s-%s-pre", r.Name, podHash, "2")
	} else if analysisRunType == v1alpha1.RolloutTypePostPromotionLabel {
		labels = analysisutil.PostPromotionLabels(podHash, "")
		name = fmt.Sprintf("%s-%s-%s-post", r.Name, podHash, "2")
	}
	return &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       metav1.NamespaceDefault,
			Labels:          labels,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(r, controllerKind)},
		},
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: cat.Spec.Metrics,
			Args:    cat.Spec.Args,
		},
	}
}

func analysisRun(at *v1alpha1.AnalysisTemplate, analysisRunType string, r *v1alpha1.Rollout) *v1alpha1.AnalysisRun {
	labels := map[string]string{}
	podHash := hash.ComputePodTemplateHash(&r.Spec.Template, r.Status.CollisionCount)
	var name string
	if analysisRunType == v1alpha1.RolloutTypeStepLabel {
		labels = analysisutil.StepLabels(*r.Status.CurrentStepIndex, podHash, "")
		name = fmt.Sprintf("%s-%s-%s-%s", r.Name, podHash, "2", at.Name)
	} else if analysisRunType == v1alpha1.RolloutTypeBackgroundRunLabel {
		labels = analysisutil.BackgroundLabels(podHash, "")
		name = fmt.Sprintf("%s-%s-%s", r.Name, podHash, "2")
	} else if analysisRunType == v1alpha1.RolloutTypePrePromotionLabel {
		labels = analysisutil.PrePromotionLabels(podHash, "")
		name = fmt.Sprintf("%s-%s-%s-pre", r.Name, podHash, "2")
	} else if analysisRunType == v1alpha1.RolloutTypePostPromotionLabel {
		labels = analysisutil.PostPromotionLabels(podHash, "")
		name = fmt.Sprintf("%s-%s-%s-post", r.Name, podHash, "2")
	}
	return &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			Namespace:       metav1.NamespaceDefault,
			Labels:          labels,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(r, controllerKind)},
		},
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics:              at.Spec.Metrics,
			DryRun:               at.Spec.DryRun,
			MeasurementRetention: at.Spec.MeasurementRetention,
			Args:                 at.Spec.Args,
		},
	}
}

func TestCreateBackgroundAnalysisRun(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	at := analysisTemplate("bar")
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completeCond, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completeCond)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, r2, at)

	createdIndex := f.expectCreateAnalysisRunAction(ar)
	f.expectUpdateReplicaSetAction(rs2)
	index := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdAr := f.getCreatedAnalysisRun(createdIndex)
	expectedArName := fmt.Sprintf("%s-%s-%s", r2.Name, rs2PodHash, "2")
	assert.Equal(t, expectedArName, createdAr.Name)

	patch := f.getPatchedRollout(index)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentBackgroundAnalysisRunStatus": {
					"name": "%s",
					"status": ""
				}
			}
		}
	}`
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, expectedArName)), patch)
}

func TestCreateBackgroundAnalysisRunWithTemplates(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	at := analysisTemplate("bar")
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{{
				TemplateName: at.Name,
			}},
		},
	}

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completeCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completeCondition)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, r2, at)

	createdIndex := f.expectCreateAnalysisRunAction(ar)
	f.expectUpdateReplicaSetAction(rs2)
	index := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdAr := f.getCreatedAnalysisRun(createdIndex)
	expectedArName := fmt.Sprintf("%s-%s-%s", r2.Name, rs2PodHash, "2")
	assert.Equal(t, expectedArName, createdAr.Name)

	patch := f.getPatchedRollout(index)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentBackgroundAnalysisRunStatus": {
					"name": "%s",
					"status": ""
				}
			}
		}
	}`
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, expectedArName)), patch)
}

func TestCreateBackgroundAnalysisRunWithClusterTemplates(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	cat := clusterAnalysisTemplate("bar")
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := clusterAnalysisRun(cat, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{{
				TemplateName: cat.Name,
				ClusterScope: true,
			}},
		},
	}

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.clusterAnalysisTemplateLister = append(f.clusterAnalysisTemplateLister, cat)
	f.objects = append(f.objects, r2, cat)

	createdIndex := f.expectCreateAnalysisRunAction(ar)
	f.expectUpdateReplicaSetAction(rs2)
	index := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdAr := f.getCreatedAnalysisRun(createdIndex)
	expectedArName := fmt.Sprintf("%s-%s-%s", r2.Name, rs2PodHash, "2")
	assert.Equal(t, expectedArName, createdAr.Name)

	patch := f.getPatchedRollout(index)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentBackgroundAnalysisRunStatus": {
					"name": "%s",
					"status": ""
				}
			}
		}
	}`
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, expectedArName)), patch)
}

func TestInvalidSpecMissingClusterTemplatesBackgroundAnalysis(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newCanaryRollout("foo", 10, nil, nil, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{{
				TemplateName: "missing",
				ClusterScope: true,
			}},
		},
	}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	expectedPatchWithoutSub := `{
		"status": {
			"conditions": [%s,%s],
			"phase": "Degraded",
			"message": "InvalidSpec: %s"
		}
	}`
	errmsg := "The Rollout \"foo\" is invalid: spec.strategy.canary.analysis.templates: Invalid value: \"missing\": ClusterAnalysisTemplate 'missing' not found"
	_, progressingCond := newProgressingCondition(conditions.ReplicaSetUpdatedReason, r, "")
	invalidSpecCond := conditions.NewRolloutCondition(v1alpha1.InvalidSpec, corev1.ConditionTrue, conditions.InvalidSpecReason, errmsg)
	invalidSpecBytes, _ := json.Marshal(invalidSpecCond)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutSub, progressingCond, string(invalidSpecBytes), strings.ReplaceAll(errmsg, "\"", "\\\""))

	patch := f.getPatchedRollout(patchIndex)
	assert.JSONEq(t, calculatePatch(r, expectedPatch), patch)
}

func TestCreateBackgroundAnalysisRunWithClusterTemplatesAndTemplate(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	at := analysisTemplate("bar")
	cat := clusterAnalysisTemplate("clusterbar")
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	ar := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "run1",
			Namespace:       metav1.NamespaceDefault,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(r1, controllerKind)},
		},
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: at.Spec.Metrics,
			Args:    at.Spec.Args,
		},
	}
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{{
				TemplateName: cat.Name,
				ClusterScope: true,
			}, {
				TemplateName: at.Name,
			}},
		},
	}
	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.clusterAnalysisTemplateLister = append(f.clusterAnalysisTemplateLister, cat)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, r2, cat, at)

	createdIndex := f.expectCreateAnalysisRunAction(ar)
	f.expectUpdateReplicaSetAction(rs2)
	index := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdAr := f.getCreatedAnalysisRun(createdIndex)
	expectedArName := fmt.Sprintf("%s-%s-%s", r2.Name, rs2PodHash, "2")
	assert.Equal(t, expectedArName, createdAr.Name)
	assert.Len(t, createdAr.Spec.Metrics, 2)

	patch := f.getPatchedRollout(index)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentBackgroundAnalysisRunStatus": {
					"name": "%s",
					"status": ""
				}
			}
		}
	}`
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, expectedArName)), patch)
}

// TestCreateAnalysisRunWithCollision ensures we will create an new analysis run with a new name
// when there is a conflict (e.g. such as when there is a retry)
func TestCreateAnalysisRunWithCollision(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	at := analysisTemplate("bar")
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	//rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)

	ar.Status.Phase = v1alpha1.AnalysisPhaseFailed

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, r2, at, ar)

	f.expectCreateAnalysisRunAction(ar) // this fails due to conflict
	f.expectGetAnalysisRunAction(ar)    // get will retrieve the existing one to compare semantic equality
	expectedAR := ar.DeepCopy()
	expectedAR.Name = ar.Name + ".1"
	createdIndex := f.expectCreateAnalysisRunAction(expectedAR) // this succeeds
	f.expectUpdateReplicaSetAction(rs2)
	index := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdAr := f.getCreatedAnalysisRun(createdIndex)
	assert.Equal(t, expectedAR.Name, createdAr.Name)

	patch := f.getPatchedRollout(index)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentBackgroundAnalysisRunStatus": {
					"name": "%s",
					"status": ""
				}
			}
		}
	}`
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, expectedAR.Name)), patch)
}

// TestCreateAnalysisRunWithCollisionAndSemanticEquality will ensure we do not create an extra
// AnalysisRun when the existing one is our own.
func TestCreateAnalysisRunWithCollisionAndSemanticEquality(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	at := analysisTemplate("bar")
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, r2, at, ar)

	f.expectCreateAnalysisRunAction(ar) // this fails due to conflict
	f.expectGetAnalysisRunAction(ar)    // get will retrieve the existing one to compare semantic equality
	f.expectUpdateReplicaSetAction(rs2)
	index := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(index)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentBackgroundAnalysisRunStatus": {
					"name": "%s",
					"status": ""
				}
			}
		}
	}`
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, ar.Name)), patch)
}

func TestCreateAnalysisRunOnAnalysisStep(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r2)
	ar.Status.Phase = v1alpha1.AnalysisPhaseRunning

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, r2, at)

	createdIndex := f.expectCreateAnalysisRunAction(ar)
	index := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdAr := f.getCreatedAnalysisRun(createdIndex)
	expectedArName := fmt.Sprintf("%s-%s-%s-%s", r2.Name, rs2PodHash, "2", "0")
	assert.Equal(t, expectedArName, createdAr.Name)

	patch := f.getPatchedRollout(index)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentStepAnalysisRunStatus": {
					"name": "%s",
					"status": ""
				}
			}
		}
	}`
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, expectedArName)), patch)
}

func TestFailCreateStepAnalysisRunIfInvalidTemplateRef(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: "bad-template",
				},
			},
		},
	}}

	at := analysisTemplate("bad-template")
	at.Spec.Metrics = append(at.Spec.Metrics, at.Spec.Metrics[0])
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)

	r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r, at)

	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	expectedPatchWithoutSub := `{
		"status": {
			"conditions": [%s,%s],
			"phase": "Degraded",
			"message": "InvalidSpec: %s"
		}
	}`
	errmsg := "The Rollout \"foo\" is invalid: spec.strategy.canary.steps[0].analysis.templates: Invalid value: \"templateNames: [bad-template]\": two metrics have the same name 'example'"
	_, progressingCond := newProgressingCondition(conditions.ReplicaSetUpdatedReason, r, "")
	invalidSpecCond := conditions.NewRolloutCondition(v1alpha1.InvalidSpec, corev1.ConditionTrue, conditions.InvalidSpecReason, errmsg)
	invalidSpecBytes, _ := json.Marshal(invalidSpecCond)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutSub, progressingCond, string(invalidSpecBytes), strings.ReplaceAll(errmsg, "\"", "\\\""))

	patch := f.getPatchedRollout(patchIndex)
	assert.JSONEq(t, calculatePatch(r, expectedPatch), patch)
}

func TestFailCreateBackgroundAnalysisRunIfInvalidTemplateRef(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: pointer.Int32Ptr(10),
	}}

	at := analysisTemplate("bad-template")
	at.Spec.Metrics = append(at.Spec.Metrics, at.Spec.Metrics[0])
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)

	r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: "bad-template",
				},
			},
		},
	}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r, at)

	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	expectedPatchWithoutSub := `{
		"status": {
			"conditions": [%s,%s],
			"phase": "Degraded",
			"message": "InvalidSpec: %s"
		}
	}`
	errmsg := "The Rollout \"foo\" is invalid: spec.strategy.canary.analysis.templates: Invalid value: \"templateNames: [bad-template]\": two metrics have the same name 'example'"
	_, progressingCond := newProgressingCondition(conditions.ReplicaSetUpdatedReason, r, "")
	invalidSpecCond := conditions.NewRolloutCondition(v1alpha1.InvalidSpec, corev1.ConditionTrue, conditions.InvalidSpecReason, errmsg)
	invalidSpecBytes, _ := json.Marshal(invalidSpecCond)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutSub, progressingCond, string(invalidSpecBytes), strings.ReplaceAll(errmsg, "\"", "\\\""))

	patch := f.getPatchedRollout(patchIndex)
	assert.JSONEq(t, calculatePatch(r, expectedPatch), patch)
}

func TestFailCreateBackgroundAnalysisRunIfMetricRepeated(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: pointer.Int32Ptr(10),
	}}

	at := analysisTemplate("bad-template")
	at.Spec.Metrics = append(at.Spec.Metrics, at.Spec.Metrics[0])
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)

	r := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				}, {
					TemplateName: at.Name,
				},
			},
		},
	}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r, at)

	patchIndex := f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))

	expectedPatchWithoutSub := `{
		"status": {
			"conditions": [%s,%s],
			"phase": "Degraded",
			"message": "InvalidSpec: %s"
		}
	}`
	errmsg := "The Rollout \"foo\" is invalid: spec.strategy.canary.analysis.templates: Invalid value: \"templateNames: [bad-template bad-template]\": two metrics have the same name 'example'"
	_, progressingCond := newProgressingCondition(conditions.ReplicaSetUpdatedReason, r, "")
	invalidSpecCond := conditions.NewRolloutCondition(v1alpha1.InvalidSpec, corev1.ConditionTrue, conditions.InvalidSpecReason, errmsg)
	invalidSpecBytes, _ := json.Marshal(invalidSpecCond)
	expectedPatch := fmt.Sprintf(expectedPatchWithoutSub, progressingCond, string(invalidSpecBytes), strings.ReplaceAll(errmsg, "\"", "\\\""))

	patch := f.getPatchedRollout(patchIndex)
	assert.JSONEq(t, calculatePatch(r, expectedPatch), patch)
}

func TestDoNothingWithAnalysisRunsWhileBackgroundAnalysisRunRunning(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		SetWeight: pointer.Int32Ptr(10),
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}
	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	ar.Status.Phase = v1alpha1.AnalysisPhaseRunning

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)
	r2.Status.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar.Name,
		Status: v1alpha1.AnalysisPhaseRunning,
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.objects = append(f.objects, r2, at, ar)

	f.expectUpdateReplicaSetAction(rs2)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	assert.JSONEq(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
}

func TestDoNothingWhileStepBasedAnalysisRunRunning(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r2)
	ar.Status.Phase = v1alpha1.AnalysisPhaseRunning

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)
	r2.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar.Name,
		Status: v1alpha1.AnalysisPhaseRunning,
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.objects = append(f.objects, r2, at, ar)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	assert.JSONEq(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
}

func TestCancelOlderAnalysisRuns(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r2)
	olderAr := ar.DeepCopy()
	olderAr.Name = "older-analysis-run"
	oldBackgroundAr := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	oldBackgroundAr.Name = "old-background-run"

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)
	r2.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar.Name,
		Status: "",
	}
	r2.Status.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   oldBackgroundAr.Name,
		Status: "",
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar, olderAr, oldBackgroundAr)
	f.objects = append(f.objects, r2, at, ar, olderAr, oldBackgroundAr)

	cancelBackgroundAr := f.expectPatchAnalysisRunAction(oldBackgroundAr)
	cancelOldAr := f.expectPatchAnalysisRunAction(olderAr)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	assert.True(t, f.verifyPatchedAnalysisRun(cancelBackgroundAr, oldBackgroundAr))
	assert.True(t, f.verifyPatchedAnalysisRun(cancelOldAr, olderAr))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentBackgroundAnalysisRunStatus":null
			}
		}
	}`
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
}

func TestDeleteAnalysisRunsWithNoMatchingRS(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r2)
	arWithDiffPodHash := ar.DeepCopy()
	arWithDiffPodHash.Name = "older-analysis-run"
	arWithDiffPodHash.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = "abc123"
	arWithDiffPodHash.Status = v1alpha1.AnalysisRunStatus{
		Phase: v1alpha1.AnalysisPhaseSuccessful,
	}
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)
	r2.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name: ar.Name,
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar, arWithDiffPodHash)
	f.objects = append(f.objects, r2, at, ar, arWithDiffPodHash)

	deletedIndex := f.expectDeleteAnalysisRunAction(arWithDiffPodHash)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	deletedAr := f.getDeletedAnalysisRun(deletedIndex)
	assert.Equal(t, deletedAr, arWithDiffPodHash.Name)
	patch := f.getPatchedRollout(patchIndex)
	assert.JSONEq(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
}

func TestDeleteAnalysisRunsAfterRSDelete(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	r3 := bumpVersion(r2)
	r3.Spec.RevisionHistoryLimit = pointer.Int32Ptr(0)
	ar := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r3)

	rs1 := newReplicaSetWithStatus(r1, 0, 0)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs3 := newReplicaSetWithStatus(r3, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2, rs3)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2, rs3)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	arToDelete := ar.DeepCopy()
	arToDelete.Name = "older-analysis-run"
	arToDelete.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = rs1PodHash
	arToDelete.Spec.Terminate = true
	arAlreadyDeleted := arToDelete.DeepCopy()
	arAlreadyDeleted.Name = "already-deleted-analysis-run"
	now := timeutil.MetaNow()
	arAlreadyDeleted.DeletionTimestamp = &now

	r3 = updateCanaryRolloutStatus(r3, rs2PodHash, 1, 0, 1, false)
	r3.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name: ar.Name,
	}

	f.rolloutLister = append(f.rolloutLister, r3)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar, arToDelete, arAlreadyDeleted)
	f.objects = append(f.objects, r3, at, ar, arToDelete, arAlreadyDeleted)

	f.expectDeleteReplicaSetAction(rs1)
	deletedIndex := f.expectDeleteAnalysisRunAction(arToDelete)
	f.expectPatchRolloutAction(r3)
	f.run(getKey(r3, t))

	deletedAr := f.getDeletedAnalysisRun(deletedIndex)
	assert.Equal(t, deletedAr, arToDelete.Name)
}

func TestIncrementStepAfterSuccessfulAnalysisRun(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r2)
	ar.Status = v1alpha1.AnalysisRunStatus{
		Phase: v1alpha1.AnalysisPhaseSuccessful,
	}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	r2.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name: ar.Name,
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.objects = append(f.objects, r2, at, ar)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentStepAnalysisRunStatus": null
			},
			"currentStepIndex": 1,
			"conditions": %s
		}
	}`
	condition := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, rs2, false, "", false)

	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, condition)), patch)
}

func TestPausedOnInconclusiveBackgroundAnalysisRun(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{
		{SetWeight: pointer.Int32Ptr(10)},
		{SetWeight: pointer.Int32Ptr(20)},
		{SetWeight: pointer.Int32Ptr(30)},
	}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}
	ar.Status = v1alpha1.AnalysisRunStatus{
		Phase: v1alpha1.AnalysisPhaseInconclusive,
	}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	r2.Status.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name: ar.Name,
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.objects = append(f.objects, r2, at, ar)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	now := timeutil.MetaNow().UTC().Format(time.RFC3339)
	expectedPatch := `{
		"status": {
			"conditions": %s,
			"canary": {
				"currentBackgroundAnalysisRunStatus": {
					"status": "Inconclusive"
				}
			},
			"pauseConditions": [{
					"reason": "%s",
					"startTime": "%s"
			}],
			"controllerPause": true,
			"phase": "Paused",
			"message": "%s"
		}
	}`
	condition := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false, "", false)

	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, condition, v1alpha1.PauseReasonInconclusiveAnalysis, now, v1alpha1.PauseReasonInconclusiveAnalysis)), patch)
}

func TestPausedStepAfterInconclusiveAnalysisRun(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r2)
	ar.Status = v1alpha1.AnalysisRunStatus{
		Phase: v1alpha1.AnalysisPhaseInconclusive,
	}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	r2.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name: ar.Name,
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.objects = append(f.objects, r2, at, ar)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	now := timeutil.MetaNow().UTC().Format(time.RFC3339)
	expectedPatch := `{
		"status": {
			"conditions": %s,
			"canary": {
				"currentStepAnalysisRunStatus": {
					"status": "Inconclusive"
				}
			},
			"pauseConditions": [{
					"reason": "%s",
					"startTime": "%s"
			}],
			"controllerPause": true,
			"phase": "Paused",
			"message": "%s"
		}
	}`
	condition := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false, "", false)
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, condition, v1alpha1.PauseReasonInconclusiveAnalysis, now, v1alpha1.PauseReasonInconclusiveAnalysis)), patch)
}

func TestErrorConditionAfterErrorAnalysisRunStep(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r2)
	ar.Status = v1alpha1.AnalysisRunStatus{
		Phase:   v1alpha1.AnalysisPhaseError,
		Message: "Error",
		MetricResults: []v1alpha1.MetricResult{{
			Phase: v1alpha1.AnalysisPhaseError,
		}},
	}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	r2.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name: ar.Name,
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.objects = append(f.objects, r2, at, ar)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"canary":{
				"currentStepAnalysisRunStatus": {
					"status": "Error",
					"message": "Error"
				}
			},
			"conditions": %s,
			"abort": true,
			"abortedAt": "%s",
			"phase": "Degraded",
			"message": "RolloutAborted: %s"
		}
	}`
	now := timeutil.MetaNow().UTC().Format(time.RFC3339)
	errmsg := fmt.Sprintf(conditions.RolloutAbortedMessage, 2) + ": " + ar.Status.Message
	condition := generateConditionsPatch(true, conditions.RolloutAbortedReason, r2, false, errmsg, false)
	expectedPatch = fmt.Sprintf(expectedPatch, condition, now, errmsg)
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
}

func TestErrorConditionAfterErrorAnalysisRunBackground(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{
		{SetWeight: pointer.Int32Ptr(10)},
		{SetWeight: pointer.Int32Ptr(20)},
		{SetWeight: pointer.Int32Ptr(40)},
	}

	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}

	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	ar.Status = v1alpha1.AnalysisRunStatus{
		Phase: v1alpha1.AnalysisPhaseError,
		MetricResults: []v1alpha1.MetricResult{{
			Phase: v1alpha1.AnalysisPhaseError,
		}},
	}

	r2.Status.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar.Name,
		Status: v1alpha1.AnalysisPhaseRunning,
	}

	rs1 := newReplicaSetWithStatus(r1, 9, 9)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 1, 10, false)
	r2.Status.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name: ar.Name,
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.objects = append(f.objects, r2, at, ar)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"canary":{
				"currentBackgroundAnalysisRunStatus": {
					"status": "Error"
				}
			},
			"conditions": %s,
			"abortedAt": "%s",
			"abort": true,
			"phase": "Degraded",
			"message": "RolloutAborted: %s"
		}
	}`
	errmsg := fmt.Sprintf(conditions.RolloutAbortedMessage, 2)
	condition := generateConditionsPatch(true, conditions.RolloutAbortedReason, r2, false, "", false)

	now := timeutil.Now().UTC().Format(time.RFC3339)
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, condition, now, errmsg)), patch)
}

func TestCancelAnalysisRunsWhenAborted(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r2)
	olderAr := ar.DeepCopy()
	olderAr.Name = "older-analysis-run"

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	r2.Status.Abort = true
	r2.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar.Name,
		Status: "",
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar, olderAr)
	f.objects = append(f.objects, r2, at, ar, olderAr)

	cancelCurrentAr := f.expectPatchAnalysisRunAction(ar)
	cancelOldAr := f.expectPatchAnalysisRunAction(olderAr)
	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	assert.True(t, f.verifyPatchedAnalysisRun(cancelOldAr, olderAr))
	assert.True(t, f.verifyPatchedAnalysisRun(cancelCurrentAr, ar))
	patch := f.getPatchedRollout(patchIndex)
	newConditions := generateConditionsPatch(true, conditions.RolloutAbortedReason, r2, false, "", false)
	expectedPatch := `{
		"status": {
			"conditions": %s,
			"abortedAt": "%s",
			"phase": "Degraded",
			"message": "RolloutAborted: %s"
		}
	}`
	errmsg := fmt.Sprintf(conditions.RolloutAbortedMessage, 2)
	now := timeutil.Now().UTC().Format(time.RFC3339)
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, newConditions, now, errmsg)), patch)
}

func TestCancelBackgroundAnalysisRunWhenRolloutIsCompleted(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{
		{SetWeight: pointer.Int32Ptr(10)},
	}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(1), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}
	ar := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r2)

	rs1 := newReplicaSetWithStatus(r1, 0, 0)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs2PodHash, 1, 1, 1, false)
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))
	r2.Status.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name: ar.Name,
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.objects = append(f.objects, r2, at, ar)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	assert.Contains(t, patch, `"currentBackgroundAnalysisRunStatus":null`)
}

func TestDoNotCreateBackgroundAnalysisRunAfterInconclusiveRun(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{
		{SetWeight: pointer.Int32Ptr(10)},
	}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(1), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2.Status.PauseConditions = []v1alpha1.PauseCondition{{
		Reason:    v1alpha1.PauseReasonInconclusiveAnalysis,
		StartTime: timeutil.MetaNow(),
	}}
	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)

	progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, r2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	pausedCondition, _ := newPausedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)

	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, r2, at)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	assert.JSONEq(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
}

func TestDoNotCreateBackgroundAnalysisRunOnNewCanaryRollout(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{
		{SetWeight: pointer.Int32Ptr(10)},
	}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}
	r1.Status.CurrentPodHash = ""
	rs1 := newReplicaSet(r1, 1)

	f.rolloutLister = append(f.rolloutLister, r1)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, r1, at)

	f.expectCreateReplicaSetAction(rs1)
	f.expectUpdateRolloutStatusAction(r1) // update conditions
	f.expectUpdateReplicaSetAction(rs1)   // scale replica set
	f.expectPatchRolloutAction(r1)
	f.run(getKey(r1, t))
}

// Same as TestDoNotCreateBackgroundAnalysisRunOnNewCanaryRollout but when Status.StableRS is ""
// https://github.com/argoproj/argo-rollouts/issues/721
func TestDoNotCreateBackgroundAnalysisRunOnNewCanaryRolloutStableRSEmpty(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{
		{SetWeight: pointer.Int32Ptr(10)},
	}

	r1 := newCanaryRollout("foo", 1, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
		},
	}
	r1.Status.StableRS = ""
	rs1 := newReplicaSet(r1, 1)

	f.rolloutLister = append(f.rolloutLister, r1)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, r1, at)

	f.expectCreateReplicaSetAction(rs1)
	f.expectUpdateRolloutStatusAction(r1) // update conditions
	f.expectUpdateReplicaSetAction(rs1)   // scale replica set
	f.expectPatchRolloutAction(r1)
	f.run(getKey(r1, t))
}

func TestCreatePrePromotionAnalysisRun(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: at.Name,
		}},
	}
	ar := analysisRun(at, v1alpha1.RolloutTypePrePromotionLabel, r2)
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 1, 1, 2, 1, true, true, false)
	progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, r2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	pausedCondition, _ := newPausedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)

	previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	previewSvc := newService("preview", 80, previewSelector, r2)
	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)

	f.objects = append(f.objects, r2, at)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	f.expectCreateAnalysisRunAction(ar)
	patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := fmt.Sprintf(`{
		"status": {
			"blueGreen": {
				"prePromotionAnalysisRunStatus": {
					"name": "%s",
					"status": ""
				}
			}
		}
	}`, ar.Name)
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
}

// TestDoNotCreatePrePromotionAnalysisProgressedRollout ensures a pre-promotion analysis is not created after a Rollout
// points the active service at the new ReplicaSet
func TestDoNotCreatePrePromotionAnalysisAfterPromotionRollout(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "bar", "")
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: "test",
		}},
	}

	rs2 := newReplicaSetWithStatus(r2, 1, 1)

	f.kubeobjects = append(f.kubeobjects, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs2)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	serviceSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	s := newService("bar", 80, serviceSelector, r2)
	f.kubeobjects = append(f.kubeobjects, s)

	at := analysisTemplate("test")
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, at)

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs2PodHash, 1, 1, 1, 1, false, true, true)
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.serviceLister = append(f.serviceLister, s)

	patchIndex := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))

	newConditions := generateConditionsPatchWithHealthy(true, conditions.NewRSAvailableReason, rs2, true, "", true, true)
	expectedPatch := fmt.Sprintf(`{
		"status":{
			"conditions":%s
		}
	}`, newConditions)
	patch := f.getPatchedRollout(patchIndex)
	assert.Equal(t, cleanPatch(expectedPatch), patch)

}

// TestDoNotCreatePrePromotionAnalysisRunOnNewRollout ensures that a pre-promotion analysis is not created
// if the Rollout does not have a stable ReplicaSet
func TestDoNotCreatePrePromotionAnalysisRunOnNewRollout(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r := newBlueGreenRollout("foo", 1, nil, "active", "")
	r.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: "test",
		}},
	}
	r.Status.Conditions = []v1alpha1.RolloutCondition{}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)
	activeSvc := newService("active", 80, nil, r)
	f.kubeobjects = append(f.kubeobjects, activeSvc)
	f.serviceLister = append(f.serviceLister, activeSvc)
	at := analysisTemplate("test")
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, at)

	rs := newReplicaSet(r, 1)

	f.expectCreateReplicaSetAction(rs)
	f.expectUpdateRolloutStatusAction(r)
	f.expectUpdateReplicaSetAction(rs) // scale RS
	f.expectPatchRolloutAction(r)
	f.run(getKey(r, t))
}

// TestDoNotCreatePrePromotionAnalysisRunOnNotReadyReplicaSet ensures that a pre-promotion analysis is not created until
// the new ReplicaSet is saturated
func TestDoNotCreatePrePromotionAnalysisRunOnNotReadyReplicaSet(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 2, nil, "active", "preview")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: "test",
		}},
	}

	rs1 := newReplicaSetWithStatus(r1, 2, 2)
	rs2 := newReplicaSetWithStatus(r2, 2, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 2, 2, 4, 2, false, true, false)

	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)
	previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	previewSvc := newService("preview", 80, previewSelector, r2)
	at := analysisTemplate("test")

	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, activeSvc, previewSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, at)

	patchRolloutIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchRolloutIndex)
	assert.JSONEq(t, calculatePatch(r2, OnlyObservedGenerationPatch), patch)
}

func TestRolloutPrePromotionAnalysisBecomesInconclusive(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: at.Name,
		}},
	}
	ar := analysisRun(at, v1alpha1.RolloutTypePrePromotionLabel, r2)
	ar.Status.Phase = v1alpha1.AnalysisPhaseInconclusive

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, rs1PodHash, 1, 1, 2, 1, true, true, false)
	r2.Status.BlueGreen.PrePromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar.Name,
		Status: v1alpha1.AnalysisPhaseRunning,
	}
	progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, r2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	pausedCondition, _ := newPausedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)

	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)

	f.objects = append(f.objects, r2, at, ar)
	f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc)

	patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	now := timeutil.MetaNow().UTC().Format(time.RFC3339)
	expectedPatch := fmt.Sprintf(`{
		"status": {
			"pauseConditions":[
				{
					"reason": "BlueGreenPause",
					"startTime": "%s"
				},{
					"reason": "InconclusiveAnalysisRun",
					"startTime": "%s"
				}
			],
			"blueGreen": {
				"prePromotionAnalysisRunStatus": {
					"status": "Inconclusive"
				}
			}
		}
	}`, now, now)
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
}

func TestRolloutPrePromotionAnalysisSwitchServiceAfterSuccess(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(true)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: at.Name,
		}},
	}
	ar := analysisRun(at, v1alpha1.RolloutTypePrePromotionLabel, r2)
	ar.Status.Phase = v1alpha1.AnalysisPhaseSuccessful

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, rs1PodHash, 1, 1, 2, 1, true, true, false)
	r2.Status.BlueGreen.PrePromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar.Name,
		Status: v1alpha1.AnalysisPhaseRunning,
	}
	progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, r2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	pausedCondition, _ := newPausedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)

	f.objects = append(f.objects, r2, at, ar)
	f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc)

	f.expectPatchServiceAction(activeSvc, rs2PodHash)
	patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
	f.run(getKey(r2, t))
	patch := f.getPatchedRolloutWithoutConditions(patchIndex)
	expectedPatch := fmt.Sprintf(`{
		"status": {
			"blueGreen": {
				"activeSelector": "%s",
				"prePromotionAnalysisRunStatus":{"status":"Successful"}
			},
			"stableRS": "%s",
			"pauseConditions": null,
			"controllerPause": null,
			"selector":"foo=bar,rollouts-pod-template-hash=%s",
			"phase": "Healthy",
			"message": null
		}
	}`, rs2PodHash, rs2PodHash, rs2PodHash)
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
}

func TestRolloutPrePromotionAnalysisHonorAutoPromotionSeconds(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(true)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.AutoPromotionSeconds = 10
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: at.Name,
		}},
	}
	ar := analysisRun(at, v1alpha1.RolloutTypePrePromotionLabel, r2)
	ar.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
	r2.Status.BlueGreen.PrePromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar.Name,
		Status: v1alpha1.AnalysisPhaseSuccessful,
	}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, rs1PodHash, 1, 1, 2, 1, true, true, false)
	now := metav1.NewTime(timeutil.MetaNow().Add(-10 * time.Second))
	r2.Status.PauseConditions[0].StartTime = now
	progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, r2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	pausedCondition, _ := newPausedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)

	f.objects = append(f.objects, r2, at, ar)
	f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc)

	f.expectPatchServiceAction(activeSvc, rs2PodHash)
	patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
	f.run(getKey(r2, t))
	patch := f.getPatchedRolloutWithoutConditions(patchIndex)
	expectedPatch := fmt.Sprintf(`{
		"status": {
			"blueGreen": {
				"activeSelector": "%s"
			},
			"stableRS": "%s",
			"pauseConditions": null,
			"controllerPause": null,
			"selector":"foo=bar,rollouts-pod-template-hash=%s",
			"phase": "Healthy",
			"message": null
		}
	}`, rs2PodHash, rs2PodHash, rs2PodHash)
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
}

func TestRolloutPrePromotionAnalysisDoNothingOnInconclusiveAnalysis(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: at.Name,
		}},
	}
	ar := analysisRun(at, v1alpha1.RolloutTypePrePromotionLabel, r2)
	ar.Status.Phase = v1alpha1.AnalysisPhaseInconclusive
	r2.Status.BlueGreen.PrePromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name: ar.Name,
	}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, rs1PodHash, 1, 1, 2, 1, true, true, false)
	inconclusivePauseCondition := v1alpha1.PauseCondition{
		Reason:    v1alpha1.PauseReasonInconclusiveAnalysis,
		StartTime: timeutil.MetaNow(),
	}
	r2.Status.PauseConditions = append(r2.Status.PauseConditions, inconclusivePauseCondition)
	r2.Status.ObservedGeneration = strconv.Itoa(int(r2.Generation))
	progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, r2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	pausedCondition, _ := newPausedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)

	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)

	f.objects = append(f.objects, r2, at, ar)
	f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc)

	f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
	f.run(getKey(r2, t))
}

func TestAbortRolloutOnErrorPrePromotionAnalysis(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = pointer.BoolPtr(false)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: at.Name,
		}},
	}
	ar := analysisRun(at, v1alpha1.RolloutTypePrePromotionLabel, r2)
	ar.Status.Phase = v1alpha1.AnalysisPhaseError
	r2.Status.BlueGreen.PrePromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar.Name,
		Status: v1alpha1.AnalysisPhaseRunning,
	}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, "", rs1PodHash, rs1PodHash, 1, 1, 2, 1, true, true, false)
	progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, r2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	pausedCondition, _ := newPausedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)
	r2.Status.Phase, r2.Status.Message = rolloututil.CalculateRolloutPhase(r2.Spec, r2.Status)

	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)

	f.objects = append(f.objects, r2, at, ar)
	f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc)

	patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"abort": true,
			"abortedAt": "%s",
			"pauseConditions": null,
			"conditions": %s,
			"controllerPause":null,
			"blueGreen": {
				"prePromotionAnalysisRunStatus": {
					"status": "Error"
				}
			},
			"phase": "Degraded",
			"message": "%s: %s"
		}
	}`
	now := timeutil.MetaNow().UTC().Format(time.RFC3339)
	progressingFalseAborted, _ := newProgressingCondition(conditions.RolloutAbortedReason, r2, "")
	newConditions := updateConditionsPatch(*r2, progressingFalseAborted)
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, now, newConditions, conditions.RolloutAbortedReason, progressingFalseAborted.Message)), patch)
}

func TestCreatePostPromotionAnalysisRun(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PostPromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: at.Name,
		}},
	}
	ar := analysisRun(at, v1alpha1.RolloutTypePostPromotionLabel, r2)
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs1PodHash, 1, 1, 2, 1, false, true, false)

	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)

	f.objects = append(f.objects, r2, at)
	f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc)

	f.expectCreateAnalysisRunAction(ar)
	patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := fmt.Sprintf(`{
		"status": {
			"blueGreen": {
				"postPromotionAnalysisRunStatus":{
					"name": "%s", 
					"status": ""
				}
			}
		}
	}`, ar.Name)
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
}

func TestRolloutPostPromotionAnalysisSuccess(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PostPromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: at.Name,
		}},
	}
	ar := analysisRun(at, v1alpha1.RolloutTypePostPromotionLabel, r2)
	ar.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
	r2.Status.BlueGreen.PostPromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar.Name,
		Status: v1alpha1.AnalysisPhaseRunning,
	}

	rs1 := newReplicaSetWithStatus(r1, 0, 0)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs1PodHash, 1, 1, 1, 1, false, true, false)

	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)

	cond, _ := newCompletedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, cond)

	f.objects = append(f.objects, r2, at, ar)
	f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc)

	patchIndex := f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := fmt.Sprintf(`{
		"status": {
			"stableRS": "%s",
			"blueGreen": {
				"postPromotionAnalysisRunStatus":{"status":"Successful"}
			},
			"phase": "Healthy",
			"message": null
		}
	}`, rs2PodHash)
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
}

// TestPostPromotionAnalysisRunHandleInconclusive ensures that the Rollout does not scale down a old ReplicaSet if
// it's paused for a inconclusive analysis run
func TestPostPromotionAnalysisRunHandleInconclusive(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PostPromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: at.Name,
		}},
	}
	ar := analysisRun(at, v1alpha1.RolloutTypePostPromotionLabel, r2)
	ar.Status.Phase = v1alpha1.AnalysisPhaseInconclusive
	r2.Status.BlueGreen.PostPromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name: ar.Name,
	}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs1PodHash, 1, 1, 2, 1, false, true, false)
	r2.Status.PauseConditions = []v1alpha1.PauseCondition{{
		Reason:    v1alpha1.PauseReasonInconclusiveAnalysis,
		StartTime: timeutil.MetaNow(),
	}}
	progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, r2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	pausedCondition, _ := newPausedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)

	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)

	f.objects = append(f.objects, r2, at, ar)
	f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc)

	patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := fmt.Sprint(`{
		"status": {
			"blueGreen": {
				"postPromotionAnalysisRunStatus": {"status":"Inconclusive"}
			},
			"phase": "Paused",
			"message": "InconclusiveAnalysisRun"
		}
	}`)
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
}

func TestAbortRolloutOnErrorPostPromotionAnalysis(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PostPromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.RolloutAnalysisTemplate{{
			TemplateName: at.Name,
		}},
	}
	ar := analysisRun(at, v1alpha1.RolloutTypePostPromotionLabel, r2)
	ar.Status.Phase = v1alpha1.AnalysisPhaseError
	r2.Status.BlueGreen.PostPromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar.Name,
		Status: v1alpha1.AnalysisPhaseRunning,
	}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, "", rs2PodHash, rs1PodHash, 1, 1, 2, 1, true, true, false)
	progressingCondition, _ := newProgressingCondition(conditions.RolloutPausedReason, r2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)

	pausedCondition, _ := newPausedCondition(true)
	conditions.SetRolloutCondition(&r2.Status, pausedCondition)

	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)

	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)

	f.objects = append(f.objects, r2, at, ar)
	f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc)

	patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)
	expectedPatch := `{
		"status": {
			"abort": true,
			"abortedAt": "%s",
			"pauseConditions": null,
			"conditions": %s,
			"controllerPause":null,
			"blueGreen": {
				"postPromotionAnalysisRunStatus": {
					"status": "Error"
				}
			},
			"phase": "Degraded",
			"message": "%s: %s"
		}
	}`
	now := timeutil.MetaNow().UTC().Format(time.RFC3339)
	progressingFalseAborted, _ := newProgressingCondition(conditions.RolloutAbortedReason, r2, "")
	newConditions := updateConditionsPatch(*r2, progressingFalseAborted)
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, now, newConditions, conditions.RolloutAbortedReason, progressingFalseAborted.Message)), patch)
}

func TestCreateAnalysisRunWithCustomAnalysisRunMetadataAndROCopyLabels(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	at := analysisTemplate("bar")
	r1 := newCanaryRollout("foo", 10, nil, steps, pointer.Int32Ptr(0), intstr.FromInt(0), intstr.FromInt(1))
	r1.ObjectMeta.Labels = make(map[string]string)
	r1.Spec.Selector.MatchLabels["my-label"] = "1234"
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.RolloutAnalysisTemplate{
				{
					TemplateName: at.Name,
				},
			},
			AnalysisRunMetadata: v1alpha1.AnalysisRunMetadata{
				Annotations: map[string]string{"testAnnotationKey": "testAnnotationValue"},
				Labels:      map[string]string{"testLabelKey": "testLabelValue"},
			},
		},
	}

	rs1 := newReplicaSetWithStatus(r1, 10, 10)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 10, 0, 10, false)
	_, _ = newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, r2, at)

	createdIndex := f.expectCreateAnalysisRunAction(ar)
	f.expectUpdateReplicaSetAction(rs2)
	_ = f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdAr := f.getCreatedAnalysisRun(createdIndex)
	expectedArName := fmt.Sprintf("%s-%s-%s", r2.Name, rs2PodHash, "2")
	assert.Equal(t, expectedArName, createdAr.Name)
	assert.Equal(t, "testAnnotationValue", createdAr.Annotations["testAnnotationKey"])
	assert.Equal(t, "testLabelValue", createdAr.Labels["testLabelKey"])
	assert.Equal(t, "1234", createdAr.Labels["my-label"])
}
