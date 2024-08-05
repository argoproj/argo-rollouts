# Best Practices

This document describes some best practices, tips and tricks when using Argo Rollouts. Be sure to read the [FAQ page](../FAQ) as well.


## Check application compatibility

Argo Rollouts is a great solution for applications that your team is deploying in a continuous manner (and you have access to the source code). Before using Argo Rollouts you need to contact the developers of the application and verify that you can indeed run multiple versions of the same application at the same time. 

Not all applications can work with Argo Rollouts. Applications that use shared resources (e.g. writing to a shared file) will have issues, and "worker" type applications (that load data from queues) will rarely work ok without source code modifications.

Note that using Argo Rollouts for "infrastructure" applications such as cert-manager, nginx, coredns, sealed-secrets etc is **NOT** recommended.

## Understand the scope of Argo Rollouts

Currently Argo Rollouts works with a single Kubernetes deployment/application and within a single cluster only. You also need to have the controller deployed on *every* cluster where a Rollout is running if have more than one clusters using Rollout workloads.

If you want to look at multiple-services on multiple clusters
see discussion at issues [2737](https://github.com/argoproj/argo-rollouts/issues/2737), [451](https://github.com/argoproj/argo-rollouts/issues/451) and [2088](https://github.com/argoproj/argo-rollouts/issues/2088).


Note also that Argo Rollouts is a self-contained solution. It doesn't need Argo CD or any other Argo project to work.

## Understand your use case

Argo Rollouts is perfect for all progressive delivery scenarios as explained in [the concepts page](../concepts).

You should *NOT* use Argo Rollouts for preview/ephemeral environments. For that use case check the [Argo CD Pull Request generator](https://argo-cd.readthedocs.io/en/stable/operator-manual/applicationset/Generators-Pull-Request/).

The recommended way to use Argo Rollouts is for brief deployments that take 15-20 minutes or maximum 1-2 hours. If you want to run new versions for days or weeks before deciding to promote, then Argo Rollouts is probably not the best solution for you.

Keeping parallel releases for long times, complicates the deployment process a lot and opens several questions where different people have different views on how Argo Rollouts should work.

For example let's say that you are testing for a week version 1.3 as stable and 1.4 as preview.
Then somebody deploys 1.5

1. Some people believe that the new state should be 1.3 stable and 1.5 as preview
1. Some people believe that the new state should be 1.4 stable and 1.5 as preview

Currently Argo Rollouts follows the first approach, under the assumption that something was really wrong with 1.4 and 1.5 is the hotfix. 

And then let's say that 1.5 has an issue. Some people believe that Argo rollouts should "rollback" to 1.3 while other people think it should rollback to 1.4

Currently Argo Rollouts assumes that the version to rollback is always 1.3 regardless of how many "hotfixes" have been previewed in-between.

All these problems are not present if you make the assumption that each release stays active only for a minimal time and you always create one new version when the previous one has finished.

Also, if you want to run a wave of multiple versions at the same time (i.e. have 1.1 and 1.2 and 1.3 running at the same time), know that Argo Rollouts was not designed for this scenario. Argo Rollouts always works with the assumption that there is one stable/previous version and one preview/next version.

A version that has just been promoted is assumed to be ready for production and has already passed all your tests (either manual or automated).

## Prepare your metrics

The end-goal for using Argo Rollouts is to have **fully automated** deployments that also include rollbacks when needed.

While Argo Rollouts supports manual promotions and other manual pauses, these are best used for experimentation and test reasons.

Ideally you should have proper metrics that tell you in 5-15 minutes if a deployment is successful or not. If you don't have those metrics, then you will miss a lot of value from Argo Rollouts.

If you are doing a deployment right now and then have an actual human looking at logs/metrics/traces for the next 2 hours, adopting Argo Rollouts is not going to help you a lot with automated deployments.

Get your [metrics](../features/analysis) in place first and test them with dry-runs before applying them to production deployments.


## There is no "Argo Rollouts API"

A lot of people want to find an official API for managing Rollouts. There isn't any separate Argo Rollouts API. You can always use the Kubernetes API and patching of resources if you want to control a rollout.

But as explained in the previous point the end goal should be fully automated deployments without you having to tell Argo Rollouts to promote or abort.

## Integrating with other systems and processes

There are two main ways to integrate other systems with Argo Rollouts.

The easiest way is to use [Notifications](../features/notifications). This means that when a rollout is finished/aborted you send a notification to another system that does other tasks that you want to happen.

Alternatively you can control Rollouts with the CLI or by patching manually the Kubernetes resources.


## Use the Kubernetes Downward API

If you want your applications to know if they are part of a canary or not, you can use [Ephemeral labels](../features/ephemeral-metadata) along with the [Kubernetes downward api](https://kubernetes.io/docs/concepts/workloads/pods/downward-api/).

This means that your application will read from files its configuration in a dynamic manner and adapt according to the situation.



## Ingress desired/stable host routes

For various reasons, it is often desired that external services are able to reach the
desired pods (aka canary/preview) or stable pods specifically, without the possibility of traffic
arbitrarily being split between the two versions. Some use cases include:

* The new version of the service is able to be reach internally/privately (e.g. for manual verification),
  before exposing it externally.
* An external CI/CD pipeline runs tests against the blue-green preview stack before it is
  promoted to production.
* Running tests which compare the behavior of old version against the new version.

If you are using an Ingress to route traffic to the service, additional host rules can be added
to the ingress rules so that it is possible to specifically reach to the desired (canary/preview)
pods or stable pods.

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: guestbook
spec:
  rules:
  # host rule to only reach the desired pods (aka canary/preview)
  - host: guestbook-desired.argoproj.io
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: guestbook-desired
            port:
              number: 443

  # host rule to only reach the stable pods
  - host: guestbook-stable.argoproj.io
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: guestbook-stable
            port:
              number: 443

  # default rule which omits host, and will split traffic between desired vs. stable
  - http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: guestbook-root
            port:
            number: 443
```

The above technique has the a benefit in that it would not incur additional cost of allocating
additional load balancers.

## Reducing operator memory usage

On clusters with thousands of rollouts memory usage for the argo-rollouts
controller can be reduced significantly by changing the `RevisionHistoryLimit` property from the
default of 10 to a lower number. 

One user of Argo Rollouts saw a 27% reduction
in memory usage for a cluster with 1290 rollouts by changing
`RevisionHistoryLimit` from 10 to 0.


## Rollout a ConfigMap change

Argo Rollouts is meant to work on a Kubernetes Deployment. When a ConfigMap is mounted inside one the Deployment container and a change occurs inside the ConfigMap, it won't trigger a new Rollout by default.

One technique to trigger the Rollout it to name dynamically the ConfigMap.
For example, adding a hash of its content at the end of the name:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config-7270e14e6
```

Each time a change occurs in the ConfigMap, its name will change in the Deployment reference as well, triggering a Rollout.

However, it's not enough to perform correctly progressive rollouts, as the old ConfigMap might get deleted as soon as the new one is created. This would prevent Experiments and rollbacks in case of rollout failure to work correctly.

While no magical solution exist to work aroud that, you can tweak your deployment tool to remove the ConfigMap only when the Rollout is completed successfully.

Example with Argo CD:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config-7270e14e6
  annotations:
    argocd.argoproj.io/sync-options: PruneLast=true
```
