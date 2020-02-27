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

Argo Rollout additionally uses
* `controller-gen` binary in order to auto-generate the crd manifest
* `golangci-lint` to lint the project.

Run the following commands to install them:
```bash
go get -u github.com/kubernetes-sigs/controller-tools/cmd/controller-gen
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

## Building

`go.mod` is used, so the `go build/test` commands automatically install the needed dependencies


The `make controller` command will build the controller.

* `make codegen` - Runs the code generator that creates the informers, client, lister, and deepcopies from the types.go and modifies the open-api spec.


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

## Documentation Changes
If you need to run the mkdocs server, you will need to do the following:

* Follow the instruction guide to install [mkDocs](https://www.mkdocs.org/#installation)
* Install the `material` theme with the [following guide](https://squidfunk.github.io/mkdocs-material/#quick-start)

Afterwards, you can run `mkdocs serve` and access your documentation at [http://127.0.0.1:8000/](http://127.0.0.1:8000/)

If you don't want to setup mkDocs locally, the following docker command should suffice:

```shell
docker run --rm -it -p 8000:8000 -v ${PWD}:/docs squidfunk/mkdocs-material
```
