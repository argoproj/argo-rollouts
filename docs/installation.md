# Installation

## Controller Installation

```bash
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts -f https://github.com/argoproj/argo-rollouts/releases/latest/download/install.yaml
```

This will create a new namespace, `argo-rollouts`, where Argo Rollouts controller will run.

!!! tip
    When installing Argo Rollouts on Kubernetes v1.14 or lower, the CRD manifests must be kubectl applied with the --validate=false option. This is caused by use of new CRD fields introduced in v1.15, which are rejected by default in lower API servers.


!!! tip 
    On GKE, you will need grant your account the ability to create new cluster roles:

    ```shell
    kubectl create clusterrolebinding YOURNAME-cluster-admin-binding --clusterrole=cluster-admin --user=YOUREMAIL@gmail.com
    ```

## Kubectl Plugin Installation

The kubectl plugin is optional, but is convenient for managing and visualizing rollouts from the 
command line.

### Brew

```shell
brew install argoproj/tap/kubectl-argo-rollouts
```

### Manual

1. Install [Argo Rollouts Kubectl plugin](https://github.com/argoproj/argo-rollouts/releases) with curl.
    ```shell
    curl -LO https://github.com/argoproj/argo-rollouts/releases/latest/download/kubectl-argo-rollouts-darwin-amd64
    ```

    !!! tip "" 
        For Linux dist, replace `darwin` with `linux`

1. Make the kubectl-argo-rollouts binary executable.

    ```shell
    chmod +x ./kubectl-argo-rollouts-darwin-amd64
    ```

1. Move the binary into your PATH.

    ```shell
    sudo mv ./kubectl-argo-rollouts-darwin-amd64 /usr/local/bin/kubectl-argo-rollouts
    ```

Test to ensure the version you installed is up-to-date:

```shell
kubectl argo rollouts version
```

## Using the CLI  with Docker

The CLI is also available as a container image at [https://quay.io/repository/argoproj/kubectl-argo-rollouts](https://quay.io/repository/argoproj/kubectl-argo-rollouts)

You can run it like any other Docker image or use it in any CI platform that supports Docker images.

```shell
docker run quay.io/argoproj/kubectl-argo-rollouts:master version
```

