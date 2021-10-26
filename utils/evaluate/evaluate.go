package evaluate

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"

	"github.com/antonmedv/expr"
	"github.com/antonmedv/expr/file"
	"github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func EvaluateResult(result interface{}, metric v1alpha1.Metric, logCtx logrus.Entry) (v1alpha1.AnalysisPhase, error) {
	successCondition := false
	failCondition := false
	var err error

	if metric.SuccessCondition != "" {
		successCondition, err = EvalCondition(result, metric.SuccessCondition)
		if err != nil {
			return v1alpha1.AnalysisPhaseError, err
		}
	}
	if metric.FailureCondition != "" {
		failCondition, err = EvalCondition(result, metric.FailureCondition)
		if err != nil {
			return v1alpha1.AnalysisPhaseError, err
		}
	}

	switch {
	case metric.SuccessCondition == "" && metric.FailureCondition == "":
		//Always return success unless there is an error
		return v1alpha1.AnalysisPhaseSuccessful, nil
	case metric.SuccessCondition != "" && metric.FailureCondition == "":
		// Without a failure condition, a measurement is considered a failure if the measurement's success condition is not true
		failCondition = !successCondition
	case metric.SuccessCondition == "" && metric.FailureCondition != "":
		// Without a success condition, a measurement is considered a successful if the measurement's failure condition is not true
		successCondition = !failCondition
	}

	if failCondition {
		return v1alpha1.AnalysisPhaseFailed, nil
	}

	if !failCondition && !successCondition {
		return v1alpha1.AnalysisPhaseInconclusive, nil
	}

	// If we reach this code path, failCondition is false and successCondition is true
	return v1alpha1.AnalysisPhaseSuccessful, nil
}

// EvalCondition evaluates the condition with the resultValue as an input
func EvalCondition(resultValue interface{}, condition string) (bool, error) {
	var err error

	env := map[string]interface{}{
		"result":  valueFromPointer(resultValue),
		"asInt":   asInt,
		"asFloat": asFloat,
		"isNaN":   math.IsNaN,
		"isInf":   isInf,
		"isNil":   isNilFunc(resultValue),
		"default": defaultFunc(resultValue),
	}

	unwrapFileErr := func(e error) error {
		if fileErr, ok := err.(*file.Error); ok {
			e = errors.New(fileErr.Message)
		}
		return e
	}

	program, err := expr.Compile(condition, expr.Env(env))
	if err != nil {
		return false, unwrapFileErr(err)
	}

	output, err := expr.Run(program, env)
	if err != nil {
		return false, unwrapFileErr(err)
	}

	switch val := output.(type) {
	case bool:
		return val, nil
	default:
		return false, fmt.Errorf("expected bool, but got %T", val)
	}
}

func isInf(f float64) bool {
	return math.IsInf(f, 0)
}

func asInt(in interface{}) int64 {
	switch i := in.(type) {
	case float64:
		return int64(i)
	case float32:
		return int64(i)
	case int64:
		return i
	case int32:
		return int64(i)
	case int16:
		return int64(i)
	case int8:
		return int64(i)
	case int:
		return int64(i)
	case uint64:
		return int64(i)
	case uint32:
		return int64(i)
	case uint16:
		return int64(i)
	case uint8:
		return int64(i)
	case uint:
		return int64(i)
	case string:
		inAsInt, err := strconv.ParseInt(i, 10, 64)
		if err == nil {
			return inAsInt
		}
		panic(err)
	}
	panic(fmt.Sprintf("asInt() not supported on %v %v", reflect.TypeOf(in), in))
}

func asFloat(in interface{}) float64 {
	switch i := in.(type) {
	case float64:
		return i
	case float32:
		return float64(i)
	case int64:
		return float64(i)
	case int32:
		return float64(i)
	case int16:
		return float64(i)
	case int8:
		return float64(i)
	case int:
		return float64(i)
	case uint64:
		return float64(i)
	case uint32:
		return float64(i)
	case uint16:
		return float64(i)
	case uint8:
		return float64(i)
	case uint:
		return float64(i)
	case string:
		inAsFloat, err := strconv.ParseFloat(i, 64)
		if err == nil {
			return inAsFloat
		}
		panic(err)
	}
	panic(fmt.Sprintf("asFloat() not supported on %v %v", reflect.TypeOf(in), in))
}

// Check whether two slices of type string are equal or not.
func Equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left := make(map[string]bool)
	for _, x := range a {
		left[x] = true
	}
	for _, x := range b {
		if !left[x] {
			return false
		}
	}
	return true
}

func defaultFunc(resultValue interface{}) func(interface{}, interface{}) interface{} {
	return func(_ interface{}, defaultValue interface{}) interface{} {
		if isNil(resultValue) {
			return defaultValue
		}
		return valueFromPointer(resultValue)
	}
}

func isNilFunc(resultValue interface{}) func(interface{}) bool {
	return func(_ interface{}) bool {
		return isNil(resultValue)
	}
}

// isNil is courtesy of: https://gist.github.com/mangatmodi/06946f937cbff24788fa1d9f94b6b138
func isNil(in interface{}) (out bool) {
	if in == nil {
		out = true
		return
	}

	switch reflect.TypeOf(in).Kind() {
	case reflect.Ptr, reflect.Map, reflect.Array, reflect.Chan, reflect.Slice:
		out = reflect.ValueOf(in).IsNil()
	}

	return
}

// valueFromPointer allows pointers to be passed in from the provider, but then extracts the value from
// the pointer if the pointer is not nil, else returns nil
func valueFromPointer(in interface{}) (out interface{}) {
	if isNil(in) {
		return
	}

	if reflect.TypeOf(in).Kind() != reflect.Ptr {
		out = in
		return
	}

	out = reflect.ValueOf(in).Elem().Interface()
	return
}
