package fixtures

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/ghodss/yaml"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	watchutil "k8s.io/client-go/tools/watch"
	retryutil "k8s.io/client-go/util/retry"

	rov1 "github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/abort"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/promote"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/restart"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/retry"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
)

type When struct {
	Common
}

func (w *When) ApplyManifests(yaml ...string) *When {
	var objects []*unstructured.Unstructured
	if len(yaml) == 0 {
		if w.rollout == nil {
			w.t.Fatal("No rollout to create")
		}
		objects = w.objects
	} else {
		objects = w.parseTextToObjects(yaml[0])
	}
	for _, obj := range objects {
		if obj.GetKind() == "Rollout" && E2EPodDelay > 0 {
			w.injectDelays(obj)
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
	sleepHandler := corev1.Handler{
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
	container := containersIf[0].(map[string]interface{})
	container["lifecycle"] = lifecycleObj
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
		nowStr := time.Now().Format(time.RFC3339)
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

func (w *When) PromoteRollout() *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	_, err := promote.PromoteRollout(w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace), w.rollout.GetName(), false, false, false)
	w.CheckError(err)
	w.log.Info("Promoted rollout")
	return w
}

func (w *When) PromoteRolloutFull() *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	_, err := promote.PromoteRollout(w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace), w.rollout.GetName(), false, false, true)
	w.CheckError(err)
	w.log.Info("Promoted rollout fully")
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

func (w *When) Sleep(d time.Duration) *When {
	time.Sleep(d)
	return w
}

// PatchSpec patches the rollout
func (w *When) PatchSpec(patch string) *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	// convert YAML patch to JSON patch
	var patchObj map[string]interface{}
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
		s, _ := info.RolloutStatusString(ro)
		return s == status
	}
	return w.WaitForRolloutCondition(checkStatus, fmt.Sprintf("status=%s", status), timeout...)
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
			status, _ := info.RolloutStatusString(ro)
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

func (w *When) DeleteRollout() *When {
	w.log.Info("Deleting")
	err := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Delete(w.Context, w.rollout.GetName(), metav1.DeleteOptions{})
	w.CheckError(err)
	return w
}

func (w *When) WaitForAnalysisRunCondition(name string, test func(ro *rov1.AnalysisRun) bool, condition string, timeout time.Duration) *When {
	start := time.Now()
	w.log.Infof("Waiting for AnalysisRun %s condition: %s", name, condition)
	opts := metav1.ListOptions{FieldSelector: fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", name)).String()}
	watch, err := w.rolloutClient.ArgoprojV1alpha1().AnalysisRuns(w.namespace).Watch(w.Context, opts)
	w.CheckError(err)
	defer watch.Stop()
	timeoutCh := make(chan bool, 1)
	go func() {
		time.Sleep(timeout)
		timeoutCh <- true
	}()
	for {
		select {
		case event := <-watch.ResultChan():
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

func checkAnalysisRunPhase(phase string) func(ar *rov1.AnalysisRun) bool {
	return func(ar *rov1.AnalysisRun) bool {
		return string(ar.Status.Phase) == phase
	}
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
