package traffic

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// ServiceProvider restores the declared blue-green active Service. It does not
// implement weighted canary routing or claim to drain established connections.
type ServiceProvider struct{ client kubernetes.Interface }

func (p *ServiceProvider) Validate(t Target) error {
	if len(t.Config) != 0 {
		return fmt.Errorf("serviceSelector does not accept configuration; it uses the Rollout's activeService")
	}
	if t.Rollout.Spec.Strategy.BlueGreen == nil || t.Rollout.Spec.Strategy.BlueGreen.ActiveService == "" {
		return fmt.Errorf("serviceSelector requires a blueGreen Rollout with an activeService; weighted canary routing is unsupported")
	}
	return nil
}

func (p *ServiceProvider) service(ctx context.Context, t Target) (*corev1.Service, error) {
	if err := p.Validate(t); err != nil {
		return nil, err
	}
	svc, err := p.client.CoreV1().Services(t.Rollout.Namespace).Get(ctx, t.Rollout.Spec.Strategy.BlueGreen.ActiveService, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if managed := svc.Annotations[v1alpha1.ManagedByRolloutsKey]; managed != "" && managed != t.Rollout.Name {
		return nil, fmt.Errorf("active Service is managed by another Rollout")
	}
	selector := maps.Clone(svc.Spec.Selector)
	delete(selector, v1alpha1.DefaultRolloutUniqueLabelKey)
	workloadSelector, err := metav1.LabelSelectorAsSelector(t.Rollout.Spec.Selector)
	if err != nil {
		return nil, err
	}
	if len(selector) == 0 || workloadSelector.Empty() || !workloadSelector.Matches(labels.Set(selector)) {
		return nil, fmt.Errorf("active Service selector does not match this Rollout")
	}
	return svc, nil
}

func (p *ServiceProvider) Capture(ctx context.Context, t Target) (*v1alpha1.RolloutPromotionTrafficState, error) {
	svc, err := p.service(ctx, t)
	if err != nil {
		return nil, err
	}
	if t.Hash == "" || svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] != t.Hash {
		return nil, fmt.Errorf("active Service no longer selects the snapshot target")
	}
	return Snapshot(ServiceSelector, t, map[string]string{"service": svc.Name, "serviceUID": string(svc.UID), "activeHash": t.Hash}), nil
}

func (p *ServiceProvider) ValidateRestore(ctx context.Context, t Target, saved *v1alpha1.RolloutPromotionTrafficState) error {
	if err := ValidateSnapshot(ServiceSelector, t, saved); err != nil {
		return err
	}
	svc, err := p.service(ctx, t)
	if err != nil {
		return err
	}
	if saved.Data["service"] != svc.Name || saved.Data["serviceUID"] == "" || saved.Data["serviceUID"] != string(svc.UID) || t.Hash == "" || saved.Data["activeHash"] != t.Hash {
		return fmt.Errorf("serviceSelector snapshot identity or destination changed")
	}
	return nil
}

func (p *ServiceProvider) Restore(ctx context.Context, t Target, saved *v1alpha1.RolloutPromotionTrafficState) (Result, error) {
	if err := p.ValidateRestore(ctx, t, saved); err != nil {
		return Result{}, err
	}
	svc, err := p.service(ctx, t)
	if err != nil {
		return Result{}, err
	}
	if string(svc.UID) != saved.Data["serviceUID"] {
		return Result{}, fmt.Errorf("active Service changed during restore")
	}
	return p.patch(ctx, t, svc)
}

func (p *ServiceProvider) Apply(ctx context.Context, t Target) (Result, error) {
	svc, err := p.service(ctx, t)
	if err != nil {
		return Result{}, err
	}
	return p.patch(ctx, t, svc)
}

func (p *ServiceProvider) patch(ctx context.Context, t Target, svc *corev1.Service) (Result, error) {
	if t.Hash == "" {
		return Result{}, fmt.Errorf("traffic target hash is required")
	}
	patch, _ := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{"uid": svc.UID, "resourceVersion": svc.ResourceVersion},
		"spec":     map[string]interface{}{"selector": map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: t.Hash}},
	})
	_, err := p.client.CoreV1().Services(svc.Namespace).Patch(ctx, svc.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	return Result{}, err
}

func (p *ServiceProvider) Verify(ctx context.Context, t Target) (bool, error) {
	svc, err := p.service(ctx, t)
	if err != nil {
		return false, err
	}
	return svc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey] == t.Hash, nil
}
