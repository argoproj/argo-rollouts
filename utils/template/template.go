package query

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/valyala/fasttemplate"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	openBracket  = "{{"
	closeBracket = "}}"
)

// ResolveArgs substitute the supplied arguments in the given template
func ResolveArgs(template string, args []v1alpha1.Argument) (string, error) {
	t, err := fasttemplate.NewTemplate(template, openBracket, closeBracket)
	if err != nil {
		return "", err
	}
	argsMap := make(map[string]string)
	for i := range args {
		arg := args[i]
		if arg.Value == nil {
			return "", fmt.Errorf("argument \"%s\" was not supplied", arg.Name)
		}
		argsMap[fmt.Sprintf("args.%s", arg.Name)] = *arg.Value
	}
	var unresolvedErr error
	s := t.ExecuteFuncString(func(w io.Writer, tag string) (int, error) {
		cleanedTag := strings.TrimSpace(tag)
		if value, ok := argsMap[cleanedTag]; ok {
			return w.Write([]byte(value))
		}
		unresolvedErr = fmt.Errorf("failed to resolve {{%s}}", tag)

		return w.Write([]byte(""))
	})
	return s, unresolvedErr
}

// ResolveQuotedArgs is used for substituting templates which need quotes escaped such as when args
// are used in JSON which we marshal and unmarshal
func ResolveQuotedArgs(template string, args []v1alpha1.Argument) (string, error) {
	quotedArgs := make([]v1alpha1.Argument, len(args))
	for i, arg := range args {
		quotedArg := v1alpha1.Argument{
			Name: arg.Name,
		}
		if arg.Value != nil {
			// The following escapes any special characters (e.g. newlines, tabs, etc...)
			// in preparation for substitution
			replacement := strconv.Quote(*arg.Value)
			replacement = replacement[1 : len(replacement)-1]
			quotedArg.Value = &replacement
		}
		quotedArgs[i] = quotedArg
	}
	return ResolveArgs(template, quotedArgs)
}
