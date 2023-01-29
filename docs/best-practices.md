# Best Practices

This document describes some best practices, tips and tricks when using Argo Rollouts.

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
apiVersion: networking.k8s.io/v1beta1
kind: Ingress
metadata:
  name: guestbook
spec:
  rules:
  # host rule to only reach the desired pods (aka canary/preview)
  - host: guestbook-desired.argoproj.io
    http:
      paths:
      - backend:
          serviceName: guestbook-desired
          servicePort: 443
        path: /*
  # host rule to only reach the stable pods
  - host: guestbook-stable.argoproj.io
    http:
      paths:
      - backend:
          serviceName: guestbook-stable
          servicePort: 443
        path: /*
  # default rule which omits host, and will split traffic between desired vs. stable
  - http:
      paths:
      - backend:
          serviceName: guestbook-root
          servicePort: 443
        path: /*
```

The above technique has the a benefit in that it would not incur additional cost of allocating
additional load balancers.

## Reducing operator memory usage

On clusters with thousands of rollouts memory usage for the argo-rollouts
operator can be reduced significantly by changing RevisionHistoryLimit from the
default of 10 to a lower number. One user of Argo Rollouts saw a 27% reduction
in memory usage for a cluster with 1290 rollouts by changing
RevisionHistoryLimit from 10 to 0.

