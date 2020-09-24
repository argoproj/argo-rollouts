package evaluate

import (
	"fmt"
	"strconv"

	"github.com/antonmedv/expr"
	"github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func EvaluateResult(result interface{}, metric v1alpha1.Metric, logCtx logrus.Entry) v1alpha1.AnalysisPhase {
	successCondition := false
	failCondition := false
	var err error

	if metric.SuccessCondition != "" {
		successCondition, err = EvalCondition(result, metric.SuccessCondition)
		if err != nil {
			logCtx.Warning(err.Error())
			return v1alpha1.AnalysisPhaseError
		}
	}
	if metric.FailureCondition != "" {
		failCondition, err = EvalCondition(result, metric.FailureCondition)
		if err != nil {
			logCtx.Warning(err.Error())
			return v1alpha1.AnalysisPhaseError
		}
	}

	switch {
	case metric.SuccessCondition == "" && metric.FailureCondition == "":
		//Always return success unless there is an error
		return v1alpha1.AnalysisPhaseSuccessful
	case metric.SuccessCondition != "" && metric.FailureCondition == "":
		// Without a failure condition, a measurement is considered a failure if the measurement's success condition is not true
		failCondition = !successCondition
	case metric.SuccessCondition == "" && metric.FailureCondition != "":
		// Without a success condition, a measurement is considered a successful if the measurement's failure condition is not true
		successCondition = !failCondition
	}

	if failCondition {
		return v1alpha1.AnalysisPhaseFailed
	}

	if !failCondition && !successCondition {
		return v1alpha1.AnalysisPhaseInconclusive
	}

	// If we reach this code path, failCondition is false and successCondition is true
	return v1alpha1.AnalysisPhaseSuccessful
}

// EvalCondition evaluates the condition with the resultValue as an input
func EvalCondition(resultValue interface{}, condition string) (bool, error) {
	var err error

	env := map[string]interface{}{
		"result":  resultValue,
		"asInt":   asInt,
		"asFloat": asFloat,
	}

	// Setup a clean recovery in case the eval code panics.
	// TODO: this actually might not be necessary since it seems evaluation lib handles panics from functions internally
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("evaluation logic panicked: %v", r)
		}
	}()

	program, err := expr.Compile(condition, expr.Env(env), expr.AsBool())
	if err != nil {
		return false, err
	}

	output, err := expr.Run(program, env)
	if err != nil {
		return false, err
	}

	return output.(bool), err
}

func asInt(in string) int64 {
	inAsInt, err := strconv.ParseInt(in, 10, 64)
	if err == nil {
		return inAsInt
	}
	panic(err)
}

func asFloat(in string) float64 {
	inAsFloat, err := strconv.ParseFloat(in, 64)
	if err == nil {
		return inAsFloat
	}
	panic(err)
}
