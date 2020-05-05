package alb

import (
	"encoding/json"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"

	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	"github.com/stretchr/testify/assert"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	jsonutil "github.com/argoproj/argo-rollouts/utils/json"
)

func fakeRollout(stableSvc, canarySvc, stableIng string, port int32) *v1alpha1.Rollout {
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
						ALB: &v1alpha1.ALBTrafficRouting{
							Ingress:     stableIng,
							ServicePort: port,
						},
					},
				},
			},
		},
	}
}

const actionTemplate = `{
	"Type":"forward",
	"ForwardConfig":{
		"TargetGroups":[
			{
				"ServiceName":"%s",
				"ServicePort":"%d",
				"Weight":%d
			},{
				"ServiceName":"%s",
				"ServicePort":"%d",
				"Weight":%d
			}
		]
	}
}`

func albActionAnnotation(stable string) string {
	return fmt.Sprintf("%s%s%s", ingressutil.ALBIngressAnnotation, ingressutil.ALBActionPrefix, stable)
}

func ingress(name string, stableSvc, canarySvc string, port, weight int32, managedBy string) *extensionsv1beta1.Ingress {
	managedByValue := fmt.Sprintf("%s:%s", managedBy, albActionAnnotation(stableSvc))
	action := fmt.Sprintf(actionTemplate, stableSvc, port, 100-weight, canarySvc, port, weight)
	var a ingressutil.ALBAction
	err := json.Unmarshal([]byte(action), &a)
	if err != nil {
		panic(err)
	}

	i := &extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{
				albActionAnnotation(stableSvc):       string(jsonutil.MustMarshal(a)),
				ingressutil.ManagedActionsAnnotation: managedByValue,
			},
		},
		Spec: extensionsv1beta1.IngressSpec{
			Rules: []extensionsv1beta1.IngressRule{
				{
					IngressRuleValue: extensionsv1beta1.IngressRuleValue{
						HTTP: &extensionsv1beta1.HTTPIngressRuleValue{
							Paths: []extensionsv1beta1.HTTPIngressPath{
								{
									Backend: extensionsv1beta1.IngressBackend{
										ServiceName: stableSvc,
										ServicePort: intstr.Parse("use-annotation"),
									},
								},
							},
						},
					},
				},
			},
		},
	}
	return i
}

func TestType(t *testing.T) {
	client := fake.NewSimpleClientset()
	rollout := fakeRollout("stable-service", "canary-service", "stable-ingress", 443)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
	})
	assert.Equal(t, Type, r.Type())
}

func TestIngressNotFound(t *testing.T) {
	ro := fakeRollout("stable-service", "canary-service", "stable-ingress", 443)
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})
	err := r.Reconcile(10)
	assert.True(t, k8serrors.IsNotFound(err))
}

func TestStableServiceNotFoundInIngress(t *testing.T) {
	ro := fakeRollout("different-stable", "canary-service", "ingress", 443)
	i := ingress("ingress", "stable-service", "preview-svc", 443, 50, ro.Name)
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})
	err := r.Reconcile(10)
	assert.Errorf(t, err, "ingress does not use the stable service")
}

func TestNoChanges(t *testing.T) {
	ro := fakeRollout("stable-svc", "canary-svc", "ingress", 443)
	i := ingress("ingress", "stable-svc", "canary-svc", 443, 10, ro.Name)
	client := fake.NewSimpleClientset()
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})
	err := r.Reconcile(10)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 0)
}

func TestErrorOnInvalidManagedBy(t *testing.T) {
	ro := fakeRollout("stable-svc", "canary-svc", "ingress", 443)
	i := ingress("ingress", "stable-svc", "canary-svc", 443, 5, ro.Name)
	i.Annotations[ingressutil.ManagedActionsAnnotation] = "test"
	client := fake.NewSimpleClientset(i)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})
	err := r.Reconcile(10)
	assert.Errorf(t, err, "incorrectly formatted managed actions annotation")
}

func TestSetInitialDesiredWeight(t *testing.T) {
	ro := fakeRollout("stable-svc", "canary-svc", "ingress", 443)
	i := ingress("ingress", "stable-svc", "canary-svc", 443, 5, ro.Name)
	i.Annotations = map[string]string{}
	client := fake.NewSimpleClientset(i)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})
	err := r.Reconcile(10)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 1)
}

func TestUpdateDesiredWeight(t *testing.T) {
	ro := fakeRollout("stable-svc", "canary-svc", "ingress", 443)
	i := ingress("ingress", "stable-svc", "canary-svc", 443, 5, ro.Name)
	client := fake.NewSimpleClientset(i)
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})
	err := r.Reconcile(10)
	assert.Nil(t, err)
	assert.Len(t, client.Actions(), 1)
}

// TestGetForwardActionStringMarshalsZeroCorrectly ensures that the annotation does not omit default value zero when marshalling
// the forward action
func TestGetForwardActionStringMarshalsZeroCorrectly(t *testing.T) {
	r := fakeRollout("stable", "canary", "ingress", 443)
	forwardAction := getForwardActionString(r, 443, 0)
	assert.Contains(t, forwardAction, `"Weight":0`)
}

func TestErrorPatching(t *testing.T) {
	ro := fakeRollout("stable-svc", "canary-svc", "ingress", 443)
	i := ingress("ingress", "stable-svc", "canary-svc", 443, 5, ro.Name)
	client := fake.NewSimpleClientset(i)
	client.ReactionChain = nil
	k8sI := kubeinformers.NewSharedInformerFactory(client, 0)
	k8sI.Extensions().V1beta1().Ingresses().Informer().GetIndexer().Add(i)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		IngressLister:  k8sI.Extensions().V1beta1().Ingresses().Lister(),
	})

	errMessage := "some error occurred"
	r.cfg.Client.(*fake.Clientset).Fake.AddReactor("patch", "ingresses", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, fmt.Errorf(errMessage)
	})

	err := r.Reconcile(10)
	assert.Error(t, err, "some error occurred")
	assert.Len(t, client.Actions(), 1)
}
