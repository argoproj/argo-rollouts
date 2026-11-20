# Controller tracing

The Argo Rollouts controller supports exporting OpenTelemetry traces over OTLP/gRPC.
Tracing is disabled by default. Set `--otlp-address` to configure a collector and enable the tracer provider.

Rollout and AnalysisRun reconciliations emit spans for meaningful work, persisted state changes, and execution
errors. Unchanged successful attempts are suppressed. Selected canary analysis handoffs share a trace after
the controller validates their identity and provenance.
See [#5085](https://github.com/argoproj/argo-rollouts/issues/5085) for the tracing foundation and
[#5105](https://github.com/argoproj/argo-rollouts/issues/5105) for rollout decisions and asynchronous handoffs.

## Configuration

The flags follow Argo CD's trace-export configuration:

| Flag | Default | Description |
| --- | --- | --- |
| `--otlp-address` | Empty | Collector address in `host:port` form, without a URL scheme or path. An empty address disables tracing. |
| `--otlp-insecure` | `true` | Use plaintext transport. Set to `false` to use TLS. |
| `--otlp-headers` | Empty | Comma-separated request headers, such as `key1=value1,key2=value2`. |
| `--otlp-attrs` | Empty | Comma-separated resource attributes in `key:value` form. |
| `--otlp-sample-ratio` | `1` | Fraction of new root traces to sample, from `0` to `1`. |

For example, add these arguments to the controller container in your existing Deployment:

```yaml
args:
  - --otlp-address=otel-collector.observability.svc.cluster.local:4317
  - --otlp-insecure=false
  - --otlp-attrs=deployment.environment.name:production,k8s.cluster.name:production
  - --otlp-sample-ratio=0.1
```

The collector must accept OTLP/gRPC using the selected transport. This example uses TLS and verifies the
collector's certificate against the system certificate authorities, or a configured custom CA.

The exporter batches spans and identifies the controller with resource attributes `service.name=argo-rollouts`
and `service.version` set to its build version. `--otlp-attrs` adds resource attributes without replacing those
service identity fields. Each attribute is split at its first colon, so its value can contain additional colons.

Sampling is parent-based: the ratio applies to new root traces, while child spans honor their parent's sampling
decision. A ratio of `0` drops new root traces but still permits children of a sampled parent. To disable the
exporter, leave `--otlp-address` empty.

## Headers and TLS

The exporter honors standard OpenTelemetry header and certificate environment variables:

| Purpose | Generic variable | Trace-specific variable |
| --- | --- | --- |
| Request headers | `OTEL_EXPORTER_OTLP_HEADERS` | `OTEL_EXPORTER_OTLP_TRACES_HEADERS` |
| Collector CA certificate file | `OTEL_EXPORTER_OTLP_CERTIFICATE` | `OTEL_EXPORTER_OTLP_TRACES_CERTIFICATE` |
| Client certificate file | `OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE` | `OTEL_EXPORTER_OTLP_TRACES_CLIENT_CERTIFICATE` |
| Client private key file | `OTEL_EXPORTER_OTLP_CLIENT_KEY` | `OTEL_EXPORTER_OTLP_TRACES_CLIENT_KEY` |

Trace-specific header and CA settings take precedence over their generic counterparts. For mutual TLS,
a complete trace-specific client certificate and key pair takes precedence over the generic pair. Client
certificates require a matching private key. Use Kubernetes Secret references for authentication headers,
and mount certificate and key files in the controller container.

Explicit `--otlp-headers` values override header environment variables. The `--otlp-insecure` flag determines
whether the connection uses plaintext or TLS, including when standard OpenTelemetry environment variables
specify another transport setting. Certificate settings apply when TLS is enabled.

Setting `OTEL_EXPORTER_OTLP_ENDPOINT` alone does not enable tracing: configure `--otlp-address` explicitly.
The controller installs a W3C Trace Context propagator. On graceful controller exit, it allows up to 10 seconds to
flush queued spans using a separate shutdown context.

## Reconciliation spans

| Span | Recorded state |
| --- | --- |
| `controller.Rollout` | Namespace, name, UID, generation, strategy, phase, revision number, current canary step, abort and promotion flags. |
| `controller.AnalysisRun` | Namespace, name, UID, generation, phase, and counts of configured metrics and their result phases. |

Both span types record `argo.rollouts.reconciliation.cause`, `argo.rollouts.reconciliation.outcome`, and
`argo.rollouts.reconciliation.status_changed`. Causes describe only what the controller can establish;
an ordinary informer event does not identify the precise action that originally queued the object.
Outcomes are `no_change`, `state_changed`, `error`, or `panic`. A no-change attempt is retained when it
performed useful work, such as creating a gate or processing a validated result.

Persisted rollout transitions produce `rollout.step.advanced`, `rollout.promoted`, `rollout.paused`,
`rollout.aborted`, and `rollout.superseded` events. Analysis phase transitions produce
`analysis_run.phase_changed`. Events describe the observed persisted transition; they are not an
exactly-once event delivery API.

A rejected analysis and a controller execution error are different outcomes. For example, an AnalysisRun
can finish with phase `Failed`, and its Rollout can record an abort, while both reconciliations complete
normally with OpenTelemetry status unset. API failures and panics set error status using a fixed description.
Raw error details stay in the existing controller logs.

Fields prefixed with `argo.rollouts.*` are project-specific vocabulary. The controller exports a fixed
allowlist of identity and decision fields. It does not export arbitrary labels or annotations, metric queries,
arguments, provider credentials, resource status messages, or demo correlation metadata.

## Canary analysis handoffs

The controller can produce this trace shape for a canary step analysis gate:

```text
controller.Rollout                    creates the AnalysisRun
  controller.AnalysisRun              records a measurement attempt
  controller.AnalysisRun              persists its terminal result
    controller.Rollout                processes the result and advances or aborts
```

Each span covers one attempt and ends when that attempt finishes. Waiting between reconciliations is a gap
between spans; it does not keep a controller span running. A child can begin after its parent has ended.

The two parent relationships use an in-memory registry shared by the Rollout and AnalysisRun controllers in
one manager. It records only span contexts produced by this process:

1. A successful API Create records the creating Rollout context against the returned AnalysisRun UID.
   Reusing an existing AnalysisRun after a name collision does not assign it a new creator.
2. A successful terminal status write records the producing AnalysisRun context against its returned UID,
   resource version, and terminal phase. Failed writes and no-op reconciliations publish no result context.

Before continuing a trace, the controller validates namespace, resource names, exact owner UID, generations,
pod-template hash, canary step index, and the current AnalysisRun reference. A terminal result also requires
the exact recorded resource version and phase, a result published before the consuming attempt began,
and a unique eligible current terminal transition.

A Rollout selects the result parent only when its native canary gate logic actually processes the result.
Its span is started at that point with the original attempt start timestamp. Earlier validation, stale-cache,
pause, defaulting, and error paths do not acquire a result parent merely because a terminal result exists.
Once started, a span's parent is never changed.

Result context remains available after a failed Rollout status write, so a retry can continue the same trace.
It is consumed only when a successful status response records the terminal phase or advances past the gate.
The registry retains a tombstone to prevent stale retries from publishing the same result again.

## Correlation boundaries

The registry holds at most 10,000 AnalysisRuns for up to 24 hours after creation. Expiry, eviction, restart,
leadership changes, missing evidence, and ambiguous results produce independent roots. Watch delivery can
precede the API response that registers a context; that race also produces an independent root. Tracing does
not delay a queue or add Kubernetes writes to recover a missing relationship.

Direct parent propagation currently covers canary step analysis. Background and blue-green reconciliations
retain their state and lifecycle telemetry but do not claim equivalent analysis parent relationships.
Separately running analysis controllers do not share the manager-local registry.

User-writable trace-context annotations are not trusted as parent carriers. Release-wide deployment identities,
CI span links, durable cross-process carriers, and GitOps recovery lineage require a separate reviewed
correlation contract. A shared deployment identifier alone does not establish a parent relationship.
