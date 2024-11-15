package plugin

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

type Config struct {
	Async     bool
	Aggregate bool
}

type State struct {
	Id       string
	SharedId string

	Value string
}

type Result struct {
	Value int
	Id    string
}

type rpcPlugin struct {
	LogCtx *log.Entry
	Seed   int64

	lock      *sync.RWMutex
	generator *rand.Rand
	randomMap map[string]*Result
}

func New(logCtx *log.Entry, seed int64) rpc.StepPlugin {
	return &rpcPlugin{
		LogCtx: logCtx,
		Seed:   seed,
		lock:   &sync.RWMutex{},
	}
}

func (p *rpcPlugin) InitPlugin() types.RpcError {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.LogCtx.Infof("InitPlugin with seed %d", p.Seed)
	p.generator = rand.New(rand.NewSource(p.Seed))
	p.randomMap = map[string]*Result{}
	return types.RpcError{}
}

func (p *rpcPlugin) Run(rollout *v1alpha1.Rollout, context *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
	// if !p.validate(rollout) {

	// }

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

	// Already completed
	if state.Value != "" {
		return CompletedResult(state)
	}

	// If Aggregate, look for previous steps. We want to re-use the value in memory for that step id
	if config.Aggregate && state.SharedId == "" {
		lastStep := getLastStep(rollout, context.PluginName)
		if lastStep != nil {
			var lastState State
			if err := json.Unmarshal(lastStep.Status, &lastState); err != nil {
				return types.RpcStepResult{}, types.RpcError{ErrorString: "could not unmarshal last step status"}
			}
			if lastState.SharedId != "" {
				// consecutive aggregate steps all use the same id
				state.SharedId = lastState.SharedId
			} else {
				// Most likely the last step was not an aggregate, so we restart a sequence with this id
				state.SharedId = lastState.Id
			}
		}
	}

	// Already started, look if it is completed
	if state.Id != "" {
		p.lock.RLock()
		v, ok := p.randomMap[state.getId()]
		p.lock.RUnlock()
		if ok {
			if v.Id == state.Id {
				// Make sure the current step is the one that updated the value
				state.Value = strconv.Itoa(v.Value)
				return CompletedResult(state)
			}
		}
		return RunningResult(state)
	}

	state.Id = uuid.New().String()
	if config.Async {
		go func(state State) {
			time.Sleep(60 * time.Second)
			p.generate(state)
		}(state)
		return RunningResult(state)
	} else {
		state.Value = strconv.Itoa(p.generate(state).Value)
	}

	return CompletedResult(state)

}

func (p *rpcPlugin) Terminate(rollout *v1alpha1.Rollout, context *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {
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

	log.Infof("Ignoring future value for '%s'", state.Id)
	state.Value = "0"
	return CompletedResult(state)

}

func (p *rpcPlugin) Abort(_ *v1alpha1.Rollout, context *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {

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

	p.lock.Lock()
	defer p.lock.Unlock()

	log.Infof("deleting entry for id '%s'", state.getId())
	delete(p.randomMap, state.getId())

	return CompletedResult(state)
}

func (p *rpcPlugin) Type() string {
	return "plugin-example"
}

// func (p *RpcPlugin) validate(rollout *v1alpha1.Rollout) bool {
// 	if rollout == nil || rollout.Status == nil || rollout.Status.Canary == nil {}
// }

func (p *rpcPlugin) generate(state State) *Result {
	p.lock.Lock()
	defer p.lock.Unlock()

	base := 0
	if state.SharedId != "" {
		v, ok := p.randomMap[state.SharedId]
		if ok {
			log.Infof("Using base '%d' for aggregate %s", v.Value, state.SharedId)
			base = v.Value
		}
	}

	result := &Result{
		Value: p.generator.Intn(100) + base,
		Id:    state.Id,
	}
	p.randomMap[state.getId()] = result
	log.Infof("Set '%d' for id %s", result.Value, state.getId())

	return result
}

func (s State) getId() string {
	if s.SharedId != "" {
		return s.SharedId
	}
	return s.Id
}

func getLastStep(rollout *v1alpha1.Rollout, name string) *v1alpha1.StepPluginStatus {
	var last *v1alpha1.StepPluginStatus
	currentStepIndex := *rollout.Status.CurrentStepIndex
	for _, status := range rollout.Status.Canary.StepPluginStatuses {
		if status.Name != name || status.Index == currentStepIndex {
			continue
		}

		if last == nil || status.Index > last.Index {
			last = &status
		}
	}
	return last
}

func CompletedResult(state State) (types.RpcStepResult, types.RpcError) {
	stateRaw, err := json.Marshal(state)
	if err != nil {
		return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("Could not marshal state: %v", err)}
	}

	return types.RpcStepResult{
		Phase:   types.PhaseSuccessful,
		Message: "Operation completed",
		Status:  stateRaw,
	}, types.RpcError{}
}

func RunningResult(state State) (types.RpcStepResult, types.RpcError) {
	stateRaw, err := json.Marshal(state)
	if err != nil {
		return types.RpcStepResult{}, types.RpcError{ErrorString: fmt.Sprintf("Could not marshal state: %v", err)}
	}

	return types.RpcStepResult{
		Phase:        types.PhaseRunning,
		RequeueAfter: 15 * time.Second,
		Status:       stateRaw,
	}, types.RpcError{}
}
