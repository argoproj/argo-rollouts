# Argo Rollouts and Helm

Argo Rollouts will respond to changes in Rollout resources
regardless of the event source. If you package your manifest
with the Helm package manager you can perform Progressive Delivery deployments with Helm

1. Install the Argo Rollouts controller in your cluster: https://github.com/argoproj/argo-rollouts#installation
2. Install the `helm` executable locally: https://helm.sh/docs/intro/install/

## Deploying the initial version

To deploy the first version of your application:

```
git clone https://github.com/argoproj/argo-rollouts.git
cd argo-rollouts/examples
helm install example ./helm-blue-green/
```

Your application will be deployed and exposed via the `example-helm-guestbook` service

## Perform the second deployment

To deploy the updated version using a Blue/Green strategy:

```
helm upgrade example ./helm-blue-green/  --set image.tag=0.2
```

Now, two versions will exist in your cluster (and each one has an associated service)

```
kubectl-argo-rollouts get rollout example-helm-guestbook
```

## Promoting the rollout

To advance the rollout and make the new version stable

```
kubectl-argo-rollouts promote example-helm-guestbook
```

This promotes container image `ks-guestbook-demo:0.2` to `green` status and `Rollout` deletes old replica which runs `ks-guestbook-demo:0.1`.
