package plugin

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/plugin/types"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

func new(logCtx *log.Entry, seed int64) *rpcPlugin {
	return &rpcPlugin{
		LogCtx: logCtx,
		Seed:   seed,
		lock:   &sync.RWMutex{},
	}
}

func newRollout() *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
			},
		},
		Status: v1alpha1.RolloutStatus{
			Canary: v1alpha1.CanaryStatus{},
		},
	}
}

func getStateFromResult(t *testing.T, result types.RpcStepResult) *State {
	require.NotNil(t, result)

	var state *State
	if err := json.Unmarshal(result.Status, &state); err != nil {
		require.NotNil(t, err)
	}
	return state
}

func toRaw(obj any) json.RawMessage {
	raw, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}
	return raw
}

func Test_rpcPlugin_Run(t *testing.T) {
	t.Run("Return value with nil context", func(t *testing.T) {
		p := New(log.WithFields(log.Fields{}), 0)
		err := p.InitPlugin()
		if err.HasError() {
			t.Fatalf("Got error during init: %s", err.Error())
		}

		rollout := newRollout()

		got, err := p.Run(rollout, nil)

		require.False(t, err.HasError(), "Error: %s", err.Error())
		assert.Equal(t, types.PhaseSuccessful, got.Phase)
		state := getStateFromResult(t, got)
		assert.Equal(t, "74", state.Value)

	})
	t.Run("Return value with empty context", func(t *testing.T) {
		p := New(log.WithFields(log.Fields{}), 0)
		err := p.InitPlugin()
		if err.HasError() {
			t.Fatalf("Got error during init: %s", err.Error())
		}

		rollout := newRollout()
		context := &types.RpcStepContext{
			PluginName: "test",
			Config:     nil,
			Status:     nil,
		}

		got, err := p.Run(rollout, context)

		require.False(t, err.HasError(), "Error: %s", err.Error())
		assert.Equal(t, types.PhaseSuccessful, got.Phase)
		state := getStateFromResult(t, got)
		assert.Equal(t, "74", state.Value)

	})

	t.Run("Return value with existing state", func(t *testing.T) {
		p := New(log.WithFields(log.Fields{}), 0)
		err := p.InitPlugin()
		if err.HasError() {
			t.Fatalf("Got error during init: %s", err.Error())
		}

		rollout := newRollout()
		context := &types.RpcStepContext{
			PluginName: "test",
			Config:     nil,
			Status: toRaw(&State{
				Id:    "fake-id",
				Value: "123",
			}),
		}

		got, err := p.Run(rollout, context)

		require.False(t, err.HasError(), "Error: %s", err.Error())
		assert.Equal(t, types.PhaseSuccessful, got.Phase)
		state := getStateFromResult(t, got)
		assert.Equal(t, "123", state.Value)
	})

	t.Run("Running status with async config", func(t *testing.T) {
		p := New(log.WithFields(log.Fields{}), 0)
		err := p.InitPlugin()
		if err.HasError() {
			t.Fatalf("Got error during init: %s", err.Error())
		}

		rollout := newRollout()
		context := &types.RpcStepContext{
			PluginName: "test",
			Config: toRaw(&Config{
				Async: true,
			}),
			Status: nil,
		}

		got, err := p.Run(rollout, context)

		require.False(t, err.HasError(), "Error: %s", err.Error())
		assert.Equal(t, types.PhaseRunning, got.Phase)
		state := getStateFromResult(t, got)
		assert.Equal(t, "", state.Value)
	})

	t.Run("Return running with async config and existing state not completed", func(t *testing.T) {
		p := new(log.WithFields(log.Fields{}), 0)
		err := p.InitPlugin()
		if err.HasError() {
			t.Fatalf("Got error during init: %s", err.Error())
		}

		rollout := newRollout()
		context := &types.RpcStepContext{
			PluginName: "test",
			Config: toRaw(&Config{
				Async: true,
			}),
			Status: toRaw(&State{
				Id: "fake-id",
			}),
		}

		got, err := p.Run(rollout, context)

		require.False(t, err.HasError(), "Error: %s", err.Error())
		assert.Equal(t, types.PhaseRunning, got.Phase)
		state := getStateFromResult(t, got)
		assert.Equal(t, "", state.Value)
	})

	t.Run("Return value with async config and existing state", func(t *testing.T) {
		p := new(log.WithFields(log.Fields{}), 0)
		err := p.InitPlugin()
		if err.HasError() {
			t.Fatalf("Got error during init: %s", err.Error())
		}
		p.randomMap["fake-id"] = &Result{
			Value: 44,
			Id:    "fake-id",
		}

		rollout := newRollout()
		context := &types.RpcStepContext{
			PluginName: "test",
			Config: toRaw(&Config{
				Async: true,
			}),
			Status: toRaw(&State{
				Id: "fake-id",
			}),
		}

		got, err := p.Run(rollout, context)

		require.False(t, err.HasError(), "Error: %s", err.Error())
		assert.Equal(t, types.PhaseSuccessful, got.Phase)
		state := getStateFromResult(t, got)
		assert.Equal(t, "44", state.Value)
	})

	t.Run("Return value with aggregate and no previous step", func(t *testing.T) {
		p := new(log.WithFields(log.Fields{}), 0)
		err := p.InitPlugin()
		if err.HasError() {
			t.Fatalf("Got error during init: %s", err.Error())
		}
		p.randomMap["foo"] = &Result{
			Value: 6,
			Id:    "foo",
		}

		rollout := newRollout()
		rollout.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{
			{
				Plugin: &v1alpha1.PluginStep{
					Name: "test",
				},
			},
			{
				Plugin: &v1alpha1.PluginStep{
					Name: "test",
				},
			},
		}
		rollout.Status.CurrentStepIndex = ptr.To(int32(0))
		rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index: 0,
				Name:  "test",
				Status: toRaw(&State{
					Id:    "foo",
					Value: "99",
				}),
			},
		}
		context := &types.RpcStepContext{
			PluginName: "test",
			Config: toRaw(&Config{
				Aggregate: true,
			}),
			Status: nil,
		}

		got, err := p.Run(rollout, context)

		require.False(t, err.HasError(), "Error: %s", err.Error())
		assert.Equal(t, types.PhaseSuccessful, got.Phase)
		state := getStateFromResult(t, got)
		assert.Equal(t, "74", state.Value)
	})

	t.Run("Return value with aggregate", func(t *testing.T) {
		p := new(log.WithFields(log.Fields{}), 0)
		err := p.InitPlugin()
		if err.HasError() {
			t.Fatalf("Got error during init: %s", err.Error())
		}
		p.randomMap["foo"] = &Result{
			Value: 6,
			Id:    "foo",
		}

		rollout := newRollout()
		rollout.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{
			{
				Plugin: &v1alpha1.PluginStep{
					Name: "test",
				},
			},
			{
				Plugin: &v1alpha1.PluginStep{
					Name: "test",
				},
			},
		}
		rollout.Status.CurrentStepIndex = ptr.To(int32(1))
		rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index: 0,
				Name:  "test",
				Status: toRaw(&State{
					Id:    "foo",
					Value: "6",
				}),
			},
		}
		context := &types.RpcStepContext{
			PluginName: "test",
			Config: toRaw(&Config{
				Aggregate: true,
			}),
			Status: nil,
		}

		got, err := p.Run(rollout, context)

		require.False(t, err.HasError(), "Error: %s", err.Error())
		assert.Equal(t, types.PhaseSuccessful, got.Phase)
		state := getStateFromResult(t, got)
		assert.Equal(t, "80", state.Value)
	})

	t.Run("Return value with aggregate of an aggregate", func(t *testing.T) {
		p := new(log.WithFields(log.Fields{}), 0)
		err := p.InitPlugin()
		if err.HasError() {
			t.Fatalf("Got error during init: %s", err.Error())
		}
		p.randomMap["foo"] = &Result{
			Value: 80,
			Id:    "bar",
		}

		rollout := newRollout()
		rollout.Spec.Strategy.Canary.Steps = []v1alpha1.CanaryStep{
			{
				Plugin: &v1alpha1.PluginStep{
					Name: "test",
				},
			},
			{
				Plugin: &v1alpha1.PluginStep{
					Name: "test",
				},
			},
			{
				Plugin: &v1alpha1.PluginStep{
					Name: "test",
				},
			},
		}
		rollout.Status.CurrentStepIndex = ptr.To(int32(2))
		rollout.Status.Canary.StepPluginStatuses = []v1alpha1.StepPluginStatus{
			{
				Index: 0,
				Name:  "test",
				Status: toRaw(&State{
					Id:    "foo",
					Value: "6",
				}),
			},
			{
				Index: 1,
				Name:  "test",
				Status: toRaw(&State{
					Id:       "bar",
					SharedId: "foo",
					Value:    "80",
				}),
			},
		}
		context := &types.RpcStepContext{
			PluginName: "test",
			Config: toRaw(&Config{
				Aggregate: true,
			}),
			Status: nil,
		}

		got, err := p.Run(rollout, context)

		require.False(t, err.HasError(), "Error: %s", err.Error())
		assert.Equal(t, types.PhaseSuccessful, got.Phase)
		state := getStateFromResult(t, got)
		assert.Equal(t, "154", state.Value)
	})

}
