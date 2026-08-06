# HAProxy Ingress

With the introduction of the Kubernetes Gateway API it is now possible to use Argo Rollouts with all compliant implementations that support it. The integration is available with the [Argo Rollouts Gateway API plugin](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/) currently hosted in Argo Labs.

!!! note

    There are two projects that call themselves an HAProxy ingress controller. The one described here is the community [HAProxy Ingress](https://haproxy-ingress.github.io/) controller, which is the [Gateway API conformant](https://gateway-api.sigs.k8s.io/implementations/) HAProxy implementation. Gateway API conformance arrived in v0.17. Earlier versions have only partial Gateway API support and will not work with the plugin.

Useful resources:

* [The Gateway API specification](https://gateway-api.sigs.k8s.io/)
* [Support of the Gateway API in HAProxy Ingress](https://haproxy-ingress.github.io/docs/configuration/gateway-api/)
* [Argo Rollouts Plugin capabilities](../plugins/)
* [Plugin for the Gateway API](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi)

The process involves the following steps:

1. Installing the Gateway API CRDs in your cluster
1. Installing HAProxy Ingress (v0.17 or later) with Gateway API support
1. Creating a GatewayClass and Gateway resources
1. Installing Argo Rollouts + gateway API plugin in the cluster
1. Defining a Rollout that takes advantage of the plugin

Note that HAProxy Ingress does not implement `GRPCRoute` or `UDPRoute`, so the gRPC routing capabilities of the plugin cannot be used with this provider. HAProxy Ingress also runs a single shared HAProxy deployment instead of creating one Service per Gateway, so all Gateway traffic enters through the chart's own `haproxy-ingress` Service.

For a full application that includes all manifests see the [plugin example](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/tree/main/examples/haproxy).
