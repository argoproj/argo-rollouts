package conditions

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

var (
	condInvalidExperimentSpec = func() v1alpha1.ExperimentCondition {
		return v1alpha1.ExperimentCondition{
			Type:   v1alpha1.InvalidExperimentSpec,
			Status: v1.ConditionFalse,
			Reason: "ForSomeReason",
		}
	}

	condInvalidExperimentSpec2 = func() v1alpha1.ExperimentCondition {
		return v1alpha1.ExperimentCondition{
			Type:   v1alpha1.InvalidExperimentSpec,
			Status: v1.ConditionTrue,
			Reason: "BecauseItIs",
		}
	}

	condAvailableExperiment = func() v1alpha1.ExperimentCondition {
		return v1alpha1.ExperimentCondition{
			Type:   v1alpha1.ExperimentRunning,
			Status: v1.ConditionTrue,
			Reason: "AwesomeController",
		}
	}

	experimentStatus = func() *v1alpha1.ExperimentStatus {
		return &v1alpha1.ExperimentStatus{
			Conditions: []v1alpha1.ExperimentCondition{condInvalidExperimentSpec(), condAvailableExperiment()},
		}
	}
)

func TestNewCondition(t *testing.T) {
	nc := NewExperimentConditions(v1alpha1.ExperimentProgressing, v1.ConditionTrue, "TestReason", "TestMessage")
	assert.Equal(t, v1alpha1.ExperimentProgressing, nc.Type)
	assert.Equal(t, v1.ConditionTrue, nc.Status)
	assert.Equal(t, "TestMessage", nc.Message)
	assert.Equal(t, "TestReason", nc.Reason)
	assert.NotNil(t, nc.LastTransitionTime)
	assert.NotNil(t, nc.LastUpdateTime)

}

func TestGetExperimentCondition(t *testing.T) {
	exampleStatus := experimentStatus()

	tests := []struct {
		name     string
		status   v1alpha1.ExperimentStatus
		condType v1alpha1.ExperimentConditionType

		expected bool
	}{
		{
			name:     "condition exists",
			status:   *exampleStatus,
			condType: v1alpha1.ExperimentRunning,

			expected: true,
		},
		{
			name:     "condition does not exist",
			status:   *exampleStatus,
			condType: FailedRSCreateReason,

			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cond := GetExperimentCondition(test.status, test.condType)
			exists := cond != nil
			assert.Equal(t, exists, test.expected)
		})
	}
}

func TestSetExperimentCondition(t *testing.T) {
	now := metav1.Now()
	before := metav1.Time{Time: now.Add(-time.Minute)}
	tests := []struct {
		name string

		status *v1alpha1.ExperimentStatus
		cond   v1alpha1.ExperimentCondition

		expectedStatus *v1alpha1.ExperimentStatus
	}{
		{
			name:   "set for the first time",
			status: &v1alpha1.ExperimentStatus{},
			cond:   condAvailableExperiment(),

			expectedStatus: &v1alpha1.ExperimentStatus{Conditions: []v1alpha1.ExperimentCondition{condAvailableExperiment()}},
		},
		{
			name:   "simple set",
			status: &v1alpha1.ExperimentStatus{Conditions: []v1alpha1.ExperimentCondition{condInvalidExperimentSpec()}},
			cond:   condAvailableExperiment(),

			expectedStatus: experimentStatus(),
		},
		{
			name:   "No Changes",
			status: &v1alpha1.ExperimentStatus{Conditions: []v1alpha1.ExperimentCondition{condAvailableExperiment()}},
			cond:   condAvailableExperiment(),

			expectedStatus: &v1alpha1.ExperimentStatus{Conditions: []v1alpha1.ExperimentCondition{condAvailableExperiment()}},
		},
		{
			name: "Status change",
			status: &v1alpha1.ExperimentStatus{Conditions: []v1alpha1.ExperimentCondition{
				{
					Type:           v1alpha1.ExperimentRunning,
					Status:         v1.ConditionTrue,
					Reason:         "AwesomeController",
					LastUpdateTime: before,
				},
			}},
			cond: v1alpha1.ExperimentCondition{
				Type:           v1alpha1.ExperimentRunning,
				Status:         v1.ConditionFalse,
				Reason:         "AwesomeController",
				LastUpdateTime: now,
			},

			expectedStatus: &v1alpha1.ExperimentStatus{Conditions: []v1alpha1.ExperimentCondition{
				{
					Type:           v1alpha1.ExperimentRunning,
					Status:         v1.ConditionFalse,
					Reason:         "AwesomeController",
					LastUpdateTime: now,
				},
			}},
		},
		{
			name: "No status change",
			status: &v1alpha1.ExperimentStatus{Conditions: []v1alpha1.ExperimentCondition{
				{
					Type:           v1alpha1.ExperimentRunning,
					Status:         v1.ConditionTrue,
					Reason:         "AwesomeController",
					LastUpdateTime: before,
				},
			}},
			cond: v1alpha1.ExperimentCondition{
				Type:           v1alpha1.ExperimentRunning,
				Status:         v1.ConditionTrue,
				Reason:         "AwesomeController",
				LastUpdateTime: now,
			},

			expectedStatus: &v1alpha1.ExperimentStatus{Conditions: []v1alpha1.ExperimentCondition{
				{
					Type:           v1alpha1.ExperimentRunning,
					Status:         v1.ConditionTrue,
					Reason:         "AwesomeController",
					LastUpdateTime: before,
				},
			}},
		},
		{
			name:   "overwrite",
			status: &v1alpha1.ExperimentStatus{Conditions: []v1alpha1.ExperimentCondition{condInvalidExperimentSpec()}},
			cond:   condInvalidExperimentSpec2(),

			expectedStatus: &v1alpha1.ExperimentStatus{Conditions: []v1alpha1.ExperimentCondition{condInvalidExperimentSpec2()}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			SetExperimentCondition(test.status, test.cond)
			assert.Equal(t, test.status, test.expectedStatus)
		})
	}
}

func TestRemoveExperimentCondition(t *testing.T) {
	tests := []struct {
		name string

		status   *v1alpha1.ExperimentStatus
		condType v1alpha1.ExperimentConditionType

		expectedStatus *v1alpha1.ExperimentStatus
	}{
		{
			name: "remove from empty status",

			status:   &v1alpha1.ExperimentStatus{},
			condType: v1alpha1.InvalidExperimentSpec,

			expectedStatus: &v1alpha1.ExperimentStatus{},
		},
		{
			name: "simple remove",

			status:   &v1alpha1.ExperimentStatus{Conditions: []v1alpha1.ExperimentCondition{condInvalidExperimentSpec()}},
			condType: v1alpha1.InvalidExperimentSpec,

			expectedStatus: &v1alpha1.ExperimentStatus{},
		},
		{
			name: "doesn't remove anything",

			status:   experimentStatus(),
			condType: FailedRSCreateReason,

			expectedStatus: experimentStatus(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			RemoveExperimentCondition(test.status, test.condType)
			assert.Equal(t, test.status, test.expectedStatus)
		})
	}
}

func TestExperimentProgressing(t *testing.T) {
	templateStatus := func(name string, current, updated, ready, available int32) v1alpha1.TemplateStatus {
		return v1alpha1.TemplateStatus{
			Name:              name,
			Replicas:          current,
			UpdatedReplicas:   updated,
			ReadyReplicas:     ready,
			AvailableReplicas: available,
		}
	}

	tests := []struct {
		name          string
		updatedStatus []v1alpha1.TemplateStatus
		oldStatus     []v1alpha1.TemplateStatus
		expected      bool
	}{
		{
			name:     "No Changes",
			expected: false,
		},
		{
			name:          "Changes add new templateStatus",
			updatedStatus: []v1alpha1.TemplateStatus{templateStatus("foo", 0, 0, 0, 0)},
			expected:      true,
		},
		{
			name:          "Changes replica",
			updatedStatus: []v1alpha1.TemplateStatus{templateStatus("foo", 0, 0, 0, 0)},
			oldStatus:     []v1alpha1.TemplateStatus{templateStatus("foo", 1, 0, 0, 0)},
			expected:      true,
		},
		{
			name:          "Changes updated replica",
			updatedStatus: []v1alpha1.TemplateStatus{templateStatus("foo", 0, 0, 0, 0)},
			oldStatus:     []v1alpha1.TemplateStatus{templateStatus("foo", 0, 1, 0, 0)},
			expected:      true,
		},
		{
			name:          "Changes ready replica",
			updatedStatus: []v1alpha1.TemplateStatus{templateStatus("foo", 0, 0, 0, 0)},
			oldStatus:     []v1alpha1.TemplateStatus{templateStatus("foo", 0, 0, 1, 0)},
			expected:      true,
		},
		{
			name:          "Changes available replica",
			updatedStatus: []v1alpha1.TemplateStatus{templateStatus("foo", 0, 0, 0, 0)},
			oldStatus:     []v1alpha1.TemplateStatus{templateStatus("foo", 0, 0, 0, 1)},
			expected:      true,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			e := &v1alpha1.Experiment{
				Status: v1alpha1.ExperimentStatus{
					TemplateStatuses: test.oldStatus,
				},
			}
			old := v1alpha1.ExperimentStatus{
				TemplateStatuses: test.updatedStatus,
			}
			assert.Equal(t, test.expected, ExperimentProgressing(e, old))
		})
	}

}

func TestExperimentComplete(t *testing.T) {
	experiment := func(current, updated, available, ready int32, running bool) *v1alpha1.Experiment {
		e := &v1alpha1.Experiment{
			Spec: v1alpha1.ExperimentSpec{},
			Status: v1alpha1.ExperimentStatus{
				TemplateStatuses: []v1alpha1.TemplateStatus{{
					Name:              "test",
					Replicas:          current,
					UpdatedReplicas:   updated,
					AvailableReplicas: available,
					ReadyReplicas:     ready,
				}},
			},
		}
		if running {
			e.Status.Running = &running
		}
		return e
	}

	tests := []struct {
		name     string
		e        *v1alpha1.Experiment
		expected bool
	}{

		{
			name:     "Experiment not running",
			e:        experiment(0, 0, 0, 0, false),
			expected: true,
		},
		{
			name:     "Experiment running: running set to true",
			e:        experiment(5, 5, 5, 5, true),
			expected: false,
		},
		{
			name:     "Experiment running: waiting for no ready replicas",
			e:        experiment(0, 0, 0, 5, false),
			expected: false,
		},
		{
			name:     "Experiment running: waiting for no available replicas",
			e:        experiment(0, 0, 5, 0, false),
			expected: false,
		},
		{
			name:     "Experiment running: waiting for no updated replicas",
			e:        experiment(0, 5, 0, 0, false),
			expected: false,
		},
		{
			name:     "Experiment running: waiting for no replicas",
			e:        experiment(5, 0, 0, 0, false),
			expected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, ExperimentCompleted(test.e.Status))
		})
	}
}

func TestExperimentRunning(t *testing.T) {
	e := &v1alpha1.Experiment{}
	assert.False(t, ExperimentRunning(e))

	now := metav1.Now()
	e.Status.AvailableAt = &now
	assert.True(t, ExperimentRunning(e))
}

func TestExperimentTimeOut(t *testing.T) {

	now := metav1.Now()
	before := metav1.NewTime(now.Add(-10 * time.Second))

	conditons := func(reason string, lastUpdate metav1.Time) []v1alpha1.ExperimentCondition {
		return []v1alpha1.ExperimentCondition{{
			Type:           v1alpha1.ExperimentProgressing,
			Reason:         reason,
			LastUpdateTime: lastUpdate,
		}}
	}

	tests := []struct {
		name                    string
		progressDeadlineSeconds int32
		isAvailable             bool
		newStatus               v1alpha1.ExperimentStatus
		expected                bool
	}{
		{
			name:        "New RS is Available",
			isAvailable: true,
			newStatus: v1alpha1.ExperimentStatus{
				Conditions: conditons(NewRSAvailableReason, metav1.Now()),
			},
			expected: false,
		},
		{
			name: "Has no progressing condition",
			newStatus: v1alpha1.ExperimentStatus{
				Conditions: []v1alpha1.ExperimentCondition{},
			},
			expected: false,
		},
		{
			name: "Experiment is already has timed out condition",
			newStatus: v1alpha1.ExperimentStatus{
				Conditions: conditons(TimedOutReason, metav1.Now()),
			},
			expected: true,
		},
		{
			name:                    "Experiment has not timed out",
			progressDeadlineSeconds: 30,
			newStatus: v1alpha1.ExperimentStatus{
				Conditions: conditons(ReplicaSetUpdatedReason, before),
			},
			expected: false,
		},
		{
			name:                    "Experiment has timed out",
			progressDeadlineSeconds: 5,
			newStatus: v1alpha1.ExperimentStatus{
				Conditions: conditons(ReplicaSetUpdatedReason, before),
			},
			expected: true,
		},
	}
	for i := range tests {
		test := tests[i]
		t.Run(test.name, func(t *testing.T) {
			experiment := &v1alpha1.Experiment{
				Spec: v1alpha1.ExperimentSpec{
					ProgressDeadlineSeconds: &test.progressDeadlineSeconds,
				},
			}
			if test.isAvailable {
				experiment.Status.AvailableAt = &now
			}
			assert.Equal(t, test.expected, ExperimentTimeOut(experiment, test.newStatus))
		})
	}
}
