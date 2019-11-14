package analysis

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"

	argoprojclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/typed/rollouts/v1alpha1"
)

// analysisStatusOrder is a list of completed analysis sorted in best to worst condition
var analysisStatusOrder = []v1alpha1.AnalysisPhase{
	v1alpha1.AnalysisPhaseSuccessful,
	v1alpha1.AnalysisPhaseRunning,
	v1alpha1.AnalysisPhasePending,
	v1alpha1.AnalysisPhaseInconclusive,
	v1alpha1.AnalysisPhaseError,
	v1alpha1.AnalysisPhaseFailed,
}

// IsWorse returns whether or not the new health status code is a worser condition than the current.
// Both statuses must be already completed
func IsWorse(current, new v1alpha1.AnalysisPhase) bool {
	currentIndex := 0
	newIndex := 0
	for i, code := range analysisStatusOrder {
		if current == code {
			currentIndex = i
		}
		if new == code {
			newIndex = i
		}
	}
	return newIndex > currentIndex
}

// Worst returns the worst of the two statuses
func Worst(left, right v1alpha1.AnalysisPhase) v1alpha1.AnalysisPhase {
	if IsWorse(left, right) {
		return right
	}
	return left
}

// IsTerminating returns whether or not the analysis run is terminating, either because a terminate
// was requested explicitly, or because a metric has already measured Failed, Error, or Inconclusive
// which causes the run to end prematurely.
func IsTerminating(run *v1alpha1.AnalysisRun) bool {
	if run.Spec.Terminate {
		return true
	}
	for _, res := range run.Status.MetricResults {
		switch res.Phase {
		case v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseInconclusive:
			return true
		}
	}
	return false
}

// GetResult returns the metric result by name
func GetResult(run *v1alpha1.AnalysisRun, metricName string) *v1alpha1.MetricResult {
	for _, result := range run.Status.MetricResults {
		if result.Name == metricName {
			return &result
		}
	}
	return nil
}

// SetResult updates the metric result
func SetResult(run *v1alpha1.AnalysisRun, result v1alpha1.MetricResult) {
	for i, r := range run.Status.MetricResults {
		if r.Name == result.Name {
			run.Status.MetricResults[i] = result
			return
		}
	}
	run.Status.MetricResults = append(run.Status.MetricResults, result)
}

// MetricCompleted returns whether or not a metric was completed or not
func MetricCompleted(run *v1alpha1.AnalysisRun, metricName string) bool {
	if result := GetResult(run, metricName); result != nil {
		return result.Phase.Completed()
	}
	return false
}

// LastMeasurement returns the last measurement started or completed for a specific metric
func LastMeasurement(run *v1alpha1.AnalysisRun, metricName string) *v1alpha1.Measurement {
	if result := GetResult(run, metricName); result != nil {
		totalMeasurements := len(result.Measurements)
		if totalMeasurements == 0 {
			return nil
		}
		return &result.Measurements[totalMeasurements-1]
	}
	return nil
}

// TerminateRun terminates an analysis run
func TerminateRun(analysisRunIf argoprojclient.AnalysisRunInterface, name string) error {
	_, err := analysisRunIf.Patch(name, patchtypes.MergePatchType, []byte(`{"spec":{"terminate":true}}`))
	return err
}

// IsSemanticallyEqual checks to see if two analysis runs are semantically equal
func IsSemanticallyEqual(left, right v1alpha1.AnalysisRunSpec) bool {
	leftBytes, err := json.Marshal(left)
	if err != nil {
		panic(err)
	}
	rightBytes, err := json.Marshal(right)
	if err != nil {
		panic(err)
	}
	return string(leftBytes) == string(rightBytes)
}

func findArg(name string, args []v1alpha1.Argument) int {
	for i, arg := range args {
		if arg.Name == name {
			return i
		}
	}
	return -1
}

// MergeArgs merges two lists of arguments, the incoming and the templates. If there are any
// unresolved arguments that have no value, raises an error.
func MergeArgs(incomingArgs, templateArgs []v1alpha1.Argument) ([]v1alpha1.Argument, error) {
	newArgs := append(templateArgs[:0:0], templateArgs...)
	for _, arg := range incomingArgs {
		i := findArg(arg.Name, newArgs)
		if i >= 0 && arg.Value != nil {
			newArgs[i].Value = arg.Value
		}
	}
	for _, arg := range newArgs {
		if arg.Value == nil {
			return nil, fmt.Errorf("args.%s was not resolved", arg.Name)
		}
	}
	return newArgs, nil
}

// CreateWithCollisionCounter attempts to create the given analysisrun and if an AlreadyExists error
// is encountered, and the existing run is semantically equal and running, returns the exiting run.
func CreateWithCollisionCounter(logCtx *log.Entry, analysisRunIf argoprojclient.AnalysisRunInterface, run v1alpha1.AnalysisRun) (*v1alpha1.AnalysisRun, error) {
	newControllerRef := metav1.GetControllerOf(&run)
	if newControllerRef == nil {
		return nil, errors.New("Supplied run does not have an owner reference")
	}
	collisionCount := 1
	baseName := run.Name
	for {
		createdRun, err := analysisRunIf.Create(&run)
		if err == nil {
			return createdRun, nil
		}
		if !k8serrors.IsAlreadyExists(err) {
			return nil, err
		}
		// TODO(jessesuen): switch from Get to List so that there's no guessing about which collision counter to use.
		existingRun, err := analysisRunIf.Get(run.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		existingEqual := IsSemanticallyEqual(run.Spec, existingRun.Spec)
		controllerRef := metav1.GetControllerOf(existingRun)
		controllerUIDEqual := controllerRef != nil && controllerRef.UID == newControllerRef.UID
		logCtx.Infof("Encountered collision of existing analysisrun %s (phase: %s, equal: %v, controllerUIDEqual: %v)", existingRun.Name, existingRun.Status.Phase, existingEqual, controllerUIDEqual)
		if !existingRun.Status.Phase.Completed() && existingEqual && controllerUIDEqual {
			// If we get here, the existing run has been determined to be our analysis run and we
			// likely reconciled the rollout with a stale cache (quite common).
			return existingRun, nil
		}
		run.Name = fmt.Sprintf("%s.%d", baseName, collisionCount)
		collisionCount++
	}
}

func NewAnalysisRunFromTemplate(template *v1alpha1.AnalysisTemplate, args []v1alpha1.Argument, name, generateName, namespace string) (*v1alpha1.AnalysisRun, error) {
	newArgs, err := MergeArgs(args, template.Spec.Args)
	if err != nil {
		return nil, err
	}
	ar := v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:         name,
			GenerateName: generateName,
			Namespace:    namespace,
		},
		Spec: v1alpha1.AnalysisRunSpec{
			Metrics: template.Spec.Metrics,
			Args:    newArgs,
		},
	}
	return &ar, nil
}
