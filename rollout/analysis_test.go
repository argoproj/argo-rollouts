package rollout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/utils/hash"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	core "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/typed/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/retry"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
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

func analysisTemplateWithNamespacedAnalysisRefs(name string, innerRefsName ...string) *v1alpha1.AnalysisTemplate {
	return analysisTemplateWithAnalysisRefs(name, false, innerRefsName...)
}

func analysisTemplateWithClusterAnalysisRefs(name string, innerRefsName ...string) *v1alpha1.AnalysisTemplate {
	return analysisTemplateWithAnalysisRefs(name, true, innerRefsName...)
}

func analysisTemplateWithAnalysisRefs(name string, clusterScope bool, innerRefsName ...string) *v1alpha1.AnalysisTemplate {
	templatesRefs := []v1alpha1.AnalysisTemplateRef{}
	for _, innerTplName := range innerRefsName {
		templatesRefs = append(templatesRefs, v1alpha1.AnalysisTemplateRef{
			TemplateName: innerTplName,
			ClusterScope: &clusterScope,
		})
	}
	return &v1alpha1.AnalysisTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{{
				Name: "example-" + name,
			}},
			DryRun: []v1alpha1.DryRun{{
				MetricName: "example-" + name,
			}},
			MeasurementRetention: []v1alpha1.MeasurementRetention{{
				MetricName: "example-" + name,
			}},
			Templates: templatesRefs,
		},
	}
}

func analysisTemplateWithOnlyNamespacedAnalysisRefs(name string, innerRefsName ...string) *v1alpha1.AnalysisTemplate {
	return analysisTemplateWithOnlyRefs(name, false, innerRefsName...)
}

func analysisTemplateWithOnlyRefs(name string, clusterScope bool, innerRefsName ...string) *v1alpha1.AnalysisTemplate {
	templatesRefs := []v1alpha1.AnalysisTemplateRef{}
	for _, innerTplName := range innerRefsName {
		templatesRefs = append(templatesRefs, v1alpha1.AnalysisTemplateRef{
			TemplateName: innerTplName,
			ClusterScope: &clusterScope,
		})
	}
	return &v1alpha1.AnalysisTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics:              []v1alpha1.Metric{},
			DryRun:               []v1alpha1.DryRun{},
			MeasurementRetention: []v1alpha1.MeasurementRetention{},
			Templates:            templatesRefs,
		},
	}
}

func clusterAnalysisTemplate(name string, metricName string) *v1alpha1.ClusterAnalysisTemplate {
	return &v1alpha1.ClusterAnalysisTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{{
				Name: metricName,
			}},
		},
	}
}

func clusterAnalysisTemplateWithAnalysisRefs(name string, innerRefsName ...string) *v1alpha1.ClusterAnalysisTemplate {
	templatesRefs := []v1alpha1.AnalysisTemplateRef{}
	for _, innerTplName := range innerRefsName {
		templatesRefs = append(templatesRefs, v1alpha1.AnalysisTemplateRef{
			TemplateName: innerTplName,
			ClusterScope: ptr.To(true),
		})
	}
	return &v1alpha1.ClusterAnalysisTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics: []v1alpha1.Metric{{
				Name: "clusterexample-" + name,
			}},
			Templates: templatesRefs,
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
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
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
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{{
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
	cat := clusterAnalysisTemplate("bar", "clusterexample")
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := clusterAnalysisRun(cat, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{{
				TemplateName: cat.Name,
				ClusterScope: ptr.To(true),
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

	r := newCanaryRollout("foo", 10, nil, nil, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{{
				TemplateName: "missing",
				ClusterScope: ptr.To(true),
			}},
		},
	}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r)

	patchIndex := f.expectPatchRolloutAction(r)
	f.runExpectError(getKey(r, t), true)

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
	cat := clusterAnalysisTemplate("clusterbar", "clusterexample")
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
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
			Templates: []v1alpha1.AnalysisTemplateRef{{
				TemplateName: cat.Name,
				ClusterScope: ptr.To(true),
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

func TestCreateBackgroundAnalysisRunWithClusterTemplatesAndTemplateAndInnerTemplates(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	at := analysisTemplateWithNamespacedAnalysisRefs("bar", "bar2")
	at2 := analysisTemplateWithClusterAnalysisRefs("bar2", "clusterbar2", "clusterbar4")
	cat := clusterAnalysisTemplateWithAnalysisRefs("clusterbar", "clusterbar2", "clusterbar3")
	cat2 := clusterAnalysisTemplate("clusterbar2", "clusterexample-clusterbar2")
	cat3 := clusterAnalysisTemplate("clusterbar3", "clusterexample-clusterbar3")
	cat4 := clusterAnalysisTemplate("clusterbar4", "clusterexample-clusterbar4")
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	ar := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "run1",
			Namespace:       metav1.NamespaceDefault,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(r1, controllerKind)},
		},
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: concatMultipleSlices([][]v1alpha1.Metric{at.Spec.Metrics, at2.Spec.Metrics, cat.Spec.Metrics, cat2.Spec.Metrics, cat3.Spec.Metrics, cat4.Spec.Metrics}),
			Args:    at.Spec.Args,
		},
	}
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{{
				TemplateName: cat.Name,
				ClusterScope: ptr.To(true),
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
	f.clusterAnalysisTemplateLister = append(f.clusterAnalysisTemplateLister, cat, cat2, cat3, cat4)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at, at2)
	f.objects = append(f.objects, r2, cat, at, at2, cat2, cat3, cat4)

	createdIndex := f.expectCreateAnalysisRunAction(ar)
	f.expectUpdateReplicaSetAction(rs2)
	index := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdAr := f.getCreatedAnalysisRun(createdIndex)
	expectedArName := fmt.Sprintf("%s-%s-%s", r2.Name, rs2PodHash, "2")
	assert.Equal(t, expectedArName, createdAr.Name)
	assert.Len(t, createdAr.Spec.Metrics, 6)

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

// Test the case where the analysis template does't have metrics, but refences other templates
func TestCreateBackgroundAnalysisRunWithTemplatesAndNoMetrics(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	at := analysisTemplateWithOnlyNamespacedAnalysisRefs("bar", "bar2")
	at2 := analysisTemplateWithClusterAnalysisRefs("bar2", "clusterbar2", "clusterbar4")
	cat := clusterAnalysisTemplateWithAnalysisRefs("clusterbar", "clusterbar2", "clusterbar3")
	cat2 := clusterAnalysisTemplate("clusterbar2", "clusterexample-clusterbar2")
	cat3 := clusterAnalysisTemplate("clusterbar3", "clusterexample-clusterbar3")
	cat4 := clusterAnalysisTemplate("clusterbar4", "clusterexample-clusterbar4")
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)

	ar := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "run1",
			Namespace:       metav1.NamespaceDefault,
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(r1, controllerKind)},
		},
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: concatMultipleSlices([][]v1alpha1.Metric{at.Spec.Metrics, at2.Spec.Metrics, cat.Spec.Metrics, cat2.Spec.Metrics, cat3.Spec.Metrics, cat4.Spec.Metrics}),
			Args:    at.Spec.Args,
		},
	}
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{{
				TemplateName: cat.Name,
				ClusterScope: ptr.To(true),
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
	f.clusterAnalysisTemplateLister = append(f.clusterAnalysisTemplateLister, cat, cat2, cat3, cat4)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at, at2)
	f.objects = append(f.objects, r2, cat, at, at2, cat2, cat3, cat4)

	createdIndex := f.expectCreateAnalysisRunAction(ar)
	f.expectUpdateReplicaSetAction(rs2)
	index := f.expectPatchRolloutAction(r1)

	f.run(getKey(r2, t))
	createdAr := f.getCreatedAnalysisRun(createdIndex)
	expectedArName := fmt.Sprintf("%s-%s-%s", r2.Name, rs2PodHash, "2")
	assert.Equal(t, expectedArName, createdAr.Name)
	assert.Len(t, createdAr.Spec.Metrics, 5)

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
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
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
	// rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

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
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
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
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
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

func TestCreateAnalysisRunOnPromotedAnalysisStepIfPreviousStepWasAnalysisToo(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
		},
	}, {
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar0Step := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r2)
	ar0Step.Status.Phase = v1alpha1.AnalysisPhaseRunning

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)
	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	// rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)
	progressingCondition, _ := newProgressingCondition(conditions.ReplicaSetUpdatedReason, rs2, "")
	conditions.SetRolloutCondition(&r2.Status, progressingCondition)
	availableCondition, _ := newAvailableCondition(true)
	conditions.SetRolloutCondition(&r2.Status, availableCondition)
	completedCondition, _ := newCompletedCondition(false)
	conditions.SetRolloutCondition(&r2.Status, completedCondition)
	r2.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar0Step.Name,
		Status: "",
	}

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar0Step)
	f.objects = append(f.objects, r2, at, ar0Step)

	patchOldAnalysisIndex := f.expectPatchAnalysisRunAction(ar0Step)
	// createdIndex := f.expectCreateAnalysisRunAction(ar0Step)
	index := f.expectPatchRolloutAction(r2)

	// simulate promote action
	r2.Status.CurrentStepIndex = ptr.To[int32](1)

	f.run(getKey(r2, t))

	assert.True(t, f.verifyPatchedAnalysisRun(patchOldAnalysisIndex, ar0Step))
	// should terminate analysis run for old step
	patchedOldAr := f.getPatchedAnalysisRun(patchOldAnalysisIndex)
	assert.True(t, patchedOldAr.Spec.Terminate)

	// should patch rollout with relevant currentStepAnalysisRun
	patch := f.getPatchedRollout(index)
	expectedPatch := `{
		"status": {
			"canary": {
				"currentStepAnalysisRunStatus":null
			}
		}
	}`
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
}

func TestFailCreateStepAnalysisRunIfInvalidTemplateRef(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: "bad-template",
				},
			},
		},
	}}

	at := analysisTemplate("bad-template")
	at.Spec.Metrics = append(at.Spec.Metrics, at.Spec.Metrics[0])
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)

	r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r, at)

	patchIndex := f.expectPatchRolloutAction(r)
	f.runExpectError(getKey(r, t), true)

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
		SetWeight: ptr.To[int32](10),
	}}

	at := analysisTemplate("bad-template")
	at.Spec.Metrics = append(at.Spec.Metrics, at.Spec.Metrics[0])
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)

	r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: "bad-template",
				},
			},
		},
	}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r, at)

	patchIndex := f.expectPatchRolloutAction(r)
	f.runExpectError(getKey(r, t), true)

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
		SetWeight: ptr.To[int32](10),
	}}

	at := analysisTemplate("bad-template")
	at2 := analysisTemplate("bad-template-2")
	at.Spec.Metrics = append(at.Spec.Metrics, at.Spec.Metrics[0])
	at2.Spec.Metrics = append(at2.Spec.Metrics, at2.Spec.Metrics[0])
	f.analysisTemplateLister = append(f.analysisTemplateLister, at, at2)

	r := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				}, {
					TemplateName: at2.Name,
				},
			},
		},
	}
	f.rolloutLister = append(f.rolloutLister, r)
	f.objects = append(f.objects, r, at, at2)

	patchIndex := f.expectPatchRolloutAction(r)
	f.runExpectError(getKey(r, t), true)

	expectedPatchWithoutSub := `{
		"status": {
			"conditions": [%s,%s],
			"phase": "Degraded",
			"message": "InvalidSpec: %s"
		}
	}`
	errmsg := "The Rollout \"foo\" is invalid: spec.strategy.canary.analysis.templates: Invalid value: \"templateNames: [bad-template bad-template-2]\": two metrics have the same name 'example'"
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
		SetWeight: ptr.To[int32](10),
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
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
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(1))
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
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
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
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
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
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	r3 := bumpVersion(r2)
	r3.Spec.RevisionHistoryLimit = ptr.To[int32](0)
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
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
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
		{SetWeight: ptr.To[int32](10)},
		{SetWeight: ptr.To[int32](20)},
		{SetWeight: ptr.To[int32](30)},
	}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
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
			"message": "%s",
			"duration": {
				"manualPauseStartedAt": "%s"
			}
		}
	}`
	condition := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false, "", false)

	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, condition, v1alpha1.PauseReasonInconclusiveAnalysis, now, v1alpha1.PauseReasonInconclusiveAnalysis, now)), patch)
}

func TestPausedStepAfterInconclusiveAnalysisRun(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
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
			"message": "%s",
			"duration": {
				"manualPauseStartedAt": "%s"
			}
		}
	}`
	condition := generateConditionsPatch(true, conditions.ReplicaSetUpdatedReason, r2, false, "", false)
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, condition, v1alpha1.PauseReasonInconclusiveAnalysis, now, v1alpha1.PauseReasonInconclusiveAnalysis, now)), patch)
}

func TestErrorConditionAfterErrorAnalysisRunStep(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
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
			"message": "RolloutAborted: %s",
			"duration": {
				"completionStatus": "Aborted",
				"finishedAt": "%s"
			}
		}
	}`
	now := timeutil.MetaNow().UTC().Format(time.RFC3339)
	errmsg := "Step-based analysis phase error/failed: " + ar.Status.Message
	condition := generateConditionsPatch(true, conditions.RolloutAbortedReason, r2, false, errmsg, false)
	expectedPatch = fmt.Sprintf(expectedPatch, condition, now, fmt.Sprintf(conditions.RolloutAbortedMessage, 2)+": "+errmsg, now)
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
	f.metricsRecorder.AssertNumberOfCalls(t, "EmitRolloutDuration", 1)
}

func TestErrorConditionAfterErrorAnalysisRunBackground(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{
		{SetWeight: ptr.To[int32](10)},
		{SetWeight: ptr.To[int32](20)},
		{SetWeight: ptr.To[int32](40)},
	}

	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
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
			"message": "RolloutAborted: %s",
			"duration": {
				"completionStatus": "Aborted",
				"finishedAt": "%s"
			}
		}
	}`
	errmsg := fmt.Sprintf(conditions.RolloutAbortedMessage, 2) + ": Background analysis phase error/failed"
	condition := generateConditionsPatch(true, conditions.RolloutAbortedReason, r2, false, "Background analysis phase error/failed", false)

	now := timeutil.Now().UTC().Format(time.RFC3339)
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, condition, now, errmsg, now)), patch)
	f.metricsRecorder.AssertNumberOfCalls(t, "EmitRolloutDuration", 1)
}

func TestCancelAnalysisRunsWhenAborted(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{{
		Analysis: &v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
		},
	}}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
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
			"canary": {
				"currentStepAnalysisRunStatus": null
			},
			"message": "RolloutAborted: %s",
			"duration": {
				"completionStatus": "Aborted",
				"finishedAt": "%s"
			}
		}
	}`
	errmsg := fmt.Sprintf(conditions.RolloutAbortedMessage, 2)
	now := timeutil.Now().UTC().Format(time.RFC3339)
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, newConditions, now, errmsg, now)), patch)
	f.metricsRecorder.AssertNumberOfCalls(t, "EmitRolloutDuration", 1)
}

func TestCancelBackgroundAnalysisRunWhenRolloutIsCompleted(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{
		{SetWeight: ptr.To[int32](10)},
	}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](1), intstr.FromInt(0), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
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

	f.expectPatchAnalysisRunAction(ar)           // terminate the AR
	patchIndex := f.expectPatchRolloutAction(r2) // patch status
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)
	assert.Contains(t, patch, `"currentBackgroundAnalysisRunStatus":null`)
}

func TestDoNotCreateBackgroundAnalysisRunAfterInconclusiveRun(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{
		{SetWeight: ptr.To[int32](10)},
	}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(1))
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
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
	r2.Status.Duration.ManualPauseStartedAt = ptr.To(timeutil.MetaNow())
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
		{SetWeight: ptr.To[int32](10)},
	}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
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

	f.expectCreateReplicaSetAction(rs1)   // create replica set
	f.expectUpdateRolloutStatusAction(r1) // update conditions
	f.expectGetRolloutAction(r1)          // second reconciliation
	f.expectUpdateReplicaSetAction(rs1)   // scale replica set
	f.expectPatchRolloutAction(r1)        // patch status
	f.runWithSyncs(getKey(r1, t), 2)
}

// Same as TestDoNotCreateBackgroundAnalysisRunOnNewCanaryRollout but when Status.StableRS is ""
// https://github.com/argoproj/argo-rollouts/issues/721
func TestDoNotCreateBackgroundAnalysisRunOnNewCanaryRolloutStableRSEmpty(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{
		{SetWeight: ptr.To[int32](10)},
	}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
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

	f.expectCreateReplicaSetAction(rs1)   // create replica set
	f.expectUpdateRolloutStatusAction(r1) // update conditions
	f.expectGetRolloutAction(r1)          // second reconciliation
	f.expectUpdateReplicaSetAction(rs1)   // scale replica set
	f.expectPatchRolloutAction(r1)        // patch status
	f.runWithSyncs(getKey(r1, t), 2)
}

func TestDoNotCreateBackgroundAnalysisRunWhenWithinRollbackWindow(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")

	r1 := newCanaryRollout("foo", 1, nil, nil, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
		},
	}
	r1.Spec.RollbackWindow = &v1alpha1.RollbackWindowSpec{Revisions: 1}

	r2 := bumpVersion(r1)
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 0, 0)

	rs2.CreationTimestamp = timeutil.MetaTime(time.Now().Add(-1 * time.Hour))
	rs1.CreationTimestamp = timeutil.MetaNow()

	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)

	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateCanaryRolloutStatus(r2, rs1PodHash, 1, 0, 1, false)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.objects = append(f.objects, r2, at)

	f.expectUpdateReplicaSetAction(rs2)
	f.expectPatchRolloutAction(r2)
	f.run(getKey(r2, t))
}

type retryRolloutRequest func(clientset.RolloutInterface, string) (*v1alpha1.Rollout, error)

func TestCanaryAnalysisRetry(t *testing.T) {
	testCanaryAnalysisRetry(t, retry.RetryRollout)
}

func testCanaryAnalysisRetry(t *testing.T, requestRetry retryRolloutRequest, onlyCases ...string) {
	t.Helper()
	tests := []struct {
		name           string
		step           bool
		background     bool
		phase          v1alpha1.AnalysisPhase
		scaling        bool
		scalingFailure bool
		statusFailure  bool
		pause          bool
	}{
		{name: "StepFailed", step: true, phase: v1alpha1.AnalysisPhaseFailed},
		{name: "StepErrorDuringScaling", step: true, phase: v1alpha1.AnalysisPhaseError, scaling: true},
		{name: "BackgroundError", background: true, phase: v1alpha1.AnalysisPhaseError},
		{name: "BackgroundFailedDuringScaling", background: true, phase: v1alpha1.AnalysisPhaseFailed, scaling: true},
		{name: "StepAndBackgroundDuringScaling", step: true, background: true, phase: v1alpha1.AnalysisPhaseFailed, scaling: true},
		{name: "TerminatingStepDuringScaling", step: true, phase: v1alpha1.AnalysisPhaseRunning, scaling: true},
		{name: "TerminatingBackground", background: true, phase: v1alpha1.AnalysisPhaseRunning},
		{name: "ScalingFailure", step: true, background: true, phase: v1alpha1.AnalysisPhaseError, scaling: true, scalingFailure: true},
		{name: "StatusFailure", step: true, background: true, phase: v1alpha1.AnalysisPhaseFailed, statusFailure: true},
		{name: "PauseAndStartingStep", step: true, background: true, phase: v1alpha1.AnalysisPhaseError, scaling: true, pause: true},
	}
	for _, tt := range tests {
		if len(onlyCases) > 0 && !slices.Contains(onlyCases, tt.name) {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			defer f.Close()
			ctx := context.Background()
			at := analysisTemplate("retry-metric")
			analysis := v1alpha1.RolloutAnalysis{Templates: []v1alpha1.AnalysisTemplateRef{{TemplateName: at.Name}}}
			steps := []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](50)}}
			if tt.step {
				steps = []v1alpha1.CanaryStep{{Analysis: &analysis}}
			}
			if tt.pause {
				steps = append([]v1alpha1.CanaryStep{{Pause: &v1alpha1.RolloutPause{}}}, steps...)
			}
			r1 := newCanaryRollout("retry-canary", 1, nil, steps, ptr.To[int32](0), intstr.FromInt(1), intstr.FromInt(0))
			r2 := bumpVersion(r1)
			if tt.background {
				r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{RolloutAnalysis: analysis}
				if tt.pause {
					r2.Spec.Strategy.Canary.Analysis.StartingStep = ptr.To[int32](1)
				}
			}
			rs1 := newReplicaSetWithStatus(r1, 1, 1)
			rs2 := newReplicaSetWithStatus(r2, 0, 0)
			if tt.scaling {
				rs1.Annotations[annotations.DesiredReplicasAnnotation] = "2"
			}
			stable := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			candidate := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			r2 = updateCanaryRolloutStatus(r2, stable, 1, 0, 1, false)
			r2.Status.Abort = true
			now := timeutil.MetaNow()
			r2.Status.AbortedAt = &now
			var oldRuns []*v1alpha1.AnalysisRun
			if tt.step {
				old := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r2)
				index := 0
				if tt.pause {
					index = 1
					old.Labels[v1alpha1.RolloutCanaryStepIndexLabel] = "1"
				}
				old.Name = fmt.Sprintf("%s-%s-2-%d", r2.Name, candidate, index)
				oldRuns = append(oldRuns, old)
				r2.Status.Canary.CurrentStepAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{Name: old.Name, Status: tt.phase}
			}
			if tt.background {
				old := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
				oldRuns = append(oldRuns, old)
				r2.Status.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{Name: old.Name, Status: tt.phase}
			}
			f.objects = append(f.objects, r2, at)
			for _, old := range oldRuns {
				old.Status.Phase = tt.phase
				// Termination is asynchronous: a manually aborted run may still report Running.
				old.Spec.Terminate = tt.phase == v1alpha1.AnalysisPhaseRunning
				f.objects = append(f.objects, old)
				f.analysisRunLister = append(f.analysisRunLister, old)
			}
			f.kubeobjects = append(f.kubeobjects, rs1, rs2)
			f.rolloutLister = append(f.rolloutLister, r2)
			f.analysisTemplateLister = append(f.analysisTemplateLister, at)
			f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
			c, i, k := f.newController(noResyncPeriodFunc)
			ros := f.client.ArgoprojV1alpha1().Rollouts(r2.Namespace)
			ars := f.client.ArgoprojV1alpha1().AnalysisRuns(r2.Namespace)
			rss := f.kubeclient.AppsV1().ReplicaSets(r2.Namespace)
			key := getKey(r2, t)

			// Rebuild caches from persisted API state so retries span real reconciliation passes.
			reconcile := func(expectError bool) *v1alpha1.Rollout {
				ro, err := ros.Get(ctx, r2.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.NoError(t, c.rolloutsIndexer.Update(ro))
				c.rolloutVersionTracker.Forget(key)
				sets, err := rss.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for j := range sets.Items {
					require.NoError(t, k.Apps().V1().ReplicaSets().Informer().GetIndexer().Update(&sets.Items[j]))
				}
				runs, err := ars.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for j := range runs.Items {
					require.NoError(t, i.Argoproj().V1alpha1().AnalysisRuns().Informer().GetIndexer().Update(&runs.Items[j]))
				}
				err = c.syncHandler(ctx, key)
				if expectError {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				ro, err = ros.Get(ctx, r2.Name, metav1.GetOptions{})
				require.NoError(t, err)
				return ro
			}
			failNextWrite := func(resource string) {
				fail := true
				reactor := func(action core.Action) (bool, runtime.Object, error) {
					if action.GetVerb() != "patch" && action.GetVerb() != "update" {
						return false, nil, nil
					}
					if !fail {
						return false, nil, nil
					}
					fail = false
					return true, nil, errors.New("API temporarily unavailable")
				}
				if resource == "rollouts" {
					f.client.PrependReactor("*", resource, reactor)
				} else {
					f.kubeclient.PrependReactor("*", resource, reactor)
				}
			}
			retry := func() {
				_, err := requestRetry(ros, r2.Name)
				require.NoError(t, err)
			}
			currentRuns := func(ro *v1alpha1.Rollout) []*v1alpha1.AnalysisRun {
				var runs []*v1alpha1.AnalysisRun
				refs := []*v1alpha1.RolloutAnalysisRunStatus{}
				if tt.step {
					refs = append(refs, ro.Status.Canary.CurrentStepAnalysisRunStatus)
				}
				if tt.background {
					refs = append(refs, ro.Status.Canary.CurrentBackgroundAnalysisRunStatus)
				}
				for _, ref := range refs {
					require.NotNil(t, ref)
					run, err := ars.Get(ctx, ref.Name, metav1.GetOptions{})
					require.NoError(t, err)
					assert.False(t, run.Spec.Terminate)
					assert.Equal(t, candidate, run.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
					assert.Equal(t, "2", run.Annotations[annotations.RevisionAnnotation])
					runs = append(runs, run)
				}
				return runs
			}

			step := func() *v1alpha1.Rollout { return reconcile(false) }
			hasFreshRuns := func(ro *v1alpha1.Rollout, previous []*v1alpha1.AnalysisRun) bool {
				refs := []*v1alpha1.RolloutAnalysisRunStatus{}
				if tt.step {
					refs = append(refs, ro.Status.Canary.CurrentStepAnalysisRunStatus)
				}
				if tt.background {
					refs = append(refs, ro.Status.Canary.CurrentBackgroundAnalysisRunStatus)
				}
				for j, ref := range refs {
					if ref == nil || ref.Name == previous[j].Name {
						return false
					}
				}
				return true
			}
			retry()
			if tt.scalingFailure {
				failNextWrite("replicasets")
				reconcile(true)
			}
			if tt.statusFailure {
				failNextWrite("rollouts")
				reconcile(true)
			}
			ro := reconcile(false)
			require.False(t, ro.Status.Abort)

			// Reconstruct the controller after retry has begun, including any created runs.
			f.objects = []runtime.Object{ro, at}
			runs, err := ars.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			for j := range runs.Items {
				f.objects = append(f.objects, &runs.Items[j])
			}
			sets, err := rss.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			f.kubeobjects = nil
			for j := range sets.Items {
				f.kubeobjects = append(f.kubeobjects, &sets.Items[j])
			}
			c, i, k = f.newController(noResyncPeriodFunc)
			ros = f.client.ArgoprojV1alpha1().Rollouts(r2.Namespace)
			ars = f.client.ArgoprojV1alpha1().AnalysisRuns(r2.Namespace)
			rss = f.kubeclient.AppsV1().ReplicaSets(r2.Namespace)
			ro = reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool {
				require.False(t, ro.Status.Abort, "the previous analysis must not re-abort the first retry")
				if tt.pause {
					return len(ro.Status.PauseConditions) > 0 && ro.Status.AbortedAt == nil
				}
				return hasFreshRuns(ro, oldRuns)
			}, "retry survives reconstruction and reaches analysis or its starting pause")
			resume := func(previous []*v1alpha1.AnalysisRun) {
				if tt.pause {
					require.NotEmpty(t, ro.Status.PauseConditions, "retry must preserve the manual pause")
					_, err := ros.Patch(ctx, r2.Name, types.MergePatchType, []byte(`{"status":{"pauseConditions":null}}`), metav1.PatchOptions{}, "status")
					require.NoError(t, err)
					ro = reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool {
						require.False(t, ro.Status.Abort)
						return hasFreshRuns(ro, previous)
					}, "analysis starts after the manual pause is resumed")
				}
			}
			if tt.pause {
				runs, err := ars.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				assert.Len(t, runs.Items, len(oldRuns), "analysis must wait for its starting step")
			}
			resume(oldRuns)
			fresh := currentRuns(ro)
			for j, old := range oldRuns {
				assert.NotEqual(t, old.Name, fresh[j].Name)
				historical, err := ars.Get(ctx, old.Name, metav1.GetOptions{})
				require.NoError(t, err)
				assert.Equal(t, tt.phase, historical.Status.Phase)
			}
			runs, err = ars.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			assert.Len(t, runs.Items, 2*len(oldRuns), "only one fresh attempt is created")

			fresh[0].Status.Phase = v1alpha1.AnalysisPhaseFailed
			_, err = ars.UpdateStatus(ctx, fresh[0], metav1.UpdateOptions{})
			require.NoError(t, err)
			if tt.scalingFailure {
				// An error during a fresh attempt must not discard its pending failure.
				stableRS, err := rss.Get(ctx, rs1.Name, metav1.GetOptions{})
				require.NoError(t, err)
				stableRS.Annotations[annotations.DesiredReplicasAnnotation] = "2"
				_, err = rss.Update(ctx, stableRS, metav1.UpdateOptions{})
				require.NoError(t, err)
				failNextWrite("replicasets")
				reconcile(true)
			}
			ro = reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool { return ro.Status.Abort }, "a fresh Canary failure aborts")
			require.True(t, ro.Status.Abort, "a fresh failure must still abort")
			reconcile(false)
			retry()
			ro = reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool {
				require.False(t, ro.Status.Abort)
				if tt.pause {
					return len(ro.Status.PauseConditions) > 0 && ro.Status.AbortedAt == nil
				}
				return hasFreshRuns(ro, fresh)
			}, "second retry reaches fresh analysis or its starting pause")
			resume(fresh)
			last := currentRuns(ro)
			for j, run := range last {
				assert.NotEqual(t, fresh[j].Name, run.Name)
				run.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
				_, err := ars.UpdateStatus(ctx, run, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			readyStep := func() *v1alpha1.Rollout {
				// Model pods becoming ready as their ReplicaSets reach the requested sizes.
				sets, err := rss.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for _, rs := range sets.Items {
					rs.Status.Replicas = *rs.Spec.Replicas
					rs.Status.ReadyReplicas = *rs.Spec.Replicas
					rs.Status.AvailableReplicas = *rs.Spec.Replicas
					_, err := rss.UpdateStatus(ctx, &rs, metav1.UpdateOptions{})
					require.NoError(t, err)
				}
				return reconcile(false)
			}
			ro = reconcileAnalysisRetryUntil(t, readyStep, func(ro *v1alpha1.Rollout) bool { return ro.Status.Phase == v1alpha1.RolloutPhaseHealthy }, "successful Canary analysis completes the rollout")
			ro = readyStep()
			assert.False(t, ro.Status.Abort)
			assert.Equal(t, candidate, ro.Status.StableRS)
			assert.Equal(t, v1alpha1.RolloutPhaseHealthy, ro.Status.Phase)
			runs, err = ars.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			assert.Len(t, runs.Items, 3*len(oldRuns), "completed analysis must not be duplicated")
		})
	}
}

// reconcileAnalysisRetryUntil waits for a persisted outcome, rather than prescribing
// how many controller passes an analysis retry must take.
func reconcileAnalysisRetryUntil(t *testing.T, reconcile func() *v1alpha1.Rollout, done func(*v1alpha1.Rollout) bool, outcome string) *v1alpha1.Rollout {
	t.Helper()
	var ro *v1alpha1.Rollout
	for pass := 0; pass < 20; pass++ {
		ro = reconcile()
		if done(ro) {
			return ro
		}
	}
	require.FailNow(t, "rollout did not converge", "%s; last status: %+v", outcome, ro.Status)
	return ro
}

func TestBlueGreenPrePromotionAnalysisRetry(t *testing.T) {
	testBlueGreenPrePromotionAnalysisRetry(t, retry.RetryRollout)
}

func testBlueGreenPrePromotionAnalysisRetry(t *testing.T, requestRetry retryRolloutRequest, onlyCases ...string) {
	t.Helper()
	cases := []struct {
		name           string
		phase          v1alpha1.AnalysisPhase
		scaling        bool
		serviceFailure bool
		statusFailure  bool
	}{
		{name: "Failed", phase: v1alpha1.AnalysisPhaseFailed},
		{name: "FailedDuringScaling", phase: v1alpha1.AnalysisPhaseFailed, scaling: true},
		{name: "Error", phase: v1alpha1.AnalysisPhaseError},
		{name: "ErrorDuringScaling", phase: v1alpha1.AnalysisPhaseError, scaling: true},
		{name: "RetryAfterServiceFailure", phase: v1alpha1.AnalysisPhaseFailed, serviceFailure: true},
		{name: "RetryAfterStatusFailure", phase: v1alpha1.AnalysisPhaseError, statusFailure: true},
		{name: "ManualAbortRunning", phase: v1alpha1.AnalysisPhaseRunning, scaling: true},
	}
	for _, tt := range cases {
		if len(onlyCases) > 0 && !slices.Contains(onlyCases, tt.name) {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			phase, scaling := tt.phase, tt.scaling
			f := newFixture(t)
			defer f.Close()
			ctx := context.Background()
			at := analysisTemplate("pre-retry")
			r1 := newBlueGreenRollout("pre-retry", 1, nil, "active", "preview")
			r2 := bumpVersion(r1)
			r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{Templates: []v1alpha1.AnalysisTemplateRef{{TemplateName: at.Name}}}
			rs1 := newReplicaSetWithStatus(r1, 1, 1)
			rs2 := newReplicaSetWithStatus(r2, 0, 0)
			if scaling {
				rs1.Annotations[annotations.DesiredReplicasAnnotation] = "2"
			}
			stable := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			candidate := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			r2 = updateBlueGreenRolloutStatus(r2, candidate, stable, stable, 1, 0, 1, 1, false, true, false)
			r2.Status.Abort = true
			now := timeutil.MetaNow()
			r2.Status.AbortedAt = &now
			old := analysisRun(at, v1alpha1.RolloutTypePrePromotionLabel, r2)
			old.Status.Phase = phase
			old.Spec.Terminate = phase == v1alpha1.AnalysisPhaseRunning
			r2.Status.BlueGreen.PrePromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{Name: old.Name, Status: phase}
			active := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: stable}, r2)
			preview := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: candidate}, r2)
			if tt.serviceFailure {
				preview.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] = stable
			}
			f.objects = []runtime.Object{r2, at, old}
			f.kubeobjects = []runtime.Object{rs1, rs2, active, preview}
			f.rolloutLister = append(f.rolloutLister, r2)
			f.analysisTemplateLister = append(f.analysisTemplateLister, at)
			f.analysisRunLister = append(f.analysisRunLister, old)
			f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
			f.serviceLister = append(f.serviceLister, active, preview)
			c, i, k := f.newController(noResyncPeriodFunc)
			ros := f.client.ArgoprojV1alpha1().Rollouts(r2.Namespace)
			ars := f.client.ArgoprojV1alpha1().AnalysisRuns(r2.Namespace)
			rss := f.kubeclient.AppsV1().ReplicaSets(r2.Namespace)
			svcs := f.kubeclient.CoreV1().Services(r2.Namespace)
			key := getKey(r2, t)
			reconcile := func(expectError ...bool) *v1alpha1.Rollout {
				ro, err := ros.Get(ctx, r2.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.NoError(t, c.rolloutsIndexer.Update(ro))
				c.rolloutVersionTracker.Forget(key)
				sets, err := rss.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for j := range sets.Items {
					require.NoError(t, k.Apps().V1().ReplicaSets().Informer().GetIndexer().Update(&sets.Items[j]))
				}
				services, err := svcs.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for j := range services.Items {
					require.NoError(t, k.Core().V1().Services().Informer().GetIndexer().Update(&services.Items[j]))
				}
				runs, err := ars.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for j := range runs.Items {
					require.NoError(t, i.Argoproj().V1alpha1().AnalysisRuns().Informer().GetIndexer().Update(&runs.Items[j]))
				}
				err = c.syncHandler(ctx, key)
				if len(expectError) > 0 && expectError[0] {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				ro, err = ros.Get(ctx, r2.Name, metav1.GetOptions{})
				require.NoError(t, err)
				return ro
			}
			step := func() *v1alpha1.Rollout { return reconcile() }
			failNextWrite := func(resource string) {
				fail := true
				reactor := func(action core.Action) (bool, runtime.Object, error) {
					if !fail || (action.GetVerb() != "patch" && action.GetVerb() != "update") {
						return false, nil, nil
					}
					fail = false
					return true, nil, errors.New("API temporarily unavailable")
				}
				if resource == "rollouts" {
					f.client.PrependReactor("*", resource, reactor)
				} else {
					f.kubeclient.PrependReactor("*", resource, reactor)
				}
			}
			_, err := requestRetry(ros, r2.Name)
			require.NoError(t, err)
			if tt.serviceFailure {
				failNextWrite("services")
				reconcile(true)
			}
			if tt.statusFailure {
				failNextWrite("rollouts")
				reconcile(true)
			}
			ro := reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool {
				require.False(t, ro.Status.Abort, "old pre-promotion result must not re-abort retry")
				runs, err := ars.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				require.Len(t, runs.Items, 1, "fresh pre-analysis must wait for candidate readiness")
				return ro.Status.AbortedAt == nil && ro.Status.BlueGreen.PrePromotionAnalysisRunStatus == nil
			}, "retry detaches previous pre-analysis while readiness is delayed")
			// Reconstruct from persisted API objects before the candidate becomes ready.
			f.objects = []runtime.Object{ro, at}
			runs, err := ars.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			for j := range runs.Items {
				f.objects = append(f.objects, &runs.Items[j])
			}
			sets, err := rss.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			services, err := svcs.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			f.kubeobjects = nil
			for j := range sets.Items {
				f.kubeobjects = append(f.kubeobjects, &sets.Items[j])
			}
			for j := range services.Items {
				f.kubeobjects = append(f.kubeobjects, &services.Items[j])
			}
			c, i, k = f.newController(noResyncPeriodFunc)
			ros = f.client.ArgoprojV1alpha1().Rollouts(r2.Namespace)
			ars = f.client.ArgoprojV1alpha1().AnalysisRuns(r2.Namespace)
			rss = f.kubeclient.AppsV1().ReplicaSets(r2.Namespace)
			svcs = f.kubeclient.CoreV1().Services(r2.Namespace)
			ro = reconcile()
			require.False(t, ro.Status.Abort)
			runs, err = ars.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			require.Len(t, runs.Items, 1, "restart must preserve the readiness gate")
			ready, err := rss.Get(ctx, rs2.Name, metav1.GetOptions{})
			require.NoError(t, err)
			ready.Status.Replicas, ready.Status.ReadyReplicas, ready.Status.AvailableReplicas = 1, 1, 1
			_, err = rss.UpdateStatus(ctx, ready, metav1.UpdateOptions{})
			require.NoError(t, err)
			ro = reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool {
				return ro.Status.BlueGreen.PrePromotionAnalysisRunStatus != nil && ro.Status.BlueGreen.PrePromotionAnalysisRunStatus.Name != old.Name
			}, "fresh pre-analysis starts after candidate readiness")
			require.NotNil(t, ro.Status.BlueGreen.PrePromotionAnalysisRunStatus)
			require.NotEqual(t, old.Name, ro.Status.BlueGreen.PrePromotionAnalysisRunStatus.Name)
			service, err := svcs.Get(ctx, active.Name, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, stable, service.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey], "traffic must wait for pre-analysis success")
			fresh, err := ars.Get(ctx, ro.Status.BlueGreen.PrePromotionAnalysisRunStatus.Name, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, candidate, fresh.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
			assert.Equal(t, "2", fresh.Annotations[annotations.RevisionAnnotation])
			assert.False(t, fresh.Spec.Terminate)
			history, err := ars.Get(ctx, old.Name, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, phase, history.Status.Phase)
			fresh.Status.Phase = v1alpha1.AnalysisPhaseFailed
			_, err = ars.UpdateStatus(ctx, fresh, metav1.UpdateOptions{})
			require.NoError(t, err)
			if tt.serviceFailure {
				service, err := svcs.Get(ctx, preview.Name, metav1.GetOptions{})
				require.NoError(t, err)
				service.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] = stable
				_, err = svcs.Update(ctx, service, metav1.UpdateOptions{})
				require.NoError(t, err)
				failNextWrite("services")
				reconcile(true)
			}
			ro = reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool { return ro.Status.Abort }, "a fresh pre-analysis failure still aborts")
			_, err = requestRetry(ros, r2.Name)
			require.NoError(t, err)
			ro = reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool {
				require.False(t, ro.Status.Abort)
				ref := ro.Status.BlueGreen.PrePromotionAnalysisRunStatus
				return ref != nil && ref.Name != fresh.Name && ref.Name != old.Name
			}, "a second retry creates another pre-analysis")
			last, err := ars.Get(ctx, ro.Status.BlueGreen.PrePromotionAnalysisRunStatus.Name, metav1.GetOptions{})
			require.NoError(t, err)
			last.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
			_, err = ars.UpdateStatus(ctx, last, metav1.UpdateOptions{})
			require.NoError(t, err)
			ro = reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool {
				return ro.Status.Phase == v1alpha1.RolloutPhaseHealthy
			}, "successful pre-analysis completes the rollout")
			ro = step()
			assert.Equal(t, candidate, ro.Status.BlueGreen.ActiveSelector)
			assert.Equal(t, v1alpha1.RolloutPhaseHealthy, ro.Status.Phase)
			runs, err = ars.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			assert.Len(t, runs.Items, 3, "one analysis per attempt, with history retained")
		})
	}
}

func TestCreatePrePromotionAnalysisRun(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = ptr.To[bool](false)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{{
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
		Templates: []v1alpha1.AnalysisTemplateRef{{
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
		Templates: []v1alpha1.AnalysisTemplateRef{{
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

	f.expectCreateReplicaSetAction(rs)   // create replica set
	f.expectUpdateRolloutStatusAction(r) // update rollout conditions
	f.expectGetRolloutAction(r)          // second reconciliation
	f.expectUpdateReplicaSetAction(rs)   // scale RS
	f.expectPatchRolloutAction(r)        // patch status
	f.runWithSyncs(getKey(r, t), 2)
}

// TestDoNotCreatePrePromotionAnalysisRunOnNotReadyReplicaSet ensures that a pre-promotion analysis is not created until
// the new ReplicaSet is saturated
func TestDoNotCreatePrePromotionAnalysisRunOnNotReadyReplicaSet(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 2, nil, "active", "preview")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = ptr.To[bool](false)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{{
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
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = ptr.To[bool](false)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{{
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
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = ptr.To[bool](true)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{{
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
	now := timeutil.MetaNow().UTC().Format(time.RFC3339)
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
			"message": null,
			"duration": {
				"completionStatus": "Promoted",
				"finishedAt": "%s"
			}
		}
	}`, rs2PodHash, rs2PodHash, rs2PodHash, now)
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
	f.metricsRecorder.AssertNumberOfCalls(t, "EmitRolloutDuration", 1)
}

func TestRolloutPrePromotionAnalysisHonorAutoPromotionSeconds(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = ptr.To[bool](true)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.AutoPromotionSeconds = 10
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{{
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
	before := metav1.NewTime(timeutil.MetaNow().Add(-10 * time.Second))
	r2.Status.PauseConditions[0].StartTime = before
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
	now := timeutil.MetaNow().UTC().Format(time.RFC3339)
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
			"message": null,
			"duration": {
				"completionStatus": "Promoted",
				"finishedAt": "%s"
			}
		}
	}`, rs2PodHash, rs2PodHash, rs2PodHash, now)
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
	f.metricsRecorder.AssertNumberOfCalls(t, "EmitRolloutDuration", 1)
}

func TestRolloutPrePromotionAnalysisDoNothingOnInconclusiveAnalysis(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = ptr.To[bool](false)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{{
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
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = ptr.To[bool](false)
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{{
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
			"message": "%s: %s",
			"duration": {
				"completionStatus": "Aborted",
				"finishedAt": "%s",
				"manualPauseStartedAt": null,
				"totalManualPauseDurationSeconds": 5
			}
		}
	}`
	now := timeutil.MetaNow().UTC().Format(time.RFC3339)
	progressingFalseAborted, _ := newProgressingCondition(conditions.RolloutAbortedReason, r2, "Blue/green pre-promotion analysis phase error/failed")
	newConditions := updateConditionsPatch(*r2, progressingFalseAborted)
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, now, newConditions, conditions.RolloutAbortedReason, progressingFalseAborted.Message, now)), patch)
	f.metricsRecorder.AssertNumberOfCalls(t, "EmitRolloutDuration", 1)
}

func TestCreatePostPromotionAnalysisRun(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PostPromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{{
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
		Templates: []v1alpha1.AnalysisTemplateRef{{
			TemplateName: at.Name,
		}},
	}
	ar := analysisRun(at, v1alpha1.RolloutTypePostPromotionLabel, r2)
	ar.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
	r2.Status.BlueGreen.PostPromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar.Name,
		Status: v1alpha1.AnalysisPhaseRunning,
	}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
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
	now := timeutil.MetaNow().UTC().Format(time.RFC3339)
	expectedPatch := fmt.Sprintf(`{
		"status": {
			"replicas":2,
			"stableRS": "%s",
			"blueGreen": {
				"postPromotionAnalysisRunStatus":{"status":"Successful"}
			},
			"phase": "Healthy",
			"message": null,
			"duration": {
				"completionStatus": "Promoted",
				"finishedAt": "%s"
			}
		}
	}`, rs2PodHash, now)
	assert.JSONEq(t, calculatePatch(r2, expectedPatch), patch)
	f.metricsRecorder.AssertNumberOfCalls(t, "EmitRolloutDuration", 1)
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
		Templates: []v1alpha1.AnalysisTemplateRef{{
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
	expectedPatch := `{
		"status": {
			"blueGreen": {
				"postPromotionAnalysisRunStatus": {"status":"Inconclusive"}
			},
			"phase": "Paused",
			"message": "InconclusiveAnalysisRun",
			"duration": {
				"manualPauseStartedAt": "%s"
			}
		}
	}`
	now := timeutil.MetaNow().UTC().Format(time.RFC3339)
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, now)), patch)
}

func TestAbortRolloutOnErrorPostPromotionAnalysis(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r2 := bumpVersion(r1)
	r2.Spec.Strategy.BlueGreen.PostPromotionAnalysis = &v1alpha1.RolloutAnalysis{
		Templates: []v1alpha1.AnalysisTemplateRef{{
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
			"message": "%s: %s",
			"duration": {
				"completionStatus": "Aborted",
				"finishedAt": "%s"
			}
		}
	}`
	now := timeutil.MetaNow().UTC().Format(time.RFC3339)
	progressingFalseAborted, _ := newProgressingCondition(conditions.RolloutAbortedReason, r2, "Blue/green post-promotion analysis phase error/failed")
	newConditions := updateConditionsPatch(*r2, progressingFalseAborted)
	assert.JSONEq(t, calculatePatch(r2, fmt.Sprintf(expectedPatch, now, newConditions, conditions.RolloutAbortedReason, progressingFalseAborted.Message, now)), patch)
	f.metricsRecorder.AssertNumberOfCalls(t, "EmitRolloutDuration", 1)
}

func TestBlueGreenPostPromotionAnalysisRetry(t *testing.T) {
	testBlueGreenPostPromotionAnalysisRetry(t, retry.RetryRollout)
}

func testBlueGreenPostPromotionAnalysisRetry(t *testing.T, requestRetry retryRolloutRequest, onlyCases ...string) {
	t.Helper()
	tests := []struct {
		name            string
		phase           v1alpha1.AnalysisPhase
		ready           bool
		manualPromotion bool
		scalingEvent    bool
		serviceFailure  bool
	}{
		{name: "FailedUnavailable", phase: v1alpha1.AnalysisPhaseFailed, manualPromotion: true},
		{name: "ErrorUnavailable", phase: v1alpha1.AnalysisPhaseError},
		{name: "FailedReady", phase: v1alpha1.AnalysisPhaseFailed, ready: true},
		{name: "ErrorReady", phase: v1alpha1.AnalysisPhaseError, ready: true, manualPromotion: true},
		{name: "ManualAbortRunning", phase: v1alpha1.AnalysisPhaseRunning, ready: true},
		{name: "RetryDuringScaling", phase: v1alpha1.AnalysisPhaseFailed, scalingEvent: true},
		{name: "RetryAfterServiceFailure", phase: v1alpha1.AnalysisPhaseFailed, serviceFailure: true},
	}
	for _, tt := range tests {
		if len(onlyCases) > 0 && !slices.Contains(onlyCases, tt.name) {
			continue
		}
		t.Run(tt.name, func(t *testing.T) {
			phase := tt.phase
			f := newFixture(t)
			defer f.Close()
			ctx := context.Background()
			at := analysisTemplate("post-analysis")
			r1 := newBlueGreenRollout("retry-post-analysis", 1, nil, "active", "preview")
			r2 := bumpVersion(r1)
			r2.Spec.Strategy.BlueGreen.AutoPromotionEnabled = ptr.To(!tt.manualPromotion)
			r2.Spec.Strategy.BlueGreen.AbortScaleDownDelaySeconds = ptr.To[int32](1)
			r2.Spec.Strategy.BlueGreen.ScaleDownDelaySeconds = ptr.To[int32](600)
			r2.Spec.Strategy.BlueGreen.PostPromotionAnalysis = &v1alpha1.RolloutAnalysis{
				Templates: []v1alpha1.AnalysisTemplateRef{{TemplateName: at.Name}},
			}
			rs1 := newReplicaSetWithStatus(r1, 1, 1)
			rs2 := newReplicaSetWithStatus(r2, 0, 0)
			if tt.ready {
				rs2 = newReplicaSetWithStatus(r2, 1, 1)
			}
			if tt.scalingEvent {
				rs1.Annotations[annotations.DesiredReplicasAnnotation] = "2"
			}
			stableHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			candidateHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			r2 = updateBlueGreenRolloutStatus(r2, candidateHash, stableHash, stableHash, 1, 0, 1, 1, false, true, false)
			r2.Status.Abort = true
			now := timeutil.MetaNow()
			r2.Status.AbortedAt = &now
			oldAR := analysisRun(at, v1alpha1.RolloutTypePostPromotionLabel, r2)
			oldAR.Status.Phase = phase
			r2.Status.BlueGreen.PostPromotionAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{Name: oldAR.Name, Status: phase}
			active := newService("active", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: stableHash}, r2)
			preview := newService("preview", 80, map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: candidateHash}, r2)
			if tt.serviceFailure {
				preview.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] = stableHash
			}
			f.objects = append(f.objects, r2, at, oldAR)
			f.kubeobjects = append(f.kubeobjects, rs1, rs2, active, preview)
			f.rolloutLister = append(f.rolloutLister, r2)
			f.analysisTemplateLister = append(f.analysisTemplateLister, at)
			f.analysisRunLister = append(f.analysisRunLister, oldAR)
			f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
			f.serviceLister = append(f.serviceLister, active, preview)
			c, i, k8sI := f.newController(noResyncPeriodFunc)
			ros := f.client.ArgoprojV1alpha1().Rollouts(r2.Namespace)
			ars := f.client.ArgoprojV1alpha1().AnalysisRuns(r2.Namespace)
			rss := f.kubeclient.AppsV1().ReplicaSets(r2.Namespace)
			svcs := f.kubeclient.CoreV1().Services(r2.Namespace)
			checkActiveTraffic := func(core.Action) (bool, runtime.Object, error) {
				activeSvc, err := svcs.Get(ctx, active.Name, metav1.GetOptions{})
				assert.NoError(t, err)
				assert.Equal(t, candidateHash, activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey], "post analysis must only start after active traffic switches to the candidate")
				return false, nil, nil
			}
			f.client.PrependReactor("create", "analysisruns", checkActiveTraffic)
			key := getKey(r2, t)

			// Feed persisted API state into the caches without asynchronous informer watches.
			// Each pass must rediscover the current analysis from the persisted rollout status.
			reconcile := func(expectError ...bool) *v1alpha1.Rollout {
				ro, err := ros.Get(ctx, r2.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.NoError(t, c.rolloutsIndexer.Update(ro))
				c.rolloutVersionTracker.Forget(key)
				rsList, err := rss.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for n := range rsList.Items {
					require.NoError(t, k8sI.Apps().V1().ReplicaSets().Informer().GetIndexer().Update(&rsList.Items[n]))
				}
				svcList, err := svcs.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for n := range svcList.Items {
					require.NoError(t, k8sI.Core().V1().Services().Informer().GetIndexer().Update(&svcList.Items[n]))
				}
				arList, err := ars.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				for n := range arList.Items {
					require.NoError(t, i.Argoproj().V1alpha1().AnalysisRuns().Informer().GetIndexer().Update(&arList.Items[n]))
				}
				err = c.syncHandler(ctx, key)
				if len(expectError) > 0 && expectError[0] {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				ro, err = ros.Get(ctx, r2.Name, metav1.GetOptions{})
				require.NoError(t, err)
				return ro
			}
			failNextServiceUpdate := func() {
				failNext := true
				f.kubeclient.PrependReactor("*", "services", func(action core.Action) (bool, runtime.Object, error) {
					if !failNext || (action.GetVerb() != "patch" && action.GetVerb() != "update") {
						return false, nil, nil
					}
					failNext = false
					return true, nil, errors.New("service update temporarily unavailable")
				})
			}

			step := func() *v1alpha1.Rollout { return reconcile() }
			if phase == v1alpha1.AnalysisPhaseRunning {
				// A manual abort must terminate the in-flight analysis before retrying.
				reconcile()
				terminated, err := ars.Get(ctx, oldAR.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.True(t, terminated.Spec.Terminate)
			}
			// Exercise the public retry operation used by the CLI and dashboard server.
			_, err := requestRetry(ros, r2.Name)
			require.NoError(t, err)
			if tt.serviceFailure {
				// A failed stage may preserve this attempt's state, but must not restore
				// the aborted attempt's analysis as current after retry has begun.
				failNextServiceUpdate()
				reconcile(true)
			}
			if !tt.ready {
				ro := reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool {
					require.False(t, ro.Status.Abort, "old analysis must not re-abort while candidate is unavailable")
					require.Equal(t, stableHash, ro.Status.BlueGreen.ActiveSelector)
					runs, err := ars.List(ctx, metav1.ListOptions{})
					require.NoError(t, err)
					require.Len(t, runs.Items, 1, "post analysis must wait for active traffic")
					candidate, err := rss.Get(ctx, rs2.Name, metav1.GetOptions{})
					require.NoError(t, err)
					return *candidate.Spec.Replicas == 1 && ro.Status.AbortedAt == nil
				}, "retry scales the candidate without bypassing readiness")
				arList, err := ars.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				// Restart from persisted objects while readiness remains delayed.
				f.objects = []runtime.Object{ro, at}
				for j := range arList.Items {
					f.objects = append(f.objects, &arList.Items[j])
				}
				rsList, err := rss.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				svcList, err := svcs.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				f.kubeobjects = nil
				for j := range rsList.Items {
					f.kubeobjects = append(f.kubeobjects, &rsList.Items[j])
				}
				for j := range svcList.Items {
					f.kubeobjects = append(f.kubeobjects, &svcList.Items[j])
				}
				c, i, k8sI = f.newController(noResyncPeriodFunc)
				f.client.PrependReactor("create", "analysisruns", checkActiveTraffic)
				ros = f.client.ArgoprojV1alpha1().Rollouts(r2.Namespace)
				ars = f.client.ArgoprojV1alpha1().AnalysisRuns(r2.Namespace)
				rss = f.kubeclient.AppsV1().ReplicaSets(r2.Namespace)
				svcs = f.kubeclient.CoreV1().Services(r2.Namespace)
				ro = reconcile()
				require.False(t, ro.Status.Abort)
				require.Equal(t, stableHash, ro.Status.BlueGreen.ActiveSelector)
				arList, err = ars.List(ctx, metav1.ListOptions{})
				require.NoError(t, err)
				require.Len(t, arList.Items, 1, "restart must preserve the readiness gate")
				candidate, err := rss.Get(ctx, rs2.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.Equal(t, int32(1), *candidate.Spec.Replicas, "retry must scale up the desired candidate")
				candidate.Status.Replicas = 1
				candidate.Status.ReadyReplicas = 1
				candidate.Status.AvailableReplicas = 1
				_, err = rss.UpdateStatus(ctx, candidate, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			ro := reconcile()
			// Manual promotion remains required when autoPromotionEnabled is false.
			assert.False(t, ro.Status.Abort)
			if tt.manualPromotion {
				assert.Equal(t, stableHash, ro.Status.BlueGreen.ActiveSelector)
				_, err := ros.Patch(ctx, r2.Name, types.MergePatchType, []byte(`{"status":{"pauseConditions":null}}`), metav1.PatchOptions{}, "status")
				require.NoError(t, err)
			}
			ro = reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool {
				require.False(t, ro.Status.Abort)
				ref := ro.Status.BlueGreen.PostPromotionAnalysisRunStatus
				return ro.Status.BlueGreen.ActiveSelector == candidateHash && ref != nil && ref.Name != oldAR.Name
			}, "post analysis starts after active traffic switches")
			assert.False(t, ro.Status.Abort)
			assert.Equal(t, candidateHash, ro.Status.BlueGreen.ActiveSelector)
			require.NotNil(t, ro.Status.BlueGreen.PostPromotionAnalysisRunStatus)
			assert.NotEqual(t, oldAR.Name, ro.Status.BlueGreen.PostPromotionAnalysisRunStatus.Name)
			arList, err := ars.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			assert.Len(t, arList.Items, 2)
			historical, err := ars.Get(ctx, oldAR.Name, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, phase, historical.Status.Phase)
			fresh, err := ars.Get(ctx, ro.Status.BlueGreen.PostPromotionAnalysisRunStatus.Name, metav1.GetOptions{})
			require.NoError(t, err)
			assert.Equal(t, candidateHash, fresh.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
			assert.Equal(t, "2", fresh.Annotations[annotations.RevisionAnnotation])
			assert.False(t, fresh.Spec.Terminate)
			assert.False(t, fresh.Status.Phase.Completed(), "retry must evaluate a new analysis")

			// A new failure must still abort, and another retry must evaluate a third run.
			fresh.Status.Phase = v1alpha1.AnalysisPhaseFailed
			_, err = ars.UpdateStatus(ctx, fresh, metav1.UpdateOptions{})
			require.NoError(t, err)
			if tt.serviceFailure {
				// In contrast, a transient error within the fresh attempt must not drop
				// its terminal run before the controller can process the new failure.
				stalePreview := []byte(`{"spec":{"selector":{"` + v1alpha1.DefaultRolloutUniqueLabelKey + `":"` + stableHash + `"}}}`)
				_, err = svcs.Patch(ctx, preview.Name, types.MergePatchType, stalePreview, metav1.PatchOptions{})
				require.NoError(t, err)
				failNextServiceUpdate()
				reconcile(true)
			}
			ro = reconcile()
			require.True(t, ro.Status.Abort)
			reconcile() // return traffic to stable and schedule candidate scale-down
			assert.Equal(t, stableHash, reconcile().Status.BlueGreen.ActiveSelector)
			later := timeutil.Now().Add(2 * time.Second)
			timeutil.SetNowTimeFunc(func() time.Time { return later })
			reconcile() // let the abort scale-down deadline expire
			candidate, err := rss.Get(ctx, rs2.Name, metav1.GetOptions{})
			require.NoError(t, err)
			require.Zero(t, *candidate.Spec.Replicas)
			candidate.Status.Replicas = 0
			candidate.Status.AvailableReplicas = 0
			candidate.Status.ReadyReplicas = 0
			_, err = rss.UpdateStatus(ctx, candidate, metav1.UpdateOptions{})
			require.NoError(t, err)
			_, err = requestRetry(ros, r2.Name)
			require.NoError(t, err)
			ro = reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool {
				require.False(t, ro.Status.Abort)
				require.Equal(t, stableHash, ro.Status.BlueGreen.ActiveSelector)
				candidate, err := rss.Get(ctx, rs2.Name, metav1.GetOptions{})
				require.NoError(t, err)
				return *candidate.Spec.Replicas == 1 && ro.Status.AbortedAt == nil
			}, "second retry scales the candidate while traffic stays stable")
			candidate, err = rss.Get(ctx, rs2.Name, metav1.GetOptions{})
			require.NoError(t, err)
			candidate.Status.Replicas = 1
			candidate.Status.AvailableReplicas = 1
			candidate.Status.ReadyReplicas = 1
			_, err = rss.UpdateStatus(ctx, candidate, metav1.UpdateOptions{})
			require.NoError(t, err)
			reconcile()
			if tt.manualPromotion {
				_, err = ros.Patch(ctx, r2.Name, types.MergePatchType, []byte(`{"status":{"pauseConditions":null}}`), metav1.PatchOptions{}, "status")
				require.NoError(t, err)
			}
			ro = reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool {
				require.False(t, ro.Status.Abort)
				ref := ro.Status.BlueGreen.PostPromotionAnalysisRunStatus
				return ref != nil && ref.Name != fresh.Name && ref.Name != oldAR.Name
			}, "second retry creates fresh post analysis")
			require.False(t, ro.Status.Abort)
			require.NotNil(t, ro.Status.BlueGreen.PostPromotionAnalysisRunStatus)
			lastName := ro.Status.BlueGreen.PostPromotionAnalysisRunStatus.Name
			assert.NotEqual(t, fresh.Name, lastName)
			assert.NotEqual(t, oldAR.Name, lastName)
			last, err := ars.Get(ctx, lastName, metav1.GetOptions{})
			require.NoError(t, err)
			last.Status.Phase = v1alpha1.AnalysisPhaseSuccessful
			_, err = ars.UpdateStatus(ctx, last, metav1.UpdateOptions{})
			require.NoError(t, err)
			ro = reconcileAnalysisRetryUntil(t, step, func(ro *v1alpha1.Rollout) bool { return ro.Status.Phase == v1alpha1.RolloutPhaseHealthy }, "successful post analysis completes the rollout")
			ro = step()
			assert.False(t, ro.Status.Abort)
			assert.Equal(t, candidateHash, ro.Status.StableRS)
			assert.Equal(t, candidateHash, ro.Status.BlueGreen.ActiveSelector)
			assert.Equal(t, v1alpha1.RolloutPhaseHealthy, ro.Status.Phase)
			arList, err = ars.List(ctx, metav1.ListOptions{})
			require.NoError(t, err)
			assert.Len(t, arList.Items, 3, "successful post analysis must not be duplicated")
		})
	}
}

func TestCreateAnalysisRunWithCustomAnalysisRunMetadataAndROCopyLabels(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{{
		SetWeight: int32Ptr(10),
	}}
	at := analysisTemplate("bar")
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](0), intstr.FromInt(0), intstr.FromInt(1))
	r1.ObjectMeta.Labels = make(map[string]string)
	r1.Spec.Selector.MatchLabels["my-label"] = "1234"
	r2 := bumpVersion(r1)
	ar := analysisRun(at, v1alpha1.RolloutTypeBackgroundRunLabel, r2)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{
			Templates: []v1alpha1.AnalysisTemplateRef{
				{
					TemplateName: at.Name,
				},
			},
			AnalysisRunMetadata: &v1alpha1.AnalysisRunMetadata{
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

func TestCancelBackgroundAnalysisRunWhenRolloutAnalysisHasNoTemplate(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	at := analysisTemplate("bar")
	steps := []v1alpha1.CanaryStep{
		{SetWeight: ptr.To[int32](10)},
	}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](1), intstr.FromInt(0), intstr.FromInt(1))
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r1 = updateCanaryRolloutStatus(r1, rs1PodHash, 1, 1, 1, false)
	ar := analysisRun(at, v1alpha1.RolloutTypeStepLabel, r1)
	r1.Status.Canary.CurrentBackgroundAnalysisRunStatus = &v1alpha1.RolloutAnalysisRunStatus{
		Name:   ar.Name,
		Status: v1alpha1.AnalysisPhaseRunning,
	}

	r2 := bumpVersion(r1)
	r2.Spec.Strategy.Canary.Analysis = &v1alpha1.RolloutAnalysisBackground{
		RolloutAnalysis: v1alpha1.RolloutAnalysis{}, // No templates provided.
	}
	rs2 := newReplicaSetWithStatus(r2, 0, 0)

	f.kubeobjects = append(f.kubeobjects, rs1, rs2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.analysisTemplateLister = append(f.analysisTemplateLister, at)
	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.objects = append(f.objects, r2, at, ar)

	_ = f.expectPatchAnalysisRunAction(ar)
	patchIndex := f.expectPatchRolloutAction(r2)
	_ = f.expectUpdateReplicaSetAction(rs1)
	f.run(getKey(r2, t))

	patch := f.getPatchedRollout(patchIndex)

	assert.Contains(t, patch, `"currentBackgroundAnalysisRunStatus":null`)
}

// TestDoNotCreatePrePromotionAnalysisRunWithEmptyTemplates verifies that when PrePromotionAnalysis
// is specified but has no templates (e.g., after a declarative deletion with server-side apply),
// the controller does not attempt to create an AnalysisRun and the rollout proceeds normally.
func TestDoNotCreatePrePromotionAnalysisRunWithEmptyTemplates(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = ptr.To[bool](false)
	r2 := bumpVersion(r1)
	// PrePromotionAnalysis is set but has no templates - simulates field ownership scenario
	r2.Spec.Strategy.BlueGreen.PrePromotionAnalysis = &v1alpha1.RolloutAnalysis{
		AnalysisRunMetadata: &v1alpha1.AnalysisRunMetadata{},
	}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs1PodHash, rs1PodHash, 1, 1, 2, 1, true, true, false)

	previewSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	previewSvc := newService("preview", 80, previewSelector, r2)
	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs1PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)

	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	// Should not create an AnalysisRun since templates are empty
	patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)

	// Verify no prePromotionAnalysisRunStatus was set (analysis was skipped)
	assert.NotContains(t, patch, "prePromotionAnalysisRunStatus")
}

// TestDoNotCreatePostPromotionAnalysisRunWithEmptyTemplates verifies that when PostPromotionAnalysis
// is specified but has no templates, the controller does not attempt to create an AnalysisRun.
func TestDoNotCreatePostPromotionAnalysisRunWithEmptyTemplates(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "")
	r2 := bumpVersion(r1)
	// PostPromotionAnalysis is set but has no templates - simulates field ownership scenario
	r2.Spec.Strategy.BlueGreen.PostPromotionAnalysis = &v1alpha1.RolloutAnalysis{
		AnalysisRunMetadata: &v1alpha1.AnalysisRunMetadata{},
	}

	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs2 := newReplicaSetWithStatus(r2, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	// Active service has been promoted to new RS (rs2), but stableRS is still old (rs1)
	r2 = updateBlueGreenRolloutStatus(r2, rs2PodHash, rs2PodHash, rs1PodHash, 1, 1, 2, 1, false, true, true)

	activeSelector := map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: rs2PodHash}
	activeSvc := newService("active", 80, activeSelector, r2)

	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, activeSvc, rs1, rs2)
	f.rolloutLister = append(f.rolloutLister, r2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, rs2)
	f.serviceLister = append(f.serviceLister, activeSvc)

	// Should not create an AnalysisRun since templates are empty
	patchIndex := f.expectPatchRolloutActionWithPatch(r2, OnlyObservedGenerationPatch)
	f.run(getKey(r2, t))
	patch := f.getPatchedRollout(patchIndex)

	// Verify no postPromotionAnalysisRunStatus was set (analysis was skipped)
	assert.NotContains(t, patch, "postPromotionAnalysisRunStatus")
}

// TestDoNotCreateStepAnalysisRunWithEmptyTemplates verifies that when a canary step has
// analysis specified but with no templates, the controller does not attempt to create an AnalysisRun.
func TestDoNotCreateStepAnalysisRunWithEmptyTemplates(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	steps := []v1alpha1.CanaryStep{
		{
			SetWeight: ptr.To[int32](10),
		},
		{
			// Analysis is set but has no templates
			Analysis: &v1alpha1.RolloutAnalysis{
				AnalysisRunMetadata: &v1alpha1.AnalysisRunMetadata{},
			},
		},
	}

	r1 := newCanaryRollout("foo", 1, nil, steps, ptr.To[int32](1), intstr.FromInt(0), intstr.FromInt(1))
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	r1 = updateCanaryRolloutStatus(r1, rs1PodHash, 1, 1, 1, false)

	f.kubeobjects = append(f.kubeobjects, rs1)
	f.replicaSetLister = append(f.replicaSetLister, rs1)
	f.rolloutLister = append(f.rolloutLister, r1)
	f.objects = append(f.objects, r1)

	// Should not create an AnalysisRun since templates are empty
	patchIndex := f.expectPatchRolloutAction(r1)
	f.run(getKey(r1, t))
	patch := f.getPatchedRollout(patchIndex)

	// Verify no currentStepAnalysisRunStatus was set (analysis was skipped)
	assert.NotContains(t, patch, "currentStepAnalysisRunStatus")
}

func concatMultipleSlices[T any](slices [][]T) []T {
	var totalLen int

	for _, s := range slices {
		totalLen += len(s)
	}

	result := make([]T, totalLen)

	var i int

	for _, s := range slices {
		i += copy(result[i:], s)
	}

	return result
}

// TestSkipPrePromotionAnalysisRun tests the skipPrePromotionAnalysisRun function
func TestSkipPrePromotionAnalysisRun(t *testing.T) {
	t.Run("should skip when StableRS equals currentPodHash", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "preview")
		newRS := newReplicaSetWithStatus(rollout, 2, 2)
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		rollout.Status.StableRS = podHash
		rollout.Status.BlueGreen.ActiveSelector = "different-hash"

		result := skipPrePromotionAnalysisRun(rollout, newRS, nil)
		assert.True(t, result, "Should skip when StableRS equals currentPodHash")
	})

	t.Run("should skip when activeSelector is empty", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "preview")
		newRS := newReplicaSetWithStatus(rollout, 2, 2)
		rollout.Status.BlueGreen.ActiveSelector = ""
		rollout.Status.StableRS = "different-hash"

		result := skipPrePromotionAnalysisRun(rollout, newRS, nil)
		assert.True(t, result, "Should skip when activeSelector is empty")
	})

	t.Run("should skip when activeSelector equals currentPodHash", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "preview")
		newRS := newReplicaSetWithStatus(rollout, 2, 2)
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		rollout.Status.BlueGreen.ActiveSelector = podHash
		rollout.Status.StableRS = "different-hash"

		result := skipPrePromotionAnalysisRun(rollout, newRS, nil)
		assert.True(t, result, "Should skip when activeSelector equals currentPodHash")
	})

	t.Run("should skip when currentPodHash is empty", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "preview")
		newRS := newReplicaSetWithStatus(rollout, 2, 2)
		newRS.Labels = nil // Remove labels to make podHash empty
		rollout.Status.BlueGreen.ActiveSelector = "some-hash"
		rollout.Status.StableRS = "different-hash"

		result := skipPrePromotionAnalysisRun(rollout, newRS, nil)
		assert.True(t, result, "Should skip when currentPodHash is empty")
	})

	t.Run("should not skip when currentAr is not nil", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "preview")
		newRS := newReplicaSetWithStatus(rollout, 2, 1) // Unsaturated
		rollout.Status.BlueGreen.ActiveSelector = "different-hash"
		rollout.Status.StableRS = "different-hash"
		currentAr := &v1alpha1.AnalysisRun{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-ar",
			},
		}

		result := skipPrePromotionAnalysisRun(rollout, newRS, currentAr)
		assert.False(t, result, "Should not skip when currentAr is not nil, even if ReplicaSet is unsaturated")
	})

	t.Run("should skip when currentAr is nil and ReplicaSet is not saturated", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "preview")
		newRS := newReplicaSetWithStatus(rollout, 2, 1) // Unsaturated: 2 desired, 1 available
		rollout.Status.BlueGreen.ActiveSelector = "different-hash"
		rollout.Status.StableRS = "different-hash"
		// Set up annotations for IsSaturated check
		if newRS.Annotations == nil {
			newRS.Annotations = make(map[string]string)
		}
		newRS.Annotations[annotations.DesiredReplicasAnnotation] = "2"

		result := skipPrePromotionAnalysisRun(rollout, newRS, nil)
		assert.True(t, result, "Should skip when currentAr is nil and ReplicaSet is not saturated")
	})

	t.Run("should not skip when currentAr is nil and ReplicaSet is saturated", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "preview")
		newRS := newReplicaSetWithStatus(rollout, 2, 2) // Saturated: 2 desired, 2 available
		rollout.Status.BlueGreen.ActiveSelector = "different-hash"
		rollout.Status.StableRS = "different-hash"
		// Set up annotations for IsSaturated check
		if newRS.Annotations == nil {
			newRS.Annotations = make(map[string]string)
		}
		newRS.Annotations[annotations.DesiredReplicasAnnotation] = "2"

		result := skipPrePromotionAnalysisRun(rollout, newRS, nil)
		assert.False(t, result, "Should not skip when currentAr is nil and ReplicaSet is saturated")
	})

	t.Run("should handle PreviewReplicaCount when currentAr is nil", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "preview")
		previewCount := int32(3)
		rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount = &previewCount
		newRS := newReplicaSetWithStatus(rollout, 3, 3) // Matches preview count
		rollout.Status.BlueGreen.ActiveSelector = "different-hash"
		rollout.Status.StableRS = "different-hash"

		result := skipPrePromotionAnalysisRun(rollout, newRS, nil)
		assert.False(t, result, "Should not skip when PreviewReplicaCount matches and ReplicaSet is saturated")

		// Test with unsaturated ReplicaSet
		newRS2 := newReplicaSetWithStatus(rollout, 3, 2) // Doesn't match preview count
		result2 := skipPrePromotionAnalysisRun(rollout, newRS2, nil)
		assert.True(t, result2, "Should skip when PreviewReplicaCount doesn't match")
	})

	t.Run("should not skip when currentAr is not nil even with PreviewReplicaCount mismatch", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "preview")
		previewCount := int32(3)
		rollout.Spec.Strategy.BlueGreen.PreviewReplicaCount = &previewCount
		newRS := newReplicaSetWithStatus(rollout, 3, 2) // Doesn't match preview count
		rollout.Status.BlueGreen.ActiveSelector = "different-hash"
		rollout.Status.StableRS = "different-hash"
		currentAr := &v1alpha1.AnalysisRun{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-ar",
			},
		}

		result := skipPrePromotionAnalysisRun(rollout, newRS, currentAr)
		assert.False(t, result, "Should not skip when currentAr is not nil, even with PreviewReplicaCount mismatch")
	})
}

// TestSkipPostPromotionAnalysisRun tests the skipPostPromotionAnalysisRun function
func TestSkipPostPromotionAnalysisRun(t *testing.T) {
	t.Run("should skip when StableRS equals currentPodHash", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "")
		newRS := newReplicaSetWithStatus(rollout, 2, 2)
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		rollout.Status.StableRS = podHash
		rollout.Status.BlueGreen.ActiveSelector = podHash

		result := skipPostPromotionAnalysisRun(rollout, newRS, nil)
		assert.True(t, result, "Should skip when StableRS equals currentPodHash")
	})

	t.Run("should skip when activeSelector does not equal currentPodHash", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "")
		newRS := newReplicaSetWithStatus(rollout, 2, 2)
		rollout.Status.BlueGreen.ActiveSelector = "different-hash"
		rollout.Status.StableRS = "different-hash"

		result := skipPostPromotionAnalysisRun(rollout, newRS, nil)
		assert.True(t, result, "Should skip when activeSelector does not equal currentPodHash")
	})

	t.Run("should skip when currentPodHash is empty", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "")
		newRS := newReplicaSetWithStatus(rollout, 2, 2)
		newRS.Labels = nil // Remove labels to make podHash empty
		rollout.Status.BlueGreen.ActiveSelector = "some-hash"
		rollout.Status.StableRS = "some-hash"

		result := skipPostPromotionAnalysisRun(rollout, newRS, nil)
		assert.True(t, result, "Should skip when currentPodHash is empty")
	})

	t.Run("should not skip when currentAr is not nil", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "")
		newRS := newReplicaSetWithStatus(rollout, 2, 1) // Unsaturated
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		rollout.Status.BlueGreen.ActiveSelector = podHash
		rollout.Status.StableRS = "different-hash"
		currentAr := &v1alpha1.AnalysisRun{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-ar",
			},
		}

		result := skipPostPromotionAnalysisRun(rollout, newRS, currentAr)
		assert.False(t, result, "Should not skip when currentAr is not nil, even if ReplicaSet is unsaturated")
	})

	t.Run("should skip when currentAr is nil and ReplicaSet is not saturated", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "")
		newRS := newReplicaSetWithStatus(rollout, 2, 1) // Unsaturated: 2 desired, 1 available
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		rollout.Status.BlueGreen.ActiveSelector = podHash
		rollout.Status.StableRS = "different-hash"
		// Set up annotations for IsSaturated check
		if newRS.Annotations == nil {
			newRS.Annotations = make(map[string]string)
		}
		newRS.Annotations[annotations.DesiredReplicasAnnotation] = "2"

		result := skipPostPromotionAnalysisRun(rollout, newRS, nil)
		assert.True(t, result, "Should skip when currentAr is nil and ReplicaSet is not saturated")
	})

	t.Run("should not skip when currentAr is nil and ReplicaSet is saturated", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "")
		newRS := newReplicaSetWithStatus(rollout, 2, 2) // Saturated: 2 desired, 2 available
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		rollout.Status.BlueGreen.ActiveSelector = podHash
		rollout.Status.StableRS = "different-hash"
		// Set up annotations for IsSaturated check
		if newRS.Annotations == nil {
			newRS.Annotations = make(map[string]string)
		}
		newRS.Annotations[annotations.DesiredReplicasAnnotation] = "2"

		result := skipPostPromotionAnalysisRun(rollout, newRS, nil)
		assert.False(t, result, "Should not skip when currentAr is nil and ReplicaSet is saturated")
	})

	t.Run("should not skip when currentAr is not nil even if ReplicaSet becomes unsaturated", func(t *testing.T) {
		rollout := newBlueGreenRollout("test", 2, nil, "active", "")
		newRS := newReplicaSetWithStatus(rollout, 2, 1) // Unsaturated
		podHash := replicasetutil.GetPodTemplateHash(newRS)
		rollout.Status.BlueGreen.ActiveSelector = podHash
		rollout.Status.StableRS = "different-hash"
		currentAr := &v1alpha1.AnalysisRun{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-ar",
			},
		}

		result := skipPostPromotionAnalysisRun(rollout, newRS, currentAr)
		assert.False(t, result, "Should not skip when currentAr is not nil, even if ReplicaSet becomes unsaturated")
	})
}
