package hash

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/controller"
)

// ComputePodTemplateHash returns a hash value calculated from pod template.
// The hash will be safe encoded to avoid bad words.
//
// This delegates to the upstream Deployment controller's ComputeHash, which
// hashes the template via spew (reflect-based dump) rather than json.Marshal.
// That makes the hash immune to JSON serialization changes such as the
// apimachinery omitzero tag on CreationTimestamp, and matches how Kubernetes
// Deployment/ReplicaSet controllers label pod-template-hash.
func ComputePodTemplateHash(template *corev1.PodTemplateSpec, collisionCount *int32) string {
	return controller.ComputeHash(template, collisionCount)
}
