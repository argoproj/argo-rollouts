package plugin

import (
	"encoding/json"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/plugin/rpc/mocks"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_stepPlugin_Run(t *testing.T) {
	setup := func(t *testing.T) (*stepPlugin, *mocks.StepPlugin) {
		plugin := &stepPlugin{
			name:   "test-plugin",
			index:  0,
			config: json.RawMessage("value"),
			log:    log.WithFields(log.Fields{}),
		}
		rpcPluginMock := mocks.NewStepPlugin(t)
		plugin.rpc = rpcPluginMock
		return plugin, rpcPluginMock
	}

	t.Run("Argument set without status", func(t *testing.T) {
		p, rpcMock := setup(t)
		r := &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				Canary: v1alpha1.CanaryStatus{
					StepPluginStatuses: []v1alpha1.StepPluginStatus{
						{
							Index:     0,
							Name:      p.name,
							Status:    json.RawMessage("step status value"),
							Operation: v1alpha1.StepPluginOperationRun,
						},
					},
				},
			},
		}
		validateArguments := func(args mock.Arguments) {
			rollout, ok0 := args.Get(0).(*v1alpha1.Rollout)
			context, ok1 := args.Get(1).(*types.RpcStepContext)
			require.Truef(t, ok0, "Argument 0 is of the wrong type")
			require.Truef(t, ok1, "Argument 1 is of the wrong type")

			assert.NotNil(t, rollout)
			assert.NotSame(t, r, rollout)
			assert.NotNil(t, context)
			assert.Equal(t, p.name, context.PluginName)
			assert.Equal(t, p.config, context.Config)
			assert.Nil(t, context.Status)
		}

		rpcMock.On("Run", mock.Anything, mock.Anything).Run(validateArguments).Return(types.RpcStepResult{}, types.RpcError{}).Once()

		p.index = 1
		_, _, err := p.Run(r)

		require.NoError(t, err)
		rpcMock.AssertExpectations(t)

	})
	t.Run("Argument set with status", func(t *testing.T) {
		p, rpcMock := setup(t)
		r := &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				Canary: v1alpha1.CanaryStatus{
					StepPluginStatuses: []v1alpha1.StepPluginStatus{
						{
							Index:     0,
							Name:      p.name,
							Status:    json.RawMessage("step status value"),
							Operation: v1alpha1.StepPluginOperationRun,
						},
					},
				},
			},
		}
		validateArguments := func(args mock.Arguments) {
			rollout, ok0 := args.Get(0).(*v1alpha1.Rollout)
			context, ok1 := args.Get(1).(*types.RpcStepContext)
			require.Truef(t, ok0, "Argument 0 is of the wrong type")
			require.Truef(t, ok1, "Argument 1 is of the wrong type")

			assert.NotNil(t, rollout)
			assert.NotSame(t, r, rollout)
			assert.NotNil(t, context)
			assert.Equal(t, p.name, context.PluginName)
			assert.Equal(t, p.config, context.Config)
			assert.Equal(t, r.Status.Canary.StepPluginStatuses[0].Status, context.Status)
		}

		rpcMock.On("Run", mock.Anything, mock.Anything).Run(validateArguments).Return(types.RpcStepResult{}, types.RpcError{}).Once()

		p.index = 0
		_, _, err := p.Run(r)

		require.NoError(t, err)
		rpcMock.AssertExpectations(t)

	})
	t.Run("use existing state", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := &v1alpha1.StepPluginStatus{
			Index:     0,
			Name:      p.name,
			Status:    json.RawMessage("step status value"),
			StartedAt: &v1.Time{Time: time.Now().Add(30 * time.Minute * -1)},
			Phase:     v1alpha1.StepPluginPhaseRunning,
			Operation: v1alpha1.StepPluginOperationRun,
		}
		r := &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				Canary: v1alpha1.CanaryStatus{
					StepPluginStatuses: []v1alpha1.StepPluginStatus{
						*currentStatus,
					},
				},
			},
		}

		rpcResult := types.RpcStepResult{
			Phase:        types.PhaseSuccessful,
			Message:      "Good message",
			RequeueAfter: time.Hour,
			Status:       json.RawMessage("status"),
		}
		rpcMock.On("Run", mock.Anything, mock.Anything).Return(rpcResult, types.RpcError{}).Once()

		status, result, err := p.Run(r)

		require.NoError(t, err)
		rpcMock.AssertExpectations(t)

		assert.Equal(t, p.name, status.Name)
		assert.Equal(t, p.index, status.Index)
		assert.Equal(t, currentStatus.StartedAt, status.StartedAt)
		assert.NotEqual(t, currentStatus.UpdatedAt, status.UpdatedAt)
		assert.Equal(t, v1alpha1.StepPluginPhase(rpcResult.Phase), status.Phase)
		assert.Equal(t, v1alpha1.StepPluginOperationRun, status.Operation)
		assert.Equal(t, rpcResult.Message, status.Message)
		assert.Equal(t, rpcResult.Status, status.Status)
		assert.NotNil(t, status.FinishedAt)
		assert.Nil(t, result.RequeueAfter)
	})
	t.Run("Successful status", func(t *testing.T) {
		p, rpcMock := setup(t)
		r := &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				Canary: v1alpha1.CanaryStatus{
					StepPluginStatuses: []v1alpha1.StepPluginStatus{},
				},
			},
		}

		rpcResult := types.RpcStepResult{
			Phase:        types.PhaseSuccessful,
			Message:      "Good message",
			RequeueAfter: time.Hour,
			Status:       json.RawMessage("status"),
		}
		rpcMock.On("Run", mock.Anything, mock.Anything).Return(rpcResult, types.RpcError{}).Once()

		status, result, err := p.Run(r)

		require.NoError(t, err)
		rpcMock.AssertExpectations(t)

		assert.Equal(t, p.name, status.Name)
		assert.Equal(t, p.index, status.Index)
		assert.NotNil(t, status.StartedAt)
		assert.NotNil(t, status.FinishedAt)
		assert.Greater(t, status.FinishedAt.Time, status.StartedAt.Time)
		assert.Equal(t, v1alpha1.StepPluginPhase(rpcResult.Phase), status.Phase)
		assert.Equal(t, v1alpha1.StepPluginOperationRun, status.Operation)
		assert.Equal(t, rpcResult.Message, status.Message)
		assert.Equal(t, rpcResult.Status, status.Status)
		assert.Nil(t, result.RequeueAfter)
	})
	t.Run("Running status", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := &v1alpha1.StepPluginStatus{
			Index:     0,
			Name:      p.name,
			Status:    json.RawMessage("step status value"),
			StartedAt: &v1.Time{Time: time.Now().Add(30 * time.Minute * -1)},
			Operation: v1alpha1.StepPluginOperationRun,
		}
		r := &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				Canary: v1alpha1.CanaryStatus{
					StepPluginStatuses: []v1alpha1.StepPluginStatus{
						*currentStatus,
					},
				},
			},
		}

		rpcResult := types.RpcStepResult{
			Phase:        types.PhaseRunning,
			Message:      "Good message",
			RequeueAfter: time.Hour,
			Status:       json.RawMessage("status"),
		}
		rpcMock.On("Run", mock.Anything, mock.Anything).Return(rpcResult, types.RpcError{}).Once()

		status, result, err := p.Run(r)

		require.NoError(t, err)
		rpcMock.AssertExpectations(t)

		assert.Equal(t, p.name, status.Name)
		assert.Equal(t, p.index, status.Index)
		assert.NotNil(t, status.StartedAt)
		assert.Nil(t, status.FinishedAt)
		assert.Equal(t, v1alpha1.StepPluginPhase(rpcResult.Phase), status.Phase)
		assert.Equal(t, v1alpha1.StepPluginOperationRun, status.Operation)
		assert.Equal(t, rpcResult.Message, status.Message)
		assert.Equal(t, rpcResult.Status, status.Status)
		assert.Equal(t, rpcResult.RequeueAfter, *result.RequeueAfter)
	})
	t.Run("Running status without requeue", func(t *testing.T) {
		p, rpcMock := setup(t)
		r := &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				Canary: v1alpha1.CanaryStatus{
					StepPluginStatuses: []v1alpha1.StepPluginStatus{},
				},
			},
		}

		rpcResult := types.RpcStepResult{
			Phase:   types.PhaseRunning,
			Message: "Good message",
			Status:  json.RawMessage("status"),
		}
		rpcMock.On("Run", mock.Anything, mock.Anything).Return(rpcResult, types.RpcError{}).Once()

		status, result, err := p.Run(r)

		require.NoError(t, err)
		rpcMock.AssertExpectations(t)

		assert.Equal(t, p.name, status.Name)
		assert.Equal(t, p.index, status.Index)
		assert.NotNil(t, status.StartedAt)
		assert.Nil(t, status.FinishedAt)
		assert.Equal(t, v1alpha1.StepPluginPhase(rpcResult.Phase), status.Phase)
		assert.Equal(t, v1alpha1.StepPluginOperationRun, status.Operation)
		assert.Equal(t, rpcResult.Message, status.Message)
		assert.Equal(t, rpcResult.Status, status.Status)
		assert.Equal(t, defaultRequeuDuration, *result.RequeueAfter)
	})
	t.Run("Running status with requeue too fast", func(t *testing.T) {
		p, rpcMock := setup(t)
		r := &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				Canary: v1alpha1.CanaryStatus{
					StepPluginStatuses: []v1alpha1.StepPluginStatus{},
				},
			},
		}

		rpcResult := types.RpcStepResult{
			Phase:        types.PhaseRunning,
			Message:      "Good message",
			RequeueAfter: 1 * time.Second,
			Status:       json.RawMessage("status"),
		}
		rpcMock.On("Run", mock.Anything, mock.Anything).Return(rpcResult, types.RpcError{}).Once()

		status, result, err := p.Run(r)

		require.NoError(t, err)
		rpcMock.AssertExpectations(t)

		assert.Equal(t, p.name, status.Name)
		assert.Equal(t, p.index, status.Index)
		assert.NotNil(t, status.StartedAt)
		assert.Nil(t, status.FinishedAt)
		assert.Equal(t, v1alpha1.StepPluginPhase(rpcResult.Phase), status.Phase)
		assert.Equal(t, v1alpha1.StepPluginOperationRun, status.Operation)
		assert.Equal(t, rpcResult.Message, status.Message)
		assert.Equal(t, rpcResult.Status, status.Status)
		assert.Equal(t, defaultRequeuDuration, *result.RequeueAfter)
	})
	t.Run("Failed status", func(t *testing.T) {
		p, rpcMock := setup(t)
		r := &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				Canary: v1alpha1.CanaryStatus{
					StepPluginStatuses: []v1alpha1.StepPluginStatus{},
				},
			},
		}

		rpcResult := types.RpcStepResult{
			Phase:        types.PhaseSuccessful,
			Message:      "Good message",
			RequeueAfter: time.Hour,
			Status:       json.RawMessage("status"),
		}
		rpcMock.On("Run", mock.Anything, mock.Anything).Return(rpcResult, types.RpcError{}).Once()

		status, result, err := p.Run(r)

		require.NoError(t, err)
		rpcMock.AssertExpectations(t)

		assert.Equal(t, p.name, status.Name)
		assert.Equal(t, p.index, status.Index)
		assert.NotNil(t, status.StartedAt)
		assert.NotNil(t, status.FinishedAt)
		assert.Greater(t, status.FinishedAt.Time, status.StartedAt.Time)
		assert.Equal(t, v1alpha1.StepPluginPhase(rpcResult.Phase), status.Phase)
		assert.Equal(t, v1alpha1.StepPluginOperationRun, status.Operation)
		assert.Equal(t, rpcResult.Message, status.Message)
		assert.Equal(t, rpcResult.Status, status.Status)
		assert.Nil(t, result.RequeueAfter)
	})
	t.Run("Error status", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := &v1alpha1.StepPluginStatus{
			Index:     0,
			Name:      p.name,
			Status:    json.RawMessage("step status value"),
			StartedAt: &v1.Time{Time: time.Now().Add(30 * time.Minute * -1)},
			Operation: v1alpha1.StepPluginOperationRun,
		}
		r := &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				Canary: v1alpha1.CanaryStatus{
					StepPluginStatuses: []v1alpha1.StepPluginStatus{
						*currentStatus,
					},
				},
			},
		}

		invalidResult := types.RpcStepResult{
			Phase:        types.PhaseSuccessful,
			Message:      "This message should not be used",
			RequeueAfter: time.Hour,
			Status:       json.RawMessage("invalid status"),
		}
		expectedError := types.RpcError{
			ErrorString: "This is an error",
		}
		rpcMock.On("Run", mock.Anything, mock.Anything).Return(invalidResult, expectedError).Once()

		status, result, err := p.Run(r)

		require.NoError(t, err)
		rpcMock.AssertExpectations(t)

		assert.Equal(t, p.name, status.Name)
		assert.Equal(t, p.index, status.Index)
		assert.Equal(t, currentStatus.StartedAt, status.StartedAt)
		assert.NotEqual(t, currentStatus.UpdatedAt, status.UpdatedAt)
		assert.Equal(t, v1alpha1.StepPluginPhaseError, status.Phase)
		assert.Equal(t, v1alpha1.StepPluginOperationRun, status.Operation)
		assert.Equal(t, expectedError.Error(), status.Message)
		assert.Equal(t, currentStatus.Status, status.Status)
		assert.Nil(t, result.RequeueAfter)
	})
}

func Test_stepPlugin_Terminate(t *testing.T) {
	setup := func(t *testing.T) (*stepPlugin, *mocks.StepPlugin) {
		plugin := &stepPlugin{
			name:   "test-plugin",
			index:  0,
			config: json.RawMessage("value"),
			log:    log.WithFields(log.Fields{}),
		}
		rpcPluginMock := mocks.NewStepPlugin(t)
		plugin.rpc = rpcPluginMock
		return plugin, rpcPluginMock
	}
	newRunningStatus := func() *v1alpha1.StepPluginStatus {
		return &v1alpha1.StepPluginStatus{
			Index:     0,
			Name:      "test-plugin",
			Status:    json.RawMessage("step status value"),
			StartedAt: &v1.Time{Time: time.Now().Add(30 * time.Minute * -1)},
			Phase:     v1alpha1.StepPluginPhaseRunning,
			Operation: v1alpha1.StepPluginOperationRun,
		}
	}
	newRollout := func(s *v1alpha1.StepPluginStatus) *v1alpha1.Rollout {
		return &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				Canary: v1alpha1.CanaryStatus{
					StepPluginStatuses: []v1alpha1.StepPluginStatus{
						*s,
					},
				},
			},
		}
	}

	t.Run("Return nil status if not running", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := newRunningStatus()
		currentStatus.Phase = v1alpha1.StepPluginPhaseSuccessful
		currentStatus.Operation = v1alpha1.StepPluginOperationRun
		r := newRollout(currentStatus)

		rpcMock.On("Terminate", mock.Anything, mock.Anything).Maybe().Panic("Terminate should not be called when plugin is not running")

		status, err := p.Terminate(r)

		require.NoError(t, err)
		assert.Nil(t, status)
	})

	t.Run("Return nil status if already terminated", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := newRunningStatus()
		currentStatus.Phase = v1alpha1.StepPluginPhaseSuccessful
		currentStatus.Operation = v1alpha1.StepPluginOperationTerminate
		r := newRollout(currentStatus)

		rpcMock.On("Terminate", mock.Anything, mock.Anything).Maybe().Panic("Terminate should not be called when plugin is not running")

		status, err := p.Terminate(r)

		require.NoError(t, err)
		assert.Nil(t, status)
	})
	t.Run("Running phase overridden to failed if running", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := newRunningStatus()
		r := newRollout(currentStatus)

		rpcResult := types.RpcStepResult{
			Phase:        types.PhaseRunning,
			Message:      "Good message",
			RequeueAfter: time.Hour,
			Status:       json.RawMessage("status"),
		}
		rpcMock.On("Terminate", mock.Anything, mock.Anything).Return(rpcResult, types.RpcError{})

		status, err := p.Terminate(r)

		require.NoError(t, err)
		assert.Equal(t, p.name, status.Name)
		assert.Equal(t, p.index, status.Index)
		assert.NotNil(t, status.FinishedAt)
		assert.Greater(t, status.FinishedAt.Time, status.StartedAt.Time)
		assert.Equal(t, v1alpha1.StepPluginOperationTerminate, status.Operation)
		assert.Equal(t, v1alpha1.StepPluginPhaseFailed, status.Phase)
		assert.Equal(t, rpcResult.Message, status.Message)
		assert.Nil(t, status.Status)
	})
	t.Run("Completes successfully", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := newRunningStatus()
		r := newRollout(currentStatus)

		rpcResult := types.RpcStepResult{
			Phase:        types.PhaseSuccessful,
			Message:      "Good message",
			RequeueAfter: time.Hour,
			Status:       json.RawMessage("status"),
		}
		rpcMock.On("Terminate", mock.Anything, mock.Anything).Return(rpcResult, types.RpcError{})

		status, err := p.Terminate(r)

		require.NoError(t, err)

		assert.Equal(t, p.name, status.Name)
		assert.Equal(t, p.index, status.Index)
		assert.NotNil(t, status.FinishedAt)
		assert.Greater(t, status.FinishedAt.Time, status.StartedAt.Time)
		assert.Equal(t, v1alpha1.StepPluginPhase(rpcResult.Phase), status.Phase)
		assert.Equal(t, v1alpha1.StepPluginOperationTerminate, status.Operation)
		assert.Equal(t, rpcResult.Message, status.Message)
		assert.Nil(t, status.Status)
	})

	t.Run("Error status", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := newRunningStatus()
		r := newRollout(currentStatus)

		invalidResult := types.RpcStepResult{
			Phase:        types.PhaseSuccessful,
			Message:      "This message should not be used",
			RequeueAfter: time.Hour,
			Status:       json.RawMessage("invalid status"),
		}
		expectedError := types.RpcError{
			ErrorString: "This is an error",
		}
		rpcMock.On("Terminate", mock.Anything, mock.Anything).Return(invalidResult, expectedError)

		status, err := p.Terminate(r)

		require.NoError(t, err)

		assert.Equal(t, p.name, status.Name)
		assert.Equal(t, p.index, status.Index)
		assert.NotNil(t, status.FinishedAt)
		assert.Greater(t, status.FinishedAt.Time, status.StartedAt.Time)
		assert.Equal(t, v1alpha1.StepPluginOperationTerminate, status.Operation)
		assert.Equal(t, v1alpha1.StepPluginPhaseError, status.Phase)
		assert.Contains(t, status.Message, expectedError.Error())
		assert.Nil(t, status.Status)
	})
}

func Test_stepPlugin_Abort(t *testing.T) {
	setup := func(t *testing.T) (*stepPlugin, *mocks.StepPlugin) {
		plugin := &stepPlugin{
			name:   "test-plugin",
			index:  0,
			config: json.RawMessage("value"),
			log:    log.WithFields(log.Fields{}),
		}
		rpcPluginMock := mocks.NewStepPlugin(t)
		plugin.rpc = rpcPluginMock
		return plugin, rpcPluginMock
	}
	newRunningStatus := func() *v1alpha1.StepPluginStatus {
		return &v1alpha1.StepPluginStatus{
			Index:     0,
			Name:      "test-plugin",
			Status:    json.RawMessage("step status value"),
			StartedAt: &v1.Time{Time: time.Now().Add(30 * time.Minute * -1)},
			Phase:     v1alpha1.StepPluginPhaseRunning,
			Operation: v1alpha1.StepPluginOperationRun,
		}
	}
	newRollout := func(s *v1alpha1.StepPluginStatus) *v1alpha1.Rollout {
		return &v1alpha1.Rollout{
			Status: v1alpha1.RolloutStatus{
				Canary: v1alpha1.CanaryStatus{
					StepPluginStatuses: []v1alpha1.StepPluginStatus{
						*s,
					},
				},
			},
		}
	}

	t.Run("Return nil status if run is in error", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := newRunningStatus()
		currentStatus.Phase = v1alpha1.StepPluginPhaseError
		currentStatus.Operation = v1alpha1.StepPluginOperationRun
		r := newRollout(currentStatus)

		rpcMock.On("Abort", mock.Anything, mock.Anything).Maybe().Panic("Abort should not be called when plugin is not running or copmpleted")

		status, err := p.Abort(r)

		require.NoError(t, err)
		assert.Nil(t, status)
	})

	t.Run("Return nil status if run has failed", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := newRunningStatus()
		currentStatus.Phase = v1alpha1.StepPluginPhaseFailed
		currentStatus.Operation = v1alpha1.StepPluginOperationRun
		r := newRollout(currentStatus)

		rpcMock.On("Abort", mock.Anything, mock.Anything).Maybe().Panic("Abort should not be called when plugin is not running or completed")

		status, err := p.Abort(r)

		require.NoError(t, err)
		assert.Nil(t, status)
	})

	t.Run("Return nil status if already aborted", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := newRunningStatus()
		currentStatus.Phase = v1alpha1.StepPluginPhaseSuccessful
		currentStatus.Operation = v1alpha1.StepPluginOperationAbort
		r := newRollout(currentStatus)

		rpcMock.On("Abort", mock.Anything, mock.Anything).Maybe().Panic("Abort should not be called when plugin is not running")

		status, err := p.Abort(r)

		require.NoError(t, err)
		assert.Nil(t, status)
	})
	t.Run("Running phase overridden to failed if running", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := newRunningStatus()
		r := newRollout(currentStatus)

		rpcResult := types.RpcStepResult{
			Phase:        types.PhaseRunning,
			Message:      "Good message",
			RequeueAfter: time.Hour,
			Status:       json.RawMessage("status"),
		}
		rpcMock.On("Abort", mock.Anything, mock.Anything).Return(rpcResult, types.RpcError{})

		status, err := p.Abort(r)

		require.NoError(t, err)
		assert.Equal(t, p.name, status.Name)
		assert.Equal(t, p.index, status.Index)
		assert.NotNil(t, status.FinishedAt)
		assert.Greater(t, status.FinishedAt.Time, status.StartedAt.Time)
		assert.Equal(t, v1alpha1.StepPluginOperationAbort, status.Operation)
		assert.Equal(t, v1alpha1.StepPluginPhaseFailed, status.Phase)
		assert.Equal(t, rpcResult.Message, status.Message)
		assert.Nil(t, status.Status)
	})
	t.Run("Completes successfully", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := newRunningStatus()
		r := newRollout(currentStatus)

		rpcResult := types.RpcStepResult{
			Phase:        types.PhaseSuccessful,
			Message:      "Good message",
			RequeueAfter: time.Hour,
			Status:       json.RawMessage("status"),
		}
		rpcMock.On("Abort", mock.Anything, mock.Anything).Return(rpcResult, types.RpcError{})

		status, err := p.Abort(r)

		require.NoError(t, err)

		assert.Equal(t, p.name, status.Name)
		assert.Equal(t, p.index, status.Index)
		assert.NotNil(t, status.FinishedAt)
		assert.Greater(t, status.FinishedAt.Time, status.StartedAt.Time)
		assert.Equal(t, v1alpha1.StepPluginPhase(rpcResult.Phase), status.Phase)
		assert.Equal(t, v1alpha1.StepPluginOperationAbort, status.Operation)
		assert.Equal(t, rpcResult.Message, status.Message)
		assert.Nil(t, status.Status)
	})

	t.Run("Error status", func(t *testing.T) {
		p, rpcMock := setup(t)
		currentStatus := newRunningStatus()
		r := newRollout(currentStatus)

		invalidResult := types.RpcStepResult{
			Phase:        types.PhaseSuccessful,
			Message:      "This message should not be used",
			RequeueAfter: time.Hour,
			Status:       json.RawMessage("invalid status"),
		}
		expectedError := types.RpcError{
			ErrorString: "This is an error",
		}
		rpcMock.On("Abort", mock.Anything, mock.Anything).Return(invalidResult, expectedError)

		status, err := p.Abort(r)

		require.NoError(t, err)

		assert.Equal(t, p.name, status.Name)
		assert.Equal(t, p.index, status.Index)
		assert.NotNil(t, status.FinishedAt)
		assert.Greater(t, status.FinishedAt.Time, status.StartedAt.Time)
		assert.Equal(t, v1alpha1.StepPluginOperationAbort, status.Operation)
		assert.Equal(t, v1alpha1.StepPluginPhaseError, status.Phase)
		assert.Contains(t, status.Message, expectedError.Error())
		assert.Nil(t, status.Status)
	})
}
