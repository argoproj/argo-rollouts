---
title: Parallel E2E tests
authors:
  - '@kostis-codefresh'
creation-date: 2026-09-30
---

# Parallel E2E tests

The E2E test suite of Argo Rollouts runs fully sequentially, and every pull request waits more than half an hour for results.

This document proposes a staged plan for running E2E tests in parallel, as requested in [#4794](https://github.com/argoproj/argo-rollouts/issues/4794).

## Summary

Every PR to Argo Rollouts currently waits about 33 minutes for E2E results. Almost all of this time is spent on the tests themselves. Setting up the environment (k3s, CRDs, controller) takes only about a minute.

We need to understand where the tests spend their time (i.e. waiting for stuff to happen) and either cut it down or reorganize the tests themselves.

## Motivation

- PR feedback is slow. Contributors and maintainers wait ~33 minutes for every push. This forces people to change their mental context to something else after a PR
- The local developer loop is slow. A full local E2E run takes ~29 minutes. Most people seems submit PRs without running the e2e tests locally at all
- The CI matrix runs 4 jobs (one per Kubernetes version), and **each job runs the entire suite serially**. There is no sharding by suite.
- Each flaky test delays the merge of a PR even further, because it must be re-run inside an already long job.

### Goals

The goals of this proposal are:

- Reduce PR E2E feedback time to a more manageable result (maybe 10-15 minutes)
- Reduce the time of a full local E2E run
- Deliver the improvement in small, incremental steps that are useful on their own
- Avoid introducing new flaky tests
- Keep the current local developer workflow working with its current defaults

### Non-Goals

- Changing the unit tests. They already run in parallel and are not the bottleneck.
- Running individual test methods in parallel inside a single suite.
- Rewriting the E2E test framework.
- Running multiple controller instances.

## Why tests are sequential today

The E2E framework (`test/fixtures/`) is built on top of testify suites. There are 15 suites, and the three largest (Functional, Analysis, Canary) take about 6 minutes each. The following issues prevent parallel execution:

1. **A single shared namespace.** All suites and all tests use the namespace of the current kubeconfig context.
2. **Hardcoded resource names.** Resource names come verbatim from the fixture YAML files and are cross-referenced inside the specs (canary/stable services, Istio virtual services, selectors etc.). Two tests applying the same fixture at the same time will collide.
3. **Unscoped teardown.** `TearDownSuite` deletes all E2E resources, not just the ones of the finishing suite. A suite that finishes early would delete the resources of all other running suites.
4. **The Istio suite calls `TearDownSuite` in the middle of tests**, which triggers the same unscoped cleanup while other suites might be running.
5. **Parallelism is disabled.** `E2E_PARALLEL` defaults to 1 and no suite calls `t.Parallel()`.

The controller itself is **not** a blocker. It already watches all namespaces and filters resources by the instance-id label, so it works unchanged with all the options described below.

## Proposal

The work is split in phases. Each phase can be merged independently and brings its own benefits. We do not need to commit to all phases. 

| Phase | What | Effort | PR E2E feedback time |
|---|---|---|---|
| — | Today | — | ~33 min |
| 1 | CI sharding by suite + smaller PR matrix (workflow changes only) | Small | ~8 min |
| 2 | Per-suite namespaces, scoped teardown, suite-level parallelism | Medium | ~8 min in CI, local runs ~29 → ~8 min |
| 3 | Split the three largest suites | Medium | ~5 min |

### Phase 1: CI sharding by suite

This phase only changes the GitHub workflow. No test code is modified, so there is no new flakiness risk. Each shard behaves exactly like today's serial run, just with fewer suites.

- Add a `shard` dimension to the E2E job matrix. Each shard selects a group of suites with a `-run` regex passed through the existing `E2E_TEST_OPTIONS` Makefile variable.
- Use ~5 shards balanced by duration (for example Functional, Analysis, Canary, BlueGreen+AWS+Experiment, and all remaining provider suites).
- Add a small CI check that verifies every `TestXxxSuite` function is covered by a shard, so that new suites are never silently skipped.
- Give each shard its own test result, controller log and coverage artifacts. Coverage data from all shards is merged afterwards.

Possible outline for the GitHub action runner:

```yaml
strategy:
  matrix:
    shard:
      - name: functional
        run: 'TestFunctionalSuite'
      - name: analysis
        run: 'TestAnalysisSuite'
      - name: canary
        run: 'TestCanarySuite'
      - name: bg-aws-experiment
        run: 'TestBlueGreenSuite|TestAWSSuite|TestExperimentSuite'
      - name: providers-misc
        run: 'TestIstioSuite|TestAPISIXSuite|TestRollbackSuite|...'
```

The existing composite status check already aggregates the whole E2E matrix, so branch protection settings do not need to change.

### Phase 2: Per-suite namespaces and suite-level parallelism

This phase fixes the E2E framework so that suites can run in parallel inside the same process. The main beneficiary is the local developer loop.

- **Per-suite namespaces.** Each suite creates its own unique namespace in `SetupSuite` and deletes it in `TearDownSuite`. Since all fixture resources are namespaced, this removes name collisions without having to rename resources or rewrite the cross-references inside the fixture files.
- **Scoped teardown.** Cleanup only affects the resources of the finishing suite. The Istio suite gets a scoped helper instead of calling `TearDownSuite` in the middle of tests.
- **Suite-level parallelism.** Each `TestXxxSuite` function calls `t.Parallel()`. Every top-level suite function gets its own suite instance, so this is safe. The existing method-level `t.Parallel()` calls are removed.
- **Opt-in concurrency.** CI sets `E2E_PARALLEL` to a conservative value (e.g. 3) and a slightly higher wait timeout. The Makefile default stays at 1, and developers can opt in with `E2E_PARALLEL=4 make test-e2e`.

A nice side effect is that failed or aborted runs leave their resources in disposable namespaces instead of polluting a shared one.

### Phase 3: Split the three largest suites (Optional)

After Phase 1, the longest shard is bounded by the largest suite (~6.5 minutes). The Functional, Analysis and Canary suites are split in two suites each. The small shared setup (common analysis templates) is duplicated in each half, which is safe because of the per-suite namespaces of Phase 2.

This is mostly a mechanical move of test methods between files. The shard check of Phase 1 guarantees that no tests are lost in the process.

## What other Argo projects do

The Argo Workflows project already shards its E2E tests by group, with one cluster per shard and a composite status check. This is the same design as Phase 1 and has been working in production CI for years.

Argo CD, on the other hand, uses much larger custom CI runners and its E2E tests still take 40+ minutes. So Argo CD has the same problem.

## Security Considerations

There is no impact for the security of the project. In fact, this proposal will improve the experience for new contributors when they submit new fixes (security related or not).

## Risks and Mitigations

There is no risk in the main project as all changes will happen on the build system and test files. No actual change on the controller itself.