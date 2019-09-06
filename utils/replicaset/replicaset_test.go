package replicaset

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	"github.com/argoproj/argo-rollouts/utils/conditions"
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
			Replicas: pointer.Int32Ptr(1),
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
	podTemplateHash := controller.ComputeHash(&rollout.Spec.Template, nil)
	template.Labels = map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: podTemplateHash,
	}
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

func TestNewRSNewReplicas(t *testing.T) {
	ro := generateRollout("test")
	ro.Spec.Strategy.BlueGreenStrategy = &v1alpha1.BlueGreenStrategy{}
	blueGreenNewRSCount, err := NewRSNewReplicas(&ro, nil, nil)
	assert.Nil(t, err)
	assert.Equal(t, blueGreenNewRSCount, *ro.Spec.Replicas)

	ro.Spec.Strategy.BlueGreenStrategy = nil
	_, err = NewRSNewReplicas(&ro, nil, nil)
	assert.Error(t, err, "no rollout strategy provided")
}

func TestNewRSNewReplicasWitPreviewReplicaCount(t *testing.T) {
	previewReplicaCount := int32(1)
	replicaCount := int32(10)

	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "foo",
			Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "foo"},
		},
	}
	rs2 := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "bar",
			Labels: map[string]string{v1alpha1.DefaultRolloutUniqueLabelKey: "bar"},
		},
	}

	tests := []struct {
		name                     string
		activeSelector           string
		overrideCurrentPodHash   string
		scaleUpPreviewCheckpoint bool
		expectReplicaCount       int32
	}{
		{
			name:               "No active rs is set",
			expectReplicaCount: replicaCount,
		},
		{
			name:               "Active rs is the new RS",
			activeSelector:     "foo",
			expectReplicaCount: replicaCount,
		},
		{
			name:                     "Rollout's currentPodHash doesn't match up",
			activeSelector:           "bar",
			overrideCurrentPodHash:   "baz",
			scaleUpPreviewCheckpoint: true,
			expectReplicaCount:       previewReplicaCount,
		},
		{
			name:                     "Rollout is unpaused and ready to scale up",
			activeSelector:           "bar",
			scaleUpPreviewCheckpoint: true,
			expectReplicaCount:       replicaCount,
		},
		{
			name:               "The rollout should use the preview value",
			activeSelector:     "bar",
			expectReplicaCount: previewReplicaCount,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			r := &v1alpha1.Rollout{
				Spec: v1alpha1.RolloutSpec{
					Replicas: &replicaCount,
					Strategy: v1alpha1.RolloutStrategy{
						BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{
							PreviewReplicaCount: &previewReplicaCount,
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					BlueGreen: v1alpha1.BlueGreenStatus{
						ScaleUpPreviewCheckPoint: test.scaleUpPreviewCheckpoint,
						ActiveSelector:           test.activeSelector,
					},
					CurrentPodHash: "foo",
				},
			}
			if test.overrideCurrentPodHash != "" {
				r.Status.CurrentPodHash = test.overrideCurrentPodHash
			}
			count, err := NewRSNewReplicas(r, []*appsv1.ReplicaSet{rs, rs2}, rs)
			assert.Nil(t, err)
			assert.Equal(t, test.expectReplicaCount, count)
		})
	}

}

func TestRevision(t *testing.T) {
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				annotations.RevisionAnnotation: "1",
			},
		},
	}
	revisionValue, err := Revision(rs)
	assert.Nil(t, err)
	assert.Equal(t, int64(1), revisionValue)

	_, err = Revision(nil)
	assert.Error(t, err, fmt.Sprintf("object does not implement the Object interfaces"))

	delete(rs.Annotations, annotations.RevisionAnnotation)
	revisionValue, err = Revision(rs)
	assert.Nil(t, err)
	assert.Equal(t, int64(0), revisionValue)

}

func TestMaxRevision(t *testing.T) {
	allRs := []*appsv1.ReplicaSet{
		{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					annotations.RevisionAnnotation: "1",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					annotations.RevisionAnnotation: "2",
				},
			},
		},
	}
	assert.Equal(t, int64(2), MaxRevision(allRs))
}

func rs(replicas int32, creationTimestamp metav1.Time) *appsv1.ReplicaSet {
	return &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			CreationTimestamp: creationTimestamp,
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &replicas,
		},
	}
}
func TestFindActiveOrLatest(t *testing.T) {
	now := metav1.Now()
	before := metav1.Time{Time: now.Add(-time.Minute)}
	tests := []struct {
		name       string
		newRS      *appsv1.ReplicaSet
		oldRSs     []*appsv1.ReplicaSet
		expectedRS *appsv1.ReplicaSet
	}{
		{
			name: "No RS exist",
		},
		{
			name:  "No active replicas return newRS",
			newRS: rs(0, now),
			oldRSs: []*appsv1.ReplicaSet{
				rs(0, before),
			},
			expectedRS: rs(0, now),
		},
		{
			name: "No active replicas and no newRS return newests old",
			oldRSs: []*appsv1.ReplicaSet{
				rs(0, before),
				rs(0, now),
			},
			expectedRS: rs(0, now),
		},
		{
			name:  "return old active rs",
			newRS: rs(0, now),
			oldRSs: []*appsv1.ReplicaSet{
				rs(1, before),
			},
			expectedRS: rs(1, before),
		},
		{
			name:  "return new active rs",
			newRS: rs(1, now),
			oldRSs: []*appsv1.ReplicaSet{
				rs(0, before),
			},
			expectedRS: rs(1, now),
		},
		{
			name:  "Multiple active rs, return nil",
			newRS: rs(1, now),
			oldRSs: []*appsv1.ReplicaSet{
				rs(1, before),
			},
			expectedRS: nil,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			rs := FindActiveOrLatest(test.newRS, test.oldRSs)
			assert.Equal(t, test.expectedRS, rs)
		})
	}

}

func newString(s string) *string {
	return &s
}

func TestResolveFenceposts(t *testing.T) {
	tests := []struct {
		maxSurge          *string
		maxUnavailable    *string
		desired           int32
		expectSurge       int32
		expectUnavailable int32
		expectError       bool
	}{
		{
			maxSurge:          newString("0%"),
			maxUnavailable:    newString("0%"),
			desired:           0,
			expectSurge:       0,
			expectUnavailable: 1,
			expectError:       false,
		},
		{
			maxSurge:          newString("39%"),
			maxUnavailable:    newString("39%"),
			desired:           10,
			expectSurge:       4,
			expectUnavailable: 3,
			expectError:       false,
		},
		{
			maxSurge:          newString("oops"),
			maxUnavailable:    newString("39%"),
			desired:           10,
			expectSurge:       0,
			expectUnavailable: 0,
			expectError:       true,
		},
		{
			maxSurge:          newString("55%"),
			maxUnavailable:    newString("urg"),
			desired:           10,
			expectSurge:       0,
			expectUnavailable: 0,
			expectError:       true,
		},
		{
			maxSurge:          nil,
			maxUnavailable:    newString("39%"),
			desired:           10,
			expectSurge:       0,
			expectUnavailable: 3,
			expectError:       false,
		},
		{
			maxSurge:          newString("39%"),
			maxUnavailable:    nil,
			desired:           10,
			expectSurge:       4,
			expectUnavailable: 0,
			expectError:       false,
		},
		{
			maxSurge:          nil,
			maxUnavailable:    nil,
			desired:           10,
			expectSurge:       0,
			expectUnavailable: 1,
			expectError:       false,
		},
	}

	for num, test := range tests {
		t.Run(fmt.Sprintf("%d", num), func(t *testing.T) {
			var maxSurge, maxUnavail *intstr.IntOrString
			if test.maxSurge != nil {
				surge := intstr.FromString(*test.maxSurge)
				maxSurge = &surge
			}
			if test.maxUnavailable != nil {
				unavail := intstr.FromString(*test.maxUnavailable)
				maxUnavail = &unavail
			}
			surge, unavail, err := resolveFenceposts(maxSurge, maxUnavail, test.desired)
			if err != nil && !test.expectError {
				t.Errorf("unexpected error %v", err)
			}
			if err == nil && test.expectError {
				t.Error("expected error")
			}
			if surge != test.expectSurge || unavail != test.expectUnavailable {
				t.Errorf("#%v got %v:%v, want %v:%v", num, surge, unavail, test.expectSurge, test.expectUnavailable)
			}
		})
	}
}

func TestMaxSurge(t *testing.T) {
	rollout := func(replicas int32, maxSurge intstr.IntOrString) *v1alpha1.Rollout {
		return &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Replicas: func(i int32) *int32 { return &i }(replicas),
				Strategy: v1alpha1.RolloutStrategy{
					CanaryStrategy: &v1alpha1.CanaryStrategy{
						MaxUnavailable: func(i int) *intstr.IntOrString { x := intstr.FromInt(i); return &x }(int(1)),
						MaxSurge:       &maxSurge,
					},
				},
			},
		}
	}
	tests := []struct {
		name     string
		rollout  *v1alpha1.Rollout
		expected int32
	}{
		{
			name:     "maxSurge with int",
			rollout:  rollout(10, intstr.FromInt(5)),
			expected: int32(5),
		},
		{
			name: "maxSurge with BlueGreen deployment strategy",
			rollout: &v1alpha1.Rollout{
				Spec: v1alpha1.RolloutSpec{
					Strategy: v1alpha1.RolloutStrategy{
						BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{},
					},
				},
			},
			expected: int32(0),
		},
		{
			name:     "maxSurge with percents",
			rollout:  rollout(10, intstr.FromString("50%")),
			expected: int32(5),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, MaxSurge(test.rollout))
		})
	}
}

func TestMaxUnavailable(t *testing.T) {
	rollout := func(replicas int32, maxUnavailable intstr.IntOrString) *v1alpha1.Rollout {
		return &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Replicas: func(i int32) *int32 { return &i }(replicas),
				Strategy: v1alpha1.RolloutStrategy{
					CanaryStrategy: &v1alpha1.CanaryStrategy{
						MaxSurge:       func(i int) *intstr.IntOrString { x := intstr.FromInt(i); return &x }(int(1)),
						MaxUnavailable: &maxUnavailable,
					},
				},
			},
		}
	}
	tests := []struct {
		name     string
		rollout  *v1alpha1.Rollout
		expected int32
	}{
		{
			name:     "maxUnavailable less than replicas",
			rollout:  rollout(10, intstr.FromInt(5)),
			expected: int32(5),
		},
		{
			name:     "maxUnavailable equal replicas",
			rollout:  rollout(10, intstr.FromInt(10)),
			expected: int32(10),
		},
		{
			name:     "maxUnavailable greater than replicas",
			rollout:  rollout(5, intstr.FromInt(10)),
			expected: int32(5),
		},
		{
			name:     "maxUnavailable with replicas is 0",
			rollout:  rollout(0, intstr.FromInt(10)),
			expected: int32(0),
		},
		{
			name: "maxUnavailable with BlueGreen deployment strategy",
			rollout: &v1alpha1.Rollout{
				Spec: v1alpha1.RolloutSpec{
					Strategy: v1alpha1.RolloutStrategy{
						BlueGreenStrategy: &v1alpha1.BlueGreenStrategy{},
					},
				},
			},
			expected: int32(0),
		},
		{
			name:     "maxUnavailable less than replicas with percents",
			rollout:  rollout(10, intstr.FromString("50%")),
			expected: int32(5),
		},
		{
			name:     "maxUnavailable equal replicas with percents",
			rollout:  rollout(10, intstr.FromString("100%")),
			expected: int32(10),
		},
		{
			name:     "maxUnavailable greater than replicas with percents",
			rollout:  rollout(5, intstr.FromString("100%")),
			expected: int32(5),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, MaxUnavailable(test.rollout))
		})
	}
}

func TestCheckPodSpecChange(t *testing.T) {
	ro := generateRollout("ngnix")
	rs := generateRS(ro)
	assert.False(t, checkPodSpecChange(&ro, &rs))
	ro.Status.CurrentPodHash = controller.ComputeHash(&ro.Spec.Template, ro.Status.CollisionCount)
	assert.False(t, checkPodSpecChange(&ro, &rs))

	ro.Status.CurrentPodHash = "different-hash"
	assert.True(t, checkPodSpecChange(&ro, &rs))
}

func TestCheckStepHashChange(t *testing.T) {
	ro := generateRollout("ngnix")
	assert.False(t, checkStepHashChange(&ro))
	ro.Status.CurrentStepHash = conditions.ComputeStepHash(&ro)
	assert.False(t, checkStepHashChange(&ro))

	ro.Status.CurrentStepHash = "different-hash"
	assert.True(t, checkStepHashChange(&ro))
}

func TestResetCurrentStepIndex(t *testing.T) {
	ro := generateRollout("ngnix")
	ro.Spec.Strategy.CanaryStrategy = &v1alpha1.CanaryStrategy{
		Steps: []v1alpha1.CanaryStep{
			{
				SetWeight: pointer.Int32Ptr(1),
			},
		},
	}
	newStepIndex := ResetCurrentStepIndex(&ro)
	assert.Equal(t, pointer.Int32Ptr(0), newStepIndex)

	ro.Spec.Strategy.CanaryStrategy.Steps = nil
	newStepIndex = ResetCurrentStepIndex(&ro)
	assert.Nil(t, newStepIndex)

}

func TestReplicaSetsByRevisionNumber(t *testing.T) {

	now := metav1.Now()
	before := metav1.NewTime(metav1.Now().Add(-5 * time.Second))

	newRS := func(revision string, createTimeStamp metav1.Time) *appsv1.ReplicaSet {
		return &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: createTimeStamp,
				Annotations: map[string]string{
					annotations.RevisionAnnotation: revision,
				},
			},
		}
	}

	t.Run("Sort only by revisionNumber", func(t *testing.T) {
		replicaSets := []*appsv1.ReplicaSet{
			newRS("1", now),
			newRS("2", now),
			newRS("0", now),
		}
		expected := []*appsv1.ReplicaSet{
			newRS("0", now),
			newRS("1", now),
			newRS("2", now),
		}
		sort.Sort(ReplicaSetsByRevisionNumber(replicaSets))
		assert.Equal(t, expected, replicaSets)
	})

	t.Run("Invalid Annotation goes first", func(t *testing.T) {
		replicaSets := []*appsv1.ReplicaSet{
			newRS("2", now),
			newRS("", now),
		}
		expected := []*appsv1.ReplicaSet{
			newRS("", now),
			newRS("2", now),
		}
		sort.Sort(ReplicaSetsByRevisionNumber(replicaSets))
		assert.Equal(t, expected, replicaSets)
	})

	t.Run("Invalid Annotation stays first", func(t *testing.T) {
		replicaSets := []*appsv1.ReplicaSet{
			newRS("", now),
			newRS("2", now),
		}
		expected := []*appsv1.ReplicaSet{
			newRS("", now),
			newRS("2", now),
		}
		sort.Sort(ReplicaSetsByRevisionNumber(replicaSets))
		assert.Equal(t, expected, replicaSets)
	})

	t.Run("Use creationTimeStamp if both have invalid annotation", func(t *testing.T) {
		replicaSets := []*appsv1.ReplicaSet{
			newRS("", now),
			newRS("", before),
		}
		expected := []*appsv1.ReplicaSet{
			newRS("", before),
			newRS("", now),
		}
		sort.Sort(ReplicaSetsByRevisionNumber(replicaSets))
		assert.Equal(t, expected, replicaSets)
	})
	t.Run("Use creationTimeStamp if both have same annotation", func(t *testing.T) {
		replicaSets := []*appsv1.ReplicaSet{
			newRS("1", now),
			newRS("1", before),
		}
		expected := []*appsv1.ReplicaSet{
			newRS("1", before),
			newRS("1", now),
		}
		sort.Sort(ReplicaSetsByRevisionNumber(replicaSets))
		assert.Equal(t, expected, replicaSets)
	})
}
