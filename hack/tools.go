//go:build tools
// +build tools

package tools

import (
	// used in `update-codegen.sh`
	_ "k8s.io/code-generator/cmd/client-gen"
	_ "k8s.io/code-generator/cmd/deepcopy-gen"
	_ "k8s.io/code-generator/cmd/defaulter-gen"
	_ "k8s.io/code-generator/cmd/go-to-protobuf"
	_ "k8s.io/code-generator/cmd/go-to-protobuf/protoc-gen-gogo"
	_ "k8s.io/code-generator/cmd/informer-gen"
	_ "k8s.io/code-generator/cmd/lister-gen"
	_ "k8s.io/code-generator/pkg/util"

	// used in `make api-proto`
	_ "github.com/gogo/protobuf/protoc-gen-gogo"
	_ "github.com/gogo/protobuf/protoc-gen-gogofast"
	_ "github.com/grpc-ecosystem/grpc-gateway/protoc-gen-grpc-gateway"
	_ "github.com/grpc-ecosystem/grpc-gateway/protoc-gen-swagger"

	// used in `make gen-openapi`
	_ "k8s.io/kube-openapi/cmd/openapi-gen"

	_ "github.com/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
	_ "github.com/argoproj/argo-events/pkg/apis/eventbus/v1alpha1"
	_ "github.com/argoproj/argo-events/pkg/apis/eventsource/v1alpha1"
	_ "github.com/argoproj/argo-events/pkg/apis/sensor/v1alpha1"
	_ "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
)
