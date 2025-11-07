---
title: Parameterized Build system
authors:
  - '@kostis-codefresh'
creation-date: 2025-06-24
---

# Parameterized Build system

The build system of Argo Rollouts is currently presenting several challenges for companies that want to keep 
internal forks.

This document provides a proposal for making the build system of Argo Rollouts more flexible.

## Summary

There is a need for companies to have an internal fork of Argo Rollouts. This fork needs to follow the upstream project (in order to get new features) but at the same time
allow

- quick security fixes 
- testing of upcoming features before releasing them to the upstream OSS fork
- integration with internal systems that are not relevant to the OSS fork.

## Motivation

Currently the [build system of Argo Rollouts](https://github.com/argoproj/argo-rollouts/tree/master/.github/workflows) has several hardcoded parameters. These include

- The Docker registry for the project is hardcoded to [quay.io/argoproj/kubectl-argo-rollouts](https://github.com/argoproj/argo-rollouts/blob/master/.github/workflows/docker-publish.yml#L44)
- There are mentions on which Golang version to use in [several](https://github.com/argoproj/argo-rollouts/blob/master/.github/workflows/testing.yaml#L96) [different](https://github.com/argoproj/argo-rollouts/blob/master/.github/workflows/docker-publish.yml#L70) [places](https://github.com/argoproj/argo-rollouts/blob/master/.github/workflows/go.yml#L12).
- The Kubernetes API versions of E2E tests [are an inline list](https://github.com/argoproj/argo-rollouts/blob/master/.github/workflows/testing.yaml#L82)

This makes having a internal fork a more difficult process than needed because any custom changes that happen internally require extra effort if

- they never need to be sent to the upstream project 
- they need to be compared with the upstream project (3-way diff)
- they need to be pinned/kept back against the upstream project



## Goals

The goals of this proposal are:

- Detect all places in the build system that have hardcoded values
- Make the different build system configurations a parameter
- Setup default values for the main OSS project
- Allow external organizations to maintain internal forks with minimal effort


## Use cases

Here are some example use cases

### Basic build/push

An external organization should be able to fork the OSS project and push the final image to their own registry instead of the default `quay.io/argoproj`

### Internal security fix

A critical vulnerability has been found and the organization needs to provide a hotfix in the internal fork. The fix could be in

- a standard library of Argo Rollouts
- the version of GoLang
- The version of Kubernetes client
- Any combination of the above.

A developer should be able to apply this fix in the internal fork, and then at the same time send the fix to the upstream OSS project. The assumption is that the OSS
project might not get the fix as fast as the internal fork, so there is a brief time window where the security fix is only in the internal fork while there is still the need
to get new features from the OSS project.

### Internal feature implementation

Same scenario as the security fix but this time code changes in the actual controller are included.

### Permanent changes only for the internal fork

The most complex scenario (and the one presenting more challenges today) is for changes that do not go to the upstream fork and need to stay
in the internal fork for a larger time period.

Examples are

- Using an older Golang version
- Supporting an older version of the Kubernetes client library
- Using an internal fork for a dependency library that will never be OSS


## Security Considerations

There is no impact for the security of the project. In fact, this proposal will allow security conscious organizations to ship security fixes much faster
and in smaller batches to the OSS project.

## Risks and Mitigations

There is no risk in the main project as all changes will happen on the build system. The end result for the main project will be exactly the same
(using default values or having implied configuration files).


