package evaluate

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/file"
	"github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

var indexedResultPattern = regexp.MustCompile(`\bresult\s*\[`)

func EvaluateResult(result any, metric v1alpha1.Metric, logCtx logrus.Entry) (v1alpha1.AnalysisPhase, error) {
	successCondition := false
	failCondition := false
	var err error

	if metric.SuccessCondition != "" {
		successCondition, err = EvalCondition(result, metric.SuccessCondition)
		if err != nil {
			return v1alpha1.AnalysisPhaseError, formatEvalError(err, "successCondition", metric.SuccessCondition, result)
		}
	}
	if metric.FailureCondition != "" {
		failCondition, err = EvalCondition(result, metric.FailureCondition)
		if err != nil {
			return v1alpha1.AnalysisPhaseError, formatEvalError(err, "failureCondition", metric.FailureCondition, result)
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

// formatEvalError wraps an expression evaluation error with context about the condition,
// the expression, and the actual result value to help users understand why the evaluation failed.
func formatEvalError(err error, conditionType string, expression string, result any) error {
	if isNilOrEmpty(result) {
		return fmt.Errorf("could not evaluate %s \"%s\": metric result is nil or empty: no data returned from the metric provider", conditionType, expression)
	}
	return fmt.Errorf("could not evaluate %s \"%s\": %w", conditionType, expression, err)
}

// isNilOrEmpty checks if a result value is nil or an empty slice/array
func isNilOrEmpty(result any) bool {
	if isNil(result) {
		return true
	}
	v := reflect.ValueOf(result)
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		return v.Len() == 0
	}
	return false
}

func EvalTime(expression string) (time.Time, error) {
	var err error

	env := map[string]any{
		"isNaN": math.IsNaN,
		"isInf": isInf,
	}

	unwrapFileErr := func(e error) error {
		if fileErr, ok := err.(*file.Error); ok {
			e = errors.New(fileErr.Message)
		}
		return e
	}

	program, err := expr.Compile(expression, expr.Env(env))
	if err != nil {
		return time.Time{}, unwrapFileErr(err)
	}

	output, err := expr.Run(program, env)
	if err != nil {
		return time.Time{}, unwrapFileErr(err)
	}

	switch val := output.(type) {
	case time.Time:
		return val, nil
	default:
		return time.Time{}, fmt.Errorf("expected time.Time, but got %T", val)
	}
}

// EvalCondition evaluates the condition with the resultValue as an input
func EvalCondition(resultValue any, condition string) (bool, error) {
	var err error

	env := map[string]any{
		"result":  valueFromPointer(resultValue),
		"asInt":   asInt,
		"asFloat": asFloat,
		"isNaN":   math.IsNaN,
		"isInf":   isInf,
		"isNil":   isNilFunc(resultValue),
		"default": defaultFunc(resultValue),
	}

	if err = validateIndexedResultAccess(resultValue, condition); err != nil {
		return false, err
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

func validateIndexedResultAccess(resultValue any, condition string) error {
	if !indexedResultPattern.MatchString(condition) {
		return nil
	}

	if !isEmptyIndexableResult(resultValue) {
		return nil
	}

	return errors.New("metric result is empty or unavailable, and the condition cannot index into result; guard the condition with len(result) > 0")
}

func isEmptyIndexableResult(resultValue any) bool {
	value := valueFromPointer(resultValue)
	if value == nil {
		return true
	}

	switch reflect.ValueOf(value).Kind() {
	case reflect.Array, reflect.Slice, reflect.String:
		return reflect.ValueOf(value).Len() == 0
	default:
		return false
	}
}

func isInf(f float64) bool {
	return math.IsInf(f, 0)
}

func asInt(in any) int64 {
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

func asFloat(in any) float64 {
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

func defaultFunc(resultValue any) func(any, any) any {
	return func(_ any, defaultValue any) any {
		if isNil(resultValue) {
			return defaultValue
		}
		return valueFromPointer(resultValue)
	}
}

func isNilFunc(resultValue any) func(any) bool {
	return func(_ any) bool {
		return isNil(resultValue)
	}
}

// isNil is courtesy of: https://gist.github.com/mangatmodi/06946f937cbff24788fa1d9f94b6b138
func isNil(in any) (out bool) {
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
func valueFromPointer(in any) (out any) {
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
