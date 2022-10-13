package ingress

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8stesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
)

const actionTemplate = `{
	"Type":"forward",
	"ForwardConfig":{
		"TargetGroups":[
			{
				"ServiceName":"%s",
				"ServicePort":"%d",
				"Weight": 85
			},{
				"ServiceName":"%s",
				"ServicePort":"%d",
				"Weight": 15
			}
		]
	}
}`

const actionTemplateWithStickyConfig = `{
	"Type":"forward",
	"ForwardConfig":{
		"TargetGroups":[
			{
				"ServiceName":"%s",
				"ServicePort":"%d",
				"Weight": 85
			},{
				"ServiceName":"%s",
				"ServicePort":"%d",
				"Weight": 15
			}
		],
		"TargetGroupStickinessConfig":{
		  "DurationSeconds" : 300,
		  "Enabled" : true
		}
	}
}`

func albActionAnnotation(stable string) string {
	return fmt.Sprintf("%s%s%s", ingressutil.ALBIngressAnnotation, ingressutil.ALBActionPrefix, stable)
}

func newALBIngress(name string, port int, serviceName string, rollout string, includeStickyConfig bool) *extensionsv1beta1.Ingress {
	canaryService := fmt.Sprintf("%s-canary", serviceName)
	albActionKey := albActionAnnotation(serviceName)
	albConditionKey := fmt.Sprintf("%s%s%s", ingressutil.ALBIngressAnnotation, ingressutil.ALBConditionPrefix, serviceName)
	managedBy := ingressutil.ManagedALBAnnotations{
		rollout: ingressutil.ManagedALBAnnotation{albActionKey, albConditionKey},
	}
	action := fmt.Sprintf(actionTemplate, serviceName, port, canaryService, port)
	if includeStickyConfig {
		action = fmt.Sprintf(actionTemplateWithStickyConfig, serviceName, port, canaryService, port)
	}
	return &extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{
				"kubernetes.io/ingress.class":  "alb",
				albActionKey:                   action,
				ingressutil.ManagedAnnotations: managedBy.String(),
			},
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
										ServicePort: intstr.FromString("use-annotations"),
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

func rollout(name, service, ingress string) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: service,
					CanaryService: fmt.Sprintf("%s-canary", service),
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						ALB: &v1alpha1.ALBTrafficRouting{
							Ingress: ingress,
						},
					},
				},
			},
		},
	}
}

func TestInvalidManagedALBActions(t *testing.T) {
	rollout := rollout("rollout", "stable-service", "test-ingress")
	ing := newALBIngress("test-ingress", 80, "stable-service", rollout.Name, false)
	ing.Annotations[ingressutil.ManagedAnnotations] = "invalid-managed-by"

	ctrl, kubeclient, enqueuedObjects := newFakeIngressController(t, ing, rollout)

	err := ctrl.syncIngress(context.Background(), "default/test-ingress")
	assert.Nil(t, err)
	assert.Len(t, kubeclient.Actions(), 0)
	assert.Len(t, enqueuedObjects, 0)
}

func TestInvalidPreviousALBActionAnnotationValue(t *testing.T) {
	ing := newALBIngress("test-ingress", 80, "stable-service", "not-existing-rollout", false)
	ing.Annotations[albActionAnnotation("stable-service")] = "{"

	ctrl, kubeclient, enqueuedObjects := newFakeIngressController(t, ing, nil)

	err := ctrl.syncIngress(context.Background(), "default/test-ingress")
	assert.Nil(t, err)
	assert.Len(t, kubeclient.Actions(), 0)
	assert.Len(t, enqueuedObjects, 0)
}

func TestInvalidPreviousALBActionAnnotationKey(t *testing.T) {
	ing := newALBIngress("test-ingress", 80, "stable-service", "also-not-existing-rollout", false)
	ing.Annotations[ingressutil.ManagedAnnotations] = "invalid-action-key"
	ctrl, kubeclient, enqueuedObjects := newFakeIngressController(t, ing, nil)

	err := ctrl.syncIngress(context.Background(), "default/test-ingress")
	assert.Nil(t, err)
	assert.Len(t, kubeclient.Actions(), 0)
	assert.Len(t, enqueuedObjects, 0)
}

func TestResetActionFailureFindNoPort(t *testing.T) {
	ing := newALBIngress("test-ingress", 80, "stable-service", "still-not-existing-rollout", false)
	ing.Annotations[albActionAnnotation("stable-service")] = "{}"

	ctrl, kubeclient, enqueuedObjects := newFakeIngressController(t, ing, nil)

	err := ctrl.syncIngress(context.Background(), "default/test-ingress")
	assert.Nil(t, err)
	assert.Len(t, kubeclient.Actions(), 0)
	assert.Len(t, enqueuedObjects, 0)
}

func TestALBIngressNoModifications(t *testing.T) {
	rollout := rollout("rollout", "stable-service", "test-ingress")
	ing := newALBIngress("test-ingress", 80, "stable-service", rollout.Name, false)

	ctrl, kubeclient, enqueuedObjects := newFakeIngressController(t, ing, rollout)

	err := ctrl.syncIngress(context.Background(), "default/test-ingress")
	assert.Nil(t, err)
	assert.Len(t, kubeclient.Actions(), 0)
	assert.Len(t, enqueuedObjects, 1)
}

func TestALBIngressResetAction(t *testing.T) {
	ing := newALBIngress("test-ingress", 80, "stable-service", "non-existing-rollout", false)

	ctrl, kubeclient, enqueuedObjects := newFakeIngressController(t, ing, nil)
	err := ctrl.syncIngress(context.Background(), "default/test-ingress")
	assert.Nil(t, err)
	assert.Len(t, enqueuedObjects, 0)
	actions := kubeclient.Actions()
	assert.Len(t, actions, 1)
	updateAction, ok := actions[0].(k8stesting.UpdateAction)
	if !ok {
		assert.Fail(t, "Client call was not an update")
		updateAction.GetObject()
	}
	acc, err := meta.Accessor(updateAction.GetObject())
	if err != nil {
		panic(err)
	}
	annotations := acc.GetAnnotations()
	assert.NotContains(t, annotations, ingressutil.ManagedAnnotations)
	expectedAction := `{"Type":"forward","ForwardConfig":{"TargetGroups":[{"ServiceName":"stable-service","ServicePort":"80","Weight":100}]}}`
	assert.Equal(t, expectedAction, annotations[albActionAnnotation("stable-service")])
}

func TestALBIngressResetActionWithStickyConfig(t *testing.T) {
	ing := newALBIngress("test-ingress", 80, "stable-service", "non-existing-rollout", true)

	ctrl, kubeclient, enqueuedObjects := newFakeIngressController(t, ing, nil)
	err := ctrl.syncIngress(context.Background(), "default/test-ingress")
	assert.Nil(t, err)
	assert.Len(t, enqueuedObjects, 0)
	actions := kubeclient.Actions()
	assert.Len(t, actions, 1)
	updateAction, ok := actions[0].(k8stesting.UpdateAction)
	if !ok {
		assert.Fail(t, "Client call was not an update")
		updateAction.GetObject()
	}
	acc, err := meta.Accessor(updateAction.GetObject())
	if err != nil {
		panic(err)
	}
	annotations := acc.GetAnnotations()
	assert.NotContains(t, annotations, ingressutil.ManagedAnnotations)
	expectedAction := `{"Type":"forward","ForwardConfig":{"TargetGroups":[{"ServiceName":"stable-service","ServicePort":"80","Weight":100}],"TargetGroupStickinessConfig":{"Enabled":true,"DurationSeconds":300}}}`
	assert.Equal(t, expectedAction, annotations[albActionAnnotation("stable-service")])
}
