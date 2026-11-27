# Controller tracing

The Argo Rollouts controller supports exporting OpenTelemetry traces over OTLP/gRPC.
Tracing is disabled by default. Set `--otlp-address` to configure a collector and enable the tracer provider.

This initial support configures the exporter and SDK. It exports spans created through the global OpenTelemetry
provider, but does not itself add Rollout, AnalysisRun, metric-provider, or traffic-router spans.
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
The exporter uses W3C Trace Context propagation. On graceful controller exit, it allows up to 10 seconds to
flush queued spans using a separate shutdown context.
