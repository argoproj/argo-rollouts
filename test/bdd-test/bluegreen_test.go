package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	argoexec "github.com/argoproj/pkg/exec"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/promote"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/set"
)

type FunctionalTestContext struct {
	RolloutName string
	Streams genericclioptions.IOStreams
	Options *options.ArgoRolloutsOptions
}

func (c *FunctionalTestContext) iApplyManifest(fileName string) error {
	folder := "../../examples"
	filePath := folder + "/" + fileName
	_, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "apply", "-f", filePath)
	if err != nil {
		return err
	}

	numAttempts := 4
	rolloutExists, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", c.RolloutName)
	for numAttempts > 0 && rolloutExists == "" {
		time.Sleep(30 * time.Second)
		rolloutExists, err = argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", c.RolloutName)
		numAttempts -= 1
	}
	if rolloutExists == "" {
		return fmt.Errorf("Rollout not created")
	}

	desiredReplicas, err := getFieldFromObject(c.RolloutName, "rollout", ".spec.replicas")
	if err != nil {
		return err
	}
	availableReplicas, err := getFieldFromObject(c.RolloutName, "rollout", ".status.availableReplicas")
	if err != nil {
		return err
	}

	numAttempts = 4
	for numAttempts > 0 && availableReplicas < desiredReplicas {
		time.Sleep(30 * time.Second)
		availableReplicas, err = getFieldFromObject(c.RolloutName, "rollout", ".status.availableReplicas")
		if err != nil {
			return err
		}
		numAttempts -= 1
	}

	if availableReplicas < desiredReplicas {
		return fmt.Errorf("Pods not ready")
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
	// Wait before promotion (for pause)
	// wait() -> anonymous function
	jsonPath := createJsonPath(".status.pauseConditions")
	numAttempts := 4
	pauseCond, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", c.RolloutName, "-o", jsonPath)
	if err != nil {
		return err
	}
	for numAttempts > 0 && pauseCond == "" {
		time.Sleep(30 * time.Second)
		pauseCond, err = argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", c.RolloutName, "-o", jsonPath)
		if err != nil {
			return err
		}
		numAttempts -= 1
	}
	if pauseCond == "" {
		return fmt.Errorf("")
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
	// TODO: Incorporate wait time
	jsonPath := createJsonPath(".spec.selector.rollouts-pod-template-hash")
	svcInjection, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "service", svcName, "-o", jsonPath)
	if err != nil {
		return err
	}

	jsonPath = createJsonPath(".status.currentPodHash")
	currentPodHash, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", "rollout", c.RolloutName, "-o", jsonPath)
	if !strings.Contains(svcInjection, currentPodHash) {
		return fmt.Errorf("Injection failed")
	}

	return nil
}

func getFieldFromObject(objName string, objType string, field string) (int, error) {
	jsonPath := createJsonPath(field)
	i, err := argoexec.RunCommand("kubectl", argoexec.CmdOpts{}, "get", objType, objName, "-o", jsonPath)
	if i == "" || err != nil {
		return 0, err
	}
	retVal, err := strconv.Atoi(i)
	return retVal, err
}

func createJsonPath(field string) string {
	return "jsonpath={" + field + "}"
}

func InitializeTestSuite(ctx *godog.TestSuiteContext) {
	ctx.BeforeSuite(func(){})
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	cctx := FunctionalTestContext{}
	ctx.BeforeScenario(func(*godog.Scenario) {
		cctx.RolloutName =  "rollout-bluegreen"
		cctx.Streams = genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}
		cctx.Options = options.NewArgoRolloutsOptions(cctx.Streams)
	})

	ctx.Step(`^I apply manifest "([^"]*)"$`, cctx.iApplyManifest)
	ctx.Step(`^I change the image to "([^"]*)"$`, cctx.iChangeTheImageTo)
	ctx.Step(`^promote the rollout$`, cctx.promoteTheRollout)
	ctx.Step(`^the active service should route traffic to new version\'s ReplicaSet$`, cctx.theActiveServiceShouldRouteTrafficToNewVersionsReplicaSet)}