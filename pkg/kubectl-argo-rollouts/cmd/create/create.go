package create

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/yaml"

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
	Global   bool
}

type CreateAnalysisRunOptions struct {
	options.ArgoRolloutsOptions

	Name         string
	GenerateName string
	InstanceID   string
	ArgFlags     []string
	From         string
	FromFile     string
	Global       bool
}

const (
	createExample = `
	# Create an experiment and watch it
	%[1]s create -f my-experiment.yaml -w`

	createAnalysisRunExample = `
  	# Create an AnalysisRun from a local AnalysisTemplate file
  	%[1]s create analysisrun --from-file my-analysis-template.yaml

  	# Create an AnalysisRun from a AnalysisTemplate in the cluster
  	%[1]s create analysisrun --from my-analysis-template

  	# Create an AnalysisRun from a local ClusterAnalysisTemplate file
  	%[1]s create analysisrun --global --from my-analysis-cluster-template.yaml

  	# Create an AnalysisRun from a ClusterAnalysisTemplate in the cluster
  	%[1]s create analysisrun --global --from my-analysis-cluster-template`
)

// NewCmdCreate returns a new instance of an `rollouts create` command
func NewCmdCreate(o *options.ArgoRolloutsOptions) *cobra.Command {
	createOptions := CreateOptions{
		ArgoRolloutsOptions: *o,
	}
	var cmd = &cobra.Command{
		Use:          "create",
		Short:        "Create a Rollout, Experiment, AnalysisTemplate, ClusterAnalysisTemplate, or AnalysisRun resource",
		Long:         "This command creates a new Rollout, Experiment, AnalysisTemplate, ClusterAnalysisTemplate, or AnalysisRun resource from a file.",
		Example:      o.Example(createExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			createOptions.DynamicClientset()
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

func (c *CreateOptions) getNamespace(un unstructured.Unstructured) string {
	ns := c.ArgoRolloutsOptions.Namespace()
	if md, ok := un.Object["metadata"]; ok {
		if md == nil {
			return ns
		}
		metadata := md.(map[string]interface{})
		if internalns, ok := metadata["namespace"]; ok {
			ns = internalns.(string)
		}
	}
	return ns
}

func (c *CreateOptions) createResource(path string) (runtime.Object, error) {
	ctx := context.TODO()
	fileBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var un unstructured.Unstructured
	err = unmarshal(fileBytes, &un)
	if err != nil {
		return nil, err
	}
	gvk := un.GroupVersionKind()
	ns := c.getNamespace(un)
	switch {
	case gvk.Group == rollouts.Group && gvk.Kind == rollouts.ExperimentKind:
		var exp v1alpha1.Experiment
		err = unmarshal(fileBytes, &exp)
		if err != nil {
			return nil, err
		}
		obj, err := c.DynamicClient.Resource(v1alpha1.ExperimentGVR).Namespace(ns).Create(ctx, &un, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(c.Out, "%s.%s/%s created\n", rollouts.ExperimentSingular, rollouts.Group, obj.GetName())
		return obj, nil
	case gvk.Group == rollouts.Group && gvk.Kind == rollouts.RolloutKind:
		var ro v1alpha1.Rollout
		err = unmarshal(fileBytes, &ro)
		if err != nil {
			return nil, err
		}
		obj, err := c.DynamicClient.Resource(v1alpha1.RolloutGVR).Namespace(ns).Create(ctx, &un, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(c.Out, "%s.%s/%s created\n", rollouts.RolloutSingular, rollouts.Group, obj.GetName())
		return obj, nil
	case gvk.Group == rollouts.Group && gvk.Kind == rollouts.AnalysisTemplateKind:
		var template v1alpha1.AnalysisTemplate
		err = unmarshal(fileBytes, &template)
		if err != nil {
			return nil, err
		}
		obj, err := c.DynamicClient.Resource(v1alpha1.AnalysisTemplateGVR).Namespace(ns).Create(ctx, &un, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(c.Out, "%s.%s/%s created\n", rollouts.AnalysisTemplateSingular, rollouts.Group, obj.GetName())
		return obj, nil
	case gvk.Group == rollouts.Group && gvk.Kind == rollouts.ClusterAnalysisTemplateKind:
		var template v1alpha1.ClusterAnalysisTemplate
		err = unmarshal(fileBytes, &template)
		if err != nil {
			return nil, err
		}
		obj, err := c.DynamicClient.Resource(v1alpha1.ClusterAnalysisTemplateGVR).Namespace(ns).Create(ctx, &un, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(c.Out, "%s.%s/%s created\n", rollouts.AnalysisTemplateSingular, rollouts.Group, obj.GetName())
		return obj, nil
	case gvk.Group == rollouts.Group && gvk.Kind == rollouts.AnalysisRunKind:
		var run v1alpha1.AnalysisRun
		err = unmarshal(fileBytes, &run)
		if err != nil {
			return nil, err
		}
		obj, err := c.DynamicClient.Resource(v1alpha1.AnalysisRunGVR).Namespace(ns).Create(ctx, &un, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(c.Out, "%s.%s/%s created\n", rollouts.AnalysisRunSingular, rollouts.Group, obj.GetName())
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
		Short:        "Create an AnalysisRun from an AnalysisTemplate or a ClusterAnalysisTemplate",
		Long:         "This command creates a new AnalysisRun from an existing AnalysisTemplate resources or from an AnalysisTemplate file.",
		Example:      o.Example(createAnalysisRunExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			createOptions.DynamicClientset()
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
			var templateName string
			var obj *unstructured.Unstructured

			if createOptions.Global {
				obj, err = createOptions.getClusterAnalysisTemplate()
				if err != nil {
					return err
				}
			} else {
				obj, err = createOptions.getAnalysisTemplate()
				if err != nil {
					return err
				}
			}

			objName, found, err := unstructured.NestedString(obj.Object, "metadata", "name")
			if err != nil {
				return err
			}
			if found {
				templateName = objName
			}

			var name, generateName string
			if createOptions.Name != "" {
				name = createOptions.Name
			} else if createOptions.GenerateName != "" {
				generateName = createOptions.GenerateName
			} else {
				generateName = templateName + "-"
			}
			ns := o.Namespace()

			if name == "" && generateName == "-" {
				return fmt.Errorf("name is invalid")
			}

			obj, err = analysisutil.NewAnalysisRunFromUnstructured(obj, templateArgs, name, generateName, ns)
			if err != nil {
				return err
			}
			if createOptions.InstanceID != "" {
				labels := map[string]string{
					v1alpha1.LabelKeyControllerInstanceID: createOptions.InstanceID,
				}
				err = unstructured.SetNestedStringMap(obj.Object, labels, "metadata", "labels")
				if err != nil {
					return err
				}
			}
			obj, err = createOptions.DynamicClient.Resource(v1alpha1.AnalysisRunGVR).Namespace(ns).Create(ctx, obj, metav1.CreateOptions{})
			if err != nil {
				return err
			}
			fmt.Fprintf(createOptions.Out, "analysisrun.argoproj.io/%s created\n", obj.GetName())
			return nil
		},
	}
	cmd.Flags().StringVar(&createOptions.Name, "name", "", "Use the specified name for the run")
	cmd.Flags().StringVar(&createOptions.GenerateName, "generate-name", "", "Use the specified generateName for the run")
	cmd.Flags().StringVar(&createOptions.InstanceID, "instance-id", "", "Instance-ID for the AnalysisRun")
	cmd.Flags().StringArrayVarP(&createOptions.ArgFlags, "argument", "a", []string{}, "Arguments to the parameter template")
	cmd.Flags().StringVar(&createOptions.From, "from", "", "Create an AnalysisRun from an AnalysisTemplate or ClusterAnalysisTemplate in the cluster")
	cmd.Flags().StringVar(&createOptions.FromFile, "from-file", "", "Create an AnalysisRun from an AnalysisTemplate or ClusterAnalysisTemplate in a local file")
	cmd.Flags().BoolVar(&createOptions.Global, "global", false, "Use a ClusterAnalysisTemplate instead of a AnalysisTemplate")
	return cmd
}

func (c *CreateAnalysisRunOptions) getAnalysisTemplate() (*unstructured.Unstructured, error) {
	ctx := context.TODO()
	if c.From != "" {
		return c.DynamicClient.Resource(v1alpha1.AnalysisTemplateGVR).Namespace(c.Namespace()).Get(ctx, c.From, metav1.GetOptions{})
	} else {
		fileBytes, err := os.ReadFile(c.FromFile)
		if err != nil {
			return nil, err
		}
		var un unstructured.Unstructured
		err = unmarshal(fileBytes, &un)
		if err != nil {
			return nil, err
		}
		return &un, nil
	}
}

func (c *CreateAnalysisRunOptions) getClusterAnalysisTemplate() (*unstructured.Unstructured, error) {
	ctx := context.TODO()
	if c.From != "" {
		return c.DynamicClient.Resource(v1alpha1.ClusterAnalysisTemplateGVR).Get(ctx, c.From, metav1.GetOptions{})
	} else {
		fileBytes, err := os.ReadFile(c.FromFile)
		if err != nil {
			return nil, err
		}
		var un unstructured.Unstructured
		err = unmarshal(fileBytes, &un)
		if err != nil {
			return nil, err
		}
		return &un, nil
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
