# Migrating to Rollouts

There are ways to migrate to Rollout:

* Convert an existing Deployment resource to a Rollout resource.
* Reference an existing Deployment from a Rollout using `workloadRef` field.

## Convert Deployment to Rollout

When converting a Deployment to a Rollout, it involves changing three fields:

1. Replacing the `apiVersion` from `apps/v1` to `argoproj.io/v1alpha1`
1. Replacing the `kind` from `Deployment` to `Rollout`
1. Replacing the deployment strategy with a [blue-green](features/bluegreen.md) or [canary](features/canary.md) strategy

Below is an example of a Rollout resource using the canary strategy.

```yaml
apiVersion: argoproj.io/v1alpha1  # Changed from apps/v1
kind: Rollout                     # Changed from Deployment
metadata:
  name: rollouts-demo
spec:
  selector:
    matchLabels:
      app: rollouts-demo
  template:
    metadata:
      labels:
        app: rollouts-demo
    spec:
      containers:
      - name: rollouts-demo
        image: argoproj/rollouts-demo:blue
        ports:
        - containerPort: 8080
  strategy:
    canary:                        # Changed from rollingUpdate or recreate
      steps:
      - setWeight: 20
      - pause: {}
```

!!! warning
    When migrating a Deployment which is already serving live production traffic, a Rollout should
    run next to the Deployment before deleting the Deployment or scaling down the Deployment.
    **Not following this approach might result in downtime**. It also allows for the Rollout to be
    tested before deleting the original Deployment.


## Reference Deployment From Rollout

Instead of removing Deployment you can scale it down to zero and reference it from the Rollout resource:

1. Create a Rollout resource.
1. Reference an existing Deployment using `workloadRef` field.
1. Scale-down an existing Deployment by changing `replicas` field of an existing Deployment to zero.
1. To perform an update, the change should be made to the Pod template field of the Deployment.

Below is an example of a Rollout resource referencing a Deployment.

```yaml
apiVersion: argoproj.io/v1alpha1               # Create a rollout resource
kind: Rollout
metadata:
  name: rollout-ref-deployment
spec:
  replicas: 5
  selector:
    matchLabels:
      app: rollout-ref-deployment
  workloadRef:                                 # Reference an existing Deployment using workloadRef field
    apiVersion: apps/v1
    kind: Deployment
    name: rollout-ref-deployment
  strategy:
    canary:
      steps:
        - setWeight: 20
        - pause: {duration: 10s}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/instance: rollout-canary
  name: rollout-ref-deployment
spec:
  replicas: 0                                  # Scale down existing deployment
  selector:
    matchLabels:
      app: rollout-ref-deployment
  template:
    metadata:
      labels:
        app: rollout-ref-deployment
    spec:
      containers:
        - name: rollouts-demo
          image: argoproj/rollouts-demo:blue
          imagePullPolicy: Always
          ports:
            - containerPort: 8080
```

Consider following if your Deployment runs in production:

### Running Rollout and Deployment side-by-side

After creation Rollout will spinup required number of Pods side-by-side with the Deployment Pods.
Rollout won't try to manage existing Deployment Pods. That means you can safely update add Rollout
to the production environment without any interruption but you are going to run twice more Pods during migration.

Argo-rollouts controller patches the spec of rollout object with an annotation of `rollout.argoproj.io/workload-generation`, which equals the generation of referenced deployment. Users can detect if the rollout matches desired generation of deployment by checking the `workloadObservedGeneration` in the rollout status.

### Traffic Management During Migration

The Rollout offers traffic management functionality that manages routing rules and flows the traffic to different
versions of an application. For example [Blue-Green](features/bluegreen.md) deployment strategy manipulates
Kubernetes Service selector and direct production traffic to "green" instances only.

If you are using this feature then Rollout switches production traffic to Pods that it manages. The switch happens
only when the required number of Pod is running and healthy so it is safe in production as well. However, if you
want to be extra careful then consider creating a temporal Service or Ingress object to validate Rollout behavior.
Once testing is done delete temporal Service/Ingress and switch rollout to production one.

# Migrating to Deployments

In case users want to rollback to the deployment kinds from rollouts, there are two scenarios aligned with those in [Migrating to Rollouts](#migrating-to-rollouts).

* Convert a Rollout resource to a Deployment resource.
* Reference an existing Deployment from a Rollout using `workloadRef` field.

## Convert Rollout to Deployment

When converting a Rollout to a Deployment, it involves changing three fields:

1. Changing the apiVersion from  argoproj.io/v1alpha1 to apps/v1
1. Changing the kind from Rollout to Deployment
1. Remove the rollout strategy in `spec.strategy.canary` or ``spec.strategy.blueGreen``


!!! warning
    When migrating a Rollout which is already serving live production traffic, a Deployment should
    run next to the rollout before deleting the rollout or scaling down the rollout.
    **Not following this approach might result in downtime**. It also allows for the Deployment to be
    tested before deleting the original Rollout.

## Reference Deployment From Rollout

When a rollout is referencing to a deployment:

1. Scale-up an existing Deployment by changing its `replicas` field to a desired number of pods.
1. Wait for the Deployment pods to become Ready.
1. Scale-down an existing Rollout by changing its `replicas` field to zero.

Please refer to [Running Rollout and Deployment side-by-side](#running-rollout-and-deployment-side-by-side) and [Traffic Management During Migration](#traffic-management-during-migration) for caveats.
