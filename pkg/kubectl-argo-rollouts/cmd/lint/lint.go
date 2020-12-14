package lint

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"unicode"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/validation"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type LintOptions struct {
	options.ArgoRolloutsOptions
	File string
}

const (
	lintExample = `
	# Lint a rollout
	%[1]s lint -f my-rollout.yaml`
)

// NewCmdLint returns a new instance of a `rollouts lint` command
func NewCmdLint(o *options.ArgoRolloutsOptions) *cobra.Command {
	lintOptions := LintOptions{
		ArgoRolloutsOptions: *o,
	}
	var cmd = &cobra.Command{
		Use:          "lint",
		Short:        "Lint and validate a Rollout",
		Long:         "This command lints and validates a new Rollout resource from a file.",
		Example:      o.Example(lintExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if lintOptions.File == "" {
				return o.UsageErr(c)
			}

			err := lintOptions.lintResource(lintOptions.File)
			if err != nil {
				return err
			}

			return nil
		},
	}
	cmd.Flags().StringVarP(&lintOptions.File, "filename", "f", "", "File to lint")
	return cmd
}

// isJSON detects if the byte array looks like json, based on the first non-whitespace character
func isJSON(fileBytes []byte) bool {
	for _, b := range fileBytes {
		if !unicode.IsSpace(rune(b)) {
			return b == '{'
		}
	}
	return false
}

func unmarshal(fileBytes []byte, obj interface{}) error {
	if isJSON(fileBytes) {
		decoder := json.NewDecoder(bytes.NewReader(fileBytes))
		decoder.DisallowUnknownFields()
		return decoder.Decode(&obj)
	}
	return yaml.UnmarshalStrict(fileBytes, &obj, yaml.DisallowUnknownFields)
}

func (l *LintOptions) lintResource(path string) error {
	fileBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	var un unstructured.Unstructured
	err = unmarshal(fileBytes, &un)
	if err != nil {
		return err
	}
	gvk := un.GroupVersionKind()
	switch {
	case gvk.Group == rollouts.Group && gvk.Kind == rollouts.RolloutKind:
		var ro v1alpha1.Rollout
		err = unmarshal(fileBytes, &ro)
		if err != nil {
			return err
		}
		errs := validation.ValidateRollout(&ro)
		if 0 < len(errs) {
			return errs[0]
		}
	}
	return nil
}
