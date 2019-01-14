package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Rollout is a specification for a Rollout resource
type Rollout struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RolloutSpec   `json:"spec"`
	Status RolloutStatus `json:"status"`
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
	MinReadySeconds int32 `json:"minReadySeconds"`
	// The deployment strategy to use to replace existing pods with new ones.
	// +optional
	Strategy RolloutStrategy `json:"strategy"`
	// The number of old ReplicaSets to retain.
	// This is a pointer to distinguish between explicit zero and not specified.
	// This is set to the max value of int32 (i.e. 2147483647) by default, which means
	// "retaining all old ReplicaSets".
	RevisionHistoryLimit *int32 `json:"revisionHistoryLimit,omitempty"`
}

const (
	// DefaultRolloutUniqueLabelKey is the default key of the selector that is added
	// to existing ReplicaSets (and label key that is added to its pods) to prevent the existing ReplicaSets
	// to select new pods (and old pods being select by new ReplicaSet).
	DefaultRolloutUniqueLabelKey string = "rollouts-pod-template-hash"
)

// RolloutStrategy defines stragtegy to apply during next rollout
type RolloutStrategy struct {
	// +optional
	BlueGreenStrategy *BlueGreenStrategy  `json:"blueGreen,omitempty"`
	Type              RolloutStrategyType `json:"type"`
}

//RolloutStrategyType defines a type that holds all the different rollout straegies
type RolloutStrategyType string

const (
	// BlueGreenRolloutStrategyType Replace the old ReplicaSets by using a blue green update
	// i.e Wait until a new stack is completely health before switching the service
	BlueGreenRolloutStrategyType RolloutStrategyType = "BlueGreenUpdate"
)

// BlueGreenStrategy defines parameters for Blue Green deployment
type BlueGreenStrategy struct {
	// Name of the service that the rollout modifies as the active service.
	ActiveService string `json:"activeService"`
	// Name of the service that the rollout modifies as the preview service.
	// +optional
	PreviewService string `json:"previewService"`
}

// RolloutStatus is the status for a Rollout resource
type RolloutStatus struct {
	// PreviewSelector indicates which replicas set the preview service is serving traffic to
	// +optional
	PreviewSelector string `json:"previewSelector"`
	// ActiveSelector indicates which replicas set the active service is serving traffic to
	// +optional
	ActiveSelector string `json:"activeSelector"`
	// VerifyingPreview indicates the rollout is verifying the replicas set being served
	// traffic from the preview service. User will need to edit this field to continue the rollout.
	// +optional
	VerifyingPreview *bool `json:"verifyingPreview,omitempty"`
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
}

// RolloutConditionType defines the conditions of Rolloutd
type RolloutConditionType string

// These are valid conditions of a rollout.
const (
	// InvalidSpec means the rollout has an invalid spec and will not progress until
	// the spec is fixed.
	InvalidSpec RolloutConditionType = "InvalidSpec"
	// Available means the rollout is available, ie. the active service is pointing at a
	// replicaset with the required replicas up and running for at least minReadySeconds.
	RolloutAvailable RolloutConditionType = "Available"
)

// RolloutCondition describes the state of a rollout at a certain point.
type RolloutCondition struct {
	// Type of deployment condition.
	Type RolloutConditionType
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus
	// The last time this condition was updated.
	LastUpdateTime metav1.Time
	// Last time the condition transitioned from one status to another.
	LastTransitionTime metav1.Time
	// The reason for the condition's last transition.
	Reason string
	// A human readable message indicating details about the transition.
	Message string
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RolloutList is a list of Rollout resources
type RolloutList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []Rollout `json:"items"`
}
