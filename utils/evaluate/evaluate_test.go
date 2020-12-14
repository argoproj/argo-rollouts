package evaluate

import (
	"fmt"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func TestEvaluateResultWithSuccess(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "true",
		FailureCondition: "false",
	}
	logCtx := logrus.WithField("test", "test")
	status := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
}

func TestEvaluateResultWithFailure(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "true",
		FailureCondition: "true",
	}
	logCtx := logrus.WithField("test", "test")
	status := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, status)

}

func TestEvaluateResultInconclusive(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "false",
		FailureCondition: "false",
	}
	logCtx := logrus.WithField("test", "test")
	status := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseInconclusive, status)
}

func TestEvaluateResultNoSuccessConditionAndNotFailing(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "",
		FailureCondition: "false",
	}
	logCtx := logrus.WithField("test", "test")
	status := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
}

func TestEvaluateResultNoFailureConditionAndNotSuccessful(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "false",
		FailureCondition: "",
	}
	logCtx := logrus.WithField("test", "test")
	status := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, status)
}

func TestEvaluateResultNoFailureConditionAndNoSuccessCondition(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "",
		FailureCondition: "",
	}
	logCtx := logrus.WithField("test", "test")
	status := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
}

func TestEvaluateResultWithErrorOnSuccessCondition(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "a == true",
		FailureCondition: "true",
	}
	logCtx := logrus.WithField("test", "test")
	status := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, status)
}

func TestEvaluateResultWithErrorOnFailureCondition(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "true",
		FailureCondition: "a == true",
	}
	logCtx := logrus.WithField("test", "test")
	status := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, status)
}

func TestEvaluateConditionWithSucces(t *testing.T) {
	b, err := EvalCondition(true, "result == true")
	assert.Nil(t, err)
	assert.True(t, b)
}

func TestEvaluateConditionWithFailure(t *testing.T) {
	b, err := EvalCondition(true, "result == false")
	assert.Nil(t, err)
	assert.False(t, b)
}

func TestErrorWithNonBoolReturn(t *testing.T) {
	b, err := EvalCondition(true, "1")
	assert.Equal(t, fmt.Errorf("expected bool, but got int"), err)
	assert.False(t, b)
}

func TestErrorWithInvalidReference(t *testing.T) {
	b, err := EvalCondition(true, "invalidVariable")
	assert.Equal(t, fmt.Errorf("unknown name invalidVariable"), err)
	assert.False(t, b)
}

func TestEvaluateArray(t *testing.T) {
	floats := []float64{float64(2), float64(2)}
	b, err := EvalCondition(floats, "all(result, {# > 1})")
	assert.Nil(t, err)
	assert.True(t, b)
}

func TestEvaluateInOperator(t *testing.T) {
	floats := []float64{float64(2), float64(2)}
	b, err := EvalCondition(floats, "2 in result")
	assert.Nil(t, err)
	assert.True(t, b)
}

func TestEvaluateFloat64(t *testing.T) {
	b, err := EvalCondition(float64(5), "result > 1")
	assert.Nil(t, err)
	assert.True(t, b)
}

func TestEvaluateInvalidStruct(t *testing.T) {
	b, err := EvalCondition(true, "result.Name() == 'hi'")
	assert.Errorf(t, err, "")
	assert.False(t, b)
}

func TestEvaluateAsIntPanic(t *testing.T) {
	b, err := EvalCondition("1.1", "asInt(result) == 1.1")
	assert.Errorf(t, err, "got expected error: %v", err)
	assert.False(t, b)
}

func TestEvaluateAsInt(t *testing.T) {
	tests := []struct {
		input       interface{}
		expression  string
		expectation bool
	}{
		{"1", "asInt(result) == 1", true},
		{1, "asInt(result) == 1", true},
		{1.123, "asInt(result) == 1", true},
	}
	for _, test := range tests {
		b, err := EvalCondition(test.input, test.expression)
		assert.NoError(t, err)
		assert.Equal(t, test.expectation, b)
	}
}

func TestEvaluateAsFloatError(t *testing.T) {
	tests := []struct {
		input      interface{}
		expression string
		errRegexp  string
	}{
		{"NotANum", "asFloat(result) == 1.1", `strconv.ParseFloat: parsing "NotANum": invalid syntax`},
		{"1.1", "asFloat(result) == \"1.1\"", `invalid operation: == \(mismatched types float64 and string\)`},
	}
	for _, test := range tests {
		b, err := EvalCondition(test.input, test.expression)
		assert.Error(t, err)
		assert.False(t, b)
		assert.Regexp(t, test.errRegexp, err.Error())
	}
}

func TestEvaluateAsFloat(t *testing.T) {
	tests := []struct {
		input       interface{}
		expression  string
		expectation bool
	}{
		{"1.1", "asFloat(result) == 1.1", true},
		{"1.1", "asFloat(result) >= 1.1", true},
		{"1.1", "asFloat(result) <= 1.1", true},
		{1.1, "asFloat(result) == 1.1", true},
		{1, "asFloat(result) == 1", true},
		{1, "asFloat(result) >= 1", true},
		{1, "asFloat(result) >= 1", true},
	}
	for _, test := range tests {
		b, err := EvalCondition(test.input, test.expression)
		assert.NoError(t, err)
		assert.Equal(t, test.expectation, b)
	}
}

func TestAsInt(t *testing.T) {
	tests := []struct {
		input       string
		output      int64
		shouldPanic bool
	}{
		{"1", 1, false},
		{"notint", 1, true},
		{"1.1", 1, true},
	}

	for _, test := range tests {
		if test.shouldPanic {
			assert.Panics(t, func() { asInt(test.input) })
		} else {
			assert.Equal(t, test.output, asInt(test.input))
		}
	}
}

func TestAsFloat(t *testing.T) {
	tests := []struct {
		input       string
		output      float64
		shouldPanic bool
	}{
		{"1", 1, false},
		{"notfloat", 1, true},
		{"1.1", 1.1, false},
	}

	for _, test := range tests {
		if test.shouldPanic {
			assert.Panics(t, func() { asFloat(test.input) })
		} else {
			assert.Equal(t, test.output, asFloat(test.input))
		}
	}
}
