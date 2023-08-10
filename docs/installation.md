# Installation

## Controller Installation

Two types of installation:

* [install.yaml](https://github.com/argoproj/argo-rollouts/blob/master/manifests/install.yaml) - Standard installation method.
```bash
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts -f https://github.com/argoproj/argo-rollouts/releases/latest/download/install.yaml
```

This will create a new namespace, `argo-rollouts`, where Argo Rollouts controller will run.

!!! tip
    If you are using another namespace name, please update `install.yaml` clusterrolebinding's serviceaccount namespace name.

!!! tip
    When installing Argo Rollouts on Kubernetes v1.14 or lower, the CRD manifests must be kubectl applied with the --validate=false option. This is caused by use of new CRD fields introduced in v1.15, which are rejected by default in lower API servers.


!!! tip
    On GKE, you will need grant your account the ability to create new cluster roles:

    ```shell
    kubectl create clusterrolebinding YOURNAME-cluster-admin-binding --clusterrole=cluster-admin --user=YOUREMAIL@gmail.com
    ```

* [namespace-install.yaml](https://github.com/argoproj/argo-rollouts/blob/master/manifests/namespace-install.yaml) - Installation of Argo Rollouts which requires
only namespace level privileges. An example usage of this installation method would be to run several Argo Rollouts controller instances in different namespaces
on the same cluster.

  > Note: Argo Rollouts CRDs are not included into [namespace-install.yaml](https://github.com/argoproj/argo-rollouts/blob/master/manifests/namespace-install.yaml).
  > and have to be installed separately. The CRD manifests are located in [manifests/crds](https://github.com/argoproj/argo-rollouts/blob/master/manifests/crds) directory.
  > Use the following command to install them:
  > ```bash
  > kubectl apply -k https://github.com/argoproj/argo-rollouts/manifests/crds\?ref\=stable
  > ```

You can find released container images of the controller at [Quay.io](https://quay.io/repository/argoproj/argo-rollouts?tab=tags). There are also old releases
at Dockerhub, but since the introduction of rate limiting, the Argo project has moved to Quay.

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

## Shell auto completion

The CLI can export shell completion code for several shells.

For bash, ensure you have bash completions installed and enabled. To access completions in your current shell, run $ `source <(kubectl-argo-rollouts completion bash)`. Alternatively, write it to a file and source in `.bash_profile`.

The completion command supports bash, zsh, fish and powershell.

See the [completion command documentation](./generated/kubectl-argo-rollouts/kubectl-argo-rollouts_completion.md) for more details.


## Using the CLI  with Docker

The CLI is also available as a container image at [https://quay.io/repository/argoproj/kubectl-argo-rollouts](https://quay.io/repository/argoproj/kubectl-argo-rollouts)

You can run it like any other Docker image or use it in any CI platform that supports Docker images.

```shell
docker run quay.io/argoproj/kubectl-argo-rollouts:master version
```

## Supported versions

Check [e2e testing file]( https://github.com/argoproj/argo-rollouts/blob/master/.github/workflows/e2e.yaml#L40-L44) to see what the Kubernetes version is being fully tested.

You can switch to different tags to see what relevant Kubernetes versions were being tested for the respective version.

## Upgrading Argo Rollouts

Argo Rollouts is a Kubernetes controller that doesn't hold any external state. It is active
only when deployments are actually happening.

To upgrade Argo Rollouts:

1. Try to find a time period when no deployments are happening
2. Delete the previous version of the controller and apply/install the new one
3. When a new Rollout takes place the new controller will be activated.

If deployments are happening while you upgrade the controller, then you shouldn't
have any downtime. Current Rollouts will be paused and as soon as the new controller becomes
active it will resume all in-flight deployments.

