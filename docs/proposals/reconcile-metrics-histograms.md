---
title: Reconcile Metrics Histogram Buckets
authors:
  - '@greta-ramaneckaite'
creation-date: 2026-10-05
---

# Reconcile Metrics Histogram Buckets

## Summary

`rollout_reconcile`, `analysis_run_reconcile`, `experiment_reconcile`, and `notification_send` are
emitted as classic Prometheus histograms with a hardcoded, narrow bucket set
(`[0.01, 0.15, 0.25, 0.5, 1]` seconds) that has been unchanged since the early days of the project.
Any reconcile slower than 1s is silently bucketed into the top bucket with no further resolution,
which breaks latency percentile/heatmap queries during real slowdowns.

This proposal has two parts:

1. Widen the existing classic histogram buckets to cover a realistic reconcile-latency range
   (already implemented in [#4983](https://github.com/argoproj/argo-rollouts/pull/4983)).
2. Additionally emit these same four metrics as Prometheus **native histograms**, so bucket
   boundaries are chosen automatically (exponential, per the native histogram spec) rather than
   hand-tuned, addressing the "automatic buckets, not manual tuning PRs" direction raised in review.

Native histograms are additive to classic histograms (client_golang can emit both from a single
`Histogram` at once), so part 2 does not require removing or replacing part 1 — it is a backward
compatible add-on for scrapers that support native histograms.

## Motivation

The original PR (#4983) proposed a `--metrics-histogram-buckets` CLI flag to make these buckets
configurable. Reviewers pushed back on two grounds:

- No prior art for this specific kind of CLI-configurable-bucket flag in comparable projects.
- Manual bucket tuning is fundamentally the wrong axis to iterate on — the team's stated preference
  is "automatic buckets," i.e. a mechanism that doesn't require a PR and reviewer judgment call
  every time the real latency distribution shifts.

The PR was reworked to simply widen the shared default bucket list (no flag, no override), which
addressed the first objection but not the second. This proposal aims to close the second part by
adopting Prometheus native histograms as the long-term automatic-bucket mechanism, while keeping
the widened classic buckets as the safe default for environments that can't yet consume native
histograms (Prometheus < v2.40, or non-Prometheus scrapers).

### Goals

- Give operators enough resolution to see real reconcile-latency regressions, today, without
  requiring a new Prometheus version or scrape config (classic histogram widening).
- Provide a path to bucket boundaries that adapt automatically as the latency distribution changes,
  without further PRs to this repo (native histograms).
- Avoid introducing new CLI flags, environment variables, or other user-facing configuration surface
  for this.

### Non-Goals

- Changing bucket boundaries or histogram behavior for any metric other than the four reconcile
  metrics listed above.
- Making native histograms a requirement — classic histograms remain the always-on default.
- Addressing metric cardinality/cost concerns unrelated to these four metrics.

## Proposal

### Part 1 — widen classic histogram buckets (already in #4983)

Replace the per-metric inline `Buckets: []float64{0.01, 0.15, .25, .5, 1}` with a single shared
`reconcileHistogramBuckets` variable used by all four metrics:

```go
var reconcileHistogramBuckets = []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
```

This is a pure data change (5 files touched, test assertions updated to match), no new flags or
overrides. It's a stopgap, not the end state — these boundaries are an educated geometric spread,
not validated against a large cross-section of real fleets, and will eventually be superseded in
practice by native histograms for anyone who adopts them.

### Part 2 — add native histograms alongside the classic ones

For the same four `prometheus.HistogramOpts`, add `NativeHistogramBucketFactor` (and friends):

```go
prometheus.HistogramOpts{
    Name:    "rollout_reconcile",
    Help:    "...",
    Buckets: reconcileHistogramBuckets, // classic buckets, unchanged from part 1

    // Additionally emit as a native histogram — exponential, auto-ranging buckets,
    // ingested by any Prometheus server with native histograms enabled (v2.40+).
    NativeHistogramBucketFactor:     1.1,  // ~80 buckets per doubling; standard default
    NativeHistogramMaxBucketNumber:  100,  // cap bucket growth/cardinality
    NativeHistogramMinResetDuration: time.Hour,
}
```

A `prometheus.Histogram` can carry both classic and sparse (native) bucket data simultaneously —
this is additive, not a migration. Scrapers/servers that don't understand native histograms ignore
the sparse series and see only the classic buckets from part 1, unchanged.

`NativeHistogramBucketFactor: 1.1` is the value documented as a generally good cost/accuracy
trade-off upstream in Prometheus (each bucket at most ~10% wider than the previous one). Combined
with `NativeHistogramMaxBucketNumber`, this bounds the per-series cardinality cost that's typically
the concern with enabling native histograms on a busy controller metric.

### Use cases

- **Operator on Prometheus >= 2.40 with native histograms enabled**: gets automatically-ranged,
  high-resolution reconcile-latency histograms with zero bucket-boundary configuration, satisfying
  the "automatic buckets" goal directly.
- **Operator on an older Prometheus or non-Prometheus scraper**: unaffected; sees only the classic
  histogram with the widened buckets from part 1, which is already a strict improvement over today.
- **Argo Rollouts maintainers**: no more bucket-boundary PRs need to be reviewed/merged going
  forward for operators who opt into native histograms at the Prometheus server config level — no
  code-level flag or override is needed on the Argo Rollouts side.

### Implementation Details/Notes/Constraints

- Scoped to the same four metrics already touched by #4983:
  `rollout_reconcile`, `analysis_run_reconcile`, `experiment_reconcile`, `notification_send`.
- No new CLI flags, environment variables, or `SetReconcileHistogramBuckets`-style override — this
  was the exact surface area rejected in round 1 of review on #4983, and this proposal does not
  reintroduce it.
- `client_golang` version currently vendored needs to support `NativeHistogramBucketFactor` et al.
  (available since v1.14+); a version bump may be required as part of implementation — to be
  confirmed against `go.mod`.

### Security Considerations

None — this only changes metric emission granularity, not access control or data content.

### Risks and Mitigations

- **Risk**: enabling native histograms increases per-series bucket count (~80 vs 11 classic
  buckets), raising memory/storage cost on the controller and on Prometheus.
  **Mitigation**: `NativeHistogramMaxBucketNumber` caps growth; factor 1.1 is the documented sane
  default rather than a more aggressive (lower) factor.
- **Risk**: classic bucket boundaries from part 1 still need real-world validation.
  **Mitigation**: tracked separately as a follow-up (compare against real fleet
  `*_reconcile_bucket` data) and not blocking for either part of this proposal, since part 1 is
  already a strict improvement over the status quo and part 2 does not depend on the exact classic
  values chosen.

### Upgrade / Downgrade Strategy

Fully backward compatible in both directions — native histogram emission is additive, and classic
histogram consumers are unaffected. No migration or opt-in flag needed on the Argo Rollouts side;
operators opt in by enabling native histograms on their own Prometheus server.

## Drawbacks

- Added bucket-count/cardinality cost for operators who do enable native histogram scraping,
  though bounded by `NativeHistogramMaxBucketNumber`.
- Two bucket mechanisms (classic + native) co-exist indefinitely rather than a single clean
  mechanism, since dropping classic buckets would break operators not yet on native-histogram-
  capable Prometheus.

## Alternatives

- **Status quo (`--metrics-histogram-buckets` CLI flag)**: rejected in review; adds user-facing
  configuration surface for a problem that has a standard solution upstream.
- **Classic bucket widening only (current state of #4983)**: a real, shippable improvement on its
  own, but doesn't address the "automatic buckets" direction maintainers want long-term.
- **Native histograms only, drop classic buckets**: would regress operators not yet running
  Prometheus >= v2.40 or using other scrapers (e.g. Datadog's OpenMetrics/Prometheus check version
  support varies) — rejected as not backward compatible enough for a default-on controller metric.
