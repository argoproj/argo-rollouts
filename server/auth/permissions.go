package auth

import "github.com/argoproj/argo-rollouts/server/auth/rbac"

// Permission is the (resource, action) an RPC requires.
type Permission struct {
	Resource string
	Action   string
}

// methodPermissions maps RolloutService gRPC FullMethod names to the permission
// they require. Informational RPCs (Version, GetNamespace) are intentionally
// absent — they require no authorization.
var methodPermissions = map[string]Permission{
	"/rollout.RolloutService/GetRolloutInfo":    {rbac.ResourceRollouts, rbac.ActionGet},
	"/rollout.RolloutService/ListRolloutInfos":  {rbac.ResourceRollouts, rbac.ActionGet},
	"/rollout.RolloutService/WatchRolloutInfo":  {rbac.ResourceRollouts, rbac.ActionGet},
	"/rollout.RolloutService/WatchRolloutInfos": {rbac.ResourceRollouts, rbac.ActionGet},
	"/rollout.RolloutService/RestartRollout":    {rbac.ResourceRollouts, rbac.ActionRestart},
	"/rollout.RolloutService/PromoteRollout":    {rbac.ResourceRollouts, rbac.ActionPromote},
	"/rollout.RolloutService/AbortRollout":      {rbac.ResourceRollouts, rbac.ActionAbort},
	"/rollout.RolloutService/SetRolloutImage":   {rbac.ResourceRollouts, rbac.ActionSetImage},
	"/rollout.RolloutService/UndoRollout":       {rbac.ResourceRollouts, rbac.ActionUndo},
	"/rollout.RolloutService/RetryRollout":      {rbac.ResourceRollouts, rbac.ActionRetry},
}

// PermissionForMethod returns the permission required by a gRPC FullMethod, and
// whether the method requires authorization at all.
func PermissionForMethod(fullMethod string) (Permission, bool) {
	p, ok := methodPermissions[fullMethod]
	return p, ok
}
