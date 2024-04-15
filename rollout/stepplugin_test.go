package rollout

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/plugin"
	"github.com/argoproj/argo-rollouts/rollout/steps/plugin/mocks"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

func newStepPluginStatus() *v1alpha1.StepPluginStatus {
	return &v1alpha1.StepPluginStatus{
		Index:  0,
		Name:   "test-plugin",
		Status: json.RawMessage("value"),
	}
}

func Test_StepPlugin_SuccessfulReconciliation(t *testing.T) {
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
				stepPluginResolver:  stepPluginResolver,
				enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
				enqueueRolloutAfter: func(obj any, duration time.Duration) { t.Error("enqueueRolloutAfter should not be called") },
			},
			pauseContext: &pauseContext{
				rollout: r,
				log:     logCtx,
			},
		}

		runStatus := newStepPluginStatus()
		stepPluginMock.On("Run", r).Return(runStatus, nil, nil)
		runStatus.Phase = v1alpha1.StepPluginPhaseSuccessful
		return roCtx, runStatus
	}

	t.Run("Status is added when not present", func(t *testing.T) {
		roCtx, runStatus := setup(t)
		runStatus.Phase = v1alpha1.StepPluginPhaseSuccessful

		err := roCtx.reconcileCanaryPluginStep()

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.stepPluginStatuses[0], *runStatus)
	})
	t.Run("Status is updated when existing", func(t *testing.T) {
		roCtx, runStatus := setup(t)
		runStatus.Phase = v1alpha1.StepPluginPhaseSuccessful

		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:   runStatus.Index,
				Name:    runStatus.Name,
				Message: "this is the existing status",
				Phase:   v1alpha1.StepPluginPhaseRunning,
			},
		}

		err := roCtx.reconcileCanaryPluginStep()

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.stepPluginStatuses[0], *runStatus)
	})
	t.Run("Status order is preserved when updating", func(t *testing.T) {
		roCtx, runStatus := setup(t)
		runStatus.Phase = v1alpha1.StepPluginPhaseSuccessful

		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index: 123,
				Name:  runStatus.Name,
			},
			{
				Index: runStatus.Index,
				Name:  runStatus.Name,
			},
			{
				Index: 456,
				Name:  "other",
			},
		}

		err := roCtx.reconcileCanaryPluginStep()

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginStatuses, 3)
		assert.Equal(t, int32(123), roCtx.stepPluginStatuses[0].Index)
		assert.Equal(t, runStatus.Index, roCtx.stepPluginStatuses[1].Index)
		assert.Equal(t, int32(456), roCtx.stepPluginStatuses[2].Index)
	})
}

func Test_StepPlugin_RunningReconciliation(t *testing.T) {
	setup := func(t *testing.T, phase v1alpha1.StepPluginPhase, requeueAfter *time.Duration) (*rolloutContext, *v1alpha1.StepPluginStatus) {
		stepPluginResolver := mocks.NewResolver(t)
		stepPluginMock := mocks.NewStepPlugin(t)
		stepPluginResolver.On("Resolve", int32(0), mock.Anything, mock.Anything).Return(stepPluginMock, nil)

		r := newStepPluginRollout()
		logCtx := logutil.WithRollout(r)
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
			reconcilerBase: reconcilerBase{
				stepPluginResolver:  stepPluginResolver,
				enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
				enqueueRolloutAfter: func(obj any, duration time.Duration) { t.Error("enqueueRolloutAfter should not be called") },
			},
			pauseContext: &pauseContext{
				rollout: r,
				log:     logCtx,
			},
		}

		runStatus := newStepPluginStatus()
		runResult := &plugin.StepResult{
			RequeueAfter: requeueAfter,
		}

		stepPluginMock.On("Run", r).Return(runStatus, runResult, nil)
		runStatus.Phase = phase
		return roCtx, runStatus
	}

	t.Run("Status is added when not present", func(t *testing.T) {
		roCtx, runStatus := setup(t, v1alpha1.StepPluginPhaseRunning, nil)

		err := roCtx.reconcileCanaryPluginStep()

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.stepPluginStatuses[0], *runStatus)
	})
	t.Run("Status is updated when existing", func(t *testing.T) {
		roCtx, runStatus := setup(t, v1alpha1.StepPluginPhaseRunning, nil)

		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:   runStatus.Index,
				Name:    runStatus.Name,
				Message: "this is the existing status",
				Phase:   v1alpha1.StepPluginPhaseRunning,
			},
		}

		err := roCtx.reconcileCanaryPluginStep()

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.stepPluginStatuses[0], *runStatus)
	})
	t.Run("Rollout is added to the queue", func(t *testing.T) {
		expectedRequeueAfter := 123 * time.Second
		roCtx, _ := setup(t, v1alpha1.StepPluginPhaseRunning, &expectedRequeueAfter)

		var requeuedAfter time.Duration
		roCtx.enqueueRolloutAfter = func(obj any, duration time.Duration) {
			requeuedAfter = duration
		}

		err := roCtx.reconcileCanaryPluginStep()

		require.NoError(t, err)
		assert.Equal(t, expectedRequeueAfter, requeuedAfter)
	})
}

func Test_StepPlugin_FailedReconciliation(t *testing.T) {
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
				stepPluginResolver:  stepPluginResolver,
				enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
				enqueueRolloutAfter: func(obj any, duration time.Duration) { t.Error("enqueueRolloutAfter should not be called") },
			},
			pauseContext: &pauseContext{
				rollout: r,
				log:     logCtx,
			},
		}

		runStatus := newStepPluginStatus()
		stepPluginMock.On("Run", r).Return(runStatus, nil, nil)
		runStatus.Phase = phase
		return roCtx, runStatus
	}

	t.Run("Status is added when not present", func(t *testing.T) {
		roCtx, runStatus := setup(t, v1alpha1.StepPluginPhaseFailed)

		err := roCtx.reconcileCanaryPluginStep()

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.stepPluginStatuses[0], *runStatus)
	})
	t.Run("Status is updated when existing", func(t *testing.T) {
		roCtx, runStatus := setup(t, v1alpha1.StepPluginPhaseFailed)

		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:   runStatus.Index,
				Name:    runStatus.Name,
				Message: "this is the existing status",
				Phase:   v1alpha1.StepPluginPhaseRunning,
			},
		}

		err := roCtx.reconcileCanaryPluginStep()

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.stepPluginStatuses[0], *runStatus)
	})
	t.Run("Rollout is aborted", func(t *testing.T) {
		roCtx, _ := setup(t, v1alpha1.StepPluginPhaseFailed)

		err := roCtx.reconcileCanaryPluginStep()

		require.NoError(t, err)
		assert.True(t, roCtx.pauseContext.IsAborted())
	})
}

func Test_StepPlugin_FullyPromoted(t *testing.T) {
	setup := func(t *testing.T) (*rolloutContext, *mocks.StepPlugin) {
		stepPluginResolver := mocks.NewResolver(t)
		stepPluginMock := mocks.NewStepPlugin(t)
		stepPluginResolver.On("Resolve", int32(0), mock.Anything, mock.Anything).Return(stepPluginMock, nil)

		r := newStepPluginRollout()
		r.Status.PromoteFull = true

		logCtx := logutil.WithRollout(r)
		roCtx := &rolloutContext{
			rollout: r,
			log:     logCtx,
			reconcilerBase: reconcilerBase{
				stepPluginResolver:  stepPluginResolver,
				enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
				enqueueRolloutAfter: func(obj any, duration time.Duration) { t.Error("enqueueRolloutAfter should not be called") },
			},
			pauseContext: &pauseContext{
				rollout: r,
				log:     logCtx,
			},
		}

		return roCtx, stepPluginMock
	}

	t.Run("Rollout is Terminated on full promotion", func(t *testing.T) {
		roCtx, stepPluginMock := setup(t)
		runStatus := newStepPluginStatus()
		runStatus.Operation = v1alpha1.StepPluginOperationTerminate
		runStatus.Phase = v1alpha1.StepPluginPhaseSuccessful
		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index:     runStatus.Index,
				Name:      runStatus.Name,
				Phase:     v1alpha1.StepPluginPhaseSuccessful,
				Operation: v1alpha1.StepPluginOperationTerminate,
			},
		}

		stepPluginMock.On("Terminate", mock.Anything).Return(runStatus, nil)

		err := roCtx.reconcileCanaryPluginStep()

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.stepPluginStatuses[0], *runStatus)
	})
}

func Test_StepPlugin_Aborted(t *testing.T) {
	setup := func(t *testing.T) (*rolloutContext, *mocks.Resolver) {
		stepPluginResolver := mocks.NewResolver(t)

		r := newStepPluginRollout()
		r.Status.Abort = true

		logCtx := logutil.WithRollout(r)
		roCtx := &rolloutContext{
			rollout: r,
			log:     logCtx,
			reconcilerBase: reconcilerBase{
				stepPluginResolver:  stepPluginResolver,
				enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
				enqueueRolloutAfter: func(obj any, duration time.Duration) { t.Error("enqueueRolloutAfter should not be called") },
			},
			pauseContext: &pauseContext{
				rollout: r,
				log:     logCtx,
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
		roCtx.rollout.Status.CurrentStepIndex = int32Ptr(2)

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

		err := roCtx.reconcileCanaryPluginStep()

		require.NoError(t, err)
		require.Len(t, roCtx.stepPluginStatuses, 3)
		assert.EqualExportedValues(t, roCtx.rollout.Status.Canary.StepPluginStatuses[0], roCtx.stepPluginStatuses[0])
		assert.EqualExportedValues(t, *expectedAbortStatus[1], roCtx.stepPluginStatuses[1])
		assert.EqualExportedValues(t, *expectedAbortStatus[0], roCtx.stepPluginStatuses[2])
	})
}

//Controller:
// Add test for plugin config
// Add InitPlugin call test

//Reconcile:
// Add disable feature

// Helper: write helper functions?

func Test_rolloutContext_isStepPluginCompleted(t *testing.T) {
	newRolloutContext := func(statuses []*v1alpha1.StepPluginStatus) *rolloutContext {
		r := newStepPluginRollout()
		logCtx := logutil.WithRollout(r)
		roCtx := &rolloutContext{
			rollout: r,
			log:     logCtx,
		}
		roCtx.newStatus = v1alpha1.RolloutStatus{
			Canary: v1alpha1.CanaryStatus{
				StepPluginStatuses: []v1alpha1.StepPluginStatus{},
			},
		}
		for _, s := range statuses {
			roCtx.stepPluginStatuses = append(roCtx.stepPluginStatuses, *s)
		}
		roCtx.newStatus.Canary.StepPluginStatuses = roCtx.calculateStepPluginStatus()
		return roCtx
	}

	tests := []struct {
		name     string
		statuses []*v1alpha1.StepPluginStatus
		index    int32
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newRolloutContext(tt.statuses)
			if got := c.isStepPluginCompleted(tt.index); got != tt.want {
				t.Errorf("rolloutContext.isStepPluginCompleted() = %v, want %v", got, tt.want)
			}
		})
	}
}
