package options

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	fakeroclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/options"
)

// NewFakeArgoRolloutsOptions returns a options.ArgoRolloutsOptions suitable for testing
func NewFakeArgoRolloutsOptions(obj ...runtime.Object) (*cmdtesting.TestFactory, *options.ArgoRolloutsOptions) {
	iostreams, _, _, _ := genericclioptions.NewTestIOStreams()
	tf := cmdtesting.NewTestFactory()
	o := options.NewArgoRolloutsOptions(iostreams)
	o.RESTClientGetter = tf

	var rolloutObjs []runtime.Object
	var kubeObjs []runtime.Object

	for _, o := range obj {
		switch o.(type) {
		case *v1alpha1.Rollout, *v1alpha1.AnalysisRun, *v1alpha1.AnalysisTemplate, *v1alpha1.Experiment:
			rolloutObjs = append(rolloutObjs, o)
		default:
			kubeObjs = append(kubeObjs, o)
		}
	}

	o.RolloutsClient = fakeroclient.NewSimpleClientset(rolloutObjs...)
	o.KubeClient = k8sfake.NewSimpleClientset(kubeObjs...)
	return tf, o
}
