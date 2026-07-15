---
title: Stable Scale-Down Policy for dynamicStableScale
authors:
  - '@parthsharma'
creation-date: 2026-07-06
---

# Stable Scale-Down Policy for `dynamicStableScale`

## Summary

Add an opt-in `stableScaleDownPolicy` field to the canary strategy that delays scaling the **stable ReplicaSet down** after traffic weight shifts away from stable, giving the service-mesh data plane time to converge before pods are removed.

This fixes brief `503` / `no healthy upstream` errors observed at the end of Istio subset-routed canary rollouts when `dynamicStableScale: true`.

## Motivation

### Problem

With `dynamicStableScale: true`, the controller scales the stable ReplicaSet in proportion to stable traffic weight. At `setWeight: 100`, stable weight becomes 0% and the stable RS is scaled to zero on the next reconcile.

The controller updates the **control plane** (VirtualService weights) immediately, but Istio/Envoy propagation is asynchronous. For a window of seconds, proxies may still route a slice of traffic to the stable subset while stable pods are already gone → `503 no healthy upstream`.

This is most visible when:

1. `dynamicStableScale: true`
2. Traffic routing via Istio DestinationRule subsets
3. A `setWeight: 100` step (often followed by a pause for analysis)

### Evidence / prior reports

- [#3681](https://github.com/argoproj/argo-rollouts/issues/3681) — Istio + dynamicStableScale, 503 at 100% weight
- [#3586](https://github.com/argoproj/argo-rollouts/issues/3586) — same pattern, duplicate of [#3372](https://github.com/argoproj/argo-rollouts/issues/3372)
- [#4347](https://github.com/argoproj/argo-rollouts/issues/4347) — stable scaled before data plane shifts; proposes VerifyWeight or scale-down delay
- [#4595](https://github.com/argoproj/argo-rollouts/issues/4595) / [#3926](https://github.com/argoproj/argo-rollouts/issues/3926) — ALB + dynamicStableScale, same class of race
- [#4710](https://github.com/argoproj/argo-rollouts/issues/4710) — promotion-time 503 with Gateway API plugin

### Why existing fixes do not cover this

| Change | Why it does not fix end-of-rollout stable scale-down |
|--------|------------------------------------------------------|
| [#3878](https://github.com/argoproj/argo-rollouts/pull/3878) guardrail | Prevents overloading stable on **interrupted** rollouts, not stable→0 at 100% |
| [#4564](https://github.com/argoproj/argo-rollouts/pull/4564) / [#4639](https://github.com/argoproj/argo-rollouts/pull/4639) | Fixes SetWeight/UpdateHash ordering on **new canary supersede** |
| [#4645](https://github.com/argoproj/argo-rollouts/pull/4645) | Delays **intermediate RS** teardown; uses `scaleDownDelaySeconds`, which is **mutually exclusive** with `dynamicStableScale` |
| [#4753](https://github.com/argoproj/argo-rollouts/pull/4753) `weightUpdateDelaySeconds` | Gates SetWeight after **DR hash change** (start/promotion), not stable replica scale-down |
| ALB `VerifyWeight` ([#957](https://github.com/argoproj/argo-rollouts/pull/957), [#3627](https://github.com/argoproj/argo-rollouts/pull/3627)) | ALB-only; Istio `VerifyWeight` is a no-op; neither gates `reconcileCanaryStableReplicaSet` |

### Goals

- Eliminate transient 503s at high canary weight when `dynamicStableScale` is enabled
- Opt-in, backward-compatible API (nil = current behavior)
- Extensible struct for future options (e.g. `requireWeightVerified`)
- Provider-agnostic (works for any traffic router using subset weights)

### Non-Goals

- Replacing `dynamicStableScale` or `scaleDownDelaySeconds`
- Implementing full Istio data-plane verification in v1 (complements [#4752](https://github.com/argoproj/argo-rollouts/issues/4752))
- Changing promotion / `UpdateHash` behavior

## Proposal

### API

`dynamicStableScale` is a plain `bool` (protobuf field 14). Nesting a policy inside it would be a breaking change. Add a **sibling** struct on `CanaryStrategy`:

```yaml
spec:
  strategy:
    canary:
      dynamicStableScale: true
      stableScaleDownPolicy:
        delaySeconds: 60
      trafficRouting:
        istio:
          virtualService:
            name: my-vsvc
          destinationRule:
            name: my-destrule
            canarySubsetName: canary
            stableSubsetName: stable
      steps:
        - setWeight: 25
        - pause: { duration: 30s }
        - setWeight: 100
        - pause: {}
```

```go
type StableScaleDownPolicy struct {
    // DelaySeconds holds the stable ReplicaSet at its current replica count
    // after a scale-down is requested, before allowing further scale-down.
    // +optional
    DelaySeconds *int32 `json:"delaySeconds,omitempty"`
}
```

| Field | Default | Semantics |
|-------|---------|-----------|
| `stableScaleDownPolicy` | `nil` | Current behavior (immediate stable scale-down) |
| `delaySeconds` | required when policy set | Seconds to wait before scaling stable below computed desired count |

**Validation:** `stableScaleDownPolicy` requires `dynamicStableScale: true` and `trafficRouting` configured.

### Controller behavior

When `reconcileCanaryStableReplicaSet` computes `desiredStableRSReplicaCount < currentStableReplicas`:

1. If policy is nil or `delaySeconds` is 0 → scale immediately (current behavior).
2. Otherwise, stamp a **stable scale-down deadline** annotation on the stable RS (separate from post-promotion `scale-down-deadline`).
3. Hold stable at **current** replica count; `enqueueRolloutAfter(remaining)`.
4. After deadline elapses → allow scale-down to computed desired count.

**Only gates scale-DOWN.** Scale-up (abort, rollback, weight decrease) is unaffected.

### Sequence

```
T+0   SetWeight(100) → status.canary.weights stable=0%
T+0   reconcileCanaryStableReplicaSet: desired=0, current=1 → stamp deadline, hold at 1
T+N   deadline elapsed → scale stable to 0
```

During `[T+0, T+N)`, stable pods remain available for any traffic still routed to the stable subset.

### Backward compatibility

- Field is optional; existing Rollouts unchanged.
- No interaction with `scaleDownDelaySeconds` (still mutually exclusive with `dynamicStableScale`).

## Test plan

- Unit: policy nil → immediate scale-down; policy set → hold until deadline; scale-up bypasses gate; validation rejects policy without dynamicStableScale.
- Manual/E2E: Istio subset rollout, fortio load, compare 503 count with/without policy.

## Alternatives considered

1. **Static stable pod floor (e.g. always keep 1 stable pod)** — keeps pods at 0% stable weight for the remainder of the rollout (or until manually changed), not just during the mesh propagation window. That is an open-ended capacity cost with no timed release. `delaySeconds` bounds extra capacity to a configurable window, then scale-down proceeds.
2. **`minPodsPerReplicaSet`** — existing field, but only applies when computed replica count is **non-zero** (`CheckMinPodsPerReplicaSet` returns 0 unchanged). At `setWeight: 100` stable count is 0, so it does **not** prevent the 503 window.
3. **Istio VerifyWeight reading VS spec** — control-plane only; returns verified before data plane converges.
4. **Reuse `scaleDownDelaySeconds`** — validation forbids with `dynamicStableScale`; different lifecycle (post-promotion old RS).
