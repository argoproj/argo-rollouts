package promotion

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/controller/promotion/traffic"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutfake "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/utils/annotations"
)

type unverifiedProvider struct{ traffic.Provider }

func (p unverifiedProvider) Verify(context.Context, traffic.Target) (bool, error) {
	return false, nil
}

// A provider with no Kubernetes traffic resources exercises the extension contract.
type opaqueProvider struct {
	traffic.Provider
	restored bool
	value    string
}

func (p *opaqueProvider) Validate(traffic.Target) error { return nil }
func (p *opaqueProvider) Restore(_ context.Context, t traffic.Target, saved *v1alpha1.RolloutPromotionTrafficState) (traffic.Result, error) {
	p.restored = saved.Version == "custom-v2" && saved.Data["route"] == "opaque-route"
	p.value = t.Hash
	return traffic.Result{}, nil
}
func (p *opaqueProvider) Verify(_ context.Context, t traffic.Target) (bool, error) {
	return p.restored && p.value == t.Hash, nil
}

func TestCustomTrafficProviderDoesNotRequireAnnotations(t *testing.T) {
	ro := newCanaryRollout("test-ns", "demo", "green", "blue", 1, nil)
	kube := kubefake.NewSimpleClientset()
	provider := &opaqueProvider{}
	controller := &Controller{
		kubeClient: kube, dynamicClient: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), ro),
		trafficProviders: traffic.Registry{"customRouter": provider},
	}
	target := Target{Name: "demo", Traffic: &v1alpha1.RolloutPromotionTraffic{Provider: "customRouter"},
		TrafficSnapshot: &v1alpha1.RolloutPromotionTrafficState{Provider: "customRouter", Version: "custom-v2", Data: map[string]string{"route": "opaque-route"}}}
	state, err := controller.applyTargetTrafficForHash(context.Background(), "test-ns", target, map[string]interface{}{}, "blue")
	require.NoError(t, err)
	require.Equal(t, "customRouter", state["trafficProvider"])
	require.Equal(t, true, state["trafficVerified"])
	require.True(t, provider.restored)
	require.Empty(t, kube.Actions())
}

func TestTrafficRollbackRetriesVerificationAfterRestart(t *testing.T) {
	for _, oldMarker := range []bool{false, true} {
		name := "unverified"
		if oldMarker {
			name = "legacy-premature-marker"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			ro := newRollout("test-ns", "demo", "green", "green", "green", nil)
			ro.Object["spec"] = map[string]interface{}{
				"selector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": "example", "component": "data"}},
				"strategy": map[string]interface{}{"blueGreen": map[string]interface{}{"activeService": "active"}},
			}
			rs := newReplicaSet("test-ns", "old", "blue")
			rs.Spec.Replicas, rs.Status.AvailableReplicas = ptr.To(int32(1)), 1
			pod := newPod("test-ns", "old", "blue")
			pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
			svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "active", Namespace: "test-ns", UID: "svc-1"},
				Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "example", "component": "data", rolloutHashLabel: "green"}}}
			promotion := &v1alpha1.RolloutPromotion{
				ObjectMeta: metav1.ObjectMeta{Name: "restore", Namespace: "test-ns", Generation: 1,
					Annotations: map[string]string{annotations.TrafficRollbackRequestAnnotation: "rollback-1"}},
				Spec: v1alpha1.RolloutPromotionSpec{
					RequestID: "forward-1", Strategy: "blueGreen",
					TrafficRollback: &v1alpha1.RolloutPromotionTrafficRollback{Enabled: ptr.To(true)},
					Rollouts:        []v1alpha1.RolloutPromotionTarget{{Name: "demo", Traffic: &v1alpha1.RolloutPromotionTraffic{Provider: "serviceSelector"}}},
				},
				Status: v1alpha1.RolloutPromotionStatus{
					ProcessedRequestIDs: []string{"forward-1"},
					TrafficSnapshots: []v1alpha1.RolloutPromotionTrafficSnapshot{{ID: "snapshot-1", RequestID: "forward-1",
						Targets: []v1alpha1.RolloutPromotionTrafficSnapshotTarget{{Name: "demo", Namespace: "test-ns", Strategy: "blueGreen", ActiveHash: "blue",
							Traffic: &v1alpha1.RolloutPromotionTrafficState{Provider: "serviceSelector", Version: "v1", RolloutUID: "rollout-uid",
								Data: map[string]string{"service": "active", "serviceUID": "svc-1", "activeHash": "blue"}},
						}},
					}},
				},
			}
			kube := kubefake.NewSimpleClientset(rs, pod, svc)
			dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), ro)
			client := rolloutfake.NewSimpleClientset(promotion)
			informer := cache.NewSharedIndexInformer(nil, &v1alpha1.RolloutPromotion{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
			require.NoError(t, informer.GetIndexer().Add(promotion))
			registry := traffic.NewRegistry(kube)
			registry["serviceSelector"] = unverifiedProvider{registry["serviceSelector"]}
			controller := NewController("test-ns", kube, dynamic, client, informer, WithTrafficProviders(registry))
			require.Error(t, controller.reconcile(ctx, "test-ns/restore"))
			live, err := client.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(ctx, "restore", metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, v1alpha1.RolloutPromotionOperationStalled, live.Status.Operation.Phase)
			require.Empty(t, live.Status.LastTrafficRollbackRequest)
			if oldMarker {
				live.Status.LastTrafficRollbackRequest = "rollback-1"
				_, err = client.ArgoprojV1alpha1().RolloutPromotions("test-ns").UpdateStatus(ctx, live, metav1.UpdateOptions{})
				require.NoError(t, err)
			}
			// A new process uses only persisted checkpoints and the live provider.
			restarted := NewController("test-ns", kube, dynamic, client, informer)
			require.NoError(t, restarted.reconcile(ctx, "test-ns/restore"))
			live, err = client.ArgoprojV1alpha1().RolloutPromotions("test-ns").Get(ctx, "restore", metav1.GetOptions{})
			require.NoError(t, err)
			require.Equal(t, v1alpha1.RolloutPromotionPhaseSucceeded, live.Status.Phase)
			require.Equal(t, "rollback-1", live.Status.LastTrafficRollbackRequest)
			before := len(kube.Actions())
			require.NoError(t, restarted.reconcile(ctx, "test-ns/restore"))
			require.Len(t, kube.Actions(), before)
			for _, action := range kube.Actions() {
				require.NotContains(t, []string{"create", "delete", "update"}, action.GetVerb())
				if action.GetVerb() == "patch" {
					require.Equal(t, "services", action.GetResource().Resource)
				}
			}
		})
	}
}

func TestTrafficConfigurationCompatibility(t *testing.T) {
	newConfig := &v1alpha1.RolloutPromotionTraffic{Provider: "serviceSelector"}
	spec, err := normalizeSpec(v1alpha1.RolloutPromotionSpec{
		TrafficRollback: &v1alpha1.RolloutPromotionTrafficRollback{Enabled: ptr.To(true)},
		Rollouts:        []v1alpha1.RolloutPromotionTarget{{Name: "demo", Traffic: newConfig}},
	})
	require.NoError(t, err)
	require.Nil(t, spec.Targets[0].WeightSwitch)
	require.Equal(t, "serviceSelector", trafficConfig(spec.Targets[0]).Provider)
	_, err = normalizeSpec(v1alpha1.RolloutPromotionSpec{Rollouts: []v1alpha1.RolloutPromotionTarget{{
		Name: "demo", Traffic: newConfig, WeightSwitch: &v1alpha1.RolloutPromotionWeightSwitch{Enabled: ptr.To(false)},
	}}})
	require.ErrorContains(t, err, "mutually exclusive")
	legacy := targetFromTrafficSnapshot(v1alpha1.RolloutPromotionTrafficSnapshotTarget{
		Name: "demo", ActiveHash: "blue", AnnotationKey: "example.com/weight", BaseSelector: "app=demo", ActiveWeight: "10", InactiveWeight: "0",
	})
	require.Equal(t, "podAnnotation", trafficConfig(legacy).Provider)
	require.Equal(t, "blue", legacy.ExpectedStableRS)
	require.Nil(t, legacy.TrafficSnapshot)
}

func TestServiceSnapshotRollbackThroughController(t *testing.T) {
	ctx := context.Background()
	ro := newRollout("test-ns", "demo", "green", "green", "green", nil)
	ro.Object["spec"] = map[string]interface{}{
		"selector": map[string]interface{}{"matchLabels": map[string]interface{}{"app": "example", "component": "data"}},
		"strategy": map[string]interface{}{"blueGreen": map[string]interface{}{"activeService": "active"}},
	}
	stable := newReplicaSet("test-ns", "old", "blue")
	stable.Spec.Replicas = ptr.To(int32(1))
	stable.Status.AvailableReplicas = 1
	pod := newPod("test-ns", "old", "blue")
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "active", Namespace: "test-ns", UID: "svc-1"},
		Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "example", "component": "data", rolloutHashLabel: "green"}}}
	kube := kubefake.NewSimpleClientset(stable, pod, service)
	dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), ro)
	controller := &Controller{kubeClient: kube, dynamicClient: dynamic}
	target := targetFromTrafficSnapshot(v1alpha1.RolloutPromotionTrafficSnapshotTarget{
		Name: "demo", Namespace: "test-ns", Strategy: "blueGreen", ActiveHash: "blue",
		Traffic: &v1alpha1.RolloutPromotionTrafficState{
			Provider: "serviceSelector", Version: "v1", RolloutUID: "rollout-uid",
			Data: map[string]string{"service": "active", "serviceUID": "svc-1", "activeHash": "blue"},
		},
	})
	state, err := controller.rollbackTrafficTarget(ctx, "test-ns", target, "blue")
	require.NoError(t, err)
	require.Equal(t, true, state["trafficRollbackApplied"])
	live, err := kube.CoreV1().Services("test-ns").Get(ctx, "active", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "blue", live.Spec.Selector[rolloutHashLabel])
	r, err := dynamic.Resource(v1alpha1.RolloutGVR).Namespace("test-ns").Get(ctx, "demo", metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, "blue", r.GetAnnotations()[annotations.TrafficOverrideHashAnnotation])
	for _, action := range kube.Actions() {
		if action.GetVerb() == "patch" {
			require.Equal(t, "services", action.GetResource().Resource)
		}
		require.NotContains(t, []string{"create", "delete", "update"}, action.GetVerb())
	}
	// A newer controller can restore old snapshots, but new snapshots bind UID.
	target.TrafficSnapshot.RolloutUID = "different-rollout"
	before := len(dynamic.Actions())
	_, err = controller.rollbackTrafficTarget(ctx, "test-ns", target, "blue")
	require.Error(t, err)
	for _, action := range dynamic.Actions()[before:] {
		require.NotEqual(t, "patch", action.GetVerb())
	}
}

func TestUnsupportedProviderFailsBeforeChangingRollout(t *testing.T) {
	ro := newCanaryRollout("test-ns", "demo", "green", "blue", 1, nil)
	dynamic := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), ro)
	controller := &Controller{dynamicClient: dynamic, kubeClient: kubefake.NewSimpleClientset()}
	for _, provider := range []string{"not-installed", "serviceSelector"} {
		targets := []Target{{
			Name: "demo", Traffic: &v1alpha1.RolloutPromotionTraffic{Provider: provider},
		}}
		err := controller.prepareForwardPromotion(context.Background(), "test-ns", targets)
		require.Error(t, err)
		err = controller.preflightControlAction(context.Background(), "test-ns", RequestSpec{Action: "abort", Targets: targets})
		require.Error(t, err)
	}
	for _, action := range dynamic.Actions() {
		require.NotEqual(t, "patch", action.GetVerb())
	}
}
