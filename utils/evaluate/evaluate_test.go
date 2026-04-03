package evaluate

import (
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

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

func TestEvaluateResultWithIndexedEmptyResult(t *testing.T) {
	metric := v1alpha1.Metric{
		SuccessCondition: "result[0] <= 10",
	}
	logCtx := logrus.WithField("test", "test")
	status, err := EvaluateResult([]float64{}, metric, *logCtx)
	assert.Equal(t, v1alpha1.AnalysisPhaseError, status)
	assert.ErrorContains(t, err, "metric result is empty or unavailable")
	assert.ErrorContains(t, err, "len(result) > 0")
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

func TestEvaluateConditionWithIndexedEmptyResult(t *testing.T) {
	b, err := EvalCondition([]float64{}, "result[0] <= 10")
	assert.False(t, b)
	assert.ErrorContains(t, err, "metric result is empty or unavailable")
	assert.ErrorContains(t, err, "len(result) > 0")
}

func TestEvaluateConditionWithIndexedNonEmptyResult(t *testing.T) {
	b, err := EvalCondition([]float64{5}, "result[0] <= 10")
	assert.NoError(t, err)
	assert.True(t, b)
}

func TestValidateIndexedResultAccess(t *testing.T) {
	tests := []struct {
		name       string
		result     any
		condition  string
		wantErr    bool
		errMessage string
	}{
		{
			name:      "without indexed access",
			result:    []float64{},
			condition: "len(result) == 0",
		},
		{
			name:      "with non-empty slice",
			result:    []float64{1},
			condition: "result[0] <= 10",
		},
		{
			name:       "with nil result",
			result:     nil,
			condition:  "result[0] <= 10",
			wantErr:    true,
			errMessage: "metric result is empty or unavailable",
		},
		{
			name:      "with non-indexable result",
			result:    true,
			condition: "result[0] <= 10",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateIndexedResultAccess(test.result, test.condition)
			if test.wantErr {
				assert.ErrorContains(t, err, test.errMessage)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestIsEmptyIndexableResult(t *testing.T) {
	tests := []struct {
		name   string
		result any
		want   bool
	}{
		{
			name:   "nil",
			result: nil,
			want:   true,
		},
		{
			name:   "empty slice",
			result: []float64{},
			want:   true,
		},
		{
			name:   "non-empty slice",
			result: []float64{1},
			want:   false,
		},
		{
			name:   "empty string",
			result: "",
			want:   true,
		},
		{
			name:   "non-empty string",
			result: "ok",
			want:   false,
		},
		{
			name:   "non-indexable type",
			result: true,
			want:   false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, isEmptyIndexableResult(test.result))
		})
	}
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
	floats := map[string]any{
		"service_apdex": map[string]any{
			"label": nil,
			"values": map[string]any{
				"values": []any{float64(2), float64(2)},
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
		input       any
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
		input      any
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
		input       any
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

func TestEvalTimeWithSuccessExpr(t *testing.T) {
	status, err := EvalTime(`date("2023-08-14 00:00:00", "2006-01-02 15:04:05", "UTC") - duration("1h")`)
	assert.Equal(t, time.Date(2023, time.August, 13, 23, 0, 0, 0, time.UTC), status)
	assert.NoError(t, err)
}

func TestEvalTimeWithNotTimeResult(t *testing.T) {
	status, err := EvalTime(`hello`)
	assert.Equal(t, time.Time{}, status)
	assert.Error(t, err)
}

func TestEvalTimeWithInvalidExpression(t *testing.T) {
	status, err := EvalTime(`now() -- ?`)
	assert.Equal(t, time.Time{}, status)
	assert.Error(t, err)
}
