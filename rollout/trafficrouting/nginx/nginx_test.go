package nginx

import (
	"fmt"
	"testing"

	"k8s.io/utils/pointer"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	networkingv1 "k8s.io/api/networking/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	kubeinformers "k8s.io/client-go/informers"
	fake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	"github.com/argoproj/argo-rollouts/utils/record"
)

const StableIngress string = "stable-ingress"
const CanaryIngress string = "rollout-stable-ingress-canary"
const StableIngresses string = "additional-stable-ingress"
const stableService string = "stable-service"
const canaryService string = "canary-service"
const createCanaryAction string = "action: create canary ingress"

type multiIngressTestData struct {
	name            string
	singleIngress   string
	canaryIngresses []string
	multiIngress    []string
	ingresses       []string
}

func generateMultiIngressTestData() []multiIngressTestData {

	return []multiIngressTestData{{
		"singleIngress",
		StableIngress,
		[]string{CanaryIngress},
		nil,
		[]string{StableIngress},
	},
		{
			"multiIngress",
			"",
			[]string{CanaryIngress, "rollout-additional-stable-ingress-canary"},
			[]string{StableIngress, StableIngresses},
			[]string{StableIngress, StableIngresses},
		}}
}

func networkingIngress(name string, port int, serviceName string) *networkingv1.Ingress {
	ingressClassName := "ingress-name"
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
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
					Name: serviceName,
					Port: networkingv1.ServiceBackendPort{
						Name:   "http",
						Number: int32(port),
					},
				},
			},
			Rules: []networkingv1.IngressRule{
				{
					Host: "fakehost.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path: "/foo",
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: serviceName,
											Port: networkingv1.ServiceBackendPort{
												Name:   "http",
												Number: int32(port),
											},
										},
										Resource: &corev1.TypedLocalObjectReference{
											APIGroup: new(string),
											Kind:     "",
											Name:     name,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func extensionsIngress(name string, port int, serviceName string) *extensionsv1beta1.Ingress {
	return &extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: extensionsv1beta1.IngressSpec{
			Rules: []extensionsv1beta1.IngressRule{
				{
					Host: "fakehost.example.com",
					IngressRuleValue: extensionsv1beta1.IngressRuleValue{
						HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
							Paths: []extensionsv1beta1.HTTPIngressPath{
								{
									Path: "/foo",
									Backend: extensionsv1beta1.IngressBackend{
										ServiceName: serviceName,
										ServicePort: intstr.FromInt(port),
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func setIngressOwnerRef(ing *extensionsv1beta1.Ingress, rollout *v1alpha1.Rollout) {
	ing.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(rollout, schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Rollout"})})
}

func fakeRollout(stableSvc, canarySvc, stableIng string, addStableIngs []string) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: stableSvc,
					CanaryService: canarySvc,
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Nginx: &v1alpha1.NginxTrafficRouting{
							StableIngress:   stableIng,
							StableIngresses: addStableIngs,
						},
					},
				},
			},
		},
	}
}

func checkBackendService(t *testing.T, ing *ingressutil.Ingress, serviceName string) {
	t.Helper()
	switch ing.Mode() {
	case ingressutil.IngressModeNetworking:
		networkingIngress, err := ing.GetNetworkingIngress()
		if err != nil {
			t.Error(err)
		}
		checkIngressBackendService(t, networkingIngress, serviceName)
	case ingressutil.IngressModeExtensions:
		extensionsIngress, err := ing.GetExtensionsIngress()
		if err != nil {
			t.Error(err)
		}
		checkBackendServiceLegacy(t, extensionsIngress, serviceName)
	}
}

func checkIngressBackendService(t *testing.T, ing *networkingv1.Ingress, serviceName string) {
	t.Helper()
	for ir := 0; ir < len(ing.Spec.Rules); ir++ {
		for ip := 0; ip < len(ing.Spec.Rules[ir].HTTP.Paths); ip++ {
			assert.Equal(t, serviceName, ing.Spec.Rules[ir].HTTP.Paths[ip].Backend.Service.Name)
			return
		}
	}
	msg := fmt.Sprintf("Service '%s' not found within backends of ingress '%s'", serviceName, ing.Name)
	assert.Fail(t, msg)
}
func checkBackendServiceLegacy(t *testing.T, ing *extensionsv1beta1.Ingress, serviceName string) {
	t.Helper()
	for ir := 0; ir < len(ing.Spec.Rules); ir++ {
		for ip := 0; ip < len(ing.Spec.Rules[ir].HTTP.Paths); ip++ {
			assert.Equal(t, serviceName, ing.Spec.Rules[ir].HTTP.Paths[ip].Backend.ServiceName)
			return
		}
	}
	msg := fmt.Sprintf("Service '%s' not found within backends of ingress '%s'", serviceName, ing.Name)
	assert.Fail(t, msg)
}

func checkTLS(t *testing.T, ing *ingressutil.Ingress, hosts [][]string, secretNames []string) {
	t.Helper()
	switch ing.Mode() {
	case ingressutil.IngressModeNetworking:
		networkingIngress, err := ing.GetNetworkingIngress()
		if err != nil {
			t.Error(err)
		}
		checkIngressTLS(t, networkingIngress, hosts, secretNames)
	case ingressutil.IngressModeExtensions:
		extensionsIngress, err := ing.GetExtensionsIngress()
		if err != nil {
			t.Error(err)
		}
		checkTLSLegacy(t, extensionsIngress, hosts, secretNames)
	}
}

func checkIngressTLS(t *testing.T, ing *networkingv1.Ingress, hosts [][]string, secretNames []string) {
	t.Helper()
	assert.Equal(t, len(hosts), len(ing.Spec.TLS), "Count of TLS rules differs")
	for it := 0; it < len(ing.Spec.TLS); it++ {
		assert.Equal(t, secretNames[it], ing.Spec.TLS[it].SecretName, "Secret name differs")
		assert.Equal(t, len(hosts[it]), len(ing.Spec.TLS[it].Hosts), "Count of hosts differs")
		for ih := 0; ih < len(ing.Spec.TLS[it].Hosts); ih++ {
			assert.Equal(t, hosts[it][ih], ing.Spec.TLS[it].Hosts[ih])
		}
	}
}

func checkTLSLegacy(t *testing.T, ing *extensionsv1beta1.Ingress, hosts [][]string, secretNames []string) {
	t.Helper()
	assert.Equal(t, len(hosts), len(ing.Spec.TLS), "Count of TLS rules differs")
	for it := 0; it < len(ing.Spec.TLS); it++ {
		assert.Equal(t, secretNames[it], ing.Spec.TLS[it].SecretName, "Secret name differs")
		assert.Equal(t, len(hosts[it]), len(ing.Spec.TLS[it].Hosts), "Count of hosts differs")
		for ih := 0; ih < len(ing.Spec.TLS[it].Hosts); ih++ {
			assert.Equal(t, hosts[it][ih], ing.Spec.TLS[it].Hosts[ih])
		}
	}
}

func TestCanaryIngressCreate(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := Reconciler{
				cfg: ReconcilerConfig{
					Rollout: fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress),
				},
			}
			for _, ing := range test.ingresses {

				stableIngress := extensionsIngress(ing, 80, stableService)
				stableIngress.Spec.IngressClassName = pointer.StringPtr("nginx-ext")
				i := ingressutil.NewLegacyIngress(stableIngress)

				desiredCanaryIngress, err := r.canaryIngress(i, ingressutil.GetCanaryIngressName(r.cfg.Rollout.GetName(), ing), 10)
				assert.Nil(t, err, "No error returned when calling canaryIngress")

				checkBackendService(t, desiredCanaryIngress, canaryService)
				desired, err := desiredCanaryIngress.GetExtensionsIngress()
				if err != nil {
					t.Error(err)
					t.FailNow()
				}

				assert.Equal(t, "true", desired.Annotations["nginx.ingress.kubernetes.io/canary"], "canary annotation set to true")
				assert.Equal(t, "10", desired.Annotations["nginx.ingress.kubernetes.io/canary-weight"], "canary-weight annotation set to expected value")
				assert.NotNil(t, desired.Spec.IngressClassName)
				assert.Equal(t, "nginx-ext", *desired.Spec.IngressClassName)
			}

		})
	}
}

func TestCanaryIngressPatchWeight(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := Reconciler{
				cfg: ReconcilerConfig{
					Rollout: fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress),
				},
			}
			for _, ing := range test.ingresses {
				stable := extensionsIngress(ing, 80, stableService)
				canary := extensionsIngress("canary-ingress", 80, canaryService)
				canary.SetAnnotations(map[string]string{
					"nginx.ingress.kubernetes.io/canary":        "true",
					"nginx.ingress.kubernetes.io/canary-weight": "10",
				})
				stableIngress := ingressutil.NewLegacyIngress(stable)
				canaryIngress := ingressutil.NewLegacyIngress(canary)

				desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout.GetName(), ing), 15)
				assert.Nil(t, err, "No error returned when calling canaryIngress")

				checkBackendService(t, desiredCanaryIngress, canaryService)

				patch, modified, err := ingressutil.BuildIngressPatch(canaryIngress.Mode(), canaryIngress, desiredCanaryIngress,
					ingressutil.WithAnnotations(), ingressutil.WithLabels(), ingressutil.WithSpec())
				assert.Nil(t, err, "compareCanaryIngresses returns no error")
				assert.True(t, modified, "compareCanaryIngresses returns modified=true")
				assert.Equal(t, "{\"metadata\":{\"annotations\":{\"nginx.ingress.kubernetes.io/canary-weight\":\"15\"}}}", string(patch), "compareCanaryIngresses returns expected patch")
			}
		})
	}
}

func TestCanaryIngressUpdateRoute(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := Reconciler{
				cfg: ReconcilerConfig{
					Rollout: fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress),
				},
			}

			for i, ing := range test.ingresses {
				stable := extensionsIngress(ing, 80, stableService)
				stable.Spec.Rules[0].HTTP.Paths[0].Path = "/bar"
				canary := extensionsIngress(test.canaryIngresses[i], 80, canaryService)
				canary.SetAnnotations(map[string]string{
					"nginx.ingress.kubernetes.io/canary":        "true",
					"nginx.ingress.kubernetes.io/canary-weight": "15",
				})
				stableIngress := ingressutil.NewLegacyIngress(stable)
				canaryIngress := ingressutil.NewLegacyIngress(canary)

				desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout.GetName(), ing), 15)
				assert.Nil(t, err, "No error returned when calling canaryIngress")

				checkBackendService(t, desiredCanaryIngress, canaryService)

				patch, modified, err := ingressutil.BuildIngressPatch(canaryIngress.Mode(), canaryIngress, desiredCanaryIngress,
					ingressutil.WithAnnotations(), ingressutil.WithLabels(), ingressutil.WithSpec())
				assert.Nil(t, err, "compareCanaryIngresses returns no error")
				assert.True(t, modified, "compareCanaryIngresses returns modified=true")
				assert.Equal(t, "{\"spec\":{\"rules\":[{\"host\":\"fakehost.example.com\",\"http\":{\"paths\":[{\"backend\":{\"serviceName\":\"canary-service\",\"servicePort\":80},\"path\":\"/bar\"}]}}]}}", string(patch), "compareCanaryIngresses returns expected patch")
			}
		})
	}
}

func TestCanaryIngressRetainIngressClass(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := Reconciler{
				cfg: ReconcilerConfig{
					Rollout: fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress),
				},
			}
			for _, ing := range test.ingresses {
				stable := extensionsIngress(ing, 80, stableService)
				stable.SetAnnotations(map[string]string{
					"kubernetes.io/ingress.class": "nginx-foo",
				})
				stableIngress := ingressutil.NewLegacyIngress(stable)

				desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout.GetName(), ing), 15)
				assert.Nil(t, err, "No error returned when calling canaryIngress")

				checkBackendService(t, desiredCanaryIngress, canaryService)

				annotations := desiredCanaryIngress.GetAnnotations()
				assert.Equal(t, "true", annotations["nginx.ingress.kubernetes.io/canary"], "canary annotation set to true")
				assert.Equal(t, "15", annotations["nginx.ingress.kubernetes.io/canary-weight"], "canary-weight annotation set to expected value")
				assert.Equal(t, "nginx-foo", annotations["kubernetes.io/ingress.class"], "ingress-class annotation retained")
			}
		})
	}
}

func TestCanaryIngressRetainTLS(t *testing.T) {
	r := Reconciler{
		cfg: ReconcilerConfig{
			Rollout: fakeRollout(stableService, canaryService, StableIngress, nil),
		},
	}
	stable := networkingIngress(StableIngress, 80, stableService)
	stable.Spec.TLS = []networkingv1.IngressTLS{
		{
			Hosts:      []string{"fakehost.example.com"},
			SecretName: "tls-secret-name",
		},
		{
			Hosts:      []string{"fakehost.example.com", "*.example.com"},
			SecretName: "tls-secret-name-two",
		},
	}
	stableIngress := ingressutil.NewIngress(stable)

	desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout.GetName(), StableIngress), 15)
	assert.Nil(t, err, "No error returned when calling canaryIngress")

	checkTLS(t, desiredCanaryIngress, [][]string{
		{"fakehost.example.com"},
		{"fakehost.example.com", "*.example.com"},
	}, []string{
		"tls-secret-name",
		"tls-secret-name-two",
	})
}

func TestCanaryIngressRetainTLSWithMultipleStableIngresses(t *testing.T) {
	r := Reconciler{
		cfg: ReconcilerConfig{
			Rollout: fakeRollout(stableService, canaryService, StableIngress, []string{StableIngresses}),
		},
	}

	// main
	stable := networkingIngress(StableIngress, 80, stableService)
	stable.Spec.TLS = []networkingv1.IngressTLS{
		{
			Hosts:      []string{"fakehost.example.com"},
			SecretName: "tls-secret-name",
		},
	}
	stableIngress := ingressutil.NewIngress(stable)

	desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout.GetName(), StableIngress), 15)
	assert.Nil(t, err, "No error returned when calling canaryIngress")

	checkTLS(t, desiredCanaryIngress, [][]string{
		{"fakehost.example.com"},
	}, []string{
		"tls-secret-name",
	})

	// additional
	stableAdditional := networkingIngress(StableIngresses, 80, stableService)
	stableAdditional.Spec.TLS = []networkingv1.IngressTLS{
		{
			Hosts:      []string{"fakehost-additional.example.com"},
			SecretName: "tls-secret-name-additional",
		},
	}
	stableAdditionalIngress := ingressutil.NewIngress(stableAdditional)

	desiredAdditionalCanaryIngress, err := r.canaryIngress(stableAdditionalIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout.GetName(), StableIngresses), 15)
	assert.Nil(t, err, "No error returned when calling canaryIngress")

	checkTLS(t, desiredAdditionalCanaryIngress, [][]string{
		{"fakehost-additional.example.com"},
	}, []string{
		"tls-secret-name-additional",
	})

}

func TestCanaryIngressRetainLegacyTLS(t *testing.T) {
	r := Reconciler{
		cfg: ReconcilerConfig{
			Rollout: fakeRollout(stableService, canaryService, StableIngress, nil),
		},
	}
	stable := extensionsIngress(StableIngress, 80, stableService)
	stable.Spec.TLS = []extensionsv1beta1.IngressTLS{
		{
			Hosts:      []string{"fakehost.example.com"},
			SecretName: "tls-secret-name",
		},
		{
			Hosts:      []string{"fakehost.example.com", "*.example.com"},
			SecretName: "tls-secret-name-two",
		},
	}
	stableIngress := ingressutil.NewLegacyIngress(stable)

	desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout.GetName(), StableIngress), 15)
	assert.Nil(t, err, "No error returned when calling canaryIngress")

	checkTLS(t, desiredCanaryIngress, [][]string{
		{"fakehost.example.com"},
		{"fakehost.example.com", "*.example.com"},
	}, []string{
		"tls-secret-name",
		"tls-secret-name-two",
	})
}

func TestCanaryIngressNotAddTLS(t *testing.T) {
	r := Reconciler{
		cfg: ReconcilerConfig{
			Rollout: fakeRollout(stableService, canaryService, StableIngress, nil),
		},
	}
	stable := networkingIngress(StableIngress, 80, stableService)

	stableIngress := ingressutil.NewIngress(stable)

	desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout.GetName(), StableIngress), 15)
	assert.Nil(t, err, "No error returned when calling canaryIngress")

	desired, err := desiredCanaryIngress.GetNetworkingIngress()

	if err != nil {
		t.Fatal(err)
	}

	assert.Nil(t, desired.Spec.TLS, "No TLS in the the canary ingress should be present")
}

func TestCanaryIngressNotAddLegacyTLS(t *testing.T) {
	r := Reconciler{
		cfg: ReconcilerConfig{
			Rollout: fakeRollout(stableService, canaryService, StableIngress, nil),
		},
	}
	stable := extensionsIngress(StableIngress, 80, stableService)

	stableIngress := ingressutil.NewLegacyIngress(stable)

	desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout.GetName(), StableIngress), 15)
	assert.Nil(t, err, "No error returned when calling canaryIngress")

	desired, err := desiredCanaryIngress.GetExtensionsIngress()

	if err != nil {
		t.Fatal(err)
	}

	assert.Nil(t, desired.Spec.TLS, "No TLS in the the canary ingress should be present")
}

func TestCanaryIngressAdditionalAnnotations(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, ing := range test.ingresses {
				r := Reconciler{
					cfg: ReconcilerConfig{
						Rollout: fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress),
					},
				}
				r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.AdditionalIngressAnnotations = map[string]string{
					"canary-by-header":       "X-Foo",
					"canary-by-header-value": "DoCanary",
				}
				stable := extensionsIngress(ing, 80, stableService)
				stableIngress := ingressutil.NewLegacyIngress(stable)

				desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout.GetName(), ing), 15)
				assert.Nil(t, err, "No error returned when calling canaryIngress")

				checkBackendService(t, desiredCanaryIngress, canaryService)

				annotations := desiredCanaryIngress.GetAnnotations()
				assert.Equal(t, "true", annotations["nginx.ingress.kubernetes.io/canary"], "canary annotation set to true")
				assert.Equal(t, "15", annotations["nginx.ingress.kubernetes.io/canary-weight"], "canary-weight annotation set to expected value")
				assert.Equal(t, "X-Foo", annotations["nginx.ingress.kubernetes.io/canary-by-header"], "canary-by-header annotation set")
				assert.Equal(t, "DoCanary", annotations["nginx.ingress.kubernetes.io/canary-by-header-value"], "canary-by-header-value annotation set")
			}
		})
	}
}

func TestReconciler_canaryIngress(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// given
			r := Reconciler{
				cfg: ReconcilerConfig{
					Rollout: fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress),
				},
			}
			for _, ing := range test.ingresses {
				stableIngress := networkingIngress(ing, 80, stableService)
				stableIngress.Spec.IngressClassName = pointer.StringPtr("nginx-ext")
				i := ingressutil.NewIngress(stableIngress)

				// when
				desiredCanaryIngress, err := r.canaryIngress(i, ingressutil.GetCanaryIngressName(r.cfg.Rollout.GetName(), ing), 10)

				// then
				assert.Nil(t, err, "No error returned when calling canaryIngress")
				checkBackendService(t, desiredCanaryIngress, canaryService)
				desired, err := desiredCanaryIngress.GetNetworkingIngress()
				if err != nil {
					t.Fatal(err)
				}
				assert.Equal(t, "true", desired.Annotations["nginx.ingress.kubernetes.io/canary"], "canary annotation set to true")
				assert.Equal(t, "10", desired.Annotations["nginx.ingress.kubernetes.io/canary-weight"], "canary-weight annotation set to expected value")
				assert.NotNil(t, desired.Spec.IngressClassName)
				assert.Equal(t, "nginx-ext", *desired.Spec.IngressClassName)
			}
		})
	}
}

func TestType(t *testing.T) {
	client := fake.NewSimpleClientset()
	rollout := fakeRollout(stableService, canaryService, StableIngress, nil)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
	})
	assert.Equal(t, Type, r.Type())
}

func TestReconcileStableIngressNotFound(t *testing.T) {
	rollout := fakeRollout(stableService, canaryService, StableIngress, nil)
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
	if err != nil {
		t.Fatal(err)
	}
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressWrapper: ingressWrapper,
	})

	err = r.SetWeight(10)
	assert.NotNil(t, err, "Reconcile returns error")
}

func TestReconcileStableIngressFound(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rollout := fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress)
			client := fake.NewSimpleClientset()
			k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
			for _, ing := range test.ingresses {
				ingress := extensionsIngress(ing, 80, stableService)
				k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(ingress)
			}
			ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
			if err != nil {
				t.Fatal(err)
			}
			r := NewReconciler(ReconcilerConfig{
				Rollout:        rollout,
				Client:         client,
				Recorder:       record.NewFakeEventRecorder(),
				ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
				IngressWrapper: ingressWrapper,
			})
			err = r.SetWeight(10)
			assert.Nil(t, err, "Reconcile returns no error")
			actions := client.Actions()
			assert.Len(t, actions, len(test.ingresses))
			if !t.Failed() {
				// Avoid "index out of range" errors
				for i := range test.ingresses {
					assert.Equal(t, "create", actions[i].GetVerb(), createCanaryAction)
					assert.Equal(t, schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "ingresses"}, actions[i].GetResource(), createCanaryAction)
				}
			}
		})
	}
}

func TestReconcileStableIngressFoundWrongBackend(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rollout := fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress)
			client := fake.NewSimpleClientset()
			k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
			for _, ing := range test.ingresses {
				k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(extensionsIngress(ing, 80, "other-service"))
			}

			ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
			if err != nil {
				t.Fatal(err)
			}
			r := NewReconciler(ReconcilerConfig{
				Rollout:        rollout,
				Client:         client,
				Recorder:       record.NewFakeEventRecorder(),
				ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
				IngressWrapper: ingressWrapper,
			})

			err = r.SetWeight(10)
			assert.NotNil(t, err, "Reconcile returns error")
			assert.Contains(t, err.Error(), "has no rules using service", "correct error is returned")
		})
	}
}

func TestReconcileStableAndCanaryIngressFoundNoOwner(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rollout := fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress)
			stableIngress := extensionsIngress(StableIngress, 80, stableService)
			canaryIngress := extensionsIngress(CanaryIngress, 80, canaryService)

			client := fake.NewSimpleClientset()
			k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
			k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(stableIngress)
			k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(canaryIngress)
			ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
			if err != nil {
				t.Fatal(err)
			}
			r := NewReconciler(ReconcilerConfig{
				Rollout:        rollout,
				Client:         client,
				Recorder:       record.NewFakeEventRecorder(),
				ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
				IngressWrapper: ingressWrapper,
			})

			err = r.SetWeight(10)
			assert.NotNil(t, err, "Reconcile returns error")
		})
	}
}

func TestReconcileStableAndCanaryIngressFoundBadOwner(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			otherRollout := fakeRollout("stable-service2", "canary-service2", "stable-ingress2", nil)
			otherRollout.SetUID("4b712b69-5de9-11ea-a10a-0a9ba5899dd2")
			rollout := fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress)
			stableIngress := extensionsIngress(StableIngress, 80, stableService)
			canaryIngress := extensionsIngress(CanaryIngress, 80, canaryService)
			setIngressOwnerRef(canaryIngress, otherRollout)
			client := fake.NewSimpleClientset()
			k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
			k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(stableIngress)
			k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(canaryIngress)
			ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
			if err != nil {
				t.Fatal(err)
			}

			r := NewReconciler(ReconcilerConfig{
				Rollout:        rollout,
				Client:         client,
				Recorder:       record.NewFakeEventRecorder(),
				ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
				IngressWrapper: ingressWrapper,
			})

			err = r.SetWeight(10)
			assert.NotNil(t, err, "Reconcile returns error")
		})
	}
}

func TestReconcileStableAndCanaryIngressFoundPatch(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rollout := fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress)
			var canaryIngresses []*extensionsv1beta1.Ingress
			var ingresses []*extensionsv1beta1.Ingress
			for i, ing := range test.ingresses {
				stableIngress := extensionsIngress(ing, 80, stableService)
				canaryIngress := extensionsIngress(test.canaryIngresses[i], 80, canaryService)
				canaryIngress.SetAnnotations(map[string]string{
					"nginx.ingress.kubernetes.io/canary":        "true",
					"nginx.ingress.kubernetes.io/canary-weight": "15",
				})
				setIngressOwnerRef(canaryIngress, rollout)
				canaryIngresses = append(canaryIngresses, canaryIngress)
				ingresses = append(ingresses, stableIngress)
				ingresses = append(ingresses, canaryIngress)
			}
			//
			var client *fake.Clientset
			if len(canaryIngresses) == 1 {
				client = fake.NewSimpleClientset(canaryIngresses[0])
			} else {
				client = fake.NewSimpleClientset(canaryIngresses[0], canaryIngresses[1])
			}
			k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
			for _, ing := range ingresses {
				k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(ing)
			}
			ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
			if err != nil {
				t.Fatal(err)
			}
			r := NewReconciler(ReconcilerConfig{
				Rollout:        rollout,
				Client:         client,
				Recorder:       record.NewFakeEventRecorder(),
				ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
				IngressWrapper: ingressWrapper,
			})

			err = r.SetWeight(10)
			assert.Nil(t, err, "Reconcile returns no error")
			actions := client.Actions()
			assert.Len(t, actions, len(test.ingresses))
			if !t.Failed() {
				// Avoid "index out of range" errors
				for i := range test.ingresses {
					assert.Equal(t, "patch", actions[i].GetVerb(), "action: patch canary ingress")
					assert.Equal(t, schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "ingresses"}, actions[i].GetResource(), "action: patch canary ingress")
				}
			}
		})
	}
}

func TestReconcileWillInvokeNetworkingIngress(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// given
			rollout := fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress)
			var ingresses []*networkingv1.Ingress
			for i, ing := range test.ingresses {
				stableIngress := networkingIngress(ing, 80, stableService)
				canaryIngress := networkingIngress(test.canaryIngresses[i], 80, canaryService)
				canaryIngress.SetAnnotations(map[string]string{
					"nginx.ingress.kubernetes.io/canary":        "true",
					"nginx.ingress.kubernetes.io/canary-weight": "15",
				})
				canaryIngress.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(rollout, schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "Rollout"})})
				ingresses = append(ingresses, stableIngress)
				ingresses = append(ingresses, canaryIngress)
			}

			var client *fake.Clientset
			if len(ingresses) == 2 {
				client = fake.NewSimpleClientset(ingresses[0], ingresses[1])
			} else {
				client = fake.NewSimpleClientset(ingresses[0], ingresses[1], ingresses[2], ingresses[3])
			}

			k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
			for _, ing := range ingresses {
				k8sI.Networking().V1().Ingresses().Informer().GetIndexer().Add(ing)
			}
			ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeNetworking, client, k8sI)
			if err != nil {
				t.Fatal(err)
			}

			r := NewReconciler(ReconcilerConfig{
				Rollout:        rollout,
				Client:         client,
				Recorder:       record.NewFakeEventRecorder(),
				ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
				IngressWrapper: ingressWrapper,
			})

			// when
			err = r.SetWeight(10)

			// then
			assert.Nil(t, err, "Reconcile returns no error")
			actions := client.Actions()
			assert.Len(t, actions, len(test.canaryIngresses))
			if !t.Failed() {
				// Avoid "index out of range" errors
				for i := range test.canaryIngresses {
					assert.Equal(t, "patch", actions[i].GetVerb(), "action: patch canary ingress")
					assert.Equal(t, schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, actions[i].GetResource(), "action: patch canary ingress")
				}
			}
		})
	}
}

func TestReconcileStableAndCanaryIngressFoundNoChange(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// given
			rollout := fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress)
			client := fake.NewSimpleClientset()
			k8sI := kubeinformers.NewSharedInformerFactory(client, 0)

			for i, ing := range test.ingresses {
				stableIngress := extensionsIngress(ing, 80, stableService)
				canaryIngress := extensionsIngress(test.canaryIngresses[i], 80, canaryService)
				setIngressOwnerRef(canaryIngress, rollout)
				canaryIngress.SetAnnotations(map[string]string{
					"nginx.ingress.kubernetes.io/canary":        "true",
					"nginx.ingress.kubernetes.io/canary-weight": "10",
				})
				k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(stableIngress)
				k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(canaryIngress)
			}

			ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
			if err != nil {
				t.Fatal(err)
			}

			r := NewReconciler(ReconcilerConfig{
				Rollout:        rollout,
				Client:         client,
				Recorder:       record.NewFakeEventRecorder(),
				ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
				IngressWrapper: ingressWrapper,
			})

			// when
			err = r.SetWeight(10)

			// then
			assert.Nil(t, err, "Reconcile returns no error")
			actions := client.Actions()
			assert.Len(t, actions, 0)
		})
	}
}

func TestReconcileCanaryCreateError(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// given
			rollout := fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress)
			client := fake.NewSimpleClientset()
			client.ReactionChain = nil
			k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
			for _, ing := range test.ingresses {
				k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(extensionsIngress(ing, 80, stableService))
			}
			ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
			if err != nil {
				t.Fatal(err)
			}
			r := NewReconciler(ReconcilerConfig{
				Rollout:        rollout,
				Client:         client,
				Recorder:       record.NewFakeEventRecorder(),
				ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
				IngressWrapper: ingressWrapper,
			})

			// Return with AlreadyExists error to create for canary
			r.cfg.Client.(*fake.Clientset).Fake.AddReactor("create", "ingresses", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
				return true, nil, errors.New("fake error")
			})

			// when
			err = r.SetWeight(10)

			// then
			assert.NotNil(t, err, "Reconcile returns error")
			assert.Equal(t, fmt.Sprintf("error creating canary ingress `%s`: fake error", test.canaryIngresses[0]), err.Error())
			actions := client.Actions()
			assert.Len(t, actions, 1)
			if !t.Failed() {
				// Avoid "index out of range" errors
				assert.Equal(t, "create", actions[0].GetVerb(), createCanaryAction)
				assert.Equal(t, schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "ingresses"}, actions[0].GetResource(), createCanaryAction)
			}
		})
	}
}

func TestReconcileCanaryCreateErrorAlreadyExistsPatch(t *testing.T) {
	tests := generateMultiIngressTestData()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// given
			rollout := fakeRollout(stableService, canaryService, test.singleIngress, test.multiIngress)
			client := fake.NewSimpleClientset()
			client.ReactionChain = nil
			k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
			var canaryIngresses []*extensionsv1beta1.Ingress
			for i, ing := range test.ingresses {
				stableIngress := extensionsIngress(ing, 80, stableService)
				canaryIngress := extensionsIngress(test.canaryIngresses[i], 80, canaryService)
				canaryIngress.SetAnnotations(map[string]string{
					"nginx.ingress.kubernetes.io/canary":        "true",
					"nginx.ingress.kubernetes.io/canary-weight": "15",
				})
				setIngressOwnerRef(canaryIngress, rollout)
				canaryIngresses = append(canaryIngresses, canaryIngress)
				k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(stableIngress)
			}
			ingressWrapper, err := ingressutil.NewIngressWrapper(ingressutil.IngressModeExtensions, client, k8sI)
			if err != nil {
				t.Fatal(err)
			}

			r := NewReconciler(ReconcilerConfig{
				Rollout:        rollout,
				Client:         client,
				Recorder:       record.NewFakeEventRecorder(),
				ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
				IngressWrapper: ingressWrapper,
			})

			// Return with AlreadyExists error to create for canary
			r.cfg.Client.(*fake.Clientset).Fake.AddReactor("create", "ingresses", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
				return true, nil, k8serrors.NewAlreadyExists(schema.GroupResource{
					Group:    "extensions",
					Resource: "ingresses",
				}, test.canaryIngresses[0])
			})

			// Respond with canaryIngress on GET
			r.cfg.Client.(*fake.Clientset).Fake.AddReactor("get", "ingresses", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
				return true, canaryIngresses[0], nil
			})

			// when
			err = r.SetWeight(10)

			// then
			assert.Nil(t, err, "Reconcile returns no error")
			actions := client.Actions()
			assert.Len(t, actions, len(test.ingresses)*3)
			if !t.Failed() {
				// Avoid "index out of range" errors
				for i := range test.ingresses {
					assert.Equal(t, "create", actions[3*i+0].GetVerb(), createCanaryAction)
					assert.Equal(t, schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "ingresses"}, actions[0].GetResource(), createCanaryAction)
					assert.Equal(t, "get", actions[3*i+1].GetVerb(), "action: get canary ingress")
					assert.Equal(t, schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "ingresses"}, actions[1].GetResource(), "action: get canary ingress")
					assert.Equal(t, "patch", actions[3*i+2].GetVerb(), "action: patch canary ingress")
					assert.Equal(t, schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "ingresses"}, actions[2].GetResource(), "action: patch canary ingress")
				}
			}
		})
	}
}
