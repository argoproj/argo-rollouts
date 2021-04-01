package tolerantinformer

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	testutil "github.com/argoproj/argo-rollouts/test/util"
)

const (
	dummyNamespace = "dummy-namespace"
)

func newFakeDynamicInformer(objs ...runtime.Object) dynamicinformer.DynamicSharedInformerFactory {
	dynamicClient := testutil.NewFakeDynamicClient(objs...)
	dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)

	// The dynamic informer factory relies on calling ForResource on any GVR which wish to be
	// monitored *before* calling .Start(), in order to work properly.
	dynamicInformerFactory.ForResource(v1alpha1.RolloutGVR)
	dynamicInformerFactory.ForResource(v1alpha1.AnalysisTemplateGVR)
	dynamicInformerFactory.ForResource(v1alpha1.AnalysisRunGVR)
	dynamicInformerFactory.ForResource(v1alpha1.ExperimentGVR)
	dynamicInformerFactory.ForResource(v1alpha1.ClusterAnalysisTemplateGVR)

	// Start then stop the informer. We just want the informer to be filled in with the fake objects
	// and not really be running in the background.
	stopCh := make(chan struct{})
	dynamicInformerFactory.Start(stopCh)
	synced := dynamicInformerFactory.WaitForCacheSync(stopCh)
	close(stopCh)
	if len(synced) != 5 {
		panic("could not sync fake informer")
	}
	for gvr, isSynced := range synced {
		if !isSynced {
			panic(fmt.Sprintf("could not sync %v", gvr))
		}
	}
	return dynamicInformerFactory
}

func TestMalformedRollout(t *testing.T) {
	good := testutil.ObjectFromPath("examples/rollout-canary.yaml")
	good.SetNamespace("default")
	bad := testutil.ObjectFromPath("test/e2e/expectedfailures/malformed-rollout.yaml")
	bad.SetNamespace(dummyNamespace)
	dynInformerFactory := newFakeDynamicInformer(good, bad)
	informer := NewTolerantRolloutInformer(dynInformerFactory)

	verify := func(ro *v1alpha1.Rollout) {
		assert.True(t, ro.Spec.Strategy.Canary != nil)
		assert.Len(t, ro.Spec.Template.Spec.Containers[0].Resources.Requests, 0)
	}

	// test cluster scoped list
	list, err := informer.Lister().List(labels.NewSelector())
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	for _, obj := range list {
		if obj.Name == "malformed-rollout" {
			verify(obj)
		}
	}

	// test namespaced scoped get
	obj, err := informer.Lister().Rollouts(dummyNamespace).Get("malformed-rollout")
	assert.NoError(t, err)
	verify(obj)

	// test namespaced scoped list
	list, err = informer.Lister().Rollouts(dummyNamespace).List(labels.NewSelector())
	assert.NoError(t, err)
	assert.Len(t, list, 1)
	verify(list[0])
}

func TestMalformedRolloutEphemeralCtr(t *testing.T) {
	good := testutil.ObjectFromPath("examples/rollout-canary.yaml")
	good.SetNamespace("default")
	bad := testutil.ObjectFromPath("test/e2e/expectedfailures/malformed-rollout-ephemeral.yaml")
	bad.SetNamespace(dummyNamespace)
	dynInformerFactory := newFakeDynamicInformer(good, bad)
	informer := NewTolerantRolloutInformer(dynInformerFactory)

	verify := func(ro *v1alpha1.Rollout) {
		assert.True(t, ro.Spec.Strategy.Canary != nil)
		assert.Len(t, ro.Spec.Template.Spec.Containers[0].Resources.Requests, 0)

		// NOTE: kubernetes drops the ephemeral containers list completely when one fails to unmarshal
		// (e.g. when one has an invalid resource quantity). The following assertion is just to detect
		// if this assumption continues to hold true over the course of time (as we update k8s libraries)
		assert.Len(t, ro.Spec.Template.Spec.EphemeralContainers, 0)
		// assert.Len(t, ro.Spec.Template.Spec.EphemeralContainers[0].Resources.Requests, 0)
	}

	// test cluster scoped list
	list, err := informer.Lister().List(labels.NewSelector())
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	for _, obj := range list {
		if obj.Name == "malformed-rollout-ephemeral" {
			verify(obj)
		}
	}

	// test namespaced scoped get
	obj, err := informer.Lister().Rollouts(dummyNamespace).Get("malformed-rollout-ephemeral")
	assert.NoError(t, err)
	verify(obj)

	// test namespaced scoped list
	list, err = informer.Lister().Rollouts(dummyNamespace).List(labels.NewSelector())
	assert.NoError(t, err)
	assert.Len(t, list, 1)
	verify(list[0])
}

func verifyAnalysisSpec(t *testing.T, s interface{}) {
	//   metrics:
	//   - name: test
	//     provider:
	//       job:
	//         spec:
	//           template:
	//             spec:
	//               containers:
	//               - name: sleep
	//                 image: alpine:3.8
	//                 resources:
	//                   requests:
	//                     memory: invalid # invalid
	if spec, ok := s.(v1alpha1.AnalysisRunSpec); ok {
		assert.Len(t, spec.Metrics[0].Provider.Job.Spec.Template.Spec.Containers[0].Resources.Requests, 0)
	} else {
		spec := s.(v1alpha1.AnalysisTemplateSpec)
		assert.Len(t, spec.Metrics[0].Provider.Job.Spec.Template.Spec.Containers[0].Resources.Requests, 0)
	}
}

func TestMalformedAnalysisRun(t *testing.T) {
	good := testutil.ObjectFromPath("test/e2e/functional/analysis-run-job.yaml")
	good.SetNamespace("default")
	bad := testutil.ObjectFromPath("test/e2e/expectedfailures/malformed-analysisrun.yaml")
	bad.SetNamespace(dummyNamespace)
	dynInformerFactory := newFakeDynamicInformer(good, bad)
	informer := NewTolerantAnalysisRunInformer(dynInformerFactory)

	// test cluster scoped list
	list, err := informer.Lister().List(labels.NewSelector())
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	for _, obj := range list {
		if obj.Name == "malformed-analysis" {
			verifyAnalysisSpec(t, obj.Spec)
		}
	}

	// test namespaced scoped get
	obj, err := informer.Lister().AnalysisRuns(dummyNamespace).Get("malformed-analysis")
	assert.NoError(t, err)
	verifyAnalysisSpec(t, obj.Spec)

	// test namespaced scoped list
	list, err = informer.Lister().AnalysisRuns(dummyNamespace).List(labels.NewSelector())
	assert.NoError(t, err)
	assert.Len(t, list, 1)
	verifyAnalysisSpec(t, obj.Spec)
}

func TestMalformedAnalysisTemplate(t *testing.T) {
	good := testutil.ObjectFromPath("test/e2e/functional/analysis-run-job.yaml")
	good.SetNamespace("default")
	good.SetKind("AnalysisTemplate")
	bad := testutil.ObjectFromPath("test/e2e/expectedfailures/malformed-analysistemplate.yaml")
	bad.SetNamespace(dummyNamespace)
	dynInformerFactory := newFakeDynamicInformer(good, bad)
	informer := NewTolerantAnalysisTemplateInformer(dynInformerFactory)

	// test cluster scoped list
	list, err := informer.Lister().List(labels.NewSelector())
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	for _, obj := range list {
		if obj.Name == "malformed-analysis" {
			verifyAnalysisSpec(t, obj.Spec)
		}
	}

	// test namespaced scoped get
	obj, err := informer.Lister().AnalysisTemplates(dummyNamespace).Get("malformed-analysis")
	assert.NoError(t, err)
	verifyAnalysisSpec(t, obj.Spec)

	// test namespaced scoped list
	list, err = informer.Lister().AnalysisTemplates(dummyNamespace).List(labels.NewSelector())
	assert.NoError(t, err)
	assert.Len(t, list, 1)
	verifyAnalysisSpec(t, obj.Spec)
}

func TestMalformedClusterAnalysisTemplate(t *testing.T) {
	good := testutil.ObjectFromPath("test/e2e/functional/analysis-run-job.yaml")
	good.SetKind("ClusterAnalysisTemplate")
	bad := testutil.ObjectFromPath("test/e2e/expectedfailures/malformed-clusteranalysistemplate.yaml")
	dynInformerFactory := newFakeDynamicInformer(good, bad)
	informer := NewTolerantClusterAnalysisTemplateInformer(dynInformerFactory)

	// test cluster scoped list
	list, err := informer.Lister().List(labels.NewSelector())
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	for _, obj := range list {
		if obj.Name == "malformed-analysis" {
			verifyAnalysisSpec(t, obj.Spec)
		}
	}

	// test cluster scoped get
	obj, err := informer.Lister().Get("malformed-analysis")
	assert.NoError(t, err)
	verifyAnalysisSpec(t, obj.Spec)
}

func TestMalformedExperiment(t *testing.T) {
	good := testutil.ObjectFromPath("test/e2e/functional/experiment-basic.yaml")
	good.SetNamespace("default")
	good.SetGenerateName("")
	good.SetName("good-experiment")
	bad := testutil.ObjectFromPath("test/e2e/expectedfailures/malformed-experiment.yaml")
	bad.SetNamespace(dummyNamespace)
	dynInformerFactory := newFakeDynamicInformer(good, bad)
	informer := NewTolerantExperimentInformer(dynInformerFactory)

	verify := func(ex *v1alpha1.Experiment) {
		assert.Len(t, ex.Spec.Templates[0].Template.Spec.Containers[0].Resources.Requests, 0)
	}

	// test cluster scoped list
	list, err := informer.Lister().List(labels.NewSelector())
	assert.NoError(t, err)
	assert.Len(t, list, 2)
	for _, obj := range list {
		if obj.Name == "malformed-experiment" {
			verify(obj)
		}
	}

	// test namespaced scoped get
	obj, err := informer.Lister().Experiments(dummyNamespace).Get("malformed-experiment")
	assert.NoError(t, err)
	verify(obj)

	// test namespaced scoped list
	list, err = informer.Lister().Experiments(dummyNamespace).List(labels.NewSelector())
	assert.NoError(t, err)
	assert.Len(t, list, 1)
	verify(obj)
}
