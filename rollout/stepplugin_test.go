package rollout

import (
	"encoding/json"
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

func Test_StepPlugin_SuccessfulReconciliation(t *testing.T) {
	setup := func(t *testing.T) (*rolloutContext, *v1alpha1.StepPluginStatus) {
		stepPluginResolver := mocks.NewResolver(t)
		stepPluginMock := mocks.NewStepPlugin(t)
		stepPluginMock.On("Enabled").Return(true).Maybe()
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
		stepPluginMock.On("Run", r).Return(runStatus, nil, nil)
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

func Test_StepPlugin_RunningReconciliation(t *testing.T) {
	setup := func(t *testing.T, phase v1alpha1.StepPluginPhase, backoff *time.Duration) (*rolloutContext, *v1alpha1.StepPluginStatus) {
		stepPluginResolver := mocks.NewResolver(t)
		stepPluginMock := mocks.NewStepPlugin(t)
		stepPluginMock.On("Enabled").Return(true).Maybe()
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
		assert.LessOrEqual(t, requeuedAfter, expectedRequeueAfter+5*time.Second)
	})
}

func Test_StepPlugin_FailedReconciliation(t *testing.T) {
	setup := func(t *testing.T, phase v1alpha1.StepPluginPhase) (*rolloutContext, *v1alpha1.StepPluginStatus) {
		stepPluginResolver := mocks.NewResolver(t)
		stepPluginMock := mocks.NewStepPlugin(t)
		stepPluginMock.On("Enabled").Return(true).Maybe()
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
		stepPluginMock.On("Run", r).Return(runStatus, nil, nil)
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
		roCtx, _ := setup(t, v1alpha1.StepPluginPhaseFailed)

		err := roCtx.stepPluginContext.reconcile(roCtx)

		require.NoError(t, err)
		assert.True(t, roCtx.pauseContext.IsAborted())
	})
}

func Test_StepPlugin_FullyPromoted(t *testing.T) {
	setup := func(t *testing.T) (*rolloutContext, *mocks.StepPlugin) {
		stepPluginResolver := mocks.NewResolver(t)
		stepPluginMock := mocks.NewStepPlugin(t)
		stepPluginMock.On("Enabled").Return(true).Maybe()
		stepPluginResolver.On("Resolve", int32(0), mock.Anything, mock.Anything).Return(stepPluginMock, nil)

		r := newStepPluginRollout()
		r.Status.PromoteFull = true

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

		return roCtx, stepPluginMock
	}

	t.Run("Rollout is Terminated on full promotion", func(t *testing.T) {
		roCtx, stepPluginMock := setup(t)
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
			stepPluginMock.On("Enabled").Return(true).Maybe()
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
}

//Controller:
// Add test for plugin config
// Add InitPlugin call test

//Reconcile:
// Add disable feature

func Test_rolloutContext_isStepPluginCompleted(t *testing.T) {
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
