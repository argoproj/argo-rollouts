package analysis

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	templateutil "github.com/argoproj/argo-rollouts/utils/template"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/kubernetes/pkg/fieldpath"
)

// BuildArgumentsForRolloutAnalysisRun builds the arguments for a analysis base created by a rollout
func BuildArgumentsForRolloutAnalysisRun(args []v1alpha1.AnalysisRunArgument, stableRS, newRS *appsv1.ReplicaSet, r *v1alpha1.Rollout) ([]v1alpha1.Argument, error) {
	var err error
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
			} else if arg.ValueFrom.FieldRef != nil {
				if strings.HasPrefix(arg.ValueFrom.FieldRef.FieldPath, "metadata") {
					value, err = fieldpath.ExtractFieldPathAsString(r, arg.ValueFrom.FieldRef.FieldPath)
					if err != nil {
						return nil, err
					}
				} else {
					// in case of error - return empty value for Validation stage, so it will pass validation
					// returned error will only be used in Analysis stage
					value, err = extractValueFromRollout(r, arg.ValueFrom.FieldRef.FieldPath)
				}
			}
		}

		analysisArg := v1alpha1.Argument{
			Name:  arg.Name,
			Value: &value,
		}
		arguments = append(arguments, analysisArg)
	}

	return arguments, err
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

// ResolveMetricArgs resolves args for single metric in AnalysisRun
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

// ValidateMetrics validates an analysis template spec
func ValidateMetrics(metrics []v1alpha1.Metric) error {
	if len(metrics) == 0 {
		return fmt.Errorf("no metrics specified")
	}
	duplicateNames := make(map[string]bool)
	for i, metric := range metrics {
		if _, ok := duplicateNames[metric.Name]; ok {
			return fmt.Errorf("metrics[%d]: duplicate name '%s'", i, metric.Name)
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
	if metric.Provider.CloudWatch != nil {
		numProviders++
	}
	if metric.Provider.Graphite != nil {
		numProviders++
	}
	if metric.Provider.Influxdb != nil {
		numProviders++
	}
	if metric.Provider.SkyWalking != nil {
		numProviders++
	}
	if metric.Provider.Plugin != nil && len(metric.Provider.Plugin) > 0 {
		// We allow exactly one plugin to be specified per analysis run template
		numProviders = numProviders + len(metric.Provider.Plugin)
	}
	if numProviders == 0 {
		return fmt.Errorf("no provider specified")
	}
	if numProviders > 1 {
		return fmt.Errorf("multiple providers specified")
	}
	return nil
}

func extractValueFromRollout(r *v1alpha1.Rollout, path string) (string, error) {
	j, _ := json.Marshal(r)
	m := interface{}(nil)
	json.Unmarshal(j, &m)
	sections := regexp.MustCompile("[\\.\\[\\]]+").Split(path, -1)
	for _, section := range sections {
		if section == "" {
			continue // if path ends with a separator char, Split returns an empty last section
		}

		if asArray, ok := m.([]interface{}); ok {
			if i, err := strconv.Atoi(section); err != nil {
				return "", fmt.Errorf("invalid index '%s'", section)
			} else if i >= len(asArray) {
				return "", fmt.Errorf("index %d out of range", i)
			} else {
				m = asArray[i]
			}
		} else if asMap, ok := m.(map[string]interface{}); ok {
			m = asMap[section]
		} else {
			return "", fmt.Errorf("invalid path %s in rollout", path)
		}
	}

	if m == nil {
		return "", fmt.Errorf("invalid path %s in rollout", path)
	}

	var isArray, isMap bool
	_, isArray = m.([]interface{})
	_, isMap = m.(map[string]interface{})
	if isArray || isMap {
		return "", fmt.Errorf("path %s in rollout must terminate in a primitive value", path)
	}

	return fmt.Sprintf("%v", m), nil
}
