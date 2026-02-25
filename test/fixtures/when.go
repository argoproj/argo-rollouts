package fixtures

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/tools/cache"
	watchutil "k8s.io/client-go/tools/watch"
	retryutil "k8s.io/client-go/util/retry"
	"sigs.k8s.io/yaml"

	"github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/abort"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/promote"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/restart"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/retry"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/status"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/undo"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/viewcontroller"
	rolloututil "github.com/argoproj/argo-rollouts/utils/rollout"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

type When struct {
	*Common
}

func (w *When) ApplyManifests(yaml ...string) *When {
	var objects []*unstructured.Unstructured
	if len(yaml) == 0 {
		if w.rollout == nil && w.rolloutPlugin == nil {
			w.t.Fatal("No rollout or rolloutplugin to create")
		}
		objects = w.objects
	} else {
		objects = w.parseTextToObjects(yaml[0])
	}
	for _, obj := range objects {
		if obj.GetKind() == "Rollout" {
			w.injectDelays(obj)
			w.injectImagePrefix(obj)
		}
		if obj.GetKind() == "StatefulSet" {
			w.injectStatefulSetDelays(obj)
			w.injectStatefulSetImagePrefix(obj)
		}
		if obj.GetKind() == "Ingress" {
			w.injectIngressAnnotations(obj)
		}
		w.applyObject(obj)
	}
	return w
}

func (w *When) DeleteObject(kind, name string) *When {
	w.deleteObject(kind, name)
	return w
}

// injectDelays adds postStart/preStop handlers to slow down readiness/termination by adding a
// preStart and postStart handlers which sleeps for the specified duration.
func (w *When) injectDelays(un *unstructured.Unstructured) {
	if E2EPodDelay == 0 {
		return
	}
	sleepHandler := corev1.LifecycleHandler{
		Exec: &corev1.ExecAction{
			Command: []string{"sleep", strconv.Itoa(E2EPodDelay)},
		},
	}
	lifecycle := corev1.Lifecycle{
		PostStart: &sleepHandler,
		PreStop:   &sleepHandler,
	}
	lifecycleObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&lifecycle)
	w.CheckError(err)
	containersIf, _, err := unstructured.NestedSlice(un.Object, "spec", "template", "spec", "containers")
	w.CheckError(err)
	container := containersIf[0].(map[string]any)
	container["lifecycle"] = lifecycleObj
	containersIf[0] = container
	err = unstructured.SetNestedSlice(un.Object, containersIf, "spec", "template", "spec", "containers")
	w.CheckError(err)
}

// injectImagePrefix prefixes images used in tests with a prefix. Useful if container registries are blocked
func (w *When) injectImagePrefix(un *unstructured.Unstructured) {
	imagePrefix := os.Getenv(EnvVarE2EImagePrefix)
	if imagePrefix == "" {
		return
	}
	containersIf, _, err := unstructured.NestedSlice(un.Object, "spec", "template", "spec", "containers")
	w.CheckError(err)
	container := containersIf[0].(map[string]any)
	container["image"] = imagePrefix + container["image"].(string)
	containersIf[0] = container
	err = unstructured.SetNestedSlice(un.Object, containersIf, "spec", "template", "spec", "containers")
	w.CheckError(err)
}

// injectStatefulSetDelays adds postStart/preStop handlers to StatefulSet to slow down readiness/termination
func (w *When) injectStatefulSetDelays(un *unstructured.Unstructured) {
	if E2EPodDelay == 0 {
		return
	}
	sleepHandler := corev1.LifecycleHandler{
		Exec: &corev1.ExecAction{
			Command: []string{"sleep", strconv.Itoa(E2EPodDelay)},
		},
	}
	lifecycle := corev1.Lifecycle{
		PostStart: &sleepHandler,
		PreStop:   &sleepHandler,
	}
	lifecycleObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&lifecycle)
	w.CheckError(err)
	containersIf, _, err := unstructured.NestedSlice(un.Object, "spec", "template", "spec", "containers")
	w.CheckError(err)
	container := containersIf[0].(map[string]any)
	container["lifecycle"] = lifecycleObj
	containersIf[0] = container
	err = unstructured.SetNestedSlice(un.Object, containersIf, "spec", "template", "spec", "containers")
	w.CheckError(err)
}

// injectStatefulSetImagePrefix prefixes images used in StatefulSet tests with a prefix
func (w *When) injectStatefulSetImagePrefix(un *unstructured.Unstructured) {
	imagePrefix := os.Getenv(EnvVarE2EImagePrefix)
	if imagePrefix == "" {
		return
	}
	containersIf, _, err := unstructured.NestedSlice(un.Object, "spec", "template", "spec", "containers")
	w.CheckError(err)
	container := containersIf[0].(map[string]any)
	container["image"] = imagePrefix + container["image"].(string)
	containersIf[0] = container
	err = unstructured.SetNestedSlice(un.Object, containersIf, "spec", "template", "spec", "containers")
	w.CheckError(err)
}

// injectIngressAnnotations injects ingress annotations defined in environment variables. Currently
// E2E_ALB_INGESS_ANNOTATIONS
func (w *When) injectIngressAnnotations(un *unstructured.Unstructured) {
	annotations := un.GetAnnotations()
	if len(annotations) == 0 {
		return
	}
	if annotations["kubernetes.io/ingress.class"] == "alb" && len(E2EALBIngressAnnotations) > 0 {
		for k, v := range E2EALBIngressAnnotations {
			annotations[k] = v
		}
		un.SetAnnotations(annotations)
	}
}

func (w *When) UpdateSpec(texts ...string) *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	var patchBytes []byte
	if len(texts) == 0 {
		nowStr := time.Now().Format(time.RFC3339Nano)
		patchBytes = []byte(fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"update":"%s"}}}}}`, nowStr))
		w.log.Infof("Updated rollout pod spec: %s", nowStr)
	} else {
		var err error
		patchBytes, err = yaml.YAMLToJSON([]byte(texts[0]))
		w.CheckError(err)
		w.log.Infof("Updated rollout spec: %s", string(patchBytes))
	}
	_, err := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Patch(w.Context, w.rollout.GetName(), types.MergePatchType, patchBytes, metav1.PatchOptions{})
	w.CheckError(err)
	return w
}

// UpdateWorkloadRef updates the workload referenced by the rollout (e.g., deployment)
func (w *When) UpdateWorkloadRef(deploymentName string, texts ...string) *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}

	currentRo, err := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Get(w.Context, w.rollout.GetName(), metav1.GetOptions{})
	w.CheckError(err)
	if currentRo.Spec.WorkloadRef == nil {
		w.t.Fatal("Rollout does not have workloadRef")
	}

	var patchBytes []byte
	if len(texts) == 0 {
		nowStr := time.Now().Format(time.RFC3339Nano)
		patchBytes = []byte(fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"update":"%s"}}}}}`, nowStr))
		w.log.Infof("Updating workload ref deployment with timestamp: %s", nowStr)
	} else {
		var err error
		patchBytes, err = yaml.YAMLToJSON([]byte(texts[0]))
		w.CheckError(err)
		w.log.Infof("Updating workload ref deployment: %s", string(patchBytes))
	}

	_, err = w.kubeClient.AppsV1().Deployments(w.namespace).Patch(
		w.Context,
		deploymentName,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	w.CheckError(err)
	w.log.Infof("Updated workload ref deployment: %s", deploymentName)
	return w
}

func (w *When) PromoteRollout() *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	w.waitForPauseConditionsSet()
	_, err := promote.PromoteRollout(w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace), w.rollout.GetName(), false, false, false)
	w.CheckError(err)
	w.log.Info("Promoted rollout")
	w.clearControllerPauseIfNeeded()
	return w
}

func (w *When) PromoteRolloutFull() *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	w.waitForPauseConditionsSet()
	_, err := promote.PromoteRollout(w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace), w.rollout.GetName(), false, false, true)
	w.CheckError(err)
	w.log.Info("Promoted rollout fully")
	w.clearControllerPauseIfNeeded()
	return w
}

func (w *When) AbortRollout() *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	_, err := abort.AbortRollout(w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace), w.rollout.GetName())
	w.CheckError(err)
	w.log.Info("Aborted rollout")
	return w
}

func (w *When) RetryRollout() *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	_, err := retry.RetryRollout(w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace), w.rollout.GetName())
	w.CheckError(err)
	w.log.Info("Retried rollout")
	return w
}

func (w *When) UndoRollout(toRevision int64) *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	_, err := undo.RunUndoRollout(w.dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace(w.namespace), w.kubeClient, w.rollout.GetName(), toRevision)
	w.CheckError(err)
	w.log.Infof("Undo rollout to %d", toRevision)
	return w
}

func (w *When) RestartRollout() *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	_, err := restart.RestartRollout(w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace), w.rollout.GetName(), nil)
	w.CheckError(err)
	w.log.Info("Restarted rollout")
	return w
}

func (w *When) ScaleRollout(scale int) *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	patchStr := fmt.Sprintf(`{"spec":{"replicas":%d}}`, scale)
	_, err := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Patch(w.Context, w.rollout.GetName(), types.MergePatchType, []byte(patchStr), metav1.PatchOptions{})
	w.CheckError(err)
	w.log.Infof("Scaled rollout to %d", scale)
	return w
}

// ScaleRolloutWithWorkloadRef scales a rollout with workload reference using JSON patch
// to ensure only the replicas field is modified and template remains untouched
func (w *When) ScaleRolloutWithWorkloadRef(scale int) *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}

	currentRo, err := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Get(w.Context, w.rollout.GetName(), metav1.GetOptions{})
	w.CheckError(err)
	w.log.Infof("Current rollout replicas: %d, workloadRef: %v", *currentRo.Spec.Replicas, currentRo.Spec.WorkloadRef != nil)

	// Create JSON patch that only modifies replicas
	patch := []map[string]interface{}{
		{
			"op":    "replace",
			"path":  "/spec/replicas",
			"value": scale,
		},
	}

	patchBytes, err := json.Marshal(patch)
	w.CheckError(err)

	_, err = w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Patch(
		w.Context,
		w.rollout.GetName(),
		types.JSONPatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	w.CheckError(err)

	// Verify the patch was successful
	updatedRo, err := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Get(w.Context, w.rollout.GetName(), metav1.GetOptions{})
	w.CheckError(err)
	w.log.Infof("Scaled rollout with workload ref to %d (actual: %d)", scale, *updatedRo.Spec.Replicas)

	return w
}

func (w *When) Sleep(d time.Duration) *When {
	w.log.Infof("Sleeping %s", d)
	time.Sleep(d)
	return w
}

// UpdateResource modifies the specified resource
func (w *When) UpdateResource(gvr schema.GroupVersionResource, name string, update func(res *unstructured.Unstructured) error) *When {
	err := retryutil.RetryOnConflict(retryutil.DefaultRetry, func() error {
		client := w.dynamicClient.Resource(gvr).Namespace(w.namespace)
		res, err := client.Get(w.Context, name, metav1.GetOptions{})
		w.CheckError(err)
		err = update(res)
		w.CheckError(err)
		_, err = client.Update(w.Context, res, metav1.UpdateOptions{})
		return err
	})
	w.CheckError(err)
	return w
}

// PatchSpec patches the rollout
func (w *When) PatchSpec(patch string) *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	// convert YAML patch to JSON patch
	var patchObj map[string]any
	err := yaml.Unmarshal([]byte(patch), &patchObj)
	w.CheckError(err)
	jsonPatch, err := json.Marshal(patchObj)
	w.CheckError(err)

	// Apply patch
	err = retryutil.RetryOnConflict(retryutil.DefaultRetry, func() error {
		ro, err := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Get(w.Context, w.rollout.GetName(), metav1.GetOptions{})
		w.CheckError(err)
		originalBytes, err := json.Marshal(ro)
		w.CheckError(err)
		newRolloutBytes, err := strategicpatch.StrategicMergePatch(originalBytes, jsonPatch, rov1.Rollout{})
		w.CheckError(err)
		var newRollout rov1.Rollout
		err = json.Unmarshal(newRolloutBytes, &newRollout)
		w.CheckError(err)
		_, err = w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Update(w.Context, &newRollout, metav1.UpdateOptions{})
		return err
	})
	w.CheckError(err)
	w.log.Infof("Patched rollout: %s", string(jsonPatch))
	return w
}

func (w *When) WaitForRolloutStatus(status string, timeout ...time.Duration) *When {
	checkStatus := func(ro *rov1.Rollout) bool {
		s, _ := rolloututil.GetRolloutPhase(ro)
		return string(s) == status
	}
	return w.WaitForRolloutCondition(checkStatus, fmt.Sprintf("status=%s", status), timeout...)
}

func (w *When) WaitForRolloutMessage(message string, timeout ...time.Duration) *When {
	checkStatus := func(ro *rov1.Rollout) bool {
		_, m := rolloututil.GetRolloutPhase(ro)
		return m == message
	}
	return w.WaitForRolloutCondition(checkStatus, fmt.Sprintf("message=%s", message), timeout...)
}

func (w *When) MarkPodsReady(revision string, count int, timeouts ...time.Duration) *When {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	timeout := E2EWaitTimeout
	if len(timeouts) > 0 {
		timeout = timeouts[0]
	}
	timeoutCh := make(chan bool, 1)
	go func() {
		time.Sleep(timeout)
		timeoutCh <- true
	}()
	for {
		select {
		case <-ticker.C:
			marked := w.Common.MarkPodsReady(revision, count)
			count -= marked
			if count <= 0 {
				return w
			}
		case <-timeoutCh:
			w.t.Fatalf("timeout after %v waiting for marking pods ready", timeout)
		}
	}
}

func (w *When) WaitForRevisionPodCount(revision string, count int, timeouts ...time.Duration) *When {
	checkCount := func(ro *rov1.Rollout) bool {
		actual := 0
		pods := w.GetPodsByRevision(revision)
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil {
				continue
			}
			actual += 1
		}
		return actual == count
	}
	return w.WaitForRolloutCondition(checkCount, fmt.Sprintf("rev=%s podcount=%d", revision, count), timeouts...)

}

func (w *When) Wait(duration time.Duration) *When {
	time.Sleep(duration)
	return w
}

// WatchRolloutStatus returns success if Rollout becomes Healthy within timeout period
// Returns error is Rollout becomes Degraded or times out
func (w *When) WatchRolloutStatus(expectedStatus string, timeouts ...time.Duration) *When {
	timeout := E2EWaitTimeout
	if len(timeouts) > 0 {
		timeout = timeouts[0]
	}

	iostreams, _, _, _ := genericclioptions.NewTestIOStreams()
	statusOptions := status.StatusOptions{
		Watch:   true,
		Timeout: timeout,
		ArgoRolloutsOptions: options.ArgoRolloutsOptions{
			RolloutsClient: w.rolloutClient,
			KubeClient:     w.kubeClient,
			Log:            w.log.Logger,
			IOStreams:      iostreams,
		},
	}

	controller := viewcontroller.NewRolloutViewController(w.namespace, w.rollout.GetName(), w.kubeClient, w.rolloutClient)
	ctx, cancel := context.WithCancel(w.Context)
	controller.Start(ctx)

	rolloutUpdates := make(chan *rollout.RolloutInfo)
	controller.RegisterCallback(func(roInfo *rollout.RolloutInfo) {
		rolloutUpdates <- roInfo
	})

	go controller.Run(ctx)
	finalStatus := statusOptions.WatchStatus(ctx.Done(), rolloutUpdates)

	controller.DeregisterCallbacks()

	cancel()
	close(rolloutUpdates)

	if finalStatus == expectedStatus {
		w.log.Infof("expected status %s", finalStatus)
	} else {
		w.t.Fatalf("unexpected status %s", finalStatus)
	}

	return w
}

func (w *When) WaitForRolloutCanaryStepIndex(index int32, timeout ...time.Duration) *When {
	checkStatus := func(ro *rov1.Rollout) bool {
		if ro.Status.CurrentStepIndex == nil || *ro.Status.CurrentStepIndex != index {
			return false
		}
		if ro.Spec.Strategy.Canary.Steps[*ro.Status.CurrentStepIndex].Pause != nil {
			// Special case for pause to deal with test timing issues. If we are waiting for a step
			// index, and that step is a pause step, we should also block until we *also* see the
			// pause condition saved in the rollout. This is because the controller increments the
			// step index in one reconciliation, and adds the pause condition in the next.
			// Without this convenience check, tests would always have to do
			//      WaitForRolloutCanaryStepIndex(N).
			//      WaitForRolloutStatus("Paused").
			// which would be annoying.
			status, _ := rolloututil.GetRolloutPhase(ro)
			return status == "Paused"
		}
		return true
	}
	return w.WaitForRolloutCondition(checkStatus, fmt.Sprintf("status.currentStepIndex=%d", index), timeout...)
}

func (w *When) WaitForRolloutAvailableReplicas(count int32, timeout ...time.Duration) *When {
	checkStatus := func(ro *rov1.Rollout) bool {
		return ro.Status.AvailableReplicas == count
	}
	return w.WaitForRolloutCondition(checkStatus, fmt.Sprintf("status.availableReplicas=%d", count), timeout...)
}

func (w *When) WaitForRolloutReplicas(count int32, timeout ...time.Duration) *When {
	checkStatus := func(ro *rov1.Rollout) bool {
		return ro.Status.Replicas == count
	}
	return w.WaitForRolloutCondition(checkStatus, fmt.Sprintf("status.replicas=%d", count), timeout...)
}

func (w *When) WaitForActiveRevision(revision string, timeout ...time.Duration) *When {
	rs := w.GetReplicaSetByRevision(revision)
	checkStatus := func(ro *rov1.Rollout) bool {
		return ro.Status.BlueGreen.ActiveSelector == rs.Labels[rov1.DefaultRolloutUniqueLabelKey]
	}
	return w.WaitForRolloutCondition(checkStatus, fmt.Sprintf("active revision=%s", revision), timeout...)
}

func (w *When) WaitForRolloutStepPluginRunning(timeout ...time.Duration) *When {
	checkStatus := func(ro *rov1.Rollout) bool {
		for _, s := range ro.Status.Canary.StepPluginStatuses {
			if s.Index == *ro.Status.CurrentStepIndex && s.Operation == rov1.StepPluginOperationRun && s.Phase == v1alpha1.StepPluginPhaseRunning {
				return true
			}
		}
		return false
	}
	return w.WaitForRolloutCondition(checkStatus, fmt.Sprintf("stepPluginStatus[currentIndex].phase=Running"), timeout...)
}

func (w *When) WaitForRolloutCondition(test func(ro *rov1.Rollout) bool, condition string, timeouts ...time.Duration) *When {
	start := time.Now()
	w.log.Infof("Waiting for condition: %s", condition)
	rolloutIf := w.dynamicClient.Resource(rov1.RolloutGVR).Namespace(w.namespace)
	ro, err := rolloutIf.Get(w.Context, w.rollout.GetName(), metav1.GetOptions{})
	w.CheckError(err)
	retryWatcher, err := watchutil.NewRetryWatcher(ro.GetResourceVersion(), &cache.ListWatch{
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			opts := metav1.ListOptions{FieldSelector: fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", w.rollout.GetName())).String()}
			return w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Watch(w.Context, opts)
		},
	})
	w.CheckError(err)
	defer retryWatcher.Stop()
	timeout := E2EWaitTimeout
	if len(timeouts) > 0 {
		timeout = timeouts[0]
	}
	timeoutCh := make(chan bool, 1)
	go func() {
		time.Sleep(timeout)
		timeoutCh <- true
	}()
	for {
		select {
		case event := <-retryWatcher.ResultChan():
			ro, ok := event.Object.(*rov1.Rollout)
			if ok {
				if test(ro) {
					//w.PrintRollout(ro)
					w.log.Infof("Condition '%s' met after %v", condition, time.Since(start).Truncate(time.Second))
					return w
				}
			} else {
				w.t.Fatal("not ok")
			}
		case <-timeoutCh:
			w.t.Fatalf("timeout after %v waiting for condition %s", timeout, condition)
		}
	}
}

// WaitForRolloutConditionToNotExist this function will check for the condition to exist for the given duration, if it is found
// the test fails.
func (w *When) WaitForRolloutConditionToNotExist(test func(ro *rov1.Rollout) bool, condition string, timeout time.Duration) *When {
	start := time.Now()
	w.log.Infof("Waiting for condition to not exist: %s", condition)
	rolloutIf := w.dynamicClient.Resource(rov1.RolloutGVR).Namespace(w.namespace)
	ro, err := rolloutIf.Get(w.Context, w.rollout.GetName(), metav1.GetOptions{})
	w.CheckError(err)
	retryWatcher, err := watchutil.NewRetryWatcher(ro.GetResourceVersion(), &cache.ListWatch{
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			opts := metav1.ListOptions{FieldSelector: fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", w.rollout.GetName())).String()}
			return w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Watch(w.Context, opts)
		},
	})
	w.CheckError(err)
	defer retryWatcher.Stop()
	timeoutCh := make(chan bool, 1)
	go func() {
		time.Sleep(timeout)
		timeoutCh <- true
	}()
	for {
		select {
		case event := <-retryWatcher.ResultChan():
			ro, ok := event.Object.(*rov1.Rollout)
			if ok {
				if test(ro) {
					//w.PrintRollout(ro)
					w.log.Infof("Condition '%s' met after %v", condition, time.Since(start).Truncate(time.Second))
					w.t.Fatal("not ok")
				}
			}
		case <-timeoutCh:
			w.t.Logf("Condition %s not found after %v", condition, timeout)
			return w
		}
	}
}

func (w *When) DeleteRollout() *When {
	w.log.Info("Deleting")
	err := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Delete(w.Context, w.rollout.GetName(), metav1.DeleteOptions{})
	w.CheckError(err)
	return w
}

func (w *When) WaitForExperimentCondition(name string, test func(ex *rov1.Experiment) bool, condition string, timeout time.Duration) *When {
	start := time.Now()
	w.log.Infof("Waiting for Experiment %s condition: %s", name, condition)
	exIf := w.dynamicClient.Resource(rov1.ExperimentGVR).Namespace(w.namespace)
	ex, err := exIf.Get(w.Context, name, metav1.GetOptions{})
	w.CheckError(err)
	retryWatcher, err := watchutil.NewRetryWatcher(ex.GetResourceVersion(), &cache.ListWatch{
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			opts := metav1.ListOptions{FieldSelector: fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", name)).String()}
			return w.rolloutClient.ArgoprojV1alpha1().Experiments(w.namespace).Watch(w.Context, opts)
		},
	})
	w.CheckError(err)
	defer retryWatcher.Stop()
	timeoutCh := make(chan bool, 1)
	go func() {
		time.Sleep(timeout)
		timeoutCh <- true
	}()
	for {
		select {
		case event := <-retryWatcher.ResultChan():
			ex, ok := event.Object.(*rov1.Experiment)
			if ok {
				if test(ex) {
					w.log.Infof("Condition '%s' met after %v", condition, time.Since(start).Truncate(time.Second))
					return w
				}
			} else {
				w.t.Fatal("not ok")
			}
		case <-timeoutCh:
			w.t.Fatalf("timeout after %v waiting for condition %s", timeout, condition)
		}
	}
}

func (w *When) WaitForAnalysisRunCondition(name string, test func(ar *rov1.AnalysisRun) bool, condition string, timeout time.Duration) *When {
	start := time.Now()
	w.log.Infof("Waiting for AnalysisRun %s condition: %s", name, condition)
	arIf := w.dynamicClient.Resource(rov1.AnalysisRunGVR).Namespace(w.namespace)
	ar, err := arIf.Get(w.Context, name, metav1.GetOptions{})
	w.CheckError(err)
	retryWatcher, err := watchutil.NewRetryWatcher(ar.GetResourceVersion(), &cache.ListWatch{
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			opts := metav1.ListOptions{FieldSelector: fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", name)).String()}
			return w.rolloutClient.ArgoprojV1alpha1().AnalysisRuns(w.namespace).Watch(w.Context, opts)
		},
	})
	w.CheckError(err)
	defer retryWatcher.Stop()
	timeoutCh := make(chan bool, 1)
	go func() {
		time.Sleep(timeout)
		timeoutCh <- true
	}()
	for {
		select {
		case event := <-retryWatcher.ResultChan():
			ar, ok := event.Object.(*rov1.AnalysisRun)
			if ok {
				if test(ar) {
					w.log.Infof("Condition '%s' met after %v", condition, time.Since(start).Truncate(time.Second))
					return w
				}
			} else {
				w.t.Fatal("not ok")
			}
		case <-timeoutCh:
			w.t.Fatalf("timeout after %v waiting for condition %s", timeout, condition)
		}
	}
}

func checkExperimentPhase(phase string) func(ex *rov1.Experiment) bool {
	return func(ex *rov1.Experiment) bool {
		return string(ex.Status.Phase) == phase
	}
}

func checkAnalysisRunPhase(phase string) func(ar *rov1.AnalysisRun) bool {
	return func(ar *rov1.AnalysisRun) bool {
		return string(ar.Status.Phase) == phase
	}
}

func (w *When) WaitForExperimentPhase(name string, phase string) *When {
	return w.WaitForExperimentCondition(name, checkExperimentPhase(phase), fmt.Sprintf("phase=%s", phase), E2EWaitTimeout)
}

func (w *When) WaitForBackgroundAnalysisRunPhase(phase string) *When {
	arun := w.GetBackgroundAnalysisRun()
	return w.WaitForAnalysisRunCondition(arun.Name, checkAnalysisRunPhase(phase), fmt.Sprintf("phase=%s", phase), E2EWaitTimeout)
}

func (w *When) WaitForInlineAnalysisRunPhase(phase string) *When {
	arun := w.GetInlineAnalysisRun()
	return w.WaitForAnalysisRunCondition(arun.Name, checkAnalysisRunPhase(phase), fmt.Sprintf("phase=%s", phase), E2EWaitTimeout)
}

func (w *When) WaitForPrePromotionAnalysisRunPhase(phase string) *When {
	arun := w.GetPrePromotionAnalysisRun()
	return w.WaitForAnalysisRunCondition(arun.Name, checkAnalysisRunPhase(phase), fmt.Sprintf("phase=%s", phase), E2EWaitTimeout)
}

func (w *When) WaitForPostPromotionAnalysisRunPhase(phase string) *When {
	arun := w.GetPostPromotionAnalysisRun()
	return w.WaitForAnalysisRunCondition(arun.Name, checkAnalysisRunPhase(phase), fmt.Sprintf("phase=%s", phase), E2EWaitTimeout)
}

func (w *When) StartLoad() *When {
	yamlBytes := w.yamlBytes("@istio/load-test-job.yaml")
	objs, err := unstructuredutil.SplitYAML(string(yamlBytes))
	w.CheckError(err)
	w.applyObject(objs[0])
	return w
}

func (w *When) StopLoad() *When {
	cmd := exec.Command("kubectl", "exec", "job/load-test", "--", "killall", "-s", "SIGINT", "wrk")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.log.Errorf("kubectl exec failed: %s", out)
		w.t.FailNow()
	}
	w.log.Info(string(out))
	return w
}

// waitForPauseConditionsSet waits for the controller to finish setting up the pause state.
// This ensures pauseConditions is populated when controllerPause is true, which indicates
// the controller has completed its reconciliation and is ready for a promote.
func (w *When) waitForPauseConditionsSet() {
	rolloutIf := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace)
	err := retryutil.OnError(retryutil.DefaultBackoff, func(err error) bool {
		return true
	}, func() error {
		ro, err := rolloutIf.Get(w.Context, w.rollout.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		if ro.Status.ObservedGeneration != strconv.FormatInt(ro.Generation, 10) {
			return fmt.Errorf("waiting for observedGeneration (%s) to match generation (%d)", ro.Status.ObservedGeneration, ro.Generation)
		}
		if ro.Status.ControllerPause && len(ro.Status.PauseConditions) == 0 {
			return fmt.Errorf("waiting for pauseConditions to be set (controllerPause=true)")
		}
		return nil
	})
	w.CheckError(err)
}

// clearControllerPauseIfNeeded checks if the controller has processed the promote.
// Due to the controller's writeBackToInformer function, watch events may be missed
// after a reconciliation. If the controller hasn't processed the promote (indicated
// by controllerPause=true with empty pauseConditions), we force a reconciliation
// by scaling the rollout, which triggers ReplicaSet changes.
// Note: This workaround is only applied to canary rollouts. Bluegreen rollouts have
// different pause/promote semantics where the scale workaround causes the controller
// to re-add the pause when reconciling with stale informer cache.
func (w *When) clearControllerPauseIfNeeded() {
	rolloutIf := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace)

	// Poll until the controller processes the promote (clears controllerPause).
	err := retryutil.OnError(retryutil.DefaultBackoff, func(err error) bool {
		return true
	}, func() error {
		ro, err := rolloutIf.Get(w.Context, w.rollout.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		if len(ro.Status.PauseConditions) > 0 {
			return nil
		}
		if !ro.Status.ControllerPause {
			return nil
		}
		return fmt.Errorf("waiting for controller to process promote")
	})

	ro, getErr := rolloutIf.Get(w.Context, w.rollout.GetName(), metav1.GetOptions{})
	w.CheckError(getErr)

	if len(ro.Status.PauseConditions) > 0 || !ro.Status.ControllerPause {
		return
	}

	// Force a reconciliation to make the controller process the promote
	if err != nil {
		w.log.Info("Forcing reconciliation (controller race condition workaround)")

		if ro.Spec.Strategy.Canary != nil {
			currentReplicas := int32(1)
			if ro.Spec.Replicas != nil {
				currentReplicas = *ro.Spec.Replicas
			}
			scalePatch := []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, currentReplicas+1))
			_, scaleErr := rolloutIf.Patch(w.Context, w.rollout.GetName(), types.MergePatchType, scalePatch, metav1.PatchOptions{})
			w.CheckError(scaleErr)

			err = retryutil.OnError(retryutil.DefaultBackoff, func(err error) bool {
				return true
			}, func() error {
				ro, err := rolloutIf.Get(w.Context, w.rollout.GetName(), metav1.GetOptions{})
				if err != nil {
					return err
				}

				observedGen, err := strconv.Atoi(ro.Status.ObservedGeneration)
				if err != nil {
					return err
				}

				if int64(observedGen) >= ro.Generation {
					return nil
				}

				return fmt.Errorf("waiting for controller to reconcile")
			})
			w.CheckError(err)

			scalePatch = []byte(fmt.Sprintf(`{"spec":{"replicas":%d}}`, currentReplicas))
			_, scaleErr = rolloutIf.Patch(w.Context, w.rollout.GetName(), types.MergePatchType, scalePatch, metav1.PatchOptions{})
			w.CheckError(scaleErr)
		} else if ro.Spec.Strategy.BlueGreen != nil {
			annotationPatch := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"e2e-reconcile-trigger":"%d"}}}`, time.Now().UnixNano()))
			_, patchErr := rolloutIf.Patch(w.Context, w.rollout.GetName(), types.MergePatchType, annotationPatch, metav1.PatchOptions{})
			w.CheckError(patchErr)
		}
	}
}

func (w *When) Then() *Then {
	return &Then{
		Common: w.Common,
	}
}

func (w *When) Given() *Given {
	return &Given{
		Common: w.Common,
	}
}

// ===================== RolloutPlugin Methods =====================

// UpdateStatefulSetImage updates the StatefulSet's container image to trigger a rollout
func (w *When) UpdateStatefulSetImage(image string) *When {
	if w.statefulSet == nil {
		w.t.Fatal("StatefulSet not set")
	}
	patchBytes := []byte(fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":"busybox","image":"%s"}]}}}}`, image))
	_, err := w.kubeClient.AppsV1().StatefulSets(w.namespace).Patch(w.Context, w.statefulSet.GetName(), types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	w.CheckError(err)
	w.log.Infof("Updated StatefulSet image to: %s", image)
	return w
}

// SetStatefulSetAnnotation sets an annotation on the StatefulSet
func (w *When) SetStatefulSetAnnotation(key, value string) *When {
	if w.statefulSet == nil {
		w.t.Fatal("StatefulSet not set")
	}
	patchBytes := []byte(fmt.Sprintf(`{"metadata":{"annotations":{"%s":"%s"}}}`, key, value))
	_, err := w.kubeClient.AppsV1().StatefulSets(w.namespace).Patch(w.Context, w.statefulSet.GetName(), types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
	w.CheckError(err)
	w.log.Infof("Set StatefulSet annotation %s=%s", key, value)
	return w
}

// WaitForStatefulSetReady waits for all StatefulSet pods to be ready
func (w *When) WaitForStatefulSetReady(timeouts ...time.Duration) *When {
	if w.statefulSet == nil {
		w.t.Fatal("StatefulSet not set")
	}
	timeout := E2EWaitTimeout
	if len(timeouts) > 0 {
		timeout = timeouts[0]
	}
	start := time.Now()
	w.log.Infof("Waiting for StatefulSet %s to be ready", w.statefulSet.GetName())

	err := wait.PollUntilContextTimeout(w.Context, time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		sts, err := w.kubeClient.AppsV1().StatefulSets(w.namespace).Get(ctx, w.statefulSet.GetName(), metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		desired := int32(1)
		if sts.Spec.Replicas != nil {
			desired = *sts.Spec.Replicas
		}
		ready := sts.Status.ReadyReplicas >= desired
		if !ready {
			w.log.Infof("StatefulSet %s: %d/%d pods ready", w.statefulSet.GetName(), sts.Status.ReadyReplicas, desired)
		}
		return ready, nil
	})
	if err != nil {
		w.t.Fatalf("Timeout waiting for StatefulSet %s to be ready after %v", w.statefulSet.GetName(), timeout)
	}
	w.log.Infof("StatefulSet %s is ready (took %v)", w.statefulSet.GetName(), time.Since(start))
	return w
}

// WaitForStatefulSetPartition waits for the StatefulSet's partition to reach the expected value
func (w *When) WaitForStatefulSetPartition(expectedPartition int32, timeouts ...time.Duration) *When {
	if w.statefulSet == nil {
		w.t.Fatal("StatefulSet not set")
	}
	timeout := E2EWaitTimeout
	if len(timeouts) > 0 {
		timeout = timeouts[0]
	}
	start := time.Now()
	w.log.Infof("Waiting for StatefulSet %s partition to be %d", w.statefulSet.GetName(), expectedPartition)

	err := wait.PollUntilContextTimeout(w.Context, time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		sts, err := w.kubeClient.AppsV1().StatefulSets(w.namespace).Get(ctx, w.statefulSet.GetName(), metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		var actualPartition int32 = 0
		if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
			actualPartition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
		}
		if actualPartition != expectedPartition {
			w.log.Infof("StatefulSet %s: partition %d (waiting for %d)", w.statefulSet.GetName(), actualPartition, expectedPartition)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		w.t.Fatalf("Timeout waiting for StatefulSet %s partition to be %d after %v", w.statefulSet.GetName(), expectedPartition, timeout)
	}
	w.log.Infof("StatefulSet %s partition is %d (took %v)", w.statefulSet.GetName(), expectedPartition, time.Since(start))
	return w
}

// WaitForRolloutPluginStatus waits for RolloutPlugin to reach the specified status/phase
func (w *When) WaitForRolloutPluginStatus(status string, timeouts ...time.Duration) *When {
	checkStatus := func(rp *rov1.RolloutPlugin) bool {
		return rp.Status.Phase == status
	}
	return w.WaitForRolloutPluginCondition(checkStatus, fmt.Sprintf("status=%s", status), timeouts...)
}

// WaitForRolloutPluginCanaryStepIndex waits for RolloutPlugin to reach the specified step index
func (w *When) WaitForRolloutPluginCanaryStepIndex(index int32, timeouts ...time.Duration) *When {
	checkStatus := func(rp *rov1.RolloutPlugin) bool {
		if rp.Status.CurrentStepIndex == nil || *rp.Status.CurrentStepIndex != index {
			return false
		}
		return true
	}
	// TODOH refactor
	// Store reference to get actual step index on failure
	var actualIndex *int32
	timeout := E2EWaitTimeout
	if len(timeouts) > 0 {
		timeout = timeouts[0]
	}

	start := time.Now()
	w.log.Infof("Waiting for RolloutPlugin condition: status.currentStepIndex=%d", index)
	rpIf := w.dynamicClient.Resource(rov1.RolloutPluginGVR).Namespace(w.namespace)
	rp, err := rpIf.Get(w.Context, w.rolloutPlugin.GetName(), metav1.GetOptions{})
	w.CheckError(err)
	retryWatcher, err := watchutil.NewRetryWatcher(rp.GetResourceVersion(), &cache.ListWatch{
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			opts := metav1.ListOptions{FieldSelector: fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", w.rolloutPlugin.GetName())).String()}
			return w.rolloutClient.ArgoprojV1alpha1().RolloutPlugins(w.namespace).Watch(w.Context, opts)
		},
	})
	w.CheckError(err)
	defer retryWatcher.Stop()

	timeoutCh := make(chan bool, 1)
	go func() {
		time.Sleep(timeout)
		timeoutCh <- true
	}()
	for {
		select {
		case event := <-retryWatcher.ResultChan():
			rp, ok := event.Object.(*rov1.RolloutPlugin)
			if ok {
				actualIndex = rp.Status.CurrentStepIndex
				if checkStatus(rp) {
					w.log.Infof("RolloutPlugin condition 'status.currentStepIndex=%d' met after %v", index, time.Since(start).Truncate(time.Second))
					return w
				}
			} else {
				w.t.Fatal("not ok")
			}
		case <-timeoutCh:
			if actualIndex != nil {
				w.t.Fatalf("timeout after %v waiting for RolloutPlugin condition status.currentStepIndex=%d (actual: %d)", timeout, index, *actualIndex)
			} else {
				w.t.Fatalf("timeout after %v waiting for RolloutPlugin condition status.currentStepIndex=%d (actual: nil)", timeout, index)
			}
		}
	}
}

// WaitForRolloutPluginCondition waits for a condition to be met on the RolloutPlugin
func (w *When) WaitForRolloutPluginCondition(test func(rp *rov1.RolloutPlugin) bool, condition string, timeouts ...time.Duration) *When {
	start := time.Now()
	w.log.Infof("Waiting for RolloutPlugin condition: %s", condition)
	rpIf := w.dynamicClient.Resource(rov1.RolloutPluginGVR).Namespace(w.namespace)
	rp, err := rpIf.Get(w.Context, w.rolloutPlugin.GetName(), metav1.GetOptions{})
	w.CheckError(err)
	retryWatcher, err := watchutil.NewRetryWatcher(rp.GetResourceVersion(), &cache.ListWatch{
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			opts := metav1.ListOptions{FieldSelector: fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", w.rolloutPlugin.GetName())).String()}
			return w.rolloutClient.ArgoprojV1alpha1().RolloutPlugins(w.namespace).Watch(w.Context, opts)
		},
	})
	w.CheckError(err)
	defer retryWatcher.Stop()
	timeout := E2EWaitTimeout
	if len(timeouts) > 0 {
		timeout = timeouts[0]
	}
	timeoutCh := make(chan bool, 1)
	go func() {
		time.Sleep(timeout)
		timeoutCh <- true
	}()
	for {
		select {
		case event := <-retryWatcher.ResultChan():
			rp, ok := event.Object.(*rov1.RolloutPlugin)
			if ok {
				if test(rp) {
					w.log.Infof("RolloutPlugin condition '%s' met after %v", condition, time.Since(start).Truncate(time.Second))
					return w
				}
			} else {
				w.t.Fatal("not ok")
			}
		case <-timeoutCh:
			w.t.Fatalf("timeout after %v waiting for RolloutPlugin condition %s", timeout, condition)
		}
	}
}

// AbortRolloutPlugin aborts the RolloutPlugin by setting status.abort=true
func (w *When) AbortRolloutPlugin() *When {
	if w.rolloutPlugin == nil {
		w.t.Fatal("RolloutPlugin not set")
	}
	patchBytes := []byte(`{"status":{"abort":true}}`)
	_, err := w.rolloutClient.ArgoprojV1alpha1().RolloutPlugins(w.namespace).Patch(w.Context, w.rolloutPlugin.GetName(), types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	w.CheckError(err)
	w.log.Info("Aborted RolloutPlugin")
	return w
}

// RestartRolloutPlugin triggers a restart of the RolloutPlugin by setting status.restart=true
// This is used after an abort to restart the rollout from step 0
func (w *When) RestartRolloutPlugin() *When {
	if w.rolloutPlugin == nil {
		w.t.Fatal("RolloutPlugin not set")
	}
	patchBytes := []byte(`{"status":{"restart":true}}`)
	_, err := w.rolloutClient.ArgoprojV1alpha1().RolloutPlugins(w.namespace).Patch(w.Context, w.rolloutPlugin.GetName(), types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	w.CheckError(err)
	w.log.Info("Triggered restart for RolloutPlugin")
	return w
}

// PromoteRolloutPlugin promotes the RolloutPlugin (advances past current pause)
func (w *When) PromoteRolloutPlugin() *When {
	if w.rolloutPlugin == nil {
		w.t.Fatal("RolloutPlugin not set")
	}
	// Promoting is done by clearing the paused status
	patchBytes := []byte(`{"status":{"paused":false,"pauseStartTime":null}}`)
	_, err := w.rolloutClient.ArgoprojV1alpha1().RolloutPlugins(w.namespace).Patch(w.Context, w.rolloutPlugin.GetName(), types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	w.CheckError(err)
	w.log.Info("Promoted RolloutPlugin")
	return w
}

// PromoteRolloutPluginFull promotes the RolloutPlugin to full (skips remaining steps)
func (w *When) PromoteRolloutPluginFull() *When {
	if w.rolloutPlugin == nil {
		w.t.Fatal("RolloutPlugin not set")
	}
	patchBytes := []byte(`{"status":{"promoteFull":true}}`)
	_, err := w.rolloutClient.ArgoprojV1alpha1().RolloutPlugins(w.namespace).Patch(w.Context, w.rolloutPlugin.GetName(), types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	w.CheckError(err)
	w.log.Info("Promoted RolloutPlugin to full")
	return w
}

// PauseRolloutPlugin pauses the RolloutPlugin
func (w *When) PauseRolloutPlugin() *When {
	if w.rolloutPlugin == nil {
		w.t.Fatal("RolloutPlugin not set")
	}
	patchBytes := []byte(`{"spec":{"paused":true}}`)
	_, err := w.rolloutClient.ArgoprojV1alpha1().RolloutPlugins(w.namespace).Patch(w.Context, w.rolloutPlugin.GetName(), types.MergePatchType, patchBytes, metav1.PatchOptions{})
	w.CheckError(err)
	w.log.Info("Paused RolloutPlugin")
	return w
}

// ResumeRolloutPlugin resumes a paused RolloutPlugin
func (w *When) ResumeRolloutPlugin() *When {
	if w.rolloutPlugin == nil {
		w.t.Fatal("RolloutPlugin not set")
	}
	patchBytes := []byte(`{"spec":{"paused":false}}`)
	_, err := w.rolloutClient.ArgoprojV1alpha1().RolloutPlugins(w.namespace).Patch(w.Context, w.rolloutPlugin.GetName(), types.MergePatchType, patchBytes, metav1.PatchOptions{})
	w.CheckError(err)
	w.log.Info("Resumed RolloutPlugin")
	return w
}

// WaitForRolloutPluginBackgroundAnalysisRunPhase waits for RolloutPlugin background analysis run to reach a phase
func (w *When) WaitForRolloutPluginBackgroundAnalysisRunPhase(phase string) *When {
	arun := w.GetRolloutPluginBackgroundAnalysisRun()
	// Fetch fresh status to check if already in desired phase (avoid race condition)
	freshArun, err := w.dynamicClient.Resource(rov1.AnalysisRunGVR).Namespace(w.namespace).Get(w.Context, arun.Name, metav1.GetOptions{})
	if err == nil {
		var freshArunTyped rov1.AnalysisRun
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(freshArun.Object, &freshArunTyped); err == nil {
			if string(freshArunTyped.Status.Phase) == phase {
				w.log.Infof("AnalysisRun %s already in phase %s", arun.Name, phase)
				return w
			}
		}
	}
	return w.WaitForAnalysisRunCondition(arun.Name, checkAnalysisRunPhase(phase), fmt.Sprintf("phase=%s", phase), E2EWaitTimeout)
}

// WaitForRolloutPluginInlineAnalysisRunPhase waits for RolloutPlugin inline (step) analysis run to reach a phase
func (w *When) WaitForRolloutPluginInlineAnalysisRunPhase(phase string) *When {
	arun := w.GetRolloutPluginInlineAnalysisRun()
	// Fetch fresh status to check if already in desired phase (avoid race condition)
	freshArun, err := w.dynamicClient.Resource(rov1.AnalysisRunGVR).Namespace(w.namespace).Get(w.Context, arun.Name, metav1.GetOptions{})
	if err == nil {
		var freshArunTyped rov1.AnalysisRun
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(freshArun.Object, &freshArunTyped); err == nil {
			if string(freshArunTyped.Status.Phase) == phase {
				w.log.Infof("AnalysisRun %s already in phase %s", arun.Name, phase)
				return w
			}
		}
	}
	return w.WaitForAnalysisRunCondition(arun.Name, checkAnalysisRunPhase(phase), fmt.Sprintf("phase=%s", phase), E2EWaitTimeout)
} // TODOH method calls
