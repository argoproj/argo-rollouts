package evaluate

import (
	"github.com/antonmedv/expr"
)

// EvalCondition evaluates the condition with the resultValue as an input
func EvalCondition(resultValue interface{}, condition string) (bool, error) {
	env := map[string]interface{}{
		"result": resultValue,
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
