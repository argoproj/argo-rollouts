package plugin

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

type Config struct {
	Return  string
	Requeue string

	Abort     Abort
	Terminate Terminate
}

type Abort struct {
	Return string
}

type Terminate struct {
	Return string
}

type State struct {
	Data  string
	Count int
}

type rpcPlugin struct {
	LogCtx *log.Entry
}

func New(logCtx *log.Entry) rpc.StepPlugin {
	return &rpcPlugin{
		LogCtx: logCtx,
	}
}

func (p *rpcPlugin) InitPlugin() types.RpcError {
	p.LogCtx.Infof("InitPlugin")
	return types.RpcError{}
}

func (p *rpcPlugin) Run(rollout *v1alpha1.Rollout, context *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	p.LogCtx.Infof("Run plugin for rollout %s/%s", rollout.Namespace, rollout.Name)

	// Get configs
	var config Config
	var state State
	if context != nil {
		if context.Config != nil {
			if err := json.Unmarshal(context.Config, &config); err != nil {
				return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Errorf("could not unmarshal config: %w", err).Error()}
			}
			p.LogCtx.Infof("Using config: %+v", config)
		}
		if context.Status != nil {
			if err := json.Unmarshal(context.Status, &state); err != nil {
				return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Errorf("could not unmarshal status: %w", err).Error()}
			}
			p.LogCtx.Infof("Using status: %+v", state)
		}
	}

	if state.Data == "" {
		state.Data = uuid.New().String()
	}
	state.Count = state.Count + 1

	var requeue time.Duration
	if config.Requeue != "" {
		v, err := time.ParseDuration(config.Requeue)
		if err != nil {
			return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Errorf("could not parse requeue duration: %w", err).Error()}
		}
		requeue = v
	}

	phase := types.PhaseSuccessful
	if config.Return != "" {
		phase = types.StepPhase(config.Return)
		if err := phase.Validate(); err != nil {
			return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Errorf("could not parse phase: %w", err).Error()}
		}
	}

	return Result(state, phase, requeue)
}

func (p *rpcPlugin) Terminate(rollout *v1alpha1.Rollout, context *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	p.LogCtx.Infof("Terminate plugin for rollout %s/%s", rollout.Namespace, rollout.Name)

	// Get configs
	var config Config
	var state State
	if context != nil {
		if context.Config != nil {
			if err := json.Unmarshal(context.Config, &config); err != nil {
				return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Errorf("could not unmarshal config: %w", err).Error()}
			}
		}
		if context.Status != nil {
			if err := json.Unmarshal(context.Status, &state); err != nil {
				return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Errorf("could not unmarshal status: %w", err).Error()}
			}
		}
	}

	phase := types.PhaseSuccessful
	if config.Terminate.Return != "" {
		phase = types.StepPhase(config.Terminate.Return)
		if err := phase.Validate(); err != nil {
			return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Errorf("could not parse phase: %w", err).Error()}
		}
	}

	return Result(state, phase, 0)
}

func (p *rpcPlugin) Abort(rollout *v1alpha1.Rollout, context *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	p.LogCtx.Infof("Abort plugin for rollout %s/%s", rollout.Namespace, rollout.Name)

	// Get configs
	var config Config
	var state State
	if context != nil {
		if context.Config != nil {
			if err := json.Unmarshal(context.Config, &config); err != nil {
				return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Errorf("could not unmarshal config: %w", err).Error()}
			}
		}
		if context.Status != nil {
			if err := json.Unmarshal(context.Status, &state); err != nil {
				return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Errorf("could not unmarshal status: %w", err).Error()}
			}
		}
	}

	phase := types.PhaseSuccessful
	if config.Abort.Return != "" {
		phase = types.StepPhase(config.Abort.Return)
		if err := phase.Validate(); err != nil {
			return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Errorf("could not parse phase: %w", err).Error()}
		}
	}

	return Result(state, phase, 0)
}

func (p *rpcPlugin) Type() string {
	return "e2e-plugin"
}

func Result(state State, phase types.StepPhase, requeue time.Duration) (types.RpcStepResult, types.RpcError) {
	stateRaw, err := json.Marshal(state)
	if err != nil {
		return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("Could not marshal state: %v", err)}
	}

	return types.RpcStepResult{
		Phase:        phase,
		RequeueAfter: requeue,
		Status:       stateRaw,
	}, types.RpcError{}
}
