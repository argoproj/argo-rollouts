package analysis

import (
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

// BuildArgumentsForRolloutAnalysisRun builds the arguments for a analysis base created by a rollout
func BuildArgumentsForRolloutAnalysisRun(rolloutAnalysisRun *v1alpha1.RolloutAnalysisStep, stableRS, newRS *appsv1.ReplicaSet) []v1alpha1.Argument {
	arguments := []v1alpha1.Argument{}
	for i := range rolloutAnalysisRun.Arguments {
		arg := rolloutAnalysisRun.Arguments[i]
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
			Value: value,
		}
		arguments = append(arguments, analysisArg)

	}
	return arguments
}

// StepLabels returns a map[string]string of common labels for analysisruns created from an analysis step
func StepLabels(r *v1alpha1.Rollout, index int32, podHash string) map[string]string {
	indexStr := strconv.Itoa(int(index))
	return map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
		v1alpha1.RolloutTypeLabel:             v1alpha1.RolloutTypeStepLabel,
		v1alpha1.RolloutCanaryStepIndexLabel:  indexStr,
	}
}

// ValidateAnalysisTemplateSpec validates an analysis template spec
func ValidateAnalysisTemplateSpec(spec v1alpha1.AnalysisTemplateSpec) error {
	if len(spec.Metrics) == 0 {
		return fmt.Errorf("no metrics specified")
	}
	duplicateNames := make(map[string]bool)
	for i, metric := range spec.Metrics {
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
	if metric.Count < metric.MaxFailures {
		return fmt.Errorf("count must be >= maxFailures")
	}
	if metric.Count < metric.MaxInconclusive {
		return fmt.Errorf("count must be >= maxInconclusive")
	}
	if metric.Count > 1 && metric.Interval == nil {
		return fmt.Errorf("interval must be specified when count > 1")
	}
	if metric.MaxFailures < 0 {
		return fmt.Errorf("maxFailures must be >= 0")
	}
	if metric.MaxInconclusive < 0 {
		return fmt.Errorf("maxInconclusive must be >= 0")
	}
	if metric.MaxConsecutiveErrors != nil && *metric.MaxConsecutiveErrors < 0 {
		return fmt.Errorf("maxConsecutiveErrors must be >= 0")
	}
	numProviders := 0
	if metric.Provider.Prometheus != nil {
		numProviders++
	}
	if metric.Provider.Job != nil {
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
