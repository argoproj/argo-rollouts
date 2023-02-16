package defaults

import (
	"os"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

const (
	// DefaultReplicas default number of replicas for a rollout if the .Spec.Replicas is nil
	DefaultReplicas = int32(1)
	// DefaultRevisionHistoryLimit default number of revisions to keep if .Spec.RevisionHistoryLimit is nil
	DefaultRevisionHistoryLimit = int32(10)
	// DefaultAnalysisRunSuccessfulHistoryLimit default number of successful AnalysisRuns to keep if .Spec.Analysis.SuccessfulRunHistoryLimit is nil
	DefaultAnalysisRunSuccessfulHistoryLimit = int32(5)
	// DefaultAnalysisRunUnsuccessfulHistoryLimit default number of unsuccessful AnalysisRuns to keep if .Spec.Analysis.UnsuccessfulRunHistoryLimit is nil
	DefaultAnalysisRunUnsuccessfulHistoryLimit = int32(5)
	// DefaultMaxSurge default number for the max number of additional pods that can be brought up during a rollout
	DefaultMaxSurge = "25"
	// DefaultMaxUnavailable default number for the max number of unavailable pods during a rollout
	DefaultMaxUnavailable = "25"
	// DefaultProgressDeadlineSeconds default number of seconds for the rollout to be making progress
	DefaultProgressDeadlineSeconds = int32(600)
	// DefaultScaleDownDelaySeconds default seconds before scaling down old replicaset after switching services
	DefaultScaleDownDelaySeconds = int32(30)
	// DefaultAbortScaleDownDelaySeconds default seconds before scaling down old replicaset after switching services
	DefaultAbortScaleDownDelaySeconds = int32(30)
	// DefaultAutoPromotionEnabled default value for auto promoting a blueGreen strategy
	DefaultAutoPromotionEnabled = true
	// DefaultConsecutiveErrorLimit is the default number times a metric can error in sequence before
	// erroring the entire metric.
	DefaultConsecutiveErrorLimit int32 = 4
	// DefaultQPS is the default Queries Per Second (QPS) for client side throttling to the K8s API server
	DefaultQPS float32 = 40.0
	// DefaultBurst is the default value for Burst for client side throttling to the K8s API server
	DefaultBurst int = 80
	// DefaultAwsLoadBalancerPageSize is the default page size used when calling aws to get load balancers by DNS name
	DefaultAwsLoadBalancerPageSize = int32(300)
	// DefaultMetricCleanupDelay is the default time to delay metrics removal upon object removal, gives time for metrics
	// to be collected
	DefaultMetricCleanupDelay = int32(65)
	// DefaultRolloutsConfigMapName is the default name of the ConfigMap that contains the Rollouts controller configuration
	DefaultRolloutsConfigMapName = "argo-rollouts-config"
	// DefaultRolloutPluginFolder is the default location where plugins will be downloaded and/or moved to.
	DefaultRolloutPluginFolder = "plugin-bin"
	// DefaultDescribeTagsLimit is the default number resources (ARNs) in a single call
	DefaultDescribeTagsLimit int = 20
)

const (
	DefaultAmbassadorAPIGroup           = "getambassador.io"
	DefaultAmbassadorVersion            = "getambassador.io/v2"
	DefaultIstioVersion                 = "v1alpha3"
	DefaultSMITrafficSplitVersion       = "v1alpha1"
	DefaultTargetGroupBindingAPIVersion = "elbv2.k8s.aws/v1beta1"
	DefaultAppMeshCRDVersion            = "v1beta2"
	DefaultTraefikAPIGroup              = "traefik.containo.us"
	DefaultTraefikVersion               = "traefik.containo.us/v1alpha1"
	DefaultApisixAPIGroup               = "apisix.apache.org"
	DefaultApisixVersion                = "apisix.apache.org/v2"
)

var (
	defaultVerifyTargetGroup     = false
	istioAPIVersion              = DefaultIstioVersion
	ambassadorAPIVersion         = DefaultAmbassadorVersion
	smiAPIVersion                = DefaultSMITrafficSplitVersion
	targetGroupBindingAPIVersion = DefaultTargetGroupBindingAPIVersion
	appmeshCRDVersion            = DefaultAppMeshCRDVersion
	defaultMetricCleanupDelay    = DefaultMetricCleanupDelay
	defaultDescribeTagsLimit     = DefaultDescribeTagsLimit
)

const (
	// EnvVarRolloutVerifyRetryInterval is the interval duration in seconds to requeue a rollout upon errors
	EnvVarRolloutVerifyRetryInterval = "ROLLOUT_VERIFY_RETRY_INTERVAL"
)

var (
	rolloutVerifyRetryInterval time.Duration = 10 * time.Second
)

func init() {
	if rolloutVerifyInterval, ok := os.LookupEnv(EnvVarRolloutVerifyRetryInterval); ok {
		if interval, err := strconv.ParseInt(rolloutVerifyInterval, 10, 32); err != nil {
			rolloutVerifyRetryInterval = time.Duration(interval) * time.Second
		}
	}
}

func GetStringOrDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	} else {
		return value
	}
}

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

// GetAnalysisRunSuccessfulHistoryLimitOrDefault returns the specified number of succeed AnalysisRuns to keep or the default number
func GetAnalysisRunSuccessfulHistoryLimitOrDefault(rollout *v1alpha1.Rollout) int32 {
	if rollout.Spec.Analysis == nil || rollout.Spec.Analysis.SuccessfulRunHistoryLimit == nil {
		return DefaultAnalysisRunSuccessfulHistoryLimit
	}
	return *rollout.Spec.Analysis.SuccessfulRunHistoryLimit
}

// GetAnalysisRunUnsuccessfulHistoryLimitOrDefault returns the specified number of failed AnalysisRuns to keep or the default number
func GetAnalysisRunUnsuccessfulHistoryLimitOrDefault(rollout *v1alpha1.Rollout) int32 {
	if rollout.Spec.Analysis == nil || rollout.Spec.Analysis.UnsuccessfulRunHistoryLimit == nil {
		return DefaultAnalysisRunUnsuccessfulHistoryLimit
	}
	return *rollout.Spec.Analysis.UnsuccessfulRunHistoryLimit
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

func GetExperimentScaleDownDelaySecondsOrDefault(e *v1alpha1.Experiment) int32 {
	if e.Spec.ScaleDownDelaySeconds != nil {
		return *e.Spec.ScaleDownDelaySeconds
	}
	return DefaultScaleDownDelaySeconds
}

func GetScaleDownDelaySecondsOrDefault(rollout *v1alpha1.Rollout) time.Duration {
	var delaySeconds int32
	if rollout.Spec.Strategy.BlueGreen != nil {
		delaySeconds = DefaultScaleDownDelaySeconds
		if rollout.Spec.Strategy.BlueGreen.ScaleDownDelaySeconds != nil {
			delaySeconds = *rollout.Spec.Strategy.BlueGreen.ScaleDownDelaySeconds
		}
	}
	if rollout.Spec.Strategy.Canary != nil {
		if rollout.Spec.Strategy.Canary.TrafficRouting != nil {
			delaySeconds = DefaultScaleDownDelaySeconds
			if rollout.Spec.Strategy.Canary.ScaleDownDelaySeconds != nil {
				delaySeconds = *rollout.Spec.Strategy.Canary.ScaleDownDelaySeconds
			}
		}
	}
	return time.Duration(delaySeconds) * time.Second
}

// GetAbortScaleDownDelaySecondsOrDefault returns the duration to delay the scale down of
// the canary/preview ReplicaSet in an abort situation. A nil value indicates it should not
// scale down at all (abortScaleDownDelaySeconds: 0). A value of 0 indicates it should scale down
// immediately. Also returns a boolean to indicate if the value was explicitly set.
func GetAbortScaleDownDelaySecondsOrDefault(rollout *v1alpha1.Rollout) (*time.Duration, bool) {
	var delaySeconds int32
	wasSet := false
	if rollout.Spec.Strategy.BlueGreen != nil {
		delaySeconds = DefaultAbortScaleDownDelaySeconds
		if rollout.Spec.Strategy.BlueGreen.AbortScaleDownDelaySeconds != nil {
			wasSet = true
			if *rollout.Spec.Strategy.BlueGreen.AbortScaleDownDelaySeconds == 0 {
				return nil, wasSet
			}
			delaySeconds = *rollout.Spec.Strategy.BlueGreen.AbortScaleDownDelaySeconds
		}
	} else if rollout.Spec.Strategy.Canary != nil {
		if rollout.Spec.Strategy.Canary.TrafficRouting != nil {
			delaySeconds = DefaultAbortScaleDownDelaySeconds
			if rollout.Spec.Strategy.Canary.AbortScaleDownDelaySeconds != nil {
				wasSet = true
				if *rollout.Spec.Strategy.Canary.AbortScaleDownDelaySeconds == 0 {
					return nil, wasSet
				}
				delaySeconds = *rollout.Spec.Strategy.Canary.AbortScaleDownDelaySeconds
			}
		}
	}
	dur := time.Duration(delaySeconds) * time.Second
	return &dur, wasSet
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
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if ns := strings.TrimSpace(string(data)); len(ns) > 0 {
			return ns
		}
	}
	return "argo-rollouts"
}

// SetDefaultVerifyTargetGroup sets the default setWeight verification when instantiating the reconciler
func SetVerifyTargetGroup(b bool) {
	defaultVerifyTargetGroup = b
}

// VerifyTargetGroup returns whether or not we should verify target groups
func VerifyTargetGroup() bool {
	return defaultVerifyTargetGroup
}

func SetIstioAPIVersion(apiVersion string) {
	istioAPIVersion = apiVersion
}

func GetIstioAPIVersion() string {
	return istioAPIVersion
}

func SetAmbassadorAPIVersion(apiVersion string) {
	ambassadorAPIVersion = apiVersion
}

func GetAmbassadorAPIVersion() string {
	return ambassadorAPIVersion
}

func SetAppMeshCRDVersion(apiVersion string) {
	appmeshCRDVersion = apiVersion
}

func GetAppMeshCRDVersion() string {
	return appmeshCRDVersion
}

func SetSMIAPIVersion(apiVersion string) {
	smiAPIVersion = apiVersion
}

func GetSMIAPIVersion() string {
	return smiAPIVersion
}

func SetTargetGroupBindingAPIVersion(apiVersion string) {
	targetGroupBindingAPIVersion = apiVersion
}

func GetTargetGroupBindingAPIVersion() string {
	return targetGroupBindingAPIVersion
}

func GetRolloutVerifyRetryInterval() time.Duration {
	return rolloutVerifyRetryInterval
}

// GetMetricCleanupDelaySeconds returns the duration to delay the cleanup of metrics
func GetMetricCleanupDelaySeconds() time.Duration {
	return time.Duration(defaultMetricCleanupDelay) * time.Second
}

// SetMetricCleanupDelaySeconds sets the metric cleanup delay in seconds
func SetMetricCleanupDelaySeconds(seconds int32) {
	defaultMetricCleanupDelay = seconds
}

// GetDescribeTagsLimit returns limit of resources can be requested in a single call
func GetDescribeTagsLimit() int {
	return defaultDescribeTagsLimit
}

// SetDescribeTagsLimit sets the limit of resources can be requested in a single call
func SetDescribeTagsLimit(limit int) {
	defaultDescribeTagsLimit = limit
}
