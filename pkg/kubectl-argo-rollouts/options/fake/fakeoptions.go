package options

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"

	fakeroclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

// NewFakeArgoRolloutsOptions returns a options.ArgoRolloutsOptions suitable for testing
func NewFakeArgoRolloutsOptions(obj ...runtime.Object) (*cmdtesting.TestFactory, *options.ArgoRolloutsOptions) {
	iostreams, _, _, _ := genericclioptions.NewTestIOStreams()
	tf := cmdtesting.NewTestFactory()
	o := options.NewArgoRolloutsOptions(iostreams)
	o.RESTClientGetter = tf
	o.RolloutsClient = fakeroclient.NewSimpleClientset(obj...)
	return tf, o
}
