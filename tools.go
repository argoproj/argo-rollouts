// This package contains code generation and build utilities
// This package imports things required by build scripts, to force `go mod` to see them as dependencies
package tools

import (
	_ "k8s.io/code-generator/cmd/client-gen/generators"
	_ "github.com/vektra/mockery"
)
