PACKAGE=github.com/argoproj/argo-rollouts
CURRENT_DIR=$(shell pwd)
DIST_DIR=${CURRENT_DIR}/dist
PLUGIN_CLI_NAME?=kubectl-argo-rollouts
TEST_TARGET ?= ./...

VERSION=$(shell cat ${CURRENT_DIR}/VERSION)
BUILD_DATE=$(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
GIT_COMMIT=$(shell git rev-parse HEAD)
GIT_TAG=$(shell if [ -z "`git status --porcelain`" ]; then git describe --exact-match --tags HEAD 2>/dev/null; fi)
GIT_TREE_STATE=$(shell if [ -z "`git status --porcelain`" ]; then echo "clean" ; else echo "dirty"; fi)
GIT_REMOTE_REPO=upstream
# build development images
DEV_IMAGE=false

# E2E variables
E2E_INSTANCE_ID ?= argo-rollouts-e2e
E2E_TEST_OPTIONS ?= 
E2E_PARALLEL ?= 1

override LDFLAGS += \
  -X ${PACKAGE}/utils/version.version=${VERSION} \
  -X ${PACKAGE}/utils/version.buildDate=${BUILD_DATE} \
  -X ${PACKAGE}/utils/version.gitCommit=${GIT_COMMIT} \
  -X ${PACKAGE}/utils/version.gitTreeState=${GIT_TREE_STATE}

# docker image publishing options
DOCKER_PUSH=false
IMAGE_TAG=latest
ifneq (${GIT_TAG},)
IMAGE_TAG=${GIT_TAG}
LDFLAGS += -X ${PACKAGE}.gitTag=${GIT_TAG}
endif
ifneq (${IMAGE_NAMESPACE},)
override LDFLAGS += -X ${PACKAGE}/install.imageNamespace=${IMAGE_NAMESPACE}
endif
ifneq (${IMAGE_TAG},)
override LDFLAGS += -X ${PACKAGE}/install.imageTag=${IMAGE_TAG}
endif

ifeq (${DOCKER_PUSH},true)
ifndef IMAGE_NAMESPACE
$(error IMAGE_NAMESPACE must be set to push images (e.g. IMAGE_NAMESPACE=argoproj))
endif
endif

ifdef IMAGE_NAMESPACE
IMAGE_PREFIX=${IMAGE_NAMESPACE}/
endif

# protoc,my.proto
define protoc
	# protoc $(1)
    [ -e vendor ] || go mod vendor
    protoc \
      -I /usr/local/include \
      -I . \
      -I ./vendor \
      -I ${GOPATH}/src \
      -I ${GOPATH}/pkg/mod/github.com/gogo/protobuf@v1.3.1/gogoproto \
      -I ${GOPATH}/pkg/mod/github.com/grpc-ecosystem/grpc-gateway@v1.16.0/third_party/googleapis \
      --gogofast_out=plugins=grpc:${GOPATH}/src \
      --grpc-gateway_out=logtostderr=true:${GOPATH}/src \
      --swagger_out=logtostderr=true,fqn_for_swagger_name=true:. \
      $(1)
endef

.PHONY: all
all: controller image

.PHONY: codegen
codegen: protogen mocks
	./hack/update-codegen.sh
	./hack/update-openapigen.sh
	PATH=${DIST_DIR}:$$PATH go run ./hack/gen-crd-spec/main.go

LEGACY_PATH=$(GOPATH)/src/github.com/argoproj/argo-rollouts

install-codegen-tools: 
	sudo ./hack/install-codegen-go-tools.sh

.PHONY: ensure-gopath
ensure-gopath:
ifneq ("$(PWD)","$(LEGACY_PATH)")
	@echo "Due to legacy requirements for codegen, repository needs to be checked out within \$$GOPATH"
	@echo "Location of this repo should be '$(LEGACY_PATH)' but is '$(PWD)'"
	@exit 1
endif

UI_PROTOGEN_CMD=yarn run protogen
.PHONY: protogen
protogen: pkg/apis/rollouts/v1alpha1/generated.proto pkg/apiclient/rollout/rollout.swagger.json \
	$(GOPATH)/bin/mockery
	go generate ./pkg/apiclient/rollout
	rm -Rf vendor
	go mod tidy
	cd ui && ${UI_PROTOGEN_CMD} && cd ..

PROTO_BINARIES := $(GOPATH)/bin/protoc-gen-gogo $(GOPATH)/bin/protoc-gen-gogofast $(GOPATH)/bin/goimports $(GOPATH)/bin/protoc-gen-grpc-gateway $(GOPATH)/bin/protoc-gen-swagger
TYPES := $(shell find pkg/apis/rollouts/v1alpha1 -type f -name '*.go' -not -name openapi_generated.go -not -name '*generated*' -not -name '*test.go')

$(GOPATH)/bin/mockery:
	./hack/recurl.sh dist/mockery.tar.gz https://github.com/vektra/mockery/releases/download/v1.1.1/mockery_1.1.1_$(shell uname -s)_$(shell uname -m).tar.gz
	tar zxvf dist/mockery.tar.gz mockery
	chmod +x mockery
	mkdir -p $(GOPATH)/bin
	mv mockery $(GOPATH)/bin/mockery
	mockery -version

$(GOPATH)/bin/controller-gen:
	$(call go_install,sigs.k8s.io/controller-tools/cmd/controller-gen)

$(GOPATH)/bin/go-to-protobuf:
	$(call go_install,k8s.io/code-generator/cmd/go-to-protobuf)

$(GOPATH)/bin/protoc-gen-gogo:
	$(call go_install,github.com/gogo/protobuf/protoc-gen-gogo)

$(GOPATH)/bin/protoc-gen-gogofast:
	$(call go_install,github.com/gogo/protobuf/protoc-gen-gogofast)

$(GOPATH)/bin/protoc-gen-grpc-gateway:
	$(call go_install,github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway)

$(GOPATH)/bin/protoc-gen-swagger:
	$(call go_install,github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger)

$(GOPATH)/bin/openapi-gen:
	$(call go_install,k8s.io/kube-openapi/cmd/openapi-gen)

$(GOPATH)/bin/swagger:
	$(call go_install,github.com/go-swagger/go-swagger/cmd/swagger)

$(GOPATH)/bin/goimports:
	$(call go_install,golang.org/x/tools/cmd/goimports)

APIMACHINERY_PKGS=k8s.io/apimachinery/pkg/util/intstr,+k8s.io/apimachinery/pkg/api/resource,+k8s.io/apimachinery/pkg/runtime/schema,+k8s.io/apimachinery/pkg/runtime,k8s.io/apimachinery/pkg/apis/meta/v1,k8s.io/api/core/v1,k8s.io/api/batch/v1

pkg/apis/rollouts/v1alpha1/generated.proto: $(GOPATH)/bin/go-to-protobuf $(PROTO_BINARIES) $(TYPES)
	[ -e vendor ] || go mod vendor
	go mod download
	${GOPATH}/bin/go-to-protobuf \
		--go-header-file=./hack/custom-boilerplate.go.txt \
		--packages=github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1 \
		--apimachinery-packages=${APIMACHINERY_PKGS} \
		--proto-import ./vendor
	touch pkg/apis/rollouts/v1alpha1/generated.proto

pkg/apiclient/rollout/rollout.swagger.json: $(PROTO_BINARIES) $(TYPES) pkg/apiclient/rollout/rollout.proto
	$(call protoc,pkg/apiclient/rollout/rollout.proto)

.PHONY: controller
controller: clean-debug
	CGO_ENABLED=0 go build -v -i -ldflags '${LDFLAGS}' -o ${DIST_DIR}/rollouts-controller ./cmd/rollouts-controller

.PHONY: server
server: clean-debug ui/dist
	cp -r ui/dist/app server/static
	CGO_ENABLED=0 go build -v -ldflags '${LDFLAGS}' -o ${DIST_DIR}/rollouts-server ./cmd/rollouts-server

.PHONY: plugin
plugin: ui/dist
	cp -r ui/dist/app server/static
	CGO_ENABLED=0 go build -v -i -ldflags '${LDFLAGS}' -o ${DIST_DIR}/${PLUGIN_CLI_NAME} ./cmd/kubectl-argo-rollouts

ui/dist:
	yarn --cwd ui install
	yarn --cwd ui build

.PHONY: plugin-linux
plugin-linux: ui/dist
	cp -r ui/dist/app server/static
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -v -i -ldflags '${LDFLAGS}' -o ${DIST_DIR}/${PLUGIN_CLI_NAME}-linux-amd64 ./cmd/kubectl-argo-rollouts

.PHONY: plugin-darwin
plugin-darwin: ui/dist
	cp -r ui/dist/app server/static
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -v -i -ldflags '${LDFLAGS}' -o ${DIST_DIR}/${PLUGIN_CLI_NAME}-darwin-amd64 ./cmd/kubectl-argo-rollouts

.PHONY: plugin-docs
plugin-docs:
	go run ./hack/gen-plugin-docs/main.go

.PHONY: builder-image
builder-image:
	docker build  -t $(IMAGE_PREFIX)argo-rollouts-ci-builder:$(IMAGE_TAG) --target builder .
		@if [ "$(DOCKER_PUSH)" = "true" ] ; then docker push $(IMAGE_PREFIX)argo-rollouts:$(IMAGE_TAG) ; fi

.PHONY: image
image:
ifeq ($(DEV_IMAGE), true)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -v -i -ldflags '${LDFLAGS}' -o ${DIST_DIR}/rollouts-controller-linux-amd64 ./cmd/rollouts-controller
	docker build -t $(IMAGE_PREFIX)argo-rollouts:$(IMAGE_TAG) -f Dockerfile.dev .
else
	docker build -t $(IMAGE_PREFIX)argo-rollouts:$(IMAGE_TAG)  .
endif
	@if [ "$(DOCKER_PUSH)" = "true" ] ; then docker push $(IMAGE_PREFIX)argo-rollouts:$(IMAGE_TAG) ; fi

.PHONY: lint
lint:
	golangci-lint run --fix

.PHONY: test
test: test-kustomize
	go test -covermode=count -coverprofile=coverage.out ${TEST_TARGET}

.PHONY: test-kustomize
test-kustomize:
	./test/kustomize/test.sh

.PHONY: start-e2e
start-e2e:
	go run ./cmd/rollouts-controller/main.go --instance-id ${E2E_INSTANCE_ID} --loglevel debug

.PHONY: test-e2e
test-e2e:
	go test -timeout 15m -v -count 1 --tags e2e -p ${E2E_PARALLEL} --short ./test/e2e ${E2E_TEST_OPTIONS}

.PHONY: coverage
coverage: test
	go tool cover -html=coverage.out -o coverage.html
	open coverage.html

.PHONY: mocks
mocks:
	./hack/update-mocks.sh

.PHONY: manifests
manifests:
	./hack/update-manifests.sh

# Cleans VSCode debug.test files from sub-dirs to prevent them from being included in packr boxes
.PHONY: clean-debug
clean-debug:
	-find ${CURRENT_DIR} -name debug.test | xargs rm -f

.PHONY: clean
clean: clean-debug
	-rm -rf ${CURRENT_DIR}/dist

.PHONY: precheckin
precheckin: test lint

.PHONY: release-docs
release-docs: plugin-docs
	docker run --rm -it \
		-v ~/.ssh:/root/.ssh \
		-v ${CURRENT_DIR}:/docs \
		-v ~/.gitconfig:/root/.gitconfig \
		squidfunk/mkdocs-material gh-deploy -r ${GIT_REMOTE_REPO}

# convenience target to run `mkdocs serve` using a docker container
.PHONY: serve-docs
serve-docs: plugin-docs
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
release: release-precheck precheckin image release-plugins