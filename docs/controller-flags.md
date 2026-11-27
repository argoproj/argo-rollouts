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

The complete list of flags, with their defaults, is generated from the controller's command definition on every
`make codegen` run, so it always matches the code of the release you are reading the docs for:

* [Flag Reference](generated/rollouts-controller/rollouts-controller.md)

Run `rollouts-controller --help` to see the flags of the exact binary you are running.

## Notes on a few flags

* `--namespaced` runs the controller in namespaced mode. It only watches the namespace it runs in and does not need
  cluster-wide RBAC. See [namespace-install.yaml](installation.md#controller-installation-with-manifests).
* `--leader-election-renew-deadline` must be strictly less than `--leader-election-lease-duration` and greater than
  1.2 times `--leader-election-retry-period`; client-go refuses to start leader election otherwise. See the
  [FAQ](FAQ.md#can-we-run-the-argo-rollouts-controller-in-ha-mode) for running several replicas.
* `--analysis-shared-secret-mode=LabelFilter` (the default) only lets AnalysisRuns read secrets in the shared-secret
  namespace that carry the label `argo-rollouts.argoproj.io/shared-secret: "true"`; `AllSecrets` allows any secret
  in that namespace.
* `--alb-ingress-classes` and `--nginx-ingress-classes` can be repeated to watch more than one ingress class.
* `--aws-verify-target-group` needs AWS permissions, see
  [AWS Load Balancer Controller](features/traffic-management/alb.md).
* `--self-service-notification-enabled` lets the controller read notification configuration from the namespace of the
  Rollout in addition to its own, see [Notifications](features/notifications.md).
