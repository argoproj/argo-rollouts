# RolloutPlugin CR - States and Transitions

## Overview
The RolloutPlugin CR manages progressive delivery rollouts for workloads (StatefulSets, Deployments, etc.). This document describes all possible states, transitions, and actions.

---

## State Diagram

```
                    ┌─────────────┐
                    │   Initial   │
                    │  (No Phase) │
                    └──────┬──────┘
                           │
                           │ New Revision Detected
                           ▼
                    ┌─────────────┐
              ┌─────│ Progressing │◄─────┐
              │     └──────┬──────┘      │
              │            │             │
              │            │ Step Complete    │
              │            │             │ Resume
              │            ▼             │
   Manual     │     ┌─────────────┐     │
   Pause      ├────►│   Paused    │─────┘
   (spec.paused)    └──────┬──────┘
              │            │
              │            │ Abort
    Abort     │            │ (status.abort=true)
    ├─────────┘            │
    │                      ▼
    │               ┌─────────────┐
    └──────────────►│  Degraded   │
                    │  (Aborted)  │
                    └──────┬──────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
              │ Restart    │ New        │ Timeout/
              │ (status.   │ Revision   │ Error
              │  restart=  │            │
              │  true)     │            │
              │            │            │
              ▼            ▼            ▼
       ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
       │ Progressing │ │ Progressing │ │   Failed    │
       └─────────────┘ └─────────────┘ └─────────────┘
              │
              │ All Steps Complete
              │ + Healthy
              ▼
       ┌─────────────┐
       │   Healthy   │
       │(Successful) │
       └─────────────┘
```

---

## States (Phases)

### 1. **Progressing**
**Description**: Rollout is actively executing steps ( pauses, analysis, etc.)

**Characteristics**:
- `status.rolloutInProgress = true`
- `status.currentStepIndex` tracks progress through canary steps
- Active traffic shifting and pod updates happening

**Exit Conditions**:
- Pause step encountered → **Paused**
- All steps complete + pods healthy → **Healthy**
- Manual abort → **Degraded**
- Timeout or error → **Failed**
- Manual pause (spec.paused=true) → **Paused**

---

### 2. **Paused**
**Description**: Rollout is temporarily halted, waiting for manual or automatic resume

**Types of Pauses**:
- **Step Pause**: Automatic pause defined in canary strategy steps
- **Manual Pause**: User sets `spec.paused=true`

**Characteristics**:
- `status.paused = true`
- `status.pauseStartTime` is set
- Pause time doesn't count toward `progressDeadlineSeconds`

**Exit Conditions**:
- Resume action (spec.paused=false or promote) → **Progressing**
- Abort → **Degraded**
- Timeout → **Failed**

**Actions Available**:
- `PromoteFull`: Skip all remaining steps → **Progressing** (final step)
- `Resume`: Continue to next step → **Progressing**
- `Abort`: Cancel rollout → **Degraded**

---

### 3. **Degraded (Aborted)**
**Description**: Rollout has been aborted, pods rolled back to previous revision

**Characteristics**:
- `status.aborted = true`
- `status.abortedRevision` = the revision that was aborted
- `status.rolloutInProgress = false`
- Pods with new revision are deleted (gracefully, one at a time)
- StatefulSet partition reset to previous state

**Exit Conditions**:
- **Restart same revision**: User sets `status.restart=true` → **Progressing** (restarts aborted revision)
- **New revision deployed**: Different image/spec → **Progressing** (starts new rollout, clears abort state)
- **No automatic restart**: Stays aborted if same revision detected again

**Actions Available**:
- `Restart` (status.restart=true): Retry the aborted revision
- Deploy new revision: Automatically clears abort and starts new rollout

---

### 4. **Healthy (Successful)**
**Description**: Rollout completed successfully, all pods updated and healthy

**Characteristics**:
- `status.rolloutInProgress = false`
- `status.currentRevision == status.updatedRevision`
- All replicas available and ready
- MinReadySeconds satisfied

**Exit Conditions**:
- New revision deployed → **Progressing**

---

### 5. **Failed**
**Description**: Rollout encountered an unrecoverable error

**Common Causes**:
- Timeout (progressDeadlineSeconds exceeded)
- Plugin errors (plugin not found, plugin execution failed)
- Analysis run failures
- Invalid configuration

**Characteristics**:
- `status.message` contains error details
- Appropriate condition set (InvalidSpec, ProgressDeadlineExceeded, etc.)

**Recovery**:
- Fix underlying issue
- Deploy new revision to retry

---

## Actions and Triggers

### User-Initiated Actions (via status fields)

| Action | Status Field | Valid From State | Description |
|--------|-------------|------------------|-------------|
| **Abort** | `status.abort=true` | Progressing, Paused | Cancel rollout, rollback to previous revision |
| **Restart** | `status.restart=true` | Degraded (Aborted) | Retry the same aborted revision |
| **Resume** | `spec.paused=false` | Paused | Continue rollout from current step |
| **Pause** | `spec.paused=true` | Progressing | Temporarily halt rollout |
| **PromoteFull** | Via ArgoCD/API | Paused, Progressing | Skip all remaining steps, complete rollout immediately |

### Automatic Transitions

| Trigger | From State | To State | Condition |
|---------|-----------|----------|-----------|
| New Revision | Healthy | Progressing | `currentRevision != updatedRevision` |
| New Revision | Degraded | Progressing | Different revision than aborted |
| Pause Step | Progressing | Paused | Step with `pause: {}` reached |
| Step Complete | Paused | Progressing | Resume triggered or pause duration elapsed |
| All Steps Done | Progressing | Healthy | All steps complete + pods healthy |
| Timeout | Progressing/Paused | Failed | `progressDeadlineSeconds` exceeded |
| Analysis Fail | Progressing | Failed/Degraded | Analysis run returns failure |

---

## State Fields in Status

### Key Status Fields

```yaml
status:
  # Phase tracking
  phase: "Progressing"                    # Current state
  message: "Rollout is progressing"       # Human-readable details
  
  # Rollout progress
  rolloutInProgress: true                 # Is rollout active?
  currentStepIndex: 2                     # Current canary step (0-based)
  
  # Revision tracking
  currentRevision: "abc123"               # Current stable revision
  updatedRevision: "def456"               # Target revision being rolled out
  
  # Abort state
  aborted: false                          # Is rollout aborted?
  abortedRevision: ""                     # Which revision was aborted
  
  # Pause state
  paused: false                           # Is rollout paused?
  pauseStartTime: null                    # When pause started
  
  # Trigger fields (one-shot)
  abort: false                            # Set to true to abort
  restart: false                          # Set to true to restart aborted
  
  # Resource status
  availableReplicas: 5
  readyReplicas: 5
  updatedReplicas: 3
```

---

## State Transition Rules

### 1. Starting a Rollout
**Condition**: `currentRevision != updatedRevision AND !rolloutInProgress`

**Logic**:
```
IF status.aborted == true:
  IF updatedRevision == abortedRevision:
    → Stay in Degraded (don't auto-restart same aborted revision)
  ELSE:
    → Clear abort state, start new rollout → Progressing
ELSE:
  → Start new rollout → Progressing
```

### 2. Abort Behavior
**Immediate Effects**:
- Set `aborted=true`, `abortedRevision=updatedRevision`
- Call `plugin.Abort()` to rollback (delete new pods gracefully)
- Set `rolloutInProgress=false`
- Set `phase=Degraded`

**Subsequent Reconciliations**:
- **Same revision**: Stays aborted, blocks automatic restart
- **New revision**: Clears abort state, starts new rollout
- **Restart action**: User sets `status.restart=true` to retry

### 3. Pause Behavior
**Types**:
- **Step Pause**: Automatic from canary strategy
  - `pauseStartTime` set
  - Waits for resume or duration expiry
  
- **Manual Pause**: User sets `spec.paused=true`
  - Takes precedence over step pauses
  - Requires user to unset `spec.paused=false` to resume

### 4. Resume Behavior
**From Step Pause**:
- Promote action → Advances to next step
- PromoteFull → Skips to final step

**From Manual Pause**:
- User sets `spec.paused=false`
- Rollout continues from where it paused

---

## Examples

### Example 1: Normal Rollout Flow
```
1. Deploy new image
   → Progressing (step 0: setWeight 20%)
   
2. Automatic pause step
   → Paused (waiting for resume)
   
3. User promotes
   → Progressing (step 1: setWeight 40%)
   
4. All steps complete
   → Healthy
```

### Example 2: Abort and Restart
```
1. Rollout in progress
   → Progressing (step 2)
   
2. User sets status.abort=true
   → Degraded (pods rolled back)
   
3. User sets status.restart=true
   → Progressing (step 0, retry same revision)
   
4. Completes successfully
   → Healthy
```

### Example 3: Abort and New Revision
```
1. Rollout in progress (image v2)
   → Progressing
   
2. User aborts
   → Degraded (abortedRevision=v2)
   
3. User deploys new image v3
   → Progressing (abort cleared, new rollout starts)
```

### Example 4: Manual Pause and Resume
```
1. Rollout in progress
   → Progressing
   
2. User sets spec.paused=true
   → Paused
   
3. User sets spec.paused=false
   → Progressing (continues from where paused)
```

---

## Best Practices

1. **Aborting**: Always check `status.phase=Degraded` after abort to confirm
2. **Restarting**: Use `status.restart=true` only when `status.aborted=true`
3. **Pausing**: Prefer step-level pauses in strategy over manual pauses
4. **Monitoring**: Watch `status.message` for detailed state information
5. **New Revisions**: Deploying a new revision automatically clears abort state

---

## Conditions

The RolloutPlugin also maintains Kubernetes conditions that provide additional state information:

| Condition | Type | Status | Reason | Description |
|-----------|------|--------|--------|-------------|
| Progressing | True | ProgressingReason | Rollout is progressing |
| Progressing | False | AbortedReason | Rollout was aborted |
| Progressing | False | PausedReason | Rollout is paused |
| Progressing | False | CompletedReason | Rollout completed |
| Progressing | False | TimedOutReason | Rollout exceeded deadline |
| InvalidSpec | True | InvalidSpecReason | Configuration error |
| Available | True | AvailableReason | Pods are available |
| Completed | True | CompletedReason | Rollout finished |

---

## Summary

The RolloutPlugin CR provides a robust state machine for progressive delivery with clear states, transitions, and user actions. Key principles:

- **Progressing**: Active rollout execution
- **Paused**: Temporary halt (step or manual)
- **Degraded**: Aborted state, requires explicit action
- **Healthy**: Successful completion
- **Failed**: Error state requiring intervention

Users control rollouts through status fields (`abort`, `restart`) and spec fields (`paused`), while the controller manages automatic transitions based on step completion, timeouts, and health checks.
