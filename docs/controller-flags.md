# Controller Flags

The Argo Rollouts controller is configured with command-line flags on the `argo-rollouts` container. Set them under
`args` in the controller Deployment, for example:

```yaml
spec:
  template:
    spec:
      containers:
        - name: argo-rollouts
          args:
            - --leader-elect
            - --rollout-threads=20
            - --loglevel=debug
```

The tables below list every flag of the `rollouts-controller` binary, with its default. Run
`rollouts-controller --help` to see the flags of the exact version you are running.

## General

| Flag | Default | Description |
|------|---------|-------------|
| `--namespaced` | `false` | Run the controller in namespaced mode. It only watches the namespace it runs in and does not need cluster-wide RBAC. See [namespace-install.yaml](installation.md#controller-installation-with-manifests). |
| `--instance-id` | `""` | Only operate on Rollout objects that carry this instance id (the `argo-rollouts.argoproj.io/controller-instance-id` label). |
| `--rollout-resync` | `900` | Full resync period of the informers, in seconds. |
| `--controllers` | all | Explicit list of controllers to run. Currently only `analysis` is supported as a value, for example `--controllers=analysis` to run an analysis-only instance. |
| `--version` | `false` | Print the version and exit. |

## Logging

| Flag | Default | Description |
|------|---------|-------------|
| `--loglevel` | `info` | Log level. One of `debug`, `info`, `warn`, `error`. |
| `--logformat` | `""` (text) | Log format. One of `text`, `json`. |
| `--kloglevel` | `0` | Verbosity of the `klog` (client-go) logger. |

## Ports and profiling

| Flag | Default | Description |
|------|---------|-------------|
| `--metricsPort` | `8090` | Port the metrics endpoint is exposed on. |
| `--metricsport` | `8090` | Deprecated alias of `--metricsPort`. |
| `--healthzPort` | `8080` | Port the healthz endpoint is exposed on. |
| `--enable-pprof-address` | `""` (disabled) | Address to serve `pprof` profiling on, for example `:6060`. |

## Worker threads

| Flag | Default | Description |
|------|---------|-------------|
| `--rollout-threads` | `10` | Worker threads for the Rollout controller. |
| `--experiment-threads` | `10` | Worker threads for the Experiment controller. |
| `--analysis-threads` | `30` | Worker threads for the Analysis controller. |
| `--service-threads` | `10` | Worker threads for the Service controller. |
| `--ingress-threads` | `10` | Worker threads for the Ingress controller. |
| `--ephemeral-metadata-threads` | `10` | Worker threads for the [Ephemeral Metadata](features/ephemeral-metadata.md) reconciler. |
| `--ephemeral-metadata-update-pod-retries` | `3` | Retries when updating a pod's ephemeral metadata. |

## Kubernetes API client

| Flag | Default | Description |
|------|---------|-------------|
| `--qps` | `40` | Maximum queries per second to the Kubernetes API server (client-side throttling). |
| `--burst` | `80` | Maximum burst for client-side throttling. |
| `--kubeconfig` | `""` | Path to a kubeconfig file. Only needed when running out of cluster. The standard `kubectl` connection flags (`--context`, `--namespace`, `--server`, `--token`, ...) are accepted as well. |

## Leader election

See the [FAQ](FAQ.md#can-we-run-the-argo-rollouts-controller-in-ha-mode) for running several replicas.

| Flag | Default | Description |
|------|---------|-------------|
| `--leader-elect` | `true` | Perform leader election between controller instances so that only one operates at a time. |
| `--leader-election-lease-duration` | `15s` | How long non-leader candidates wait after observing a leadership renewal before trying to acquire a lease that was not renewed. Effectively the maximum time a leader can be stopped before it is replaced. |
| `--leader-election-renew-deadline` | `10s` | Interval between attempts by the acting leader to renew its lease before it stops leading. Must be strictly less than the lease duration and greater than 1.2 times the retry period; client-go refuses to start leader election otherwise. |
| `--leader-election-retry-period` | `2s` | Wait between attempts to acquire or renew the lease. |

## Traffic routing

API versions the controller uses when it creates or reads traffic-routing resources.

| Flag | Default | Description |
|------|---------|-------------|
| `--istio-api-version` | `v1alpha3` | Istio `VirtualService` API version. |
| `--ambassador-api-version` | `getambassador.io/v2` | Ambassador `Mapping` API version. |
| `--traffic-split-api-version` | `v1alpha1` | SMI `TrafficSplit` API version. |
| `--traefik-api-group` | `traefik.io` | Traefik API group. |
| `--traefik-api-version` | `traefik.io/v1alpha1` | Traefik API version. |
| `--appmesh-crd-version` | `v1beta2` | AWS App Mesh CRD version. |
| `--ingress-api-version` | `""` (detected) | Ingress API version to use. When empty, the controller detects it from the API server. |
| `--alb-ingress-classes` | `alb` | Ingress class annotations the ALB ingress controller operates on. Repeat the flag to add more. |
| `--nginx-ingress-classes` | `nginx` | Ingress class annotations the NGINX ingress controller operates on. Repeat the flag to add more. |
| `--aws-verify-target-group` | `false` | Verify the ALB target group before progressing through steps. Requires AWS permissions, see [AWS Load Balancer Controller](features/traffic-management/alb.md). |
| `--alb-verify-weight` | `false` | Deprecated alias of `--aws-verify-target-group`. |
| `--aws-target-group-binding-api-version` | `elbv2.k8s.aws/v1beta1` | AWS `TargetGroupBinding` API version used when verifying target group weights. |
| `--alb-tag-key-resource-id` | `ingress.k8s.aws/resource` | AWS load balancer tag key for the resource id used when verifying target group weights. |

## Analysis

| Flag | Default | Description |
|------|---------|-------------|
| `--analysis-shared-secret-namespace` | `""` | Namespace containing secrets that AnalysisRuns in any namespace may reference. |
| `--analysis-shared-secret-mode` | `LabelFilter` | Which secrets in the shared-secret namespace AnalysisRuns may read: `LabelFilter` (only secrets labelled `argo-rollouts.argoproj.io/shared-secret: "true"`) or `AllSecrets` (any secret in the namespace). |

## Notifications

| Flag | Default | Description |
|------|---------|-------------|
| `--self-service-notification-enabled` | `false` | Let the controller read notification configuration from the namespace of the Rollout, in addition to its own namespace. See [Notifications](features/notifications.md). |
