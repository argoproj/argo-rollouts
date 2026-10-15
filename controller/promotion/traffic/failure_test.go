package traffic

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
)

func TestProviderAPIFailuresAreNotSuccessfulTrafficChanges(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{PodAnnotation, ServiceSelector} {
		for _, operation := range []string{"capture", "restore", "apply", "verify"} {
			t.Run(name+"/"+operation, func(t *testing.T) {
				setup := func() (Provider, Target, *v1alpha1.RolloutPromotionTrafficState, *kubefake.Clientset) {
					target, svc, sets, pods := fixture()
					if name == ServiceSelector {
						target.Config = nil
					}
					client := kubefake.NewSimpleClientset(svc, sets[0], sets[1], pods[0], pods[1])
					provider, err := NewRegistry(client).Resolve(name)
					require.NoError(t, err)
					state, err := provider.Capture(ctx, target)
					require.NoError(t, err)
					if operation == "verify" {
						_, err = provider.Apply(ctx, target)
						require.NoError(t, err)
					}
					client.ClearActions()
					return provider, target, state, client
				}
				call := func(p Provider, target Target, state *v1alpha1.RolloutPromotionTrafficState) error {
					switch operation {
					case "capture":
						_, err := p.Capture(ctx, target)
						return err
					case "restore":
						_, err := p.Restore(ctx, target, state)
						return err
					case "apply":
						_, err := p.Apply(ctx, target)
						return err
					default:
						ok, err := p.Verify(ctx, target)
						if err != nil {
							require.False(t, ok)
						} else {
							require.True(t, ok)
						}
						return err
					}
				}
				p, target, state, client := setup()
				require.NoError(t, call(p, target, state))
				for failAt := 1; failAt <= len(client.Actions()); failAt++ {
					t.Run(fmt.Sprintf("api-%d", failAt), func(t *testing.T) {
						p, target, state, client := setup()
						count := 0
						client.PrependReactor("*", "*", func(ktesting.Action) (bool, runtime.Object, error) {
							count++
							if count == failAt {
								return true, nil, errors.New("injected API failure")
							}
							return false, nil, nil
						})
						require.ErrorContains(t, call(p, target, state), "injected API failure")
						require.Equal(t, failAt, count)
						require.NoError(t, call(p, target, state), "retry must converge")
						for _, a := range client.Actions() {
							require.NotContains(t, []string{"create", "delete", "update"}, a.GetVerb())
						}
					})
				}
			})
		}
	}
}

func TestServiceProviderRejectsChangedIdentityAndUnrelatedSelectors(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"manager", "selector", "empty-selector", "invalid-selector", "empty-hash", "different-hash", "replaced-during-restore"} {
		t.Run(scenario, func(t *testing.T) {
			target, svc, _, _ := fixture()
			target.Config = nil
			client := kubefake.NewSimpleClientset(svc)
			p := &ServiceProvider{client: client}
			state, err := p.Capture(ctx, target)
			require.NoError(t, err)
			switch scenario {
			case "manager":
				svc.Annotations = map[string]string{v1alpha1.ManagedByRolloutsKey: "unrelated"}
			case "selector":
				svc.Spec.Selector["app"] = "unrelated"
			case "empty-selector":
				svc.Spec.Selector = nil
			case "invalid-selector":
				target.Rollout.Spec.Selector.MatchExpressions = []metav1.LabelSelectorRequirement{{Key: "app", Operator: "invalid"}}
			case "empty-hash":
				target.Hash = ""
			case "different-hash":
				target.Hash = "other"
			case "replaced-during-restore":
				calls := 0
				client.PrependReactor("get", "services", func(ktesting.Action) (bool, runtime.Object, error) {
					calls++
					current := svc.DeepCopy()
					if calls > 1 {
						current.UID = "new-service"
					}
					return true, current, nil
				})
			}
			require.NoError(t, client.Tracker().Update(corev1.SchemeGroupVersion.WithResource("services"), svc, "test"))
			client.ClearActions()
			_, err = p.Restore(ctx, target, state)
			require.Error(t, err)
			for _, a := range client.Actions() {
				require.NotEqual(t, "patch", a.GetVerb())
			}
			if scenario != "replaced-during-restore" {
				_, err = p.Capture(ctx, target)
				require.Error(t, err)
			}
		})
	}
}

func TestAnnotationValidationAndSnapshotGuards(t *testing.T) {
	ctx := context.Background()
	target, _, sets, pods := fixture()
	client := kubefake.NewSimpleClientset(sets[0], sets[1], pods[0], pods[1])
	p := &AnnotationProvider{client: client}
	for _, config := range []map[string]string{
		{"annotationKey": "example.com/weight"},
		{"annotationKey": "example.com/weight", "baseSelector": "broken in (", "activeWeight": "10", "inactiveWeight": "0"},
		{"annotationKey": "example.com/weight", "baseSelector": "app=demo", "activeWeight": "10", "inactiveWeight": "0", "reapplyDelaySeconds": "not-a-number"},
	} {
		bad := target
		bad.Config = config
		_, err := p.Capture(ctx, bad)
		require.Error(t, err)
		_, err = p.Apply(ctx, bad)
		require.Error(t, err)
		ok, err := p.Verify(ctx, bad)
		require.Error(t, err)
		require.False(t, ok)
	}
	saved, err := p.Capture(ctx, target)
	require.NoError(t, err)
	for _, state := range []*v1alpha1.RolloutPromotionTrafficState{nil, {Provider: "unknown"}, {Provider: PodAnnotation, Version: SnapshotVersion, RolloutUID: "other"}} {
		_, err = p.Restore(ctx, target, state)
		require.Error(t, err)
	}
	saved.Data["activeHash"] = "different"
	_, err = p.Restore(ctx, target, saved)
	require.Error(t, err)
	target.Hash = ""
	_, err = p.Apply(ctx, target)
	require.Error(t, err)
	require.Empty(t, client.Actions())
}

func TestAnnotationVerificationDetectsDriftAndReapply(t *testing.T) {
	ctx := context.Background()
	target, _, sets, pods := fixture()
	client := kubefake.NewSimpleClientset(sets[0], sets[1], pods[0], pods[1])
	p := &AnnotationProvider{client: client}
	ok, err := p.Verify(ctx, target)
	require.NoError(t, err)
	require.False(t, ok)
	target.Config["reapplyDelaySeconds"] = "1"
	result, err := p.Apply(ctx, target)
	require.NoError(t, err)
	require.Equal(t, 1, result.ActivePods)
	patches := 0
	for _, a := range client.Actions() {
		if a.GetVerb() == "patch" {
			patches++
		}
	}
	require.Equal(t, 8, patches, "must reapply both templates and both Pods")
	live, err := client.CoreV1().Pods("test").Get(ctx, "blue", metav1.GetOptions{})
	require.NoError(t, err)
	live.Annotations = nil
	_, err = client.CoreV1().Pods("test").Update(ctx, live, metav1.UpdateOptions{})
	require.NoError(t, err)
	ok, err = p.Verify(ctx, target)
	require.NoError(t, err)
	require.False(t, ok, "a correct template does not prove a live Pod is correctly routed")
}

func TestAnnotationAdoptionCapability(t *testing.T) {
	ctx := context.Background()
	target, _, sets, pods := fixture()
	target.Rollout.Status.CurrentPodHash = "blue"
	target.Rollout.Annotations = map[string]string{
		annotations.LegacyAdoptionStateAnnotation:            annotations.LegacyAdoptionStatePromoted,
		annotations.LegacyAdoptionBaseSelectorAnnotation:     "app=demo",
		annotations.LegacyAdoptionWeightAnnotationAnnotation: "example.com/weight",
		annotations.LegacyAdoptionActiveWeightAnnotation:     "10",
		annotations.LegacyAdoptionCandidateWeightAnnotation:  "0",
		annotations.LegacyAdoptionLegacyWeightAnnotation:     "10",
		annotations.LegacyAdoptionInactiveWeightAnnotation:   "0",
	}
	p := &AnnotationProvider{client: kubefake.NewSimpleClientset(sets[0], sets[1], pods[0], pods[1])}
	require.NoError(t, p.PromoteAdoption(ctx, target.Rollout))
	ok, err := p.VerifyAdoption(ctx, target.Rollout)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, p.RestoreAdoption(ctx, target.Rollout))
	ok, err = p.VerifyAdoption(ctx, target.Rollout)
	require.NoError(t, err)
	require.False(t, ok)
}
