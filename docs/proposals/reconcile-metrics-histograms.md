---
title: Reconcile Metrics Histogram Buckets
authors:
  - '@greta-ramaneckaite'
creation-date: 2026-10-05
---

# Reconcile Metrics Histogram Buckets

## Summary

`rollout_reconcile`, `analysis_run_reconcile`, `experiment_reconcile`, and `notification_send` are
emitted as classic Prometheus histograms with a hardcoded bucket set (`[0.01, 0.15, 0.25, 0.5, 1]`
seconds) that has been unchanged since the early days of the project. Any reconcile slower than 1s
lands in the top bucket with no further resolution, so latency percentiles and heatmaps go flat
exactly when the controller is slow.

This proposal fixes that once, in a way that doesn't need revisiting:

1. **Add native histograms** to these four metrics, so Prometheus picks bucket boundaries
   automatically. This is the long-term mechanism — no future bucket-tuning PRs.
2. **Widen the classic buckets one time** to a range that covers realistic reconcile latencies.
   Classic buckets are still what most scrapers read, so they need sane defaults; this is a
   one-off reset alongside (1), not the start of ongoing tuning.

Both changes ship together in one implementation PR (draft:
[#4983](https://github.com/argoproj/argo-rollouts/pull/4983), currently classic widening only).

## Motivation

Hand-tuning histogram buckets — through a CLI flag, an override, or repeated PRs changing the
defaults — is the wrong axis to solve this on:

- No prior art in comparable projects — argocd, flagger, and flux have no CLI-configurable bucket
  boundaries.
- It's a slippery slope: once one metric's buckets are configurable, the next metric gets the same
  request, and the project maintains a pile of overrides instead of good defaults.
- Buckets go stale as latency distributions shift. A mechanism that adapts on its own beats a human
  noticing, opening a PR, and getting new hardcoded numbers reviewed each time.

Native histograms are that mechanism. The classic widening exists only because not every consumer
can read native histograms yet.

## Proposal

```go
var reconcileHistogramBuckets = []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

prometheus.HistogramOpts{
    Name:    "rollout_reconcile",
    Help:    "...",
    Buckets: reconcileHistogramBuckets, // classic buckets, widened once

    // Also emit as a native histogram — exponential, auto-ranging buckets.
    NativeHistogramBucketFactor:     1.1, // each bucket at most ~10% wider than the previous
    NativeHistogramMaxBucketNumber:  100, // cap per-series bucket count
    NativeHistogramMinResetDuration: time.Hour,
}
```

Applied to the same four metrics. No new CLI flags, environment variables, or override mechanism.
The vendored `client_golang` (v1.24.0) already supports these fields, so no dependency bump.

**Who sees what:** a single `Histogram` carries both classic and native data. Native histograms are
only exposed over the protobuf exposition format, so they're picked up by Prometheus >= v2.40 with
native histograms enabled (`--enable-feature=native-histograms` before v3, or
`scrape_native_histograms` / protobuf scraping in v3). Text-format and OpenMetrics scrapers —
including older Prometheus and most third-party agents — ignore the native data and read the
widened classic buckets.

**Risk:** native histograms add per-series cost (up to `NativeHistogramMaxBucketNumber` buckets vs.
11 classic), but only for operators who enable them on their Prometheus server.
`NativeHistogramMaxBucketNumber` bounds the growth. Classic consumers see a bucket-count change
(5 → 11) and should update any dashboards that hardcode `le` values. Otherwise fully backward
compatible, no migration.

## Alternatives

- **CLI flag for bucket boundaries**: no prior art, and sets a precedent for per-metric overrides
  that doesn't scale.
- **Native histograms only, leave classic buckets as-is**: classic consumers would keep the 1s
  ceiling, so most users see no improvement.
- **Classic widening only**: fixes today's ceiling but leaves bucket choice manual, inviting future
  tuning PRs.
