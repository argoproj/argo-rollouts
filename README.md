
# Argo Rollouts - Progressive Delivery for Kubernetes

[![codecov](https://codecov.io/gh/argoproj/argo-rollouts/branch/master/graph/badge.svg)](https://codecov.io/gh/argoproj/argo-rollouts)
[![slack](https://img.shields.io/badge/slack-argoproj-brightgreen.svg?logo=slack)](https://argoproj.github.io/community/join-slack)
[![CII Best Practices](https://bestpractices.coreinfrastructure.org/projects/3834/badge)](https://bestpractices.coreinfrastructure.org/projects/3834)
[![Artifact HUB](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/argo-rollouts)](https://artifacthub.io/packages/helm/argo/argo-rollouts)

## What is Argo Rollouts?

Argo Rollouts is a Kubernetes controller and set of CRDs which provide advanced deployment capabilities such as blue-green, canary, canary analysis, experimentation, and progressive delivery features to Kubernetes.

Argo Rollouts (optionally) integrates with ingress controllers and service meshes, leveraging their traffic shaping abilities to gradually shift traffic to the new version during an update. Additionally, Rollouts can query and interpret metrics from various providers to verify key KPIs and drive automated promotion or rollback during an update.

[![Argo Rollotus Demo](https://img.youtube.com/vi/hIL0E2gLkf8/0.jpg)](https://youtu.be/hIL0E2gLkf8)

## Quick Start

```bash
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
* Ingress controller integration: NGINX, ALB, Apache APISIX
* Service Mesh integration: Istio, Linkerd, SMI
* Metric provider integration: Prometheus, Wavefront, Kayenta, Web, Kubernetes Jobs, Datadog, New Relic, InfluxDB

## Supported Traffic Shaping Integrations
| Traffic Shaping Integration       | SetWeight                    | SetWeightExperiments        | SetMirror                  | SetHeader                  |
|-----------------------------------|------------------------------|-----------------------------|----------------------------|----------------------------|
| ALB Ingress Controller            | :white_check_mark: (stable)  | :white_check_mark: (stable) | :x:                        | :white_check_mark: (alpha) |
| Ambassador                        | :white_check_mark: (stable)  | :x:                         | :x:                        | :x:                        |
| Apache APISIX Ingress Controller  | :white_check_mark: (alpha)   | :x:                         | :x:                        | :white_check_mark: (alpha)                        |
| Istio                             | :white_check_mark: (stable)  | :white_check_mark: (stable) | :white_check_mark: (alpha) | :white_check_mark: (alpha) |
| Nginx Ingress Controller          | :white_check_mark: (stable)  | :x:                         | :x:                        | :x:                        |
| SMI                               | :white_check_mark: (stable)  | :white_check_mark: (stable) | :x:                        | :x:                        |
| Traefik                           | :white_check_mark: (beta)    | :x:                         | :x:                        | :x:                        |

:white_check_mark: = Supported

:x: = Not Supported

## Documentation

To learn more about Argo Rollouts go to the [complete documentation](https://argo-rollouts.readthedocs.io/en/stable/).

## Community

You can reach the Argo Rollouts community and developers via the following channels:

* Q & A: [Github Discussions](https://github.com/argoproj/argo-rollouts/discussions)
* Chat: [The #argo-rollouts Slack channel](https://argoproj.github.io/community/join-slack)
* Contributors Office Hours: [Every Thursday](https://calendar.google.com/calendar/u/0/embed?src=argoproj@gmail.com) | [Agenda](https://docs.google.com/document/d/1xkoFkVviB70YBzSEa4bDnu-rUZ1sIFtwKKG1Uw8XsY8)
* User Community meeting: [First Wednesday of each month](https://calendar.google.com/calendar/u/0/embed?src=argoproj@gmail.com) | [Agenda](https://docs.google.com/document/d/1ttgw98MO45Dq7ZUHpIiOIEfbyeitKHNfMjbY5dLLMKQ)

## Who uses Argo Rollouts?

[Official Argo Rollouts User List](https://github.com/argoproj/argo-rollouts/blob/master/USERS.md)

## Community Blogs and Presentations

* [Awesome-Argo: A Curated List of Awesome Projects and Resources Related to Argo](https://github.com/terrytangyuan/awesome-argo)
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
* [How Scalable is Argo-Rollouts: A Cloud Operator’s Perspective](https://www.youtube.com/watch?v=rCEhxJ2NSTI)
* [Minimize Impact in Kubernetes Using Argo Rollouts](https://medium.com/@arielsimhon/minimize-impact-in-kubernetes-using-argo-rollouts-992fb9519969)
* [Progressive Application Delivery with GitOps on Red Hat OpenShift](https://www.youtube.com/watch?v=DfeL7cdTx4c)
