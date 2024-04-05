package types

import (
	"encoding/json"
	"time"
)

type Phase string

const (
	PhaseRunning    Phase = "Running"
	PhaseSuccessful Phase = "Successful"
	PhaseFailed     Phase = "Failed"
	PhaseError      Phase = "Error"
)

type RpcStepContext struct {
	PluginName string
	Config     json.RawMessage
	Status     json.RawMessage
}

type RpcStepResult struct {
	Phase        Phase
	Message      string
	RequeueAfter time.Duration
	Status       json.RawMessage
}
