package conditions

import (
	"fmt"
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
			name: "Phase change",
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

func TestExperimentRunning(t *testing.T) {
	e := &v1alpha1.Experiment{}
	assert.False(t, ExperimentRunning(e))

	now := metav1.Now()
	e.Status.AvailableAt = &now
	assert.True(t, ExperimentRunning(e))
}

func TestVerifyExperimentSpecBaseCases(t *testing.T) {
	ex := &v1alpha1.Experiment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "foo",
		},
		Spec: v1alpha1.ExperimentSpec{
			Templates: []v1alpha1.TemplateSpec{{
				Name: "test",
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"key": "value"},
				},
			}},
		},
	}
	cond := VerifyExperimentSpec(ex, nil)
	assert.Nil(t, cond)

	sameNameTemplate := ex.DeepCopy()
	sameNameTemplate.Spec.Templates = append(sameNameTemplate.Spec.Templates, sameNameTemplate.Spec.Templates[0])
	sameNameTemplateConf := VerifyExperimentSpec(sameNameTemplate, nil)
	assert.NotNil(t, sameNameTemplateConf)
	sameNameTemplateConfMessage := fmt.Sprintf(ExperimentTemplateNameRepeatedMessage, sameNameTemplate.Name, sameNameTemplate.Spec.Templates[0].Name)
	assert.Equal(t, sameNameTemplateConfMessage, sameNameTemplateConf.Message)
	assert.Equal(t, InvalidSpecReason, sameNameTemplateConf.Reason)

	noNameTemplate := ex.DeepCopy()
	noNameTemplate.Spec.Templates[0].Name = ""
	noNameTemplateConf := VerifyExperimentSpec(noNameTemplate, nil)
	assert.NotNil(t, noNameTemplateConf)
	noNameTemplateMessage := fmt.Sprintf(ExperimentTemplateNameEmpty, noNameTemplate.Name, 0)
	assert.Equal(t, noNameTemplateMessage, noNameTemplateConf.Message)
	assert.Equal(t, InvalidSpecReason, noNameTemplateConf.Reason)

	selectorEverything := ex.DeepCopy()
	selectorEverything.Spec.Templates[0].Selector = &metav1.LabelSelector{}
	selectorEverythingConf := VerifyExperimentSpec(selectorEverything, nil)
	assert.NotNil(t, selectorEverythingConf)
	selectAllMessage := fmt.Sprintf(ExperimentSelectAllMessage, 0)
	assert.Equal(t, selectAllMessage, selectorEverythingConf.Message)
	assert.Equal(t, InvalidSpecReason, selectorEverythingConf.Reason)

	noSelector := ex.DeepCopy()
	noSelector.Spec.Templates[0].Selector = nil
	noSelectorCond := VerifyExperimentSpec(noSelector, nil)
	assert.NotNil(t, noSelectorCond)
	missingField := fmt.Sprintf(".Spec.Templates[%d].Selector", 0)
	assert.Equal(t, fmt.Sprintf(MissingFieldMessage, missingField), noSelectorCond.Message)
	assert.Equal(t, InvalidSpecReason, noSelectorCond.Reason)

	minReadyLongerThanProgressDeadline := ex.DeepCopy()
	minReadyLongerThanProgressDeadline.Spec.Templates[0].MinReadySeconds = 1000
	minReadyLongerThanProgressDeadlineCond := VerifyExperimentSpec(minReadyLongerThanProgressDeadline, nil)
	assert.NotNil(t, minReadyLongerThanProgressDeadlineCond)
	assert.Equal(t, InvalidSpecReason, minReadyLongerThanProgressDeadlineCond.Reason)
	minReadyLongerMessage := fmt.Sprintf(ExperimentMinReadyLongerThanDeadlineMessage, 0)
	assert.Equal(t, minReadyLongerMessage, minReadyLongerThanProgressDeadlineCond.Message)

	//Test switching from a prev invalid spec to another
	prevLastUpdateTime := selectorEverythingConf.LastUpdateTime
	sameInvalidSpec := VerifyExperimentSpec(selectorEverything, selectorEverythingConf)
	assert.NotNil(t, sameInvalidSpec)
	assert.Equal(t, selectAllMessage, sameInvalidSpec.Message)
	assert.Equal(t, InvalidSpecReason, sameInvalidSpec.Reason)
	assert.NotEqual(t, prevLastUpdateTime, sameInvalidSpec.LastUpdateTime)
}
