package podannotation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
)

func routerScenario(t *testing.T, mode string) (*Reconciler, *v1alpha1.Rollout, *fake.Clientset) {
	t.Helper()
	ro := testRollout("green", "blue")
	objects := []runtime.Object{}
	for _, hash := range []string{"blue", "green", "old"} {
		objects = append(objects, testPod(hash, hash), testReplicaSet(hash, hash))
	}
	switch mode {
	case "promoted":
		ro.Status.StableRS = "green"
	case "abort":
		ro.Status.Abort = true
	case "override", "next-revision":
		ro.Annotations = map[string]string{annotations.TrafficOverrideHashAnnotation: "blue"}
		if mode == "next-revision" {
			ro.Annotations[annotations.TrafficOverrideSourceHashAnnotation] = "old"
		}
	case "pending", "adopted", "promoting":
		ro = testLegacyAdoptionRollout("green")
		objects = append(objects, testLegacyPod("legacy"), testLegacyReplicaSet("legacy"))
		if mode == "adopted" {
			ro.Annotations[annotations.LegacyAdoptionStateAnnotation] = annotations.LegacyAdoptionStatePromoted
		}
		if mode == "promoting" {
			ro.Annotations[annotations.LegacyAdoptionStateAnnotation] = annotations.LegacyAdoptionStatePromoting
		}
	}
	var config Config
	require.NoError(t, json.Unmarshal(ro.Spec.Strategy.Canary.TrafficRouting.Plugins[PluginName], &config))
	config.RoleLabelKey = "example.com/role"
	raw, err := json.Marshal(config)
	require.NoError(t, err)
	ro.Spec.Strategy.Canary.TrafficRouting.Plugins[PluginName] = raw
	client := fake.NewSimpleClientset(objects...)
	return NewReconciler(client), ro, client
}

// Fail each API call in a successful reconciliation in turn. A partial write
// must surface its error, and retrying it must converge without Pod lifecycle work.
func TestRouterAPIFailureRecovery(t *testing.T) {
	for _, mode := range []string{"canary", "promoted", "abort", "override", "next-revision", "pending", "adopted", "promoting"} {
		for _, operation := range []string{"hash", "weight", "remove", "verify"} {
			t.Run(mode+"/"+operation, func(t *testing.T) {
				call := func(r *Reconciler, ro *v1alpha1.Rollout) pluginTypes.RpcError {
					switch operation {
					case "hash":
						return r.UpdateHash(ro, ro.Status.CurrentPodHash, ro.Status.StableRS, nil)
					case "weight":
						return r.SetWeight(ro, 30, nil)
					case "remove":
						return r.RemoveManagedRoutes(ro)
					default:
						_, err := r.VerifyWeight(ro, 30, nil)
						return err
					}
				}
				setup := func() (*Reconciler, *v1alpha1.Rollout, *fake.Clientset) {
					r, ro, client := routerScenario(t, mode)
					if operation == "verify" {
						require.False(t, r.SetWeight(ro, 30, nil).HasError())
					}
					client.ClearActions()
					return r, ro, client
				}
				r, ro, client := setup()
				require.False(t, call(r, ro).HasError())
				calls := len(client.Actions())
				for failAt := 1; failAt <= calls; failAt++ {
					t.Run(fmt.Sprintf("api-%d", failAt), func(t *testing.T) {
						r, ro, client := setup()
						count := 0
						client.PrependReactor("*", "*", func(ktesting.Action) (bool, runtime.Object, error) {
							count++
							if count == failAt {
								return true, nil, errors.New("injected API outage")
							}
							return false, nil, nil
						})
						require.True(t, call(r, ro).HasError(), "must propagate API failure %d", failAt)
						require.Equal(t, failAt, count, "must stop after failed write/read")
						require.False(t, call(r, ro).HasError(), "retry must recover")
						assertNoPodLifecycleActions(t, client)
					})
				}
			})
		}
	}
}

func TestRouterRejectsUnsupportedOperationsAndInvalidConfig(t *testing.T) {
	r, ro, client := routerScenario(t, "canary")
	require.False(t, r.InitPlugin().HasError())
	require.Equal(t, PluginName, r.Type())
	require.True(t, r.SetHeaderRoute(ro, nil).HasError())
	require.True(t, r.SetMirrorRoute(ro, nil).HasError())
	extra := []v1alpha1.WeightDestination{{Weight: 1}}
	require.True(t, r.UpdateHash(ro, "green", "blue", extra).HasError())
	require.True(t, r.SetWeight(ro, 30, extra).HasError())
	verified, err := r.VerifyWeight(ro, 30, extra)
	require.True(t, err.HasError())
	require.Equal(t, pluginTypes.NotVerified, verified)
	require.Empty(t, client.Actions())
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	require.True(t, NewReconciler(nil).InitPlugin().HasError())

	for _, raw := range []string{
		`{`, `{}`, `{"annotationKey":"example.com/weight"}`,
		`{"annotationKey":"example.com/weight","baseSelector":"app=example","maxPodWeight":101}`,
		`{"annotationKey":"example.com/weight","baseSelector":"app=example","trafficWeightScale":-1}`,
		`{"annotationKey":"example.com/weight","baseSelector":"app=example","weightCalculation":"unknown"}`,
		`{"annotationKey":"example.com/weight","baseSelector":"app=example","patchPods":false,"patchReplicaSets":false}`,
		`{"annotationKey":"example.com/weight","baseSelector":"app=example","prettyJSON":true,"canaryWeightTemplate":"[broken"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			bad := ro.DeepCopy()
			bad.Spec.Strategy.Canary.TrafficRouting.Plugins[PluginName] = json.RawMessage(raw)
			require.True(t, r.UpdateHash(bad, "green", "blue", nil).HasError())
			require.True(t, r.SetWeight(bad, 30, nil).HasError())
			verified, err := r.VerifyWeight(bad, 30, nil)
			require.True(t, err.HasError())
			require.Equal(t, pluginTypes.NotVerified, verified)
			require.True(t, r.RemoveManagedRoutes(bad).HasError())
		})
	}
	for _, weight := range []int32{-1, 101} {
		require.True(t, r.SetWeight(ro, weight, nil).HasError())
	}
	for _, field := range []string{"stableWeightTemplate", "canaryWeightTemplate", "promotedWeightTemplate"} {
		config := map[string]interface{}{"annotationKey": testAnnotationKey, "baseSelector": "app=example", "prettyJSON": true, "inactiveWeightTemplate": "0", "stableWeightTemplate": defaultWeightPlaceholder, "canaryWeightTemplate": defaultWeightPlaceholder, "promotedWeightTemplate": defaultWeightPlaceholder}
		config[field] = "[broken"
		raw, err := json.Marshal(config)
		require.NoError(t, err)
		bad := ro.DeepCopy()
		bad.Spec.Strategy.Canary.TrafficRouting.Plugins[PluginName] = raw
		_, err = configForRollout(bad)
		require.ErrorContains(t, err, field)
	}
	require.Empty(t, client.Actions(), "invalid requests must not touch traffic")
}

func TestRouterDefaultsMissingHashesAndVerificationDrift(t *testing.T) {
	r, ro, client := routerScenario(t, "canary")
	ro.Spec.Strategy.Canary.TrafficRouting.Plugins[PluginName] = json.RawMessage(`{"annotationKey":"example.com/traffic-weight","baseSelector":"app=example"}`)
	config, err := configForRollout(ro)
	require.NoError(t, err)
	require.Equal(t, "0", config.InactiveWeightTemplate)
	require.Equal(t, 100, config.TrafficWeightScale)
	require.False(t, r.SetWeight(ro, 30, nil).HasError())
	verified, rpcErr := r.VerifyWeight(ro, 30, nil)
	require.False(t, rpcErr.HasError())
	require.Equal(t, pluginTypes.Verified, verified)
	stable, err := client.AppsV1().ReplicaSets(ro.Namespace).Get(context.Background(), "blue", metav1.GetOptions{})
	require.NoError(t, err)
	stable.Spec.Template.Annotations = nil
	_, err = client.AppsV1().ReplicaSets(ro.Namespace).Update(context.Background(), stable, metav1.UpdateOptions{})
	require.NoError(t, err)
	verified, rpcErr = r.VerifyWeight(ro, 30, nil)
	require.False(t, rpcErr.HasError())
	require.Equal(t, pluginTypes.NotVerified, verified)
	ro.Status.CurrentPodHash = ""
	require.True(t, r.SetWeight(ro, 30, nil).HasError())
	verified, rpcErr = r.VerifyWeight(ro, 30, nil)
	require.False(t, rpcErr.HasError())
	require.Equal(t, pluginTypes.NotVerified, verified)
	require.False(t, r.RemoveManagedRoutes(ro).HasError())
	ro.Spec.Strategy.Canary.TrafficRouting = nil
	_, err = configForRollout(ro)
	require.Error(t, err)
	ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{}
	_, err = configForRollout(ro)
	require.Error(t, err)
}

func TestLegacyRestoreAndVerificationFailureRecovery(t *testing.T) {
	ctx := context.Background()
	for _, operation := range []string{"restore", "delete", "promote", "pending", "verify-pending", "verify-promoted"} {
		t.Run(operation, func(t *testing.T) {
			setup := func() (*Reconciler, *v1alpha1.Rollout, *fake.Clientset) {
				r, ro, client := routerScenario(t, "pending")
				ro.Spec.WorkloadRef = &v1alpha1.ObjectRef{Kind: "Deployment", Name: "source"}
				require.NoError(t, client.Tracker().Add(&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: ro.Namespace}, Spec: appsv1.DeploymentSpec{Paused: true}}))
				if operation == "verify-pending" {
					require.NoError(t, r.ReconcileLegacyAdoptionPending(ctx, ro))
				} else {
					require.NoError(t, r.PromoteLegacyAdoption(ctx, ro))
				}
				if operation == "verify-promoted" {
					ro.Annotations[annotations.LegacyAdoptionStateAnnotation] = annotations.LegacyAdoptionStatePromoted
				}
				client.ClearActions()
				return r, ro, client
			}
			call := func(r *Reconciler, ro *v1alpha1.Rollout) error {
				switch operation {
				case "restore":
					return r.RestoreLegacyAdoption(ctx, ro)
				case "delete":
					return r.RestoreLegacyAdoptionForDeletion(ctx, ro)
				case "promote":
					return r.PromoteLegacyAdoption(ctx, ro)
				case "pending":
					return r.ReconcileLegacyAdoptionPending(ctx, ro)
				case "verify-pending":
					ok, err := r.VerifyLegacyAdoptionPending(ctx, ro)
					if err == nil {
						require.True(t, ok)
					}
					return err
				default:
					ok, err := r.VerifyLegacyAdoptionPromoted(ctx, ro)
					if err == nil {
						require.True(t, ok)
					}
					return err
				}
			}
			r, ro, client := setup()
			require.NoError(t, call(r, ro))
			for failAt := 1; failAt <= len(client.Actions()); failAt++ {
				r, ro, client := setup()
				count := 0
				client.PrependReactor("*", "*", func(ktesting.Action) (bool, runtime.Object, error) {
					count++
					if count == failAt {
						return true, nil, errors.New("API unavailable")
					}
					return false, nil, nil
				})
				require.ErrorContains(t, call(r, ro), "API unavailable", "API call %d", failAt)
				require.NoError(t, call(r, ro))
				assertNoPodLifecycleActions(t, client)
			}
			require.Error(t, call(r, nil))
			require.Error(t, call(r, testRollout("green", "blue")))
		})
	}
}
