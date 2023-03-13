package experiments

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var experimentKind = v1alpha1.SchemeGroupVersion.WithKind("Experiment")

func (c *Controller) getServicesForExperiment(experiment *v1alpha1.Experiment) (map[string]*corev1.Service, error) {
	svcList, err := c.serviceLister.Services(experiment.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	templateToService := make(map[string]*corev1.Service)
	for _, svc := range svcList {
		err = GetServiceForExperiment(experiment, svc, templateToService)
		if err != nil {
			return nil, err
		}
	}
	return templateToService, nil
}

func templateDefined(experiment *v1alpha1.Experiment, templateName string) bool {
	for _, tmpl := range experiment.Spec.Templates {
		if tmpl.Name == templateName {
			return true
		}
	}
	return false
}

func GetServiceForExperiment(experiment *v1alpha1.Experiment, svc *corev1.Service, templateToService map[string]*corev1.Service) error {
	controllerRef := metav1.GetControllerOf(svc)
	if controllerRef == nil || controllerRef.UID != experiment.UID || svc.Annotations == nil || svc.Annotations[v1alpha1.ExperimentNameAnnotationKey] != experiment.Name {
		return nil
	}
	if templateName := svc.Annotations[v1alpha1.ExperimentTemplateNameAnnotationKey]; templateName != "" {
		if _, ok := templateToService[templateName]; ok {
			return fmt.Errorf("multiple Services match single experiment template: %s", templateName)
		}
		if templateDefined(experiment, templateName) {
			templateToService[templateName] = svc
			logCtx := log.WithField(logutil.ExperimentKey, experiment.Name).WithField(logutil.NamespaceKey, experiment.Namespace)
			logCtx.Infof("Claimed Service '%s' for template '%s'", svc.Name, templateName)
		}
	}
	return nil
}

func (ec *experimentContext) CreateService(serviceName string, template v1alpha1.TemplateSpec, selector map[string]string, ports []corev1.ServicePort) (*corev1.Service, error) {
	ctx := context.TODO()
	serviceAnnotations := newServiceAnnotations(ec.ex.Name, template.Name)
	newService := &corev1.Service{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: ec.ex.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(ec.ex, experimentKind),
			},
			Annotations: serviceAnnotations,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports:    ports,
		},
	}

	service, err := ec.kubeclientset.CoreV1().Services(ec.ex.Namespace).Create(ctx, newService, metav1.CreateOptions{})
	if err != nil {
		// If service already exists, get service and check that it is owned by Experiment Template. Otherwise return error.
		if errors.IsAlreadyExists(err) {
			svc, err := ec.kubeclientset.CoreV1().Services(ec.ex.Namespace).Get(ctx, serviceName, metav1.GetOptions{})
			if err != nil {
				return nil, fmt.Errorf("did not get existing service with name %s: %v", serviceName, err)
			}
			controllerRef := metav1.GetControllerOf(svc)
			if controllerRef == nil || controllerRef.UID != ec.ex.UID || svc.Annotations == nil || svc.Annotations[v1alpha1.ExperimentNameAnnotationKey] != ec.ex.Name || svc.Annotations[v1alpha1.ExperimentTemplateNameAnnotationKey] != template.Name {
				return nil, fmt.Errorf("service %s already exists and is not owned by experiment template %s", serviceName, template.Name)
			}
			return svc, nil
		} else {
			return nil, fmt.Errorf("cannot create service: %v %v", err, newService)
		}
	}
	return service, nil
}

func (ec *experimentContext) deleteService(service corev1.Service) error {
	ctx := context.TODO()
	ec.log.Infof("Trying to cleanup service '%s'", service.Name)
	err := ec.kubeclientset.CoreV1().Services(ec.ex.Namespace).Delete(ctx, service.Name, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func newServiceAnnotations(experimentName, templateName string) map[string]string {
	return map[string]string{
		v1alpha1.ExperimentNameAnnotationKey:         experimentName,
		v1alpha1.ExperimentTemplateNameAnnotationKey: templateName,
	}
}
