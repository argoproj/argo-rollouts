# Apache APISIX

[Apache APISIX](https://apisix.apache.org/) and the
[Apache APISIX Ingress Controller](https://apisix.apache.org/docs/ingress-controller/)
can manage traffic for an Argo Rollouts canary through an `ApisixRoute`.
Argo Rollouts updates the route's backend weights and can create a higher-priority
route that sends requests with a matching header to the canary.

## Supported capabilities

The APISIX traffic router currently has the following support status:

| Capability | Status |
| --- | --- |
| Weighted traffic routing (`setWeight`) | Alpha |
| Header-based routing (`setHeaderRoute`) | Alpha |
| Experiment weights | Not supported |
| Traffic mirroring | Not supported |

See the project-wide
[traffic shaping support matrix](https://github.com/argoproj/argo-rollouts#supported-traffic-shaping-integrations)
for the current status of every traffic router.

When header-based routing is active, Argo Rollouts creates a managed
`ApisixRoute` with the name specified in `managedRoutes`. The controller deletes
that route when the rollout completes or is aborted. Its ServiceAccount therefore
needs `get`, `watch`, `update`, `create`, and `delete` access to `ApisixRoute`
resources.

## Prerequisites

You need:

- Kubernetes 1.26 or newer
- Helm 3.8 or newer
- `kubectl`
- the [Argo Rollouts kubectl plugin](../../installation.md#kubectl-plugin-installation)

This example targets the following component versions:

| Component | Version |
| --- | --- |
| Apache APISIX Helm chart | 2.16.0 |
| Apache APISIX | 3.17.0 |
| Apache APISIX Ingress Controller | 2.1.0 |

The table records the versions pinned by this example. It is not a minimum-version
compatibility guarantee. Validate the complete workflow in your own environment
before using it for production traffic.

## Install APISIX and Argo Rollouts

Add the official APISIX Helm repository and install APISIX together with the
Ingress Controller:

```bash
helm repo add apisix https://apache.github.io/apisix-helm-chart
helm repo update apisix

helm upgrade --install apisix apisix/apisix \
  --version 2.16.0 \
  --namespace ingress-apisix \
  --create-namespace \
  --set ingress-controller.enabled=true \
  --set ingress-controller.apisix.adminService.namespace=ingress-apisix \
  --set ingress-controller.gatewayProxy.createDefault=true
```

These values follow the APISIX Ingress Controller
[Helm installation guide](https://apisix.apache.org/docs/ingress-controller/install/).
Wait for the APISIX workloads before continuing:

```bash
kubectl rollout status --namespace ingress-apisix \
  deployment/apisix --timeout=300s
kubectl rollout status --namespace ingress-apisix \
  deployment/apisix-ingress-controller --timeout=300s
kubectl rollout status --namespace ingress-apisix \
  statefulset/apisix-etcd --timeout=300s
```

Install Argo Rollouts using the
[standard installation](../../installation.md#controller-installation):

```bash
kubectl create namespace argo-rollouts
kubectl apply --server-side \
  --namespace argo-rollouts \
  --filename https://github.com/argoproj/argo-rollouts/releases/latest/download/install.yaml

kubectl wait --namespace argo-rollouts \
  --for=condition=Available deployment/argo-rollouts --timeout=180s
```

Server-side apply avoids the client-side `last-applied-configuration` annotation
size limit for the bundled CRDs.

If you maintain custom controller RBAC, verify the permissions required for
managed APISIX routes:

```bash
kubectl auth can-i create apisixroutes.apisix.apache.org \
  --namespace default \
  --as=system:serviceaccount:argo-rollouts:argo-rollouts
kubectl auth can-i delete apisixroutes.apisix.apache.org \
  --namespace default \
  --as=system:serviceaccount:argo-rollouts:argo-rollouts
```

Both commands should return `yes`.

## Deploy the example

The example contains stable and canary Services, an `ApisixRoute`, and a
Rollout:

```bash
kubectl apply --kustomize \
  "https://github.com/argoproj/argo-rollouts//examples/apisix?ref=stable"
```

The route uses the `apisix` IngressClass and references both Services:

```yaml
apiVersion: apisix.apache.org/v2
kind: ApisixRoute
metadata:
  name: rollouts-apisix-route
spec:
  ingressClassName: apisix
  http:
    - name: rollouts-apisix
      match:
        paths:
          - /*
        methods:
          - GET
          - POST
          - PUT
          - DELETE
          - PATCH
        hosts:
          - rollouts-demo.apisix.local
      backends:
        - serviceName: rollout-apisix-canary-stable
          servicePort: 80
        - serviceName: rollout-apisix-canary-canary
          servicePort: 80
```

The Rollout directs matching `trace: debug` requests to the canary before it
starts changing the primary route's weights:

```yaml
strategy:
  canary:
    canaryService: rollout-apisix-canary-canary
    stableService: rollout-apisix-canary-stable
    trafficRouting:
      managedRoutes:
        - name: set-header
      apisix:
        route:
          name: rollouts-apisix-route
          rules:
            - rollouts-apisix
    steps:
      - setCanaryScale:
          replicas: 1
        setHeaderRoute:
          name: set-header
          match:
            - headerName: trace
              headerValue:
                exact: debug
      - pause: {}
      - setWeight: 20
      - pause: {}
      - setWeight: 40
      - pause:
          duration: 15
      - setWeight: 60
      - pause:
          duration: 15
      - setWeight: 80
      - pause:
          duration: 15
```

The initial creation skips the canary steps because there is no update yet.
Wait for it to become healthy:

```bash
kubectl argo rollouts status rollout-apisix-canary --timeout 180s
```

Forward the APISIX gateway port in a separate terminal:

```bash
kubectl port-forward --namespace ingress-apisix \
  service/apisix-gateway 9080:80
```

Verify that the initial version is blue:

```bash
curl --silent --show-error \
  --header 'Host: rollouts-demo.apisix.local' \
  http://127.0.0.1:9080/color
```

## Trigger a rollout

Update the `rollouts-demo` container to the yellow image:

```bash
kubectl argo rollouts set image rollout-apisix-canary \
  rollouts-demo=argoproj/rollouts-demo:yellow
```

Watch the rollout until it pauses after creating the header route:

```bash
kubectl argo rollouts get rollout rollout-apisix-canary --watch
```

When the rollout shows `Paused`, press `Ctrl+C` to stop watching.

## Verify header-based routing

A request without the `trace` header still goes to the stable version:

```bash
curl --silent --show-error \
  --header 'Host: rollouts-demo.apisix.local' \
  http://127.0.0.1:9080/color
```

The response should be `blue`. The same request with `trace: debug` goes to the
canary and should return `yellow`:

```bash
curl --silent --show-error \
  --header 'Host: rollouts-demo.apisix.local' \
  --header 'trace: debug' \
  http://127.0.0.1:9080/color
```

Inspect the managed route created by Argo Rollouts:

```bash
kubectl get apisixroute set-header --output yaml
```

The route has a higher priority than the primary route, matches the `trace`
header, and has only the canary Service as its backend.

## Verify weighted traffic

Promote the rollout to the 20% weight step:

```bash
kubectl argo rollouts promote rollout-apisix-canary
```

Inspect the backend weights:

```bash
kubectl get apisixroute rollouts-apisix-route \
  --output=jsonpath='{range .spec.http[0].backends[*]}{.serviceName}={.weight}{"\n"}{end}'
```

The stable and canary weights should be `80` and `20`. Send 100 requests to
observe the actual responses:

```bash
for i in $(seq 1 100); do
  curl --silent --show-error \
    --header 'Host: rollouts-demo.apisix.local' \
    http://127.0.0.1:9080/color
done | sort | uniq -c
```

Both `blue` and `yellow` should appear, with a distribution close to the
configured weights. A small sample is not expected to produce an exact 80/20
split.

## Abort and recover

Abort the rollout while it is paused:

```bash
kubectl argo rollouts abort rollout-apisix-canary
kubectl wait --for=delete apisixroute/set-header --timeout=60s
```

Argo Rollouts removes the managed header route and restores the primary route
to 100% stable traffic. Verify the weights and request result:

```bash
kubectl get apisixroute rollouts-apisix-route \
  --output=jsonpath='{range .spec.http[0].backends[*]}{.serviceName}={.weight}{"\n"}{end}'

curl --silent --show-error \
  --header 'Host: rollouts-demo.apisix.local' \
  http://127.0.0.1:9080/color
```

An aborted Rollout remains `Degraded` until its desired state is changed back
to the stable version. Restore the blue image:

```bash
kubectl argo rollouts set image rollout-apisix-canary \
  rollouts-demo=argoproj/rollouts-demo:blue
kubectl argo rollouts status rollout-apisix-canary --timeout 180s
```

## Integrating with GitOps

Argo Rollouts owns the backend `weight` fields while a rollout is in progress.
If Argo CD also manages the `ApisixRoute`, configure it to ignore those fields
to prevent sync operations from replacing the active weights:

```yaml
spec:
  ignoreDifferences:
    - group: apisix.apache.org
      kind: ApisixRoute
      jqPathExpressions:
        - .spec.http[].backends[].weight
  syncPolicy:
    syncOptions:
      - RespectIgnoreDifferences=true
```

The example omits initial weights from Git for the same reason.

## Troubleshooting

### The `set-header` route is not created

Check the Rollouts controller events and logs:

```bash
kubectl describe rollout rollout-apisix-canary
kubectl logs --namespace argo-rollouts deployment/argo-rollouts
```

An `apisixroutes ... is forbidden` message means the controller ServiceAccount
cannot create or delete managed routes. Run the `kubectl auth can-i` checks from
the installation section and update custom RBAC if either returns `no`.

### Requests return 404

The example route matches the `rollouts-demo.apisix.local` host. Include
`Host: rollouts-demo.apisix.local` in local requests or create a DNS record for
that host.

### The route exists but APISIX does not use it

Confirm that the route uses the `apisix` IngressClass and inspect the Ingress
Controller:

```bash
kubectl get ingressclass apisix
kubectl get apisixroute rollouts-apisix-route --output yaml
kubectl logs --namespace ingress-apisix \
  deployment/apisix-ingress-controller
```

See the APISIX Ingress Controller
[configuration troubleshooting guide](https://apisix.apache.org/docs/ingress-controller/reference/apisix-ingress-controller/configuration-troubleshoot/)
for inspecting the translated gateway configuration.

### APISIX returns 502 or 503

Check that both Services have ready endpoints:

```bash
kubectl get endpoints \
  rollout-apisix-canary-stable rollout-apisix-canary-canary
kubectl get pods --selector app=rollout-apisix-canary
```

## Cleanup

Stop the port-forward process, then delete the example:

```bash
kubectl delete --kustomize \
  "https://github.com/argoproj/argo-rollouts//examples/apisix?ref=stable"
```

To remove the controllers as well:

```bash
helm uninstall apisix --namespace ingress-apisix
kubectl delete namespace ingress-apisix
kubectl delete namespace argo-rollouts
```
