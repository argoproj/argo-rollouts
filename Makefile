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

# build development images
DEV_IMAGE=false

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

.PHONY: all
all: controller image

.PHONY: codegen
codegen: mocks
	./hack/update-codegen.sh
	./hack/update-openapigen.sh
	go run ./hack/gen-crd-spec/main.go

.PHONY: controller
controller: clean-debug
	CGO_ENABLED=0 go build -v -i -ldflags '${LDFLAGS}' -o ${DIST_DIR}/rollouts-controller ./cmd/rollouts-controller

.PHONY: plugin
plugin:
	CGO_ENABLED=0 go build -v -i -ldflags '${LDFLAGS}' -o ${DIST_DIR}/${PLUGIN_CLI_NAME} ./cmd/kubectl-argo-rollouts

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
test:
	go test -failfast -covermode=count -coverprofile=coverage.out ${TEST_TARGET}

.PHONY: coverage
coverage: test
	go tool cover -html=coverage.out -o coverage.html
	open coverage.html

.PHONY: mocks
mocks:
	mockery -dir ./metricproviders -name Provider -output ./providers/mocks

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

.PHONY: release-precheck
release-precheck: manifests
	@if [ "$(GIT_TREE_STATE)" != "clean" ]; then echo 'git tree state is $(GIT_TREE_STATE)' ; exit 1; fi
	@if [ -z "$(GIT_TAG)" ]; then echo 'commit must be tagged to perform release' ; exit 1; fi
	@if [ "$(GIT_TAG)" != "v`cat VERSION`" ]; then echo 'VERSION does not match git tag'; exit 1; fi

.PHONY: release
release: release-precheck precheckin image
