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
	"github.com/sirupsen/logrus"
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
	LogCtx *logrus.Entry
	Seed   int64

	lock      *sync.RWMutex
	generator *rand.Rand
	randomMap map[string]*Result
}

func New(logCtx *logrus.Entry, seed int64) rpc.StepPlugin {
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
	rand.New(rand.NewSource(p.Seed))
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
		if err := json.Unmarshal(context.Config, &config); err != nil {
			return types.RpcStepResult{}, types.RpcError{ErrorString: "could not unmarshal config"}
		}
		if err := json.Unmarshal(context.Status, &state); err != nil {
			return types.RpcStepResult{}, types.RpcError{ErrorString: "could not unmarshal status"}
		}
	}

	// Already completed
	if state.Value != "" {
		return CompletedResult(state)
	}

	if config.Aggregate && state.SharedId != "" {
		lastStep := getLastStep(rollout, context.PluginName)
		if lastStep != nil {
			var lastState State
			if err := json.Unmarshal(lastStep.Status, &lastState); err != nil {
				return types.RpcStepResult{}, types.RpcError{ErrorString: "could not unmarshal last step status"}
			}
			if lastState.SharedId != "" {
				state.SharedId = lastState.SharedId
			} else {
				state.SharedId = lastState.Id
			}
		}
	}

	// Already started
	if state.Id != "" {
		p.lock.RLock()
		v, ok := p.randomMap[state.getId()]
		p.lock.RUnlock()
		if ok {
			if v.Id == state.Id {
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

func (p *rpcPlugin) Terminate(_ *v1alpha1.Rollout, _ *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {

	// probably need to use channel to cancel the sleep and not generate a number
	panic("not implemented") // TODO: Implement
}

func (p *rpcPlugin) Abort(_ *v1alpha1.Rollout, _ *types.RpcStepContext) (types.RpcStepResult, types.RpcError) {

	// just for show, set value to zero
	panic("not implemented") // TODO: Implement
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
			base = v.Value
		}
	}

	result := &Result{
		Value: p.generator.Intn(100) + base,
		Id:    state.Id,
	}
	p.randomMap[state.getId()] = result

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
