package query

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/valyala/fasttemplate"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	openBracket               = "{{"
	closeBracket              = "}}"
	experimentPodTemplateHash = "templates.%s.podTemplateHash"
	experimentReplicasetName  = "templates.%s.replicaset.name"
	experimentAvailableAt     = "experiment.availableAt"
	experimentEndsAt          = "experiment.finishedAt"
)

// ResolveExperimentArgsValue substitutes values from the experiment (i.e. a template's pod hash) in the args value field
func ResolveExperimentArgsValue(argTemplate string, ex *v1alpha1.Experiment, templateRSs map[string]*appsv1.ReplicaSet) (string, error) {
	t, err := fasttemplate.NewTemplate(argTemplate, openBracket, closeBracket)
	if err != nil {
		return "", err
	}
	argsMap := make(map[string]string)
	if ex.Status.AvailableAt != nil {
		argsMap[experimentAvailableAt] = ex.Status.AvailableAt.Format(time.RFC3339)
		if ex.Spec.Duration != "" {
			duration, err := ex.Spec.Duration.Duration()
			if err != nil {
				return "", err
			}
			argsMap[experimentEndsAt] = ex.Status.AvailableAt.Add(duration).Format(time.RFC3339)
		}
	}
	for _, template := range ex.Spec.Templates {
		if rs, ok := templateRSs[template.Name]; ok {
			argsMap[fmt.Sprintf(experimentPodTemplateHash, template.Name)] = rs.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			argsMap[fmt.Sprintf(experimentReplicasetName, template.Name)] = rs.Name
		}
	}
	return resolve(t, argsMap)
}

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
	return resolve(t, argsMap)
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

func resolve(t *fasttemplate.Template, argsMap map[string]string) (string, error) {
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
