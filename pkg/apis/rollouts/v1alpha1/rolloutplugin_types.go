package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RolloutPlugin is a specification for managing rollouts of various Kubernetes resource types
// through a plugin architecture
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:path=rolloutplugins,shortName=rp
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase",description="RolloutPlugin status"
// +kubebuilder:printcolumn:name="Plugin",type="string",JSONPath=".spec.plugin.name",description="Plugin name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time since resource was created"
type RolloutPlugin struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   RolloutPluginSpec   `json:"spec" protobuf:"bytes,2,opt,name=spec"`
	Status RolloutPluginStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// RolloutPluginList is a list of RolloutPlugin resources
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type RolloutPluginList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata" protobuf:"bytes,1,opt,name=metadata"`
	Items           []RolloutPlugin `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// RolloutPluginSpec is the spec for a RolloutPlugin resource
type RolloutPluginSpec struct {
	// WorkloadRef is a reference to the workload (StatefulSet, DaemonSet, etc.) being managed
	WorkloadRef WorkloadRef `json:"workloadRef" protobuf:"bytes,1,opt,name=workloadRef"`

	// Plugin contains the configuration for the resource-specific plugin
	Plugin PluginConfig `json:"plugin" protobuf:"bytes,2,opt,name=plugin"`

	// Strategy defines the deployment strategy
	Strategy RolloutPluginStrategy `json:"strategy" protobuf:"bytes,3,opt,name=strategy"`

	// Analysis configuration for the analysis runs to retain
	// +optional
	Analysis *AnalysisRunStrategy `json:"analysis,omitempty" protobuf:"bytes,4,opt,name=analysis"`

	// Paused pauses the rollout at its current step.
	Paused bool `json:"paused,omitempty" protobuf:"varint,6,opt,name=paused"`

	// ProgressDeadlineSeconds The maximum time in seconds for a rollout to
	// make progress before it is considered to be failed. Argo Rollouts will
	// continue to process failed rollouts and a condition with a
	// ProgressDeadlineExceeded reason will be surfaced in the rollout status.
	// Note that progress will not be estimated during the time a rollout is paused.
	// Defaults to 600s.
	ProgressDeadlineSeconds *int32 `json:"progressDeadlineSeconds,omitempty" protobuf:"varint,7,opt,name=progressDeadlineSeconds"`

	// ProgressDeadlineAbort is whether to abort the update when ProgressDeadlineSeconds
	// is exceeded.
	// +optional
	ProgressDeadlineAbort bool `json:"progressDeadlineAbort,omitempty" protobuf:"varint,8,opt,name=progressDeadlineAbort"`
}

// WorkloadRef references a Kubernetes resource to be managed by the RolloutPlugin
type WorkloadRef struct {
	// APIVersion of the referenced resource
	APIVersion string `json:"apiVersion" protobuf:"bytes,1,opt,name=apiVersion"`

	// Kind of the referenced resource (e.g., StatefulSet, DaemonSet)
	Kind string `json:"kind" protobuf:"bytes,2,opt,name=kind"`

	// Name of the referenced resource
	Name string `json:"name" protobuf:"bytes,3,opt,name=name"`

	// Namespace of the referenced resource (optional, defaults to the RolloutPlugin's namespace)
	// +optional
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,4,opt,name=namespace"`
}

// PluginConfig contains configuration for the resource plugin
type PluginConfig struct {
	// Name of the plugin (e.g., "statefulset", "daemonset")
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`

	// Verify enables plugin binary verification
	// +optional
	Verify bool `json:"verify,omitempty" protobuf:"varint,2,opt,name=verify"`

	// SHA256 is the expected checksum of the plugin binary
	// +optional
	SHA256 string `json:"sha256,omitempty" protobuf:"bytes,3,opt,name=sha256"`

	// URL is the location to download the plugin binary
	// +optional
	URL string `json:"url,omitempty" protobuf:"bytes,4,opt,name=url"`

	// Config contains plugin-specific configuration as key-value pairs
	// +optional
	Config map[string]string `json:"config,omitempty" protobuf:"bytes,5,rep,name=config"`
}

// RolloutPluginStrategy defines the strategy for the rollout
// Only canary strategy is supported for RolloutPlugin
type RolloutPluginStrategy struct {
	// Canary strategy configuration (reuses existing CanaryStrategy type)
	// +optional
	Canary *CanaryStrategy `json:"canary,omitempty" protobuf:"bytes,2,opt,name=canary"`
}

// RolloutPluginPhase are a set of phases that a RolloutPlugin can be in
type RolloutPluginPhase string

const (
	// RolloutPluginPhaseHealthy indicates the RolloutPlugin is healthy
	RolloutPluginPhaseHealthy RolloutPluginPhase = "Healthy"
	// RolloutPluginPhaseDegraded indicates the RolloutPlugin is degraded
	RolloutPluginPhaseDegraded RolloutPluginPhase = "Degraded"
	// RolloutPluginPhaseProgressing indicates the RolloutPlugin is progressing
	RolloutPluginPhaseProgressing RolloutPluginPhase = "Progressing"
	// RolloutPluginPhasePaused indicates the RolloutPlugin is paused
	RolloutPluginPhasePaused RolloutPluginPhase = "Paused"
)

// RolloutPluginStatus is the status for a RolloutPlugin resource
type RolloutPluginStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,1,opt,name=observedGeneration"`

	// Phase is the current phase of the rollout
	Phase RolloutPluginPhase `json:"phase,omitempty" protobuf:"bytes,3,opt,name=phase,casttype=RolloutPluginPhase"`

	// Message provides additional details about the current phase
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,4,opt,name=message"`

	// CurrentRevision is the hash/version of the current stable workload
	// +optional
	CurrentRevision string `json:"currentRevision,omitempty" protobuf:"bytes,5,opt,name=currentRevision"`

	// UpdatedRevision is the hash/version of the updated workload being rolled out
	// +optional
	UpdatedRevision string `json:"updatedRevision,omitempty" protobuf:"bytes,6,opt,name=updatedRevision"`

	// CurrentStepIndex is the index of the current step in the rollout
	// +optional
	CurrentStepIndex *int32 `json:"currentStepIndex,omitempty" protobuf:"varint,7,opt,name=currentStepIndex"`

	// PauseConditions is a list of reasons why rolloutplugin became automatically paused (e.g.
	// CanaryPauseStep). The items in this list are populated by the controller but are cleared
	// by the user (e.g. ArgoCD resume action, kubectl patch) when they wish to unpause.
	// If pause conditions is empty, but controllerPause is true, it indicates
	// the user manually unpaused the RolloutPlugin
	// +optional
	PauseConditions []PauseCondition `json:"pauseConditions,omitempty" protobuf:"bytes,26,rep,name=pauseConditions"`

	// ControllerPause indicates the controller has paused the rolloutplugin. It is set to true when
	// the controller adds a pause condition. This field helps to discern the scenario where a
	// rolloutplugin was resumed after being paused by the controller (e.g. via ArgoCD resume action).
	// In that situation, the pauseConditions would have been cleared, but controllerPause would
	// still be set to true.
	ControllerPause bool `json:"controllerPause,omitempty" protobuf:"varint,27,opt,name=controllerPause"`

	// Aborted indicates whether the rolloutplugin has been aborted
	Aborted bool `json:"aborted,omitempty" protobuf:"varint,12,opt,name=aborted"`

	// AbortedRevision is the UpdatedRevision that was aborted
	// This is used to prevent retrying the same failed revision without explicit user action
	// +optional
	AbortedRevision string `json:"abortedRevision,omitempty" protobuf:"bytes,25,opt,name=abortedRevision"`

	// Abort will stop the rolloutplugin and revert to the previous version when set to true.
	// +optional
	Abort bool `json:"abort,omitempty" protobuf:"varint,21,opt,name=abort"`

	// PromoteFull when set to true will skip analysis, pause, and steps and promote the rollout immediately
	// +optional
	PromoteFull bool `json:"promoteFull,omitempty" protobuf:"varint,20,opt,name=promoteFull"`

	// Conditions is a list of conditions describing the current state
	// +optional
	Conditions []RolloutPluginCondition `json:"conditions,omitempty" protobuf:"bytes,13,rep,name=conditions"`

	// PluginStatus contains plugin-specific status information
	// +optional
	PluginStatus map[string]string `json:"pluginStatus,omitempty" protobuf:"bytes,14,rep,name=pluginStatus"`

	// Replicas is the total number of replicas targeted by the workload
	// +optional
	Replicas int32 `json:"replicas,omitempty" protobuf:"varint,15,opt,name=replicas"`

	// UpdatedReplicas is the number of replicas that have been updated
	// +optional
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty" protobuf:"varint,16,opt,name=updatedReplicas"`

	// ReadyReplicas is the number of replicas that are ready
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty" protobuf:"varint,17,opt,name=readyReplicas"`

	// AvailableReplicas is the number of replicas that are available
	// +optional
	AvailableReplicas int32 `json:"availableReplicas,omitempty" protobuf:"varint,18,opt,name=availableReplicas"`

	// Canary-specific status fields for canary strategy
	// +optional
	Canary CanaryStatus `json:"canary,omitempty" protobuf:"bytes,19,opt,name=canary"`

	// RestartCount tracks the number of restart attempts for the current rollout
	// Incremented each time Restart is processed
	// Reset to 0 when a new rollout starts
	// +optional
	RestartCount int32 `json:"restartCount,omitempty" protobuf:"varint,22,opt,name=restartCount"`

	// RestartedAt indicates when the last restart occurred
	// +optional
	RestartedAt *metav1.Time `json:"restartedAt,omitempty" protobuf:"bytes,23,opt,name=restartedAt"`

	// Restart when set to true will restart the rollout from step 0.
	// Similar to status.Abort and status.PromoteFull, this is a one-shot trigger field.
	// +optional
	Restart bool `json:"restart,omitempty" protobuf:"varint,24,opt,name=restart"`

	// AbortedAt indicates when the rollout was aborted
	// +optional
	AbortedAt *metav1.Time `json:"abortedAt,omitempty" protobuf:"bytes,28,opt,name=abortedAt"`
}

// RolloutPluginConditionType defines the conditions that a RolloutPlugin can have
type RolloutPluginConditionType string

const (
	// RolloutPluginConditionInvalidSpec means the spec is invalid
	RolloutPluginConditionInvalidSpec RolloutPluginConditionType = "InvalidSpec"
	// RolloutPluginConditionProgressing means the RolloutPlugin is progressing
	RolloutPluginConditionProgressing RolloutPluginConditionType = "Progressing"
	// RolloutPluginConditionHealthy means the RolloutPlugin is healthy
	RolloutPluginConditionHealthy RolloutPluginConditionType = "Healthy"
	// RolloutPluginConditionPaused means the RolloutPlugin is paused
	RolloutPluginConditionPaused RolloutPluginConditionType = "Paused"
	// RolloutPluginConditionCompleted means the RolloutPlugin completed its rollout
	RolloutPluginConditionCompleted RolloutPluginConditionType = "Completed"
)

// RolloutPluginCondition describes a condition of the RolloutPlugin
type RolloutPluginCondition struct {
	// Type of the condition
	Type RolloutPluginConditionType `json:"type" protobuf:"bytes,1,opt,name=type,casttype=RolloutPluginConditionType"`

	// Status of the condition
	Status corev1.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status,casttype=k8s.io/api/core/v1.ConditionStatus"`

	// LastUpdateTime is the last time the condition was updated
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty" protobuf:"bytes,3,opt,name=lastUpdateTime"`

	// LastTransitionTime is the last time the condition transitioned from one status to another
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,4,opt,name=lastTransitionTime"`

	// Reason is a brief explanation for the condition's last transition
	Reason string `json:"reason,omitempty" protobuf:"bytes,5,opt,name=reason"`

	// Message is a human-readable explanation for the condition's last transition
	Message string `json:"message,omitempty" protobuf:"bytes,6,opt,name=message"`
}
