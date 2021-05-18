####################################################################################################
# Builder image
# Initial stage which pulls prepares build dependencies and CLI tooling we need for our final image
# Also used as the image in CI jobs so needs all dependencies
####################################################################################################
FROM golang:1.16.3 as builder

RUN apt-get update && apt-get install -y \
    wget \
    ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

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
# UI build stage
####################################################################################################
FROM docker.io/library/node:12.18.4 as argo-rollouts-ui

WORKDIR /src
ADD ["ui/package.json", "ui/yarn.lock", "./"]

RUN yarn install --network-timeout 300000

ADD ["ui/", "."]

ARG ARGO_VERSION=latest
ENV ARGO_VERSION=$ARGO_VERSION
RUN NODE_ENV='production' yarn build

####################################################################################################
# Rollout Controller Build stage which performs the actual build of argo-rollouts binaries
####################################################################################################
FROM golang:1.16.3 as argo-rollouts-build

WORKDIR /go/src/github.com/argoproj/argo-rollouts

# Copy only go.mod and go.sum files. This way on subsequent docker builds if the
# dependencies didn't change it won't re-download the dependencies for nothing.
COPY go.mod go.sum ./
RUN go mod download

# Copy UI files for plugin build
COPY --from=argo-rollouts-ui /src/dist/app ./ui/dist/app

# Perform the build
COPY . .

# stop make from trying to re-build this without yarn installed
RUN touch ui/dist/node_modules.marker && \
    mkdir -p ui/dist/app && \
    touch ui/dist/app/index.html && \
    find ui/dist

ARG MAKE_TARGET="controller plugin-linux plugin-darwin"
RUN make ${MAKE_TARGET}

####################################################################################################
# Kubectl plugin image
####################################################################################################
FROM docker.io/library/ubuntu:20.10 as kubectl-argo-rollouts

COPY --from=argo-rollouts-build /go/src/github.com/argoproj/argo-rollouts/dist/kubectl-argo-rollouts-linux-amd64 /bin/kubectl-argo-rollouts

WORKDIR /home/argo-rollouts

ENTRYPOINT ["/bin/kubectl-argo-rollouts"]

CMD ["dashboard"]

####################################################################################################
# Final image
####################################################################################################
FROM scratch

COPY --from=argo-rollouts-build /go/src/github.com/argoproj/argo-rollouts/dist/rollouts-controller /bin/
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Use numeric user, allows kubernetes to identify this user as being
# non-root when we use a security context with runAsNonRoot: true
USER 999

WORKDIR /home/argo-rollouts

ENTRYPOINT [ "/bin/rollouts-controller" ]
