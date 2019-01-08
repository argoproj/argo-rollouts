package replicaset

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/kubernetes/pkg/controller"

	"github.com/argoproj/rollout-controller/pkg/apis/rollouts/v1alpha1"
	"github.com/stretchr/testify/assert"
)

// generateRollout creates a rollout, with the input image as its template
func generateRollout(image string) v1alpha1.Rollout {
	podLabels := map[string]string{"name": image}
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
				},
			},
		},
	}
}

// generateRS creates a replica set, with the input rollout's template as its template
func generateRS(rollout v1alpha1.Rollout) appsv1.ReplicaSet {
	template := rollout.Spec.Template.DeepCopy()
	return appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			UID:    uuid.NewUUID(),
			Name:   fmt.Sprintf("%s-%s", rollout.Name, controller.ComputeHash(&rollout.Spec.Template, nil)),
			Labels: template.Labels,
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: new(int32),
			Template: *template,
			Selector: &metav1.LabelSelector{MatchLabels: template.Labels},
		},
	}
}

func TestFindOldReplicaSets(t *testing.T) {
	now := metav1.Now()
	before := metav1.Time{Time: now.Add(-time.Minute)}

	rollout := generateRollout("nginx")
	newRS := generateRS(rollout)
	*(newRS.Spec.Replicas) = 1
	newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey] = "hash"
	newRS.CreationTimestamp = now

	oldRollout := generateRollout("nginx")
	oldRollout.Spec.Template.Spec.Containers[0].Name = "nginx-old-1"
	oldRS := generateRS(oldRollout)
	oldRS.Status.FullyLabeledReplicas = *(oldRS.Spec.Replicas)
	oldRS.CreationTimestamp = before

	tests := []struct {
		Name     string
		rollout  v1alpha1.Rollout
		rsList   []*appsv1.ReplicaSet
		expected []*appsv1.ReplicaSet
	}{
		{
			Name:     "Get old ReplicaSets",
			rollout:  rollout,
			rsList:   []*appsv1.ReplicaSet{&newRS, &oldRS},
			expected: []*appsv1.ReplicaSet{&oldRS},
		},
		{
			Name:     "Get old ReplicaSets with no new ReplicaSet",
			rollout:  rollout,
			rsList:   []*appsv1.ReplicaSet{&oldRS},
			expected: []*appsv1.ReplicaSet{&oldRS},
		},
		{
			Name:     "Get empty old ReplicaSets",
			rollout:  rollout,
			rsList:   []*appsv1.ReplicaSet{&newRS},
			expected: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			allRS := FindOldReplicaSets(&test.rollout, test.rsList)
			sort.Sort(controller.ReplicaSetsByCreationTimestamp(allRS))
			sort.Sort(controller.ReplicaSetsByCreationTimestamp(test.expected))
			if !reflect.DeepEqual(allRS, test.expected) {
				t.Errorf("In test case %q, expected %#v, got %#v", test.Name, test.expected, allRS)
			}
		})
	}
}

func TestGetReplicaCountForReplicaSets(t *testing.T) {
	rs1 := generateRS(generateRollout("foo"))
	*(rs1.Spec.Replicas) = 1
	rs1.Status.Replicas = 2
	rs2 := generateRS(generateRollout("bar"))
	*(rs2.Spec.Replicas) = 2
	rs2.Status.Replicas = 3
	rs2.Status.ReadyReplicas = 1

	rs3 := generateRS(generateRollout("baz"))
	*(rs3.Spec.Replicas) = 3
	rs3.Status.Replicas = 4
	rs3.Status.ReadyReplicas = 2
	rs3.Status.AvailableReplicas = 1

	tests := []struct {
		Name                   string
		sets                   []*appsv1.ReplicaSet
		expectedCount          int32
		expectedActualCount    int32
		expectedReadyCount     int32
		expectedAvailableCount int32
	}{
		{
			"1 Spec, 2 Actual, 0 Ready, 0 Available",
			[]*appsv1.ReplicaSet{&rs1},
			1,
			2,
			0,
			0,
		},
		{
			"3 Spec, 5 Actual, 1 Ready, 0 Available",
			[]*appsv1.ReplicaSet{&rs1, &rs2},
			3,
			5,
			1,
			0,
		},
		{
			"6 Spec, 9 Actual, 3 Ready, 1 Available",
			[]*appsv1.ReplicaSet{&rs1, &rs2, &rs3},
			6,
			9,
			3,
			1,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			assert.Equal(t, test.expectedCount, GetReplicaCountForReplicaSets(test.sets))
			assert.Equal(t, test.expectedActualCount, GetActualReplicaCountForReplicaSets(test.sets))
			assert.Equal(t, test.expectedReadyCount, GetReadyReplicaCountForReplicaSets(test.sets))
			assert.Equal(t, test.expectedAvailableCount, GetAvailableReplicaCountForReplicaSets(test.sets))
		})
	}
}
