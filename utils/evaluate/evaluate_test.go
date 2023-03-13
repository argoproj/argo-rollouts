package evaluate

import (
	"fmt"
	"math"
	"reflect"
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
	status, err := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
	assert.NoError(t, err)
}

func TestEvaluateResultWithFailure(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "true",
		FailureCondition: "true",
	}
	logCtx := logrus.WithField("test", "test")
	status, err := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, status)
	assert.NoError(t, err)

}

func TestEvaluateResultInconclusive(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "false",
		FailureCondition: "false",
	}
	logCtx := logrus.WithField("test", "test")
	status, err := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseInconclusive, status)
	assert.NoError(t, err)
}

func TestEvaluateResultNoSuccessConditionAndNotFailing(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "",
		FailureCondition: "false",
	}
	logCtx := logrus.WithField("test", "test")
	status, err := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
	assert.NoError(t, err)
}

func TestEvaluateResultNoFailureConditionAndNotSuccessful(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "false",
		FailureCondition: "",
	}
	logCtx := logrus.WithField("test", "test")
	status, err := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseFailed, status)
	assert.NoError(t, err)
}

func TestEvaluateResultNoFailureConditionAndNoSuccessCondition(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "",
		FailureCondition: "",
	}
	logCtx := logrus.WithField("test", "test")
	status, err := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseSuccessful, status)
	assert.NoError(t, err)
}

func TestEvaluateResultWithErrorOnSuccessCondition(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "a == true",
		FailureCondition: "true",
	}
	logCtx := logrus.WithField("test", "test")
	status, err := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, status)
	assert.Error(t, err)
}

func TestEvaluateResultWithErrorOnFailureCondition(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "true",
		FailureCondition: "a == true",
	}
	logCtx := logrus.WithField("test", "test")
	status, err := EvaluateResult(true, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, status)
	assert.Error(t, err)
}

func TestEvaluateConditionWithSuccess(t *testing.T) {
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
	floats := map[string]interface{}{
		"service_apdex": map[string]interface{}{
			"label": nil,
			"values": map[string]interface{}{
				"values": []interface{}{float64(2), float64(2)},
			},
		},
	}
	b, err := EvalCondition(floats, "all(result.service_apdex.values.values, {# > 1})")
	if err != nil {
		panic(err)
	}
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

func TestIsInf(t *testing.T) {
	inf, notInf := math.Inf(0), 0.0
	assert.True(t, isInf(inf))
	assert.False(t, isInf(notInf))
}

func TestEqual(t *testing.T) {
	assert.True(t, Equal([]string{"a", "b"}, []string{"b", "a"}))
	assert.False(t, Equal([]string{"a"}, []string{"a", "b"}))
	assert.False(t, Equal([]string{"a", "b"}, []string{}))
}

func TestDefaultFunc(t *testing.T) {
	var nilFloat *float64
	var oneFloat float64 = 1
	var oneFloatPointer *float64 = new(float64)
	*oneFloatPointer = 1

	assert.True(t, defaultFunc(nilFloat)(nilFloat, 0) == 0)
	assert.True(t, defaultFunc(nilFloat)(nilFloat, 1) == 1)
	assert.True(t, defaultFunc(nilFloat)(nilFloat, 2) == 2)
	assert.True(t, defaultFunc(oneFloatPointer)(oneFloatPointer, 0) == oneFloat)
}

func TestIsNilFunc(t *testing.T) {
	var nilFloat *float64
	var oneFloat float64 = 1
	var nilArr []string
	var twoArr []string = []string{"hi", "hello"}

	assert.True(t, isNilFunc(nilFloat)(nilFloat))
	assert.True(t, isNilFunc(nilArr)(nilArr))
	assert.False(t, isNilFunc(oneFloat)(oneFloat))
	assert.False(t, isNilFunc(twoArr)(twoArr))
	assert.False(t, isNilFunc(1)(1))
	assert.False(t, isNilFunc(false)(false))
}

func TestIsNil(t *testing.T) {
	var nilFloat *float64
	var oneFloat float64 = 1
	var nilArr []string
	var twoArr []string = []string{"hi", "hello"}

	assert.True(t, isNil(nilFloat))
	assert.True(t, isNil(nilArr))
	assert.False(t, isNil(oneFloat))
	assert.False(t, isNil(twoArr))
	assert.False(t, isNil(1))
	assert.False(t, isNil(false))
}

func TestValueFromPointer(t *testing.T) {
	var nilFloat *float64
	var oneFloat float64 = 1
	var oneFloatPointer *float64 = new(float64)
	*oneFloatPointer = 1
	var nilArr []string
	var twoArr []string = []string{"hi", "hello"}

	assert.True(t, valueFromPointer(nilFloat) == nil)
	assert.True(t, valueFromPointer(nilArr) == nil)
	assert.True(t, reflect.DeepEqual(valueFromPointer(twoArr).([]string), twoArr))
	assert.True(t, valueFromPointer(oneFloatPointer) == oneFloat)
	assert.True(t, valueFromPointer(1) == 1)
	assert.True(t, valueFromPointer(false) == false)
}
