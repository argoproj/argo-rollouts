# Google Cloud 

With the introduction of the Kubernetes Gateway API it is now possible to use Argo Rollouts with all compliant implementations that support it. The integration is available with the [Argo Rollouts Gateway API plugin](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/) currently hosted in Argo Labs.

Useful resources:

* [The Gateway API specification](https://gateway-api.sigs.k8s.io/)
* [Support of the Gateway API in Google Cloud](https://cloud.google.com/kubernetes-engine/docs/concepts/gateway-api)
* [Argo Rollouts Plugin capabilities](../plugins/) 
* [Plugin for the Gateway API](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi)

The process involves the following steps:

1. Creating a Kubernetes cluster with support for the Gateway API in Google Cloud
1. Creating a Load balancer that is managed by the Gateway API in Google Cloud
1. Installing Argo Rollouts + gateway API plugin in the cluster
1. Defining a Rollout that takes advantage of the plugin

For a full application that includes all manifests see the [plugin example](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/tree/main/examples/google-cloud).


