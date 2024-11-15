package rollout

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/plugin/mocks"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func newStepPluginRollout() *v1alpha1.Rollout {
	steps := []v1alpha1.CanaryStep{
		{
			Plugin: &v1alpha1.PluginStep{
				Name: "test-plugin",
			},
		},
	}
	return newCanaryRollout("foo", 3, nil, steps, ptr.To(int32(0)), intstr.FromInt(1), intstr.FromInt(0))
}

func newStepPluginStatus(operation v1alpha1.StepPluginOperation, phase v1alpha1.StepPluginPhase) *v1alpha1.StepPluginStatus {
	now := metav1.Now()
	status := &v1alpha1.StepPluginStatus{
		Index:      0,
		Name:       "test-plugin",
		Message:    "this is a default message",
		Status:     json.RawMessage("value"),
		Operation:  operation,
		Phase:      phase,
		StartedAt:  &now,
		UpdatedAt:  &now,
		Executions: 1,
		Backoff:    "0s",
	}
	return status
}

func Test_stepPluginContext_reconcile_ReconciliationError(t *testing.T) {
	stepPluginResolver := mocks.NewResolver(t)
	r := newStepPluginRollout()
	logCtx := logutil.WithRollout(r)
	roCtx := &rolloutContext{
		rollout: r,
		log:     logutil.WithRollout(r),
		reconcilerBase: reconcilerBase{
			enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
			enqueueRolloutAfter: func(obj any, duration time.Duration) { t.Error("enqueueRolloutAfter should not be called") },
			recorder:            record.NewFakeEventRecorder(),
		},
		pauseContext: &pauseContext{
			rollout: r,
			log:     logCtx,
		},
		stepPluginContext: &stepPluginContext{
			resolver: stepPluginResolver,
			log:      logCtx,
		},
	}

	stepPluginResolver.On("Resolve", mock.Anything, mock.Anything, mock.Anything).Return(nil, fmt.Errorf("test error"))

	var requeuedAfter time.Duration
	roCtx.enqueueRolloutAfter = func(obj any, duration time.Duration) {
		requeuedAfter = duration
	}

	err := roCtx.stepPluginContext.reconcile(roCtx)

	require.NoError(t, err)
	assert.Equal(t, roCtx.rollout.Status.Canary.StepPluginStatuses, roCtx.stepPluginContext.stepPluginStatuses)
	assert.Equal(t, defaultControllerErrorBackoff, requeuedAfter)
	assert.True(t, roCtx.stepPluginContext.hasError)

}

func Test_stepPluginContext_reconcile_SuccessfulReconciliation(t *testing.T) {
	setup := func(t *testing.T) (*rolloutContext, *v1alpha1.StepPluginStatus) {
		stepPluginResolver := mocks.NewResolver(t)
		stepPluginMock := mocks.NewStepPlugin(t)
		stepPluginResolver.On("Resolve", int32(0), mock.Anything, mock.Anything).Return(stepPluginMock, nil)

		r := newStepPluginRollout()
		logCtx := logutil.WithRollout(r)
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
			reconcilerBase: reconcilerBase{
				enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
				enqueueRolloutAfter: func(obj any, duration time.Duration) { t.Error("enqueueRolloutAfter should not be called") },
				recorder:            record.NewFakeEventRecorder(),
			},
			pauseContext: &pauseContext{
				rollout: r,
				log:     logCtx,
			},
			stepPluginContext: &stepPluginContext{
				resolver: stepPluginResolver,
				log:      logCtx,
			},
		}

		runStatus := newStepPluginStatus(v1alpha1.StepPluginOperationRun, v1alpha1.StepPluginPhaseSuccessful)
		stepPluginMock.On("Run", r).Return(runStatus, nil)
		return roCtx, runStatus
	}

	t.Run("Status is added when not present", func(t *testing.T) {
		roCtx, runStatus := setup(t)

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.stepPluginContext.stepPluginStatuses[0], *runStatus)
	})
	t.Run("Status is updated when existing", func(t *testing.T) {
		roCtx, runStatus := setup(t)

		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     runStatus.Index,
				Name:      runStatus.Name,
				Message:   "this is the existing status",
				Operation: v1alpha1.StepPluginOperationRun,
				Phase:     v1alpha1.StepPluginPhaseRunning,
			},
		}

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.stepPluginContext.stepPluginStatuses[0], *runStatus)
	})
	t.Run("Status order is preserved when updating", func(t *testing.T) {
		roCtx, runStatus := setup(t)

		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     123,
				Name:      runStatus.Name,
				Operation: v1alpha1.StepPluginOperationRun,
			},
			{
				Index:     runStatus.Index,
				Name:      runStatus.Name,
				Operation: v1alpha1.StepPluginOperationRun,
			},
			{
				Index:     456,
				Name:      "other",
				Operation: v1alpha1.StepPluginOperationRun,
			},
		}

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 3)
		assert.Equal(t, int32(123), roCtx.stepPluginContext.stepPluginStatuses[0].Index)
		assert.Equal(t, runStatus.Index, roCtx.stepPluginContext.stepPluginStatuses[1].Index)
		assert.Equal(t, int32(456), roCtx.stepPluginContext.stepPluginStatuses[2].Index)
	})
}

func Test_stepPluginContext_reconcile_RunningReconciliation(t *testing.T) {
	setup := func(t *testing.T, phase v1alpha1.StepPluginPhase, backoff *time.Duration) (*rolloutContext, *v1alpha1.StepPluginStatus) {
		stepPluginResolver := mocks.NewResolver(t)
		stepPluginMock := mocks.NewStepPlugin(t)
		stepPluginResolver.On("Resolve", int32(0), mock.Anything, mock.Anything).Return(stepPluginMock, nil)

		r := newStepPluginRollout()
		logCtx := logutil.WithRollout(r)
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
			reconcilerBase: reconcilerBase{
				enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
				enqueueRolloutAfter: func(obj any, duration time.Duration) {},
				recorder:            record.NewFakeEventRecorder(),
			},
			pauseContext: &pauseContext{
				rollout: r,
				log:     logCtx,
			},
			stepPluginContext: &stepPluginContext{
				resolver: stepPluginResolver,
				log:      logCtx,
			},
		}

		runStatus := newStepPluginStatus(v1alpha1.StepPluginOperationRun, phase)
		if backoff != nil {
			runStatus.Backoff = v1alpha1.DurationString(backoff.String())
		}
		stepPluginMock.On("Run", r).Return(runStatus, nil)
		return roCtx, runStatus
	}

	t.Run("Status is added when not present", func(t *testing.T) {
		roCtx, runStatus := setup(t, v1alpha1.StepPluginPhaseRunning, nil)

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.stepPluginContext.stepPluginStatuses[0], *runStatus)
	})
	t.Run("Status is updated when existing", func(t *testing.T) {
		roCtx, runStatus := setup(t, v1alpha1.StepPluginPhaseRunning, nil)

		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     runStatus.Index,
				Name:      runStatus.Name,
				Message:   "this is the existing status",
				Operation: v1alpha1.StepPluginOperationRun,
				Phase:     v1alpha1.StepPluginPhaseRunning,
			},
		}

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.stepPluginContext.stepPluginStatuses[0], *runStatus)
	})
	t.Run("Rollout is added to the queue", func(t *testing.T) {
		expectedRequeueAfter := 123 * time.Second
		roCtx, _ := setup(t, v1alpha1.StepPluginPhaseRunning, &expectedRequeueAfter)

		var requeuedAfter time.Duration
		roCtx.enqueueRolloutAfter = func(obj any, duration time.Duration) {
			requeuedAfter = duration
		}

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		assert.GreaterOrEqual(t, requeuedAfter, expectedRequeueAfter)
		assert.LessOrEqual(t, requeuedAfter, expectedRequeueAfter+defaultBackoffDelay)
	})
}

func Test_stepPluginContext_reconcile_FailedReconciliation(t *testing.T) {
	setup := func(t *testing.T, phase v1alpha1.StepPluginPhase) (*rolloutContext, *v1alpha1.StepPluginStatus) {
		stepPluginResolver := mocks.NewResolver(t)
		stepPluginMock := mocks.NewStepPlugin(t)
		stepPluginResolver.On("Resolve", int32(0), mock.Anything, mock.Anything).Return(stepPluginMock, nil)

		r := newStepPluginRollout()
		logCtx := logutil.WithRollout(r)
		roCtx := &rolloutContext{
			rollout: r,
			log:     logCtx,
			reconcilerBase: reconcilerBase{
				enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
				enqueueRolloutAfter: func(obj any, duration time.Duration) { t.Error("enqueueRolloutAfter should not be called") },
				recorder:            record.NewFakeEventRecorder(),
			},
			pauseContext: &pauseContext{
				rollout: r,
				log:     logCtx,
			},
			stepPluginContext: &stepPluginContext{
				resolver: stepPluginResolver,
				log:      logCtx,
			},
		}

		runStatus := newStepPluginStatus(v1alpha1.StepPluginOperationRun, phase)
		stepPluginMock.On("Run", r).Return(runStatus, nil)
		return roCtx, runStatus
	}

	t.Run("Status is added when not present", func(t *testing.T) {
		roCtx, runStatus := setup(t, v1alpha1.StepPluginPhaseFailed)

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.stepPluginContext.stepPluginStatuses[0], *runStatus)
	})
	t.Run("Status is updated when existing", func(t *testing.T) {
		roCtx, runStatus := setup(t, v1alpha1.StepPluginPhaseFailed)

		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     runStatus.Index,
				Name:      runStatus.Name,
				Message:   "this is the existing status",
				Operation: v1alpha1.StepPluginOperationRun,
				Phase:     v1alpha1.StepPluginPhaseRunning,
			},
		}

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.stepPluginContext.stepPluginStatuses[0], *runStatus)
	})
	t.Run("Rollout is aborted", func(t *testing.T) {
		roCtx, failedStatus := setup(t, v1alpha1.StepPluginPhaseFailed)

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		assert.True(t, roCtx.pauseContext.IsAborted())
		assert.Contains(t, roCtx.pauseContext.abortMessage, failedStatus.Message)
	})
}

func Test_stepPluginContext_reconcile_FullyPromoted(t *testing.T) {
	newRolloutContext := func(t *testing.T) *rolloutContext {
		r := newStepPluginRollout()

		logCtx := logutil.WithRollout(r)
		roCtx := &rolloutContext{
			rollout: r,
			log:     logCtx,
			reconcilerBase: reconcilerBase{
				enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
				enqueueRolloutAfter: func(obj any, duration time.Duration) { t.Error("enqueueRolloutAfter should not be called") },
				recorder:            record.NewFakeEventRecorder(),
			},
			pauseContext: &pauseContext{
				rollout: r,
				log:     logCtx,
			},
			stepPluginContext: &stepPluginContext{
				log: logCtx,
			},
		}
		return roCtx
	}
	setup := func(t *testing.T) (*rolloutContext, *mocks.StepPlugin) {
		stepPluginResolver := mocks.NewResolver(t)
		stepPluginMock := mocks.NewStepPlugin(t)
		stepPluginResolver.On("Resolve", int32(0), mock.Anything, mock.Anything).Return(stepPluginMock, nil)

		roCtx := newRolloutContext(t)
		roCtx.stepPluginContext.resolver = stepPluginResolver

		return roCtx, stepPluginMock
	}

	t.Run("Rollout is Terminated on full promotion", func(t *testing.T) {
		roCtx, stepPluginMock := setup(t)
		roCtx.rollout.Status.PromoteFull = true
		roCtx.rollout.Status.StableRS = "stable-value"
		roCtx.rollout.Status.CurrentPodHash = "current-value"

		runStatus := newStepPluginStatus(v1alpha1.StepPluginOperationTerminate, v1alpha1.StepPluginPhaseSuccessful)
		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     runStatus.Index,
				Name:      runStatus.Name,
				Phase:     v1alpha1.StepPluginPhaseRunning,
				Operation: v1alpha1.StepPluginOperationRun,
			},
		}

		stepPluginMock.On("Terminate", mock.Anything).Return(runStatus, nil)

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 2)
		assert.EqualExportedValues(t, roCtx.stepPluginContext.stepPluginStatuses[1], *runStatus)
	})

	t.Run("Rollout is Terminated on fully promoted", func(t *testing.T) {
		roCtx, stepPluginMock := setup(t)
		roCtx.rollout.Status.PromoteFull = false
		roCtx.rollout.Status.StableRS = "stable-value"
		roCtx.rollout.Status.CurrentPodHash = "stable-value"

		runStatus := newStepPluginStatus(v1alpha1.StepPluginOperationTerminate, v1alpha1.StepPluginPhaseSuccessful)
		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     runStatus.Index,
				Name:      runStatus.Name,
				Phase:     v1alpha1.StepPluginPhaseRunning,
				Operation: v1alpha1.StepPluginOperationRun,
			},
		}

		stepPluginMock.On("Terminate", mock.Anything).Return(runStatus, nil)

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 2)
		assert.EqualExportedValues(t, roCtx.stepPluginContext.stepPluginStatuses[1], *runStatus)
	})

	t.Run("Rollout is Reconciled when already terminated", func(t *testing.T) {
		roCtx := newRolloutContext(t)
		roCtx.rollout.Status.PromoteFull = false
		roCtx.rollout.Status.StableRS = "stable-value"
		roCtx.rollout.Status.CurrentPodHash = "stable-value"
		roCtx.rollout.Status.CurrentStepIndex = ptr.To(int32(1))

		runStatus := newStepPluginStatus(v1alpha1.StepPluginOperationTerminate, v1alpha1.StepPluginPhaseSuccessful)
		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     runStatus.Index,
				Name:      runStatus.Name,
				Phase:     v1alpha1.StepPluginPhaseRunning,
				Operation: v1alpha1.StepPluginOperationRun,
			},
			{
				Index:     runStatus.Index,
				Name:      runStatus.Name,
				Phase:     v1alpha1.StepPluginPhaseSuccessful,
				Operation: v1alpha1.StepPluginOperationTerminate,
			},
		}

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 2)
		assert.EqualExportedValues(t, roCtx.rollout.Status.Canary.StepPluginStatuses[1], roCtx.stepPluginContext.stepPluginStatuses[1])
	})

	t.Run("Reconciliation error", func(t *testing.T) {
		roCtx, stepPluginMock := setup(t)
		roCtx.rollout.Status.PromoteFull = true

		runStatus := newStepPluginStatus(v1alpha1.StepPluginOperationTerminate, v1alpha1.StepPluginPhaseSuccessful)
		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     runStatus.Index,
				Name:      runStatus.Name,
				Phase:     v1alpha1.StepPluginPhaseRunning,
				Operation: v1alpha1.StepPluginOperationRun,
			},
		}

		var requeuedAfter time.Duration
		roCtx.enqueueRolloutAfter = func(obj any, duration time.Duration) {
			requeuedAfter = duration
		}

		stepPluginMock.On("Terminate", mock.Anything).Return(nil, fmt.Errorf("error"))

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 1)
		assert.Equal(t, roCtx.rollout.Status.Canary.StepPluginStatuses, roCtx.stepPluginContext.stepPluginStatuses)
		assert.Equal(t, defaultControllerErrorBackoff, requeuedAfter)
		assert.True(t, roCtx.stepPluginContext.hasError)
	})
}

func Test_stepPluginContext_reconcile_Aborted(t *testing.T) {
	setup := func(t *testing.T) (*rolloutContext, *mocks.Resolver) {
		stepPluginResolver := mocks.NewResolver(t)

		r := newStepPluginRollout()
		r.Status.Abort = true

		logCtx := logutil.WithRollout(r)
		roCtx := &rolloutContext{
			rollout: r,
			log:     logCtx,
			reconcilerBase: reconcilerBase{
				enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
				enqueueRolloutAfter: func(obj any, duration time.Duration) { t.Error("enqueueRolloutAfter should not be called") },
				recorder:            record.NewFakeEventRecorder(),
			},
			pauseContext: &pauseContext{
				rollout: r,
				log:     logCtx,
			},
			stepPluginContext: &stepPluginContext{
				resolver: stepPluginResolver,
				log:      logCtx,
			},
		}

		return roCtx, stepPluginResolver
	}
	t.Run("Abort called on each plugin step", func(t *testing.T) {
		roCtx, stepPluginResolver := setup(t)
		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     0,
				Name:      "test-plugin",
				Operation: v1alpha1.StepPluginOperationRun,
				Phase:     v1alpha1.StepPluginPhaseSuccessful,
			},
			{
				Index:     2,
				Name:      "test-plugin",
				Operation: v1alpha1.StepPluginOperationRun,
				Phase:     v1alpha1.StepPluginPhaseRunning,
			},
		}
		roCtx.rollout.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{
			{
				Plugin: &v1alpha1.PluginStep{
					Name: "test-plugin",
				},
			},
			{
				Pause: &v1alpha1.RolloutPause{}, // Not a step plugin
			},
			{
				Plugin: &v1alpha1.PluginStep{
					Name: "test-plugin",
				},
			},
		}
		roCtx.rollout.Status.CurrentStepIndex = int32Ptr(0)

		expectedAbortStatus := []*v1alpha1.StepPluginStatus{}
		for _, stepIndex := range []int32{0, 2} {
			abortStatus := &v1alpha1.StepPluginStatus{
				Index:     stepIndex,
				Name:      "test-plugin",
				Operation: v1alpha1.StepPluginOperationAbort,
				Phase:     v1alpha1.StepPluginPhaseSuccessful,
			}
			stepPluginMock := mocks.NewStepPlugin(t)
			stepPluginResolver.On("Resolve", stepIndex, mock.Anything, mock.Anything).Return(stepPluginMock, nil)
			stepPluginMock.On("Abort", mock.Anything).Return(abortStatus, nil)
			expectedAbortStatus = append(expectedAbortStatus, abortStatus)
		}

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 4)
		assert.EqualExportedValues(t, roCtx.rollout.Status.Canary.StepPluginStatuses[0], roCtx.stepPluginContext.stepPluginStatuses[0])
		assert.EqualExportedValues(t, roCtx.rollout.Status.Canary.StepPluginStatuses[1], roCtx.stepPluginContext.stepPluginStatuses[1])
		assert.EqualExportedValues(t, *expectedAbortStatus[1], roCtx.stepPluginContext.stepPluginStatuses[2])
		assert.EqualExportedValues(t, *expectedAbortStatus[0], roCtx.stepPluginContext.stepPluginStatuses[3])
	})
	t.Run("Rollout is reconciled when already aborted", func(t *testing.T) {
		roCtx, stepPluginResolver := setup(t)
		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     0,
				Name:      "test-plugin",
				Operation: v1alpha1.StepPluginOperationRun,
				Phase:     v1alpha1.StepPluginPhaseSuccessful,
			},
			{
				Index:     2,
				Name:      "test-plugin",
				Operation: v1alpha1.StepPluginOperationRun,
				Phase:     v1alpha1.StepPluginPhaseRunning,
			},
			{
				Index:     2,
				Name:      "test-plugin",
				Operation: v1alpha1.StepPluginOperationAbort,
				Phase:     v1alpha1.StepPluginPhaseSuccessful,
			},
			{
				Index:     0,
				Name:      "test-plugin",
				Operation: v1alpha1.StepPluginOperationAbort,
				Phase:     v1alpha1.StepPluginPhaseSuccessful,
			},
		}
		roCtx.rollout.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{
			{
				Plugin: &v1alpha1.PluginStep{
					Name: "test-plugin",
				},
			},
			{
				Pause: &v1alpha1.RolloutPause{}, // Not a step plugin
			},
			{
				Plugin: &v1alpha1.PluginStep{
					Name: "test-plugin",
				},
			},
		}
		roCtx.rollout.Status.CurrentStepIndex = int32Ptr(0)

		for _, stepIndex := range []int32{0, 2} {
			stepPluginMock := mocks.NewStepPlugin(t)
			stepPluginResolver.On("Resolve", stepIndex, mock.Anything, mock.Anything).Return(stepPluginMock, nil)
			stepPluginMock.On("Abort", mock.Anything).Return(nil, nil)
		}

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 4)
		assert.Equal(t, roCtx.rollout.Status.Canary.StepPluginStatuses, roCtx.stepPluginContext.stepPluginStatuses)
	})
	t.Run("Reconciliation error", func(t *testing.T) {
		roCtx, stepPluginResolver := setup(t)
		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     0,
				Name:      "test-plugin",
				Operation: v1alpha1.StepPluginOperationRun,
				Phase:     v1alpha1.StepPluginPhaseSuccessful,
			},
		}
		roCtx.rollout.Status.CurrentStepIndex = int32Ptr(0)

		runStatus := newStepPluginStatus(v1alpha1.StepPluginOperationTerminate, v1alpha1.StepPluginPhaseSuccessful)
		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     runStatus.Index,
				Name:      runStatus.Name,
				Phase:     v1alpha1.StepPluginPhaseRunning,
				Operation: v1alpha1.StepPluginOperationRun,
			},
		}

		var requeuedAfter time.Duration
		roCtx.enqueueRolloutAfter = func(obj any, duration time.Duration) {
			requeuedAfter = duration
		}

		stepPluginMock := mocks.NewStepPlugin(t)
		stepPluginResolver.On("Resolve", int32(0), mock.Anything, mock.Anything).Return(stepPluginMock, nil)
		stepPluginMock.On("Abort", mock.Anything).Return(nil, fmt.Errorf("error"))

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 1)
		assert.Equal(t, roCtx.rollout.Status.Canary.StepPluginStatuses, roCtx.stepPluginContext.stepPluginStatuses)
		assert.Equal(t, defaultControllerErrorBackoff, requeuedAfter)
		assert.True(t, roCtx.stepPluginContext.hasError)
	})
}

func Test_stepPluginContext_reconcile_Retry_After_Abort(t *testing.T) {
	setup := func(t *testing.T) (*rolloutContext, *mocks.Resolver) {
		stepPluginResolver := mocks.NewResolver(t)

		r := newStepPluginRollout()
		r.Status.Abort = true

		logCtx := logutil.WithRollout(r)
		roCtx := &rolloutContext{
			rollout: r,
			log:     logCtx,
			reconcilerBase: reconcilerBase{
				enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
				enqueueRolloutAfter: func(obj any, duration time.Duration) { t.Error("enqueueRolloutAfter should not be called") },
				recorder:            record.NewFakeEventRecorder(),
			},
			pauseContext: &pauseContext{
				rollout: r,
				log:     logCtx,
			},
			stepPluginContext: &stepPluginContext{
				resolver: stepPluginResolver,
				log:      logCtx,
			},
		}

		return roCtx, stepPluginResolver
	}

	t.Run("Rollout is retried when already aborted", func(t *testing.T) {
		roCtx, stepPluginResolver := setup(t)
		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     0,
				Name:      "test-plugin",
				Operation: v1alpha1.StepPluginOperationRun,
				Phase:     v1alpha1.StepPluginPhaseSuccessful,
			},
			{
				Index:     2,
				Name:      "test-plugin",
				Operation: v1alpha1.StepPluginOperationRun,
				Phase:     v1alpha1.StepPluginPhaseRunning,
			},
			{
				Index:     2,
				Name:      "test-plugin",
				Operation: v1alpha1.StepPluginOperationAbort,
				Phase:     v1alpha1.StepPluginPhaseSuccessful,
			},
			{
				Index:     0,
				Name:      "test-plugin",
				Operation: v1alpha1.StepPluginOperationAbort,
				Phase:     v1alpha1.StepPluginPhaseSuccessful,
			},
		}
		roCtx.rollout.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{
			{
				Plugin: &v1alpha1.PluginStep{
					Name: "test-plugin",
				},
			},
			{
				Pause: &v1alpha1.RolloutPause{},
			},
			{
				Plugin: &v1alpha1.PluginStep{
					Name: "test-plugin",
				},
			},
		}
		roCtx.rollout.Status.CurrentStepIndex = int32Ptr(0)
		roCtx.rollout.Status.Abort = false
		roCtx.rollout.Status.AbortedAt = nil

		stepPluginMock := mocks.NewStepPlugin(t)
		stepPluginResolver.On("Resolve", int32(0), mock.Anything, mock.Anything).Return(stepPluginMock, nil)
		runStatus := newStepPluginStatus(v1alpha1.StepPluginOperationRun, v1alpha1.StepPluginPhaseSuccessful)
		stepPluginMock.On("Run", roCtx.rollout).Return(runStatus, nil)

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginContext.stepPluginStatuses, 1)
		assert.Equal(t, *runStatus, roCtx.stepPluginContext.stepPluginStatuses[0])
	})
}

func Test_stepPluginContext_isStepPluginCompleted(t *testing.T) {
	newRolloutContext := func(statuses []*v1alpha1.StepPluginStatus, hasError bool) *rolloutContext {
		r := newStepPluginRollout()
		logCtx := logutil.WithRollout(r)
		roCtx := &rolloutContext{
			rollout: r,
			log:     logCtx,
			stepPluginContext: &stepPluginContext{
				log: logCtx,
			},
		}

		for _, s := range statuses {
			roCtx.stepPluginContext.stepPluginStatuses = append(roCtx.stepPluginContext.stepPluginStatuses, *s)
		}
		roCtx.stepPluginContext.hasError = hasError
		return roCtx
	}

	tests := []struct {
		name     string
		statuses []*v1alpha1.StepPluginStatus
		index    int32
		hasError bool
		want     bool
	}{
		{
			name:     "Status is not set",
			statuses: nil,
			index:    0,
			want:     false,
		},
		{
			name: "Phase is successful",
			statuses: []*v1alpha1.StepPluginStatus{
				{Index: 0, Operation: v1alpha1.StepPluginOperationRun, Phase: v1alpha1.StepPluginPhaseSuccessful},
			},
			index: 0,
			want:  true,
		},
		{
			name: "With transient error",
			statuses: []*v1alpha1.StepPluginStatus{
				{Index: 0, Operation: v1alpha1.StepPluginOperationRun, Phase: v1alpha1.StepPluginPhaseSuccessful},
			},
			index:    0,
			hasError: true,
			want:     false,
		},
		{
			name: "Phase is failed",
			statuses: []*v1alpha1.StepPluginStatus{
				{Index: 0, Operation: v1alpha1.StepPluginOperationRun, Phase: v1alpha1.StepPluginPhaseFailed},
			},
			index: 0,
			want:  true,
		},
		{
			name: "Phase is error",
			statuses: []*v1alpha1.StepPluginStatus{
				{Index: 0, Operation: v1alpha1.StepPluginOperationRun, Phase: v1alpha1.StepPluginPhaseError},
			},
			index: 0,
			want:  false,
		},
		{
			name: "Phase is running",
			statuses: []*v1alpha1.StepPluginStatus{
				{Index: 0, Operation: v1alpha1.StepPluginOperationRun, Phase: v1alpha1.StepPluginPhaseRunning},
			},
			index: 0,
			want:  false,
		},
		{
			name: "Phase is running, but terminated",
			statuses: []*v1alpha1.StepPluginStatus{
				{Index: 0, Operation: v1alpha1.StepPluginOperationRun, Phase: v1alpha1.StepPluginPhaseRunning},
				{Index: 0, Operation: v1alpha1.StepPluginOperationTerminate},
			},
			index: 0,
			want:  true,
		},
		{
			name: "Phase is running, but aborted",
			statuses: []*v1alpha1.StepPluginStatus{
				{Index: 0, Operation: v1alpha1.StepPluginOperationRun, Phase: v1alpha1.StepPluginPhaseRunning},
				{Index: 0, Operation: v1alpha1.StepPluginOperationTerminate},
			},
			index: 0,
			want:  true,
		},
		{
			name: "status for index is missing",
			statuses: []*v1alpha1.StepPluginStatus{
				{Index: 1, Operation: v1alpha1.StepPluginOperationRun, Phase: v1alpha1.StepPluginPhaseSuccessful},
			},
			index: 0,
			want:  false,
		},
		{
			name: "status is disabled",
			statuses: []*v1alpha1.StepPluginStatus{
				{Index: 0, Operation: v1alpha1.StepPluginOperationRun, Disabled: true},
			},
			index: 0,
			want:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newRolloutContext(tt.statuses, tt.hasError)
			if got := c.stepPluginContext.isStepPluginCompleted(tt.index, c.rollout.Spec.Strategy.Canary.Steps[tt.index].Plugin); got != tt.want {
				t.Errorf("rolloutContext.isStepPluginCompleted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_stepPluginContext_updateStatus(t *testing.T) {

	t.Run("status is not", func(t *testing.T) {
		stepPluginContext := stepPluginContext{
			stepPluginStatuses: nil,
		}

		expected := []v1alpha1.StepPluginStatus{}
		status := &v1alpha1.RolloutStatus{
			Canary: v1alpha1.CanaryStatus{
				StepPluginStatuses: expected,
			},
		}

		stepPluginContext.updateStatus(status)

		assert.Equal(t, expected, status.Canary.StepPluginStatuses)
	})
	t.Run("status is set", func(t *testing.T) {

		expected := []v1alpha1.StepPluginStatus{{}}

		stepPluginContext := stepPluginContext{
			stepPluginStatuses: expected,
		}

		status := &v1alpha1.RolloutStatus{
			Canary: v1alpha1.CanaryStatus{
				StepPluginStatuses: []v1alpha1.StepPluginStatus{},
			},
		}

		stepPluginContext.updateStatus(status)

		assert.Equal(t, expected, status.Canary.StepPluginStatuses)
	})
}
