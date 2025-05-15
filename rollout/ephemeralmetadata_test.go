package rollout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
)

// TestSyncCanaryEphemeralMetadataInitialRevision verifies when we create a revision 1 ReplicaSet
// (with no previous revisions), that the ReplicaSet will get the stable metadata.
func TestSyncCanaryEphemeralMetadataInitialRevision(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newCanaryRollout("foo", 1, nil, nil, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(1))
	r1.Spec.Strategy.Canary.CanaryMetadata = &v1alpha1.PodTemplateMetadata{
		Labels: map[string]string{
			"role": "canary",
		},
	}
	r1.Spec.Strategy.Canary.StableMetadata = &v1alpha1.PodTemplateMetadata{
		Labels: map[string]string{
			"role": "stable",
		},
	}
	rs1 := newReplicaSetWithStatus(r1, 3, 3)
	f.rolloutLister = append(f.rolloutLister, r1)
	f.objects = append(f.objects, r1)

	f.expectUpdateRolloutStatusAction(r1)
	idx := f.expectCreateReplicaSetAction(rs1)
	f.expectUpdateReplicaSetAction(rs1)
	f.expectPatchReplicaSetAction(rs1) // set final status to success
	f.expectPatchRolloutAction(r1)
	f.run(getKey(r1, t))
	createdRS1 := f.getCreatedReplicaSet(idx)
	expectedLabels := map[string]string{
		"foo":                        "bar",
		"role":                       "stable",
		"rollouts-pod-template-hash": r1.Status.CurrentPodHash,
	}
	assert.Equal(t, expectedLabels, createdRS1.Spec.Template.Labels)
}

// TestSyncBlueGreenEphemeralMetadataInitialRevision verifies when we create a revision 1 ReplicaSet
// (with no previous revisions), that the ReplicaSet will get the active metadata.
func TestSyncBlueGreenEphemeralMetadataInitialRevision(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 1, nil, "active", "preview")
	r1.Spec.Strategy.BlueGreen.PreviewMetadata = &v1alpha1.PodTemplateMetadata{
		Labels: map[string]string{
			"role": "preview",
		},
	}
	r1.Spec.Strategy.BlueGreen.ActiveMetadata = &v1alpha1.PodTemplateMetadata{
		Labels: map[string]string{
			"role": "active",
		},
	}
	rs1 := newReplicaSetWithStatus(r1, 1, 1)
	f.rolloutLister = append(f.rolloutLister, r1)
	f.objects = append(f.objects, r1)
	previewSvc := newService("preview", 80, nil, r1)
	activeSvc := newService("active", 80, nil, r1)
	f.kubeobjects = append(f.kubeobjects, previewSvc, activeSvc)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	f.expectUpdateRolloutStatusAction(r1)
	idx := f.expectCreateReplicaSetAction(rs1)
	f.expectPatchRolloutAction(r1)
	f.expectPatchServiceAction(previewSvc, rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
	f.expectUpdateReplicaSetAction(rs1) // scale replicaset
	patchFinalStatusRs1Index := f.expectPatchReplicaSetAction(rs1)
	f.run(getKey(r1, t))

	f.verifyPatchedReplicaSetFinalStatus(patchFinalStatusRs1Index, FinalStatusSuccess)

	createdRS1 := f.getCreatedReplicaSet(idx)
	expectedLabels := map[string]string{
		"foo":                        "bar",
		"role":                       "active",
		"rollouts-pod-template-hash": r1.Status.CurrentPodHash,
	}
	assert.Equal(t, expectedLabels, createdRS1.Spec.Template.Labels)
}

// TestSyncCanaryEphemeralMetadataSecondRevision verifies when we deploy a canary ReplicaSet, the canary
// contains the canary ephemeral metadata.  Also verifies we patch existing pods of the ReplicaSet
// with the metadata
func TestSyncCanaryEphemeralMetadataSecondRevision(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newCanaryRollout("foo", 1, nil, nil, ptr.To[int32](1), intstr.FromInt(1), intstr.FromInt(1))
	r1.Annotations[annotations.RevisionAnnotation] = "1"
	r1.Spec.Strategy.Canary.CanaryMetadata = &v1alpha1.PodTemplateMetadata{
		Labels: map[string]string{
			"role": "canary",
		},
	}
	r1.Spec.Strategy.Canary.StableMetadata = &v1alpha1.PodTemplateMetadata{
		Labels: map[string]string{
			"role": "stable",
		},
	}
	rs1 := newReplicaSetWithStatus(r1, 3, 3)
	r2 := bumpVersion(r1)
	r2.Status.StableRS = r1.Status.CurrentPodHash
	rs2 := newReplicaSetWithStatus(r2, 3, 3)
	rsGVK := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}
	pod1 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo-abc123",
			Namespace: r1.Namespace,
			Labels: map[string]string{
				"foo":                        "bar",
				"rollouts-pod-template-hash": r1.Status.CurrentPodHash,
			},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(rs1, rsGVK)},
		},
	}
	pod2 := pod1.DeepCopy()
	pod2.Name = "foo-abc456"

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, rs1, &pod1, pod2)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	f.expectUpdateRolloutStatusAction(r2)         // Update Rollout conditions
	rs2idx := f.expectCreateReplicaSetAction(rs2) // Create revision 2 ReplicaSet
	rs1idx := f.expectUpdateReplicaSetAction(rs1) // update stable replicaset with stable metadata
	f.expectListPodAction(r1.Namespace)           // list pods to patch ephemeral data on revision 1 ReplicaSets pods
	pod1Idx := f.expectUpdatePodAction(&pod1)     // Update pod1 with ephemeral data
	pod2Idx := f.expectUpdatePodAction(pod2)      // Update pod2 with ephemeral data
	f.expectUpdateReplicaSetAction(rs1)           // scale revision 1 ReplicaSet down
	f.expectPatchRolloutAction(r2)                // Patch Rollout status

	f.run(getKey(r2, t))
	// revision 2 replicaset should been updated to use canary metadata
	createdRS2 := f.getCreatedReplicaSet(rs2idx)
	expectedCanaryLabels := map[string]string{
		"foo":                        "bar",
		"role":                       "canary",
		"rollouts-pod-template-hash": r2.Status.CurrentPodHash,
	}
	assert.Equal(t, expectedCanaryLabels, createdRS2.Spec.Template.Labels)

	// revision 1 replicaset should been updated to use stable metadata
	updatedRS1 := f.getCreatedReplicaSet(rs1idx)
	expectedStableLabels := map[string]string{
		"foo":                        "bar",
		"role":                       "stable",
		"rollouts-pod-template-hash": r1.Status.CurrentPodHash,
	}
	assert.Equal(t, expectedStableLabels, updatedRS1.Spec.Template.Labels)
	// also it's pods
	updatedPod1 := f.getUpdatedPod(pod1Idx)
	assert.Equal(t, expectedStableLabels, updatedPod1.Labels)
	updatedPod2 := f.getUpdatedPod(pod2Idx)
	assert.Equal(t, expectedStableLabels, updatedPod2.Labels)
}

// TestSyncBlueGreenEphemeralMetadataSecondRevision verifies when we deploy a canary ReplicaSet, the canary
// contains the canary ephemeral metadata.  Also verifies we patch existing pods of the ReplicaSet
// with the metadata
func TestSyncBlueGreenEphemeralMetadataSecondRevision(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 3, nil, "active", "preview")
	r1.Spec.Strategy.BlueGreen.AutoPromotionEnabled = ptr.To[bool](false)
	r1.Annotations[annotations.RevisionAnnotation] = "1"
	r1.Spec.Strategy.BlueGreen.PreviewMetadata = &v1alpha1.PodTemplateMetadata{
		Labels: map[string]string{
			"role": "preview",
		},
	}
	r1.Spec.Strategy.BlueGreen.ActiveMetadata = &v1alpha1.PodTemplateMetadata{
		Labels: map[string]string{
			"role": "active",
		},
	}
	rs1 := newReplicaSetWithStatus(r1, 3, 3)
	r2 := bumpVersion(r1)
	r2.Status.StableRS = r1.Status.CurrentPodHash
	rs2 := newReplicaSetWithStatus(r2, 3, 3)
	rs2PodHash := rs2.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
	rsGVK := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}
	pod1 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo-abc123",
			Namespace: r1.Namespace,
			Labels: map[string]string{
				"foo":                        "bar",
				"rollouts-pod-template-hash": r1.Status.CurrentPodHash,
			},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(rs1, rsGVK)},
		},
	}
	pod2 := pod1.DeepCopy()
	pod2.Name = "foo-abc456"

	previewSvc := newService("preview", 80, nil, r1)
	activeSvc := newService("active", 80, nil, r1)

	f.rolloutLister = append(f.rolloutLister, r2)
	f.objects = append(f.objects, r2)
	f.kubeobjects = append(f.kubeobjects, rs1, &pod1, pod2, previewSvc, activeSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	f.expectUpdateRolloutStatusAction(r2)                          // Update Rollout conditions
	rs2idx := f.expectCreateReplicaSetAction(rs2)                  // Create revision 2 ReplicaSet
	f.expectPatchServiceAction(previewSvc, rs2PodHash)             // Update preview service to point at revision 2 replicaset
	f.expectUpdateReplicaSetAction(rs2)                            // scale revision 2 ReplicaSet up
	rs1idx := f.expectUpdateReplicaSetAction(rs1)                  // update stable replicaset with stable metadata
	f.expectListPodAction(r1.Namespace)                            // list pods to patch ephemeral data on revision 1 ReplicaSets pods`
	pod1Idx := f.expectUpdatePodAction(&pod1)                      // Update pod1 with ephemeral data
	pod2Idx := f.expectUpdatePodAction(pod2)                       // Update pod2 with ephemeral data
	f.expectPatchRolloutAction(r2)                                 // Patch Rollout status
	patchFinalStatusRs2Index := f.expectPatchReplicaSetAction(rs2) // set final status of new RS to success

	f.run(getKey(r2, t))

	f.verifyPatchedReplicaSetFinalStatus(patchFinalStatusRs2Index, FinalStatusSuccess)

	// revision 2 replicaset should been updated to use canary metadata
	createdRS2 := f.getCreatedReplicaSet(rs2idx)
	expectedCanaryLabels := map[string]string{
		"foo":                        "bar",
		"role":                       "preview",
		"rollouts-pod-template-hash": r2.Status.CurrentPodHash,
	}
	assert.Equal(t, expectedCanaryLabels, createdRS2.Spec.Template.Labels)

	// revision 1 replicaset should been updated to use stable metadata
	updatedRS1 := f.getCreatedReplicaSet(rs1idx)
	expectedStableLabels := map[string]string{
		"foo":                        "bar",
		"role":                       "active",
		"rollouts-pod-template-hash": r1.Status.CurrentPodHash,
	}
	assert.Equal(t, expectedStableLabels, updatedRS1.Spec.Template.Labels)
	// also it's pods
	updatedPod1 := f.getUpdatedPod(pod1Idx)
	assert.Equal(t, expectedStableLabels, updatedPod1.Labels)
	updatedPod2 := f.getUpdatedPod(pod2Idx)
	assert.Equal(t, expectedStableLabels, updatedPod2.Labels)
}

func TestReconcileEphemeralMetadata(t *testing.T) {
	newRS := &v1.ReplicaSet{}
	stableRS := &v1.ReplicaSet{}

	mockContext := &rolloutContext{
		rollout: &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{
						CanaryMetadata: &v1alpha1.PodTemplateMetadata{},
						StableMetadata: &v1alpha1.PodTemplateMetadata{},
					},
				},
			},
			Status: v1alpha1.RolloutStatus{
				StableRS: "some-stable-rs-hash",
			},
		},
		newRS:    newRS,
		stableRS: stableRS,
		otherRSs: []*v1.ReplicaSet{new(v1.ReplicaSet), new(v1.ReplicaSet)},
	}

	// Scenario 1: upgrading state when the new ReplicaSet is a canary
	err := mockContext.reconcileEphemeralMetadata()
	assert.NoError(t, err)

	// Scenario 2: Sync stable metadata to the stable ReplicaSet
	mockContext.rollout.Status.StableRS = "" // Set stable ReplicaSet to empty to simulate an upgrading state
	err = mockContext.reconcileEphemeralMetadata()
	assert.NoError(t, err)
}
