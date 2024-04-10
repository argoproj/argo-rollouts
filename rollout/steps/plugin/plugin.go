package plugin

import (
	"encoding/json"
	"fmt"
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

	stepStatus := p.getStepStatus(rollout)
	if stepStatus == nil {
		now := metatime.MetaNow()
		stepStatus = &v1alpha1.StepPluginStatus{
			Index:     p.index,
			Name:      p.name,
			StartedAt: &now,
			Phase:     v1alpha1.StepPluginPhaseRunning,
		}
	}
	resp, err := p.rpc.Run(rollout.DeepCopy(), p.getStepContext(stepStatus))
	finishedAt := metatime.MetaNow()
	if err.HasError() {
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
		if resp.RequeueAfter < minRequeueDuration {
			result.RequeueAfter = &resp.RequeueAfter
		}
	}

	return stepStatus, result, nil
}

func (p *stepPlugin) Terminate(rollout *v1alpha1.Rollout) (*v1alpha1.StepPluginStatus, error) {
	stepStatus := p.getStepStatus(rollout)
	if stepStatus == nil || stepStatus.Phase != v1alpha1.StepPluginPhaseRunning {
		return stepStatus, nil
	}

	resp, err := p.rpc.Terminate(rollout.DeepCopy(), p.getStepContext(stepStatus))
	finishedAt := metatime.MetaNow()
	if err.HasError() {
		stepStatus.Phase = v1alpha1.StepPluginPhaseError
		stepStatus.Message = fmt.Sprintf("Terminated: %s", err.Error())
		stepStatus.FinishedAt = &finishedAt
		return stepStatus, nil
	}

	if resp.Phase != "" {
		stepStatus.Phase = v1alpha1.StepPluginPhase(resp.Phase)
	}

	if stepStatus.Phase == v1alpha1.StepPluginPhaseRunning {
		p.log.Warnf("terminate cannot run asynchronously. Overriding status phase to %s.", v1alpha1.StepPluginPhaseSuccessful)
		stepStatus.Phase = v1alpha1.StepPluginPhaseSuccessful
	}

	stepStatus.Message = fmt.Sprintf("Terminated: %s", resp.Message)
	stepStatus.FinishedAt = &finishedAt
	stepStatus.Status = resp.Status
	return stepStatus, nil
}

func (p *stepPlugin) Abort(rollout *v1alpha1.Rollout) (*v1alpha1.StepPluginStatus, error) {
	return nil, fmt.Errorf("Not implemented")
}

func (p *stepPlugin) getStepStatus(rollout *v1alpha1.Rollout) *v1alpha1.StepPluginStatus {
	for _, s := range rollout.Status.Canary.StepPluginStatuses {
		if s.Index == p.index && s.Name == p.name {
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
