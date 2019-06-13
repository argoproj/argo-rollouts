####################################################################################################
# Builder image
# Initial stage which pulls prepares build dependencies and CLI tooling we need for our final image
# Also used as the image in CI jobs so needs all dependencies
####################################################################################################
FROM golang:1.12.6 as builder

RUN apt-get update && apt-get install -y \
    git \
    make \
    wget \
    gcc \
    zip && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# Install docker
ENV DOCKER_VERSION=18.06.0
RUN curl -O https://download.docker.com/linux/static/stable/x86_64/docker-${DOCKER_VERSION}-ce.tgz && \
  tar -xzf docker-${DOCKER_VERSION}-ce.tgz && \
  mv docker/docker /usr/local/bin/docker && \
  rm -rf ./docker

# Install dep
ENV DEP_VERSION=0.5.0
RUN wget https://github.com/golang/dep/releases/download/v${DEP_VERSION}/dep-linux-amd64 -O /usr/local/bin/dep && \
    chmod +x /usr/local/bin/dep

# Install golangci-lint
RUN wget https://install.goreleaser.com/github.com/golangci/golangci-lint.sh  && \
    chmod +x ./golangci-lint.sh && \
    ./golangci-lint.sh -b $GOPATH/bin && \
    golangci-lint linters

COPY .golangci.yml ${GOPATH}/src/dummy/.golangci.yml

RUN cd ${GOPATH}/src/dummy && \
    touch dummy.go \
    golangci-lint run

####################################################################################################
# Rollout Controller Build stage which performs the actual build of argo-rollouts binaries
####################################################################################################
FROM golang:1.12.6 as argo-rollouts-build

COPY --from=builder /usr/local/bin/dep /usr/local/bin/dep


# A dummy directory is created under $GOPATH/src/dummy so we are able to use dep
# to install all the packages of our dep lock file
COPY Gopkg.toml ${GOPATH}/src/dummy/Gopkg.toml
COPY Gopkg.lock ${GOPATH}/src/dummy/Gopkg.lock

RUN cd ${GOPATH}/src/dummy && \
    dep ensure -vendor-only && \
    mv vendor/* ${GOPATH}/src/ && \
    rmdir vendor

# Perform the build
WORKDIR /go/src/github.com/argoproj/argo-rollouts
COPY . .
ARG MAKE_TARGET="controller"
RUN make ${MAKE_TARGET}


RUN groupadd -g 999 argo-rollouts && \
    useradd -r -u 999 -g argo-rollouts argo-rollouts && \
    mkdir -p /home/argo-rollouts && \
    chown argo-rollouts:argo-rollouts /home/argo-rollouts


####################################################################################################
# Final image
####################################################################################################
FROM scratch

COPY --from=argo-rollouts-build /go/src/github.com/argoproj/argo-rollouts/dist/rollouts-controller /bin/

# Import the user and group files from the builder.
COPY --from=argo-rollouts-build /etc/passwd /etc/passwd

USER argo-rollouts

WORKDIR /home/argo-rollouts

ENTRYPOINT [ "/bin/rollouts-controller" ]
