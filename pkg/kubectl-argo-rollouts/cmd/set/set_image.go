package set

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	completionutil "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/util/completion"
)

const (
	setImageExample = `
  # Set rollout image
  %[1]s set image my-rollout www=image:v2`
)

const (
	maxAttempts = 5
)

// NewCmdSetImage returns a new instance of an `rollouts set image` command
func NewCmdSetImage(o *options.ArgoRolloutsOptions) *cobra.Command {
	var cmd = &cobra.Command{
		Use:          "image ROLLOUT_NAME CONTAINER=IMAGE",
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

			var un *unstructured.Unstructured
			var err error
			for attempt := 0; attempt < maxAttempts; attempt++ {
				un, err = SetImage(o.DynamicClientset(), o.Namespace(), rollout, container, image)
				if err != nil {
					if k8serr.IsConflict(err) && attempt < maxAttempts {
						continue
					}
					return err
				}
				break
			}
			fmt.Fprintf(o.Out, "%s \"%s\" image updated\n", strings.ToLower(un.GetKind()), un.GetName())
			return nil
		},
		ValidArgsFunction: completionutil.RolloutNameCompletionFunc(o),
	}
	return cmd
}

var deploymentGVR = schema.GroupVersionResource{
	Group:    "apps",
	Version:  "v1",
	Resource: "deployments",
}

// SetImage updates a rollout's container image
// We use a dynamic clientset instead of a rollout clientset in order to allow an older plugin
// to still work with a newer version of Rollouts (without dropping newly introduced fields during
// the marshalling)
func SetImage(dynamicClient dynamic.Interface, namespace, rollout, container, image string) (*unstructured.Unstructured, error) {
	ctx := context.TODO()
	rolloutIf := dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace(namespace)
	ro, err := rolloutIf.Get(ctx, rollout, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	workloadRef, ok, err := unstructured.NestedMap(ro.Object, "spec", "workloadRef")
	if err != nil {
		return nil, err
	}
	if ok {
		deployIf := dynamicClient.Resource(deploymentGVR).Namespace(namespace)
		deployName, ok := workloadRef["name"].(string)
		if !ok {
			return nil, fmt.Errorf("spec.workloadRef.name is not a string: %v", workloadRef["name"])
		}
		deployUn, err := deployIf.Get(ctx, deployName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		newDeploy, err := newRolloutSetImage(deployUn, container, image)
		if err != nil {
			return nil, err
		}
		return deployIf.Update(ctx, newDeploy, metav1.UpdateOptions{})
	} else {
		newRo, err := newRolloutSetImage(ro, container, image)
		if err != nil {
			return nil, err
		}
		return rolloutIf.Update(ctx, newRo, metav1.UpdateOptions{})
	}
}

func newRolloutSetImage(orig *unstructured.Unstructured, container string, image string) (*unstructured.Unstructured, error) {
	ro := orig.DeepCopy()
	containerFound := false

	fields := []string{"spec", "template", "spec"}
	for _, field := range []string{"initContainers", "containers", "ephemeralContainers"} {
		ctrListIf, ok, err := unstructured.NestedFieldNoCopy(ro.Object, append(fields, field)...)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		ctrList := ctrListIf.([]interface{})
		for _, ctrIf := range ctrList {
			ctr := ctrIf.(map[string]interface{})
			if name, _, _ := unstructured.NestedString(ctr, "name"); name == container || container == "*" {
				ctr["image"] = image
				containerFound = true
			}
		}
	}
	if !containerFound {
		return nil, fmt.Errorf("unable to find container named \"%s\"", container)
	}
	return ro, nil
}
