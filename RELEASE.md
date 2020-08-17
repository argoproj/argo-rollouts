# Releasing

1. Make sure you are logged into Docker Hub:

    ```bash
    docker login
    ```

1. Make a PR with new changelog in CHANGELOG.md and new version in VERSION.

1. Once approved and merged, export the upstream repository, version and branch name, e.g.:

    ```bash
    REPO=upstream ;# or origin 
    BRANCH=release-v0.8 # should not include the patch version
    VERSION=v$(cat VERSION)
    ```



1. Create or checkout release branch:

    For major/minor releases:
    ```bash
    git checkout -b $BRANCH
    ```
    For patch releases:
    ```bash
    git checkout $BRANCH
    git cherry-pick <commits shas for release>
    ```
    
1. Upgrade the manifests to the new version
    ```
    make IMAGE_TAG=$VERSION manifests
    git commit -am "Update manifests to $VERSION"
    git push $REPO $BRANCH
    ```

1. Create a [Github releases](https://github.com/argoproj/argo-rollouts/releases) pointing at the release branch with the following content:
   * Getting started (copy from previous release and new version)
   * Changelog

1. Build and push release to Docker Hub

    ```bash
    git fetch --all
    git clean -fd
    make IMAGE_NAMESPACE=argoproj IMAGE_TAG=$VERSION DOCKER_PUSH=true release
    ```

1. Upload `./dist/kubectl-argo-rollouts-{darwin/linux}` binaries to the new Github release.


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
    kubectl apply -n argo-rollouts -f https://raw.githubusercontent.com/argoproj/argo-rollouts/$VERSION/manifests/install.yaml
    ```


1. Check the Kubectl Argo Rollout plugin:
    ```bash
    brew upgrade kubectl-argo-rollouts
    kubectl argo rollouts version
    ```
