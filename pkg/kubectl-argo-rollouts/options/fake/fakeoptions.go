package options

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	cmdtesting "k8s.io/kubectl/pkg/cmd/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
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
	var allObjs []runtime.Object

	// Loop through supplied fake objects. Set TypeMeta if it wasn't set in the test
	// so that the objects can also go into the fake dynamic client
	for _, o := range obj {
		switch typedO := o.(type) {
		case *v1alpha1.Rollout:
			typedO.TypeMeta = metav1.TypeMeta{
				Kind:       "Rollout",
				APIVersion: "argoproj.io/v1alpha1",
			}
			rolloutObjs = append(rolloutObjs, o)
		case *v1alpha1.AnalysisRun:
			typedO.TypeMeta = metav1.TypeMeta{
				Kind:       "AnalysisRun",
				APIVersion: "argoproj.io/v1alpha1",
			}
			rolloutObjs = append(rolloutObjs, o)
		case *v1alpha1.AnalysisTemplate:
			typedO.TypeMeta = metav1.TypeMeta{
				Kind:       "AnalysisTemplate",
				APIVersion: "argoproj.io/v1alpha1",
			}
			rolloutObjs = append(rolloutObjs, o)
		case *v1alpha1.ClusterAnalysisTemplate:
			typedO.TypeMeta = metav1.TypeMeta{
				Kind:       "ClusterAnalysisTemplate",
				APIVersion: "argoproj.io/v1alpha1",
			}
			rolloutObjs = append(rolloutObjs, o)
		case *v1alpha1.Experiment:
			typedO.TypeMeta = metav1.TypeMeta{
				Kind:       "Experiment",
				APIVersion: "argoproj.io/v1alpha1",
			}
			rolloutObjs = append(rolloutObjs, o)
		default:
			kubeObjs = append(kubeObjs, o)
		}
		allObjs = append(allObjs, o)
	}

	o.RolloutsClient = fakeroclient.NewSimpleClientset(rolloutObjs...)
	o.KubeClient = k8sfake.NewSimpleClientset(kubeObjs...)
	err := v1alpha1.AddToScheme(scheme.Scheme)
	if err != nil {
		panic(err)
	}
	listMapping := map[schema.GroupVersionResource]string{
		v1alpha1.RolloutGVR:                 rollouts.RolloutKind + "List",
		v1alpha1.AnalysisTemplateGVR:        rollouts.AnalysisTemplateKind + "List",
		v1alpha1.AnalysisRunGVR:             rollouts.AnalysisRunKind + "List",
		v1alpha1.ExperimentGVR:              rollouts.ExperimentKind + "List",
		v1alpha1.ClusterAnalysisTemplateGVR: rollouts.ClusterAnalysisTemplateKind + "List",
	}

	o.DynamicClient = dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme.Scheme, listMapping, allObjs...)
	return tf, o
}
