package evaluate

import (
	"strconv"

	"github.com/antonmedv/expr"
)

// EvalCondition evaluates the condition with the resultValue as an input
func EvalCondition(resultValue interface{}, condition string) (bool, error) {
	env := map[string]interface{}{
		"result": resultValue,
		"asInt": func(in string) int64 {
			inAsInt, err := strconv.ParseInt(in, 10, 64)
			if err == nil {
				return inAsInt
			}
			panic(err)
		},
		"asFloat": func(in string) float64 {
			inAsFloat, err := strconv.ParseFloat(in, 64)
			if err == nil {
				return inAsFloat
			}
			panic(err)
		},
	}

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
