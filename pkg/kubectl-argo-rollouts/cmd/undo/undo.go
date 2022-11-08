package undo

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	completionutil "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/util/completion"
	routils "github.com/argoproj/argo-rollouts/utils/unstructured"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	appsclient "k8s.io/client-go/kubernetes/typed/apps/v1"
)

const (
	revisionAnnotation = "rollout.argoproj.io/revision"

	undoExample = `
	# Undo a rollout
	%[1]s undo guestbook

	# Undo a rollout revision 3
	%[1]s undo guestbook --to-revision=3`
)

// NewCmdUndo returns a new instance of an `rollouts undo` command
func NewCmdUndo(o *options.ArgoRolloutsOptions) *cobra.Command {
	var (
		toRevision = int64(0)
	)
	var cmd = &cobra.Command{
		Use:          "undo ROLLOUT_NAME",
		Short:        "Undo a rollout",
		Long:         "Rollback to the previous rollout.",
		Example:      o.Example(undoExample),
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) != 1 {
				return o.UsageErr(c)
			}
			name := args[0]
			rolloutIf := o.DynamicClientset().Resource(v1alpha1.RolloutGVR).Namespace(o.Namespace())
			clientset := o.KubeClientset()
			result, err := RunUndoRollout(rolloutIf, clientset, name, toRevision)
			if err != nil {
				return err
			}
			fmt.Fprintf(o.Out, result)
			return nil
		},
		ValidArgsFunction: completionutil.RolloutNameCompletionFunc(o),
	}
	cmd.Flags().Int64Var(&toRevision, "to-revision", toRevision, "The revision to rollback to. Default to 0 (last revision).")
	return cmd
}

// RunUndoRollout performs the execution of 'rollouts undo' sub command
func RunUndoRollout(rolloutIf dynamic.ResourceInterface, c kubernetes.Interface, name string, toRevision int64) (string, error) {
	ctx := context.TODO()
	var err error

	ro, err := rolloutIf.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	rsForRevision, err := rolloutRevision(ro, c, toRevision)
	if err != nil {
		return "", err
	}

	equal, err := equalIgnoreHash(ro, rsForRevision)
	if err != nil {
		return "", err
	}
	if equal {
		return fmt.Sprintf("skipped rollback (current template already matches revision %d)", toRevision), nil
	}

	// remove hash label before patching back into the rollout
	delete(rsForRevision.Spec.Template.Labels, v1alpha1.DefaultRolloutUniqueLabelKey)

	// make patch to restore
	patchType, patch, err := getRolloutPatch(&rsForRevision.Spec.Template, nil)
	if err != nil {
		return "", fmt.Errorf("failed restoring revision %d: %v", toRevision, err)
	}

	// Restore revision depending on whether workload ref is nil
	rollout := routils.ObjectToRollout(ro)
	if rollout == nil {
		return "", fmt.Errorf("Invalid rollout object")
	}
	if rollout.Spec.WorkloadRef != nil {
		err = undoWorkloadRef(c, rollout, patchType, patch)
	} else {
		_, err = rolloutIf.Patch(ctx, name, patchType, patch, metav1.PatchOptions{})
	}
	if err != nil {
		return "", fmt.Errorf("failed restoring revision %d: %v", toRevision, err)
	}
	return fmt.Sprintf("rollout '%s' undo\n", ro.GetName()), nil
}

func undoWorkloadRef(c kubernetes.Interface, rollout *v1alpha1.Rollout, patchType types.PatchType, patch []byte) error {
	var err error

	refName := rollout.Spec.WorkloadRef.Name
	namespace := rollout.GetNamespace()

	switch rollout.Spec.WorkloadRef.Kind {
	case "Deployment":
		_, err = c.AppsV1().Deployments(namespace).Patch(context.TODO(), refName, patchType, patch, metav1.PatchOptions{})
	case "ReplicaSet":
		_, err = c.AppsV1().ReplicaSets(namespace).Patch(context.TODO(), refName, patchType, patch, metav1.PatchOptions{})
	case "PodTemplate":
		_, err = c.CoreV1().PodTemplates(namespace).Patch(context.TODO(), refName, patchType, patch, metav1.PatchOptions{})
	default:
		return fmt.Errorf("workload of type %s is not supported", rollout.Spec.WorkloadRef.Kind)
	}
	return err
}

func rolloutRevision(ro *unstructured.Unstructured, c kubernetes.Interface, toRevision int64) (*appsv1.ReplicaSet, error) {
	allRSs, err := getAllReplicaSets(ro, c.AppsV1())
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve replica sets from rollout %s: %v", ro.GetName(), err)
	}
	var (
		latestReplicaSet   *appsv1.ReplicaSet
		latestRevision     = int64(-1)
		previousReplicaSet *appsv1.ReplicaSet
		previousRevision   = int64(-1)
	)
	for _, rs := range allRSs {
		if v, err := revision(rs); err == nil {
			if toRevision == 0 {
				if latestRevision < v {
					// newest one we've seen so far
					previousRevision = latestRevision
					previousReplicaSet = latestReplicaSet
					latestRevision = v
					latestReplicaSet = rs
				} else if previousRevision < v {
					// second newest one we've seen so far
					previousRevision = v
					previousReplicaSet = rs
				}
			} else if toRevision == v {
				return rs, nil
			}
		}
	}

	if toRevision > 0 {
		return nil, fmt.Errorf("unable to find specified revision %v in history", toRevision)
	}

	if previousReplicaSet == nil {
		return nil, fmt.Errorf("no revision found for rollout %q", ro.GetName())
	}

	return previousReplicaSet, nil
}

func getRolloutPatch(podTemplate *corev1.PodTemplateSpec, annotations map[string]string) (types.PatchType, []byte, error) {
	patch, err := json.Marshal([]interface{}{
		map[string]interface{}{
			"op":    "replace",
			"path":  "/spec/template",
			"value": podTemplate,
		},
	})
	return types.JSONPatchType, patch, err
}

func getAllReplicaSets(ro *unstructured.Unstructured, c appsclient.AppsV1Interface) ([]*appsv1.ReplicaSet, error) {
	rsList, err := listReplicaSets(ro, rsListFromClient(c))
	if err != nil {
		return nil, err
	}
	return rsList, nil
}

func rsListFromClient(c appsclient.AppsV1Interface) rsListFunc {
	return func(namespace string, options metav1.ListOptions) ([]*appsv1.ReplicaSet, error) {
		rsList, err := c.ReplicaSets(namespace).List(context.TODO(), options)
		if err != nil {
			return nil, err
		}
		var ret []*appsv1.ReplicaSet
		for i := range rsList.Items {
			ret = append(ret, &rsList.Items[i])
		}
		return ret, err
	}
}

type rsListFunc func(string, metav1.ListOptions) ([]*appsv1.ReplicaSet, error)

func listReplicaSets(ro *unstructured.Unstructured, getRSList rsListFunc) ([]*appsv1.ReplicaSet, error) {
	namespace := ro.GetNamespace()
	labelSelector, err := extractLabelSelector(ro.Object)
	if err != nil {
		return nil, err
	}
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, err
	}
	options := metav1.ListOptions{LabelSelector: selector.String()}
	all, err := getRSList(namespace, options)
	if err != nil {
		return nil, err
	}
	// Only include those whose ControllerRef matches the rollout.
	owned := make([]*appsv1.ReplicaSet, 0, len(all))
	for _, rs := range all {
		if metav1.IsControlledBy(rs, ro) {
			owned = append(owned, rs)
		}
	}
	return owned, nil
}

func extractLabelSelector(v map[string]interface{}) (*metav1.LabelSelector, error) {
	labels, _, _ := unstructured.NestedStringMap(v, "spec", "selector", "matchLabels")
	items, _, _ := unstructured.NestedSlice(v, "spec", "selector", "matchExpressions")
	matchExpressions := []metav1.LabelSelectorRequirement{}
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unable to retrieve matchExpressions for object, item %v is not a map", item)
		}
		out := metav1.LabelSelectorRequirement{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(m, &out); err != nil {
			return nil, fmt.Errorf("unable to retrieve matchExpressions for object: %v", err)
		}
		matchExpressions = append(matchExpressions, out)
	}
	return &metav1.LabelSelector{
		MatchLabels:      labels,
		MatchExpressions: matchExpressions,
	}, nil
}

func revision(obj runtime.Object) (int64, error) {
	acc, err := meta.Accessor(obj)
	if err != nil {
		return 0, err
	}
	v, ok := acc.GetAnnotations()[revisionAnnotation]
	if !ok {
		return 0, nil
	}
	return strconv.ParseInt(v, 10, 64)
}

func equalIgnoreHash(ro *unstructured.Unstructured, rs *appsv1.ReplicaSet) (bool, error) {
	roTemplate, found, err := unstructured.NestedMap(ro.Object, "spec", "template")
	if !found || err != nil {
		return false, err
	}

	rsTemplate, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&rs.Spec.Template)
	if err != nil {
		return false, err
	}

	unstructured.RemoveNestedField(roTemplate, "metadata", "labels", v1alpha1.DefaultRolloutUniqueLabelKey)
	unstructured.RemoveNestedField(rsTemplate, "metadata", "labels", v1alpha1.DefaultRolloutUniqueLabelKey)
	return apiequality.Semantic.DeepDerivative(roTemplate, rsTemplate), nil
}
