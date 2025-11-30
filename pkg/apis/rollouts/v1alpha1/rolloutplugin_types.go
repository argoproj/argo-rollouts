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
// +kubebuilder:printcolumn:name="Strategy",type="string",JSONPath=".spec.strategy.type",description="Rollout strategy type"
// +kubebuilder:printcolumn:name="Step",type="integer",JSONPath=".status.currentStepIndex",description="Current step index"
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

	// Strategy defines the deployment strategy (canary or blueGreen)
	Strategy RolloutPluginStrategy `json:"strategy" protobuf:"bytes,3,opt,name=strategy"`
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
// This is similar to RolloutStrategy but specific to RolloutPlugin
type RolloutPluginStrategy struct {
	// Type of strategy (Canary or BlueGreen)
	// +optional
	Type string `json:"type,omitempty" protobuf:"bytes,1,opt,name=type"`

	// Canary strategy configuration (reuses existing CanaryStrategy type)
	// +optional
	Canary *CanaryStrategy `json:"canary,omitempty" protobuf:"bytes,2,opt,name=canary"`

	// BlueGreen strategy configuration (reuses existing BlueGreenStrategy type)
	// +optional
	BlueGreen *BlueGreenStrategy `json:"blueGreen,omitempty" protobuf:"bytes,3,opt,name=blueGreen"`
}

// RolloutPluginStatus is the status for a RolloutPlugin resource
type RolloutPluginStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,1,opt,name=observedGeneration"`

	// Initialized indicates whether the plugin has been initialized
	Initialized bool `json:"initialized,omitempty" protobuf:"varint,2,opt,name=initialized"`

	// Phase is the current phase of the rollout (Progressing, Paused, Successful, Failed, etc.)
	Phase string `json:"phase,omitempty" protobuf:"bytes,3,opt,name=phase"`

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

	// CurrentStepComplete indicates whether the current step has completed
	CurrentStepComplete bool `json:"currentStepComplete,omitempty" protobuf:"varint,8,opt,name=currentStepComplete"`

	// RolloutInProgress indicates whether a rollout is currently in progress
	RolloutInProgress bool `json:"rolloutInProgress,omitempty" protobuf:"varint,9,opt,name=rolloutInProgress"`

	// Paused indicates whether the rollout is currently paused
	Paused bool `json:"paused,omitempty" protobuf:"varint,10,opt,name=paused"`

	// PauseStartTime is the time when the rollout was paused
	// +optional
	PauseStartTime *metav1.Time `json:"pauseStartTime,omitempty" protobuf:"bytes,11,opt,name=pauseStartTime"`

	// Aborted indicates whether the rollout has been aborted
	Aborted bool `json:"aborted,omitempty" protobuf:"varint,12,opt,name=aborted"`

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
}

// RolloutPluginCondition describes a condition of the RolloutPlugin
type RolloutPluginCondition struct {
	// Type of the condition (e.g., Progressing, Available, ReplicaFailure)
	Type string `json:"type" protobuf:"bytes,1,opt,name=type"`

	// Status of the condition (True, False, Unknown)
	Status corev1.ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status,casttype=k8s.io/api/core/v1.ConditionStatus"`

	// LastUpdateTime is the last time the condition was updated
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty" protobuf:"bytes,3,opt,name=lastUpdateTime"`

	// LastTransitionTime is the last time the condition transitioned from one status to another
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,4,opt,name=lastTransitionTime"`

	// Reason is a brief machine-readable explanation for the condition's last transition
	Reason string `json:"reason,omitempty" protobuf:"bytes,5,opt,name=reason"`

	// Message is a human-readable explanation for the condition's last transition
	Message string `json:"message,omitempty" protobuf:"bytes,6,opt,name=message"`
}
