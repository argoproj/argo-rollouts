# Promotion traffic-provider E2E tests

Run only in a disposable cluster. These tests create and delete isolated
namespaces and restart the two controller Deployments in `argo-rollouts`.
They never infer a cluster from the current kubectl context.

Prerequisites:

* Build and install both controllers and CRDs from this branch.
* Register the bundled Pod annotation TrafficRouter in the controller ConfigMap
  as shown in `docs/features/declarative-promotion.md`.
* Use the example Deployment names `argo-rollouts` and
  `argo-rollouts-promotion-controller` in namespace `argo-rollouts`.
* Allow pulling `traefik/whoami:v1.10.4`, or load it into the test cluster.
  `PROMOTION_E2E_IMAGE` may override it with a compatible HTTP server image.

```sh
PROMOTION_E2E_KUBECONFIG=/path/to/disposable-kubeconfig \
  go test -tags=e2e ./test/promotione2e -v -count=1 -timeout=25m
```

Coverage includes:

* Native blue-green promotion, Service-selector traffic rollback, a real HTTP
  request through the Service proxy, and a subsequent release.
* Retained annotation-routed canary rollback before promotion, at the 50% gate,
  and after final promotion.
* Legacy `weightSwitch` configuration and provider-based configuration.
* Annotation-based blue-green promotion and rollback using `weightSwitch`.
* A subsequent canary release through all gates after each rollback.
* Duplicate rollback request replay and controller restart at a manual gate.
* Exact Pod UID and restart-count preservation during traffic-only rollback.
* Native canary promotion without a traffic adapter.

The annotation cases verify Kubernetes annotations and replica lifecycle.
They do not emulate a cloud load balancer, prove observed request percentages,
or certify long-lived connection draining by an external router.
