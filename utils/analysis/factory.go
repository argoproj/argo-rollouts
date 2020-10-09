package analysis

import (
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// BuildArgumentsForRolloutAnalysisRun builds the arguments for a analysis base created by a rollout
func BuildArgumentsForRolloutAnalysisRun(args []v1alpha1.AnalysisRunArgument, stableRS, newRS *appsv1.ReplicaSet) []v1alpha1.Argument {
	arguments := []v1alpha1.Argument{}
	for i := range args {
		arg := args[i]
		value := arg.Value
		if arg.ValueFrom != nil {
			switch *arg.ValueFrom.PodTemplateHashValue {
			case v1alpha1.Latest:
				value = newRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
			case v1alpha1.Stable:
				value = stableRS.Labels[v1alpha1.DefaultRolloutUniqueLabelKey]
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
	if metric.Count > 0 {
		if metric.Count < metric.FailureLimit {
			return fmt.Errorf("count must be >= failureLimit")
		}
		if metric.Count < metric.InconclusiveLimit {
			return fmt.Errorf("count must be >= inconclusiveLimit")
		}
	}
	if metric.Count > 1 && metric.Interval == "" {
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

	if metric.FailureLimit < 0 {
		return fmt.Errorf("failureLimit must be >= 0")
	}
	if metric.InconclusiveLimit < 0 {
		return fmt.Errorf("inconclusiveLimit must be >= 0")
	}
	if metric.ConsecutiveErrorLimit != nil && *metric.ConsecutiveErrorLimit < 0 {
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
	if numProviders == 0 {
		return fmt.Errorf("no provider specified")
	}
	if numProviders > 1 {
		return fmt.Errorf("multiple providers specified")
	}
	return nil
}
