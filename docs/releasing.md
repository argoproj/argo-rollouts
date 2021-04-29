# Releasing

1. Tag a commit to release from using semantic versioning (e.g. v1.0.0-rc1)

1. Visit the [Release GitHub Action](https://github.com/argoproj/argo-rollouts/actions/workflows/release.yaml)
   and enter the tag.

[![GitHub Release Action](release-action.png)](release-action.png)

1. When the action completes, visit the generated draft [Github releases](https://github.com/argoproj/argo-rollouts/releases) and enter the details about the release:
   * Getting started (copy from previous release and new version)
   * Changelog

1. Update `stable` tag:

    ```bash
    git tag stable --force && git push $REPO stable --force
    ```

1. Update Brew formula:

    ```bash
    git clone git@github.com:argoproj/homebrew-tap.git
    cd homebrew-tap
    git pull
    ./update.sh kubectl-argo-rollouts $VERSION
    git commit -am "Update kubectl-argo-rollouts to $VERSION"
    git push
    ```

### Verify

1. Install locally using the command below and follow the [Getting Started Guide](https://argoproj.github.io/argo-rollouts/getting-started/):

    ```bash
    kubectl apply -n argo-rollouts -f https://github.com/argoproj/argo-rollouts/releases/download/${VERSION}/install.yaml
    ```


1. Check the Kubectl Argo Rollout plugin:
    ```bash
    brew upgrade kubectl-argo-rollouts
    kubectl argo rollouts version
    ```
