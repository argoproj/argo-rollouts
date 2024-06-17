# Kong Ingress

With the introduction of the Kubernetes Gateway API it is now possible to use Argo Rollouts with all compliant implementations that support it. The integration is available with the [Argo Rollouts Gateway API plugin](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/) currently hosted in Argo Labs.

Useful resources:

* [The Gateway API specification](https://gateway-api.sigs.k8s.io/)
* [Support of the Gateway API in Kong](https://docs.konghq.com/kubernetes-ingress-controller/latest/concepts/gateway-api/)
* [Argo Rollouts Plugin capabilities](../plugins/) 
* [Plugin for the Gateway API](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi)

The process involves the following steps:

1. Installing the Gateway API CRDs in your cluster
1. Installing Kong and enabling the Gateway API support feature
1. Creating a GatewayClass and Gateway resources 
1. Installing Argo Rollouts + gateway API plugin in the cluster
1. Defining a Rollout that takes advantage of the plugin

For a full application that includes all manifests see the [plugin example](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/tree/main/examples/kong).


