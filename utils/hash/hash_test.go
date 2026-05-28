package hash

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestHashUtils(t *testing.T) {
	templateRed := generatePodTemplate("red")
	hashRed := ComputePodTemplateHash(&templateRed, nil)
	template := generatePodTemplate("red")

	t.Run("HashForSameTemplates", func(t *testing.T) {
		podHash := ComputePodTemplateHash(&template, nil)
		assert.Equal(t, hashRed, podHash)
	})
	t.Run("HashForDifferentTemplates", func(t *testing.T) {
		podHash := ComputePodTemplateHash(&template, ptr.To[int32](1))
		assert.NotEqual(t, hashRed, podHash)
	})
	// Ensures the pod-template-hash stays stable across the apimachinery bump that
	// added the `omitzero` tag to ObjectMeta.CreationTimestamp (k8s >= v0.31).
	// 7bb4fbc896 is the value produced by controller versions <= 1.8.x. A change
	// here would silently re-trigger a rollout for every Rollout on upgrade.
	t.Run("HashStableAcrossCreationTimestampOmitzero", func(t *testing.T) {
		assert.Equal(t, "7bb4fbc896", hashRed)
	})
}

func generatePodTemplate(image string) corev1.PodTemplateSpec {
	podLabels := map[string]string{"name": image}

	return corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: podLabels,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:                   image,
					Image:                  image,
					ImagePullPolicy:        corev1.PullAlways,
					TerminationMessagePath: corev1.TerminationMessagePathDefault,
				},
			},
			DNSPolicy:       corev1.DNSClusterFirst,
			RestartPolicy:   corev1.RestartPolicyAlways,
			SecurityContext: &corev1.PodSecurityContext{},
		},
	}
}
