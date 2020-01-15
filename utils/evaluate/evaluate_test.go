package evaluate

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEvaluateConditonWithSucces(t *testing.T) {
	b, err := EvalCondition(true, "result == true")
	assert.Nil(t, err)
	assert.True(t, b)
}

func TestEvaluateConditonWithFailure(t *testing.T) {
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
	assert.Equal(t, fmt.Errorf("unknown name invalidVariable (1:1)\n | invalidVariable\n | ^"), err)
	assert.False(t, b)
}

func TestEvaluateArray(t *testing.T) {
	floats := []float64{float64(2), float64(2)}
	b, err := EvalCondition(floats, "all(result, {# > 1})")
	assert.Nil(t, err)
	assert.True(t, b)
}

func TestEvaluatInOperator(t *testing.T) {
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
	b, err := EvalCondition("1", "asInt(result) == 1")
	assert.NoError(t, err)
	assert.True(t, b)
}

func TestEvaluateAsFloatPanic(t *testing.T) {
	b, err := EvalCondition("NotANum", "asFloat(result) == 1.1")
	assert.Errorf(t, err, "got expected error: %v", err)
	assert.False(t, b)
}

func TestEvaluateAsFloat(t *testing.T) {
	b, err := EvalCondition("1.1", "asFloat(result) == 1.1")
	assert.NoError(t, err)
	assert.True(t, b)
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
