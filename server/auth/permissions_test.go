package auth

import (
	"testing"

	"github.com/argoproj/argo-rollouts/server/auth/rbac"
	"github.com/stretchr/testify/assert"
)

func TestPermissionForMutatingMethods(t *testing.T) {
	cases := map[string]Permission{
		"/rollout.RolloutService/PromoteRollout":  {rbac.ResourceRollouts, rbac.ActionPromote},
		"/rollout.RolloutService/AbortRollout":    {rbac.ResourceRollouts, rbac.ActionAbort},
		"/rollout.RolloutService/RetryRollout":    {rbac.ResourceRollouts, rbac.ActionRetry},
		"/rollout.RolloutService/RestartRollout":  {rbac.ResourceRollouts, rbac.ActionRestart},
		"/rollout.RolloutService/SetRolloutImage": {rbac.ResourceRollouts, rbac.ActionSetImage},
		"/rollout.RolloutService/UndoRollout":     {rbac.ResourceRollouts, rbac.ActionUndo},
	}
	for method, want := range cases {
		got, ok := PermissionForMethod(method)
		assert.True(t, ok, method)
		assert.Equal(t, want, got, method)
	}
}

func TestPermissionForReadMethods(t *testing.T) {
	for _, m := range []string{
		"/rollout.RolloutService/GetRolloutInfo",
		"/rollout.RolloutService/ListRolloutInfos",
		"/rollout.RolloutService/WatchRolloutInfo",
		"/rollout.RolloutService/WatchRolloutInfos",
	} {
		got, ok := PermissionForMethod(m)
		assert.True(t, ok, m)
		assert.Equal(t, rbac.ResourceRollouts, got.Resource, m)
		assert.Equal(t, rbac.ActionGet, got.Action, m)
	}
}

func TestPermissionAbsentForInformationalMethods(t *testing.T) {
	for _, m := range []string{
		"/rollout.RolloutService/Version",
		"/rollout.RolloutService/GetNamespace",
		"/rollout.RolloutService/Unknown",
	} {
		_, ok := PermissionForMethod(m)
		assert.False(t, ok, m)
	}
}
