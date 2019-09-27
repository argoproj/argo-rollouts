package query

import (
	"fmt"
	"io"

	"github.com/valyala/fasttemplate"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	openBracket  = "{{"
	closeBracket = "}}"
)

// BuildQuery starts in a template and injects the provider args
func BuildQuery(template string, args []v1alpha1.Argument) (string, error) {
	t, err := fasttemplate.NewTemplate(template, openBracket, closeBracket)
	if err != nil {
		return "", err
	}
	argsMap := make(map[string]string)
	for i := range args {
		arg := args[i]
		argsMap[fmt.Sprintf("input.%s", arg.Name)] = arg.Value
	}
	var unresolvedErr error
	s := t.ExecuteFuncString(func(w io.Writer, tag string) (int, error) {
		if value, ok := argsMap[tag]; ok {
			return w.Write([]byte(value))
		}
		unresolvedErr = fmt.Errorf("failed to resolve {{%s}}", tag)

		return w.Write([]byte(""))
	})
	return s, unresolvedErr
}
