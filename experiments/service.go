package experiments

import (
	"context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

func (ec *experimentContext) createService(serviceName string, template v1alpha1.TemplateSpec) (*corev1.Service, error) {
	ctx := context.TODO()
	newService := &corev1.Service{
		TypeMeta:   metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceName,
			Namespace: ec.ex.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: template.Selector.MatchLabels,
			Ports: []corev1.ServicePort{{
				Protocol:   "TCP",
				Port:       int32(80),
				TargetPort: intstr.FromInt(8080),
			}},
		},
	}
	service, err := ec.kubeclientset.CoreV1().Services(ec.ex.Namespace).Create(ctx, newService, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return service, nil
}

func (ec *experimentContext) getService(serviceName string) (*corev1.Service, error) {
	service, err := ec.serviceLister.Services(ec.ex.Namespace).Get(serviceName)
	if err != nil {
		return nil, err
	}
	return service, nil
}