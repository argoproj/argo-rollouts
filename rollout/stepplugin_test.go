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
		stepPluginResolver.On("Resolve", int32(0), mock.Anything).Return(stepPluginMock, nil)

		r := newStepPluginRollout()
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
			reconcilerBase: reconcilerBase{
				stepPluginResolver:  stepPluginResolver,
				enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
				enqueueRolloutAfter: func(obj any, duration time.Duration) { t.Error("enqueueRolloutAfter should not be called") },
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
		require.Len(t, roCtx.newStatus.Canary.StepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.newStatus.Canary.StepPluginStatuses[0], *runStatus)
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
		require.Len(t, roCtx.newStatus.Canary.StepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.newStatus.Canary.StepPluginStatuses[0], *runStatus)
	})
}

func Test_StepPlugin_RunningReconciliation(t *testing.T) {
	setup := func(t *testing.T, phase v1alpha1.StepPluginPhase, requeueAfter *time.Duration) (*rolloutContext, *v1alpha1.StepPluginStatus) {
		stepPluginResolver := mocks.NewResolver(t)
		stepPluginMock := mocks.NewStepPlugin(t)
		stepPluginResolver.On("Resolve", int32(0), mock.Anything).Return(stepPluginMock, nil)

		r := newStepPluginRollout()
		roCtx := &rolloutContext{
			rollout: r,
			log:     logutil.WithRollout(r),
			reconcilerBase: reconcilerBase{
				stepPluginResolver:  stepPluginResolver,
				enqueueRollout:      func(obj any) { t.Error("enqueueRollout should not be called") },
				enqueueRolloutAfter: func(obj any, duration time.Duration) { t.Error("enqueueRolloutAfter should not be called") },
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
		require.Len(t, roCtx.newStatus.Canary.StepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.newStatus.Canary.StepPluginStatuses[0], *runStatus)
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
		require.Len(t, roCtx.newStatus.Canary.StepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.newStatus.Canary.StepPluginStatuses[0], *runStatus)
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
		stepPluginResolver.On("Resolve", int32(0), mock.Anything).Return(stepPluginMock, nil)

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
		require.Len(t, roCtx.newStatus.Canary.StepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.newStatus.Canary.StepPluginStatuses[0], *runStatus)
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
		require.Len(t, roCtx.newStatus.Canary.StepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.newStatus.Canary.StepPluginStatuses[0], *runStatus)
	})
	t.Run("Rollout is aborted", func(t *testing.T) {
		roCtx, _ := setup(t, v1alpha1.StepPluginPhaseFailed)

		err := roCtx.reconcileCanaryPluginStep()

		require.NoError(t, err)
		assert.True(t, roCtx.pauseContext.IsAborted())
	})
}

//Controller:
// Add test for plugin config
// Add InitPlugin call test

//Reconcile:
// Add disable feature

// Plugin:
//if run error, save message+phase, but not state

//When promote-full, terminate called on current step
//When abort, abort all steps? validate status? validate order

//error during run?
//error during abort?
//error during terminate? safe to ignore?

// Helper: write helper functions?

func Test_rolloutContext_isPluginStepCompleted(t *testing.T) {
	newRolloutContext := func(status *v1alpha1.StepPluginStatus) *rolloutContext {
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
		if status != nil {
			roCtx.newStatus.Canary.StepPluginStatuses = append(roCtx.newStatus.Canary.StepPluginStatuses, *status)
		}
		return roCtx
	}

	tests := []struct {
		name   string
		status *v1alpha1.StepPluginStatus
		index  int32
		want   bool
	}{
		{
			name:   "Status is not set",
			status: nil,
			index:  0,
			want:   false,
		},
		{
			name:   "Phase is successful",
			status: &v1alpha1.StepPluginStatus{Index: 0, Phase: v1alpha1.StepPluginPhaseSuccessful},
			index:  0,
			want:   true,
		},
		{
			name:   "Phase is failed",
			status: &v1alpha1.StepPluginStatus{Index: 0, Phase: v1alpha1.StepPluginPhaseFailed},
			index:  0,
			want:   true,
		},
		{
			name:   "Phase is error",
			status: &v1alpha1.StepPluginStatus{Index: 0, Phase: v1alpha1.StepPluginPhaseError},
			index:  0,
			want:   true,
		},
		{
			name:   "Phase is running",
			status: &v1alpha1.StepPluginStatus{Index: 0, Phase: v1alpha1.StepPluginPhaseRunning},
			index:  0,
			want:   false,
		},
		{
			name:   "status for index is missing",
			status: &v1alpha1.StepPluginStatus{Index: 1, Phase: v1alpha1.StepPluginPhaseSuccessful},
			index:  0,
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newRolloutContext(tt.status)
			if got := c.isPluginStepCompleted(tt.index); got != tt.want {
				t.Errorf("rolloutContext.isPluginStepCompleted() = %v, want %v", got, tt.want)
			}
		})
	}
}
