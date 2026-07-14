package tolerantinformer

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutinformers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	testutil "github.com/argoproj/argo-rollouts/test/util"
)

const (
	dummyNamespace = "dummy-namespace"
)

// fakeInformers holds the pre-wired tolerant informers and the underlying dynamic
// factory. Wrappers are constructed before factory.Start so SetTransform installs
// cleanly on the shared informers; tests reuse these instead of re-constructing.
type fakeInformers struct {
	factory                 dynamicinformer.DynamicSharedInformerFactory
	rollout                 rolloutinformers.RolloutInformer
	analysisTemplate        rolloutinformers.AnalysisTemplateInformer
	analysisRun             rolloutinformers.AnalysisRunInformer
	experiment              rolloutinformers.ExperimentInformer
	clusterAnalysisTemplate rolloutinformers.ClusterAnalysisTemplateInformer
}

func newFakeDynamicInformer(objs ...runtime.Object) *fakeInformers {
	dynamicClient := testutil.NewFakeDynamicClient(objs...)
	factory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)

	// Construct tolerant wrappers BEFORE Start: each constructor calls
	// SharedIndexInformer.SetTransform, which is rejected once the informer has
	// started.
	fi := &fakeInformers{
		factory:                 factory,
		rollout:                 NewTolerantRolloutInformer(factory),
		analysisTemplate:        NewTolerantAnalysisTemplateInformer(factory),
		analysisRun:             NewTolerantAnalysisRunInformer(factory),
		experiment:              NewTolerantExperimentInformer(factory),
		clusterAnalysisTemplate: NewTolerantClusterAnalysisTemplateInformer(factory),
	}

	// Start then stop the informer. We just want the informer to be filled in with the fake objects
	// and not really be running in the background.
	stopCh := make(chan struct{})
	factory.Start(stopCh)
	synced := factory.WaitForCacheSync(stopCh)
	close(stopCh)
	if len(synced) != 5 {
		panic("could not sync fake informer")
	}
	for gvr, isSynced := range synced {
		if !isSynced {
			panic(fmt.Sprintf("could not sync %v", gvr))
		}
	}
	return fi
}

func TestMalformedRollout(t *testing.T) {
	good := testutil.ObjectFromPath("examples/rollout-canary.yaml")
	good.SetNamespace("default")
	bad := testutil.ObjectFromPath("test/e2e/expectedfailures/malformed-rollout.yaml")
	bad.SetNamespace(dummyNamespace)
	fi := newFakeDynamicInformer(good, bad)
	informer := fi.rollout

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
	fi := newFakeDynamicInformer(good, bad)
	informer := fi.rollout

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

func verifyAnalysisSpec(t *testing.T, s any) {
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
	fi := newFakeDynamicInformer(good, bad)
	informer := fi.analysisRun

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
	fi := newFakeDynamicInformer(good, bad)
	informer := fi.analysisTemplate

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
	fi := newFakeDynamicInformer(good, bad)
	informer := fi.clusterAnalysisTemplate

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
	fi := newFakeDynamicInformer(good, bad)
	informer := fi.experiment

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

// storeUpdateCase covers one tolerant resource kind for the GetStore().Update /
// poisoned-indexer regressions introduced by #4745 + notifications-engine.
type storeUpdateCase struct {
	name         string
	obj          *unstructured.Unstructured
	cacheKey     string
	assertTyped  func(t *testing.T, raw any)
	list         func(t *testing.T, fi *fakeInformers) (annotations map[string]string, err error)
	get          func(t *testing.T, fi *fakeInformers, name string) (annotations map[string]string, err error)
	rawIndexer   func(fi *fakeInformers) cache.Indexer
	wrappedStore func(fi *fakeInformers) cache.Store
}

func storeUpdateCases(t *testing.T) []storeUpdateCase {
	t.Helper()

	rollout := testutil.ObjectFromPath("examples/rollout-canary.yaml")
	rollout.SetNamespace("default")

	analysisRun := testutil.ObjectFromPath("test/e2e/functional/analysis-run-job.yaml")
	analysisRun.SetNamespace("default")

	analysisTemplate := testutil.ObjectFromPath("test/e2e/functional/analysis-run-job.yaml")
	analysisTemplate.SetNamespace("default")
	analysisTemplate.SetKind("AnalysisTemplate")
	analysisTemplate.SetName("good-analysis-template")

	experiment := testutil.ObjectFromPath("test/e2e/functional/experiment-basic.yaml")
	experiment.SetNamespace("default")
	experiment.SetGenerateName("")
	experiment.SetName("good-experiment")

	clusterAT := testutil.ObjectFromPath("test/e2e/functional/analysis-run-job.yaml")
	clusterAT.SetKind("ClusterAnalysisTemplate")
	clusterAT.SetNamespace("")
	clusterAT.SetName("good-cluster-analysis-template")

	return []storeUpdateCase{
		{
			name:     "Rollout",
			obj:      rollout,
			cacheKey: "default/" + rollout.GetName(),
			assertTyped: func(t *testing.T, raw any) {
				_, ok := raw.(*v1alpha1.Rollout)
				assert.True(t, ok, "expected *v1alpha1.Rollout, got %T", raw)
			},
			list: func(t *testing.T, fi *fakeInformers) (map[string]string, error) {
				list, err := fi.rollout.Lister().List(labels.NewSelector())
				if err != nil {
					return nil, err
				}
				require.Len(t, list, 1)
				return list[0].Annotations, nil
			},
			get: func(t *testing.T, fi *fakeInformers, name string) (map[string]string, error) {
				obj, err := fi.rollout.Lister().Rollouts("default").Get(name)
				if err != nil {
					return nil, err
				}
				return obj.Annotations, nil
			},
			rawIndexer: func(fi *fakeInformers) cache.Indexer {
				return fi.rollout.(*tolerantRolloutInformer).delegate.Informer().GetIndexer()
			},
			wrappedStore: func(fi *fakeInformers) cache.Store { return fi.rollout.Informer().GetStore() },
		},
		{
			name:     "AnalysisRun",
			obj:      analysisRun,
			cacheKey: "default/" + analysisRun.GetName(),
			assertTyped: func(t *testing.T, raw any) {
				_, ok := raw.(*v1alpha1.AnalysisRun)
				assert.True(t, ok, "expected *v1alpha1.AnalysisRun, got %T", raw)
			},
			list: func(t *testing.T, fi *fakeInformers) (map[string]string, error) {
				list, err := fi.analysisRun.Lister().List(labels.NewSelector())
				if err != nil {
					return nil, err
				}
				require.Len(t, list, 1)
				return list[0].Annotations, nil
			},
			get: func(t *testing.T, fi *fakeInformers, name string) (map[string]string, error) {
				obj, err := fi.analysisRun.Lister().AnalysisRuns("default").Get(name)
				if err != nil {
					return nil, err
				}
				return obj.Annotations, nil
			},
			rawIndexer: func(fi *fakeInformers) cache.Indexer {
				return fi.analysisRun.(*tolerantAnalysisRunInformer).delegate.Informer().GetIndexer()
			},
			wrappedStore: func(fi *fakeInformers) cache.Store { return fi.analysisRun.Informer().GetStore() },
		},
		{
			name:     "AnalysisTemplate",
			obj:      analysisTemplate,
			cacheKey: "default/" + analysisTemplate.GetName(),
			assertTyped: func(t *testing.T, raw any) {
				_, ok := raw.(*v1alpha1.AnalysisTemplate)
				assert.True(t, ok, "expected *v1alpha1.AnalysisTemplate, got %T", raw)
			},
			list: func(t *testing.T, fi *fakeInformers) (map[string]string, error) {
				list, err := fi.analysisTemplate.Lister().List(labels.NewSelector())
				if err != nil {
					return nil, err
				}
				require.Len(t, list, 1)
				return list[0].Annotations, nil
			},
			get: func(t *testing.T, fi *fakeInformers, name string) (map[string]string, error) {
				obj, err := fi.analysisTemplate.Lister().AnalysisTemplates("default").Get(name)
				if err != nil {
					return nil, err
				}
				return obj.Annotations, nil
			},
			rawIndexer: func(fi *fakeInformers) cache.Indexer {
				return fi.analysisTemplate.(*tolerantAnalysisTemplateInformer).delegate.Informer().GetIndexer()
			},
			wrappedStore: func(fi *fakeInformers) cache.Store { return fi.analysisTemplate.Informer().GetStore() },
		},
		{
			name:     "Experiment",
			obj:      experiment,
			cacheKey: "default/" + experiment.GetName(),
			assertTyped: func(t *testing.T, raw any) {
				_, ok := raw.(*v1alpha1.Experiment)
				assert.True(t, ok, "expected *v1alpha1.Experiment, got %T", raw)
			},
			list: func(t *testing.T, fi *fakeInformers) (map[string]string, error) {
				list, err := fi.experiment.Lister().List(labels.NewSelector())
				if err != nil {
					return nil, err
				}
				require.Len(t, list, 1)
				return list[0].Annotations, nil
			},
			get: func(t *testing.T, fi *fakeInformers, name string) (map[string]string, error) {
				obj, err := fi.experiment.Lister().Experiments("default").Get(name)
				if err != nil {
					return nil, err
				}
				return obj.Annotations, nil
			},
			rawIndexer: func(fi *fakeInformers) cache.Indexer {
				return fi.experiment.(*tolerantExperimentInformer).delegate.Informer().GetIndexer()
			},
			wrappedStore: func(fi *fakeInformers) cache.Store { return fi.experiment.Informer().GetStore() },
		},
		{
			name:     "ClusterAnalysisTemplate",
			obj:      clusterAT,
			cacheKey: clusterAT.GetName(),
			assertTyped: func(t *testing.T, raw any) {
				_, ok := raw.(*v1alpha1.ClusterAnalysisTemplate)
				assert.True(t, ok, "expected *v1alpha1.ClusterAnalysisTemplate, got %T", raw)
			},
			list: func(t *testing.T, fi *fakeInformers) (map[string]string, error) {
				list, err := fi.clusterAnalysisTemplate.Lister().List(labels.NewSelector())
				if err != nil {
					return nil, err
				}
				require.Len(t, list, 1)
				return list[0].Annotations, nil
			},
			get: func(t *testing.T, fi *fakeInformers, name string) (map[string]string, error) {
				obj, err := fi.clusterAnalysisTemplate.Lister().Get(name)
				if err != nil {
					return nil, err
				}
				return obj.Annotations, nil
			},
			rawIndexer: func(fi *fakeInformers) cache.Indexer {
				return fi.clusterAnalysisTemplate.(*tolerantClusterAnalysisTemplateInformer).delegate.Informer().GetIndexer()
			},
			wrappedStore: func(fi *fakeInformers) cache.Store { return fi.clusterAnalysisTemplate.Informer().GetStore() },
		},
	}
}

// TestStoreUpdateWithUnstructuredDoesNotPanicList reproduces the notifications-engine
// pattern: after a dynamic client Patch it writes *unstructured.Unstructured into
// Informer().GetStore(). Typed listers that hard-cast panic (see prometheus scrape
// path in controller/metrics). Both the transformingInformer wrapper and coerceToTyped
// in List must keep this safe for every tolerant resource kind.
func TestStoreUpdateWithUnstructuredDoesNotPanicList(t *testing.T) {
	for _, tc := range storeUpdateCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			fi := newFakeDynamicInformer(tc.obj)

			patched := tc.obj.DeepCopy()
			patched.SetAnnotations(map[string]string{"notified": "true"})
			require.NoError(t, tc.wrappedStore(fi).Update(patched))

			raw, exists, err := tc.rawIndexer(fi).GetByKey(tc.cacheKey)
			require.NoError(t, err)
			require.True(t, exists)
			tc.assertTyped(t, raw)

			annotations, err := tc.list(t, fi)
			require.NoError(t, err)
			assert.Equal(t, "true", annotations["notified"])
		})
	}
}

// TestPoisonedCacheListRecovers coerces objects that were written to the raw
// indexer as Unstructured (bypassing both SetTransform and the Informer wrapper).
// Covers both List and namespaced/cluster Get — Get also goes through getTyped
// and was on the same hard-cast panic path before the fix.
func TestPoisonedCacheListRecovers(t *testing.T) {
	for _, tc := range storeUpdateCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			fi := newFakeDynamicInformer(tc.obj)

			poisoned := tc.obj.DeepCopy()
			poisoned.SetAnnotations(map[string]string{"poison": "1"})
			require.NoError(t, tc.rawIndexer(fi).Update(poisoned))

			annotations, err := tc.list(t, fi)
			require.NoError(t, err)
			assert.Equal(t, "1", annotations["poison"])

			annotations, err = tc.get(t, fi, tc.obj.GetName())
			require.NoError(t, err)
			assert.Equal(t, "1", annotations["poison"])
		})
	}
}

// TestListerReturnsIsolatedCopies guards the contract that objects returned from
// the tolerant listers can be mutated by callers without corrupting the shared
// informer cache. Real consumers (e.g. validation_references.go's
// setArgValuePlaceHolder / validateAnalysisMetrics) mutate the returned spec
// in-place; the lister must shield the cached object from those mutations.
func TestListerReturnsIsolatedCopies(t *testing.T) {
	at := testutil.ObjectFromPath("test/e2e/expectedfailures/malformed-analysistemplate.yaml")
	at.SetNamespace(dummyNamespace)
	fi := newFakeDynamicInformer(at)
	lister := fi.analysisTemplate.Lister().AnalysisTemplates(dummyNamespace)

	first, err := lister.Get("malformed-analysis")
	assert.NoError(t, err)
	assert.NotEmpty(t, first.Spec.Metrics, "fixture should have metrics to mutate")
	originalMetricName := first.Spec.Metrics[0].Name

	// Simulate the in-place mutations performed by validation_references.go and
	// other reconcile paths.
	dummy := "mutated"
	first.Spec.Args = append(first.Spec.Args, v1alpha1.Argument{Name: "synthetic", Value: &dummy})
	first.Spec.Metrics[0].Name = "mutated-metric-name"

	second, err := lister.Get("malformed-analysis")
	assert.NoError(t, err)
	assert.Empty(t, second.Spec.Args, "cached object should not see appended arg")
	assert.Equal(t, originalMetricName, second.Spec.Metrics[0].Name, "cached object should not see metric rename")
}
