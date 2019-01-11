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

	v1alpha1 "github.com/argoproj/rollout-controller/pkg/apis/rollouts/v1alpha1"
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

	// Check if revision anotations can be set
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

	// Check if anotations are copied properly from rollout to RS
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

	t.Run("SetNewReplicaSetAnnotationsHandleBadOldRevison", func(t *testing.T) {
		badRS := tRS.DeepCopy()
		badRS.Annotations[RevisionAnnotation] = "Not an int"
		assert.False(t, SetNewReplicaSetAnnotations(&tRollout, badRS, "not an int", true))
		assert.Equal(t, tRollout.Annotations[RevisionAnnotation], "1")
	})

	t.Run("SetNewReplicaSetAnnotationsHandleBadNewRevison", func(t *testing.T) {
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

	tRS.Annotations[DesiredReplicasAnnotation] = "Not a number"
	t.Run("GetDesiredReplicasAnnotationInvalidAnnotations", func(t *testing.T) {
		_, ok := GetDesiredReplicasAnnotation(&tRS)
		if ok {
			t.Errorf("IsSaturated Expected=false Obtained=true")
		}
	})

	copyRS := tRS.DeepCopy()
	copyRS.Annotations = nil
	t.Run("GetDesiredReplicasAnnotationNoAnnotations", func(t *testing.T) {
		_, ok := GetDesiredReplicasAnnotation(copyRS)
		if ok {
			t.Errorf("IsSaturated Expected=false Obtained=true")
		}
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
	tests := []struct {
		name       string
		replicaSet *appsv1.ReplicaSet
		expected   bool
	}{
		{
			name: "test Annotations nil",
			replicaSet: &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{Name: "hello", Namespace: "test"},
				Spec: appsv1.ReplicaSetSpec{
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"foo": "bar"}},
				},
			},
			expected: true,
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
			expected: false,
		},
	}

	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ReplicasAnnotationsNeedUpdate(test.replicaSet, 10)
			if result != test.expected {
				t.Errorf("case[%d]:%s Expected %v, Got: %v", i, test.name, test.expected, result)
			}
		})
	}
}
