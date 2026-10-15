//go:build e2e

package promotione2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/utils/annotations"
)

// These tests require an isolated cluster with both proposed controllers and the
// annotation TrafficRouter installed. They do not target the user's current context.
type fixture struct {
	t      *testing.T
	ctx    context.Context
	kube   kubernetes.Interface
	client rolloutclient.Interface
	ns     string
	name   string
}

func newFixture(t *testing.T, name string) *fixture {
	t.Helper()
	path := os.Getenv("PROMOTION_E2E_KUBECONFIG")
	if path == "" {
		t.Skip("PROMOTION_E2E_KUBECONFIG must explicitly select an isolated test cluster")
	}
	config, err := clientcmd.BuildConfigFromFlags("", path)
	require.NoError(t, err)
	kube, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)
	client, err := rolloutclient.NewForConfig(config)
	require.NoError(t, err)
	ctx := context.Background()
	ns, err := kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "promotion-e2e-"}}, metav1.CreateOptions{})
	require.NoError(t, err)
	f := &fixture{t: t, ctx: ctx, kube: kube, client: client, ns: ns.Name, name: name}
	t.Cleanup(func() {
		if t.Failed() {
			ros, _ := client.ArgoprojV1alpha1().Rollouts(ns.Name).List(ctx, metav1.ListOptions{})
			rps, _ := client.ArgoprojV1alpha1().RolloutPromotions(ns.Name).List(ctx, metav1.ListOptions{})
			data, _ := json.Marshal(map[string]interface{}{"rollouts": ros, "promotions": rps})
			t.Log(string(data))
		}
		require.NoError(t, kube.CoreV1().Namespaces().Delete(ctx, ns.Name, metav1.DeleteOptions{}))
	})
	return f
}

func (f *fixture) wait(description string, condition func() bool) {
	f.t.Helper()
	require.Eventually(f.t, condition, 180*time.Second, time.Second, description)
}

func (f *fixture) rollout() *v1alpha1.Rollout {
	f.t.Helper()
	ro, err := f.client.ArgoprojV1alpha1().Rollouts(f.ns).Get(f.ctx, f.name, metav1.GetOptions{})
	require.NoError(f.t, err)
	return ro
}

func (f *fixture) start(canary, routed bool) {
	image := os.Getenv("PROMOTION_E2E_IMAGE")
	if image == "" {
		image = "traefik/whoami:v1.10.4"
	}
	for _, suffix := range []string{"active", "preview"} {
		_, err := f.kube.CoreV1().Services(f.ns).Create(f.ctx, &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: f.name + "-" + suffix},
			Spec:       corev1.ServiceSpec{Selector: map[string]string{"app": f.name}, Ports: []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt32(80)}}},
		}, metav1.CreateOptions{})
		require.NoError(f.t, err)
	}
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: f.name, Namespace: f.ns},
		Spec: v1alpha1.RolloutSpec{
			Replicas: ptr.To(int32(2)), RevisionHistoryLimit: ptr.To(int32(5)),
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": f.name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": f.name}},
				Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "app", Image: image, ImagePullPolicy: corev1.PullIfNotPresent,
					Env:            []corev1.EnvVar{{Name: "RELEASE", Value: "one"}},
					ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/", Port: intstr.FromInt32(80)}}, PeriodSeconds: 1},
				}}},
			},
		},
	}
	if !canary {
		if routed {
			ro.Spec.Template.Annotations = map[string]string{"example.com/weight": "100"}
		}
		ro.Spec.Strategy.BlueGreen = &v1alpha1.BlueGreenStrategy{
			ActiveService: f.name + "-active", PreviewService: f.name + "-preview",
			AutoPromotionEnabled: ptr.To(false), ScaleDownDelaySeconds: ptr.To(int32(600)), ScaleDownDelayRevisionLimit: ptr.To(int32(5)),
		}
	} else {
		ro.Spec.Strategy.Canary = &v1alpha1.CanaryStrategy{
			StableService: f.name + "-active", CanaryService: f.name + "-preview",
			MaxSurge: ptr.To(intstr.FromString("100%")), MaxUnavailable: ptr.To(intstr.FromInt32(0)),
			Steps: []v1alpha1.CanaryStep{
				{SetWeight: ptr.To(int32(0))}, {Pause: &v1alpha1.RolloutPause{}},
				{SetWeight: ptr.To(int32(50))}, {Pause: &v1alpha1.RolloutPause{}},
				{SetWeight: ptr.To(int32(100))}, {Pause: &v1alpha1.RolloutPause{}},
			},
		}
		if routed {
			ro.Spec.Strategy.Canary.ScaleDownDelaySeconds = ptr.To(int32(600))
			ro.Spec.Strategy.Canary.ScaleDownDelayRevisionLimit = ptr.To(int32(5))
			ro.Spec.Strategy.Canary.AbortScaleDownDelaySeconds = ptr.To(int32(600))
			ro.Spec.Template.Annotations = map[string]string{"example.com/weight": "0"}
			raw, _ := json.Marshal(map[string]interface{}{
				"annotationKey": "example.com/weight", "baseSelector": "app=" + f.name, "maxPodWeight": 100,
				"weightCalculation": "aggregatePerPod", "prettyJSON": false,
				"stableWeightTemplate": "__ROLLOUT_WEIGHT__", "canaryWeightTemplate": "__ROLLOUT_WEIGHT__",
				"promotedWeightTemplate": "__ROLLOUT_WEIGHT__", "inactiveWeightTemplate": "0",
			})
			ro.Spec.Strategy.Canary.TrafficRouting = &v1alpha1.RolloutTrafficRouting{Plugins: map[string]json.RawMessage{"argoproj/pod-annotation-traffic-router": raw}}
			ro.Spec.Strategy.Canary.Steps = append([]v1alpha1.CanaryStep{{SetCanaryScale: &v1alpha1.SetCanaryScale{Weight: ptr.To(int32(100))}}}, ro.Spec.Strategy.Canary.Steps...)
		}
	}
	_, err := f.client.ArgoprojV1alpha1().Rollouts(f.ns).Create(f.ctx, ro, metav1.CreateOptions{})
	require.NoError(f.t, err)
	f.wait("initial revision healthy", func() bool {
		r := f.rollout()
		return r.Status.Phase == v1alpha1.RolloutPhaseHealthy && r.Status.ReadyReplicas == 2
	})
}

func (f *fixture) update(version string) (string, string) {
	old := f.rollout()
	previousCandidate := old.Status.CurrentPodHash
	oldHash := old.Status.StableRS
	if value := old.Annotations[annotations.TrafficOverrideHashAnnotation]; value != "" {
		oldHash = value
	}
	old.Spec.Template.Spec.Containers[0].Env[0].Value = version
	if old.Spec.Strategy.BlueGreen != nil && old.Spec.Template.Annotations["example.com/weight"] != "" {
		old.Spec.Template.Annotations["example.com/weight"] = "0"
	}
	_, err := f.client.ArgoprojV1alpha1().Rollouts(f.ns).Update(f.ctx, old, metav1.UpdateOptions{})
	require.NoError(f.t, err)
	f.wait("new revision ready and manually paused", func() bool {
		r := f.rollout()
		return r.Status.CurrentPodHash != "" && r.Status.CurrentPodHash != previousCandidate && r.Status.CurrentPodHash != oldHash && r.Status.UpdatedReplicas == 2 && len(r.Status.PauseConditions) > 0 && f.readyHash(oldHash) == 2 && f.readyHash(r.Status.CurrentPodHash) == 2
	})
	return oldHash, f.rollout().Status.CurrentPodHash
}

func (f *fixture) readyHash(hash string) int {
	pods, err := f.kube.CoreV1().Pods(f.ns).List(f.ctx, metav1.ListOptions{LabelSelector: "app=" + f.name + "," + v1alpha1.DefaultRolloutUniqueLabelKey + "=" + hash})
	if err != nil {
		return 0
	}
	ready := 0
	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				ready++
			}
		}
	}
	return ready
}

func (f *fixture) request(id, provider string, legacy bool, applyOnPromotion ...bool) *v1alpha1.RolloutPromotion {
	r := f.rollout()
	target := v1alpha1.RolloutPromotionTarget{
		Name: f.name, ExpectedRolloutUID: string(r.UID), ExpectedCurrentPodHash: r.Status.CurrentPodHash,
		Readiness: &v1alpha1.RolloutPromotionReadiness{RequireCurrentPodHash: true, RequirePaused: true},
	}
	if r.Spec.Strategy.Canary != nil {
		target.Strategy = "canary"
		target.ExpectedCurrentStepIndex = r.Status.CurrentStepIndex
	}
	if provider != "" {
		apply := len(applyOnPromotion) > 0 && applyOnPromotion[0]
		target.Traffic = &v1alpha1.RolloutPromotionTraffic{Provider: provider, ApplyOnPromotion: &apply}
		if provider == "podAnnotation" {
			target.Traffic.Config = map[string]string{"annotationKey": "example.com/weight", "baseSelector": "app=" + f.name, "activeWeight": "100", "inactiveWeight": "0"}
		}
		if legacy {
			target.Traffic = nil
			target.WeightSwitch = &v1alpha1.RolloutPromotionWeightSwitch{Enabled: ptr.To(true), ApplyOnPromotion: &apply, AnnotationKey: "example.com/weight", BaseSelector: "app=" + f.name, ActiveWeight: "100", InactiveWeight: "0"}
		}
	}
	p := &v1alpha1.RolloutPromotion{ObjectMeta: metav1.ObjectMeta{Name: id, Namespace: f.ns},
		Spec: v1alpha1.RolloutPromotionSpec{
			RequestID: id, Action: "promote", Mode: "together",
			Wait:     v1alpha1.RolloutPromotionWait{Enabled: ptr.To(true), Attempts: 120, Seconds: 1},
			Rollouts: []v1alpha1.RolloutPromotionTarget{target},
		}}
	if provider != "" {
		p.Spec.TrafficRollback = &v1alpha1.RolloutPromotionTrafficRollback{Enabled: ptr.To(true)}
	}
	if id == "before" {
		p.Annotations = map[string]string{annotations.TrafficRollbackRequestAnnotation: "rollback-1"}
	}
	live, err := f.client.ArgoprojV1alpha1().RolloutPromotions(f.ns).Create(f.ctx, p, metav1.CreateOptions{})
	require.NoError(f.t, err)
	return live
}

func (f *fixture) waitPromotion(name string) {
	f.wait("promotion succeeded: "+name, func() bool {
		p, err := f.client.ArgoprojV1alpha1().RolloutPromotions(f.ns).Get(f.ctx, name, metav1.GetOptions{})
		return err == nil && p.Status.Phase == v1alpha1.RolloutPromotionPhaseSucceeded
	})
}

func (f *fixture) pods() map[types.UID]int32 {
	f.t.Helper()
	pods, err := f.kube.CoreV1().Pods(f.ns).List(f.ctx, metav1.ListOptions{LabelSelector: "app=" + f.name})
	require.NoError(f.t, err)
	result := map[types.UID]int32{}
	for _, pod := range pods.Items {
		require.Nil(f.t, pod.DeletionTimestamp)
		for _, c := range pod.Status.ContainerStatuses {
			result[pod.UID] += c.RestartCount
		}
	}
	return result
}

func (f *fixture) rollback(promotion, hash string) {
	before := f.pods()
	r := f.rollout()
	template, _ := json.Marshal(r.Spec.Template)
	patch, _ := json.Marshal(map[string]interface{}{"metadata": map[string]interface{}{"annotations": map[string]string{annotations.TrafficRollbackRequestAnnotation: "rollback-1"}}})
	_, err := f.client.ArgoprojV1alpha1().RolloutPromotions(f.ns).Patch(f.ctx, promotion, types.MergePatchType, patch, metav1.PatchOptions{})
	require.NoError(f.t, err)
	f.wait("traffic rollback completed", func() bool {
		p, err := f.client.ArgoprojV1alpha1().RolloutPromotions(f.ns).Get(f.ctx, promotion, metav1.GetOptions{})
		return err == nil && p.Status.LastTrafficRollbackRequest == "rollback-1" && p.Status.Phase == "Succeeded"
	})
	require.Equal(f.t, hash, f.rollout().Annotations[annotations.TrafficOverrideHashAnnotation])
	require.Equal(f.t, before, f.pods(), "traffic rollback must preserve every Pod UID and restart count")
	after, _ := json.Marshal(f.rollout().Spec.Template)
	require.JSONEq(f.t, string(template), string(after))
	// Reapplying the same request must be a no-op.
	_, err = f.client.ArgoprojV1alpha1().RolloutPromotions(f.ns).Patch(f.ctx, promotion, types.MergePatchType, patch, metav1.PatchOptions{})
	require.NoError(f.t, err)
	time.Sleep(3 * time.Second)
	require.Equal(f.t, before, f.pods())
}

func (f *fixture) verifyWeights(activeHash string) {
	f.wait("annotation weights converge", func() bool {
		pods, err := f.kube.CoreV1().Pods(f.ns).List(f.ctx, metav1.ListOptions{LabelSelector: "app=" + f.name})
		if err != nil || len(pods.Items) < 4 {
			return false
		}
		for _, pod := range pods.Items {
			expected := "0"
			if pod.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] == activeHash {
				expected = "100"
			}
			if pod.Annotations["example.com/weight"] != expected {
				return false
			}
		}
		return true
	})
}

func TestServiceSelectorBlueGreen(t *testing.T) {
	f := newFixture(t, "bluegreen")
	f.start(false, false)
	oldHash, newHash := f.update("two")
	f.request("forward", "serviceSelector", false)
	f.waitPromotion("forward")
	f.wait("new service backend", func() bool {
		body, err := f.kube.CoreV1().Services(f.ns).ProxyGet("http", f.name+"-active", "80", "/", nil).DoRaw(f.ctx)
		return err == nil && strings.Contains(string(body), f.name+"-"+newHash+"-")
	})
	f.rollback("forward", oldHash)
	f.wait("restored service backend stays selected", func() bool {
		body, err := f.kube.CoreV1().Services(f.ns).ProxyGet("http", f.name+"-active", "80", "/", nil).DoRaw(f.ctx)
		return err == nil && strings.Contains(string(body), f.name+"-"+oldHash+"-")
	})
	// The next release must use the restored baseline and complete a new promotion.
	_, next := f.update("three")
	f.request("next", "serviceSelector", false)
	f.waitPromotion("next")
	require.Equal(t, next, f.rollout().Status.StableRS)
}

func TestLegacyAnnotationBlueGreen(t *testing.T) {
	f := newFixture(t, "annotation-bluegreen")
	f.start(false, true)
	oldHash, newHash := f.update("two")
	f.verifyWeights(oldHash)
	f.request("forward", "podAnnotation", true, true)
	f.waitPromotion("forward")
	f.verifyWeights(newHash)
	f.rollback("forward", oldHash)
	f.verifyWeights(oldHash)
	_, next := f.update("three")
	f.verifyWeights(oldHash)
	f.request("next", "podAnnotation", true, true)
	f.waitPromotion("next")
	f.verifyWeights(next)
}

func TestAnnotationCanaryRollback(t *testing.T) {
	for _, stage := range []string{"before", "during", "after"} {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t, "canary")
			f.start(true, true)
			oldHash, newHash := f.update("two")
			f.verifyWeights(oldHash)
			before := f.pods()
			if stage == "before" {
				// Submit the rollback annotation atomically with the first request.
				// This tests rollback-before-commit without a timing race.
				f.request("before", "podAnnotation", true)
				f.rollback("before", oldHash)
			} else {
				f.request("first", "podAnnotation", false)
				f.waitPromotion("first")
				f.wait("50 percent manual gate", func() bool {
					r := f.rollout()
					return r.Status.CurrentStepIndex != nil && *r.Status.CurrentStepIndex == 4 && len(r.Status.PauseConditions) > 0
				})
				// Both providers preserve old/new replicas; canary changes weights.
				require.Equal(t, before, f.pods())
				if stage == "during" {
					f.restartControllers()
					require.Equal(t, before, f.pods())
					require.Equal(t, int32(4), *f.rollout().Status.CurrentStepIndex)
				}
				promotion := "first"
				if stage == "after" {
					f.request("second", "podAnnotation", false)
					f.waitPromotion("second")
					f.wait("100 percent manual gate", func() bool {
						r := f.rollout()
						return r.Status.CurrentStepIndex != nil && *r.Status.CurrentStepIndex == 6 && len(r.Status.PauseConditions) > 0
					})
					f.verifyWeights(newHash)
					f.request("complete", "podAnnotation", false)
					f.waitPromotion("complete")
					f.wait("new revision stable", func() bool { return f.rollout().Status.StableRS == newHash })
					promotion = "complete"
				}
				f.rollback(promotion, oldHash)
			}
			f.verifyWeights(oldHash)
			require.Equal(t, before, f.pods())
			_, next := f.update("three")
			f.verifyWeights(oldHash)
			for i, step := range []int32{4, 6} {
				id := fmt.Sprintf("next-%d", i)
				f.request(id, "podAnnotation", false)
				f.waitPromotion(id)
				f.wait("next canary gate", func() bool {
					r := f.rollout()
					return r.Status.CurrentStepIndex != nil && *r.Status.CurrentStepIndex == step && len(r.Status.PauseConditions) > 0
				})
			}
			f.request("next-complete", "podAnnotation", false)
			f.waitPromotion("next-complete")
			f.wait("next release stable", func() bool { return f.rollout().Status.StableRS == next })
		})
	}
}

func (f *fixture) restartControllers() {
	f.t.Helper()
	for _, name := range []string{"argo-rollouts", "argo-rollouts-promotion-controller"} {
		dep, err := f.kube.AppsV1().Deployments("argo-rollouts").Get(f.ctx, name, metav1.GetOptions{})
		require.NoError(f.t, err)
		if dep.Spec.Template.Annotations == nil {
			dep.Spec.Template.Annotations = map[string]string{}
		}
		dep.Spec.Template.Annotations["example.com/e2e-restart"] = time.Now().UTC().Format(time.RFC3339Nano)
		dep, err = f.kube.AppsV1().Deployments("argo-rollouts").Update(f.ctx, dep, metav1.UpdateOptions{})
		require.NoError(f.t, err)
		generation := dep.Generation
		f.wait("controller restarted: "+name, func() bool {
			live, err := f.kube.AppsV1().Deployments("argo-rollouts").Get(f.ctx, name, metav1.GetOptions{})
			return err == nil && live.Status.ObservedGeneration >= generation && live.Status.UpdatedReplicas == *live.Spec.Replicas && live.Status.AvailableReplicas >= *live.Spec.Replicas
		})
	}
	time.Sleep(3 * time.Second)
}

func TestNativeCanaryWithoutTrafficProvider(t *testing.T) {
	f := newFixture(t, "native")
	f.start(true, false)
	ro := f.rollout()
	ro.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{{SetWeight: ptr.To(int32(50))}, {Pause: &v1alpha1.RolloutPause{}}, {SetWeight: ptr.To(int32(100))}}
	ro.Spec.Template.Spec.Containers[0].Env[0].Value = "two"
	_, err := f.client.ArgoprojV1alpha1().Rollouts(f.ns).Update(f.ctx, ro, metav1.UpdateOptions{})
	require.NoError(t, err)
	f.wait("native canary paused", func() bool {
		r := f.rollout()
		return r.Status.CurrentPodHash != r.Status.StableRS && len(r.Status.PauseConditions) > 0
	})
	f.request("native-promote", "", false)
	f.waitPromotion("native-promote")
	f.wait("native canary healthy", func() bool {
		r := f.rollout()
		return r.Status.CurrentPodHash == r.Status.StableRS && r.Status.Phase == v1alpha1.RolloutPhaseHealthy
	})
}
