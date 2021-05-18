package nginx

import (
	"fmt"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
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

func ingress(name string, port int, serviceName string) *extensionsv1beta1.Ingress {
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

func fakeRollout(stableSvc, canarySvc, stableIng string) *v1alpha1.Rollout {
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
							StableIngress: stableIng,
						},
					},
				},
			},
		},
	}
}

func checkBackendService(t *testing.T, ing *extensionsv1beta1.Ingress, serviceName string) {
	for ir := 0; ir < len(ing.Spec.Rules); ir++ {
		for ip := 0; ip < len(ing.Spec.Rules[ir].HTTP.Paths); ip++ {
			assert.Equal(t, serviceName, ing.Spec.Rules[ir].HTTP.Paths[ip].Backend.ServiceName)
			return
		}
	}
	msg := fmt.Sprintf("Service '%s' not found within backends of ingress '%s'", serviceName, ing.Name)
	assert.Fail(t, msg)
}

func TestCanaryIngressCreate(t *testing.T) {
	r := Reconciler{
		cfg: ReconcilerConfig{
			Rollout: fakeRollout("stable-service", "canary-service", "stable-ingress"),
		},
	}
	stableIngress := ingress("stable-ingress", 80, "stable-service")

	desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout), 10)
	assert.Nil(t, err, "No error returned when calling canaryIngress")

	checkBackendService(t, desiredCanaryIngress, "canary-service")
	assert.Equal(t, "true", desiredCanaryIngress.Annotations["nginx.ingress.kubernetes.io/canary"], "canary annotation set to true")
	assert.Equal(t, "10", desiredCanaryIngress.Annotations["nginx.ingress.kubernetes.io/canary-weight"], "canary-weight annotation set to expected value")
}

func TestCanaryIngressPatchWeight(t *testing.T) {
	r := Reconciler{
		cfg: ReconcilerConfig{
			Rollout: fakeRollout("stable-service", "canary-service", "stable-ingress"),
		},
	}
	stableIngress := ingress("stable-ingress", 80, "stable-service")
	canaryIngress := ingress("canary-ingress", 80, "canary-service")
	canaryIngress.SetAnnotations(map[string]string{
		"nginx.ingress.kubernetes.io/canary":        "true",
		"nginx.ingress.kubernetes.io/canary-weight": "10",
	})

	desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout), 15)
	assert.Nil(t, err, "No error returned when calling canaryIngress")

	checkBackendService(t, desiredCanaryIngress, "canary-service")

	patch, modified, err := compareCanaryIngresses(canaryIngress, desiredCanaryIngress)
	assert.Nil(t, err, "compareCanaryIngresses returns no error")
	assert.True(t, modified, "compareCanaryIngresses returns modified=true")
	assert.Equal(t, "{\"metadata\":{\"annotations\":{\"nginx.ingress.kubernetes.io/canary-weight\":\"15\"}}}", string(patch), "compareCanaryIngresses returns expected patch")
}

func TestCanaryIngressUpdatedRoute(t *testing.T) {
	r := Reconciler{
		cfg: ReconcilerConfig{
			Rollout: fakeRollout("stable-service", "canary-service", "stable-ingress"),
		},
	}
	stableIngress := ingress("stable-ingress", 80, "stable-service")
	stableIngress.Spec.Rules[0].HTTP.Paths[0].Path = "/bar"
	canaryIngress := ingress("canary-ingress", 80, "canary-service")
	canaryIngress.SetAnnotations(map[string]string{
		"nginx.ingress.kubernetes.io/canary":        "true",
		"nginx.ingress.kubernetes.io/canary-weight": "15",
	})

	desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout), 15)
	assert.Nil(t, err, "No error returned when calling canaryIngress")

	checkBackendService(t, desiredCanaryIngress, "canary-service")

	patch, modified, err := compareCanaryIngresses(canaryIngress, desiredCanaryIngress)
	assert.Nil(t, err, "compareCanaryIngresses returns no error")
	assert.True(t, modified, "compareCanaryIngresses returns modified=true")
	assert.Equal(t, "{\"spec\":{\"rules\":[{\"host\":\"fakehost.example.com\",\"http\":{\"paths\":[{\"backend\":{\"serviceName\":\"canary-service\",\"servicePort\":80},\"path\":\"/bar\"}]}}]}}", string(patch), "compareCanaryIngresses returns expected patch")
}

func TestCanaryIngressRetainIngressClass(t *testing.T) {
	r := Reconciler{
		cfg: ReconcilerConfig{
			Rollout: fakeRollout("stable-service", "canary-service", "stable-ingress"),
		},
	}
	stableIngress := ingress("stable-ingress", 80, "stable-service")
	stableIngress.SetAnnotations(map[string]string{
		"kubernetes.io/ingress.class": "nginx-foo",
	})
	desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout), 15)
	assert.Nil(t, err, "No error returned when calling canaryIngress")

	checkBackendService(t, desiredCanaryIngress, "canary-service")

	assert.Equal(t, "true", desiredCanaryIngress.Annotations["nginx.ingress.kubernetes.io/canary"], "canary annotation set to true")
	assert.Equal(t, "15", desiredCanaryIngress.Annotations["nginx.ingress.kubernetes.io/canary-weight"], "canary-weight annotation set to expected value")
	assert.Equal(t, "nginx-foo", desiredCanaryIngress.Annotations["kubernetes.io/ingress.class"], "ingress-class annotation retained")
}

func TestCanaryIngressAdditionalAnnotations(t *testing.T) {
	r := Reconciler{
		cfg: ReconcilerConfig{
			Rollout: fakeRollout("stable-service", "canary-service", "stable-ingress"),
		},
	}
	r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.AdditionalIngressAnnotations = map[string]string{
		"canary-by-header":       "X-Foo",
		"canary-by-header-value": "DoCanary",
	}
	stableIngress := ingress("stable-ingress", 80, "stable-service")

	desiredCanaryIngress, err := r.canaryIngress(stableIngress, ingressutil.GetCanaryIngressName(r.cfg.Rollout), 15)
	assert.Nil(t, err, "No error returned when calling canaryIngress")

	checkBackendService(t, desiredCanaryIngress, "canary-service")

	assert.Equal(t, "true", desiredCanaryIngress.Annotations["nginx.ingress.kubernetes.io/canary"], "canary annotation set to true")
	assert.Equal(t, "15", desiredCanaryIngress.Annotations["nginx.ingress.kubernetes.io/canary-weight"], "canary-weight annotation set to expected value")
	assert.Equal(t, "X-Foo", desiredCanaryIngress.Annotations["nginx.ingress.kubernetes.io/canary-by-header"], "canary-by-header annotation set")
	assert.Equal(t, "DoCanary", desiredCanaryIngress.Annotations["nginx.ingress.kubernetes.io/canary-by-header-value"], "canary-by-header-value annotation set")
}

func TestType(t *testing.T) {
	client := fake.NewSimpleClientset()
	rollout := fakeRollout("stable-service", "canary-service", "stable-ingress")
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
	})
	assert.Equal(t, Type, r.Type())
}

func TestReconcileStableIngressNotFound(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "stable-ingress")
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})

	err := r.SetWeight(10)
	assert.NotNil(t, err, "Reconcile returns error")
}

func TestReconcileStableIngressFound(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "stable-ingress")
	stableIngress := ingress("stable-ingress", 80, "stable-service")

	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(stableIngress)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})

	err := r.SetWeight(10)
	assert.Nil(t, err, "Reconcile returns no error")
	actions := client.Actions()
	assert.Len(t, actions, 1)
	if !t.Failed() {
		// Avoid "index out of range" errors
		assert.Equal(t, "create", actions[0].GetVerb(), "action: create canary ingress")
		assert.Equal(t, schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "ingresses"}, actions[0].GetResource(), "action: create canary ingress")
	}
}

func TestReconcileStableIngressFoundWrongBackend(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "stable-ingress")
	stableIngress := ingress("stable-ingress", 80, "other-service")

	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(stableIngress)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})

	err := r.SetWeight(10)
	assert.NotNil(t, err, "Reconcile returns error")
	assert.Contains(t, err.Error(), "has no rules using service", "correct error is returned")
}

func TestReconcileStableAndCanaryIngressFoundNoOwner(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "stable-ingress")
	stableIngress := ingress("stable-ingress", 80, "stable-service")
	canaryIngress := ingress("rollout-stable-ingress-canary", 80, "canary-service")

	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(stableIngress)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(canaryIngress)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})

	err := r.SetWeight(10)
	assert.NotNil(t, err, "Reconcile returns error")
}

func TestReconcileStableAndCanaryIngressFoundBadOwner(t *testing.T) {
	otherRollout := fakeRollout("stable-service2", "canary-service2", "stable-ingress2")
	otherRollout.SetUID("4b712b69-5de9-11ea-a10a-0a9ba5899dd2")
	rollout := fakeRollout("stable-service", "canary-service", "stable-ingress")
	stableIngress := ingress("stable-ingress", 80, "stable-service")
	canaryIngress := ingress("rollout-stable-ingress-canary", 80, "canary-service")
	setIngressOwnerRef(canaryIngress, otherRollout)
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(stableIngress)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(canaryIngress)

	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})

	err := r.SetWeight(10)
	assert.NotNil(t, err, "Reconcile returns error")
}

func TestReconcileStableAndCanaryIngressFoundPatch(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "stable-ingress")
	stableIngress := ingress("stable-ingress", 80, "stable-service")
	canaryIngress := ingress("rollout-stable-ingress-canary", 80, "canary-service")
	canaryIngress.SetAnnotations(map[string]string{
		"nginx.ingress.kubernetes.io/canary":        "true",
		"nginx.ingress.kubernetes.io/canary-weight": "15",
	})
	setIngressOwnerRef(canaryIngress, rollout)
	client := fake.NewSimpleClientset(canaryIngress)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(stableIngress)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(canaryIngress)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})

	err := r.SetWeight(10)
	assert.Nil(t, err, "Reconcile returns no error")
	actions := client.Actions()
	assert.Len(t, actions, 1)
	if !t.Failed() {
		// Avoid "index out of range" errors
		assert.Equal(t, "patch", actions[0].GetVerb(), "action: patch canary ingress")
		assert.Equal(t, schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "ingresses"}, actions[0].GetResource(), "action: patch canary ingress")
	}
}

func TestReconcileStableAndCanaryIngressFoundNoChange(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "stable-ingress")
	stableIngress := ingress("stable-ingress", 80, "stable-service")
	canaryIngress := ingress("rollout-stable-ingress-canary", 80, "canary-service")
	setIngressOwnerRef(canaryIngress, rollout)
	canaryIngress.SetAnnotations(map[string]string{
		"nginx.ingress.kubernetes.io/canary":        "true",
		"nginx.ingress.kubernetes.io/canary-weight": "10",
	})
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(stableIngress)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(canaryIngress)

	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})

	err := r.SetWeight(10)
	assert.Nil(t, err, "Reconcile returns no error")
	actions := client.Actions()
	assert.Len(t, actions, 0)
}

func TestReconcileCanaryCreateError(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "stable-ingress")
	stableIngress := ingress("stable-ingress", 80, "stable-service")

	client := fake.NewSimpleClientset()
	client.ReactionChain = nil
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)

	// stableIngress exists
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(stableIngress)

	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})

	// Return with AlreadyExists error to create for canary
	r.cfg.Client.(*fake.Clientset).Fake.AddReactor("create", "ingresses", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, errors.New("fake error")
	})

	err := r.SetWeight(10)
	assert.NotNil(t, err, "Reconcile returns error")
	assert.Equal(t, "error creating canary ingress `rollout-stable-ingress-canary`: fake error", err.Error())
	actions := client.Actions()
	assert.Len(t, actions, 1)
	if !t.Failed() {
		// Avoid "index out of range" errors
		assert.Equal(t, "create", actions[0].GetVerb(), "action: create canary ingress")
		assert.Equal(t, schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "ingresses"}, actions[0].GetResource(), "action: create canary ingress")
	}
}

func TestReconcileCanaryCreateErrorAlreadyExistsPatch(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "stable-ingress")
	stableIngress := ingress("stable-ingress", 80, "stable-service")
	canaryIngress := ingress("rollout-stable-ingress-canary", 80, "canary-service")
	canaryIngress.SetAnnotations(map[string]string{
		"nginx.ingress.kubernetes.io/canary":        "true",
		"nginx.ingress.kubernetes.io/canary-weight": "15",
	})
	setIngressOwnerRef(canaryIngress, rollout)

	client := fake.NewSimpleClientset()
	client.ReactionChain = nil
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)

	// stableIngress exists
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(stableIngress)

	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})

	// Return with AlreadyExists error to create for canary
	r.cfg.Client.(*fake.Clientset).Fake.AddReactor("create", "ingresses", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, k8serrors.NewAlreadyExists(schema.GroupResource{
			Group:    "extensions",
			Resource: "ingresses",
		}, "rollout-stable-ingress-canary")
	})

	// Respond with canaryIngress on GET
	r.cfg.Client.(*fake.Clientset).Fake.AddReactor("get", "ingresses", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, canaryIngress, nil
	})

	err := r.SetWeight(10)
	assert.Nil(t, err, "Reconcile returns no error")
	actions := client.Actions()
	assert.Len(t, actions, 3)
	if !t.Failed() {
		// Avoid "index out of range" errors
		assert.Equal(t, "create", actions[0].GetVerb(), "action: create canary ingress")
		assert.Equal(t, schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "ingresses"}, actions[0].GetResource(), "action: create canary ingress")
		assert.Equal(t, "get", actions[1].GetVerb(), "action: get canary ingress")
		assert.Equal(t, schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "ingresses"}, actions[1].GetResource(), "action: get canary ingress")
		assert.Equal(t, "patch", actions[2].GetVerb(), "action: patch canary ingress")
		assert.Equal(t, schema.GroupVersionResource{Group: "extensions", Version: "v1beta1", Resource: "ingresses"}, actions[2].GetResource(), "action: patch canary ingress")
	}
}
