package evaluate

import (
	"fmt"
	"strconv"

	"github.com/antonmedv/expr"
)

// EvalCondition evaluates the condition with the resultValue as an input
func EvalCondition(resultValue interface{}, condition string) (bool, error) {
	var err error

	env := map[string]interface{}{
		"result":  resultValue,
		"asInt":   asInt,
		"asFloat": asFloat,
	}

	// Setup a clean recovery in case the eval code panics.
	// TODO: this actually might not be nessary since it seems evaluation lib handles panics from functions internally
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
