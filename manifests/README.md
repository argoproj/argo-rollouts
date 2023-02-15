# Argo Rollouts Installation Manifests

* [install.yaml](install.yaml) - Standard installation method. This will create a new namespace, `argo-rollouts`,
  where Argo Rollouts controller will run.

* [namespace-install.yaml](namespace-install.yaml) - Installation which requires only namespace level privileges.

  > Note: Argo Rollouts CRDs are not included into [namespace-install.yaml](namespace-install.yaml).
  > and have to be installed separately. The CRD manifests are located in [manifests/crds](./crds) directory.
  > Use the following command to install them:
  > ```bash
  > kubectl apply -k https://github.com/argoproj/argo-rollouts/manifests/crds\?ref\=stable
  > ```
