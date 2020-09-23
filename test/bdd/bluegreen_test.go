package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/promote"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/set"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	argoexec "github.com/argoproj/pkg/exec"
)

type FunctionalTestContext struct {
	RolloutName string
	FilePath    string
	Streams     genericclioptions.IOStreams
	Options     *options.ArgoRolloutsOptions
}

func (c *FunctionalTestContext) iApplyManifest(fileName string) error {
	folder := "../../examples"
	filePath := folder + "/" + fileName
	c.FilePath = filePath
	_, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "apply", "-f", filePath)
	if err != nil {
		return err
	}

	numAttempts := 4
	isSuccess, err := retry(numAttempts, func() (bool, error) {
		rolloutExists, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", c.RolloutName)
		return rolloutExists != "", err
	})
	if err != nil {
		return err
	}
	if !isSuccess {
		return fmt.Errorf("Unable to apply manifest %s. Rollout not created", fileName)
	}

	isSuccess, err = retry(numAttempts, func() (bool, error) {
		jsonPath := createJsonPath(".spec.replicas")
		desiredReplicas, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", c.RolloutName, "-o", jsonPath)
		if desiredReplicas == "" || err != nil {
			return false, err
		}

		jsonPath = createJsonPath(".status.availableReplicas")
		availableReplicas, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", c.RolloutName, "-o", jsonPath)
		if availableReplicas == "" || err != nil {
			return false, err
		}

		numDesiredReplicas, err := strconv.Atoi(desiredReplicas)
		if err != nil {
			return false, err
		}
		numAvailableReplicas, err := strconv.Atoi(availableReplicas)
		if err != nil {
			return false, err
		}

		return numAvailableReplicas == numDesiredReplicas, err
	})
	if err != nil {
		return err
	}
	if !isSuccess {
		return fmt.Errorf("Replicas not available")
	}

	return nil
}

func (c *FunctionalTestContext) iChangeTheImageTo(image string) error {
	jsonPath := createJsonPath(".spec.template.spec.containers[0].name")
	containerName, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", c.RolloutName, "-o", jsonPath)
	if err != nil {
		return err
	}

	jsonPath = createJsonPath(".spec.template.spec.containers[0].image")
	currentImage, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", c.RolloutName, "-o", jsonPath)
	if err != nil {
		return err
	}
	i := strings.Split(currentImage, ":")
	newImage := i[0] + ":" + image

	cmdSetImage := set.NewCmdSetImage(c.Options)
	cmdSetImage.PersistentPreRunE = c.Options.PersistentPreRunE
	cmdSetImage.SetArgs([]string{c.RolloutName, containerName + "=" + newImage})
	err = cmdSetImage.Execute()
	return err
}

func (c *FunctionalTestContext) promoteTheRollout() error {
	jsonPath := createJsonPath(".status.pauseConditions")
	numAttempts := 4
	isSuccess, err := retry(numAttempts, func() (bool, error) {
		pauseCond, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", c.RolloutName, "-o", jsonPath)
		return pauseCond != "", err
	})
	if err != nil {
		return err
	}
	if !isSuccess {
		return fmt.Errorf("Unable to promote rollout %s. Rollout did not enter paused state.", c.RolloutName)
	}

	cmdPromote := promote.NewCmdPromote(c.Options)
	cmdPromote.SetArgs([]string{c.RolloutName})
	err = cmdPromote.Execute()
	return err
}

func (c *FunctionalTestContext) theActiveServiceShouldRouteTrafficToNewVersionsReplicaSet() error {
	jsonPath := createJsonPath(".spec.strategy.blueGreen.activeService")
	svcName, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", c.RolloutName, "-o", jsonPath)
	if err != nil {
		return err
	}
	return c.theServiceShouldRouteTrafficToNewVersionsReplicaSet(svcName)
}

func (c *FunctionalTestContext) theServiceShouldRouteTrafficToNewVersionsReplicaSet(svcName string) error {
	numAttempts := 3
	isSuccess, err := retry(numAttempts, func() (bool, error) {
		jsonPath := createJsonPath(".spec.selector.rollouts-pod-template-hash")
		svcInjection, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "service", svcName, "-o", jsonPath)
		if err != nil {
			return false, err
		}

		jsonPath = createJsonPath(".status.currentPodHash")
		currentPodHash, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", c.RolloutName, "-o", jsonPath)
		if err != nil {
			return false, err
		}
		return strings.Contains(svcInjection, currentPodHash), nil
	})
	if err != nil {
		return err
	}
	if !isSuccess {
		return fmt.Errorf("Service %s does not contain Rollout %s's current pod hash. Injection failed.", svcName, c.RolloutName)
	}

	return nil
}

func retry(numAttempts int, cond func() (bool, error)) (bool, error) {
	for numAttempts > 0 {
		isSuccess, err := cond()
		if err != nil {
			return false, err
		}
		if isSuccess {
			return isSuccess, nil
		}
		time.Sleep(30 * time.Second)
		numAttempts -= 1
	}
	return false, nil
}

func createJsonPath(field string) string {
	return "jsonpath={" + field + "}"
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	cctx := FunctionalTestContext{}
	ctx.BeforeScenario(func(*godog.Scenario) {
		cctx.RolloutName = "rollout-bluegreen"
		cctx.Streams = genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
		cctx.Options = options.NewArgoRolloutsOptions(cctx.Streams)
	})

	ctx.Step(`^I apply manifest "([^"]*)"$`, cctx.iApplyManifest)
	ctx.Step(`^I change the image to "([^"]*)"$`, cctx.iChangeTheImageTo)
	ctx.Step(`^promote the rollout$`, cctx.promoteTheRollout)
	ctx.Step(`^the active service should route traffic to new version\'s ReplicaSet$`, cctx.theActiveServiceShouldRouteTrafficToNewVersionsReplicaSet)

	ctx.AfterScenario(func(*godog.Scenario, error) {
		argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "delete", "-f", cctx.FilePath)
	})
}
