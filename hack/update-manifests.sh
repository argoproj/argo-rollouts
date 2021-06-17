#!/bin/sh

set -x
set -e

SRCROOT="$( CDPATH='' cd -- "$(dirname "$0")/.." && pwd -P )"
AUTOGENMSG="# This is an auto-generated file. DO NOT EDIT"

if [ ! -z "${IMAGE_NAMESPACE}" ]; then
  SET_IMAGE_NAMESPACE="=${IMAGE_NAMESPACE}"
fi

if [ ! -z "${IMAGE_TAG}" ]; then
  SET_IMAGE_TAG=":${IMAGE_TAG}"
fi

if [ ! -z "${SET_IMAGE_NAMESPACE}" ] || [ ! -z "${SET_IMAGE_TAG}" ]; then
  (cd ${SRCROOT}/manifests/base && kustomize edit set image quay.io/argoproj/argo-rollouts${SET_IMAGE_NAMESPACE}${SET_IMAGE_TAG})
  (cd ${SRCROOT}/manifests/dashboard-install && kustomize edit set image quay.io/argoproj/kubectl-argo-rollouts${SET_IMAGE_NAMESPACE}${SET_IMAGE_TAG})
fi

kust_cmd="kustomize build --load-restrictor LoadRestrictionsNone"
echo "${AUTOGENMSG}" > "${SRCROOT}/manifests/install.yaml"
${kust_cmd} "${SRCROOT}/manifests/cluster-install" >> "${SRCROOT}/manifests/install.yaml"

echo "${AUTOGENMSG}" > "${SRCROOT}/manifests/namespace-install.yaml"
${kust_cmd} "${SRCROOT}/manifests/namespace-install" >> "${SRCROOT}/manifests/namespace-install.yaml"

echo "${AUTOGENMSG}" > "${SRCROOT}/manifests/dashboard-install.yaml"
${kust_cmd} "${SRCROOT}/manifests/dashboard-install" >> "${SRCROOT}/manifests/dashboard-install.yaml"

echo "${AUTOGENMSG}" > "${SRCROOT}/manifests/notifications-install.yaml"
${kust_cmd} "${SRCROOT}/manifests/notifications" >> "${SRCROOT}/manifests/notifications-install.yaml"
