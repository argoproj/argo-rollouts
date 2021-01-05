package analysis

import (
	"encoding/json"
	"fmt"
	"strconv"

	templateutil "github.com/argoproj/argo-rollouts/utils/template"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/kubernetes/pkg/fieldpath"
)

// BuildArgumentsForRolloutAnalysisRun builds the arguments for a analysis base created by a rollout
func BuildArgumentsForRolloutAnalysisRun(args []v1alpha1.AnalysisRunArgument, stableRS, newRS *appsv1.ReplicaSet, r *v1alpha1.Rollout) []v1alpha1.Argument {
	arguments := []v1alpha1.Argument{}
	for i := range args {
		arg := args[i]
		value := arg.Value
		if arg.ValueFrom != nil {
			if arg.ValueFrom.PodTemplateHashValue != nil {
				switch *arg.ValueFrom.PodTemplateHashValue {
				case v1alpha1.Latest:
					value = newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
				case v1alpha1.Stable:
					value = stableRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
				}
			} else {
				if arg.ValueFrom.FieldRef != nil {
					value, _ = fieldpath.ExtractFieldPathAsString(r, arg.ValueFrom.FieldRef.FieldPath)
				}
			}

		}
		analysisArg := v1alpha1.Argument{
			Name:  arg.Name,
			Value: &value,
		}
		arguments = append(arguments, analysisArg)

	}
	return arguments
}

// PostPromotionLabels returns a map[string]string of common labels for the post promotion analysis
func PostPromotionLabels(podHash, instanceID string) map[string]string {
	labels := map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
		v1alpha1.RolloutTypeLabel:             v1alpha1.RolloutTypePostPromotionLabel,
	}
	if instanceID != "" {
		labels[v1alpha1.LabelKeyControllerInstanceID] = instanceID
	}
	return labels

}

// PrePromotionLabels returns a map[string]string of common labels for the pre promotion analysis
func PrePromotionLabels(podHash, instanceID string) map[string]string {
	labels := map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
		v1alpha1.RolloutTypeLabel:             v1alpha1.RolloutTypePrePromotionLabel,
	}
	if instanceID != "" {
		labels[v1alpha1.LabelKeyControllerInstanceID] = instanceID
	}
	return labels

}

// BackgroundLabels returns a map[string]string of common labels for the background analysis
func BackgroundLabels(podHash, instanceID string) map[string]string {
	labels := map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
		v1alpha1.RolloutTypeLabel:             v1alpha1.RolloutTypeBackgroundRunLabel,
	}
	if instanceID != "" {
		labels[v1alpha1.LabelKeyControllerInstanceID] = instanceID
	}
	return labels

}

// StepLabels returns a map[string]string of common labels for analysisruns created from an analysis step
func StepLabels(index int32, podHash, instanceID string) map[string]string {
	indexStr := strconv.Itoa(int(index))
	labels := map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
		v1alpha1.RolloutTypeLabel:             v1alpha1.RolloutTypeStepLabel,
		v1alpha1.RolloutCanaryStepIndexLabel:  indexStr,
	}
	if instanceID != "" {
		labels[v1alpha1.LabelKeyControllerInstanceID] = instanceID
	}
	return labels
}

// resolveMetricArgs resolves args for single metric in AnalysisRun
// Returns resolved metric
// Uses ResolveQuotedArgs to handle escaped quotes
func ResolveMetricArgs(metric v1alpha1.Metric, args []v1alpha1.Argument) (*v1alpha1.Metric, error) {
	metricBytes, err := json.Marshal(metric)
	if err != nil {
		return nil, err
	}
	var newMetricStr string
	newMetricStr, err = templateutil.ResolveQuotedArgs(string(metricBytes), args)
	if err != nil {
		return nil, err
	}
	var newMetric v1alpha1.Metric
	err = json.Unmarshal([]byte(newMetricStr), &newMetric)
	if err != nil {
		return nil, err
	}
	return &newMetric, nil
}

func ResolveMetrics(metrics []v1alpha1.Metric, args []v1alpha1.Argument) ([]v1alpha1.Metric, error) {
	for i, arg := range args {
		if arg.ValueFrom != nil {
			if arg.Value != nil {
				return nil, fmt.Errorf("arg '%s' has both Value and ValueFrom fields", arg.Name)
			}
			argVal := "dummy-value"
			args[i].Value = &argVal
		}
	}

	for i, metric := range metrics {
		resolvedMetric, err := ResolveMetricArgs(metric, args)
		if err != nil {
			return nil, err
		}
		metrics[i] = *resolvedMetric
	}
	return metrics, nil
}

// ValidateMetrics validates an analysis template spec
func ValidateMetrics(metrics []v1alpha1.Metric) error {
	if len(metrics) == 0 {
		return fmt.Errorf("no metrics specified")
	}
	duplicateNames := make(map[string]bool)
	for i, metric := range metrics {
		if _, ok := duplicateNames[metric.Name]; ok {
			return fmt.Errorf("metrics[%d]: duplicate name '%s", i, metric.Name)
		}
		duplicateNames[metric.Name] = true
		if err := ValidateMetric(metric); err != nil {
			return fmt.Errorf("metrics[%d]: %v", i, err)
		}
	}
	return nil
}

// ValidateMetric validates a single metric spec
func ValidateMetric(metric v1alpha1.Metric) error {
	count := 0
	if metric.Count != nil {
		count = metric.Count.IntValue()
	}

	failureLimit := 0
	if metric.FailureLimit != nil {
		failureLimit = metric.FailureLimit.IntValue()
	}

	inconclusiveLimit := 0
	if metric.InconclusiveLimit != nil {
		inconclusiveLimit = metric.InconclusiveLimit.IntValue()
	}

	if count > 0 {
		if count < failureLimit {
			return fmt.Errorf("count must be >= failureLimit")
		}
		if count < inconclusiveLimit {
			return fmt.Errorf("count must be >= inconclusiveLimit")
		}
	}
	if count > 1 && metric.Interval == "" {
		return fmt.Errorf("interval must be specified when count > 1")
	}
	if metric.Interval != "" {
		if _, err := metric.Interval.Duration(); err != nil {
			return fmt.Errorf("invalid interval string: %v", err)
		}
	}
	if metric.InitialDelay != "" {
		if _, err := metric.InitialDelay.Duration(); err != nil {
			return fmt.Errorf("invalid startDelay string: %v", err)
		}
	}

	if failureLimit < 0 {
		return fmt.Errorf("failureLimit must be >= 0")
	}
	if inconclusiveLimit < 0 {
		return fmt.Errorf("inconclusiveLimit must be >= 0")
	}

	if metric.ConsecutiveErrorLimit != nil && metric.ConsecutiveErrorLimit.IntValue() < 0 {
		return fmt.Errorf("consecutiveErrorLimit must be >= 0")
	}
	numProviders := 0
	if metric.Provider.Prometheus != nil {
		numProviders++
	}
	if metric.Provider.Job != nil {
		numProviders++
	}
	if metric.Provider.Web != nil {
		numProviders++
	}
	if metric.Provider.Wavefront != nil {
		numProviders++
	}
	if metric.Provider.Kayenta != nil {
		numProviders++
	}
	if metric.Provider.Datadog != nil {
		numProviders++
	}
	if metric.Provider.NewRelic != nil {
		numProviders++
	}
	if numProviders == 0 {
		return fmt.Errorf("no provider specified")
	}
	if numProviders > 1 {
		return fmt.Errorf("multiple providers specified")
	}
	return nil
}
