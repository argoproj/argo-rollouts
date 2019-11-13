package set

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/typed/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

const (
	setImageExample = `
  # Set rollout image
  %[1]s set image my-rollout www=image:v2
`
)

const (
	maxAttempts = 5
)

// NewCmdSetImage returns a new instance of an `rollouts set image` command
func NewCmdSetImage(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "image ROLLOUT CONTAINER=IMAGE",
		Short:        "Update the image of a rollout",
		Example:      o.Example(setImageExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 2 {
				return o.UsageErr(c)
			}
			rollout := args[0]
			imageSplit := strings.Split(args[1], "=")
			if len(imageSplit) != 2 {
				return o.UsageErr(c)
			}
			container := imageSplit[0]
			image := imageSplit[1]

			rolloutIf := o.RolloutsClientset().ArgoprojV1alpha1().Rollouts(o.Namespace())
			for attempt := 0; attempt < maxAttempts; attempt++ {
				err := setImage(rolloutIf, rollout, container, image)
				if err != nil {
					if k8serr.IsConflict(err) && attempt < maxAttempts {
						continue
					}
					return err
				}
				break
			}
			fmt.Fprintf(o.Out, "rollout \"%s\" image updated\n", rollout)
			return nil
		},
	}
	o.AddKubectlFlags(cmd)
	return cmd
}

func setImage(rolloutIf rolloutclient.RolloutInterface, rollout string, container string, image string) error {
	ro, err := rolloutIf.Get(rollout, metav1.GetOptions{})
	if err != nil {
		return err
	}
	newRo, err := newRolloutSetImage(ro, container, image)
	if err != nil {
		return err
	}
	_, err = rolloutIf.Update(newRo)
	if err != nil {
		return err
	}
	return nil
}

func newRolloutSetImage(orig *v1alpha1.Rollout, container string, image string) (*v1alpha1.Rollout, error) {
	ro := orig.DeepCopy()
	containerFound := false
	for i, ctr := range ro.Spec.Template.Spec.InitContainers {
		if ctr.Name == container || container == "*" {
			containerFound = true
			ctr.Image = image
			ro.Spec.Template.Spec.InitContainers[i] = ctr
		}
	}
	for i, ctr := range ro.Spec.Template.Spec.Containers {
		if ctr.Name == container || container == "*" {
			containerFound = true
			ctr.Image = image
			ro.Spec.Template.Spec.Containers[i] = ctr
		}
	}
	for i, ctr := range ro.Spec.Template.Spec.EphemeralContainers {
		if ctr.Name == container || container == "*" {
			containerFound = true
			ctr.Image = image
			ro.Spec.Template.Spec.EphemeralContainers[i] = ctr
		}
	}
	if !containerFound {
		return nil, fmt.Errorf("unable to find container named \"%s\"", container)
	}
	return ro, nil
}
