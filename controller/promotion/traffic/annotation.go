package traffic

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/podannotation"
)

type AnnotationProvider struct{ client kubernetes.Interface }

func (p *AnnotationProvider) Validate(t Target) error {
	for key := range t.Config {
		switch key {
		case "annotationKey", "baseSelector", "activeWeight", "inactiveWeight", "reapplyDelaySeconds":
		default:
			return fmt.Errorf("unknown podAnnotation configuration key %q", key)
		}
	}
	if errors := validation.IsQualifiedName(t.Config["annotationKey"]); len(errors) != 0 {
		return fmt.Errorf("invalid podAnnotation annotationKey: %v", errors)
	}
	if strings.TrimSpace(t.Config["baseSelector"]) == "" || t.Config["activeWeight"] == "" || t.Config["inactiveWeight"] == "" {
		return fmt.Errorf("podAnnotation requires baseSelector, activeWeight and inactiveWeight")
	}
	if _, err := labels.Parse(t.Config["baseSelector"]); err != nil {
		return err
	}
	_, err := annotationDelay(t.Config)
	return err
}

func annotationDelay(config map[string]string) (time.Duration, error) {
	if config["reapplyDelaySeconds"] == "" {
		return 0, nil
	}
	seconds, err := strconv.ParseInt(config["reapplyDelaySeconds"], 10, 32)
	if err != nil || seconds < 0 {
		return 0, fmt.Errorf("reapplyDelaySeconds must be a nonnegative integer")
	}
	return time.Duration(seconds) * time.Second, nil
}

func (p *AnnotationProvider) Capture(ctx context.Context, t Target) (*v1alpha1.RolloutPromotionTrafficState, error) {
	if err := p.Validate(t); err != nil {
		return nil, err
	}
	return Snapshot(PodAnnotation, t, map[string]string{"activeHash": t.Hash}), nil
}

func (p *AnnotationProvider) ValidateRestore(ctx context.Context, t Target, saved *v1alpha1.RolloutPromotionTrafficState) error {
	if err := ValidateSnapshot(PodAnnotation, t, saved); err != nil {
		return err
	}
	if t.Hash == "" || saved.Data["activeHash"] != t.Hash {
		return fmt.Errorf("annotation snapshot target hash mismatch")
	}
	t.Config = saved.Config
	return p.Validate(t)
}

func (p *AnnotationProvider) Restore(ctx context.Context, t Target, saved *v1alpha1.RolloutPromotionTrafficState) (Result, error) {
	if err := p.ValidateRestore(ctx, t, saved); err != nil {
		return Result{}, err
	}
	t.Config = saved.Config
	return p.Apply(ctx, t)
}

// Only objects in this Rollout's ownership chain are mutable, even if another
// workload accidentally uses the same labels or a caller supplies a broad selector.
func (p *AnnotationProvider) objects(ctx context.Context, t Target) ([]appsv1.ReplicaSet, []corev1.Pod, error) {
	sets, err := p.client.AppsV1().ReplicaSets(t.Rollout.Namespace).List(ctx, metav1.ListOptions{LabelSelector: t.Config["baseSelector"]})
	if err != nil {
		return nil, nil, err
	}
	owned := map[types.UID]bool{}
	var rs []appsv1.ReplicaSet
	for _, item := range sets.Items {
		if metav1.IsControlledBy(&item, t.Rollout) && item.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] != "" {
			rs = append(rs, item)
			owned[item.UID] = true
		}
	}
	items, err := p.client.CoreV1().Pods(t.Rollout.Namespace).List(ctx, metav1.ListOptions{LabelSelector: t.Config["baseSelector"]})
	if err != nil {
		return nil, nil, err
	}
	var pods []corev1.Pod
	for _, item := range items.Items {
		owner := metav1.GetControllerOf(&item)
		if owner != nil && owned[owner.UID] && item.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] != "" && item.DeletionTimestamp == nil {
			pods = append(pods, item)
		}
	}
	return rs, pods, nil
}

func (p *AnnotationProvider) Apply(ctx context.Context, t Target) (Result, error) {
	if err := p.Validate(t); err != nil {
		return Result{}, err
	}
	if t.Hash == "" {
		return Result{}, fmt.Errorf("traffic target hash is required")
	}
	result, err := p.applyOnce(ctx, t)
	if err != nil {
		return result, err
	}
	delay, _ := annotationDelay(t.Config)
	if delay > 0 {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(delay):
		}
		return p.applyOnce(ctx, t)
	}
	return result, nil
}

func (p *AnnotationProvider) applyOnce(ctx context.Context, t Target) (Result, error) {
	rs, pods, err := p.objects(ctx, t)
	if err != nil {
		return Result{}, err
	}
	result := Result{}
	value := func(hash string) string {
		if hash == t.Hash {
			return t.Config["activeWeight"]
		}
		return t.Config["inactiveWeight"]
	}
	// Update templates first so replacement Pods inherit the selected traffic state.
	for _, item := range rs {
		patch, _ := json.Marshal(map[string]interface{}{
			"metadata": map[string]interface{}{"uid": item.UID, "resourceVersion": item.ResourceVersion},
			"spec":     map[string]interface{}{"template": map[string]interface{}{"metadata": map[string]interface{}{"annotations": map[string]string{t.Config["annotationKey"]: value(item.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])}}}},
		})
		if _, err := p.client.AppsV1().ReplicaSets(item.Namespace).Patch(ctx, item.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return result, err
		}
		if item.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] == t.Hash {
			result.ActiveReplicaSets++
		} else {
			result.InactiveReplicaSets++
		}
	}
	for _, item := range pods {
		patch, _ := json.Marshal(map[string]interface{}{"metadata": map[string]interface{}{
			"uid": item.UID, "resourceVersion": item.ResourceVersion,
			"annotations": map[string]string{t.Config["annotationKey"]: value(item.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])},
		}})
		if _, err := p.client.CoreV1().Pods(item.Namespace).Patch(ctx, item.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
			return result, err
		}
		if item.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] == t.Hash {
			result.ActivePods++
		} else {
			result.InactivePods++
		}
	}
	return result, nil
}

func (p *AnnotationProvider) Verify(ctx context.Context, t Target) (bool, error) {
	if err := p.Validate(t); err != nil {
		return false, err
	}
	rs, pods, err := p.objects(ctx, t)
	if err != nil {
		return false, err
	}
	check := func(hash, actual string) bool {
		expected := t.Config["inactiveWeight"]
		if hash == t.Hash {
			expected = t.Config["activeWeight"]
		}
		if actual == expected {
			return true
		}
		var actualJSON, expectedJSON interface{}
		return json.Unmarshal([]byte(actual), &actualJSON) == nil && json.Unmarshal([]byte(expected), &expectedJSON) == nil && reflect.DeepEqual(actualJSON, expectedJSON)
	}
	found := false
	for _, item := range rs {
		if !check(item.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], item.Spec.Template.Annotations[t.Config["annotationKey"]]) {
			return false, nil
		}
	}
	for _, item := range pods {
		if item.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] == t.Hash {
			found = true
		}
		if !check(item.Labels[v1alpha1.DefaultRolloutUniqueLabelKey], item.Annotations[t.Config["annotationKey"]]) {
			return false, nil
		}
	}
	return found, nil
}

func (p *AnnotationProvider) PromoteAdoption(ctx context.Context, ro *v1alpha1.Rollout) error {
	return podannotation.NewReconciler(p.client).PromoteLegacyAdoption(ctx, ro)
}
func (p *AnnotationProvider) RestoreAdoption(ctx context.Context, ro *v1alpha1.Rollout) error {
	return podannotation.NewReconciler(p.client).RestoreLegacyAdoption(ctx, ro)
}
func (p *AnnotationProvider) VerifyAdoption(ctx context.Context, ro *v1alpha1.Rollout) (bool, error) {
	return podannotation.NewReconciler(p.client).VerifyLegacyAdoptionPromoted(ctx, ro)
}
