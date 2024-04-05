package rollout

import (
	"encoding/json"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/steps/plugin/mocks"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

func newStepPluginRollout() *v1alpha1.Rollout {
	steps := []v1alpha1.CanaryStep{
		{
			Plugin: &v1alpha1.PluginStep{
				Name: "test-plugin",
			},
		},
	}
	return newCanaryRollout("foo", 3, nil, steps, pointer.Int32(0), intstr.FromInt(1), intstr.FromInt(0))
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
				stepPluginResolver: stepPluginResolver,
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

		existingStatus := runStatus.DeepCopy()
		roCtx.rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{}

		err := roCtx.reconcileCanaryPluginStep()

		require.NoError(t, err)
		require.Len(t, roCtx.newStatus.Canary.StepPluginStatuses, 1)
		assert.EqualExportedValues(t, roCtx.newStatus.Canary.StepPluginStatuses[0], *runStatus)
	})
}

//Controller:
// Add test for plugin config
// Add InitPlugin call test

//Reconcile:
// Add disable feature

//RPC:
// Test config
// Test state on first tun
// Test state with existing

// if running || failed || success, state saved
// if running, requeue after
// if failed, abort
// if success, next step, else current step

//if run error, save message+phase, but not state

//When promote-full, terminate called on current step
//When abort, abort all steps? validate status? validate order

//error during run?
//error during abort?
//error during terminate? safe to ignore?

// cannot modify ptr reference of rollout input object

//input rollout contains all plugin status state

// Helper: write helper functions?

func TestReconcileCanaryPluginStep(t *testing.T) {
	t.Run("The plugin is skippped if it is disabled globally", func(t *testing.T) {
		t.Fail()
	})

	t.Run("The plugin receives the Rollout user configuration", func(t *testing.T) {

		t.Fail()
	})

	t.Run("The plugin receives an empty state the first time it is executed", func(t *testing.T) {

		t.Fail()
	})

	t.Run("The plugin receives the existing state if it is present", func(t *testing.T) {

		t.Fail()
	})
}

// func Test_rolloutContext_reconcileCanaryPluginStep(t *testing.T) {
// 	type fields struct {
// 		reconcilerBase  reconcilerBase
// 		log             *log.Entry
// 		rollout         *v1alpha1.Rollout
// 		newRollout      *v1alpha1.Rollout
// 		newRS           *appsv1.ReplicaSet
// 		stableRS        *appsv1.ReplicaSet
// 		allRSs          []*appsv1.ReplicaSet
// 		olderRSs        []*appsv1.ReplicaSet
// 		otherRSs        []*appsv1.ReplicaSet
// 		currentArs      analysisutil.CurrentAnalysisRuns
// 		otherArs        []*v1alpha1.AnalysisRun
// 		currentEx       *v1alpha1.Experiment
// 		otherExs        []*v1alpha1.Experiment
// 		newStatus       v1alpha1.RolloutStatus
// 		pauseContext    *pauseContext
// 		targetsVerified *bool
// 	}
// 	tests := []struct {
// 		name    string
// 		fields  fields
// 		wantErr bool
// 	}{
// 		// TODO: Add test cases.
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			c := &rolloutContext{
// 				reconcilerBase:  tt.fields.reconcilerBase,
// 				log:             tt.fields.log,
// 				rollout:         tt.fields.rollout,
// 				newRollout:      tt.fields.newRollout,
// 				newRS:           tt.fields.newRS,
// 				stableRS:        tt.fields.stableRS,
// 				allRSs:          tt.fields.allRSs,
// 				olderRSs:        tt.fields.olderRSs,
// 				otherRSs:        tt.fields.otherRSs,
// 				currentArs:      tt.fields.currentArs,
// 				otherArs:        tt.fields.otherArs,
// 				currentEx:       tt.fields.currentEx,
// 				otherExs:        tt.fields.otherExs,
// 				newStatus:       tt.fields.newStatus,
// 				pauseContext:    tt.fields.pauseContext,
// 				targetsVerified: tt.fields.targetsVerified,
// 			}
// 			if err := c.reconcileCanaryPluginStep(); (err != nil) != tt.wantErr {
// 				t.Errorf("rolloutContext.reconcileCanaryPluginStep() error = %v, wantErr %v", err, tt.wantErr)
// 			}
// 		})
// 	}
// }
