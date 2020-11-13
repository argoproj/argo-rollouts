package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"

	argoprojclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/typed/rollouts/v1alpha1"
)

// CurrentAnalysisRuns holds all the current analysis runs for a Rollout
type CurrentAnalysisRuns struct {
	BlueGreenPrePromotion  *v1alpha1.AnalysisRun
	BlueGreenPostPromotion *v1alpha1.AnalysisRun
	CanaryStep             *v1alpha1.AnalysisRun
	CanaryBackground       *v1alpha1.AnalysisRun
}

func (c CurrentAnalysisRuns) ToArray() []*v1alpha1.AnalysisRun {
	currentAnalysisRuns := []*v1alpha1.AnalysisRun{}
	if c.BlueGreenPostPromotion != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.BlueGreenPostPromotion)
	}
	if c.BlueGreenPrePromotion != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.BlueGreenPrePromotion)
	}
	if c.CanaryStep != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.CanaryStep)
	}
	if c.CanaryBackground != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.CanaryBackground)
	}
	return currentAnalysisRuns
}

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
	_, err := analysisRunIf.Patch(context.TODO(), name, patchtypes.MergePatchType, []byte(`{"spec":{"terminate":true}}`), metav1.PatchOptions{})
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
		if i >= 0 {
			if arg.Value != nil {
				newArgs[i].Value = arg.Value
			} else if arg.ValueFrom != nil {
				newArgs[i].ValueFrom = arg.ValueFrom
			}
		}
	}
	for _, arg := range newArgs {
		if arg.Value == nil && arg.ValueFrom == nil {
			return nil, fmt.Errorf("args.%s was not resolved", arg.Name)
		}
	}
	return newArgs, nil
}

// CreateWithCollisionCounter attempts to create the given analysisrun and if an AlreadyExists error
// is encountered, and the existing run is semantically equal and running, returns the exiting run.
func CreateWithCollisionCounter(logCtx *log.Entry, analysisRunIf argoprojclient.AnalysisRunInterface, run v1alpha1.AnalysisRun) (*v1alpha1.AnalysisRun, error) {
	ctx := context.TODO()
	newControllerRef := metav1.GetControllerOf(&run)
	if newControllerRef == nil {
		return nil, errors.New("Supplied run does not have an owner reference")
	}
	collisionCount := 1
	baseName := run.Name
	for {
		createdRun, err := analysisRunIf.Create(ctx, &run, metav1.CreateOptions{})
		if err == nil {
			return createdRun, nil
		}
		if !k8serrors.IsAlreadyExists(err) {
			return nil, err
		}
		// TODO(jessesuen): switch from Get to List so that there's no guessing about which collision counter to use.
		existingRun, err := analysisRunIf.Get(ctx, run.Name, metav1.GetOptions{})
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

func NewAnalysisRunFromTemplates(templates []*v1alpha1.AnalysisTemplate, clusterTemplates []*v1alpha1.ClusterAnalysisTemplate, args []v1alpha1.Argument, name, generateName, namespace string) (*v1alpha1.AnalysisRun, error) {
	template, err := FlattenTemplates(templates, clusterTemplates)
	if err != nil {
		return nil, err
	}
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

func FlattenTemplates(templates []*v1alpha1.AnalysisTemplate, clusterTemplates []*v1alpha1.ClusterAnalysisTemplate) (*v1alpha1.AnalysisTemplate, error) {
	metrics, err := flattenMetrics(templates, clusterTemplates)
	if err != nil {
		return nil, err
	}
	args, err := flattenArgs(templates, clusterTemplates)
	if err != nil {
		return nil, err
	}
	return &v1alpha1.AnalysisTemplate{
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics: metrics,
			Args:    args,
		},
	}, nil
}

func flattenArgs(templates []*v1alpha1.AnalysisTemplate, clusterTemplates []*v1alpha1.ClusterAnalysisTemplate) ([]v1alpha1.Argument, error) {
	argsMap := map[string]v1alpha1.Argument{}

	var combinedArgs []v1alpha1.Argument

	for i := range templates {
		combinedArgs = append(combinedArgs, templates[i].Spec.Args...)
	}

	for i := range clusterTemplates {
		combinedArgs = append(combinedArgs, clusterTemplates[i].Spec.Args...)
	}

	for j := range combinedArgs {
		arg := combinedArgs[j]
		if storedArg, ok := argsMap[arg.Name]; ok {
			if arg.Value != nil && storedArg.Value != nil && *arg.Value != *storedArg.Value {
				return nil, fmt.Errorf("two args with the same name have the different values: arg %s", arg.Name)
			}
			// If the controller have a storedArg with a non-nul value, the storedArg should not be replaced by
			// the arg with a nil value
			if storedArg.Value != nil {
				continue
			}
		}
		argsMap[arg.Name] = arg
	}

	if len(argsMap) == 0 {
		return nil, nil
	}
	args := make([]v1alpha1.Argument, 0, len(argsMap))
	for name := range argsMap {
		arg := argsMap[name]
		args = append(args, arg)
	}
	return args, nil
}

func flattenMetrics(templates []*v1alpha1.AnalysisTemplate, clusterTemplates []*v1alpha1.ClusterAnalysisTemplate) ([]v1alpha1.Metric, error) {
	metricMap := map[string]v1alpha1.Metric{}

	var combinedMetrics []v1alpha1.Metric

	for i := range templates {
		combinedMetrics = append(combinedMetrics, templates[i].Spec.Metrics...)
	}

	for i := range clusterTemplates {
		combinedMetrics = append(combinedMetrics, clusterTemplates[i].Spec.Metrics...)
	}

	for j := range combinedMetrics {
		metric := combinedMetrics[j]
		if _, ok := metricMap[metric.Name]; !ok {
			metricMap[metric.Name] = metric
		} else {
			return nil, fmt.Errorf("two metrics have the same name %s", metric.Name)
		}
	}
	metrics := make([]v1alpha1.Metric, 0, len(metricMap))
	for name := range metricMap {
		metric := metricMap[name]
		metrics = append(metrics, metric)
	}
	return metrics, nil
}

func NewAnalysisRunFromUnstructured(obj *unstructured.Unstructured, templateArgs []v1alpha1.Argument, name, generateName, namespace string) (*unstructured.Unstructured, error) {
	var newArgs []v1alpha1.Argument

	objArgs, ok, err := unstructured.NestedSlice(obj.Object, "spec", "args")
	if err != nil {
		return nil, err
	}
	if !ok {
		// Args not set in AnalysisTemplate
		newArgs = templateArgs
	} else {
		var args []v1alpha1.Argument
		argBytes, err := json.Marshal(objArgs)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(argBytes, &args)
		if err != nil {
			return nil, err
		}
		newArgs, err = MergeArgs(templateArgs, args)
		if err != nil {
			return nil, err
		}
	}

	// Change kind to AnalysisRun
	err = unstructured.SetNestedField(obj.Object, "AnalysisRun", "kind")
	if err != nil {
		return nil, err
	}
	err = unstructured.SetNestedField(obj.Object, name, "metadata", "name")
	if err != nil {
		return nil, err
	}
	err = unstructured.SetNestedField(obj.Object, generateName, "metadata", "generateName")
	if err != nil {
		return nil, err
	}
	err = unstructured.SetNestedField(obj.Object, namespace, "metadata", "namespace")
	if err != nil {
		return nil, err
	}

	// Set args
	for i := 0; i < len(newArgs); i++ {
		var newArgInterface map[string]interface{}
		newArgBytes, err := json.Marshal(newArgs[i])
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(newArgBytes, &newArgInterface)
		if err != nil {
			return nil, err
		}
		err = unstructured.SetNestedMap(obj.Object, newArgInterface, field.NewPath("spec", "args").Index(i).String())
		if err != nil {
			return nil, err
		}
	}

	return obj, nil
}

//TODO(dthomson) remove v0.9.0
func NewAnalysisRunFromClusterTemplate(template *v1alpha1.ClusterAnalysisTemplate, args []v1alpha1.Argument, name, generateName, namespace string) (*v1alpha1.AnalysisRun, error) {
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

//TODO(dthomson) remove v0.9.0
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

// GetInstanceID takes an object and returns the controller instance id if it has one
func GetInstanceID(obj runtime.Object) string {
	objMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		// The objects passed into this function are already valid Kubernetes objects stored in the API server.
		// This function errors when the object passed can't be converted to a map[string]string. As a result,
		// the object passed in will never fail and the controller should panic in that case.
		panic(err)
	}
	uObj := unstructured.Unstructured{Object: objMap}
	labels := uObj.GetLabels()
	if labels != nil {
		return labels[v1alpha1.LabelKeyControllerInstanceID]
	}
	return ""
}
