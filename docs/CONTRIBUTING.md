# Contributing
## Before You Start
Argo Rollouts is written in Golang. If you do not have a good grounding in Go, try out [the tutorial](https://tour.golang.org/).

## Pre-requisites
Install:

* [docker](https://docs.docker.com/install/#supported-platforms)
* [golang](https://golang.org/)
* [kubectl](https://kubernetes.io/docs/tasks/tools/install-kubectl/)
* [kustomize](https://github.com/kubernetes-sigs/kustomize/releases)
* [minikube](https://kubernetes.io/docs/setup/minikube/) or Docker for Desktop

Argo Rollout additionally uses `golangci-lint` to lint the project.

Run the following commands to install them:
```bash
go get -u github.com/golangci/golangci-lint/cmd/golangci-lint
```


Brew users can quickly install the lot:
    
```bash
brew install go kubectl kustomize
```

Set up environment variables (e.g. is `~/.bashrc`):

```bash
export GOPATH=~/go
export PATH=$PATH:$GOPATH/bin
```

Checkout the code:

```bash
go get -u github.com/argoproj/argo-rollouts
cd ~/go/src/github.com/argoproj/argo-rollouts
```

Run the following command to download all the dependencies:
```
go mod download
```


## Building

`go.mod` is used, so the `go build/test` commands automatically install the needed dependencies


The `make controller` command will build the controller.

* `make codegen` - Runs the code generator that creates the informers, client, lister, and deepcopies from the types.go 
and modifies the open-api spec. This command fails if the user has not run `go mod download` to download all the 
dependencies of the project. 


## Running Tests

To run unit tests:

```bash
make test
```


## Running Locally

It is much easier to run and debug if you run Argo Rollout in your local machine than in the Kubernetes cluster.

```bash
cd ~/go/src/github.com/argoproj/argo-rollouts
make controller
./dist/rollouts-controller
```

## Running Local Containers

You may need to run containers locally, so here's how:

Create login to Docker Hub, then login.

```bash
docker login
```

Add your username as the environment variable, e.g. to your `~/.bash_profile`:

```bash
export IMAGE_NAMESPACE=argoproj
```

Build the images:

```bash
DOCKER_PUSH=true make image
```

Update the manifests:

```bash
make manifests
```

Install the manifests:

```bash
kubectl -n argo-rollouts apply -f manifests/install.yaml
```

## Upgrading Kubernetes Libraries
Argo Rollouts has a dependency on the kubernetes/kubernetes repo for some of the functionality that has not been 
pushed into the other kubernetes repositories yet. In order to import the kubernetes/kubernetes repo, all of the 
associated repos have to pinned to the correct version specified by the kubernetes/kubernetes release. The 
`./hack/update-k8s-dependencies.sh` updates all the dependencies to the those correct versions.

## Documentation Changes

Modify contents in `docs/` directory. 

Preview changes in your browser by visiting http://localhost:8000 after running:

```shell
make serve-docs
```

To publish changes, run:

```shell
make release-docs
```
