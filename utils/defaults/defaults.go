package defaults

import (
	"io/ioutil"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	// DefaultReplicas default number of replicas for a rollout if the .Spec.Replicas is nil
	DefaultReplicas = int32(1)
	// DefaultRevisionHistoryLimit default number of revisions to keep if .Spec.RevisionHistoryLimit is nil
	DefaultRevisionHistoryLimit = int32(10)
	// DefaultMaxSurge default number for the max number of additional pods that can be brought up during a rollout
	DefaultMaxSurge = "25"
	// DefaultMaxUnavailable default number for the max number of unavailable pods during a rollout
	DefaultMaxUnavailable = "25"
	// DefaultProgressDeadlineSeconds default number of seconds for the rollout to be making progress
	DefaultProgressDeadlineSeconds = int32(600)
	// DefaultScaleDownDelaySeconds default seconds before scaling down old replicaset after switching services
	DefaultScaleDownDelaySeconds = int32(30)
	// DefaultAutoPromotionEnabled default value for auto promoting a blueGreen strategy
	DefaultAutoPromotionEnabled = true
	// DefaultConsecutiveErrorLimit is the default number times a metric can error in sequence before
	// erroring the entire metric.
	DefaultConsecutiveErrorLimit int32 = 4
)

const (
	DefaultAmbassadorVersion      = "v2"
	DefaultIstioVersion           = "v1alpha3"
	DefaultSMITrafficSplitVersion = "v1alpha1"
)

// GetReplicasOrDefault returns the deferenced number of replicas or the default number
func GetReplicasOrDefault(replicas *int32) int32 {
	if replicas == nil {
		return DefaultReplicas
	}
	return *replicas
}

// GetRevisionHistoryLimitOrDefault returns the specified number of replicas in a rollout or the default number
func GetRevisionHistoryLimitOrDefault(rollout *v1alpha1.Rollout) int32 {
	if rollout.Spec.RevisionHistoryLimit == nil {
		return DefaultRevisionHistoryLimit
	}
	return *rollout.Spec.RevisionHistoryLimit
}

func GetMaxSurgeOrDefault(rollout *v1alpha1.Rollout) *intstr.IntOrString {
	if rollout.Spec.Strategy.Canary != nil && rollout.Spec.Strategy.Canary.MaxSurge != nil {
		return rollout.Spec.Strategy.Canary.MaxSurge
	}
	defaultValue := intstr.FromString(DefaultMaxSurge)
	return &defaultValue
}

func GetMaxUnavailableOrDefault(rollout *v1alpha1.Rollout) *intstr.IntOrString {
	if rollout.Spec.Strategy.BlueGreen != nil && rollout.Spec.Strategy.BlueGreen.MaxUnavailable != nil {
		return rollout.Spec.Strategy.BlueGreen.MaxUnavailable
	}
	if rollout.Spec.Strategy.Canary != nil && rollout.Spec.Strategy.Canary.MaxUnavailable != nil {
		return rollout.Spec.Strategy.Canary.MaxUnavailable
	}
	defaultValue := intstr.FromString(DefaultMaxUnavailable)
	return &defaultValue
}

func GetCanaryIngressAnnotationPrefixOrDefault(rollout *v1alpha1.Rollout) string {
	if rollout.Spec.Strategy.Canary != nil && rollout.Spec.Strategy.Canary.TrafficRouting != nil && rollout.Spec.Strategy.Canary.TrafficRouting.Nginx != nil && rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.AnnotationPrefix != "" {
		return rollout.Spec.Strategy.Canary.TrafficRouting.Nginx.AnnotationPrefix
	}
	return "nginx.ingress.kubernetes.io"
}

func GetProgressDeadlineSecondsOrDefault(rollout *v1alpha1.Rollout) int32 {
	if rollout.Spec.ProgressDeadlineSeconds != nil {
		return *rollout.Spec.ProgressDeadlineSeconds
	}
	return DefaultProgressDeadlineSeconds
}

func GetExperimentProgressDeadlineSecondsOrDefault(e *v1alpha1.Experiment) int32 {
	if e.Spec.ProgressDeadlineSeconds != nil {
		return *e.Spec.ProgressDeadlineSeconds
	}
	return DefaultProgressDeadlineSeconds
}

func GetScaleDownDelaySecondsOrDefault(rollout *v1alpha1.Rollout) int32 {
	if rollout.Spec.Strategy.BlueGreen != nil {
		if rollout.Spec.Strategy.BlueGreen.ScaleDownDelaySeconds != nil {
			return *rollout.Spec.Strategy.BlueGreen.ScaleDownDelaySeconds
		}
		return DefaultScaleDownDelaySeconds
	}
	if rollout.Spec.Strategy.Canary != nil {
		if rollout.Spec.Strategy.Canary.TrafficRouting != nil {
			if rollout.Spec.Strategy.Canary.ScaleDownDelaySeconds != nil {
				return *rollout.Spec.Strategy.Canary.ScaleDownDelaySeconds
			}
			return DefaultScaleDownDelaySeconds
		}
	}
	return 0
}

func GetAutoPromotionEnabledOrDefault(rollout *v1alpha1.Rollout) bool {
	if rollout.Spec.Strategy.BlueGreen == nil {
		return DefaultAutoPromotionEnabled
	}
	if rollout.Spec.Strategy.BlueGreen.AutoPromotionEnabled == nil {
		return DefaultAutoPromotionEnabled
	}
	return *rollout.Spec.Strategy.BlueGreen.AutoPromotionEnabled
}

func GetConsecutiveErrorLimitOrDefault(metric *v1alpha1.Metric) int32 {
	if metric.ConsecutiveErrorLimit != nil {
		return int32(metric.ConsecutiveErrorLimit.IntValue())
	}
	return DefaultConsecutiveErrorLimit
}

func Namespace() string {
	// This way assumes you've set the POD_NAMESPACE environment variable using the downward API.
	// This check has to be done first for backwards compatibility with the way InClusterConfig was originally set up
	if ns, ok := os.LookupEnv("POD_NAMESPACE"); ok {
		return ns
	}
	// Fall back to the namespace associated with the service account token, if available
	if data, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if ns := strings.TrimSpace(string(data)); len(ns) > 0 {
			return ns
		}
	}
	return "argo-rollouts"
}
