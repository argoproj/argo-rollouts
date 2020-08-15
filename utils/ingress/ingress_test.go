package ingress

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestGetRolloutIngressKeysForCanary(t *testing.T) {
	keys := GetRolloutIngressKeys(&v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myrollout",
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{

			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
			},
		},
	})
	assert.Empty(t, keys)
}

func TestGetRolloutIngressKeysForCanaryWithTrafficRouting(t *testing.T) {
	keys := GetRolloutIngressKeys(&v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myrollout",
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					CanaryService: "canary-service",
					StableService: "stable-service",
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Nginx: &v1alpha1.NginxTrafficRouting{
							StableIngress: "stable-ingress",
						},
						ALB: &v1alpha1.ALBTrafficRouting{
							Ingress: "alb-ingress",
						},
					},
				},
			},
		},
	})
	assert.ElementsMatch(t, keys, []string{"default/stable-ingress", "default/myrollout-stable-ingress-canary", "default/alb-ingress"})
}

func TestGetCanaryIngressName(t *testing.T) {
	rollout := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "myrollout",
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					CanaryService: "canary-service",
					StableService: "stable-service",
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Nginx: &v1alpha1.NginxTrafficRouting{
							StableIngress: "stable-ingress",
						},
					},
				},
			},
		},
	}

	t.Run("NoTrim", func(t *testing.T) {
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress = "stable-ingress"
		canaryIngress := GetCanaryIngressName(rollout)
		assert.Equal(t, "myrollout-stable-ingress-canary", canaryIngress)
	})
	t.Run("Trim", func(t *testing.T) {
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress = fmt.Sprintf("stable-ingress%s", strings.Repeat("a", 260))
		canaryIngress := GetCanaryIngressName(rollout)
		assert.Equal(t, 253, len(canaryIngress), "canary ingress truncated to 253")
		assert.Equal(t, true, strings.HasSuffix(canaryIngress, "-canary"), "canary ingress has -canary suffix")
	})
	t.Run("NoStableIngress", func(t *testing.T) {
		rollout.Spec.Strategy.Canary.TrafficRouting.Nginx = nil
		canaryIngress := GetCanaryIngressName(rollout)
		assert.Equal(t, "", canaryIngress, "canary ingress is empty")
	})
}

func TestHasRuleWithService(t *testing.T) {
	ingress := &extensionsv1beta1.Ingress{
		Spec: extensionsv1beta1.IngressSpec{
			Rules: []extensionsv1beta1.IngressRule{{
				IngressRuleValue: extensionsv1beta1.IngressRuleValue{
					HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
						Paths: []extensionsv1beta1.HTTPIngressPath{{
							Backend: extensionsv1beta1.IngressBackend{
								ServiceName: "test",
							},
						}},
					},
				},
			}},
		},
	}
	assert.False(t, HasRuleWithService(ingress, "not-found"))
	assert.True(t, HasRuleWithService(ingress, "test"))
}

func TestManagedALBActions(t *testing.T) {
	t.Run("No annotations", func(t *testing.T) {
		m, err := NewManagedALBActions("")
		assert.Nil(t, err)
		assert.Len(t, m, 0)
		assert.Equal(t, m.String(), "")
	})

	t.Run("Incorrectly formatted action", func(t *testing.T) {
		m, err := NewManagedALBActions("no-colon")
		assert.Nil(t, m)
		assert.Errorf(t, err, "incorrectly formatted managed actions annotation")
	})
	t.Run("Handle one action", func(t *testing.T) {
		annotation := "ro1:alb.ingress.kubernetes.io/actions.svc1"
		m, err := NewManagedALBActions(annotation)
		assert.Nil(t, err)
		assert.Len(t, m, 1)
		assert.Equal(t, "alb.ingress.kubernetes.io/actions.svc1", m["ro1"])
		assert.Equal(t, annotation, m.String())
	})
	t.Run("Handle multiple actions", func(t *testing.T) {
		annotation := "ro1:alb.ingress.kubernetes.io/actions.svc1,ro2:alb.ingress.kubernetes.io/actions.svc2"
		m, err := NewManagedALBActions(annotation)
		assert.Nil(t, err)
		assert.Len(t, m, 2)
		assert.Equal(t, "alb.ingress.kubernetes.io/actions.svc1", m["ro1"])
		assert.Equal(t, "alb.ingress.kubernetes.io/actions.svc2", m["ro2"])
		assert.Contains(t, m.String(), "ro1:alb.ingress.kubernetes.io/actions.svc1")
		assert.Contains(t, m.String(), "ro2:alb.ingress.kubernetes.io/actions.svc2")
	})

}

func TestALBActionAnnotationKey(t *testing.T) {
	r := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: "svc",
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						ALB: &v1alpha1.ALBTrafficRouting{
							AnnotationPrefix: "test.annotation",
						},
					},
				},
			},
		},
		Status: v1alpha1.RolloutStatus{},
	}
	assert.Equal(t, "test.annotation/actions.svc", ALBActionAnnotationKey(r))
	r.Spec.Strategy.Canary.TrafficRouting.ALB.RootService = "root-svc"
	assert.Equal(t, "test.annotation/actions.root-svc", ALBActionAnnotationKey(r))
	r.Spec.Strategy.Canary.TrafficRouting.ALB.AnnotationPrefix = ""
	assert.Equal(t, "alb.ingress.kubernetes.io/actions.root-svc", ALBActionAnnotationKey(r))

}
