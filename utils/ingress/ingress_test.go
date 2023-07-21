package ingress

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"

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

func TestGetRolloutIngressKeysForCanaryWithTrafficRoutingMultiIngress(t *testing.T) {
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
							StableIngresses: []string{"stable-ingress", "stable-ingress-additional"},
						},
						ALB: &v1alpha1.ALBTrafficRouting{
							Ingresses: []string{"alb-ingress", "alb-multi-ingress"},
						},
					},
				},
			},
		},
	})
	assert.ElementsMatch(t, keys, []string{"default/stable-ingress", "default/myrollout-stable-ingress-canary", "default/stable-ingress-additional", "default/myrollout-stable-ingress-additional-canary", "default/alb-ingress", "default/alb-multi-ingress"})
}

func TestGetCanaryIngressName(t *testing.T) {
	singleIngressRollout := &v1alpha1.Rollout{
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
							StableIngresses: []string{"stable-ingress", "stable-ingress-additional"},
						},
					},
				},
			},
		},
	}

	multiIngressRollout := &v1alpha1.Rollout{
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
							StableIngresses: []string{"stable-ingress", "stable-ingress-additional"},
						},
					},
				},
			},
		},
	}

	t.Run("StableIngress - NoTrim", func(t *testing.T) {
		singleIngressRollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress = "stable-ingress"
		canaryIngress := GetCanaryIngressName(singleIngressRollout.GetName(), singleIngressRollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress)
		assert.Equal(t, "myrollout-stable-ingress-canary", canaryIngress)
	})
	t.Run("StableIngress - Trim", func(t *testing.T) {
		singleIngressRollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress = fmt.Sprintf("stable-ingress%s", strings.Repeat("a", 260))
		canaryIngress := GetCanaryIngressName(singleIngressRollout.GetName(), singleIngressRollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngress)
		assert.Equal(t, 253, len(canaryIngress), "canary ingress truncated to 253")
		assert.Equal(t, true, strings.HasSuffix(canaryIngress, "-canary"), "canary ingress has -canary suffix")
	})
	t.Run("StableIngresses - NoTrim", func(t *testing.T) {
		for _, ing := range multiIngressRollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngresses {
			canaryIngress := GetCanaryIngressName(multiIngressRollout.GetName(), ing)
			assert.Equal(t, fmt.Sprintf("%s-%s-canary", multiIngressRollout.ObjectMeta.Name, ing), canaryIngress)
		}
	})
	t.Run("StableIngresses - Trim", func(t *testing.T) {
		multiIngressRollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngresses = []string{fmt.Sprintf("stable-ingress%s", strings.Repeat("a", 260))}
		for _, ing := range multiIngressRollout.Spec.Strategy.Canary.TrafficRouting.Nginx.StableIngresses {
			canaryIngress := GetCanaryIngressName(multiIngressRollout.GetName(), ing)
			assert.Equal(t, 253, len(canaryIngress), "canary ingress truncated to 253")
			assert.Equal(t, true, strings.HasSuffix(canaryIngress, "-canary"), "canary ingress has -canary suffix")
		}
	})
	t.Run("NoStableIngress", func(t *testing.T) {
		multiIngressRollout.Spec.Strategy.Canary.TrafficRouting.Nginx = nil
		canaryIngress := GetCanaryIngressName(multiIngressRollout.GetName(), "")
		assert.Equal(t, "", canaryIngress, "canary ingress is empty")
	})
}

func TestGetCanaryAlbIngressName(t *testing.T) {
	singleIngressRollout := &v1alpha1.Rollout{
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
						ALB: &v1alpha1.ALBTrafficRouting{
							Ingress: "stable-ingress",
						},
					},
				},
			},
		},
	}

	multiIngressRollout := &v1alpha1.Rollout{
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
						ALB: &v1alpha1.ALBTrafficRouting{
							Ingresses: []string{"stable-ingress", "stable-ingress-additional"},
						},
					},
				},
			},
		},
	}

	t.Run("Ingress - NoTrim", func(t *testing.T) {
		singleIngressRollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress = "stable-ingress"
		canaryIngress := GetCanaryIngressName(singleIngressRollout.GetName(), singleIngressRollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress)
		assert.Equal(t, "myrollout-stable-ingress-canary", canaryIngress)
	})
	t.Run("Ingress - Trim", func(t *testing.T) {
		singleIngressRollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress = fmt.Sprintf("stable-ingress%s", strings.Repeat("a", 260))
		canaryIngress := GetCanaryIngressName(singleIngressRollout.GetName(), singleIngressRollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingress)
		assert.Equal(t, 253, len(canaryIngress), "canary ingress truncated to 253")
		assert.Equal(t, true, strings.HasSuffix(canaryIngress, "-canary"), "canary ingress has -canary suffix")
	})
	t.Run("Ingresses - NoTrim", func(t *testing.T) {
		for _, ing := range multiIngressRollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingresses {
			canaryIngress := GetCanaryIngressName(multiIngressRollout.GetName(), ing)
			assert.Equal(t, fmt.Sprintf("%s-%s-canary", multiIngressRollout.ObjectMeta.Name, ing), canaryIngress)
		}
	})
	t.Run("Ingresses - Trim", func(t *testing.T) {
		multiIngressRollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingresses = []string{fmt.Sprintf("stable-ingress%s", strings.Repeat("a", 260))}
		for _, ing := range multiIngressRollout.Spec.Strategy.Canary.TrafficRouting.ALB.Ingresses {
			canaryIngress := GetCanaryIngressName(multiIngressRollout.GetName(), ing)
			assert.Equal(t, 253, len(canaryIngress), "canary ingress truncated to 253")
			assert.Equal(t, true, strings.HasSuffix(canaryIngress, "-canary"), "canary ingress has -canary suffix")
		}
	})
	t.Run("NoIngress", func(t *testing.T) {
		multiIngressRollout.Spec.Strategy.Canary.TrafficRouting.ALB = nil
		canaryIngress := GetCanaryIngressName(multiIngressRollout.GetName(), "")
		assert.Equal(t, "", canaryIngress, "canary ingress is empty")
	})
}

func TestHasRuleWithService(t *testing.T) {
	t.Run("will check rule with legacy ingress", func(t *testing.T) {
		// given
		t.Parallel()
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
		i := NewLegacyIngress(ingress)

		// then
		assert.False(t, HasRuleWithService(i, "not-found"))
		assert.True(t, HasRuleWithService(i, "test"))
	})
	t.Run("will check rule with networking ingress", func(t *testing.T) {
		// given
		t.Parallel()
		ingress := &networkingv1.Ingress{
			Spec: networkingv1.IngressSpec{
				Rules: []networkingv1.IngressRule{{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{{
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "test",
									},
								},
							}},
						},
					},
				}},
			},
		}
		i := NewIngress(ingress)

		// then
		assert.False(t, HasRuleWithService(i, "not-found"))
		assert.True(t, HasRuleWithService(i, "test"))
	})
	t.Run("will return false if invalid IngressMode passed", func(t *testing.T) {
		// given
		t.Parallel()
		invalid_ingress := &Ingress{}

		// then
		assert.False(t, HasRuleWithService(invalid_ingress, "test"))
	})
}

func TestBuildIngressPath(t *testing.T) {
	t.Run("will build network ingress patch successfully", func(t *testing.T) {
		// given
		t.Parallel()
		i1 := getNetworkingIngress()
		i2 := getNetworkingIngress()
		annotations := i2.GetAnnotations()
		annotations["annotation-key1"] = "changed-annotation1"

		labels := i2.GetLabels()
		labels["label-key1"] = "changed-label1"

		i2.SetAnnotations(annotations)
		i2.SetLabels(labels)
		className := "modified-ingress-class"
		i2.Spec.IngressClassName = &className
		ingress1 := NewIngress(i1)
		ingress2 := NewIngress(i2)

		expected_patch := `{"metadata":{"annotations":{"annotation-key1":"changed-annotation1"},"labels":{"label-key1":"changed-label1"}},"spec":{"ingressClassName":"modified-ingress-class"}}`

		// when
		patch, ok, err := BuildIngressPatch(IngressModeNetworking, ingress1, ingress2, WithLabels(), WithAnnotations(), WithSpec())

		// then
		assert.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, expected_patch, string(patch))
	})
	t.Run("will build extensions ingress patch successfully", func(t *testing.T) {
		t.Parallel()
		i1 := getExtensionsIngress()
		i2 := getExtensionsIngress()
		annotations := i2.GetAnnotations()
		annotations["annotation-key1"] = "changed-annotation1"

		labels := i2.GetLabels()
		labels["label-key1"] = "changed-label1"

		i2.SetAnnotations(annotations)
		i2.SetLabels(labels)
		className := "modified-ingress-class"
		i2.Spec.IngressClassName = &className
		ingress1 := NewLegacyIngress(i1)
		ingress2 := NewLegacyIngress(i2)

		expected_patch := `{"metadata":{"annotations":{"annotation-key1":"changed-annotation1"},"labels":{"label-key1":"changed-label1"}},"spec":{"ingressClassName":"modified-ingress-class"}}`

		// when
		patch, ok, err := BuildIngressPatch(IngressModeExtensions, ingress1, ingress2, WithLabels(), WithAnnotations(), WithSpec())

		// then
		assert.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, expected_patch, string(patch))
	})
	t.Run("will return error if invalid ingress mode passed", func(t *testing.T) {
		// given
		t.Parallel()
		ingress1 := NewLegacyIngress(getExtensionsIngress())
		ingress2 := NewLegacyIngress(getExtensionsIngress())

		// when
		patch, ok, err := BuildIngressPatch(9999, ingress1, ingress2, WithLabels(), WithAnnotations(), WithSpec())

		// then
		assert.Error(t, err)
		assert.False(t, ok)
		assert.Equal(t, "", string(patch))
	})
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

func TestDetermineIngressMode(t *testing.T) {
	type testCase struct {
		name          string
		apiVersion    string
		faKeDiscovery discovery.ServerVersionInterface
		expectedMode  IngressMode
		expectedError error
	}

	cases := []*testCase{
		{
			name:         "will return extensions mode if apiVersion is extensions/v1beta1",
			apiVersion:   "extensions/v1beta1",
			expectedMode: IngressModeExtensions,
		},
		{
			name:         "will return networking mode if apiVersion is networking/v1",
			apiVersion:   "networking/v1",
			expectedMode: IngressModeNetworking,
		},
		{
			name:          "will return networking mode if server version is 1.19",
			apiVersion:    "",
			faKeDiscovery: newFakeDiscovery("1", "19", nil),
			expectedMode:  IngressModeNetworking,
		},
		{
			name:          "will return networking mode if server version is 2.0",
			apiVersion:    "",
			faKeDiscovery: newFakeDiscovery("2", "0", nil),
			expectedMode:  IngressModeNetworking,
		},
		{
			name:          "will return extensions mode if server version is 1.18",
			apiVersion:    "",
			faKeDiscovery: newFakeDiscovery("1", "18", nil),
			expectedMode:  IngressModeExtensions,
		},
		{
			name:          "will return networking mode if server minor version has '+' suffix, e.g. 1.19+",
			apiVersion:    "",
			faKeDiscovery: newFakeDiscovery("1", "19+", nil),
			expectedMode:  IngressModeNetworking,
		},
		{
			name:          "will return error if fails to retrieve server version",
			apiVersion:    "",
			faKeDiscovery: newFakeDiscovery("", "", errors.New("internal server error")),
			expectedMode:  0,
			expectedError: errors.New("internal server error"),
		},
		{
			name:          "will return error if fails to parse major version",
			apiVersion:    "",
			faKeDiscovery: newFakeDiscovery("wrong", "", nil),
			expectedMode:  0,
			expectedError: &strconv.NumError{
				Func: "Atoi",
				Num:  "wrong",
				Err:  errors.New("invalid syntax"),
			},
		},
		{
			name:          "will return error if fails to parse minor version",
			apiVersion:    "",
			faKeDiscovery: newFakeDiscovery("1", "wrong", nil),
			expectedMode:  0,
			expectedError: &strconv.NumError{
				Func: "Atoi",
				Num:  "wrong",
				Err:  errors.New("invalid syntax"),
			},
		},
		{
			name:          "will return error if fails to parse minor version with '+' suffix, e.g. 1.wrong+",
			apiVersion:    "",
			faKeDiscovery: newFakeDiscovery("1", "wrong+", nil),
			expectedMode:  0,
			expectedError: &strconv.NumError{
				Func: "Atoi",
				Num:  "wrong",
				Err:  errors.New("invalid syntax"),
			},
		},
		{
			name:          "will return error if fails to parse minor version with just '+'",
			apiVersion:    "",
			faKeDiscovery: newFakeDiscovery("1", "+", nil),
			expectedMode:  0,
			expectedError: &strconv.NumError{
				Func: "Atoi",
				Num:  "",
				Err:  errors.New("invalid syntax"),
			},
		},
	}
	for _, c := range cases {
		c := c // necessary to ensure all test cases are executed when running in parallel mode

		t.Run(c.name, func(t *testing.T) {
			// given
			t.Parallel()

			// when
			mode, err := DetermineIngressMode(c.apiVersion, c.faKeDiscovery)

			// then
			assert.Equal(t, c.expectedError, err)
			assert.Equal(t, c.expectedMode, mode)
		})
	}
}

type fakeDiscovery struct {
	major, minor string
	err          error
}

func newFakeDiscovery(major, minor string, err error) *fakeDiscovery {
	return &fakeDiscovery{
		major: major,
		minor: minor,
		err:   err,
	}
}

func (f *fakeDiscovery) ServerVersion() (*version.Info, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &version.Info{
		Major: f.major,
		Minor: f.minor,
	}, nil
}

func getNetworkingIngress() *networkingv1.Ingress {
	ingressClassName := "ingress-name"
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "networking-ingress",
			Namespace: "some-namespace",
			Annotations: map[string]string{
				"annotation-key1": "annotation-value1",
			},
			Labels: map[string]string{
				"label-key1": "label-value1",
				"label-key2": "label-value2",
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressClassName,
			DefaultBackend: &networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: "backend",
					Port: networkingv1.ServiceBackendPort{
						Name:   "http",
						Number: 8080,
					},
				},
			},
		},
		Status: networkingv1.IngressStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{
						IP:       "127.0.0.1",
						Hostname: "localhost",
						Ports: []corev1.PortStatus{
							{
								Port:     8080,
								Protocol: "http",
							},
						},
					},
				},
			},
		},
	}
}

func getExtensionsIngress() *extensionsv1beta1.Ingress {
	ingressClassName := "ingress-name"
	return &extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "extensions-ingress",
			Namespace: "some-namespace",
			Annotations: map[string]string{
				"annotation-key1": "annotation-value1",
			},
			Labels: map[string]string{
				"label-key1": "label-value1",
				"label-key2": "label-value2",
			},
		},
		Spec: extensionsv1beta1.IngressSpec{
			IngressClassName: &ingressClassName,
			Backend: &extensionsv1beta1.IngressBackend{
				ServiceName: "some-service",
				ServicePort: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "8080",
				},
			},
		},
		Status: extensionsv1beta1.IngressStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{
					{
						IP:       "127.0.0.1",
						Hostname: "localhost",
						Ports: []corev1.PortStatus{
							{
								Port:     8080,
								Protocol: "http",
							},
						},
					},
				},
			},
		},
	}
}

func TestManagedALBAnnotations(t *testing.T) {
	emptyJson, _ := NewManagedALBAnnotations("")
	assert.NotNil(t, emptyJson)
	assert.Equal(t, 0, len(emptyJson))
	assert.Equal(t, "{}", emptyJson.String())

	_, err := NewManagedALBAnnotations("invalid json")
	assert.Error(t, err)

	json := "{\"rollouts-demo\":[\"alb.ingress.kubernetes.io/actions.action1\", \"alb.ingress.kubernetes.io/actions.header-action\", \"alb.ingress.kubernetes.io/conditions.header-action\"]}"
	actual, err := NewManagedALBAnnotations(json)
	assert.NoError(t, err)

	rolloutsDemoAnnotation := actual["rollouts-demo"]
	assert.NotNil(t, rolloutsDemoAnnotation)
	assert.Equal(t, 3, len(rolloutsDemoAnnotation))
}

func TestALBHeaderBasedActionAnnotationKey(t *testing.T) {
	r := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						ALB: &v1alpha1.ALBTrafficRouting{
							AnnotationPrefix: "alb.ingress.kubernetes.io",
						},
					},
				},
			},
		},
	}
	assert.Equal(t, "alb.ingress.kubernetes.io/actions.route", ALBHeaderBasedActionAnnotationKey(r, "route"))
}

func TestALBHeaderBasedConditionAnnotationKey(t *testing.T) {
	r := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						ALB: &v1alpha1.ALBTrafficRouting{
							AnnotationPrefix: "alb.ingress.kubernetes.io",
						},
					},
				},
			},
		},
	}
	assert.Equal(t, "alb.ingress.kubernetes.io/conditions.route", ALBHeaderBasedConditionAnnotationKey(r, "route"))
}
