
# Argo Rollouts - Progressive Delivery for Kubernetes
[![codecov](https://codecov.io/gh/argoproj/argo-rollouts/branch/master/graph/badge.svg)](https://codecov.io/gh/argoproj/argo-rollouts)
[![slack](https://img.shields.io/badge/slack-argoproj-brightgreen.svg?logo=slack)](https://argoproj.github.io/community/join-slack)

## What is Argo Rollouts?
Argo Rollouts is a Kubernetes controller and set of CRDs which provide advanced deployment capabilities such as blue-green, canary, canary analysis, experimentation, and progressive delivery features to Kubernetes. 

Argo Rollouts (optionally) integrates with ingress controllers and service meshes, leveraging their traffic shaping abilities to gradually shift traffic to the new version during an update. Additionally, Rollouts can query and interpret metrics from various providers to verify key KPIs and drive automated promotion or rollback during an update.

## Why Argo Rollouts?
Kubernetes Deployments provides the `RollingUpdate` strategy which provide a basic set of safety guarantees (readiness probes) during an update. However the rolling update strategy faces many limitations:
* Few controls over the speed of the rollout
* Inability to control traffic flow to the new version
* Readiness probes are unsuitable for deeper, stress, or one-time checks
* No ability to query external metrics to verify an update
* Can halt the progression, but unable to automatically abort and rollback the update

For these reasons, in large scale high-volume production environments, a rolling update is often considered too risky of an update procedure since it provides no control over the blast radius, may rollout too aggressively, and provides no automated rollback upon failures.

## Features
* Blue-Green (aka red-black) update strategy
* Canary update strategy
* Fine-grained, weighted traffic shifting
* Automated rollbacks and promotions
* Manual judgement
* Customizable metric queries and analysis of business KPIs
* Ingress controller integration: NGINX, ALB
* Service Mesh integration: Istio, Linkerd, SMI
* Metric provider integration: Prometheus, Wavefront, Kayenta, Web, Kubernetes Jobs

## Documentation
To learn more about Argo Rollouts go to the [complete documentation](https://argoproj.github.io/argo-rollouts/).

## Who uses Argo Rollouts?
Organizations below are **officially** using Argo Rollouts. Please send a PR with your organization name if you are using Argo Rollouts.

1. [ADP](https://www.adp.com)
1. [Intuit](https://www.intuit.com/)
1. [Nozzle](https://nozzle.io)
1. [PayPay](https://paypay.ne.jp/)
1. [Twilio SendGrid](https://sendgrid.com)
1. [Ubie](https://ubie.life/)
1. [VISITS Technologies](https://visits.world/en)
1. [Spotify](https://www.spotify.com/)
1. [New Relic](https://newrelic.com/)

## Community Blogs and Presentations
* [How Intuit Does Canary and Blue Green Deployments](https://www.youtube.com/watch?v=yeVkTTO9nOA)
* [Leveling Up Your CD: Unlocking Progressive Delivery on Kubernetes](https://www.youtube.com/watch?v=Nv0PPwbIEkY)
