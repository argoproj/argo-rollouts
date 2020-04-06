package create

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"
	"unicode"

	"github.com/ghodss/yaml"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/get"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
)

type CreateOptions struct {
	get.GetOptions
	options.ArgoRolloutsOptions

	Files    []string
	From     string
	FromFile string
}

type CreateAnalysisRunOptions struct {
	options.ArgoRolloutsOptions

	Name         string
	GenerateName string
	InstanceID   string
	ArgFlags     []string
	From         string
	FromFile     string
}

const (
	createExample = `
	# Create an experiement and watch it
	%[1]s create -f my-experiment.yaml -w`

	createAnalysisRunExample = `
  	# Create an AnalysisRun from a local template file
  	%[1]s create analysisrun --from-file my-analysis-template.yaml
  
  	# Create an AnalysisRun from a template in the cluster
  	%[1]s create analysisrun --from my-analysis-template`
)

// NewCmdCreate returns a new instance of an `rollouts create` command
func NewCmdCreate(o *options.ArgoRolloutsOptions) *cobra.Command {
	createOptions := CreateOptions{
		ArgoRolloutsOptions: *o,
	}
	var cmd = &cobra.Command{
		Use:          "create",
		Short:        "Create a Rollout, Experiment, AnalysisTemplate, or AnalysisRun resource",
		Long:         "This command creates a new Rollout, Experiment, AnalysisTemplate, or AnalysisRun resource from a file.",
		Example:      o.Example(createExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(createOptions.Files) == 0 {
				return o.UsageErr(c)
			}
			if len(createOptions.Files) > 1 && createOptions.Watch {
				return errors.New("Cannot watch multiple resources")
			}

			var objs []runtime.Object
			for _, f := range createOptions.Files {
				obj, err := createOptions.createResource(f)
				if err != nil {
					return err
				}
				objs = append(objs, obj)
			}
			if createOptions.Watch {
				switch obj := objs[0].(type) {
				case *v1alpha1.Rollout:
					getCmd := get.NewCmdGetRollout(o)
					getCmd.SetArgs([]string{obj.Name, "--watch"})
					return getCmd.Execute()
				case *v1alpha1.Experiment:
					getCmd := get.NewCmdGetExperiment(o)
					getCmd.SetArgs([]string{obj.Name, "--watch"})
					return getCmd.Execute()
				default:
					return errors.New("Can only watch resources of type Rollout or Experiment")
				}
			}
			return nil
		},
	}
	cmd.AddCommand(NewCmdCreateAnalysisRun(o))
	cmd.Flags().StringArrayVarP(&createOptions.Files, "filename", "f", []string{}, "Files to use to create the resource")
	cmd.Flags().BoolVarP(&createOptions.Watch, "watch", "w", false, "Watch live updates to the resource after creating")
	cmd.Flags().BoolVar(&createOptions.NoColor, "no-color", false, "Do not colorize output")
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
	} else {
		return yaml.UnmarshalStrict(fileBytes, &obj, yaml.DisallowUnknownFields)
	}
}

func (c *CreateOptions) createResource(path string) (runtime.Object, error) {
	fileBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var un unstructured.Unstructured
	err = unmarshal(fileBytes, &un)
	if err != nil {
		return nil, err
	}
	gvk := un.GroupVersionKind()
	ns := c.ArgoRolloutsOptions.Namespace()
	switch {
	case gvk.Group == rollouts.Group && gvk.Kind == rollouts.ExperimentKind:
		var exp v1alpha1.Experiment
		err = unmarshal(fileBytes, &exp)
		if err != nil {
			return nil, err
		}
		obj, err := c.RolloutsClientset().ArgoprojV1alpha1().Experiments(ns).Create(&exp)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(c.Out, "%s.%s/%s created\n", rollouts.ExperimentSingular, rollouts.Group, obj.Name)
		return obj, nil
	case gvk.Group == rollouts.Group && gvk.Kind == rollouts.RolloutKind:
		var ro v1alpha1.Rollout
		err = unmarshal(fileBytes, &ro)
		if err != nil {
			return nil, err
		}
		obj, err := c.RolloutsClientset().ArgoprojV1alpha1().Rollouts(ns).Create(&ro)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(c.Out, "%s.%s/%s created\n", rollouts.RolloutSingular, rollouts.Group, obj.Name)
		return obj, nil
	case gvk.Group == rollouts.Group && gvk.Kind == rollouts.AnalysisTemplateKind:
		var template v1alpha1.AnalysisTemplate
		err = unmarshal(fileBytes, &template)
		if err != nil {
			return nil, err
		}
		obj, err := c.RolloutsClientset().ArgoprojV1alpha1().AnalysisTemplates(ns).Create(&template)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(c.Out, "%s.%s/%s created\n", rollouts.AnalysisTemplateSingular, rollouts.Group, obj.Name)
		return obj, nil
	case gvk.Group == rollouts.Group && gvk.Kind == rollouts.AnalysisRunKind:
		var run v1alpha1.AnalysisRun
		err = unmarshal(fileBytes, &run)
		if err != nil {
			return nil, err
		}
		obj, err := c.RolloutsClientset().ArgoprojV1alpha1().AnalysisRuns(ns).Create(&run)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(c.Out, "%s.%s/%s created\n", rollouts.AnalysisRunSingular, rollouts.Group, obj.Name)
		return obj, nil
	default:
		return nil, fmt.Errorf("creates of %s/%s unsupported", gvk.Group, gvk.Kind)
	}
}

// NewCmdCreateAnalysisRun returns a new instance of an `rollouts create analysisrun` command
func NewCmdCreateAnalysisRun(o *options.ArgoRolloutsOptions) *cobra.Command {
	createOptions := CreateAnalysisRunOptions{
		ArgoRolloutsOptions: *o,
	}
	var cmd = &cobra.Command{
		Use:          "analysisrun",
		Aliases:      []string{"ar"},
		Short:        "Create an AnalysisRun from a template",
		Long:         "This command creates a new AnalysisRun from an existing AnalysisTemplate resources or from an AnalysisTemplate file.",
		Example:      o.Example(createAnalysisRunExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			froms := 0
			if createOptions.From != "" {
				froms++
			}
			if createOptions.FromFile != "" {
				froms++
			}
			if froms != 1 {
				return fmt.Errorf("one of --from or --from-file must be specified")
			}
			templateArgs, err := createOptions.ParseArgFlags()
			if err != nil {
				return err
			}
			template, err := createOptions.getAnalysisTemplate()
			if err != nil {
				return err
			}
			var name, generateName string
			if createOptions.Name != "" {
				name = createOptions.Name
			} else if createOptions.GenerateName != "" {
				generateName = createOptions.GenerateName
			} else {
				generateName = template.Name + "-"
			}

			ns := o.Namespace()
			run, err := analysisutil.NewAnalysisRunFromTemplate(template, templateArgs, name, generateName, ns)
			if err != nil {
				return err
			}
			if createOptions.InstanceID != "" {
				run.Labels = map[string]string{
					v1alpha1.LabelKeyControllerInstanceID: createOptions.InstanceID,
				}
			}
			created, err := createOptions.RolloutsClientset().ArgoprojV1alpha1().AnalysisRuns(ns).Create(run)
			if err != nil {
				return err
			}
			fmt.Fprintf(createOptions.Out, "analysisrun.argoproj.io/%s created\n", created.Name)
			return nil
		},
	}
	cmd.Flags().StringVar(&createOptions.Name, "name", "", "Use the specified name for the run")
	cmd.Flags().StringVar(&createOptions.GenerateName, "generate-name", "", "Use the specified generateName for the run")
	cmd.Flags().StringVar(&createOptions.InstanceID, "instance-id", "", "Instance-ID for the AnalysisRun")
	cmd.Flags().StringArrayVarP(&createOptions.ArgFlags, "argument", "a", []string{}, "Arguments to the parameter template")
	cmd.Flags().StringVar(&createOptions.From, "from", "", "Create an AnalysisRun from an AnalysisTemplate in the cluster")
	cmd.Flags().StringVar(&createOptions.FromFile, "from-file", "", "Create an AnalysisRun from an AnalysisTemplate in a local file")
	return cmd
}

func (c *CreateAnalysisRunOptions) getAnalysisTemplate() (*v1alpha1.AnalysisTemplate, error) {
	if c.From != "" {
		return c.RolloutsClientset().ArgoprojV1alpha1().AnalysisTemplates(c.Namespace()).Get(c.From, metav1.GetOptions{})
	} else {
		fileBytes, err := ioutil.ReadFile(c.FromFile)
		if err != nil {
			return nil, err
		}
		var tmpl v1alpha1.AnalysisTemplate
		err = unmarshal(fileBytes, &tmpl)
		if err != nil {
			return nil, err
		}
		return &tmpl, nil
	}
}

func (c *CreateAnalysisRunOptions) ParseArgFlags() ([]v1alpha1.Argument, error) {
	var args []v1alpha1.Argument
	for _, argFlag := range c.ArgFlags {
		argSplit := strings.SplitN(argFlag, "=", 2)
		if len(argSplit) != 2 {
			return nil, errors.New("arguments must be in the form NAME=VALUE")
		}
		arg := v1alpha1.Argument{
			Name:  argSplit[0],
			Value: pointer.StringPtr(argSplit[1]),
		}
		args = append(args, arg)
	}
	return args, nil
}
