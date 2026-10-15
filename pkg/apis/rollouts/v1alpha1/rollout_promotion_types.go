package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	RolloutPromotionActionPromote     = "promote"
	RolloutPromotionActionPromoteFull = "promoteFull"
	RolloutPromotionActionAbort       = "abort"
	RolloutPromotionActionRetry       = "retry"
	RolloutPromotionActionRollback    = "rollback"

	RolloutPromotionModeTogether   = "together"
	RolloutPromotionModeSequential = "sequential"

	RolloutPromotionStrategyAuto      = "auto"
	RolloutPromotionStrategyBlueGreen = "blueGreen"
	RolloutPromotionStrategyCanary    = "canary"

	RolloutPromotionRollbackTargetStable       = "stable"
	RolloutPromotionTrafficRollbackPrevious    = "previousSnapshot"
	RolloutPromotionDefaultTrafficHistoryLimit = 10
	RolloutPromotionCanaryCompletionNextPause  = "nextPause"
	RolloutPromotionCanaryCompletionFullWeight = "completeAfterFullWeight"

	RolloutPromotionPhasePending   = "Pending"
	RolloutPromotionPhaseRunning   = "Running"
	RolloutPromotionPhaseSucceeded = "Succeeded"
	RolloutPromotionPhaseFailed    = "Failed"

	RolloutPromotionOperationForward         = "Forward"
	RolloutPromotionOperationTrafficRollback = "TrafficRollback"

	RolloutPromotionOperationPrepared  = "Prepared"
	RolloutPromotionOperationCommitted = "Committed"
	RolloutPromotionOperationApplying  = "Applying"
	RolloutPromotionOperationVerifying = "Verifying"
	RolloutPromotionOperationStalled   = "Stalled"
	RolloutPromotionOperationSucceeded = "Succeeded"
	RolloutPromotionOperationFailed    = "Failed"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:path=rolloutpromotions,shortName=rp
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase",description="Current execution phase"
// +kubebuilder:printcolumn:name="Action",type="string",JSONPath=".spec.action",description="Requested rollout action"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time since resource was created"

// RolloutPromotion requests idempotent lifecycle actions for one or more Rollouts.
// +protobuf=false
type RolloutPromotion struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RolloutPromotionSpec   `json:"spec"`
	Status RolloutPromotionStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RolloutPromotionList contains RolloutPromotion resources.
// +protobuf=false
type RolloutPromotionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RolloutPromotion `json:"items"`
}

// RolloutPromotionSpec describes a single, replay-safe promotion request.
// +protobuf=false
type RolloutPromotionSpec struct {
	// RequestID identifies one forward lifecycle action. Reapplying a manifest
	// with an already processed ID is a no-op while that ID remains in this CR's history.
	RequestID string `json:"requestID,omitempty"`
	// +kubebuilder:default=promote
	// +kubebuilder:validation:Enum=promote;promoteFull;abort;retry;rollback
	Action string `json:"action,omitempty"`
	// +kubebuilder:default=together
	// +kubebuilder:validation:Enum=together;sequential
	Mode string `json:"mode,omitempty"`
	// +kubebuilder:default=auto
	// +kubebuilder:validation:Enum=auto;blueGreen;canary
	Strategy string `json:"strategy,omitempty"`
	// +kubebuilder:validation:MinItems=1
	// +listType=atomic
	Rollouts []RolloutPromotionTarget `json:"rollouts"`
	Wait     RolloutPromotionWait     `json:"wait,omitempty"`
	// Canary configures how a promote request finishes a canary rollout after
	// the requested manual gate has been opened.
	Canary *RolloutPromotionCanary `json:"canary,omitempty"`
	// TrafficRollback stores pre-promotion traffic snapshots in status. A
	// rollback request restores the latest snapshot without changing PodTemplate.
	TrafficRollback *RolloutPromotionTrafficRollback `json:"trafficRollback,omitempty"`
}

// RolloutPromotionCanary configures canary-specific promotion behavior.
// +protobuf=false
type RolloutPromotionCanary struct {
	// CompletionPolicy controls whether promote stops at the next pause or
	// completes a rollout after it reaches 100% traffic at its final pause.
	// Completing the rollout advances stableRS; old ReplicaSet retention remains
	// controlled by the Rollout's scaleDownDelaySeconds.
	// +kubebuilder:default=nextPause
	// +kubebuilder:validation:Enum=nextPause;completeAfterFullWeight
	CompletionPolicy string `json:"completionPolicy,omitempty"`
}

// RolloutPromotionTrafficRollback configures controller-owned traffic history.
// +protobuf=false
type RolloutPromotionTrafficRollback struct {
	// +kubebuilder:default=false
	Enabled *bool `json:"enabled,omitempty"`
	// +kubebuilder:default=previousSnapshot
	// +kubebuilder:validation:Enum=previousSnapshot
	Mode string `json:"mode,omitempty"`
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=50
	HistoryLimit int32 `json:"historyLimit,omitempty"`
}

// RolloutPromotionTarget identifies a Rollout and optional safety expectations.
// +protobuf=false
type RolloutPromotionTarget struct {
	// +kubebuilder:validation:MinLength=1
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	// +kubebuilder:validation:Enum=auto;blueGreen;canary
	Strategy       string `json:"strategy,omitempty"`
	ActiveService  string `json:"activeService,omitempty"`
	PreviewService string `json:"previewService,omitempty"`
	// ExpectedRolloutUID prevents a completed request for an earlier Rollout
	// object from being replayed against a newly created object with the same
	// name and ReplicaSet hash.
	ExpectedRolloutUID string `json:"expectedRolloutUID,omitempty"`
	// ExpectedPodTemplateHash is the SHA256 of the JSON encoded spec.template.
	// It binds a request even before status.currentPodHash has been reconciled.
	ExpectedPodTemplateHash string `json:"expectedPodTemplateHash,omitempty"`
	// Readiness defers transient preconditions to the controller without relaxing
	// identity or step guards. Omission preserves the legacy request contract.
	Readiness                 *RolloutPromotionReadiness `json:"readiness,omitempty"`
	ExpectedCurrentPodHash    string                     `json:"expectedCurrentPodHash,omitempty"`
	ExpectedStableRS          string                     `json:"expectedStableRS,omitempty"`
	ExpectedActiveServiceHash string                     `json:"expectedActiveServiceHash,omitempty"`
	// ExpectedCurrentStepIndex prevents a stale canary request from advancing a later pause.
	// The pointer preserves step zero as an explicit safety expectation.
	// +kubebuilder:validation:Minimum=0
	ExpectedCurrentStepIndex *int32 `json:"expectedCurrentStepIndex,omitempty"`
	// +kubebuilder:validation:Minimum=0
	ToRevision int64 `json:"toRevision,omitempty"`
	// RollbackTarget resolves a dynamic rollback destination. "stable" restores
	// the ReplicaSet currently recorded in Rollout status.stableRS. It is
	// mutually exclusive with ToRevision.
	// +kubebuilder:validation:Enum=stable
	RollbackTarget string                        `json:"rollbackTarget,omitempty"`
	WeightSwitch   *RolloutPromotionWeightSwitch `json:"weightSwitch,omitempty"`
	// Traffic selects an optional traffic adapter. Omission uses native Rollout
	// actions only. WeightSwitch remains a compatible alias for podAnnotation;
	// setting both is rejected.
	Traffic *RolloutPromotionTraffic `json:"traffic,omitempty"`
}

// RolloutPromotionTraffic selects a compiled-in traffic adapter. Config is
// validated by that adapter; unknown providers and unsupported capabilities fail closed.
// +protobuf=false
type RolloutPromotionTraffic struct {
	Provider string `json:"provider"`
	// ApplyOnPromotion defaults to true. Canary TrafficRouters normally own the
	// forward weight steps, so use false when only snapshot rollback is needed.
	ApplyOnPromotion *bool             `json:"applyOnPromotion,omitempty"`
	Config           map[string]string `json:"config,omitempty"`
}

// RolloutPromotionTrafficState is a versioned, provider-owned rollback payload.
// It stores no credentials. RolloutUID binds new snapshots to an object identity.
// +protobuf=false
type RolloutPromotionTrafficState struct {
	Provider   string            `json:"provider"`
	Version    string            `json:"version"`
	RolloutUID string            `json:"rolloutUID"`
	Config     map[string]string `json:"config,omitempty"`
	Data       map[string]string `json:"data,omitempty"`
}

// RolloutPromotionReadiness guards a forward promotion before its durable commit.
// Polling uses spec.wait.attempts and seconds, independently of wait.enabled.
// +protobuf=false
type RolloutPromotionReadiness struct {
	RequireCurrentPodHash bool `json:"requireCurrentPodHash,omitempty"`
	RequirePaused         bool `json:"requirePaused,omitempty"`
	// +listType=atomic
	AllowedCurrentStepIndexes []int32 `json:"allowedCurrentStepIndexes,omitempty"`
}

// RolloutPromotionWeightSwitch defines the legacy post-promotion annotation patch.
// Canary traffic weights are reconciled by the Rollout TrafficRouter instead.
// +protobuf=false
type RolloutPromotionWeightSwitch struct {
	// +kubebuilder:default=false
	Enabled *bool `json:"enabled,omitempty"`
	// ApplyOnPromotion controls the legacy post-promotion patch. Snapshot traffic
	// rollback still uses this configuration when the value is false.
	// +kubebuilder:default=true
	ApplyOnPromotion *bool  `json:"applyOnPromotion,omitempty"`
	AnnotationKey    string `json:"annotationKey,omitempty"`
	BaseSelector     string `json:"baseSelector,omitempty"`
	ActiveWeight     string `json:"activeWeight,omitempty"`
	InactiveWeight   string `json:"inactiveWeight,omitempty"`
	// +kubebuilder:default=0
	// +kubebuilder:validation:Minimum=0
	ReapplyDelaySeconds int32 `json:"reapplyDelaySeconds,omitempty"`
}

// RolloutPromotionWait configures completion polling.
// +protobuf=false
type RolloutPromotionWait struct {
	// +kubebuilder:default=true
	Enabled *bool `json:"enabled,omitempty"`
	// +kubebuilder:default=180
	// +kubebuilder:validation:Minimum=1
	Attempts int32 `json:"attempts,omitempty"`
	// +kubebuilder:default=5
	// +kubebuilder:validation:Minimum=1
	Seconds int32 `json:"seconds,omitempty"`
}

// RolloutPromotionStatus records the observed result of a promotion request.
// +protobuf=false
type RolloutPromotionStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +listType=atomic
	ProcessedRequestIDs []string `json:"processedRequestIDs,omitempty"`
	// +listType=atomic
	TrafficSnapshots           []RolloutPromotionTrafficSnapshot `json:"trafficSnapshots,omitempty"`
	LastTrafficRollbackRequest string                            `json:"lastTrafficRollbackRequest,omitempty"`
	ActiveTrafficSnapshotID    string                            `json:"activeTrafficSnapshotID,omitempty"`
	// Operation is the durable execution record for the currently active request.
	// A controller must persist Committed before changing any Rollout or traffic
	// resource, then resume any non-terminal phase after a restart.
	Operation *RolloutPromotionOperationStatus `json:"operation,omitempty"`
	// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed
	Phase          string       `json:"phase,omitempty"`
	StartTime      *metav1.Time `json:"startTime,omitempty"`
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// +listType=atomic
	Targets []RolloutPromotionTargetStatus `json:"targets,omitempty"`
}

// RolloutPromotionOperationStatus records the durable commit point of one
// forward promotion or traffic rollback transaction.
// +protobuf=false
type RolloutPromotionOperationStatus struct {
	ID                 string `json:"id"`
	RequestID          string `json:"requestID"`
	Kind               string `json:"kind"`
	Action             string `json:"action"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`
	// +kubebuilder:validation:Enum=Prepared;Committed;Applying;Verifying;Stalled;Succeeded;Failed
	Phase              string       `json:"phase"`
	Attempt            int32        `json:"attempt,omitempty"`
	CommitTime         *metav1.Time `json:"commitTime,omitempty"`
	LastTransitionTime *metav1.Time `json:"lastTransitionTime,omitempty"`
	CompletionTime     *metav1.Time `json:"completionTime,omitempty"`
	Message            string       `json:"message,omitempty"`
}

// RolloutPromotionTrafficSnapshot is the serving baseline and restoration state recorded
// immediately before a forward promotion action.
// +protobuf=false
type RolloutPromotionTrafficSnapshot struct {
	ID        string      `json:"id"`
	RequestID string      `json:"requestID,omitempty"`
	CreatedAt metav1.Time `json:"createdAt"`
	// +listType=atomic
	Targets []RolloutPromotionTrafficSnapshotTarget `json:"targets"`
}

// RolloutPromotionTrafficSnapshotTarget contains enough information to restore
// traffic to an existing ReplicaSet without changing workload desired state.
// +protobuf=false
type RolloutPromotionTrafficSnapshotTarget struct {
	Name       string `json:"name"`
	Namespace  string `json:"namespace,omitempty"`
	Strategy   string `json:"strategy,omitempty"`
	ActiveHash string `json:"activeHash"`
	// Traffic is used by new snapshots. The annotation fields below are retained
	// only to read snapshots written by older promotion controllers.
	Traffic             *RolloutPromotionTrafficState `json:"traffic,omitempty"`
	AnnotationKey       string                        `json:"annotationKey,omitempty"`
	BaseSelector        string                        `json:"baseSelector,omitempty"`
	ActiveWeight        string                        `json:"activeWeight,omitempty"`
	InactiveWeight      string                        `json:"inactiveWeight,omitempty"`
	ReapplyDelaySeconds int32                         `json:"reapplyDelaySeconds,omitempty"`
}

// RolloutPromotionTargetStatus records the controller-observed state of one target.
// +protobuf=false
type RolloutPromotionTargetStatus struct {
	Name                            string `json:"name"`
	Namespace                       string `json:"namespace,omitempty"`
	Strategy                        string `json:"strategy,omitempty"`
	Phase                           string `json:"phase,omitempty"`
	Message                         string `json:"message,omitempty"`
	ActiveService                   string `json:"activeService,omitempty"`
	PreviewService                  string `json:"previewService,omitempty"`
	ExpectedRolloutUID              string `json:"expectedRolloutUID,omitempty"`
	ExpectedCurrentPodHash          string `json:"expectedCurrentPodHash,omitempty"`
	ExpectedStableRS                string `json:"expectedStableRS,omitempty"`
	ExpectedActiveServiceHash       string `json:"expectedActiveServiceHash,omitempty"`
	ExpectedCurrentStepIndex        *int32 `json:"expectedCurrentStepIndex,omitempty"`
	RollbackTarget                  string `json:"rollbackTarget,omitempty"`
	ToRevision                      int64  `json:"toRevision,omitempty"`
	ResolvedToRevision              int64  `json:"resolvedToRevision,omitempty"`
	RolloutUID                      string `json:"rolloutUID,omitempty"`
	CurrentPodHash                  string `json:"currentPodHash,omitempty"`
	ActiveSelector                  string `json:"activeSelector,omitempty"`
	StableRS                        string `json:"stableRS,omitempty"`
	ActiveServiceHash               string `json:"activeServiceHash,omitempty"`
	RolloutPhase                    string `json:"rolloutPhase,omitempty"`
	PauseConditions                 int32  `json:"pauseConditions,omitempty"`
	CurrentStepIndex                int32  `json:"currentStepIndex,omitempty"`
	PreviousStepIndex               int32  `json:"previousStepIndex,omitempty"`
	StepCount                       int32  `json:"stepCount,omitempty"`
	Aborted                         bool   `json:"aborted,omitempty"`
	ControllerPause                 bool   `json:"controllerPause,omitempty"`
	SpecPaused                      bool   `json:"specPaused,omitempty"`
	CurrentStepAnalysisPhase        string `json:"currentStepAnalysisPhase,omitempty"`
	Promoted                        bool   `json:"promoted,omitempty"`
	CanaryWeight                    *int32 `json:"canaryWeight,omitempty"`
	CanaryWeightVerified            *bool  `json:"canaryWeightVerified,omitempty"`
	BaselineCurrentPodHash          string `json:"baselineCurrentPodHash,omitempty"`
	ActionApplied                   bool   `json:"actionApplied,omitempty"`
	Verified                        bool   `json:"verified,omitempty"`
	ActionAlreadyComplete           bool   `json:"actionAlreadyComplete,omitempty"`
	TrafficProvider                 string `json:"trafficProvider,omitempty"`
	TrafficApplied                  bool   `json:"trafficApplied,omitempty"`
	TrafficVerified                 bool   `json:"trafficVerified,omitempty"`
	WeightSwitchEnabled             bool   `json:"weightSwitchEnabled,omitempty"`
	WeightSwitchApplied             bool   `json:"weightSwitchApplied,omitempty"`
	WeightSwitchActiveHash          string `json:"weightSwitchActiveHash,omitempty"`
	WeightSwitchActivePods          int32  `json:"weightSwitchActivePods,omitempty"`
	WeightSwitchActiveReplicaSets   int32  `json:"weightSwitchActiveReplicaSets,omitempty"`
	WeightSwitchInactivePods        int32  `json:"weightSwitchInactivePods,omitempty"`
	WeightSwitchInactiveReplicaSets int32  `json:"weightSwitchInactiveReplicaSets,omitempty"`
	TrafficRollbackTargetHash       string `json:"trafficRollbackTargetHash,omitempty"`
	TrafficRollbackApplied          bool   `json:"trafficRollbackApplied,omitempty"`
}
