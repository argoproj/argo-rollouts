---
title: Configurable Initial Promotion for Blue-Green Rollouts
authors:
  - 'TBD'
sponsors:
  - 'TBD'
creation-date: 2026-05-29
---

# Configurable Initial Promotion for Blue-Green Rollouts

Allow users to opt out of the implicit "fast-track" behavior that automatically
points the active Service at the first ReplicaSet when a Blue-Green Rollout is
created for the first time, so that the initial revision can be brought up in
preview only and promoted through the normal pause / analysis flow.

## Summary

When a Blue-Green Rollout is created and its referenced active Service has no
`rollouts-pod-template-hash` selector yet, the controller currently treats the
first reconciliation as a "fast-tracked" update: the active Service selector is
set to the new ReplicaSet immediately, `prePromotionAnalysis` is skipped, and
the `autoPromotionEnabled: false` pause is bypassed. There is no way to
configure this behavior.

This proposal introduces an opt-in field on the Blue-Green strategy that lets
users treat the initial rollout like any subsequent one: the new ReplicaSet is
only exposed via the preview Service until pause / analysis / manual promotion
conditions are satisfied.

## Motivation

Today the only ways to keep the active Service from being claimed on initial
create are operational hacks:

- Pre-seed the active Service selector with a dummy
  `rollouts-pod-template-hash` value so the fast-track branch in
  `isBlueGreenFastTracked` does not fire.
- Create the Rollout with `spec.paused: true` or `replicas: 0` and re-link the
  Service afterwards.

Both approaches are fragile, undocumented, and surprising — particularly for
teams that adopt Argo Rollouts expecting `autoPromotionEnabled: false` and
`prePromotionAnalysis` to gate *every* promotion, including the first one. The
first deployment of a new service is often when the strongest safety net is
desired (no traffic baseline, no proven configuration), yet today it is the
only deployment that bypasses the gates.

### Goals

- Allow users to declaratively keep the active Service unbound (or pointing at
  an existing selector) on the first reconciliation of a Blue-Green Rollout.
- Run `prePromotionAnalysis` and honor `autoPromotionEnabled` /
  `autoPromotionSeconds` for the initial promotion when opted in.
- Preserve today's behavior as the default to avoid breaking existing users.

### Non-Goals

- Changing the default behavior of Blue-Green Rollouts.
- Altering canary strategy semantics.
- Introducing new traffic-routing primitives — the preview / active Service
  pair is reused as-is.

## Proposal

Add a new optional field to `BlueGreenStrategy`:

```go
// PromoteOnInitialRollout controls whether the very first ReplicaSet of a
// Blue-Green Rollout is promoted to the active Service automatically.
// Defaults to true to preserve existing behavior. When set to false, the
// initial rollout follows the same pause, prePromotionAnalysis and
// autoPromotionEnabled flow as subsequent updates: the new ReplicaSet is only
// exposed through the preview Service until the user (or autoPromotionSeconds)
// promotes it.
// +optional
PromoteOnInitialRollout *bool `json:"promoteOnInitialRollout,omitempty" protobuf:"varint,N,opt,name=promoteOnInitialRollout"`
```

Example usage:

```yaml
strategy:
  blueGreen:
    activeService: my-svc-active
    previewService: my-svc-preview
    autoPromotionEnabled: false
    promoteOnInitialRollout: false
    prePromotionAnalysis:
      templates:
        - templateName: smoke-tests
```

### Behavior

The change is localized to
[`isBlueGreenFastTracked`](../../rollout/bluegreen.go) and the surrounding
pause / active-service reconciliation:

- Today:

  ```go
  if _, ok := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]; !ok {
      return true // fast-track: promote on first create
  }
  ```

- Proposed:

  ```go
  if _, ok := activeSvc.Spec.Selector[v1alpha1.DefaultRolloutUniqueLabelKey]; !ok {
      if defaults.GetPromoteOnInitialRolloutOrDefault(c.rollout) {
          return true
      }
      // fall through: treat the initial rollout like any other update
  }
  ```

With `promoteOnInitialRollout: false`:

1. The controller scales up the new ReplicaSet and points the preview Service
   at it (existing behavior).
2. `prePromotionAnalysis`, if configured, runs against the preview pods.
3. The rollout enters the `BlueGreenPause` state when
   `autoPromotionEnabled: false`, or waits for `autoPromotionSeconds` to
   elapse.
4. On resume / auto-promote, `reconcileActiveService` switches the active
   Service to the new ReplicaSet — identical to the path taken for the second
   and later revisions.

### Edge cases

- **Active Service already has a hash selector pointing at a non-Rollout
  workload.** Out of scope; the controller already replaces the selector on
  the first reconciliation. `promoteOnInitialRollout: false` does not change
  this — the field only governs the "no hash selector yet" branch.
- **`PromoteFull` annotation or scale-down deadline set on the new RS.**
  These continue to fast-track regardless of `promoteOnInitialRollout` so
  manual override paths remain functional.
- **`Abort` status set before initial promotion.** The existing abort path
  (`newPodHash = StableRS`) is unchanged; with no `StableRS` yet the active
  Service simply remains unbound, matching today's pre-fast-track state.
- **Upgrades from earlier versions.** Default `true` preserves behavior for
  existing Rollouts and CRDs that do not set the field.

### Use cases

- Greenfield services that want their very first deploy gated by smoke tests
  before any traffic is routed.
- GitOps-managed environments where a single PR creates both the Service and
  the Rollout, and operators want a manual approval step before exposing the
  first version.
- Environments that rely on `autoPromotionSeconds` as a bake window and expect
  that window to apply uniformly to all revisions.

### Implementation Details / Constraints

- Add the field to `pkg/apis/rollouts/v1alpha1/types.go` and regenerate
  CRDs / deepcopy / OpenAPI via `make codegen`.
- Add a defaulting helper in `utils/defaults` (e.g.
  `GetPromoteOnInitialRolloutOrDefault`) returning `true` when unset.
- Update `isBlueGreenFastTracked` in `rollout/bluegreen.go` as shown above.
- Extend `bluegreen_test.go` with cases covering:
  - Default behavior unchanged when field is unset.
  - Initial reconciliation pauses and does not switch the active Service when
    field is `false`.
  - `prePromotionAnalysis` is executed before promotion.
  - Manual promotion / `autoPromotionSeconds` correctly switches the active
    Service after the initial pause.
- Update `docs/features/bluegreen.md` and the field reference in
  `docs/features/specification.md`.
- No metric or CRD-breaking change; field is additive and optional.

## Drawbacks

- Adds another knob to the Blue-Green strategy surface.
- Users who set `promoteOnInitialRollout: false` without also configuring
  `previewService` will see the first revision running but unreachable through
  any Rollout-managed Service until promotion. This should be called out in
  the docs.

## Alternatives

1. **Change the default.** Always treat the initial rollout like an update.
   Rejected because it silently changes behavior for every existing user and
   would break setups that rely on the active Service being claimed on first
   create.
2. **Document the `spec.paused: true` workaround only.** Rejected because it
   requires a follow-up edit to the Rollout (or external automation) to
   un-pause, and it disables *all* reconciliation rather than just the active
   Service switch.
3. **Special-case `prePromotionAnalysis` to always run on initial rollout.**
   Partial fix — it would gate promotion but still imply that the active
   Service is switched immediately on success, without honoring
   `autoPromotionEnabled: false`. The proposed field generalizes the behavior
   to all gating mechanisms.
