package plugin

import (
	"encoding/json"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
	metatime "github.com/argoproj/argo-rollouts/utils/time"
	log "github.com/sirupsen/logrus"
)

type stepPlugin struct {
	rpc    rpc.StepPlugin
	index  int32
	name   string
	config json.RawMessage
	log    *log.Entry
}

type StepResult struct {
	RequeueAfter *time.Duration
}

type StepPlugin interface {
	Run(*v1alpha1.Rollout) (*v1alpha1.StepPluginStatus, *StepResult, error)
	Terminate(*v1alpha1.Rollout) (*v1alpha1.StepPluginStatus, error)
	Abort(*v1alpha1.Rollout) (*v1alpha1.StepPluginStatus, error)
}

var (
	minRequeueDuration    = time.Second * 10
	defaultRequeuDuration = time.Second * 30
)

// Run exectues a plugin
func (p *stepPlugin) Run(rollout *v1alpha1.Rollout) (*v1alpha1.StepPluginStatus, *StepResult, error) {
	result := &StepResult{}

	stepStatus := p.getStepStatus(rollout, v1alpha1.StepPluginOperationRun)
	if stepStatus == nil {
		now := metatime.MetaNow()
		stepStatus = &v1alpha1.StepPluginStatus{
			Index:     p.index,
			Name:      p.name,
			StartedAt: &now,
			Operation: v1alpha1.StepPluginOperationRun,
			Phase:     v1alpha1.StepPluginPhaseRunning,
		}
	}
	resp, err := p.rpc.Run(rollout.DeepCopy(), p.getStepContext(stepStatus))
	finishedAt := metatime.MetaNow()
	if err.HasError() {
		p.log.Errorf("error during plugin execution")
		stepStatus.Phase = v1alpha1.StepPluginPhaseError
		stepStatus.Message = err.Error()
		stepStatus.FinishedAt = &finishedAt
		return stepStatus, result, nil
	}

	stepStatus.Message = resp.Message
	if resp.Phase != "" {
		stepStatus.Phase = v1alpha1.StepPluginPhase(resp.Phase)
	}

	if stepStatus.Phase == v1alpha1.StepPluginPhaseSuccessful || stepStatus.Phase == v1alpha1.StepPluginPhaseFailed {
		stepStatus.FinishedAt = &finishedAt
	}

	stepStatus.Status = resp.Status

	if stepStatus.Phase == v1alpha1.StepPluginPhaseRunning {
		result.RequeueAfter = &defaultRequeuDuration
		if resp.RequeueAfter > minRequeueDuration {
			result.RequeueAfter = &resp.RequeueAfter
		}
	}

	return stepStatus, result, nil
}

func (p *stepPlugin) Terminate(rollout *v1alpha1.Rollout) (*v1alpha1.StepPluginStatus, error) {
	terminateStatus := p.getStepStatus(rollout, v1alpha1.StepPluginOperationTerminate)
	if terminateStatus != nil {
		// Already terminated
		return nil, nil
	}

	stepStatus := p.getStepStatus(rollout, v1alpha1.StepPluginOperationRun)
	if stepStatus == nil || stepStatus.Phase != v1alpha1.StepPluginPhaseRunning {
		// Step is not running, no need to call terminate
		return nil, nil
	}

	now := metatime.MetaNow()
	terminateStatus = &v1alpha1.StepPluginStatus{
		Index:     stepStatus.Index,
		Name:      stepStatus.Name,
		StartedAt: &now,
		Operation: v1alpha1.StepPluginOperationTerminate,
		Phase:     v1alpha1.StepPluginPhaseSuccessful,
	}
	resp, err := p.rpc.Terminate(rollout.DeepCopy(), p.getStepContext(stepStatus))
	finishedAt := metatime.MetaNow()
	if err.HasError() {
		terminateStatus.Phase = v1alpha1.StepPluginPhaseError
		terminateStatus.Message = err.Error()
		terminateStatus.FinishedAt = &finishedAt
		return terminateStatus, nil
	}

	if resp.Phase != "" {
		terminateStatus.Phase = v1alpha1.StepPluginPhase(resp.Phase)
	}

	if terminateStatus.Phase == v1alpha1.StepPluginPhaseRunning {
		p.log.Warnf("terminate cannot run asynchronously. Overriding status phase to %s.", v1alpha1.StepPluginPhaseFailed)
		terminateStatus.Phase = v1alpha1.StepPluginPhaseFailed
	}

	terminateStatus.Message = resp.Message
	terminateStatus.FinishedAt = &finishedAt
	return terminateStatus, nil
}

func (p *stepPlugin) Abort(rollout *v1alpha1.Rollout) (*v1alpha1.StepPluginStatus, error) {
	abortStatus := p.getStepStatus(rollout, v1alpha1.StepPluginOperationAbort)
	if abortStatus != nil {
		// Already aborted
		return nil, nil
	}

	stepStatus := p.getStepStatus(rollout, v1alpha1.StepPluginOperationRun)
	if stepStatus == nil || (stepStatus.Phase != v1alpha1.StepPluginPhaseRunning && stepStatus.Phase != v1alpha1.StepPluginPhaseSuccessful) {
		// Step plugin isn't in a phase where it needs to be aborted
		return nil, nil
	}

	now := metatime.MetaNow()
	abortStatus = &v1alpha1.StepPluginStatus{
		Index:     stepStatus.Index,
		Name:      stepStatus.Name,
		StartedAt: &now,
		Operation: v1alpha1.StepPluginOperationAbort,
		Phase:     v1alpha1.StepPluginPhaseSuccessful,
	}
	resp, err := p.rpc.Abort(rollout.DeepCopy(), p.getStepContext(stepStatus))
	finishedAt := metatime.MetaNow()
	if err.HasError() {
		abortStatus.Phase = v1alpha1.StepPluginPhaseError
		abortStatus.Message = err.Error()
		abortStatus.FinishedAt = &finishedAt
		return abortStatus, nil
	}

	if resp.Phase != "" {
		abortStatus.Phase = v1alpha1.StepPluginPhase(resp.Phase)
	}

	if abortStatus.Phase == v1alpha1.StepPluginPhaseRunning {
		p.log.Warnf("abort cannot run asynchronously. Overriding status phase to %s.", v1alpha1.StepPluginPhaseFailed)
		abortStatus.Phase = v1alpha1.StepPluginPhaseFailed
	}

	abortStatus.Message = resp.Message
	abortStatus.FinishedAt = &finishedAt
	return abortStatus, nil
}

func (p *stepPlugin) getStepStatus(rollout *v1alpha1.Rollout, operation v1alpha1.StepPluginOperation) *v1alpha1.StepPluginStatus {
	for _, s := range rollout.Status.Canary.StepPluginStatuses {
		if s.Index == p.index && s.Name == p.name && s.Operation == operation {
			return s.DeepCopy()
		}
	}
	return nil
}

func (p *stepPlugin) getStepContext(stepStatus *v1alpha1.StepPluginStatus) *types.RpcStepContext {
	var status json.RawMessage = nil
	if stepStatus != nil {
		status = stepStatus.Status
	}
	return &types.RpcStepContext{
		PluginName: p.name,
		Config:     p.config,
		Status:     status,
	}
}
