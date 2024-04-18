package types

import (
	"encoding/json"
	"fmt"
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

func (p Phase) Validate() error {
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
