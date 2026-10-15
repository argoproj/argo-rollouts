package traffic

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func fixture() (Target, *corev1.Service, []*appsv1.ReplicaSet, []*corev1.Pod) {
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "test", UID: "rollout-1"},
		Spec: v1alpha1.RolloutSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "demo"}},
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "demo"}}},
			Strategy: v1alpha1.RolloutStrategy{BlueGreen: &v1alpha1.BlueGreenStrategy{ActiveService: "active"}},
		},
	}
	target := Target{Rollout: ro, Hash: "blue", Config: map[string]string{
		"annotationKey": "example.com/weight", "baseSelector": "app=demo",
		"activeWeight": "[{\"weight\":10}]", "inactiveWeight": "[{\"weight\":0}]",
	}}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "active", Namespace: "test", UID: "service-1"},
		Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": "demo", v1alpha1.DefaultRolloutUniqueLabelKey: "blue"}},
	}
	var sets []*appsv1.ReplicaSet
	var pods []*corev1.Pod
	for _, hash := range []string{"blue", "green"} {
		rs := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{Name: hash, Namespace: "test", UID: types.UID("rs-" + hash),
				Labels:          map[string]string{"app": "demo", v1alpha1.DefaultRolloutUniqueLabelKey: hash},
				OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(ro, v1alpha1.SchemeGroupVersion.WithKind("Rollout"))}},
			Spec: appsv1.ReplicaSetSpec{Replicas: ptr.To(int32(1))},
		}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: hash, Namespace: "test", UID: types.UID("pod-" + hash),
			Labels: rs.Labels, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(rs, appsv1.SchemeGroupVersion.WithKind("ReplicaSet"))}}}
		sets = append(sets, rs)
		pods = append(pods, pod)
	}
	return target, service, sets, pods
}

func TestAnnotationProviderRoundTripAndOwnership(t *testing.T) {
	ctx := context.Background()
	target, _, sets, pods := fixture()
	foreignRS := sets[0].DeepCopy()
	foreignRS.Name, foreignRS.UID, foreignRS.OwnerReferences = "foreign", "foreign-rs", nil
	foreignPod := pods[0].DeepCopy()
	foreignPod.Name, foreignPod.UID = "foreign", "foreign-pod"
	foreignPod.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(foreignRS, appsv1.SchemeGroupVersion.WithKind("ReplicaSet"))}
	client := kubefake.NewSimpleClientset(sets[0], sets[1], pods[0], pods[1], foreignRS, foreignPod)
	provider := &AnnotationProvider{client: client}
	saved, err := provider.Capture(ctx, target)
	require.NoError(t, err)
	require.Equal(t, PodAnnotation, saved.Provider)
	require.Equal(t, "rollout-1", saved.RolloutUID)
	target.Hash = "green"
	_, err = provider.Apply(ctx, target)
	require.NoError(t, err)
	ok, err := provider.Verify(ctx, target)
	require.NoError(t, err)
	require.True(t, ok)
	// Formatting changes made by a router are not traffic drift.
	formatted := target
	formatted.Config = map[string]string{}
	for key, value := range target.Config {
		formatted.Config[key] = value
	}
	formatted.Config["activeWeight"] = "[\n  {\"weight\": 10}\n]"
	ok, err = provider.Verify(ctx, formatted)
	require.NoError(t, err)
	require.True(t, ok)
	target.Hash = "blue"
	for i := 0; i < 3; i++ {
		result, err := provider.Restore(ctx, target, saved)
		require.NoError(t, err)
		require.Equal(t, 1, result.ActivePods)
		ok, err := provider.Verify(ctx, target)
		require.NoError(t, err)
		require.True(t, ok)
	}
	for _, hash := range []string{"blue", "green"} {
		live, err := client.CoreV1().Pods("test").Get(ctx, hash, metav1.GetOptions{})
		require.NoError(t, err)
		require.Equal(t, types.UID("pod-"+hash), live.UID)
	}
	unrelated, err := client.CoreV1().Pods("test").Get(ctx, "foreign", metav1.GetOptions{})
	require.NoError(t, err)
	require.Empty(t, unrelated.Annotations)
	for _, action := range client.Actions() {
		require.NotContains(t, []string{"create", "delete", "update"}, action.GetVerb())
	}
	// A failed write must not be reported as converged.
	client.PrependReactor("patch", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected patch failure")
	})
	_, err = provider.Restore(ctx, target, saved)
	require.ErrorContains(t, err, "injected")
}

func TestServiceProviderRoundTripWithoutPodAnnotations(t *testing.T) {
	ctx := context.Background()
	target, service, sets, pods := fixture()
	target.Config = nil
	client := kubefake.NewSimpleClientset(service, sets[0], sets[1], pods[0], pods[1])
	provider := &ServiceProvider{client: client}
	saved, err := provider.Capture(ctx, target)
	require.NoError(t, err)
	target.Hash = "green"
	_, err = provider.Apply(ctx, target)
	require.NoError(t, err)
	target.Hash = "blue"
	for i := 0; i < 3; i++ {
		_, err = provider.Restore(ctx, target, saved)
		require.NoError(t, err)
		ok, err := provider.Verify(ctx, target)
		require.NoError(t, err)
		require.True(t, ok)
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "patch" {
			require.Equal(t, "services", action.GetResource().Resource)
		}
		require.NotContains(t, []string{"create", "delete", "update"}, action.GetVerb())
	}
	for _, hash := range []string{"blue", "green"} {
		live, err := client.CoreV1().Pods("test").Get(ctx, hash, metav1.GetOptions{})
		require.NoError(t, err)
		require.Empty(t, live.Annotations)
		require.Equal(t, types.UID("pod-"+hash), live.UID)
	}
	for _, scenario := range []string{"rollout-recreated", "service-recreated", "bad-version", "bad-hash"} {
		t.Run(scenario, func(t *testing.T) {
			state := saved.DeepCopy()
			switch scenario {
			case "rollout-recreated":
				state.RolloutUID = "other"
			case "service-recreated":
				state.Data["serviceUID"] = "other"
			case "bad-version":
				state.Version = "v999"
			case "bad-hash":
				state.Data["activeHash"] = "other"
			}
			before := len(client.Actions())
			_, err := provider.Restore(ctx, target, state)
			require.Error(t, err)
			for _, action := range client.Actions()[before:] {
				require.NotEqual(t, "patch", action.GetVerb())
			}
		})
	}
}

func TestProviderValidationAndCapabilities(t *testing.T) {
	target, _, _, _ := fixture()
	registry := NewRegistry(kubefake.NewSimpleClientset())
	_, err := registry.Resolve("not-registered")
	require.Error(t, err)
	provider, err := registry.Resolve(PodAnnotation)
	require.NoError(t, err)
	for _, config := range []map[string]string{
		nil, {"annotationKey": "invalid key"}, {"unknownKey": "value"},
		{"annotationKey": "example.com/weight", "baseSelector": "app=demo", "activeWeight": "10", "inactiveWeight": "0", "reapplyDelaySeconds": "-1"},
	} {
		bad := target
		bad.Config = config
		require.Error(t, provider.Validate(bad))
	}
	service, _ := registry.Resolve(ServiceSelector)
	require.Error(t, service.Validate(target))
	target.Config = nil
	require.NoError(t, service.Validate(target))
	target.Rollout.Spec.Strategy = v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{}}
	require.ErrorContains(t, service.Validate(target), "unsupported")
}

func TestAnnotationReapplyHonorsCancellation(t *testing.T) {
	target, _, sets, pods := fixture()
	provider := &AnnotationProvider{client: kubefake.NewSimpleClientset(sets[0], sets[1], pods[0], pods[1])}
	target.Config["reapplyDelaySeconds"] = "30"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := provider.Apply(ctx, target)
	require.ErrorIs(t, err, context.Canceled)
}
