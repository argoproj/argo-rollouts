package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	log "github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	patchtypes "k8s.io/apimachinery/pkg/types"

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
		// If this metric is running in the dryRun mode then we don't care about the failures and hence the terminal
		// decision shouldn't be affected.
		if res.DryRun {
			continue
		}

		switch res.Phase {
		case v1alpha1.AnalysisPhaseFailed, v1alpha1.AnalysisPhaseError, v1alpha1.AnalysisPhaseInconclusive:
			return true
		}
	}
	return false
}

// GetMeasurementRetentionMetrics returns an array of metric names matching the RegEx rules from the MeasurementRetention rules.
func GetMeasurementRetentionMetrics(measurementRetentionMetrics []v1alpha1.MeasurementRetention, metrics []v1alpha1.Metric) (map[string]*v1alpha1.MeasurementRetention, error) {
	metricsMap := make(map[string]*v1alpha1.MeasurementRetention)
	if len(measurementRetentionMetrics) == 0 {
		return metricsMap, nil
	}
	// Iterate all the rules in `measurementRetentionMetrics` and try to match the `metrics` one by one
	for index, measurementRetentionObject := range measurementRetentionMetrics {
		matchCount := 0
		for _, metric := range metrics {
			if matched, _ := regexp.MatchString(measurementRetentionObject.MetricName, metric.Name); matched {
				metricsMap[metric.Name] = &measurementRetentionObject
				matchCount++
			}
		}
		if matchCount < 1 {
			return metricsMap, fmt.Errorf("measurementRetention[%d]: Rule didn't match any metric name(s)", index)
		}
	}
	return metricsMap, nil
}

// GetDryRunMetrics returns an array of metric names matching the RegEx rules from the Dry-Run metrics.
func GetDryRunMetrics(dryRunMetrics []v1alpha1.DryRun, metrics []v1alpha1.Metric) (map[string]bool, error) {
	metricsMap := make(map[string]bool)
	if len(dryRunMetrics) == 0 {
		return metricsMap, nil
	}
	// Iterate all the rules in `dryRunMetrics` and try to match the `metrics` one by one
	for index, dryRunObject := range dryRunMetrics {
		matchCount := 0
		for _, metric := range metrics {
			if matched, _ := regexp.MatchString(dryRunObject.MetricName, metric.Name); matched {
				metricsMap[metric.Name] = true
				matchCount++
			}
		}
		if matchCount < 1 {
			return metricsMap, fmt.Errorf("dryRun[%d]: Rule didn't match any metric name(s)", index)
		}
	}
	return metricsMap, nil
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

func ArrayMeasurement(run *v1alpha1.AnalysisRun, metricName string) []v1alpha1.Measurement {
	if result := GetResult(run, metricName); result != nil && len(result.Measurements) > 0 {
		return result.Measurements
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
	// NOTE: only consider metrics & args when comparing for semantic equality
	leftBytes, err := json.Marshal(v1alpha1.AnalysisRunSpec{Metrics: left.Metrics, DryRun: left.DryRun, MeasurementRetention: left.MeasurementRetention, Args: left.Args})
	if err != nil {
		panic(err)
	}
	rightBytes, err := json.Marshal(v1alpha1.AnalysisRunSpec{Metrics: right.Metrics, DryRun: right.DryRun, MeasurementRetention: right.MeasurementRetention, Args: right.Args})
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

func ResolveArgs(args []v1alpha1.Argument) error {
	for _, arg := range args {
		if arg.Value == nil && arg.ValueFrom == nil {
			return fmt.Errorf("args.%s was not resolved", arg.Name)
		}
	}
	return nil
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
	err := ResolveArgs(newArgs)
	if err != nil {
		return nil, err
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

func NewAnalysisRunFromTemplates(templates []*v1alpha1.AnalysisTemplate, clusterTemplates []*v1alpha1.ClusterAnalysisTemplate, args []v1alpha1.Argument, dryRunMetrics []v1alpha1.DryRun, measurementRetentionMetrics []v1alpha1.MeasurementRetention, name, generateName, namespace string) (*v1alpha1.AnalysisRun, error) {
	template, err := FlattenTemplates(templates, clusterTemplates)
	if err != nil {
		return nil, err
	}
	newArgs, err := MergeArgs(args, template.Spec.Args)
	if err != nil {
		return nil, err
	}
	dryRun, err := mergeDryRunMetrics(dryRunMetrics, template.Spec.DryRun)
	if err != nil {
		return nil, err
	}
	measurementRetention, err := mergeMeasurementRetentionMetrics(measurementRetentionMetrics, template.Spec.MeasurementRetention)
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
			Metrics:              template.Spec.Metrics,
			DryRun:               dryRun,
			MeasurementRetention: measurementRetention,
			Args:                 newArgs,
		},
	}
	return &ar, nil
}

func FlattenTemplates(templates []*v1alpha1.AnalysisTemplate, clusterTemplates []*v1alpha1.ClusterAnalysisTemplate) (*v1alpha1.AnalysisTemplate, error) {
	metrics, err := flattenMetrics(templates, clusterTemplates)
	if err != nil {
		return nil, err
	}
	dryRunMetrics, err := flattenDryRunMetrics(templates, clusterTemplates)
	if err != nil {
		return nil, err
	}
	measurementRetentionMetrics, err := flattenMeasurementRetentionMetrics(templates, clusterTemplates)
	if err != nil {
		return nil, err
	}
	args, err := flattenArgs(templates, clusterTemplates)
	if err != nil {
		return nil, err
	}
	return &v1alpha1.AnalysisTemplate{
		Spec: v1alpha1.AnalysisTemplateSpec{
			Metrics:              metrics,
			DryRun:               dryRunMetrics,
			MeasurementRetention: measurementRetentionMetrics,
			Args:                 args,
		},
	}, nil
}

func flattenArgs(templates []*v1alpha1.AnalysisTemplate, clusterTemplates []*v1alpha1.ClusterAnalysisTemplate) ([]v1alpha1.Argument, error) {
	var combinedArgs []v1alpha1.Argument
	appendOrUpdate := func(newArg v1alpha1.Argument) error {
		for i, prevArg := range combinedArgs {
			if prevArg.Name == newArg.Name {
				// found two args with same name. verify they have the same value, otherwise update
				// the combined args with the new non-nil value
				if prevArg.Value != nil && newArg.Value != nil && *prevArg.Value != *newArg.Value {
					return fmt.Errorf("Argument `%s` specified multiple times with different values: '%s', '%s'", prevArg.Name, *prevArg.Value, *newArg.Value)
				}
				// If previous arg value is already set (not nil), it should not be replaced by
				// a new arg with a nil value
				if prevArg.Value == nil {
					combinedArgs[i] = newArg
				}
				return nil
			}
		}
		combinedArgs = append(combinedArgs, newArg)
		return nil
	}

	for _, template := range templates {
		for _, arg := range template.Spec.Args {
			if err := appendOrUpdate(arg); err != nil {
				return nil, err
			}
		}
	}
	for _, template := range clusterTemplates {
		for _, arg := range template.Spec.Args {
			if err := appendOrUpdate(arg); err != nil {
				return nil, err
			}
		}
	}
	return combinedArgs, nil
}

func flattenMetrics(templates []*v1alpha1.AnalysisTemplate, clusterTemplates []*v1alpha1.ClusterAnalysisTemplate) ([]v1alpha1.Metric, error) {
	var combinedMetrics []v1alpha1.Metric
	for _, template := range templates {
		combinedMetrics = append(combinedMetrics, template.Spec.Metrics...)
	}

	for _, template := range clusterTemplates {
		combinedMetrics = append(combinedMetrics, template.Spec.Metrics...)
	}

	metricMap := map[string]bool{}
	for _, metric := range combinedMetrics {
		if _, ok := metricMap[metric.Name]; ok {
			return nil, fmt.Errorf("two metrics have the same name '%s'", metric.Name)
		}
		metricMap[metric.Name] = true
	}
	return combinedMetrics, nil
}

func mergeDryRunMetrics(leftDryRunMetrics []v1alpha1.DryRun, rightDryRunMetrics []v1alpha1.DryRun) ([]v1alpha1.DryRun, error) {
	var combinedDryRunMetrics []v1alpha1.DryRun
	combinedDryRunMetrics = append(combinedDryRunMetrics, leftDryRunMetrics...)
	combinedDryRunMetrics = append(combinedDryRunMetrics, rightDryRunMetrics...)

	err := validateDryRunMetrics(combinedDryRunMetrics)
	if err != nil {
		return nil, err
	}
	return combinedDryRunMetrics, nil
}

func mergeMeasurementRetentionMetrics(leftMeasurementRetentionMetrics []v1alpha1.MeasurementRetention, rightMeasurementRetentionMetrics []v1alpha1.MeasurementRetention) ([]v1alpha1.MeasurementRetention, error) {
	var combinedMeasurementRetentionMetrics []v1alpha1.MeasurementRetention
	combinedMeasurementRetentionMetrics = append(combinedMeasurementRetentionMetrics, leftMeasurementRetentionMetrics...)
	combinedMeasurementRetentionMetrics = append(combinedMeasurementRetentionMetrics, rightMeasurementRetentionMetrics...)

	err := validateMeasurementRetentionMetrics(combinedMeasurementRetentionMetrics)
	if err != nil {
		return nil, err
	}
	return combinedMeasurementRetentionMetrics, nil
}

func flattenDryRunMetrics(templates []*v1alpha1.AnalysisTemplate, clusterTemplates []*v1alpha1.ClusterAnalysisTemplate) ([]v1alpha1.DryRun, error) {
	var combinedDryRunMetrics []v1alpha1.DryRun
	for _, template := range templates {
		combinedDryRunMetrics = append(combinedDryRunMetrics, template.Spec.DryRun...)
	}

	for _, template := range clusterTemplates {
		combinedDryRunMetrics = append(combinedDryRunMetrics, template.Spec.DryRun...)
	}

	err := validateDryRunMetrics(combinedDryRunMetrics)
	if err != nil {
		return nil, err
	}
	return combinedDryRunMetrics, nil
}

func flattenMeasurementRetentionMetrics(templates []*v1alpha1.AnalysisTemplate, clusterTemplates []*v1alpha1.ClusterAnalysisTemplate) ([]v1alpha1.MeasurementRetention, error) {
	var combinedMeasurementRetentionMetrics []v1alpha1.MeasurementRetention
	for _, template := range templates {
		combinedMeasurementRetentionMetrics = append(combinedMeasurementRetentionMetrics, template.Spec.MeasurementRetention...)
	}

	for _, template := range clusterTemplates {
		combinedMeasurementRetentionMetrics = append(combinedMeasurementRetentionMetrics, template.Spec.MeasurementRetention...)
	}

	err := validateMeasurementRetentionMetrics(combinedMeasurementRetentionMetrics)
	if err != nil {
		return nil, err
	}
	return combinedMeasurementRetentionMetrics, nil
}

func validateDryRunMetrics(dryRunMetrics []v1alpha1.DryRun) error {
	metricMap := map[string]bool{}
	for _, dryRun := range dryRunMetrics {
		if _, ok := metricMap[dryRun.MetricName]; ok {
			return fmt.Errorf("two Dry-Run metric rules have the same name '%s'", dryRun.MetricName)
		}
		metricMap[dryRun.MetricName] = true
	}
	return nil
}

func validateMeasurementRetentionMetrics(measurementRetentionMetrics []v1alpha1.MeasurementRetention) error {
	metricMap := map[string]bool{}
	for _, measurementRetention := range measurementRetentionMetrics {
		if _, ok := metricMap[measurementRetention.MetricName]; ok {
			return fmt.Errorf("two Measurement Retention metric rules have the same name '%s'", measurementRetention.MetricName)
		}
		metricMap[measurementRetention.MetricName] = true
	}
	return nil
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

	// Remove resourceVersion if exists
	_, found, err := unstructured.NestedString(obj.Object, "metadata", "resourceVersion")
	if err != nil {
		return nil, err
	}
	if found {
		unstructured.RemoveNestedField(obj.Object, "metadata", "resourceVersion")
	}

	// Set args
	newArgVals := []interface{}{}
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
		newArgVals = append(newArgVals, newArgInterface)
	}
	if len(newArgVals) > 0 {
		err = unstructured.SetNestedSlice(obj.Object, newArgVals, "spec", "args")
		if err != nil {
			return nil, err
		}
	}

	return obj, nil
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
