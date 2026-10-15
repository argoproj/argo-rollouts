// Package traffic separates promotion orchestration from traffic resource writes.
package traffic

import (
	"context"
	"fmt"
	"maps"

	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	PodAnnotation   = "podAnnotation"
	ServiceSelector = "serviceSelector"
	SnapshotVersion = "v1"
)

// Target describes a retained traffic destination, not a desired PodTemplate.
type Target struct {
	Rollout *v1alpha1.Rollout
	Hash    string
	Config  map[string]string
}

type Result struct {
	ActivePods, InactivePods, ActiveReplicaSets, InactiveReplicaSets int
}

// Provider implementations must be idempotent, must not create/scale workloads,
// and must verify only the resources they own. Verify describes control-plane
// convergence, not measured traffic or connection draining at a load balancer.
type Provider interface {
	Validate(Target) error
	Capture(context.Context, Target) (*v1alpha1.RolloutPromotionTrafficState, error)
	ValidateRestore(context.Context, Target, *v1alpha1.RolloutPromotionTrafficState) error
	Apply(context.Context, Target) (Result, error)
	Verify(context.Context, Target) (bool, error)
	Restore(context.Context, Target, *v1alpha1.RolloutPromotionTrafficState) (Result, error)
}

// Adoption is an optional capability, not a requirement of all traffic adapters.
type Adoption interface {
	PromoteAdoption(context.Context, *v1alpha1.Rollout) error
	RestoreAdoption(context.Context, *v1alpha1.Rollout) error
	VerifyAdoption(context.Context, *v1alpha1.Rollout) (bool, error)
}

// Registry is fixed at controller construction; requests cannot load executable code.
type Registry map[string]Provider

func NewRegistry(client kubernetes.Interface) Registry {
	return Registry{
		PodAnnotation:   &AnnotationProvider{client: client},
		ServiceSelector: &ServiceProvider{client: client},
	}
}

func (r Registry) Resolve(name string) (Provider, error) {
	p, ok := r[name]
	if !ok || p == nil {
		return nil, fmt.Errorf("unsupported traffic provider %q", name)
	}
	return p, nil
}

func Snapshot(provider string, target Target, data map[string]string) *v1alpha1.RolloutPromotionTrafficState {
	return &v1alpha1.RolloutPromotionTrafficState{
		Provider: provider, Version: SnapshotVersion,
		RolloutUID: string(target.Rollout.UID), Config: maps.Clone(target.Config), Data: maps.Clone(data),
	}
}

func ValidateSnapshot(provider string, target Target, saved *v1alpha1.RolloutPromotionTrafficState) error {
	if saved == nil || saved.Provider != provider || saved.Version != SnapshotVersion {
		return fmt.Errorf("unsupported or missing %s traffic snapshot version", provider)
	}
	if saved.RolloutUID == "" || saved.RolloutUID != string(target.Rollout.UID) {
		return fmt.Errorf("traffic snapshot belongs to a different or unknown Rollout UID")
	}
	return nil
}
