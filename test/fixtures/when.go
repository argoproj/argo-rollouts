package fixtures

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/ghodss/yaml"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"

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

func (w *When) ApplyManifests() *When {
	if w.rollout == nil {
		w.t.Fatal("No rollout to create")
	}
	var objects []runtime.Object
	for _, obj := range w.objects {
		objects = append(objects, obj)
	}
	objects = append(objects, w.rollout)

	for _, obj := range objects {
		objBytes, err := json.Marshal(obj)
		w.CheckError(err)
		cmd := exec.Command("kubectl", "apply", "-f", "-")
		cmd.Env = os.Environ()
		cmd.Stdin = bytes.NewReader(objBytes)
		out, err := cmd.CombinedOutput()
		if err != nil {
			gvk := obj.GetObjectKind().GroupVersionKind()
			objMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
			un := unstructured.Unstructured{Object: objMap}
			w.log.Errorf("kubectl apply of %s %s failed: %s", gvk.Kind, un.GetName(), out)
			w.t.FailNow()
		}
		w.log.Info(string(out))
	}
	return w
}

func (w *When) UpdateSpec() *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	patchStr := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"update":"%s"}}}}}`, time.Now())
	_, err := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Patch(w.rollout.Name, types.MergePatchType, []byte(patchStr))
	w.CheckError(err)
	return w
}

func (w *When) PromoteRollout() *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	_, err := promote.PromoteRollout(w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace), w.rollout.Name, false, false)
	w.CheckError(err)
	w.log.Info("Promoted rollout")
	return w
}

func (w *When) AbortRollout() *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	_, err := abort.AbortRollout(w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace), w.rollout.Name)
	w.CheckError(err)
	w.log.Info("Aborted rollout")
	return w
}

func (w *When) RetryRollout() *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	_, err := retry.RetryRollout(w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace), w.rollout.Name)
	w.CheckError(err)
	w.log.Info("Retried rollout")
	return w
}

func (w *When) RestartRollout() *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	_, err := restart.RestartRollout(w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace), w.rollout.Name, nil)
	w.CheckError(err)
	w.log.Info("Restarted rollout")
	return w
}

func (w *When) ScaleRollout(scale int) *When {
	if w.rollout == nil {
		w.t.Fatal("Rollout not set")
	}
	patchStr := fmt.Sprintf(`{"spec":{"replicas":%d}}`, scale)
	_, err := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Patch(w.rollout.Name, types.MergePatchType, []byte(patchStr))
	w.CheckError(err)
	w.log.Infof("Scaled rollout to %d", scale)
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
	ro, err := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Get(w.rollout.Name, metav1.GetOptions{})
	w.CheckError(err)
	originalBytes, err := json.Marshal(ro)
	w.CheckError(err)
	newRolloutBytes, err := strategicpatch.StrategicMergePatch(originalBytes, jsonPatch, rov1.Rollout{})
	w.CheckError(err)
	var newRollout rov1.Rollout
	err = json.Unmarshal(newRolloutBytes, &newRollout)
	w.CheckError(err)
	_, err = w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Update(&newRollout)
	w.CheckError(err)
	w.log.Infof("Patched rollout: %s", string(jsonPatch))
	return w
}

func (w *When) WaitForRolloutStatus(status string) *When {
	checkStatus := func(ro *rov1.Rollout) bool {
		if info.RolloutStatusString(ro) == status {
			return true
		}
		return false
	}
	return w.WaitForRolloutCondition(checkStatus, fmt.Sprintf("status=%s", status), E2EWaitTimeout)
}

func (w *When) WaitForRolloutCanaryStepIndex(index int32) *When {
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
			return info.RolloutStatusString(ro) == "Paused"
		}
		return true
	}
	return w.WaitForRolloutCondition(checkStatus, fmt.Sprintf("status.currentStepIndex=%d", index), E2EWaitTimeout)
}

func (w *When) WaitForRolloutAvailableReplicas(count int32) *When {
	checkStatus := func(ro *rov1.Rollout) bool {
		return ro.Status.AvailableReplicas == count
	}
	return w.WaitForRolloutCondition(checkStatus, fmt.Sprintf("status.availableReplicas=%d", count), E2EWaitTimeout)
}

func (w *When) WaitForRolloutReplicas(count int32) *When {
	checkStatus := func(ro *rov1.Rollout) bool {
		return ro.Status.Replicas == count
	}
	return w.WaitForRolloutCondition(checkStatus, fmt.Sprintf("status.replicas=%d", count), E2EWaitTimeout)
}

func (w *When) WaitForRolloutCondition(test func(ro *rov1.Rollout) bool, condition string, timeout time.Duration) *When {
	start := time.Now()
	w.log.Infof("Waiting for condition: %s", condition)
	opts := metav1.ListOptions{FieldSelector: fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", w.rollout.Name)).String()}
	watch, err := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Watch(opts)
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
	err := w.rolloutClient.ArgoprojV1alpha1().Rollouts(w.namespace).Delete(w.rollout.Name, nil)
	w.CheckError(err)
	return w
}

func (w *When) WaitForAnalysisRunCondition(name string, test func(ro *rov1.AnalysisRun) bool, condition string, timeout time.Duration) *When {
	start := time.Now()
	w.log.Infof("Waiting for AnalysisRun %s condition: %s", name, condition)
	opts := metav1.ListOptions{FieldSelector: fields.ParseSelectorOrDie(fmt.Sprintf("metadata.name=%s", name)).String()}
	watch, err := w.rolloutClient.ArgoprojV1alpha1().AnalysisRuns(w.namespace).Watch(opts)
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

func (w *When) WaitForBackgroundAnalysisRunPhase(phase string) *When {
	checkPhase := func(ar *rov1.AnalysisRun) bool {
		return string(ar.Status.Phase) == phase
	}
	arun := w.GetBackgroundAnalysisRun()
	return w.WaitForAnalysisRunCondition(arun.Name, checkPhase, fmt.Sprintf("phase=%s", phase), E2EWaitTimeout)
}

func (w *When) WaitForInlineAnalysisRunPhase(phase string) *When {
	checkPhase := func(ar *rov1.AnalysisRun) bool {
		return string(ar.Status.Phase) == phase
	}
	arun := w.GetInlineAnalysisRun()
	return w.WaitForAnalysisRunCondition(arun.Name, checkPhase, fmt.Sprintf("phase=%s", phase), E2EWaitTimeout)
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
