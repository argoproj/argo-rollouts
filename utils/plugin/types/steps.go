package types

import (
	"encoding/json"
	"fmt"
	"time"
)

// StepPhase is the type of phase of step plugin result
type StepPhase string

const (
	// PhaseRunning is the Running phase of a step plugin
	PhaseRunning StepPhase = "Running"
	// PhaseRunning is the Successful phase of a step plugin
	PhaseSuccessful StepPhase = "Successful"
	// PhaseRunning is the Failed phase of a step plugin
	PhaseFailed StepPhase = "Failed"
	// PhaseRunning is the Error phase of a step plugin
	PhaseError StepPhase = "Error"
)

// RpcStepContext is the context of the step plugin operation
type RpcStepContext struct {
	// PluginName is the name of the plugin as defined by the user
	PluginName string
	// Config holds the user specified configuration in the Rollout object for this plugin step
	Config json.RawMessage
	// Status holds a previous execution status related to the operation
	Status json.RawMessage
}

type RpcStepResult struct {
	// Phase of the operation to idicate if it has completed or not
	Phase StepPhase
	// Message contains information about the execution
	Message string
	// RequeueAfter is the duration to wait before executing the operation again when it does not return a completed phase
	RequeueAfter time.Duration
	// Status hold the execution status of this plugin step. It can be used to persist a state between executions
	Status json.RawMessage
}

// Validate the phase of a step plugin
func (p StepPhase) Validate() error {
	switch p {
	case PhaseRunning:
	case PhaseSuccessful:
	case PhaseFailed:
	case PhaseError:
	default:
		return fmt.Errorf("phase '%s' is not valid", p)
	}
	return nil
}
