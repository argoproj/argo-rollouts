package options

import (
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	roclientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
)

const (
	cliName = "kubectl argo rollouts"
)

// ArgoRolloutsOptions are a set of common CLI flags and convenience functions made available to
// all commands of the kubectl-argo-rollouts plugin
type ArgoRolloutsOptions struct {
	CLIName          string
	RESTClientGetter genericclioptions.RESTClientGetter
	ConfigFlags      *genericclioptions.ConfigFlags
	KlogLevel        int
	LogLevel         string
	RolloutsClient   roclientset.Interface
	KubeClient       kubernetes.Interface
	DynamicClient    dynamic.Interface

	Log *log.Logger
	genericclioptions.IOStreams

	Now func() metav1.Time
}

// NewArgoRolloutsOptions provides an instance of ArgoRolloutsOptions with default values
func NewArgoRolloutsOptions(streams genericclioptions.IOStreams) *ArgoRolloutsOptions {
	logCtx := log.New()
	logCtx.SetOutput(streams.ErrOut)
	klog.SetOutput(streams.ErrOut)
	configFlags := genericclioptions.NewConfigFlags(true)

	return &ArgoRolloutsOptions{
		CLIName:          cliName,
		RESTClientGetter: configFlags,
		ConfigFlags:      configFlags,
		IOStreams:        streams,
		Log:              logCtx,
		LogLevel:         log.InfoLevel.String(),
		Now:              metav1.Now,
	}
}

// Example returns the example string with the CLI command replaced in the example
func (o *ArgoRolloutsOptions) Example(example string) string {
	return strings.Trim(fmt.Sprintf(example, cliName), "\n")
}

// UsageErr is a convenience function to output usage and return an error
func (o *ArgoRolloutsOptions) UsageErr(c *cobra.Command) error {
	c.Usage()
	c.SilenceErrors = true
	return errors.New(c.UsageString())
}

// PersistentPreRunE contains common logic which will be executed for all commands
func (o *ArgoRolloutsOptions) PersistentPreRunE(c *cobra.Command, args []string) error {
	// NOTE: we set the output of the cobra command to stderr because the only thing that should
	// emit to this are returned errors from command.RunE
	c.SetOut(o.ErrOut)
	c.SetErr(o.ErrOut)
	level, err := log.ParseLevel(o.LogLevel)
	if err != nil {
		return err
	}
	o.Log.SetLevel(level)
	if flag.Lookup("v") != nil {
		// the '-v' flag is set by klog.Init(), which we only call in main.go
		err := flag.Set("v", strconv.Itoa(o.KlogLevel))
		if err != nil {
			return err
		}
	}
	return nil
}

// AddKubectlFlags adds kubectl related flags to the command
func (o *ArgoRolloutsOptions) AddKubectlFlags(cmd *cobra.Command) {
	flags := cmd.PersistentFlags()
	o.ConfigFlags.AddFlags(flags)
	flags.IntVarP(&o.KlogLevel, "kloglevel", "v", 0, "Log level for kubernetes client library")
	flags.StringVar(&o.LogLevel, "loglevel", log.InfoLevel.String(), "Log level for kubectl argo rollouts")
}

// RolloutsClientset returns a Rollout client interface based on client flags
func (o *ArgoRolloutsOptions) RolloutsClientset() roclientset.Interface {
	if o.RolloutsClient != nil {
		return o.RolloutsClient
	}
	config, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		panic(err)
	}
	rolloutsClient, err := roclientset.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	o.RolloutsClient = rolloutsClient
	return o.RolloutsClient
}

// KubeClientset returns a Kubernetes client interface based on client flags
func (o *ArgoRolloutsOptions) KubeClientset() kubernetes.Interface {
	if o.KubeClient != nil {
		return o.KubeClient
	}
	config, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		panic(err)
	}
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	o.KubeClient = kubeClient
	return o.KubeClient
}

// DynamicClientset returns a Dynamic client interface based on client flags
func (o *ArgoRolloutsOptions) DynamicClientset() dynamic.Interface {
	if o.DynamicClient != nil {
		return o.DynamicClient
	}
	config, err := o.RESTClientGetter.ToRESTConfig()
	if err != nil {
		panic(err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	o.DynamicClient = dynamicClient
	return o.DynamicClient
}

// Namespace returns the namespace based on client flags or kube context
func (o *ArgoRolloutsOptions) Namespace() string {
	namespace, _, err := o.RESTClientGetter.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		panic(err)
	}
	return namespace
}
