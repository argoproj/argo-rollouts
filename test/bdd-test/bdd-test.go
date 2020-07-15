package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/promote"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/set"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	argoexec "github.com/argoproj/pkg/exec"
)

func main() {
	rolloutName := "rollout-bluegreen"
	activeServiceName := "rollout-bluegreen-active"
	filePath := "../../examples/rollout-bluegreen.yaml"
	newImage := "rollouts-demo=argoproj/rollouts-demo:green"


	// Create RO and active service
	_, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "apply", "-f", filePath)
	if err != nil {
		panic(err)
	}
	streams := genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
	o := options.NewArgoRolloutsOptions(streams)

	// Change image
	cmdSetImage := set.NewCmdSetImage(o)
	cmdSetImage.PersistentPreRunE = o.PersistentPreRunE
	cmdSetImage.SetArgs([]string{rolloutName, newImage})
	err = cmdSetImage.Execute()
	if err != nil {
		panic(err)
	}

	currentPodHash, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", rolloutName, "-o", "jsonpath={.status.currentPodHash}")
	if err != nil {
		panic(err)
	}

	activeRSName := fmt.Sprint(rolloutName, "-7d6b6cb796")
	numReplicas := getNumReplicas(activeRSName)

	numAttempts := 4
	for i := 0; i < numAttempts; i ++ {
		numReadyReplicas := getNumReadyReplicas(activeRSName)
		if numReadyReplicas < numReplicas {
			time.Sleep(30 * time.Second)
		} else {
			break
		}
	}

	numReadyReplicas := getNumReadyReplicas(activeRSName)
	if numReadyReplicas < numReplicas {
		err = fmt.Errorf("Pods not available")
		panic(err)
	}

	// Promote rollout
	cmdPromote := promote.NewCmdPromote(o)
	cmdPromote.SetArgs([]string{rolloutName})
	err = cmdPromote.Execute()
	if err != nil {
		panic(err)
	}

	// Check if active service selector contains rollout-pod-template-hash
	svcInjection, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "service", activeServiceName, "-o", "jsonpath='{.spec.selector.rollouts-pod-template-hash}'")
	if err != nil {
		panic(err)
	}

	if strings.Contains(svcInjection, currentPodHash) {
		fmt.Println("BlueGreen Test was Successful!")
	} else {
		fmt.Println("Injection failed")
	}
}

func getNumReplicas(replicaSetName string) int {
	i, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "replicaset", replicaSetName, "-o", "jsonpath={.spec.replicas}")
	if err != nil {
		panic(err)
	}
	numReadyReplicas, err := strconv.Atoi(i)
	return numReadyReplicas
}

func getNumReadyReplicas(replicaSetName string) int {
	i, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "replicaset", replicaSetName, "-o", "jsonpath={.status.readyReplicas}")
	if err != nil {
		panic(err)
	}
	numReadyReplicas, err := strconv.Atoi(i)
	return numReadyReplicas
}
