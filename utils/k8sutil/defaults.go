package k8sutil

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// DefaultPodTemplate applies the subset of Kubernetes PodTemplate defaulting needed by
// library callers, without importing k8s.io/kubernetes.
func DefaultPodTemplate(template *corev1.PodTemplateSpec) {
	defaultPodSpec(&template.Spec)
	for i := range template.Spec.InitContainers {
		defaultContainer(&template.Spec.InitContainers[i])
	}
	for i := range template.Spec.Containers {
		defaultContainer(&template.Spec.Containers[i])
	}
	for i := range template.Spec.EphemeralContainers {
		defaultContainer((*corev1.Container)(&template.Spec.EphemeralContainers[i].EphemeralContainerCommon))
	}
}

// defaultPodSpec and defaultContainer are adapted from k8s.io/kubernetes/pkg/apis/core/v1/defaults.go.
func defaultPodSpec(spec *corev1.PodSpec) {
	if spec.DNSPolicy == "" {
		spec.DNSPolicy = corev1.DNSClusterFirst
	}
	if spec.RestartPolicy == "" {
		spec.RestartPolicy = corev1.RestartPolicyAlways
	}
	if spec.SecurityContext == nil {
		spec.SecurityContext = &corev1.PodSecurityContext{}
	}
	if spec.TerminationGracePeriodSeconds == nil {
		period := int64(corev1.DefaultTerminationGracePeriodSeconds)
		spec.TerminationGracePeriodSeconds = &period
	}
	if spec.SchedulerName == "" {
		spec.SchedulerName = corev1.DefaultSchedulerName
	}
}

func defaultContainer(container *corev1.Container) {
	if container.ImagePullPolicy == "" {
		if strings.HasSuffix(container.Image, ":latest") || !strings.Contains(container.Image, ":") {
			container.ImagePullPolicy = corev1.PullAlways
		} else {
			container.ImagePullPolicy = corev1.PullIfNotPresent
		}
	}
	if container.TerminationMessagePath == "" {
		container.TerminationMessagePath = corev1.TerminationMessagePathDefault
	}
	if container.TerminationMessagePolicy == "" {
		container.TerminationMessagePolicy = corev1.TerminationMessageReadFile
	}
}
