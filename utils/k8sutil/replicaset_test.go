package k8sutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
)

func TestComputeHashMatchesLegacyImplementation(t *testing.T) {
	template := corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "nginx"}},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "nginx", Image: "nginx:1.19"}},
		},
	}
	collisionCount := ptr.To[int32](2)

	hash := ComputeHash(&template, collisionCount)
	assert.NotEmpty(t, hash)
	assert.Equal(t, ComputeHash(&template, collisionCount), hash)
}

func TestDefaultPodTemplateServiceAccount(t *testing.T) {
	var desired corev1.PodTemplateSpec
	err := yaml.Unmarshal([]byte(`
metadata:
  labels:
    app: serviceaccount-ro
spec:
  containers:
  - image: nginx:1.19-alpine
    name: app
  serviceAccountName: default
`), &desired)
	assert.NoError(t, err)

	DefaultPodTemplate(&desired)
	assert.Equal(t, "default", desired.Spec.ServiceAccountName)
}

func TestFilterActiveReplicaSets(t *testing.T) {
	active := FilterActiveReplicaSets([]*appsv1.ReplicaSet{
		{Spec: appsv1.ReplicaSetSpec{Replicas: ptr.To[int32](1)}},
		{Spec: appsv1.ReplicaSetSpec{Replicas: ptr.To[int32](0)}},
		nil,
	})
	assert.Len(t, active, 1)
}
