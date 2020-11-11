package analysis

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/undefinedlabs/go-mpatch"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	core "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/metricproviders"
	"github.com/argoproj/argo-rollouts/metricproviders/mocks"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
)

var (
	noResyncPeriodFunc = func() time.Duration { return 0 }
)

type fixture struct {
	t *testing.T

	client     *fake.Clientset
	kubeclient *k8sfake.Clientset

	// Objects to put in the store.
	analysisRunLister []*v1alpha1.AnalysisRun
	// Actions expected to happen on the client.
	actions []core.Action
	// Objects from here preloaded into NewSimpleFake.
	objects         []runtime.Object
	enqueuedObjects map[string]int
	unfreezeTime    func() error
	// fake provider
	provider *mocks.Provider
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{}
	f.t = t
	f.objects = []runtime.Object{}
	f.enqueuedObjects = make(map[string]int)
	now := time.Now()
	patch, err := mpatch.PatchMethod(time.Now, func() time.Time {
		return now
	})
	assert.NoError(t, err)
	f.unfreezeTime = patch.Unpatch
	return f
}

func (f *fixture) Close() {
	f.unfreezeTime()
}

func getKey(analysisRun *v1alpha1.AnalysisRun, t *testing.T) string {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(analysisRun)
	if err != nil {
		t.Errorf("Unexpected error getting key for analysisRun %v: %v", analysisRun.Name, err)
		return ""
	}
	return key
}

type resyncFunc func() time.Duration

func (f *fixture) newController(resync resyncFunc) (*Controller, informers.SharedInformerFactory, kubeinformers.SharedInformerFactory) {
	f.client = fake.NewSimpleClientset(f.objects...)
	f.kubeclient = k8sfake.NewSimpleClientset()

	i := informers.NewSharedInformerFactory(f.client, resync())
	k8sI := kubeinformers.NewSharedInformerFactory(f.kubeclient, resync())

	analysisRunWorkqueue := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "AnalysisRuns")

	metricsServer := metrics.NewMetricsServer(metrics.ServerConfig{
		Addr:               "localhost:8080",
		K8SRequestProvider: &metrics.K8sRequestsCountProvider{},
	})

	c := NewController(ControllerConfig{
		KubeClientSet:        f.kubeclient,
		ArgoProjClientset:    f.client,
		AnalysisRunInformer:  i.Argoproj().V1alpha1().AnalysisRuns(),
		JobInformer:          k8sI.Batch().V1().Jobs(),
		ResyncPeriod:         resync(),
		AnalysisRunWorkQueue: analysisRunWorkqueue,
		MetricsServer:        metricsServer,
		Recorder:             &record.FakeRecorder{},
	})

	c.enqueueAnalysis = func(obj interface{}) {
		var key string
		var err error
		if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
			panic(err)
		}
		count, ok := f.enqueuedObjects[key]
		if !ok {
			count = 0
		}
		count++
		f.enqueuedObjects[key] = count
		c.analysisRunWorkQueue.Add(obj)
	}
	c.enqueueAnalysisAfter = func(obj interface{}, duration time.Duration) {
		c.enqueueAnalysis(obj)
	}
	f.provider = &mocks.Provider{}
	c.newProvider = func(logCtx log.Entry, metric v1alpha1.Metric) (metricproviders.Provider, error) {
		return f.provider, nil
	}

	for _, ar := range f.analysisRunLister {
		i.Argoproj().V1alpha1().AnalysisRuns().Informer().GetIndexer().Add(ar)
	}

	return c, i, k8sI
}

func (f *fixture) run(analysisRunName string) {
	c, i, k8sI := f.newController(noResyncPeriodFunc)
	f.runController(analysisRunName, true, false, c, i, k8sI)
}

func (f *fixture) runExpectError(analysisRunName string, startInformers bool) {
	c, i, k8sI := f.newController(noResyncPeriodFunc)
	f.runController(analysisRunName, startInformers, true, c, i, k8sI)
}

func (f *fixture) runController(analysisRunName string, startInformers bool, expectError bool, c *Controller, i informers.SharedInformerFactory, k8sI kubeinformers.SharedInformerFactory) *Controller {
	if startInformers {
		stopCh := make(chan struct{})
		defer close(stopCh)
		i.Start(stopCh)
		k8sI.Start(stopCh)

		assert.True(f.t, cache.WaitForCacheSync(stopCh, c.analysisRunSynced))
	}

	err := c.syncHandler(analysisRunName)
	if !expectError && err != nil {
		f.t.Errorf("error syncing experiment: %v", err)
	} else if expectError && err == nil {
		f.t.Error("expected error syncing experiment, got nil")
	}

	actions := filterInformerActions(f.client.Actions())
	for i, action := range actions {
		if len(f.actions) < i+1 {
			actionsBytes, _ := json.Marshal(actions[i:])
			f.t.Errorf("%d unexpected actions: %+v", len(actions)-len(f.actions), string(actionsBytes))
			break
		}

		expectedAction := f.actions[i]
		checkAction(expectedAction, action, f.t)
	}

	if len(f.actions) > len(actions) {
		f.t.Errorf("%d expected actions did not occur:%+v", len(f.actions)-len(actions), f.actions[len(actions):])
	}

	// k8sActions := filterInformerActions(f.kubeclient.Actions())
	// for i, action := range k8sActions {
	// 	if len(f.kubeactions) < i+1 {
	// 		f.t.Errorf("%d unexpected actions: %+v", len(k8sActions)-len(f.kubeactions), k8sActions[i:])
	// 		break
	// 	}

	// 	expectedAction := f.kubeactions[i]
	// 	checkAction(expectedAction, action, f.t)
	// }

	// if len(f.kubeactions) > len(k8sActions) {
	// 	f.t.Errorf("%d expected actions did not occur:%+v", len(f.kubeactions)-len(k8sActions), f.kubeactions[len(k8sActions):])
	// }
	return c
}

// checkAction verifies that expected and actual actions are equal
func checkAction(expected, actual core.Action, t *testing.T) {
	if !(expected.Matches(actual.GetVerb(), actual.GetResource().Resource) && actual.GetSubresource() == expected.GetSubresource()) {
		t.Errorf("Expected\n\t%#v\ngot\n\t%#v", expected, actual)
		if patch, ok := actual.(core.PatchAction); ok {
			patchBytes := patch.GetPatch()
			t.Errorf("Patch Received: %s", string(patchBytes))
		}
		if patch, ok := expected.(core.PatchAction); ok {
			patchBytes := patch.GetPatch()
			t.Errorf("Expected Patch: %s", string(patchBytes))
		}
		return
	}

	if reflect.TypeOf(actual) != reflect.TypeOf(expected) {
		t.Errorf("Action has wrong type. Expected: %t. Got: %t", expected, actual)
		return
	}
}

// filterInformerActions filters list, and watch actions for testing resources.
// Since list, and watch don't change resource state we can filter it to lower
// nose level in our tests.
func filterInformerActions(actions []core.Action) []core.Action {
	ret := []core.Action{}
	for _, action := range actions {
		if action.Matches("list", "analysisruns") ||
			action.Matches("watch", "analysisruns") ||
			action.Matches("list", "rollouts") ||
			action.Matches("watch", "rollouts") {
			continue
		}
		ret = append(ret, action)
	}

	return ret
}

func (f *fixture) expectUpdateAnalysisRunAction(analysisRun *v1alpha1.AnalysisRun) int {
	action := core.NewUpdateAction(schema.GroupVersionResource{Resource: "analysisrun"}, analysisRun.Namespace, analysisRun)
	len := len(f.actions)
	f.actions = append(f.actions, action)
	return len
}

func (f *fixture) getUpdatedAnalysisRun(index int) *v1alpha1.AnalysisRun {
	action := filterInformerActions(f.client.Actions())[index]
	updateAction, ok := action.(core.UpdateAction)
	if !ok {
		assert.Fail(f.t, "Expected Update action, not %s", action.GetVerb())
	}
	obj := updateAction.GetObject()
	ar := &v1alpha1.AnalysisRun{}
	converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ := converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ar)
	return ar
}

func (f *fixture) expectPatchAnalysisRunAction(analysisRun *v1alpha1.AnalysisRun) int {
	analysisRunSchema := schema.GroupVersionResource{
		Resource: "analysisruns",
		Version:  "v1alpha1",
	}
	len := len(f.actions)
	f.actions = append(f.actions, core.NewPatchAction(analysisRunSchema, analysisRun.Namespace, analysisRun.Name, types.MergePatchType, nil))
	return len
}

func (f *fixture) getPatchedAnalysisRun(index int) v1alpha1.AnalysisRun {
	action := filterInformerActions(f.client.Actions())[index]
	patchAction, ok := action.(core.PatchAction)
	if !ok {
		f.t.Fatalf("Expected Patch action, not %s", action.GetVerb())
	}
	ar := v1alpha1.AnalysisRun{}
	err := json.Unmarshal(patchAction.GetPatch(), &ar)
	if err != nil {
		panic(err)
	}
	return ar
}

func TestNoReconcileForNotFoundAnalysisRun(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	ar := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
	}
	f.run(getKey(ar, t))
}

func TestNoReconcileForAnalysisRunWithDeletionTimestamp(t *testing.T) {
	f := newFixture(t)
	defer f.Close()

	ar := &v1alpha1.AnalysisRun{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: metav1.NamespaceDefault,
		},
	}
	now := metav1.Now()
	ar.DeletionTimestamp = &now

	f.analysisRunLister = append(f.analysisRunLister, ar)
	f.objects = append(f.objects, ar)

	f.run(getKey(ar, t))
}
