package rollout

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
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
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
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

	idx := f.expectCreateReplicaSetAction(rs1) // sync 1: create RS
	f.expectUpdateRolloutStatusAction(r1)      // sync 1: update status
	f.expectGetRolloutAction(r1)               // re-seed between syncs
	f.expectPatchRolloutAction(r1)             // sync 2: patch status
	f.expectPatchReplicaSetAction(rs1)         // sync 2: scale up RS
	f.runWithSyncs(getKey(r1, t), 2)
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

	idx := f.expectCreateReplicaSetAction(rs1)                                                // sync 1: create RS
	f.expectUpdateRolloutStatusAction(r1)                                                     // sync 1: update status
	f.expectGetRolloutAction(r1)                                                              // re-seed between syncs
	f.expectPatchRolloutAction(r1)                                                            // sync 2: patch status
	f.expectPatchServiceAction(previewSvc, rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]) // sync 2
	f.expectPatchReplicaSetAction(rs1)                                                        // sync 2: scale replicaset
	f.runWithSyncs(getKey(r1, t), 2)
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

	rs2idx := f.expectCreateReplicaSetAction(rs2) // sync 1: Create revision 2 ReplicaSet
	f.expectUpdateRolloutStatusAction(r2)         // sync 1: Update Rollout conditions
	f.expectGetRolloutAction(r2)                  // re-seed between syncs
	rs1idx := f.expectPatchReplicaSetAction(rs1)  // sync 2: patch stable replicaset with stable metadata
	pod1Idx := f.expectUpdatePodAction(&pod1)     // sync 2: Update pod1 with ephemeral data
	pod2Idx := f.expectUpdatePodAction(pod2)      // sync 2: Update pod2 with ephemeral data
	f.expectPatchReplicaSetAction(rs1)            // sync 2: scale revision 1 ReplicaSet down
	f.expectPatchRolloutAction(r2)                // sync 2: Patch Rollout status

	f.runWithSyncs(getKey(r2, t), 2)
	// revision 2 replicaset should been updated to use canary metadata
	createdRS2 := f.getCreatedReplicaSet(rs2idx)
	expectedCanaryLabels := map[string]string{
		"foo":                        "bar",
		"role":                       "canary",
		"rollouts-pod-template-hash": r2.Status.CurrentPodHash,
	}
	assert.Equal(t, expectedCanaryLabels, createdRS2.Spec.Template.Labels)

	// revision 1 replicaset should been patched to use stable metadata
	updatedRS1 := f.getReplicaSetAfterPatch(rs1idx, rs1)
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

	rs2idx := f.expectCreateReplicaSetAction(rs2)      // sync 1: Create revision 2 ReplicaSet
	f.expectUpdateRolloutStatusAction(r2)              // sync 1: Update Rollout conditions
	f.expectGetRolloutAction(r2)                       // re-seed between syncs
	f.expectPatchServiceAction(previewSvc, rs2PodHash) // sync 2: Update preview service to point at revision 2 replicaset
	f.expectPatchReplicaSetAction(rs2)                 // sync 2: scale revision 2 ReplicaSet up
	rs1idx := f.expectPatchReplicaSetAction(rs1)       // sync 2: patch stable replicaset with stable metadata
	pod1Idx := f.expectUpdatePodAction(&pod1)          // sync 2: Update pod1 with ephemeral data
	pod2Idx := f.expectUpdatePodAction(pod2)           // sync 2: Update pod2 with ephemeral data
	f.expectPatchRolloutAction(r2)                     // sync 2: Patch Rollout status

	f.runWithSyncs(getKey(r2, t), 2)
	// revision 2 replicaset should been updated to use canary metadata
	createdRS2 := f.getCreatedReplicaSet(rs2idx)
	expectedCanaryLabels := map[string]string{
		"foo":                        "bar",
		"role":                       "preview",
		"rollouts-pod-template-hash": r2.Status.CurrentPodHash,
	}
	assert.Equal(t, expectedCanaryLabels, createdRS2.Spec.Template.Labels)

	// revision 1 replicaset should been patched to use stable metadata
	updatedRS1 := f.getReplicaSetAfterPatch(rs1idx, rs1)
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

func TestReconcileEphemeralMetadataSkipsWhenNotConfigured(t *testing.T) {
	selector := &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}}
	zero := int32(0)
	otherRS := &v1.ReplicaSet{
		Spec: v1.ReplicaSetSpec{
			Selector: selector,
			Replicas: &zero,
		},
	}
	kubeclient := k8sfake.NewSimpleClientset()
	podListCalls := 0
	kubeclient.Fake.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		podListCalls++
		return true, nil, fmt.Errorf("unexpected pod list")
	})

	mockContext := &rolloutContext{
		reconcilerBase: reconcilerBase{
			kubeclientset: kubeclient,
		},
		rollout: &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{
					Canary: &v1alpha1.CanaryStrategy{},
				},
			},
			Status: v1alpha1.RolloutStatus{
				StableRS: "stable-hash",
			},
		},
		newRS: &v1.ReplicaSet{
			Spec: v1.ReplicaSetSpec{
				Selector: selector,
				Replicas: ptr.To[int32](3),
			},
		},
		otherRSs: []*v1.ReplicaSet{otherRS},
	}

	err := mockContext.reconcileEphemeralMetadata()
	assert.NoError(t, err)
	assert.Equal(t, 0, podListCalls)
}

func TestSyncEphemeralMetadataSkipsPodListOnZeroReplicaRS(t *testing.T) {
	testPodHash := "abc123"
	zero := int32(0)
	rs := &v1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "old-rs",
			Namespace: "default",
		},
		Spec: v1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"rollouts-pod-template-hash": testPodHash,
				},
			},
			Replicas: &zero,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"role":                       "canary",
						"rollouts-pod-template-hash": testPodHash,
					},
				},
			},
		},
	}

	kubeclient := k8sfake.NewSimpleClientset()
	podListCalls := 0
	kubeclient.Fake.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
		podListCalls++
		return true, nil, fmt.Errorf("unexpected pod list")
	})

	ctx := &rolloutContext{
		reconcilerBase: reconcilerBase{
			kubeclientset: kubeclient,
		},
		log: logrus.WithField("test", "zero-replica"),
	}

	err := ctx.syncEphemeralMetadata(context.Background(), rs, nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, podListCalls)
}

// ephemeralMetadataPatchBody returns the merge patch that syncEphemeralMetadata is expected to send. A nil
// rsAnnotation removes the ephemeral metadata annotation of the ReplicaSet.
func ephemeralMetadataPatchBody(t *testing.T, uid types.UID, rsAnnotation *v1alpha1.PodTemplateMetadata, templateLabels, templateAnnotations map[string]any) string {
	t.Helper()
	var annotation any
	if rsAnnotation != nil {
		recorded, err := json.Marshal(rsAnnotation)
		require.NoError(t, err)
		annotation = string(recorded)
	}
	templateMetadata := map[string]any{}
	if templateLabels != nil {
		templateMetadata["labels"] = templateLabels
	}
	if templateAnnotations != nil {
		templateMetadata["annotations"] = templateAnnotations
	}
	body, err := json.Marshal(map[string]any{
		"metadata": map[string]any{
			"uid":         string(uid),
			"annotations": map[string]any{replicasetutil.EphemeralMetadataAnnotation: annotation},
		},
		"spec": map[string]any{"template": map[string]any{"metadata": templateMetadata}},
	})
	require.NoError(t, err)
	return string(body)
}

// The ephemeral metadata of a ReplicaSet is written as a merge patch of the keys that change. It is applied although
// the ReplicaSet in the API has a newer resourceVersion than the copy the change was computed for, which would
// reject an Update.
func TestSyncEphemeralMetadataPatchesReplicaSet(t *testing.T) {
	canary := &v1alpha1.PodTemplateMetadata{
		Labels:      map[string]string{"role": "canary", "tier": "canary"},
		Annotations: map[string]string{"note": "canary"},
	}
	stable := &v1alpha1.PodTemplateMetadata{
		Labels:      map[string]string{"role": "stable"},
		Annotations: map[string]string{"owner": "stable"},
	}
	tests := []struct {
		name string
		// existing is the metadata the ReplicaSet has when the controller reads it, desired what it is synced to.
		existing *v1alpha1.PodTemplateMetadata
		desired  *v1alpha1.PodTemplateMetadata
		// The parts of the expected patch. A nil value removes the key.
		patchTemplateLabels      map[string]any
		patchTemplateAnnotations map[string]any
		// The metadata the pod template has afterwards, next to what it had before any metadata was injected.
		wantTemplateLabels      map[string]string
		wantTemplateAnnotations map[string]string
	}{
		{
			name:                     "added",
			desired:                  canary,
			patchTemplateLabels:      map[string]any{"role": "canary", "tier": "canary"},
			patchTemplateAnnotations: map[string]any{"note": "canary"},
			wantTemplateLabels:       map[string]string{"role": "canary", "tier": "canary"},
			wantTemplateAnnotations:  map[string]string{"note": "canary"},
		},
		{
			name:                     "replaced",
			existing:                 canary,
			desired:                  stable,
			patchTemplateLabels:      map[string]any{"role": "stable", "tier": nil},
			patchTemplateAnnotations: map[string]any{"note": nil, "owner": "stable"},
			wantTemplateLabels:       map[string]string{"role": "stable"},
			wantTemplateAnnotations:  map[string]string{"owner": "stable"},
		},
		{
			name:                     "removed",
			existing:                 canary,
			patchTemplateLabels:      map[string]any{"role": nil, "tier": nil},
			patchTemplateAnnotations: map[string]any{"note": nil},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// The labels of the pod template before any metadata is injected.
			templateLabels := map[string]string{}
			f := newReplicaSetPatchFixture(t, func(rs *v1.ReplicaSet) {
				for key, value := range rs.Spec.Template.Labels {
					templateLabels[key] = value
				}
				if test.existing != nil {
					synced, modified := replicasetutil.SyncReplicaSetEphemeralPodMetadata(rs, test.existing)
					require.True(t, modified)
					synced.DeepCopyInto(rs)
				}
			}, func(rs *v1.ReplicaSet) {
				// What the copy the controller read does not have: a label another writer added to the pod
				// template, and a scale that the controller itself sent earlier in the same reconcile.
				rs.Spec.Template.Labels["added-by"] = "someone-else"
				rs.Spec.Replicas = ptr.To[int32](4)
			})
			// The fixture is set up so that an Update of the cached copy is rejected.
			_, err := f.roCtx.updateReplicaSet(context.Background(), f.cached.DeepCopy())
			require.True(t, errors.IsConflict(err), "got: %v", err)
			f.kubeclient.ClearActions()

			err = f.roCtx.syncEphemeralMetadata(context.Background(), f.cached, test.desired)

			require.NoError(t, err)
			assert.Equal(t, []string{"patch"}, f.replicaSetVerbs())
			patch := f.replicaSetPatch(t)
			assert.Equal(t, types.MergePatchType, patch.GetPatchType())
			assert.Empty(t, patch.GetSubresource())
			assert.JSONEq(t, ephemeralMetadataPatchBody(t, f.cached.UID, test.desired, test.patchTemplateLabels, test.patchTemplateAnnotations), string(patch.GetPatch()))
			assert.NotContains(t, string(patch.GetPatch()), "resourceVersion")

			live := f.liveReplicaSet(t)
			wantLabels := map[string]string{"added-by": "someone-else"}
			for key, value := range templateLabels {
				wantLabels[key] = value
			}
			for key, value := range test.wantTemplateLabels {
				wantLabels[key] = value
			}
			assert.Equal(t, wantLabels, live.Spec.Template.Labels)
			if len(test.wantTemplateAnnotations) == 0 {
				assert.Empty(t, live.Spec.Template.Annotations)
			} else {
				assert.Equal(t, test.wantTemplateAnnotations, live.Spec.Template.Annotations)
			}
			assert.Equal(t, test.desired, replicasetutil.ParseExistingPodMetadata(live))
			// Everything the patch does not name is left as it was in the API.
			assert.Equal(t, int32(4), *live.Spec.Replicas)
			assert.Equal(t, int32(7), live.Status.ReadyReplicas)
			assert.Equal(t, f.cached.Annotations[annotations.RevisionAnnotation], live.Annotations[annotations.RevisionAnnotation])
			assert.Equal(t, f.cached.Annotations[annotations.DesiredReplicasAnnotation], live.Annotations[annotations.DesiredReplicasAnnotation])

			// The write is recorded, so that no reconcile starts from the copy that predates it.
			assert.Equal(t, f.cached, f.roCtx.staleReplicaSet([]*v1.ReplicaSet{f.cached}))
			assert.Nil(t, f.roCtx.staleReplicaSet([]*v1.ReplicaSet{live}))
		})
	}
}

// A ReplicaSet that already has the metadata is not written to.
func TestSyncEphemeralMetadataSendsNothingWhenNothingChanges(t *testing.T) {
	stable := &v1alpha1.PodTemplateMetadata{Labels: map[string]string{"role": "stable"}}
	f := newReplicaSetPatchFixture(t, func(rs *v1.ReplicaSet) {
		synced, _ := replicasetutil.SyncReplicaSetEphemeralPodMetadata(rs, stable)
		synced.DeepCopyInto(rs)
	}, nil)

	require.NoError(t, f.roCtx.syncEphemeralMetadata(context.Background(), f.cached, stable))

	assert.Empty(t, f.replicaSetVerbs())
}

// If the patch is rejected, its error is returned, and the pods are not synced to metadata the ReplicaSet does not
// have.
func TestSyncEphemeralMetadataReturnsPatchError(t *testing.T) {
	f := newReplicaSetPatchFixture(t, nil, nil)
	f.kubeclient.PrependReactor("patch", "replicasets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.NewForbidden(v1.Resource("replicasets"), f.cached.Name, fmt.Errorf("not allowed"))
	})

	err := f.roCtx.syncEphemeralMetadata(context.Background(), f.cached, &v1alpha1.PodTemplateMetadata{Labels: map[string]string{"role": "stable"}})

	require.Error(t, err)
	assert.True(t, errors.IsForbidden(err), "got: %v", err)
	for _, action := range f.kubeclient.Actions() {
		assert.NotEqual(t, "pods", action.GetResource().Resource, "unexpected %s of pods", action.GetVerb())
	}
	assert.NotContains(t, f.liveReplicaSet(t).Spec.Template.Labels, "role")
	assert.Nil(t, f.roCtx.staleReplicaSet([]*v1.ReplicaSet{f.cached}))
}

func TestReplicaSetEphemeralMetadataPatch(t *testing.T) {
	canary := &v1alpha1.PodTemplateMetadata{
		Labels:      map[string]string{"role": "canary", "tier": "canary"},
		Annotations: map[string]string{"note": "canary"},
	}
	stable := &v1alpha1.PodTemplateMetadata{
		Labels:      map[string]string{"role": "stable"},
		Annotations: map[string]string{"owner": "stable"},
	}
	ro := newCanaryRollout("foo", 10, nil, nil, ptr.To[int32](0), intstr.FromInt32(1), intstr.FromInt32(0))
	plain := newReplicaSetWithStatus(ro, 10, 10)
	withCanary, _ := replicasetutil.SyncReplicaSetEphemeralPodMetadata(plain, canary)
	withStable, _ := replicasetutil.SyncReplicaSetEphemeralPodMetadata(plain, stable)

	// Whatever the ephemeral metadata sync changes, the patch changes: applying it to the ReplicaSet the change was
	// computed for gives the ReplicaSet the sync produced.
	t.Run("the patch is the change", func(t *testing.T) {
		transitions := []struct {
			name    string
			from    *v1.ReplicaSet
			desired *v1alpha1.PodTemplateMetadata
		}{
			{name: "added", from: plain, desired: canary},
			{name: "replaced", from: withCanary, desired: stable},
			{name: "replaced back", from: withStable, desired: canary},
			{name: "removed", from: withCanary, desired: nil},
		}
		for _, transition := range transitions {
			t.Run(transition.name, func(t *testing.T) {
				synced, modified := replicasetutil.SyncReplicaSetEphemeralPodMetadata(transition.from, transition.desired)
				require.True(t, modified)

				patch, err := replicaSetEphemeralMetadataPatch(transition.from, synced)

				require.NoError(t, err)
				require.NotNil(t, patch)
				patched := applyReplicaSetMergePatch(t, transition.from, patch)
				assert.True(t, equality.Semantic.DeepEqual(synced, patched), "patch %s gives\n%v\ninstead of\n%v", patch, patched, synced)
			})
		}
	})
	t.Run("nothing to change", func(t *testing.T) {
		patch, err := replicaSetEphemeralMetadataPatch(withCanary, withCanary.DeepCopy())
		require.NoError(t, err)
		assert.Nil(t, patch)
	})
	t.Run("no uid precondition when the uid is unknown", func(t *testing.T) {
		from := withCanary.DeepCopy()
		from.UID = ""
		synced, _ := replicasetutil.SyncReplicaSetEphemeralPodMetadata(from, nil)

		patch, err := replicaSetEphemeralMetadataPatch(from, synced)

		require.NoError(t, err)
		assert.NotContains(t, string(patch), "uid")
	})
	// A change to anything else is refused as a whole, so that it is never applied in part.
	t.Run("a change to another field", func(t *testing.T) {
		others := map[string]func(rs *v1.ReplicaSet){
			"replicas":                    func(rs *v1.ReplicaSet) { rs.Spec.Replicas = ptr.To[int32](3) },
			"minReadySeconds":             func(rs *v1.ReplicaSet) { rs.Spec.MinReadySeconds = 5 },
			"labels of the ReplicaSet":    func(rs *v1.ReplicaSet) { rs.Labels["role"] = "stable" },
			"containers of the template":  func(rs *v1.ReplicaSet) { rs.Spec.Template.Spec.Containers[0].Image = "other" },
			"finalizers of the template":  func(rs *v1.ReplicaSet) { rs.Spec.Template.Finalizers = []string{"other"} },
			"owner references":            func(rs *v1.ReplicaSet) { rs.OwnerReferences = nil },
			"status":                      func(rs *v1.ReplicaSet) { rs.Status.ReadyReplicas = 1 },
			"resourceVersion of the copy": func(rs *v1.ReplicaSet) { rs.ResourceVersion = "5" },
		}
		for name, change := range others {
			t.Run(name, func(t *testing.T) {
				synced, _ := replicasetutil.SyncReplicaSetEphemeralPodMetadata(withCanary, stable)
				change(synced)

				patch, err := replicaSetEphemeralMetadataPatch(withCanary, synced)

				assert.ErrorIs(t, err, errNotEphemeralMetadataChange)
				assert.Nil(t, patch)
			})
		}
	})
}

// A change that the patch cannot express is written with an Update, as all of it was before.
func TestPatchReplicaSetEphemeralMetadataUpdatesWhatItCannotPatch(t *testing.T) {
	newUnsupportedChange := func(rs *v1.ReplicaSet) *v1.ReplicaSet {
		synced, _ := replicasetutil.SyncReplicaSetEphemeralPodMetadata(rs, &v1alpha1.PodTemplateMetadata{Labels: map[string]string{"role": "stable"}})
		synced.Spec.MinReadySeconds = 5
		return synced
	}
	t.Run("applied in full", func(t *testing.T) {
		f := newReplicaSetPatchFixture(t, nil, func(rs *v1.ReplicaSet) {
			// The ReplicaSet in the API is the one the controller read.
			rs.ResourceVersion = "100"
		})

		_, err := f.roCtx.patchReplicaSetEphemeralMetadata(context.Background(), f.cached, newUnsupportedChange(f.cached))

		require.NoError(t, err)
		assert.Equal(t, []string{"update"}, f.replicaSetVerbs())
		live := f.liveReplicaSet(t)
		assert.Equal(t, int32(5), live.Spec.MinReadySeconds)
		assert.Equal(t, "stable", live.Spec.Template.Labels["role"])
	})
	t.Run("rejected if the ReplicaSet was written to since it was read", func(t *testing.T) {
		f := newReplicaSetPatchFixture(t, nil, nil)

		_, err := f.roCtx.patchReplicaSetEphemeralMetadata(context.Background(), f.cached, newUnsupportedChange(f.cached))

		require.Error(t, err)
		assert.True(t, errors.IsConflict(err), "got: %v", err)
		assert.Equal(t, []string{"update"}, f.replicaSetVerbs())
		assert.NotContains(t, f.liveReplicaSet(t).Spec.Template.Labels, "role")
	})
}

// The rollback of #5227: a canary update is half way when the pod template is changed back. The canary ReplicaSet is
// now neither the new nor the stable one, so the canary metadata that was injected into it has to be removed. It
// still has its pods, so the ReplicaSet controller has updated its status since the informer delivered it. That
// must not stop the reconcile before it scales anything.
func TestRollbackRemovesEphemeralMetadataFromReplicaSetWrittenToSinceItWasRead(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	canaryMetadata := &v1alpha1.PodTemplateMetadata{Labels: map[string]string{"role": "canary"}}
	stableMetadata := &v1alpha1.PodTemplateMetadata{Labels: map[string]string{"role": "stable"}}
	steps := []v1alpha1.CanaryStep{{SetWeight: ptr.To[int32](50)}, {Pause: &v1alpha1.RolloutPause{}}}
	r1 := newCanaryRollout("foo", 10, nil, steps, ptr.To[int32](1), intstr.FromInt32(1), intstr.FromInt32(1))
	r1.Spec.Strategy.Canary.CanaryMetadata = canaryMetadata
	r1.Spec.Strategy.Canary.StableMetadata = stableMetadata
	r2 := bumpVersion(r1)
	// The pod template was changed back to that of the first revision.
	r3 := bumpVersion(r2)
	r3.Spec.Template = r1.Spec.Template

	// The stable ReplicaSet, which is the new one again.
	rs1 := newReplicaSetWithStatus(r1, 5, 5)
	rs1, _ = replicasetutil.SyncReplicaSetEphemeralPodMetadata(rs1, stableMetadata)
	rs1.Annotations[annotations.RevisionAnnotation] = r3.Annotations[annotations.RevisionAnnotation]
	rs1PodHash := rs1.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]

	// The abandoned canary ReplicaSet, as the informer has it and as it is in the API.
	cachedRS2 := newReplicaSetWithStatus(r2, 5, 5)
	cachedRS2, _ = replicasetutil.SyncReplicaSetEphemeralPodMetadata(cachedRS2, canaryMetadata)
	cachedRS2.ResourceVersion = "100"
	liveRS2 := cachedRS2.DeepCopy()
	liveRS2.ResourceVersion = "250"
	liveRS2.Status.ReadyReplicas = 3
	liveRS2.Status.AvailableReplicas = 3

	// The reconcile that noticed the change of the pod template has already run.
	r3 = updateCanaryRolloutStatus(r3, rs1PodHash, 10, 5, 10, false)
	r3.Status.CurrentPodHash = rs1PodHash
	r3.Status.CurrentStepIndex = ptr.To[int32](2)
	r3.Status.ObservedGeneration = strconv.Itoa(int(r3.Generation))

	f.kubeobjects = append(f.kubeobjects, rs1, liveRS2)
	f.replicaSetLister = append(f.replicaSetLister, rs1, cachedRS2)
	f.rolloutLister = append(f.rolloutLister, r3)
	f.objects = append(f.objects, r3)

	// The informers are not started: they hold what the fixture put in them.
	c, _, _ := f.newController(noResyncPeriodFunc)
	// As the API server does, reject an Update that does not carry the resourceVersion of the ReplicaSet.
	f.kubeclient.PrependReactor("update", "replicasets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		update := action.(k8stesting.UpdateAction).GetObject().(*v1.ReplicaSet)
		if update.Name == liveRS2.Name && update.ResourceVersion != liveRS2.ResourceVersion {
			return true, nil, errors.NewConflict(v1.Resource("replicasets"), update.Name, fmt.Errorf("the object has been modified; please apply your changes to the latest version and try again"))
		}
		return false, nil, nil
	})
	getReplicaSet := func(name string) *v1.ReplicaSet {
		rs, err := f.kubeclient.AppsV1().ReplicaSets(metav1.NamespaceDefault).Get(context.Background(), name, metav1.GetOptions{})
		require.NoError(t, err)
		return rs
	}

	err := c.syncHandler(context.Background(), getKey(r3, t))

	require.NoError(t, err)
	// The canary metadata is gone from the abandoned ReplicaSet, and what the ReplicaSet controller wrote is kept.
	rs2 := getReplicaSet(liveRS2.Name)
	assert.NotContains(t, rs2.Spec.Template.Labels, "role")
	assert.Nil(t, replicasetutil.ParseExistingPodMetadata(rs2))
	assert.Equal(t, int32(3), rs2.Status.ReadyReplicas)
	// The reconcile went on to the scaling: the stable ReplicaSet is being scaled back up.
	assert.Greater(t, *getReplicaSet(rs1.Name).Spec.Replicas, int32(5))
}
