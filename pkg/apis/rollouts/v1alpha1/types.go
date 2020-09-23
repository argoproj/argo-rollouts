package v1alpha1

import (
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:path=rollouts,shortName=ro
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.HPAReplicas,selectorpath=.status.selector
// +kubebuilder:printcolumn:name="Desired",type="integer",JSONPath=".spec.replicas",description="Number of desired pods"
// +kubebuilder:printcolumn:name="Current",type="integer",JSONPath=".status.replicas",description="Total number of non-terminated pods targeted by this rollout"
// +kubebuilder:printcolumn:name="Up-to-date",type="integer",JSONPath=".status.updatedReplicas",description="Total number of non-terminated pods targeted by this rollout that have the desired template spec"
// +kubebuilder:printcolumn:name="Available",type="integer",JSONPath=".status.availableReplicas",description="Total number of available pods (ready for at least minReadySeconds) targeted by this rollout"

// Rollout is a specification for a Rollout resource
type Rollout struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RolloutSpec   `json:"spec"`
	Status RolloutStatus `json:"status,omitempty"`
}

// RolloutSpec is the spec for a Rollout resource
type RolloutSpec struct {
	// Number of desired pods. This is a pointer to distinguish between explicit
	// zero and not specified. Defaults to 1.
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// Label selector for pods. Existing ReplicaSets whose pods are
	// selected by this will be the ones affected by this rollout.
	// It must match the pod template's labels.
	Selector *metav1.LabelSelector `json:"selector"`
	// Template describes the pods that will be created.
	Template corev1.PodTemplateSpec `json:"template"`
	// Minimum number of seconds for which a newly created pod should be ready
	// without any of its container crashing, for it to be considered available.
	// Defaults to 0 (pod will be considered available as soon as it is ready)
	// +optional
	MinReadySeconds int32 `json:"minReadySeconds,omitempty"`
	// The deployment strategy to use to replace existing pods with new ones.
	// +optional
	Strategy RolloutStrategy `json:"strategy"`
	// The number of old ReplicaSets to retain. If unspecified, will retain 10 old ReplicaSets
	RevisionHistoryLimit *int32 `json:"revisionHistoryLimit,omitempty"`
	// Paused pauses the rollout at its current step.
	Paused bool `json:"paused,omitempty"`
	// ProgressDeadlineSeconds The maximum time in seconds for a rollout to
	// make progress before it is considered to be failed. Argo Rollouts will
	// continue to process failed rollouts and a condition with a
	// ProgressDeadlineExceeded reason will be surfaced in the rollout status.
	// Note that progress will not be estimated during the time a rollout is paused.
	// Defaults to 600s.
	ProgressDeadlineSeconds *int32 `json:"progressDeadlineSeconds,omitempty"`
	// RestartAt indicates when all the pods of a Rollout should be restarted
	RestartAt *metav1.Time `json:"restartAt,omitempty"`
}

const (
	// DefaultRolloutUniqueLabelKey is the default key of the selector that is added
	// to existing ReplicaSets (and label key that is added to its pods) to prevent the existing ReplicaSets
	// to select new pods (and old pods being select by new ReplicaSet).
	DefaultRolloutUniqueLabelKey string = "rollouts-pod-template-hash"
	// DefaultReplicaSetScaleDownDeadlineAnnotationKey is the default key attached to an old stable ReplicaSet after
	// the rollout transitioned to a new version. It contains the time when the controller can scale down the RS.
	DefaultReplicaSetScaleDownDeadlineAnnotationKey = "scale-down-deadline"
	// ManagedByRolloutKey is the key used to indicate which rollout(s) manage a resource but doesn't own it.
	ManagedByRolloutsKey = "argo-rollouts.argoproj.io/managed-by-rollouts"
	// DefaultReplicaSetRestartAnnotationKey indicates that the ReplicaSet with this annotation was restarted at the
	// time listed in the value
	DefaultReplicaSetRestartAnnotationKey = "argo-rollouts.argoproj.io/restarted-after"
	// LabelKeyControllerInstanceID is the label the controller uses for the rollout, experiment, analysis segregation
	// between controllers. Controllers will only operate on objects with the same instanceID as the controller.
	LabelKeyControllerInstanceID = "argo-rollouts.argoproj.io/controller-instance-id"
)

// RolloutStrategy defines strategy to apply during next rollout
type RolloutStrategy struct {
	// +optional
	BlueGreen *BlueGreenStrategy `json:"blueGreen,omitempty"`
	// +optional
	Canary *CanaryStrategy `json:"canary,omitempty"`
}

// BlueGreenStrategy defines parameters for Blue Green deployment
type BlueGreenStrategy struct {
	// Name of the service that the rollout modifies as the active service.
	ActiveService string `json:"activeService"`
	// Name of the service that the rollout modifies as the preview service.
	// +optional
	PreviewService string `json:"previewService,omitempty"`
	// PreviewReplica the number of replicas to run under the preview service before the switchover. Once the rollout is
	// resumed the new replicaset will be full scaled up before the switch occurs
	// +optional
	PreviewReplicaCount *int32 `json:"previewReplicaCount,omitempty"`
	// AutoPromotionEnabled indicates if the rollout should automatically promote the new ReplicaSet
	// to the active service or enter a paused state. If not specified, the default value is true.
	// +optional
	AutoPromotionEnabled *bool `json:"autoPromotionEnabled,omitempty"`
	// AutoPromotionSeconds automatically promotes the current ReplicaSet to active after the
	// specified pause delay in seconds after the ReplicaSet becomes ready.
	// If omitted, the Rollout enters and remains in a paused state until manually resumed by
	// removing the pause condition.
	// +optional
	AutoPromotionSeconds *int32 `json:"autoPromotionSeconds,omitempty"`
	// ScaleDownDelaySeconds adds a delay before scaling down the previous replicaset.
	// If omitted, the Rollout waits 30 seconds before scaling down the previous ReplicaSet.
	// A minimum of 30 seconds is recommended to ensure IP table propagation across the nodes in
	// a cluster. See https://github.com/argoproj/argo-rollouts/issues/19#issuecomment-476329960 for
	// more information
	// +optional
	ScaleDownDelaySeconds *int32 `json:"scaleDownDelaySeconds,omitempty"`
	// ScaleDownDelayRevisionLimit limits the number of old RS that can run at one time before getting scaled down
	// +optional
	ScaleDownDelayRevisionLimit *int32 `json:"scaleDownDelayRevisionLimit,omitempty"`
	// PrePromotionAnalysis configuration to run analysis before a selector switch
	PrePromotionAnalysis *RolloutAnalysis `json:"prePromotionAnalysis,omitempty"`
	// AntiAffinity enables anti-affinity rules for Blue Green deployment
	// +optional
	AntiAffinity *AntiAffinity `json:"antiAffinity,omitempty"`
	// PostPromotionAnalysis configuration to run analysis after a selector switch
	PostPromotionAnalysis *RolloutAnalysis `json:"postPromotionAnalysis,omitempty"`
}

// AntiAffinity defines which inter-pod scheduling rule to use for anti-affinity injection
type AntiAffinity struct {
	// +optional
	PreferredDuringSchedulingIgnoredDuringExecution *PreferredDuringSchedulingIgnoredDuringExecution `json:"preferredDuringSchedulingIgnoredDuringExecution,omitempty"`
	// +optional
	RequiredDuringSchedulingIgnoredDuringExecution *RequiredDuringSchedulingIgnoredDuringExecution `json:"requiredDuringSchedulingIgnoredDuringExecution,omitempty"`
}

// PreferredDuringSchedulingIgnoredDuringExecution defines the weight of the anti-affinity injection
type PreferredDuringSchedulingIgnoredDuringExecution struct {
	// Weight associated with matching the corresponding podAffinityTerm, in the range 1-100.
	Weight int32 `json:"weight"`
}

// RequiredDuringSchedulingIgnoredDuringExecution defines inter-pod scheduling rule to be RequiredDuringSchedulingIgnoredDuringExecution
type RequiredDuringSchedulingIgnoredDuringExecution struct{}

// CanaryStrategy defines parameters for a Replica Based Canary
type CanaryStrategy struct {
	// CanaryService holds the name of a service which selects pods with canary version and don't select any pods with stable version.
	// +optional
	CanaryService string `json:"canaryService,omitempty"`
	// StableService holds the name of a service which selects pods with stable version and don't select any pods with canary version.
	// +optional
	StableService string `json:"stableService,omitempty"`
	// Steps define the order of phases to execute the canary deployment
	// +optional
	Steps []CanaryStep `json:"steps,omitempty"`
	// TrafficRouting hosts all the supported service meshes supported to enable more fine-grained traffic routing
	TrafficRouting *RolloutTrafficRouting `json:"trafficRouting,omitempty"`

	// MaxUnavailable The maximum number of pods that can be unavailable during the update.
	// Value can be an absolute number (ex: 5) or a percentage of total pods at the start of update (ex: 10%).
	// Absolute number is calculated from percentage by rounding down.
	// This can not be 0 if MaxSurge is 0.
	// By default, a fixed value of 1 is used.
	// Example: when this is set to 30%, the old RC can be scaled down by 30%
	// immediately when the rolling update starts. Once new pods are ready, old RC
	// can be scaled down further, followed by scaling up the new RC, ensuring
	// that at least 70% of original number of pods are available at all times
	// during the update.
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`

	// MaxSurge The maximum number of pods that can be scheduled above the original number of
	// pods.
	// Value can be an absolute number (ex: 5) or a percentage of total pods at
	// the start of the update (ex: 10%). This can not be 0 if MaxUnavailable is 0.
	// Absolute number is calculated from percentage by rounding up.
	// By default, a value of 1 is used.
	// Example: when this is set to 30%, the new RC can be scaled up by 30%
	// immediately when the rolling update starts. Once old pods have been killed,
	// new RC can be scaled up further, ensuring that total number of pods running
	// at any time during the update is at most 130% of original pods.
	// +optional
	MaxSurge *intstr.IntOrString `json:"maxSurge,omitempty"`
	// Analysis runs a separate analysisRun while all the steps execute. This is intended to be a continuous validation of the new ReplicaSet
	Analysis *RolloutAnalysisBackground `json:"analysis,omitempty"`
	// AntiAffinity enables anti-affinity rules for Canary deployment
	// +optional
	AntiAffinity *AntiAffinity `json:"antiAffinity,omitempty"`
}

// ALBTrafficRouting configuration for ALB ingress controller to control traffic routing
type ALBTrafficRouting struct {
	// Ingress refers to the name of an `Ingress` resource in the same namepace as the `Rollout`
	Ingress string `json:"ingress"`
	// ServicePort refers to the port that the Ingress action should route traffic to
	ServicePort int32 `json:"servicePort"`
	// RootService references the service in the ingress to the controller should add the action to
	RootService string `json:"rootService,omitempty"`
	// AnnotationPrefix has to match the configured annotation prefix on the alb ingress controller
	// +optional
	AnnotationPrefix string `json:"annotationPrefix,omitempty"`
}

// RolloutTrafficRouting hosts all the different configuration for supported service meshes to enable more fine-grained traffic routing
type RolloutTrafficRouting struct {
	// Istio holds Istio specific configuration to route traffic
	Istio *IstioTrafficRouting `json:"istio,omitempty"`
	// Nginx holds Nginx Ingress specific configuration to route traffic
	Nginx *NginxTrafficRouting `json:"nginx,omitempty"`
	// Nginx holds ALB Ingress specific configuration to route traffic
	ALB *ALBTrafficRouting `json:"alb,omitempty"`
	// SMI holds TrafficSplit specific configuration to route traffic
	SMI *SMITrafficRouting `json:"smi,omitempty"`
}

// SMITrafficRouting configuration for TrafficSplit Custom Resource to control traffic routing
type SMITrafficRouting struct {
	// RootService holds the name of that clients use to communicate.
	// +optional
	RootService string `json:"rootService,omitempty"`
	// TrafficSplitName holds the name of the TrafficSplit.
	// +optional
	TrafficSplitName string `json:"trafficSplitName,omitempty"`
}

// NginxTrafficRouting configuration for Nginx ingress controller to control traffic routing
type NginxTrafficRouting struct {
	// AnnotationPrefix has to match the configured annotation prefix on the nginx ingress controller
	// +optional
	AnnotationPrefix string `json:"annotationPrefix,omitempty"`
	// StableIngress refers to the name of an `Ingress` resource in the same namespace as the `Rollout`
	StableIngress string `json:"stableIngress"`
	// +optional
	AdditionalIngressAnnotations map[string]string `json:"additionalIngressAnnotations,omitempty"`
}

// IstioTrafficRouting configuration for Istio service mesh to enable fine grain configuration
type IstioTrafficRouting struct {
	// VirtualService reference to a Virtual Service that modified to shape traffic
	VirtualService IstioVirtualService `json:"virtualService"`
}

// IstioVirtualService holds information on the virtual service the rollout needs to modify
type IstioVirtualService struct {
	// Name holds the name of the VirtualService
	Name string `json:"name"`
	// Routes list of routes within VirtualService to edit
	Routes []string `json:"routes"`
}

// RolloutExperimentStep defines a template that is used to create a experiment for a step
type RolloutExperimentStep struct {
	// Templates what templates that should be added to the experiment. Should be non-nil
	// +patchMergeKey=name
	// +patchStrategy=merge
	Templates []RolloutExperimentTemplate `json:"templates" patchStrategy:"merge" patchMergeKey:"name"`
	// Duration is a duration string (e.g. 30s, 5m, 1h) that the experiment should run for
	// +optional
	Duration DurationString `json:"duration,omitempty"`
	// Analyses reference which analysis templates to run with the experiment
	// +patchMergeKey=name
	// +patchStrategy=merge
	Analyses []RolloutExperimentStepAnalysisTemplateRef `json:"analyses,omitempty" patchStrategy:"merge" patchMergeKey:"name"`
}

type RolloutExperimentStepAnalysisTemplateRef struct {
	// Name is a name for this analysis template invocation
	Name string `json:"name"`
	// TemplateName reference of the AnalysisTemplate name used by the Experiment to create the run
	TemplateName string `json:"templateName"`
	// Whether to look for the templateName at cluster scope or namespace scope
	// +optional
	ClusterScope bool `json:"clusterScope,omitempty"`
	// Args the arguments that will be added to the AnalysisRuns
	// +patchMergeKey=name
	// +patchStrategy=merge
	Args []AnalysisRunArgument `json:"args,omitempty" patchStrategy:"merge" patchMergeKey:"name"`
}

// RolloutExperimentTemplate defines the template used to create experiments for the Rollout's experiment canary step
type RolloutExperimentTemplate struct {
	// Name description of template that passed to the template
	Name string `json:"name"`
	// SpecRef indicates where the rollout should get the RS template from
	SpecRef ReplicaSetSpecRef `json:"specRef"`
	// Replicas replica count for the template
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// Metadata sets labels and annotations to use for the RS created from the template
	// +optional
	Metadata PodTemplateMetadata `json:"metadata,omitempty"`
	// Selector overrides the selector to be used for the template's ReplicaSet. If omitted, will
	// use the same selector as the Rollout
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
}

// PodTemplateMetadata extra labels to add to the template
type PodTemplateMetadata struct {
	// Labels Additional labels to add to the experiment
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations additional annotations to add to the experiment
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ReplicaSetSpecRef defines which RS that the experiment's template will use.
type ReplicaSetSpecRef string

const (
	// CanarySpecRef indicates the RS template should be pulled from the newRS's template
	CanarySpecRef ReplicaSetSpecRef = "canary"
	// StableSpecRef indicates the RS template should be pulled from the stableRS's template
	StableSpecRef ReplicaSetSpecRef = "stable"
)

// CanaryStep defines a step of a canary deployment.
type CanaryStep struct {
	// SetWeight sets what percentage of the newRS should receive
	SetWeight *int32 `json:"setWeight,omitempty"`
	// Pause freezes the rollout by setting spec.Paused to true.
	// A Rollout will resume when spec.Paused is reset to false.
	// +optional
	Pause *RolloutPause `json:"pause,omitempty"`
	// Experiment defines the experiment object that should be created
	Experiment *RolloutExperimentStep `json:"experiment,omitempty"`
	// Analysis defines the AnalysisRun that will run for a step
	Analysis *RolloutAnalysis `json:"analysis,omitempty"`
	// SetCanaryScale defines how to scale the newRS without changing traffic weight
	// +optional
	SetCanaryScale *SetCanaryScale `json:"setCanaryScale,omitempty"`
}

// SetCanaryScale defines how to scale the newRS without changing traffic weight
type SetCanaryScale struct {
	// Weight sets the percentage of replicas the newRS should have
	// +optional
	Weight *int32 `json:"weight,omitempty"`
	// Replicas sets the number of replicas the newRS should have
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// MatchTrafficWeight cancels out previously set Replicas or Weight, effectively activating SetWeight
	// +optional
	MatchTrafficWeight bool `json:"matchTrafficWeight,omitempty"`
}

// RolloutAnalysisBackground defines a template that is used to create a background analysisRun
type RolloutAnalysisBackground struct {
	RolloutAnalysis `json:",inline"`
	// StartingStep indicates which step the background analysis should start on
	// If not listed, controller defaults to 0
	StartingStep *int32 `json:"startingStep,omitempty"`
}

// RolloutAnalysis defines a template that is used to create a analysisRun
type RolloutAnalysis struct {
	// Whether to look for the templateName at cluster scope or namespace scope
	// Deprecated and will be removed in v0.9
	// +optional
	ClusterScope bool `json:"clusterScope,omitempty"`
	// TemplateName reference of the AnalysisTemplate name used by the Rollout to create the run
	// Deprecated and will be removed in v0.9
	TemplateName string `json:"templateName,omitempty"`
	//Templates reference to a list of analysis templates to combine for an AnalysisRun
	Templates []RolloutAnalysisTemplate `json:"templates,omitempty"`
	// Args the arguments that will be added to the AnalysisRuns
	// +patchMergeKey=name
	// +patchStrategy=merge
	Args []AnalysisRunArgument `json:"args,omitempty" patchStrategy:"merge" patchMergeKey:"name"`
}

type RolloutAnalysisTemplate struct {
	//TemplateName name of template to use in AnalysisRun
	// +optional
	TemplateName string `json:"templateName"`
	// Whether to look for the templateName at cluster scope or namespace scope
	// +optional
	ClusterScope bool `json:"clusterScope,omitempty"`
}

// AnalysisRunArgument argument to add to analysisRun
type AnalysisRunArgument struct {
	// Name argument name
	Name string `json:"name"`
	// Value a hardcoded value for the argument. This field is a one of field with valueFrom
	Value string `json:"value,omitempty"`
	// ValueFrom A reference to where the value is stored. This field is a one of field with valueFrom
	ValueFrom *ArgumentValueFrom `json:"valueFrom,omitempty"`
}

// ArgumentValueFrom defines references to fields within resources to grab for the value (i.e. Pod Template Hash)
type ArgumentValueFrom struct {
	// PodTemplateHashValue gets the value from one of the children ReplicaSet's Pod Template Hash
	PodTemplateHashValue *ValueFromPodTemplateHash `json:"podTemplateHashValue,omitempty"`
}

// ValueFromPodTemplateHash indicates which ReplicaSet pod template pod hash to use
type ValueFromPodTemplateHash string

const (
	// Stable tells the Rollout to get the pod template hash from the stable ReplicaSet
	Stable ValueFromPodTemplateHash = "Stable"
	// Latest tells the Rollout to get the pod template hash from the latest ReplicaSet
	Latest ValueFromPodTemplateHash = "Latest"
)

const (
	// RolloutTypeLabel indicates how the rollout created the analysisRun
	RolloutTypeLabel = "rollout-type"
	// RolloutTypeStepLabel indicates that the analysisRun was created as a canary step
	RolloutTypeStepLabel = "Step"
	// RolloutTypeBackgroundRunLabel indicates that the analysisRun was created in Background to an execution
	RolloutTypeBackgroundRunLabel = "Background"
	// RolloutTypePrePromotionLabel indicates that the analysisRun was created before the active service promotion
	RolloutTypePrePromotionLabel = "PrePromotion"
	// RolloutTypePostPromotionLabel indicates that the analysisRun was created after the active service promotion
	RolloutTypePostPromotionLabel = "PostPromotion"
	// RolloutCanaryStepIndexLabel indicates which step created this analysisRun
	RolloutCanaryStepIndexLabel = "step-index"
)

// RolloutPause defines a pause stage for a rollout
type RolloutPause struct {
	// Duration the amount of time to wait before moving to the next step.
	// +optional
	Duration *intstr.IntOrString `json:"duration,omitempty"`
}

// DurationSeconds converts the pause duration to seconds
// If Duration is nil 0 is returned
// if Duration values is string and does not contain a valid unit -1 is returned
func (p RolloutPause) DurationSeconds() int32 {
	if p.Duration != nil {
		if p.Duration.Type == intstr.String {
			s, err := strconv.Atoi(p.Duration.StrVal)
			if err != nil {
				d, err := time.ParseDuration(p.Duration.StrVal)
				if err != nil {
					return -1
				}
				return int32(d.Seconds())
			}
			// special case where no unit was specified
			return int32(s)
		}
		return p.Duration.IntVal
	}
	return 0
}

// DurationFromInt creates duration in seconds from int value
func DurationFromInt(i int) *intstr.IntOrString {
	d := intstr.FromInt(i)
	return &d
}

// DurationFromString creates duration from string
// value must be a string representation of an int with optional time unit (see time.ParseDuration)
func DurationFromString(s string) *intstr.IntOrString {
	d := intstr.FromString(s)
	return &d
}

// PauseReason reasons that the rollout can pause
type PauseReason string

const (
	// PauseReasonInconclusiveAnalysis pauses rollout when rollout has an inconclusive analysis run
	PauseReasonInconclusiveAnalysis PauseReason = "InconclusiveAnalysisRun"
	// PauseReasonInconclusiveExperiment pauses rollout when rollout has an inconclusive experiment
	PauseReasonInconclusiveExperiment PauseReason = "InconclusiveExperiment"
	// PauseReasonCanaryPauseStep pause rollout for canary pause step
	PauseReasonCanaryPauseStep PauseReason = "CanaryPauseStep"
	// PauseReasonBlueGreenPause pause rollout before promoting rollout
	PauseReasonBlueGreenPause PauseReason = "BlueGreenPause"
)

// PauseCondition the reason for a pause and when it started
type PauseCondition struct {
	Reason    PauseReason `json:"reason"`
	StartTime metav1.Time `json:"startTime"`
}

// RolloutStatus is the status for a Rollout resource
type RolloutStatus struct {
	// Abort cancel the current rollout progression
	Abort bool `json:"abort,omitempty"`
	// PauseConditions indicates why the rollout is currently paused
	PauseConditions []PauseCondition `json:"pauseConditions,omitempty"`
	//ControllerPause indicates the controller has paused the rollout
	ControllerPause bool `json:"controllerPause,omitempty"`
	// AbortedAt indicates the controller reconciled an aborted rollout. The controller uses this to understand if
	// the controller needs to do some specific work when a Rollout is aborted. For example, the reconcileAbort is used
	// to indicate if the Rollout should enter an aborted state when the latest AnalysisRun is a failure, or the controller
	// has already put the Rollout into an aborted and should create a new AnalysisRun.
	AbortedAt *metav1.Time `json:"abortedAt,omitempty"`
	// CurrentPodHash the hash of the current pod template
	// +optional
	CurrentPodHash string `json:"currentPodHash,omitempty"`
	// CurrentStepHash the hash of the current list of steps for the current strategy. This is used to detect when the
	// list of current steps change
	// +optional
	CurrentStepHash string `json:"currentStepHash,omitempty"`
	// Total number of non-terminated pods targeted by this rollout (their labels match the selector).
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
	// Total number of non-terminated pods targeted by this rollout that have the desired template spec.
	// +optional
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty"`
	// Total number of ready pods targeted by this rollout.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
	// Total number of available pods (ready for at least minReadySeconds) targeted by this rollout.
	// +optional
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`
	// CurrentStepIndex defines the current step of the rollout is on. If the current step index is null, the
	// controller will execute the rollout.
	// +optional
	CurrentStepIndex *int32 `json:"currentStepIndex,omitempty"`
	// Count of hash collisions for the Rollout. The Rollout controller uses this
	// field as a collision avoidance mechanism when it needs to create the name for the
	// newest ReplicaSet.
	// +optional
	CollisionCount *int32 `json:"collisionCount,omitempty"`
	// The generation observed by the rollout controller by taking a hash of the spec.
	// +optional
	ObservedGeneration string `json:"observedGeneration,omitempty"`
	// Conditions a list of conditions a rollout can have.
	// +optional
	Conditions []RolloutCondition `json:"conditions,omitempty"`
	// Canary describes the state of the canary rollout
	// +optional
	Canary CanaryStatus `json:"canary,omitempty"`
	// BlueGreen describes the state of the bluegreen rollout
	// +optional
	BlueGreen BlueGreenStatus `json:"blueGreen,omitempty"`
	// HPAReplicas the number of non-terminated replicas that are receiving active traffic
	// +optional
	HPAReplicas int32 `json:"HPAReplicas,omitempty"`
	// Selector that identifies the pods that are receiving active traffic
	// +optional
	Selector string `json:"selector,omitempty"`
	// StableRS indicates the replicaset that has successfully rolled out
	// +optional
	StableRS string `json:"stableRS,omitempty"`
	// RestartedAt indicates last time a Rollout was restarted
	RestartedAt *metav1.Time `json:"restartedAt,omitempty"`
}

// BlueGreenStatus status fields that only pertain to the blueGreen rollout
type BlueGreenStatus struct {
	// PreviewSelector indicates which replicas set the preview service is serving traffic to
	// +optional
	PreviewSelector string `json:"previewSelector,omitempty"`
	// ActiveSelector indicates which replicas set the active service is serving traffic to
	// +optional
	ActiveSelector string `json:"activeSelector,omitempty"`
	// PreviousActiveSelector indicates the last selector that the active service used. This is used to know which replicaset
	// to avoid scaling down for the scale down delay
	// Deprecated: PreviousActiveSelector is tracked with the replicaset now instead of the rollout. will remove in v0.6
	// +optional
	PreviousActiveSelector string `json:"previousActiveSelector,omitempty"`
	// ScaleDownDelayStartTime indicates the start of the scaleDownDelay
	// Deprecated: ScaleDownDelay is now tracked at the replicaset now instead of the rollout. will remove in v0.6
	// +optional
	ScaleDownDelayStartTime *metav1.Time `json:"scaleDownDelayStartTime,omitempty"`
	// ScaleUpPreviewCheckPoint indicates that the Replicaset receiving traffic from the preview service is ready to be scaled up after the rollout is unpaused
	// +optional
	ScaleUpPreviewCheckPoint bool `json:"scaleUpPreviewCheckPoint,omitempty"`
	// PrePromotionAnalysisRun is the current analysis run running before the active service promotion
	// TODO(Deprecated): Remove in v0.10
	PrePromotionAnalysisRun string `json:"prePromotionAnalysisRun,omitempty"`
	// PrePromotionAnalysisRunStatus indicates the status of the current prepromotion analysis run
	PrePromotionAnalysisRunStatus *RolloutAnalysisRunStatus `json:"prePromotionAnalysisRunStatus,omitempty"`
	// PostPromotionAnalysisRun is the current analysis run running after the active service promotion
	// TODO(Deprecated): Remove in v0.10
	PostPromotionAnalysisRun string `json:"postPromotionAnalysisRun,omitempty"`
	// PostPromotionAnalysisRunStatus indicates the status of the current post promotion analysis run
	PostPromotionAnalysisRunStatus *RolloutAnalysisRunStatus `json:"postPromotionAnalysisRunStatus,omitempty"`
}

// CanaryStatus status fields that only pertain to the canary rollout
type CanaryStatus struct {
	// StableRS indicates the last replicaset that walked through all the canary steps or was the only replicaset
	// +optional
	StableRS string `json:"stableRS,omitempty"`
	// CurrentStepAnalysisRun indicates the analysisRun for the current step index
	// TODO(Deprecated): Remove in v0.10
	CurrentStepAnalysisRun string `json:"currentStepAnalysisRun,omitempty"`
	// CurrentStepAnalysisRunStatus indicates the status of the current step analysis run
	CurrentStepAnalysisRunStatus *RolloutAnalysisRunStatus `json:"currentStepAnalysisRunStatus,omitempty"`
	// CurrentBackgroundAnalysisRun indicates the analysisRun for the Background step
	// TODO(Deprecated): Remove in v0.10
	CurrentBackgroundAnalysisRun string `json:"currentBackgroundAnalysisRun,omitempty"`
	// CurrentBackgroundAnalysisRunStatus indicates the status of the current background analysis run
	CurrentBackgroundAnalysisRunStatus *RolloutAnalysisRunStatus `json:"currentBackgroundAnalysisRunStatus,omitempty"`
	// CurrentExperiment indicates the running experiment
	CurrentExperiment string `json:"currentExperiment,omitempty"`
}

type RolloutAnalysisRunStatus struct {
	Name    string        `json:"name"`
	Status  AnalysisPhase `json:"status"`
	Message string        `json:"message,omitempty"`
}

// RolloutConditionType defines the conditions of Rollout
type RolloutConditionType string

// These are valid conditions of a rollout.
const (
	// InvalidSpec means the rollout has an invalid spec and will not progress until
	// the spec is fixed.
	InvalidSpec RolloutConditionType = "InvalidSpec"
	// RolloutAvailable means the rollout is available, ie. the active service is pointing at a
	// replicaset with the required replicas up and running for at least minReadySeconds.
	RolloutAvailable RolloutConditionType = "Available"
	// RolloutProgressing means the rollout is progressing. Progress for a rollout is
	// considered when a new replica set is created or adopted, when pods scale
	// up or old pods scale down, or when the services are updated. Progress is not estimated
	// for paused rollouts.
	RolloutProgressing RolloutConditionType = "Progressing"
	// RolloutReplicaFailure ReplicaFailure is added in a deployment when one of its pods
	// fails to be created or deleted.
	RolloutReplicaFailure RolloutConditionType = "ReplicaFailure"
)

// RolloutCondition describes the state of a rollout at a certain point.
type RolloutCondition struct {
	// Type of deployment condition.
	Type RolloutConditionType `json:"type"`
	// Phase of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus `json:"status"`
	// The last time this condition was updated.
	LastUpdateTime metav1.Time `json:"lastUpdateTime"`
	// Last time the condition transitioned from one status to another.
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`
	// The reason for the condition's last transition.
	Reason string `json:"reason"`
	// A human readable message indicating details about the transition.
	Message string `json:"message"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RolloutList is a list of Rollout resources
type RolloutList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Rollout `json:"items"`
}
