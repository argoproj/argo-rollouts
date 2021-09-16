package main

import (
	"os"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/klog/v2"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

func main() {
	klog.InitFlags(nil)
	streams := genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	o := options.NewArgoRolloutsOptions(streams)
	root := cmd.NewCmdArgoRollouts(o)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
