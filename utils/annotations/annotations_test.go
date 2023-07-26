package annotations

import (
	"fmt"
	"math/rand"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/storage/names"

	v1alpha1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func newROControllerRef(r *v1alpha1.Rollout) *metav1.OwnerReference {
	isController := true
	return &metav1.OwnerReference{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "Rollouts",
		Name:       r.GetName(),
		UID:        r.GetUID(),
		Controller: &isController,
	}
}

// generateRS creates a replica set, with the input rollout's template as its template
func generateRS(r *v1alpha1.Rollout) appsv1.ReplicaSet {
	template := r.Spec.Template.DeepCopy()
	return appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			UID:             randomUID(),
			Name:            names.SimpleNameGenerator.GenerateName("replicaset"),
			Labels:          template.Labels,
			OwnerReferences: []metav1.OwnerReference{*newROControllerRef(r)},
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: new(int32),
			Template: *template,
			Selector: &metav1.LabelSelector{MatchLabels: template.Labels},
		},
	}
}

func randomUID() types.UID {
	return types.UID(strconv.FormatInt(rand.Int63(), 10))
}

// generateRollout creates a rollout, with the input image as its template
func generateRollout(image string) v1alpha1.Rollout {
	podLabels := map[string]string{"name": image}
	terminationSec := int64(30)
	return v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:        image,
			Annotations: make(map[string]string),
		},
		Spec: v1alpha1.RolloutSpec{
			Replicas: func(i int32) *int32 { return &i }(1),
			Selector: &metav1.LabelSelector{MatchLabels: podLabels},
			Template: corev1.PodTemplateSpec{
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
					DNSPolicy:                     corev1.DNSClusterFirst,
					TerminationGracePeriodSeconds: &terminationSec,
					RestartPolicy:                 corev1.RestartPolicyAlways,
					SecurityContext:               &corev1.PodSecurityContext{},
				},
			},
		},
	}
}

func TestAnnotationUtils(t *testing.T) {

	//Setup
	tRollout := generateRollout("nginx")
	tRS := generateRS(&tRollout)
	tRollout.Annotations[RevisionAnnotation] = "1"

	// Check if revision annotations can be set
	t.Run("SetRolloutRevision", func(t *testing.T) {
		copyRollout := tRollout.DeepCopy()
		updated := SetRolloutRevision(copyRollout, "2")
		if !updated {
			t.Errorf("SetRolloutRevision() Expected=True Obtained=False")
		}
		if copyRollout.Annotations[RevisionAnnotation] != "2" {
			t.Errorf("Revision Expected=2 Obtained=%s", copyRollout.Annotations[RevisionAnnotation])
		}
	})
	t.Run("SetRolloutRevisionNoAnnotations", func(t *testing.T) {
		copyRollout := tRollout.DeepCopy()
		copyRollout.Annotations = nil
		updated := SetRolloutRevision(copyRollout, "2")
		if !updated {
			t.Errorf("SetRolloutRevision() Expected=True Obtained=False")
		}
		if copyRollout.Annotations[RevisionAnnotation] != "2" {
			t.Errorf("Revision Expected=2 Obtained=%s", copyRollout.Annotations[RevisionAnnotation])
		}
	})

	// Check if WorkloadRefGeneration annotations can be set
	t.Run("SetRolloutWorkloadRefGeneration", func(t *testing.T) {
		copyRollout := tRollout.DeepCopy()
		copyRollout.Annotations = nil
		updated := SetRolloutWorkloadRefGeneration(copyRollout, "1")
		if !updated {
			t.Errorf("SetRolloutWorkloadRefGeneration() Expected=True Obtained=False")
		}
		if copyRollout.Annotations[WorkloadGenerationAnnotation] != "1" {
			t.Errorf("Revision Expected=1 Obtained=%s", copyRollout.Annotations[WorkloadGenerationAnnotation])
		}
	})

	t.Run("SetRolloutWorkloadRefGenerationAlreadySet", func(t *testing.T) {
		copyRollout := tRollout.DeepCopy()
		copyRollout.Annotations = map[string]string{WorkloadGenerationAnnotation: "1"}
		updated := SetRolloutWorkloadRefGeneration(copyRollout, "2")
		if !updated {
			t.Errorf("SetRolloutWorkloadRefGeneration() Expected=True Obtained=False")
		}
		if copyRollout.Annotations[WorkloadGenerationAnnotation] != "2" {
			t.Errorf("WorkloadGeneration Expected=1 Obtained=%s", copyRollout.Annotations[WorkloadGenerationAnnotation])
		}
	})

	t.Run("SetRolloutWorkloadRefGenerationUnchanged", func(t *testing.T) {
		copyRollout := tRollout.DeepCopy()
		copyRollout.Annotations = map[string]string{WorkloadGenerationAnnotation: "2"}
		updated := SetRolloutWorkloadRefGeneration(copyRollout, "2")
		if updated {
			t.Errorf("SetRolloutWorkloadRefGeneration() Expected=False Obtained=True")
		}
		if copyRollout.Annotations[WorkloadGenerationAnnotation] != "2" {
			t.Errorf("WorkloadGeneration Expected=2 Obtained=%s", copyRollout.Annotations[WorkloadGenerationAnnotation])
		}
	})
	t.Run("RemoveRolloutWorkloadRefGeneration", func(t *testing.T) {
		copyRollout := tRollout.DeepCopy()

		copyRollout.Annotations = nil
		RemoveRolloutWorkloadRefGeneration(copyRollout)
		assert.Nil(t, copyRollout.Annotations)

		copyRollout.Annotations = map[string]string{WorkloadGenerationAnnotation: "2"}
		RemoveRolloutWorkloadRefGeneration(copyRollout)
		_, ok := copyRollout.Annotations[WorkloadGenerationAnnotation]
		assert.False(t, ok)
	})

	t.Run("SetRolloutRevisionAlreadySet", func(t *testing.T) {
		copyRollout := tRollout.DeepCopy()
		copyRollout.Labels = map[string]string{RevisionAnnotation: "2"}
		updated := SetRolloutRevision(copyRollout, "2")
		if !updated {
			t.Errorf("SetRolloutRevision() Expected=False Obtained=True")
		}
		if copyRollout.Annotations[RevisionAnnotation] != "2" {
			t.Errorf("Revision Expected=2 Obtained=%s", copyRollout.Annotations[RevisionAnnotation])
		}
	})

	// Check if annotations are copied properly from rollout to RS
	t.Run("SetNewReplicaSetAnnotations", func(t *testing.T) {
		//Try to set the increment revision from 1 through 20
		tRS.Annotations = nil
		for i := 0; i < 20; i++ {

			nextRevision := fmt.Sprintf("%d", i+1)
			SetNewReplicaSetAnnotations(&tRollout, &tRS, nextRevision, true)
			//Now the ReplicaSets Revision Annotation should be i+1

			if tRS.Annotations[RevisionAnnotation] != nextRevision {
				t.Errorf("Revision Expected=%s Obtained=%s", nextRevision, tRS.Annotations[RevisionAnnotation])
			}
		}
	})

	t.Run("SetNewReplicaSetAnnotationsCopyAnnotations", func(t *testing.T) {
		newRS := tRS.DeepCopy()
		newRollout := tRollout.DeepCopy()
		newRollout.Annotations["key"] = "value"
		assert.True(t, SetNewReplicaSetAnnotations(newRollout, newRS, "20", false))
		assert.Equal(t, newRS.Annotations[RevisionAnnotation], "20")
		assert.Equal(t, "value", newRS.Annotations["key"])
	})

	t.Run("SetNewReplicaSetAnnotationsHandleBadOldRevision", func(t *testing.T) {
		badRS := tRS.DeepCopy()
		badRS.Annotations[RevisionAnnotation] = "Not an int"
		assert.False(t, SetNewReplicaSetAnnotations(&tRollout, badRS, "not an int", true))
		assert.Equal(t, tRollout.Annotations[RevisionAnnotation], "1")
	})

	t.Run("SetNewReplicaSetAnnotationsHandleBadNewRevision", func(t *testing.T) {
		assert.False(t, SetNewReplicaSetAnnotations(&tRollout, &tRS, "not an int", true))
		assert.Equal(t, tRS.Annotations[RevisionAnnotation], "20")
	})

	// Check if annotations are set properly
	t.Run("SetReplicasAnnotations", func(t *testing.T) {
		copyRS := tRS.DeepCopy()
		updated := SetReplicasAnnotations(copyRS, 10)
		if !updated {
			t.Errorf("SetReplicasAnnotations() failed")
		}
		value, ok := copyRS.Annotations[DesiredReplicasAnnotation]
		if !ok {
			t.Errorf("SetReplicasAnnotations did not set DesiredReplicasAnnotation")
		}
		if value != "10" {
			t.Errorf("SetReplicasAnnotations did not set DesiredReplicasAnnotation correctly value=%s", value)
		}
	})

	t.Run("SetReplicasAnnotationsNoAnnotations", func(t *testing.T) {
		copyRS := tRS.DeepCopy()
		copyRS.Annotations = nil
		updated := SetReplicasAnnotations(copyRS, 10)
		if !updated {
			t.Errorf("SetReplicasAnnotations() failed")
		}
		value, ok := copyRS.Annotations[DesiredReplicasAnnotation]
		if !ok {
			t.Errorf("SetReplicasAnnotations did not set DesiredReplicasAnnotation")
		}
		if value != "10" {
			t.Errorf("SetReplicasAnnotations did not set DesiredReplicasAnnotation correctly value=%s", value)
		}
	})

	t.Run("SetReplicasAnnotationsNoChanges", func(t *testing.T) {
		copyRS := tRS.DeepCopy()
		copyRS.Annotations[DesiredReplicasAnnotation] = "10"
		updated := SetReplicasAnnotations(copyRS, 10)
		if updated {
			t.Errorf("SetReplicasAnnotations() make no changes")
		}
		value, ok := copyRS.Annotations[DesiredReplicasAnnotation]
		if !ok {
			t.Errorf("SetReplicasAnnotations did not set DesiredReplicasAnnotation")
		}
		if value != "10" {
			t.Errorf("SetReplicasAnnotations did not set DesiredReplicasAnnotation correctly value=%s", value)
		}
	})

	// Check if we can grab annotations from Replica Set
	tRS.Annotations[DesiredReplicasAnnotation] = "1"
	t.Run("GetDesiredReplicasAnnotation", func(t *testing.T) {
		desired, ok := GetDesiredReplicasAnnotation(&tRS)
		if !ok {
			t.Errorf("GetDesiredReplicasAnnotation Expected=true Obtained=false")
		}
		if desired != 1 {
			t.Errorf("GetDesiredReplicasAnnotation Expected=1 Obtained=%d", desired)
		}
	})

	t.Run("GetDesiredReplicasAnnotationNoReplicaSet", func(t *testing.T) {
		replicas, ok := GetDesiredReplicasAnnotation(nil)
		assert.False(t, ok)
		assert.Equal(t, int32(0), replicas)
	})

	tRS.Annotations[DesiredReplicasAnnotation] = "Not a number"
	t.Run("GetDesiredReplicasAnnotationInvalidAnnotations", func(t *testing.T) {
		_, ok := GetDesiredReplicasAnnotation(&tRS)
		if ok {
			t.Errorf("IsSaturated Expected=false Obtained=true")
		}
	})

	// Check if we can grab annotations from rollout
	t.Run("GetDesiredReplicasAnnotationNotSet", func(t *testing.T) {
		generation, ok := GetWorkloadGenerationAnnotation(&tRollout)
		assert.False(t, ok)
		assert.Equal(t, int32(0), generation)
	})

	tRollout.Annotations[WorkloadGenerationAnnotation] = "1"
	t.Run("GetDesiredReplicasAnnotation", func(t *testing.T) {
		generation, ok := GetWorkloadGenerationAnnotation(&tRollout)
		assert.True(t, ok)
		assert.Equal(t, int32(1), generation)
	})

	tRollout.Annotations[WorkloadGenerationAnnotation] = "20000000000"
	t.Run("GetDesiredReplicasAnnotationOutOfRange", func(t *testing.T) {
		_, ok := GetWorkloadGenerationAnnotation(&tRollout)
		assert.Falsef(t, ok, "Should be an error as 20M value does not fit into int32")
	})

	t.Run("GetWorkloadGenerationAnnotationNilInput", func(t *testing.T) {
		generation, ok := GetWorkloadGenerationAnnotation(nil)
		assert.False(t, ok)
		assert.Equal(t, int32(0), generation)
	})

	tRollout.Annotations[WorkloadGenerationAnnotation] = "Not a number"
	t.Run("GetWorkloadGenerationAnnotationInvalidAnnotations", func(t *testing.T) {
		_, ok := GetWorkloadGenerationAnnotation(&tRollout)
		assert.False(t, ok)
	})

	copyRS := tRS.DeepCopy()
	copyRS.Annotations = nil
	t.Run("GetDesiredReplicasAnnotationNoAnnotations", func(t *testing.T) {
		_, ok := GetDesiredReplicasAnnotation(copyRS)
		if ok {
			t.Errorf("IsSaturated Expected=false Obtained=true")
		}
	})

	t.Run("GetDesiredReplicasAnnotationOutOfInt32Value", func(t *testing.T) {
		cRS := tRS.DeepCopy()
		cRS.Annotations[DesiredReplicasAnnotation] = "20000000000"
		_, ok := GetDesiredReplicasAnnotation(cRS)
		assert.Equal(t, false, ok, "Should be an error as 20M value does not fit into int32")
	})

	//Check if annotations reflect rollouts state
	tRS.Annotations[DesiredReplicasAnnotation] = "1"
	tRS.Status.AvailableReplicas = 1
	tRS.Spec.Replicas = new(int32)
	*tRS.Spec.Replicas = 1

	t.Run("IsSaturated", func(t *testing.T) {
		saturated := IsSaturated(&tRollout, &tRS)
		if !saturated {
			t.Errorf("IsSaturated Expected=true Obtained=false")
		}
	})

	t.Run("IsSaturatedFalseNoRS", func(t *testing.T) {
		saturated := IsSaturated(&tRollout, nil)
		if saturated {
			t.Errorf("IsSaturated Expected=false Obtained=true")
		}
	})

	t.Run("IsSaturatedFalseInvalidAnnotations", func(t *testing.T) {
		tRS.Annotations[DesiredReplicasAnnotation] = "Not a number"
		saturated := IsSaturated(&tRollout, &tRS)
		if saturated {
			t.Errorf("IsSaturated Expected=false Obtained=true")
		}
	})

	//Tear Down
}

func TestReplicasAnnotationsNeedUpdate(t *testing.T) {

	desiredReplicas := fmt.Sprintf("%d", int32(10))
	zeroDesiredReplicas := fmt.Sprintf("%d", int32(0))
	tests := []struct {
		name            string
		replicaSet      *appsv1.ReplicaSet
		desiredReplicas int32
		expected        bool
	}{
		{
			name: "test Annotations nil",
			replicaSet: &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Name: "hello", Namespace: "test"},
				Spec: appsv1.ReplicaSetSpec{
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				},
			},
			desiredReplicas: 10,
			expected:        true,
		},
		{
			name: "test desiredReplicas update",
			replicaSet: &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "hello",
					Namespace:   "test",
					Annotations: map[string]string{DesiredReplicasAnnotation: "8"},
				},
				Spec: appsv1.ReplicaSetSpec{
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				},
			},
			expected: true,
		},
		{
			name: "test remove scale-down-delay",
			replicaSet: &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "hello",
					Namespace: "test",
					Annotations: map[string]string{
						DesiredReplicasAnnotation:                                zeroDesiredReplicas,
						v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey: "set-to-something",
					},
				},
				Spec: appsv1.ReplicaSetSpec{
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				},
			},
			desiredReplicas: 0,
			expected:        true,
		},
		{
			name: "test needn't update",
			replicaSet: &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "hello",
					Namespace:   "test",
					Annotations: map[string]string{DesiredReplicasAnnotation: desiredReplicas},
				},
				Spec: appsv1.ReplicaSetSpec{
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				},
			},
			desiredReplicas: 10,
			expected:        false,
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ReplicasAnnotationsNeedUpdate(test.replicaSet, test.desiredReplicas)
			if result != test.expected {
				t.Errorf("case[%d]:%s Expected %v, Got: %v", i, test.name, test.expected, result)
			}
		})
	}
}

func TestGetRevisionAnnotation(t *testing.T) {
	rev, found := GetRevisionAnnotation(&v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{
				RevisionAnnotation: "1",
			},
		},
	})
	assert.True(t, found)
	assert.Equal(t, int32(1), rev)

	rev, found = GetRevisionAnnotation(&v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "foo",
			Namespace:   metav1.NamespaceDefault,
			Annotations: map[string]string{},
		},
	})
	assert.False(t, found)
	assert.Equal(t, int32(0), rev)

	rev, found = GetRevisionAnnotation(nil)
	assert.False(t, found)
	assert.Equal(t, int32(0), rev)

	rev, found = GetRevisionAnnotation(&v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
			Annotations: map[string]string{
				RevisionAnnotation: "abc",
			},
		},
	})
	assert.False(t, found)
	assert.Equal(t, int32(0), rev)
}
