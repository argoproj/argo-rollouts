---
title: Unit tests that verify end state
authors:
  - '@kostis-codefresh'
creation-date: 2026-09-30
---

# Unit tests that verify end state

The unit tests of the Argo Rollouts controllers verify the exact sequence of Kubernetes API calls instead of the resulting state of the cluster.

This document proposes a gradual migration to unit tests that verify the end state of the Kubernetes objects after a reconciliation.

## Summary

Most controller unit tests today describe *how* the controller talks to the API server ("first create a ReplicaSet, then update the Rollout, then patch its status, then update the ReplicaSet"),
and not *what* the cluster should look like after the reconciliation ("the new ReplicaSet has 1 replica and the Rollout has the Progressing condition").

As a result, any internal refactoring of the controller breaks a large number of tests, even when the behavior of the controller has not changed at all.

## Motivation

The main test fixture of the Rollout controller (see [rollout/controller_test.go](https://github.com/argoproj/argo-rollouts/blob/master/rollout/controller_test.go)) keeps
a list of expected API actions. After every sync the actual actions recorded by the fake clientset are compared one by one against this list. Tests then fetch the
objects that were sent to the API server by their position in the list.

This pattern is used everywhere:

- There are around 500 `f.expect*()` calls in the unit tests, most of them in the `rollout` package.
- There are around 240 lookups that fetch a created/patched/updated object by its index in the action list.
- The same action filtering helper (`filterInformerActions`) has been copied in the `rollout`, `experiments` and `analysis` packages.

This approach has several drawbacks:

- **Refactorings break tests.** Merging an update into a patch, reordering two independent calls or adding an extra GET fails many tests even though the result is identical.
- **Tests are recorded, not reasoned about.** When a test breaks, contributors often "fix" it by re-recording the new call order instead of checking if the behavior is still correct.
- **Tests can pass with the wrong result.** A test that matches the expected call sequence can still pass if the final object is wrong and the test does not check that particular field.
- **Tests are hard to read.** The intent of the test is hidden behind a list of API calls and index bookkeeping.
- **Cleanups are discouraged.** The cost of updating dozens of tests stops contributors from simplifying the controller code.

This is one of the items tracked in [#107 Test refactoring, cleanup, and standardization](https://github.com/argoproj/argo-rollouts/issues/107) and it is also a natural first step
towards [#252 Utilize envtest.Environment instead of fake clients](https://github.com/argoproj/argo-rollouts/issues/252).

## Goals

The goals of this proposal are:

- Unit tests assert on the resulting state of Kubernetes objects (Rollouts, ReplicaSets, Services, AnalysisRuns, Experiments etc.) after a reconciliation
- Internal refactorings that keep the same behavior do not require test changes
- Tests clearly express the expected behavior of the controller
- The migration can happen incrementally, one test file at a time
- The new assertion style can be reused later with a real API server (envtest)

## Non-Goals

- Migrating the unit tests to envtest. This is covered by [#252](https://github.com/argoproj/argo-rollouts/issues/252).
- Changing any behavior of the controllers.
- Rewriting all tests in a single pull request.
- Removing *all* assertions on API calls. Some tests are specifically about the calls themselves (see below).

## Proposal

The fake clientsets used in unit tests already keep an in-memory object tracker that stores every object the controller creates, updates or patches.
Some tests (for example in the `experiments` package) already use this tracker directly.

Instead of recording the expected API calls, tests will run the reconciliation and then read the objects back from the tracker and check their state.

### Example

Here is a simplified version of a canary test as it is written today:

```go
createdRSIndex := f.expectCreateReplicaSetAction(rs2)
updatedRolloutIndex := f.expectUpdateRolloutAction(r2)
updatedConditionsIndex := f.expectUpdateRolloutStatusAction(r2)
f.expectGetRolloutAction(r2)
f.expectPatchRolloutAction(r2)
updatedRSIndex := f.expectUpdateReplicaSetAction(rs2)
f.runWithSyncs(getKey(r2, t), 2)

updatedRS := f.getUpdatedReplicaSet(updatedRSIndex)
assert.Equal(t, int32(1), *updatedRS.Spec.Replicas)

updatedRollout := f.getUpdatedRollout(updatedConditionsIndex)
progressing := conditions.GetRolloutCondition(updatedRollout.Status, v1alpha1.RolloutProgressing)
assert.Equal(t, conditions.NewReplicaSetReason, progressing.Reason)
```

And here is the same test written with end-state assertions:

```go
f.runWithSyncs(getKey(r2, t), 2)

newRS := f.getReplicaSet(rs2.Name)
assert.Equal(t, int32(1), *newRS.Spec.Replicas)
assert.Equal(t, "2", newRS.Annotations[annotations.RevisionAnnotation])

rollout := f.getRollout(r2.Name)
progressing := conditions.GetRolloutCondition(rollout.Status, v1alpha1.RolloutProgressing)
assert.Equal(t, conditions.NewReplicaSetReason, progressing.Reason)
```

The second version does not care whether the ReplicaSet was scaled with an update or a patch, or in which order the Rollout and the ReplicaSet were written.

### Test fixture changes

The test fixtures will get a small set of helpers:

- Getters that read the current version of an object from the fake clientset (e.g. `getRollout`, `getReplicaSet`, `getService`, `getAnalysisRun`)
- A switch that disables the strict call-order verification for tests that have been migrated
- A few narrow helpers for the cases where the API calls *are* the behavior under test, for example "no write happened during this sync" or "the ReplicaSet was created exactly once"

The existing helpers stay in place until all tests in a package are migrated.

## Migration strategy

1. Finish the smaller cleanups of [#107](https://github.com/argoproj/argo-rollouts/issues/107) first (a single set of ReplicaSet builders and no hard-coded pod hashes). This makes the migrated tests shorter and easier to review.
2. Add the new helpers next to the existing ones.
3. Migrate the tests file by file, starting with the `rollout` package (`canary_test.go`, `bluegreen_test.go`, `sync_test.go`, etc.), and then `experiments` and `analysis`.
4. All new unit tests use end-state assertions.
5. Once a package is fully migrated, remove the action-list machinery from its fixture.

Each step is a separate pull request so that reviews stay small.

## Security Considerations

There is no impact for the security of the project. In fact, this proposal will improve the experience for new contributors when they submit new fixes (security related or not).

## Risks and Mitigations

There is no risk in the main project as all changes will happen in test files. No actual change on the controller itself.

For the testing suite:

- **Some tests really DO need intermediate state checks.** These tests can either stay as is or have intermediate state checks and not just at the end.
- **The fake object tracker is not a real API server.** It does not apply defaulting, validation or strategic merge semantics exactly like Kubernetes does.
  Tests that depend on the patch content itself will keep checking it. A real API server in tests is the goal of [#252](https://github.com/argoproj/argo-rollouts/issues/252).
- **Loss of coverage for unnecessary writes.** Today's tests implicitly catch extra API calls. Tests where this matters (e.g. "a steady state rollout does not write anything") will keep an explicit assertion on the number of writes.
- **Large amount of test changes.** The migration touches a large part of the test code. Doing it incrementally, file by file, keeps each change reviewable and the test suite green at all times.
- **Overlap with other work.** The retry changes for the Experiment and AnalysisRun controllers ([#227](https://github.com/argoproj/argo-rollouts/issues/227)) touch the same test fixtures, so the two efforts should be sequenced and not run in parallel on the same files.

## Drawbacks

- The migration is a considerable effort (several weeks for the `rollout` package alone).
- For a period of time both test styles will exist in the codebase.

## Alternatives

- **Move directly to envtest** ([#252](https://github.com/argoproj/argo-rollouts/issues/252)). This gives the most realistic tests but it is a much bigger change, it makes tests slower, and it still requires the same end-state assertions. This proposal is a cheaper first step in that direction.
- **Keep call-based assertions but ignore the order.** This is cheaper, but tests would still be coupled to the exact API calls (update vs patch, number of calls), so most refactorings would still break them.
