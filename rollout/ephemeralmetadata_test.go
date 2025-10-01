package rollout

import (
	"context"
	"fmt"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
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
	f.expectListPodAction(r1.Namespace)
	f.expectUpdateReplicaSetAction(rs1)
	_ = f.expectPatchRolloutAction(r1)
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
	f.expectListPodAction(r1.Namespace)
	f.run(getKey(r1, t))
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
	f.expectListPodAction(r1.Namespace)
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

	f.expectUpdateRolloutStatusAction(r2)              // Update Rollout conditions
	rs2idx := f.expectCreateReplicaSetAction(rs2)      // Create revision 2 ReplicaSet
	f.expectPatchServiceAction(previewSvc, rs2PodHash) // Update preview service to point at revision 2 replicaset
	f.expectUpdateReplicaSetAction(rs2)                // scale revision 2 ReplicaSet up
	f.expectListPodAction(r1.Namespace)
	rs1idx := f.expectUpdateReplicaSetAction(rs1) // update stable replicaset with stable metadata
	f.expectListPodAction(r1.Namespace)           // list pods to patch ephemeral data on revision 1 ReplicaSets pods`
	pod1Idx := f.expectUpdatePodAction(&pod1)     // Update pod1 with ephemeral data
	pod2Idx := f.expectUpdatePodAction(pod2)      // Update pod2 with ephemeral data
	f.expectPatchRolloutAction(r2)                // Patch Rollout status

	f.run(getKey(r2, t))
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
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}}
	newRS := &v1.ReplicaSet{
		Spec: v1.ReplicaSetSpec{Selector: selector},
	}
	stableRS := &v1.ReplicaSet{
		Spec: v1.ReplicaSetSpec{Selector: selector},
	}

	mockContext := &rolloutContext{
		reconcilerBase: reconcilerBase{
			kubeclientset: k8sfake.NewSimpleClientset(),
		},
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
		otherRSs: []*v1.ReplicaSet{{
			Spec: v1.ReplicaSetSpec{Selector: selector},
		}, {
			Spec: v1.ReplicaSetSpec{Selector: selector},
		}},
	}

	// Scenario 1: upgrading state when the new ReplicaSet is a canary
	err := mockContext.reconcileEphemeralMetadata()
	assert.NoError(t, err)

	// Scenario 2: Sync stable metadata to the stable ReplicaSet
	mockContext.rollout.Status.StableRS = "" // Set stable ReplicaSet to empty to simulate an upgrading state
	err = mockContext.reconcileEphemeralMetadata()
	assert.NoError(t, err)
}

// TestSyncCanaryEphemeralMetadataReplicaSetAlreadyPatched verifies that even if the ephemeral metadata of a ReplicaSet
// already has been patched, then it still patches the pods of the ReplicaSet that do not have the metadata yet.
func TestSyncCanaryEphemeralMetadataReplicaSetAlreadyPatched(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newCanaryRollout("foo", 3, nil, nil, ptr.To[int32](1), intstr.FromInt32(1), intstr.FromInt32(1))
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

	// Create a ReplicaSet that already has the stable metadata label
	rs1 := newReplicaSetWithStatus(r1, 3, 3)
	rs1.Spec.Template.Labels = map[string]string{
		"role": "stable",
	}

	rsGVK := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}

	// pod1 already has the stable metadata label and should not be updated
	pod1 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo-abc123",
			Namespace: r1.Namespace,
			Labels: map[string]string{
				"foo":                        "bar",
				"role":                       "stable",
				"rollouts-pod-template-hash": r1.Status.CurrentPodHash,
			},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(rs1, rsGVK)},
		},
	}

	// pod2 does not have the stable metadata label and must be updated
	pod2 := pod1.DeepCopy()
	pod2.Name = "foo-abc456"
	pod2.Labels = map[string]string{
		"foo":                        "bar",
		"role":                       "canary",
		"rollouts-pod-template-hash": r1.Status.CurrentPodHash,
	}

	f.rolloutLister = append(f.rolloutLister, r1)
	f.objects = append(f.objects, r1)
	f.kubeobjects = append(f.kubeobjects, rs1, &pod1, pod2)
	f.replicaSetLister = append(f.replicaSetLister, rs1)

	f.expectPatchRolloutAction(r1)
	// ReplicaSet should not be updated since it already has the stable metadata
	f.expectListPodAction(r1.Namespace)
	// pod1 should not be updated since it already has the stable metadata
	pod2Idx := f.expectUpdatePodAction(pod2) // Update pod2 with ephemeral data

	f.run(getKey(r1, t))

	// pod2 should have been updated to have the stable metadata
	expectedPodLabels := map[string]string{
		"foo":                        "bar",
		"role":                       "stable",
		"rollouts-pod-template-hash": r1.Status.CurrentPodHash,
	}
	updatedPod2 := f.getUpdatedPod(pod2Idx)
	assert.Equal(t, expectedPodLabels, updatedPod2.Labels)
}

// TestSyncBlueGreenEphemeralMetadataReplicaSetAlreadyPatched verifies that even if the ephemeral metadata of a ReplicaSet
// already has been patched, then it still patches the pods of the ReplicaSet that do not have the metadata yet.
func TestSyncBlueGreenEphemeralMetadataReplicaSetAlreadyPatched(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	r1 := newBlueGreenRollout("foo", 3, nil, "active", "preview")
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

	// Create a ReplicaSet that already has the active metadata label
	rs1 := newReplicaSetWithStatus(r1, 3, 3)
	rs1.Spec.Template.Labels = map[string]string{
		"role": "active",
	}

	rsGVK := schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "ReplicaSet"}

	// pod1 already has the active metadata label and should not be updated
	pod1 := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo-abc123",
			Namespace: r1.Namespace,
			Labels: map[string]string{
				"foo":                        "bar",
				"role":                       "active",
				"rollouts-pod-template-hash": r1.Status.CurrentPodHash,
			},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(rs1, rsGVK)},
		},
	}

	// pod2 does not have the active metadata label and must be updated
	pod2 := pod1.DeepCopy()
	pod2.Name = "foo-abc456"
	pod2.Labels = map[string]string{
		"foo":                        "bar",
		"role":                       "preview",
		"rollouts-pod-template-hash": r1.Status.CurrentPodHash,
	}

	previewSvc := newService("preview", 80, nil, r1)
	activeSvc := newService("active", 80, nil, r1)

	f.rolloutLister = append(f.rolloutLister, r1)
	f.objects = append(f.objects, r1)
	f.kubeobjects = append(f.kubeobjects, rs1, &pod1, pod2, previewSvc, activeSvc)
	f.replicaSetLister = append(f.replicaSetLister, rs1)
	f.serviceLister = append(f.serviceLister, activeSvc, previewSvc)

	f.expectPatchRolloutAction(r1)
	// ReplicaSet should not be updated since it already has the active metadata
	f.expectPatchServiceAction(previewSvc, rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
	f.expectPatchServiceAction(activeSvc, rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey])
	f.expectListPodAction(r1.Namespace)
	// pod1 should not be updated since it already has the active metadata
	pod2Idx := f.expectUpdatePodAction(pod2) // Update pod2 with ephemeral data

	f.run(getKey(r1, t))

	// pod2 should have been updated to have the active metadata
	expectedPodLabels := map[string]string{
		"foo":                        "bar",
		"role":                       "active",
		"rollouts-pod-template-hash": r1.Status.CurrentPodHash,
	}
	updatedPod2 := f.getUpdatedPod(pod2Idx)
	assert.Equal(t, expectedPodLabels, updatedPod2.Labels)
}

// TestSyncEphemeralMetadata verifies that syncEphemeralMetadata correctly applies metadata to pods
// and handles retry logic when pod updates encounter conflicts.
func TestSyncEphemeralMetadata(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	testRSName := "test-rs"
	testPodName := "test-pod"
	testNamespace := "default"
	testUID := types.UID("test-uid")
	testPodHash := "abc123"
	testRoleLabel := "role"
	testRoleValue := "canary"

	rs := &v1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testRSName,
			Namespace: testNamespace,
			UID:       testUID,
			Labels: map[string]string{
				"rollouts-pod-template-hash": testPodHash,
			},
		},
		Spec: v1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"rollouts-pod-template-hash": testPodHash,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"foo":                        "bar",
						"rollouts-pod-template-hash": testPodHash,
					},
				},
			},
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:            testPodName,
			Namespace:       testNamespace,
			ResourceVersion: "1",
			Labels: map[string]string{
				"foo":                        "bar",
				"rollouts-pod-template-hash": testPodHash,
				// Missing testRoleLabel: testRoleValue - this will trigger the update
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "ReplicaSet",
					Name:       testRSName,
					UID:        testUID,
					Controller: ptr.To[bool](true),
				},
			},
		},
	}

	f.kubeobjects = append(f.kubeobjects, rs, pod)
	f.kubeclient = k8sfake.NewSimpleClientset(f.kubeobjects...)

	// Mock pod update to fail first time, succeed second time (to test retry logic)
	updateAttempts := 0
	f.kubeclient.Fake.PrependReactor("update", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		updateAttempts++
		if updateAttempts == 1 {
			// First attempt fails with conflict
			return true, nil, errors.NewConflict(corev1.Resource("pods"), testPodName, fmt.Errorf("conflict"))
		}
		// Second attempt succeeds - let the fake clientset handle the update normally
		return false, nil, nil
	})

	// Create rollout context with minimal setup
	ctx := &rolloutContext{
		reconcilerBase: reconcilerBase{
			kubeclientset:               f.kubeclient,
			ephemeralMetadataThreads:    DefaultEphemeralMetadataThreads,
			ephemeralMetadataPodRetries: DefaultEphemeralMetadataPodRetries,
		},
		log: logrus.WithField("test", "retry"),
	}

	// Metadata to apply (this will trigger pod update since pod is missing role label)
	metadata := &v1alpha1.PodTemplateMetadata{
		Labels: map[string]string{
			testRoleLabel: testRoleValue,
		},
	}

	// Call the function under test
	err := ctx.syncEphemeralMetadata(context.Background(), rs, metadata)

	assert.NoError(t, err)
	assert.Equal(t, 2, updateAttempts)

	// Verify the pod now has the expected metadata
	updatedPod, err := f.kubeclient.CoreV1().Pods(testNamespace).Get(context.Background(), testPodName, metav1.GetOptions{})
	assert.NoError(t, err)
	expectedLabels := map[string]string{
		"foo":                        "bar",
		"rollouts-pod-template-hash": testPodHash,
		testRoleLabel:                testRoleValue, // This should be added by syncEphemeralMetadata
	}
	assert.Equal(t, expectedLabels, updatedPod.Labels)
}
