
# Argo Rollouts - Progressive Delivery for Kubernetes
[![codecov](https://codecov.io/gh/argoproj/argo-rollouts/branch/master/graph/badge.svg)](https://codecov.io/gh/argoproj/argo-rollouts)
[![slack](https://img.shields.io/badge/slack-argoproj-brightgreen.svg?logo=slack)](https://argoproj.github.io/community/join-slack)
[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/3834/badge)](https://bestpractices.coreinfrastructure.org/projects/3834)

## What is Argo Rollouts?
Argo Rollouts is a Kubernetes controller and set of CRDs which provide advanced deployment capabilities such as blue-green, canary, canary analysis, experimentation, and progressive delivery features to Kubernetes. 

Argo Rollouts (optionally) integrates with ingress controllers and service meshes, leveraging their traffic shaping abilities to gradually shift traffic to the new version during an update. Additionally, Rollouts can query and interpret metrics from various providers to verify key KPIs and drive automated promotion or rollback during an update.

[![Argo Rollotus Demo](https://img.youtube.com/vi/hIL0E2gLkf8/0.jpg)](https://youtu.be/hIL0E2gLkf8)

## Quick Start

```
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts -f https://github.com/argoproj/argo-rollouts/releases/latest/download/install.yaml
```

Follow the full [getting started guide](docs/getting-started.md) to walk through creating and then updating a rollout object. 

## Why Argo Rollouts?
Kubernetes Deployments provides the `RollingUpdate` strategy which provide a basic set of safety guarantees (readiness probes) during an update. However the rolling update strategy faces many limitations:
* Few controls over the speed of the rollout
* Inability to control traffic flow to the new version
* Readiness probes are unsuitable for deeper, stress, or one-time checks
* No ability to query external metrics to verify an update
* Can halt the progression, but unable to automatically abort and rollback the update

For these reasons, in large scale high-volume production environments, a rolling update is often considered too risky of an update procedure since it provides no control over the blast radius, may rollout too aggressively, and provides no automated rollback upon failures.

## Features
* Blue-Green update strategy
* Canary update strategy
* Fine-grained, weighted traffic shifting
* Automated rollbacks and promotions
* Manual judgement
* Customizable metric queries and analysis of business KPIs
* Ingress controller integration: NGINX, ALB
* Service Mesh integration: Istio, Linkerd, SMI
* Metric provider integration: Prometheus, Wavefront, Kayenta, Web, Kubernetes Jobs, Datadog, New Relic

## Documentation
To learn more about Argo Rollouts go to the [complete documentation](https://argoproj.github.io/argo-rollouts/).

## Who uses Argo Rollouts?

[Official Argo Rollouts User List](https://github.com/argoproj/argo-rollouts/blob/master/USERS.md)

## Community Blogs and Presentations

* [Automation of Everything - How To Combine Argo Events, Workflows & Pipelines, CD, and Rollouts](https://youtu.be/XNXJtxkUKeY)
* [Argo Rollouts - Canary Deployments Made Easy In Kubernetes](https://youtu.be/84Ky0aPbHvY)
* [How Intuit Does Canary and Blue Green Deployments](https://www.youtube.com/watch?v=yeVkTTO9nOA)
* [Leveling Up Your CD: Unlocking Progressive Delivery on Kubernetes](https://www.youtube.com/watch?v=Nv0PPwbIEkY)
* [Minimize failed deployments with Argo Rollouts and Smoke tests](https://codefresh.io/continuous-deployment/minimize-failed-deployments-argo-rollouts-smoke-tests/)
* [Recover automatically from failed deployments with Argo Rollouts and Prometheus metrics](https://codefresh.io/continuous-deployment/recover-automatically-from-failed-deployments/)
* [Kubernetes Blue-Green deployments with Argo Rollouts](https://www.youtube.com/watch?v=krDxDz4V4Tg)
* [Kubernetes canary deployments with Argo Rollouts](https://www.youtube.com/watch?v=fviYWA2mcF8)
* [GitOps with Argo CD and an Argo Rollouts canary release](https://www.youtube.com/watch?v=35Qimb_AZ8U)
* [Multi-Stage Delivery with Keptn and Argo Rollouts](https://www.youtube.com/watch?v=w-E8FzTbN3g&t=1s)
* [Gradual Code Releases Using an In-House Kubernetes Canary Controller on top of Argo Rollouts](https://doordash.engineering/2021/04/14/gradual-code-releases-using-an-in-house-kubernetes-canary-controller/)


