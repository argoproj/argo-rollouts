package rollout

import (
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func TestIsProgressiveMigrationComplete(t *testing.T) {
	tests := []struct {
		name     string
		rollout  *v1alpha1.Rollout
		newRS    *appsv1.ReplicaSet
		expected bool
	}{
		{
			name: "migration complete - healthy phase",
			rollout: &v1alpha1.Rollout{
				Status: v1alpha1.RolloutStatus{
					Phase: v1alpha1.RolloutPhaseHealthy,
				},
				Spec: v1alpha1.RolloutSpec{
					WorkloadRef: &v1alpha1.ObjectRef{
						ScaleDown: v1alpha1.ScaleDownProgressively,
					},
				},
			},
			expected: true,
		},
		{
			name: "migration incomplete - progressing phase",
			rollout: &v1alpha1.Rollout{
				Status: v1alpha1.RolloutStatus{
					Phase: v1alpha1.RolloutPhaseProgressing,
				},
				Spec: v1alpha1.RolloutSpec{
					WorkloadRef: &v1alpha1.ObjectRef{
						ScaleDown: v1alpha1.ScaleDownProgressively,
					},
				},
			},
			expected: false,
		},
		{
			name: "no workloadRef",
			rollout: &v1alpha1.Rollout{
				Status: v1alpha1.RolloutStatus{
					Phase: v1alpha1.RolloutPhaseHealthy,
				},
			},
			expected: false,
		},
		{
			name: "workloadRef with different scaleDown strategy",
			rollout: &v1alpha1.Rollout{
				Status: v1alpha1.RolloutStatus{
					Phase: v1alpha1.RolloutPhaseHealthy,
				},
				Spec: v1alpha1.RolloutSpec{
					WorkloadRef: &v1alpha1.ObjectRef{
						ScaleDown: v1alpha1.ScaleDownOnSuccess,
					},
				},
			},
			expected: false,
		},
		{
			name: "migration incomplete - revision 1 with full ready replicas but still progressing",
			rollout: &v1alpha1.Rollout{
				Status: v1alpha1.RolloutStatus{
					Phase:         v1alpha1.RolloutPhaseProgressing,
					ReadyReplicas: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Replicas: pointer.Int32(10),
					WorkloadRef: &v1alpha1.ObjectRef{
						ScaleDown: v1alpha1.ScaleDownProgressively,
					},
				},
			},
			newRS: &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"deployment.kubernetes.io/revision": "1",
					},
				},
			},
			expected: false,
		},
		{
			name: "migration incomplete - revision 1 with partial ready replicas",
			rollout: &v1alpha1.Rollout{
				Status: v1alpha1.RolloutStatus{
					Phase:         v1alpha1.RolloutPhaseProgressing,
					ReadyReplicas: 5,
				},
				Spec: v1alpha1.RolloutSpec{
					Replicas: pointer.Int32(10),
					WorkloadRef: &v1alpha1.ObjectRef{
						ScaleDown: v1alpha1.ScaleDownProgressively,
					},
				},
			},
			newRS: &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"deployment.kubernetes.io/revision": "1",
					},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &rolloutContext{
				rollout: tt.rollout,
				newRS:   tt.newRS,
			}
			result := c.isProgressiveMigrationComplete()
			assert.Equal(t, tt.expected, result)
		})
	}
}
