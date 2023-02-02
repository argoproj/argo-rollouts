PACKAGE=github.com/argoproj/argo-rollouts
CURRENT_DIR=$(shell pwd)
DIST_DIR=${CURRENT_DIR}/dist
PATH := $(DIST_DIR):$(PATH)
PLUGIN_CLI_NAME?=kubectl-argo-rollouts
TEST_TARGET ?= ./...

BUILD_DATE=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT=$(shell git rev-parse HEAD)
GIT_TAG=$(shell if [ -z "`git status --porcelain`" ]; then git describe --exact-match --tags HEAD 2>/dev/null; fi)
GIT_TREE_STATE=$(shell if [ -z "`git status --porcelain`" ]; then echo "clean" ; else echo "dirty"; fi)
GIT_REMOTE_REPO=upstream
VERSION=$(shell if [ ! -z "${GIT_TAG}" ] ; then echo "${GIT_TAG}" | sed -e "s/^v//"  ; else cat VERSION ; fi)

# docker image publishing options
DOCKER_PUSH=false
IMAGE_TAG=latest
# build development images
DEV_IMAGE ?= false

# E2E variables
E2E_INSTANCE_ID ?= argo-rollouts-e2e
E2E_TEST_OPTIONS ?= 
E2E_PARALLEL ?= 1
E2E_WAIT_TIMEOUT ?= 120
GOPATH ?= $(shell go env GOPATH)

override LDFLAGS += \
  -X ${PACKAGE}/utils/version.version=${VERSION} \
  -X ${PACKAGE}/utils/version.buildDate=${BUILD_DATE} \
  -X ${PACKAGE}/utils/version.gitCommit=${GIT_COMMIT} \
  -X ${PACKAGE}/utils/version.gitTreeState=${GIT_TREE_STATE}

ifneq (${GIT_TAG},)
IMAGE_TAG=${GIT_TAG}
override LDFLAGS += -X ${PACKAGE}.gitTag=${GIT_TAG}
endif

ifeq (${DOCKER_PUSH},true)
ifndef IMAGE_NAMESPACE
$(error IMAGE_NAMESPACE must be set to push images (e.g. IMAGE_NAMESPACE=quay.io/argoproj))
endif
endif

ifdef IMAGE_NAMESPACE
IMAGE_PREFIX=${IMAGE_NAMESPACE}/
endif

# protoc,my.proto
define protoc
	# protoc $(1)
    PATH=${DIST_DIR}:$$PATH protoc \
      -I /usr/local/include \
      -I ${DIST_DIR}/protoc-include \
      -I . \
      -I ./vendor \
      -I ${GOPATH}/src \
      -I ${GOPATH}/pkg/mod/github.com/gogo/protobuf@v1.3.2/gogoproto \
      -I ${GOPATH}/pkg/mod/github.com/grpc-ecosystem/grpc-gateway@v1.16.0/third_party/googleapis \
      --gogofast_out=plugins=grpc:${GOPATH}/src \
      --grpc-gateway_out=logtostderr=true:${GOPATH}/src \
      --swagger_out=logtostderr=true,fqn_for_swagger_name=true:. \
      $(1)
endef

.PHONY: all
all: controller image

# downloads vendor files needed by tools.go (i.e. go_install)
.PHONY: go-mod-vendor
go-mod-vendor:
	go mod tidy
	go mod vendor

.PHONY: install-go-tools-local
install-go-tools-local: go-mod-vendor
	./hack/installers/install-codegen-go-tools.sh

.PHONY: install-protoc-local
install-protoc-local:
	./hack/installers/install-protoc.sh

.PHONY: install-devtools-local
install-devtools-local:
	./hack/installers/install-dev-tools.sh

# Installs all tools required to build and test locally
.PHONY: install-tools-local
install-tools-local: install-go-tools-local install-protoc-local install-devtools-local

TYPES := $(shell find pkg/apis/rollouts/v1alpha1 -type f -name '*.go' -not -name openapi_generated.go -not -name '*generated*' -not -name '*test.go')
APIMACHINERY_PKGS=k8s.io/apimachinery/pkg/util/intstr,+k8s.io/apimachinery/pkg/api/resource,+k8s.io/apimachinery/pkg/runtime/schema,+k8s.io/apimachinery/pkg/runtime,k8s.io/apimachinery/pkg/apis/meta/v1,k8s.io/api/core/v1,k8s.io/api/batch/v1

.PHONY: install-toolchain
install-toolchain: install-go-tools-local install-protoc-local

# generates all auto-generated code
.PHONY: codegen
codegen: go-mod-vendor gen-proto gen-k8scodegen gen-openapi gen-mocks gen-crd manifests docs

# generates all files related to proto files
.PHONY: gen-proto
gen-proto: k8s-proto api-proto ui-proto

# generates the .proto files affected by changes to types.go
.PHONY: k8s-proto
k8s-proto: go-mod-vendor $(TYPES)
	PATH=${DIST_DIR}:$$PATH go-to-protobuf \
		--go-header-file=./hack/custom-boilerplate.go.txt \
		--packages=github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1 \
		--apimachinery-packages=${APIMACHINERY_PKGS} \
		--proto-import $(CURDIR)/vendor \
		--proto-import=${DIST_DIR}/protoc-include
	touch pkg/apis/rollouts/v1alpha1/generated.proto
	cp -R ${GOPATH}/src/github.com/argoproj/argo-rollouts/pkg . | true


# generates *.pb.go, *.pb.gw.go, swagger from .proto files
.PHONY: api-proto
api-proto: go-mod-vendor k8s-proto
	$(call protoc,pkg/apiclient/rollout/rollout.proto)

# generates ui related proto files
.PHONY: ui-proto
ui-proto:
	yarn --cwd ui run protogen

# generates k8s client, informer, lister, deepcopy from types.go
.PHONY: gen-k8scodegen
gen-k8scodegen: go-mod-vendor
	./hack/update-codegen.sh

# generates ./manifests/crds/
.PHONY: gen-crd
gen-crd: install-go-tools-local
	go run ./hack/gen-crd-spec/main.go

# generates mock files from interfaces
.PHONY: gen-mocks
gen-mocks: install-go-tools-local
	./hack/update-mocks.sh

# generates openapi_generated.go
.PHONY: gen-openapi
gen-openapi: $(DIST_DIR)/openapi-gen
	PATH=${DIST_DIR}:$$PATH openapi-gen \
		--go-header-file ${CURRENT_DIR}/hack/custom-boilerplate.go.txt \
		--input-dirs github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1 \
		--output-package github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1 \
		--report-filename pkg/apis/api-rules/violation_exceptions.list

.PHONY: controller
controller:
	CGO_ENABLED=0 go build -v -ldflags '${LDFLAGS}' -o ${DIST_DIR}/rollouts-controller ./cmd/rollouts-controller

.PHONY: plugin
plugin: ui/dist
	cp -r ui/dist/app/* server/static
	CGO_ENABLED=0 go build -v -ldflags '${LDFLAGS}' -o ${DIST_DIR}/${PLUGIN_CLI_NAME} ./cmd/kubectl-argo-rollouts

ui/dist:
	yarn --cwd ui install
	yarn --cwd ui build

.PHONY: plugin-linux
plugin-linux: ui/dist
	cp -r ui/dist/app/* server/static
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -v -ldflags '${LDFLAGS}' -o ${DIST_DIR}/${PLUGIN_CLI_NAME}-linux-amd64 ./cmd/kubectl-argo-rollouts
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -v -ldflags '${LDFLAGS}' -o ${DIST_DIR}/${PLUGIN_CLI_NAME}-linux-arm64 ./cmd/kubectl-argo-rollouts

.PHONY: plugin-darwin
plugin-darwin: ui/dist
	cp -r ui/dist/app/* server/static
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -v -ldflags '${LDFLAGS}' -o ${DIST_DIR}/${PLUGIN_CLI_NAME}-darwin-amd64 ./cmd/kubectl-argo-rollouts
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -v -ldflags '${LDFLAGS}' -o ${DIST_DIR}/${PLUGIN_CLI_NAME}-darwin-arm64 ./cmd/kubectl-argo-rollouts

.PHONY: plugin-windows
plugin-windows: ui/dist
	cp -r ui/dist/app/* server/static
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -v -ldflags '${LDFLAGS}' -o ${DIST_DIR}/${PLUGIN_CLI_NAME}-windows-amd64 ./cmd/kubectl-argo-rollouts

.PHONY: docs
docs:
	go run ./hack/gen-docs/main.go

.PHONY: builder-image
builder-image:
	DOCKER_BUILDKIT=1 docker build  -t $(IMAGE_PREFIX)argo-rollouts-ci-builder:$(IMAGE_TAG) --target builder .
		@if [ "$(DOCKER_PUSH)" = "true" ] ; then docker push $(IMAGE_PREFIX)argo-rollouts:$(IMAGE_TAG) ; fi

.PHONY: image
image:
ifeq ($(DEV_IMAGE), true)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -v -ldflags '${LDFLAGS}' -o ${DIST_DIR}/rollouts-controller-linux-amd64 ./cmd/rollouts-controller
	DOCKER_BUILDKIT=1 docker build -t $(IMAGE_PREFIX)argo-rollouts:$(IMAGE_TAG) -f Dockerfile.dev ${DIST_DIR}
else
	DOCKER_BUILDKIT=1 docker build -t $(IMAGE_PREFIX)argo-rollouts:$(IMAGE_TAG)  .
endif
	@if [ "$(DOCKER_PUSH)" = "true" ] ; then docker push $(IMAGE_PREFIX)argo-rollouts:$(IMAGE_TAG) ; fi

.PHONY: plugin-image
plugin-image:
	DOCKER_BUILDKIT=1 docker build --target kubectl-argo-rollouts -t $(IMAGE_PREFIX)kubectl-argo-rollouts:$(IMAGE_TAG) .
	if [ "$(DOCKER_PUSH)" = "true" ] ; then docker push $(IMAGE_PREFIX)kubectl-argo-rollouts:$(IMAGE_TAG) ; fi

.PHONY: lint
lint: go-mod-vendor
	golangci-lint run --fix

.PHONY: test
test: test-kustomize
	@make test-unit

.PHONY: test-kustomize
test-kustomize:
	./test/kustomize/test.sh

.PHONY: start-e2e
start-e2e:
	go run ./cmd/rollouts-controller/main.go --instance-id ${E2E_INSTANCE_ID} --loglevel debug --kloglevel 6

.PHONY: test-e2e
test-e2e: install-devtools-local
	${DIST_DIR}/gotestsum --rerun-fails-report=rerunreport.txt --junitfile=junit.xml --format=testname --packages="./test/e2e" --rerun-fails=5 -- -timeout 60m -count 1 --tags e2e -p ${E2E_PARALLEL} -parallel ${E2E_PARALLEL} -v --short ./test/e2e ${E2E_TEST_OPTIONS}

.PHONY: test-unit
 test-unit: install-devtools-local
	${DIST_DIR}/gotestsum --junitfile=junit.xml --format=testname -- -covermode=count -coverprofile=coverage.out `go list ./... | grep -v ./test/cmd/sample-metrics-plugin`


.PHONY: coverage
coverage: test
	go tool cover -html=coverage.out -o coverage.html
	open coverage.html

.PHONY: manifests
manifests:
	./hack/update-manifests.sh

.PHONY: clean
clean:
	-rm -rf ${CURRENT_DIR}/dist
	-rm -rf ${CURRENT_DIR}/ui/dist

.PHONY: precheckin
precheckin: test lint

.PHONY: release-docs
release-docs: docs
	docker run --rm -it \
		-v ~/.ssh:/root/.ssh \
		-v ${CURRENT_DIR}:/docs \
		-v ~/.gitconfig:/root/.gitconfig \
		squidfunk/mkdocs-material gh-deploy -r ${GIT_REMOTE_REPO}

# convenience target to run `mkdocs serve` using a docker container
.PHONY: serve-docs
serve-docs: docs
	docker run --rm -it -p 8000:8000 -v ${CURRENT_DIR}:/docs squidfunk/mkdocs-material serve -a 0.0.0.0:8000

.PHONY: release-precheck
release-precheck: manifests
	@if [ "$(GIT_TREE_STATE)" != "clean" ]; then echo 'git tree state is $(GIT_TREE_STATE)' ; exit 1; fi
	@if [ -z "$(GIT_TAG)" ]; then echo 'commit must be tagged to perform release' ; exit 1; fi
	@if [ "$(GIT_TAG)" != "v`cat VERSION`" ]; then echo 'VERSION does not match git tag'; exit 1; fi

.PHONY: release-plugins
release-plugins:
	./hack/build-release-plugins.sh

.PHONY: release
release: release-precheck precheckin image plugin-image release-plugins

.PHONY: trivy
trivy:
	@trivy fs --clear-cache
	@trivy fs .

.PHONY: checksums
checksums:
	shasum -a 256 ./dist/kubectl-argo-rollouts-* | awk -F './dist/' '{print $$1 $$2}' > ./dist/argo-rollouts-checksums.txt

# Build sample plugin with debug info
# https://www.jetbrains.com/help/go/attach-to-running-go-processes-with-debugger.html
.PHONY: build-sample-metric-plugin-debug
build-sample-metric-plugin-debug:
	go build -gcflags="all=-N -l" -o metric-plugin test/cmd/sample-metrics-plugin/main.go

.PHONY: build-sample-traffic-plugin-debug
build-sample-traffic-plugin-debug:
	go build -gcflags="all=-N -l" -o traffic-plugin test/cmd/sample-trafficrouter-plugin/main.go

