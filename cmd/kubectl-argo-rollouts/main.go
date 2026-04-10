package main

import (
	"flag"
	"os"

	log "github.com/sirupsen/logrus"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	"k8s.io/klog/v2"

	logutil "github.com/argoproj/argo-rollouts/utils/log"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

func main() {
	klog.InitFlags(nil)
	// Opt into fixed stderrthreshold behavior (kubernetes/klog#212).
	_ = flag.Set("legacy_stderr_threshold_behavior", "false")
	_ = flag.Set("stderrthreshold", "INFO")
	logutil.SetKLogLogger(log.New())
	streams := genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	o := options.NewArgoRolloutsOptions(streams)
	root := cmd.NewCmdArgoRollouts(o)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
