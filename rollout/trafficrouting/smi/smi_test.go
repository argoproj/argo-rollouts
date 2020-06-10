package smi

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	smiv1alpha1 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	smiv1alpha2 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha2"
	smiv1alpha3 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha3"
	fake "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned/fake"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	core "k8s.io/client-go/testing"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

var rootSvc = "root-service"
var stableSvc = "stable-service"
var canarySvc = "canary-service"
var trafficSplitName = "traffic-split-name"
var controllerKind = schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"}

func fakeRollout(stableSvc, canarySvc, rootSvc string, trafficSplitName string) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: metav1.NamespaceDefault,
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: stableSvc,
					CanaryService: canarySvc,
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						SMI: &v1alpha1.SMITrafficRouting{
							RootService:      rootSvc,
							TrafficSplitName: trafficSplitName,
						},
					},
				},
			},
		},
	}
}

func TestType(t *testing.T) {
	client := fake.NewSimpleClientset()
	rollout := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
	})
	assert.Equal(t, Type, r.Type())
}

func TestReconcileCreateNewTrafficSplit(t *testing.T) {
	desiredWeight := int32(10)
	ro := fakeRollout(stableSvc, canarySvc, rootSvc, trafficSplitName)
	client := fake.NewSimpleClientset()
	r := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		ApiVersion:     "v1alpha1",
	})
	// v1alpha1
	err := r.Reconcile(desiredWeight)
	assert.Nil(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 2)
	assert.Equal(t, "get", actions[0].GetVerb())
	assert.Equal(t, "create", actions[1].GetVerb())

	obj := actions[1].(core.CreateAction).GetObject()
	ts1 := &smiv1alpha1.TrafficSplit{}
	converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ := converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ts1)

	//assert.Equal(t, expectedTS.ObjectMeta, ts.ObjectMeta)
	assert.Equal(t, rootSvc, ts1.Spec.Service)
	assert.Equal(t, canarySvc, ts1.Spec.Backends[0].Service)
	assert.Equal(t, int64(desiredWeight), ts1.Spec.Backends[0].Weight.Value())
	assert.Equal(t, stableSvc, ts1.Spec.Backends[1].Service)
	assert.Equal(t, int64(100-desiredWeight), ts1.Spec.Backends[1].Weight.Value())

	// v1alpha2
	r = NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		ApiVersion:     "v1alpha2",
	})
	client.ClearActions()

	err = r.Reconcile(desiredWeight)
	assert.Nil(t, err)
	actions = client.Actions()
	assert.Len(t, actions, 2)
	assert.Equal(t, "get", actions[0].GetVerb())
	assert.Equal(t, "create", actions[1].GetVerb())

	obj = actions[1].(core.CreateAction).GetObject()
	ts2 := &smiv1alpha2.TrafficSplit{}
	converter = runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ = converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ts2)

	objectMeta := objectMeta(trafficSplitName, ro, r.cfg.ControllerKind)
	expectedTs2 := trafficSplitV1Alpha2(ro, objectMeta, rootSvc, desiredWeight)
	assert.Equal(t, expectedTs2, *ts2)

	// v1alpha3
	r = NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		ApiVersion:     "v1alpha3",
	})
	client.ClearActions()

	err = r.Reconcile(desiredWeight)
	assert.Nil(t, err)
	//actions = client.Actions()
	//assert.Len(t, actions, 2)
	//assert.Equal(t, "get", actions[0].GetVerb())
	//assert.Equal(t, "create", actions[1].GetVerb())

	obj = actions[1].(core.CreateAction).GetObject()
	ts3 := &smiv1alpha3.TrafficSplit{}
	converter = runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ = converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ts3)

	expectedTs3 := trafficSplitV1Alpha3(ro, objectMeta, rootSvc, desiredWeight)
	assert.Equal(t, expectedTs3, *ts3)
}

func TestReconcilePatchExistingTrafficSplit(t *testing.T) {
	ro := fakeRollout(stableSvc, canarySvc, rootSvc, "traffic-split-name")
	objectMeta := objectMeta("traffic-split-name", ro, controllerKind)

	// v1alpha1
	ts1 := trafficSplitV1Alpha1(ro, objectMeta, rootSvc, int32(10))
	client := fake.NewSimpleClientset(&ts1)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		ApiVersion:     "v1alpha1",
	})

	err := r.Reconcile(50)
	assert.Nil(t, err)

	actions := client.Actions()
	assert.Len(t, actions, 2)
	assert.Equal(t, "get", actions[0].GetVerb())
	assert.Equal(t, "patch", actions[1].GetVerb())

	patchAction := actions[1].(core.PatchAction)
	ts1Patched := &smiv1alpha1.TrafficSplit{}
	err = json.Unmarshal(patchAction.GetPatch(), &ts1Patched)
	if err != nil {
		panic(err)
	}
	canaryWeight, isInt64 := ts1Patched.Spec.Backends[0].Weight.AsInt64()
	assert.True(t, isInt64)
	stableWeight, isInt64 := ts1Patched.Spec.Backends[1].Weight.AsInt64()
	assert.True(t, isInt64)

	assert.Equal(t, int64(50), canaryWeight)
	assert.Equal(t, int64(50), stableWeight)

	// v1alpha2
	ts2 := trafficSplitV1Alpha2(ro, objectMeta, rootSvc, int32(10))
	client = fake.NewSimpleClientset(&ts2)
	r = NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		ApiVersion:     "v1alpha2",
	})

	err = r.Reconcile(50)
	assert.Nil(t, err)

	actions = client.Actions()
	assert.Len(t, actions, 2)
	assert.Equal(t, "get", actions[0].GetVerb())
	assert.Equal(t, "patch", actions[1].GetVerb())

	patchAction = actions[1].(core.PatchAction)
	ts2Patched := &smiv1alpha2.TrafficSplit{}
	err = json.Unmarshal(patchAction.GetPatch(), &ts2Patched)
	if err != nil {
		panic(err)
	}

	assert.Equal(t, 50, ts2Patched.Spec.Backends[0].Weight)
	assert.Equal(t, 50, ts2Patched.Spec.Backends[1].Weight)

	// v1alpha3
	ts3 := trafficSplitV1Alpha3(ro, objectMeta, rootSvc, int32(10))
	client = fake.NewSimpleClientset(&ts3)
	r = NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"},
		ApiVersion:     "v1alpha3",
	})

	err = r.Reconcile(50)
	assert.Nil(t, err)

	actions = client.Actions()
	assert.Len(t, actions, 2)
	assert.Equal(t, "get", actions[0].GetVerb())
	assert.Equal(t, "patch", actions[1].GetVerb())

	patchAction = actions[1].(core.PatchAction)
	ts3Patched := &smiv1alpha3.TrafficSplit{}
	err = json.Unmarshal(patchAction.GetPatch(), &ts3Patched)
	if err != nil {
		panic(err)
	}

	assert.Equal(t, 50, ts3Patched.Spec.Backends[0].Weight)
	assert.Equal(t, 50, ts3Patched.Spec.Backends[1].Weight)
}

func TestReconcilePatchExistingTrafficSplitNoChange(t *testing.T) {
	// v1alpha1
	ro := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-v1alpha1")
	objMeta := objectMeta("traffic-split-v1alpha1", ro, controllerKind)
	ts1 := trafficSplitV1Alpha1(ro, objMeta, rootSvc, int32(10))
	client := fake.NewSimpleClientset(&ts1)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: controllerKind,
		ApiVersion:     "v1alpha1",
	})
	buf := bytes.NewBufferString("")
	logger := log.New()
	logger.SetOutput(buf)
	r.log.Logger = logger
	err := r.Reconcile(10)
	assert.Nil(t, err)
	logMessage := buf.String()
	assert.True(t, strings.Contains(logMessage, "Traffic Split `traffic-split-v1alpha1` was not modified"))

	// v1alpha2
	ro = fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-v1alpha2")
	objMeta = objectMeta("traffic-split-v1alpha2", ro, controllerKind)
	ts2 := trafficSplitV1Alpha2(ro, objMeta, rootSvc, int32(10))
	client = fake.NewSimpleClientset(&ts2)
	r = NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: controllerKind,
		ApiVersion:     "v1alpha2",
	})
	buf = bytes.NewBufferString("")
	logger = log.New()
	logger.SetOutput(buf)
	r.log.Logger = logger
	err = r.Reconcile(10)
	assert.Nil(t, err)
	logMessage = buf.String()
	assert.True(t, strings.Contains(logMessage, "Traffic Split `traffic-split-v1alpha2` was not modified"))

	// v1alpha3
	ro = fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-v1alpha3")
	objMeta = objectMeta("traffic-split-v1alpha3", ro, controllerKind)
	ts3 := trafficSplitV1Alpha3(ro, objMeta, rootSvc, int32(10))
	client = fake.NewSimpleClientset(&ts3)
	r = NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: controllerKind,
		ApiVersion:     "v1alpha3",
	})
	buf = bytes.NewBufferString("")
	logger = log.New()
	logger.SetOutput(buf)
	r.log.Logger = logger
	err = r.Reconcile(10)
	assert.Nil(t, err)
	logMessage = buf.String()
	assert.True(t, strings.Contains(logMessage, "Traffic Split `traffic-split-v1alpha3` was not modified"))
}

func TestReconcileGetTrafficSplitError(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	client := fake.NewSimpleClientset()
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: controllerKind,
		ApiVersion:     "v1alpha1",
	})
	//Throw error when client tries to get TrafficSplit
	client.ReactionChain = nil
	r.cfg.Client.(*fake.Clientset).Fake.AddReactor("get", "trafficsplits", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, k8serrors.NewServerTimeout(schema.GroupResource{Group: "split.smi-spec.io", Resource: "trafficsplits"}, "get", 0)
	})
	err := r.Reconcile(10)
	assert.NotNil(t, err)
	assert.True(t, k8serrors.IsServerTimeout(err))
}

func TestReconcileRolloutDoesNotOwnTrafficSplitError(t *testing.T) {
	ro := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	objMeta := objectMeta("traffic-split-name", ro, controllerKind)

	// v1alpha1
	ts1 := trafficSplitV1Alpha1(ro, objMeta, rootSvc, int32(10))
	ts1.OwnerReferences = nil

	client := fake.NewSimpleClientset(&ts1)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: controllerKind,
		ApiVersion:     "v1alpha1",
	})
	err := r.Reconcile(10)
	assert.EqualError(t, err, "Rollout does not own TrafficSplit 'traffic-split-name'")

	// v1alpha2
	ts2 := trafficSplitV1Alpha2(ro, objMeta, rootSvc, int32(10))
	ts2.OwnerReferences = nil

	client = fake.NewSimpleClientset(&ts2)
	r = NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: controllerKind,
		ApiVersion:     "v1alpha2",
	})
	err = r.Reconcile(10)
	assert.EqualError(t, err, "Rollout does not own TrafficSplit 'traffic-split-name'")

	// v1alpha3
	ts3 := trafficSplitV1Alpha3(ro, objMeta, rootSvc, int32(10))
	ts3.OwnerReferences = nil

	client = fake.NewSimpleClientset(&ts3)
	r = NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: controllerKind,
		ApiVersion:     "v1alpha3",
	})
	err = r.Reconcile(10)
	assert.EqualError(t, err, "Rollout does not own TrafficSplit 'traffic-split-name'")
}
