package main

import (
	"fmt"
	"os"
	"strings"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/promote"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/set"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	argoexec "github.com/argoproj/pkg/exec"
)

func main() {
	// Default loading rules
	//var kubeconfig *string
	//if home := homedir.HomeDir(); home != "" {
	//	kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	//} else {
	//	kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	//}
	//flag.Parse()

	//config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	//if err != nil {
	//	panic(err)
	//}
	//clientset, err := kubernetes.NewForConfig(config)
	//if err != nil {
	//	panic(err)
	//}

	// Create file
	argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "apply", "-f", "../../examples/rollout-bluegreen.yaml")
	streams := genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	o := options.NewArgoRolloutsOptions(streams)

	// Change image
	cmdSetImage := set.NewCmdSetImage(o)
	cmdSetImage.PersistentPreRunE = o.PersistentPreRunE
	cmdSetImage.SetArgs([]string{"rollout-bluegreen", "rollouts-demo=argoproj/rollouts-demo:green"})
	err := cmdSetImage.Execute()
	if err != nil {
		panic(err)
	}

	// Promote rollout
	cmdPromote := promote.NewCmdPromote(o)
	cmdPromote.SetArgs([]string{"rollout-bluegreen"})
	err = cmdPromote.Execute()
	if err != nil {
		panic(err)
	}

	// Check if active service selector contains rollout-pod-template-hash
	currentPodHash, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", "rollout-bluegreen", "-o", "jsonpath={.status.currentPodHash}")
	if err != nil {
		panic(err)
	}
	svcInjection, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "service", "rollout-bluegreen-active", "-o", "jsonpath='{.spec.selector.rollouts-pod-template-hash}'")
	if err != nil {
		panic(err)
	}

	if strings.Contains(svcInjection, currentPodHash) {
		fmt.Println("BlueGreen Test was Successful!")
	} else {
		fmt.Println("Injection failed")
	}
}