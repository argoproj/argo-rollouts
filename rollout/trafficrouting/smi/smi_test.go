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

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/record"
)

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
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{},
	})
	assert.Nil(t, err)
	assert.Equal(t, Type, r.Type())
}

func TestUnsupportedTrafficSplitApiVersionError(t *testing.T) {
	ro := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	client := fake.NewSimpleClientset()
	defaults.SetSMIAPIVersion("does-not-exist")
	defer defaults.SetSMIAPIVersion(defaults.DefaultSMITrafficSplitVersion)
	_, err := NewReconciler(ReconcilerConfig{
		Rollout:        ro,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{},
	})
	assert.EqualError(t, err, "Unsupported TrafficSplit API version `does-not-exist`")
}

func TestReconcileCreateNewTrafficSplit(t *testing.T) {
	desiredWeight := int32(10)

	t.Run("v1alpha1", func(t *testing.T) {
		ro := fakeRollout("stable-service", "canary-service", "", "")
		client := fake.NewSimpleClientset()
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetWeight(desiredWeight)
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

		assert.Equal(t, objectMeta(ro.Name, ro, schema.GroupVersionKind{}), ts1.ObjectMeta) // If TrafficSplitName not set, then set to Rollout name
		assert.Equal(t, "stable-service", ts1.Spec.Service)                                 // // If root service not set, then set root service to be stable service
		assert.Equal(t, "canary-service", ts1.Spec.Backends[0].Service)
		assert.Equal(t, int64(desiredWeight), ts1.Spec.Backends[0].Weight.Value())
		assert.Equal(t, "stable-service", ts1.Spec.Backends[1].Service)
		assert.Equal(t, int64(100-desiredWeight), ts1.Spec.Backends[1].Weight.Value())
	})

	t.Run("v1alpha2", func(t *testing.T) {
		ro := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
		client := fake.NewSimpleClientset()
		defaults.SetSMIAPIVersion("v1alpha2")
		defer defaults.SetSMIAPIVersion(defaults.DefaultSMITrafficSplitVersion)
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetWeight(desiredWeight)
		assert.Nil(t, err)
		actions := client.Actions()
		assert.Len(t, actions, 2)
		assert.Equal(t, "get", actions[0].GetVerb())
		assert.Equal(t, "create", actions[1].GetVerb())

		obj := actions[1].(core.CreateAction).GetObject()
		ts2 := &smiv1alpha2.TrafficSplit{}
		converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
		objMap, _ := converter.ToUnstructured(obj)
		runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ts2)

		objectMeta := objectMeta("traffic-split-name", ro, r.cfg.ControllerKind)
		expectedTs2 := trafficSplitV1Alpha2(ro, objectMeta, "root-service", desiredWeight)
		assert.Equal(t, expectedTs2, ts2)
	})

	t.Run("v1alpha3", func(t *testing.T) {
		ro := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
		client := fake.NewSimpleClientset()
		defaults.SetSMIAPIVersion("v1alpha3")
		defer defaults.SetSMIAPIVersion(defaults.DefaultSMITrafficSplitVersion)
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetWeight(desiredWeight)
		assert.Nil(t, err)
		actions := client.Actions()
		assert.Len(t, actions, 2)
		assert.Equal(t, "get", actions[0].GetVerb())
		assert.Equal(t, "create", actions[1].GetVerb())

		obj := actions[1].(core.CreateAction).GetObject()
		ts3 := &smiv1alpha3.TrafficSplit{}
		converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
		objMap, _ := converter.ToUnstructured(obj)
		runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ts3)

		objectMeta := objectMeta("traffic-split-name", ro, r.cfg.ControllerKind)
		expectedTs3 := trafficSplitV1Alpha3(ro, objectMeta, "root-service", desiredWeight)
		assert.Equal(t, expectedTs3, ts3)
	})
}

func TestReconcilePatchExistingTrafficSplit(t *testing.T) {
	ro := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	objectMeta := objectMeta("traffic-split-name", ro, schema.GroupVersionKind{})

	t.Run("v1alpha1", func(t *testing.T) {
		ts1 := trafficSplitV1Alpha1(ro, objectMeta, "root-service", int32(10))
		client := fake.NewSimpleClientset(ts1)
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetWeight(50)
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
	})

	t.Run("v1alpha2", func(t *testing.T) {
		ts2 := trafficSplitV1Alpha2(ro, objectMeta, "root-service", int32(10))
		client := fake.NewSimpleClientset(ts2)
		defaults.SetSMIAPIVersion("v1alpha2")
		defer defaults.SetSMIAPIVersion(defaults.DefaultSMITrafficSplitVersion)
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetWeight(50)
		assert.Nil(t, err)

		actions := client.Actions()
		assert.Len(t, actions, 2)
		assert.Equal(t, "get", actions[0].GetVerb())
		assert.Equal(t, "patch", actions[1].GetVerb())

		patchAction := actions[1].(core.PatchAction)
		ts2Patched := &smiv1alpha2.TrafficSplit{}
		err = json.Unmarshal(patchAction.GetPatch(), &ts2Patched)
		if err != nil {
			panic(err)
		}

		assert.Equal(t, 50, ts2Patched.Spec.Backends[0].Weight)
		assert.Equal(t, 50, ts2Patched.Spec.Backends[1].Weight)
	})

	t.Run("v1alpha3", func(t *testing.T) {
		ts3 := trafficSplitV1Alpha3(ro, objectMeta, "root-service", int32(10))
		client := fake.NewSimpleClientset(ts3)
		defaults.SetSMIAPIVersion("v1alpha3")
		defer defaults.SetSMIAPIVersion(defaults.DefaultSMITrafficSplitVersion)
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetWeight(50)
		assert.Nil(t, err)

		actions := client.Actions()
		assert.Len(t, actions, 2)
		assert.Equal(t, "get", actions[0].GetVerb())
		assert.Equal(t, "patch", actions[1].GetVerb())

		patchAction := actions[1].(core.PatchAction)
		ts3Patched := &smiv1alpha3.TrafficSplit{}
		err = json.Unmarshal(patchAction.GetPatch(), &ts3Patched)
		if err != nil {
			panic(err)
		}

		assert.Equal(t, 50, ts3Patched.Spec.Backends[0].Weight)
		assert.Equal(t, 50, ts3Patched.Spec.Backends[1].Weight)
	})
}

func TestReconcilePatchExistingTrafficSplitNoChange(t *testing.T) {
	t.Run("v1alpha1", func(t *testing.T) {
		ro := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-v1alpha1")
		objMeta := objectMeta("traffic-split-v1alpha1", ro, schema.GroupVersionKind{})
		ts1 := trafficSplitV1Alpha1(ro, objMeta, "root-service", int32(10))
		client := fake.NewSimpleClientset(ts1)
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		buf := bytes.NewBufferString("")
		logger := log.New()
		logger.SetOutput(buf)
		r.log.Logger = logger
		err = r.SetWeight(10)
		assert.Nil(t, err)
		logMessage := buf.String()
		assert.True(t, strings.Contains(logMessage, "Traffic Split `traffic-split-v1alpha1` was not modified"))

	})

	t.Run("v1alpha2", func(t *testing.T) {
		ro := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-v1alpha2")
		objMeta := objectMeta("traffic-split-v1alpha2", ro, schema.GroupVersionKind{})
		ts2 := trafficSplitV1Alpha2(ro, objMeta, "root-service", int32(10))
		client := fake.NewSimpleClientset(ts2)
		defaults.SetSMIAPIVersion("v1alpha2")
		defer defaults.SetSMIAPIVersion(defaults.DefaultSMITrafficSplitVersion)
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		buf := bytes.NewBufferString("")
		logger := log.New()
		logger.SetOutput(buf)
		r.log.Logger = logger
		err = r.SetWeight(10)
		assert.Nil(t, err)
		logMessage := buf.String()
		assert.True(t, strings.Contains(logMessage, "Traffic Split `traffic-split-v1alpha2` was not modified"))
	})

	t.Run("v1alpha3", func(t *testing.T) {
		ro := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-v1alpha3")
		objMeta := objectMeta("traffic-split-v1alpha3", ro, schema.GroupVersionKind{})
		ts3 := trafficSplitV1Alpha3(ro, objMeta, "root-service", int32(10))
		client := fake.NewSimpleClientset(ts3)
		defaults.SetSMIAPIVersion("v1alpha3")
		defer defaults.SetSMIAPIVersion(defaults.DefaultSMITrafficSplitVersion)
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		buf := bytes.NewBufferString("")
		logger := log.New()
		logger.SetOutput(buf)
		r.log.Logger = logger
		err = r.SetWeight(10)
		assert.Nil(t, err)
		logMessage := buf.String()
		assert.True(t, strings.Contains(logMessage, "Traffic Split `traffic-split-v1alpha3` was not modified"))
	})
}

func TestReconcileGetTrafficSplitError(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	client := fake.NewSimpleClientset()
	r, err := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       record.NewFakeEventRecorder(),
		ControllerKind: schema.GroupVersionKind{},
	})
	assert.Nil(t, err)
	//Throw error when client tries to get TrafficSplit
	client.ReactionChain = nil
	r.cfg.Client.(*fake.Clientset).Fake.AddReactor("get", "trafficsplits", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, k8serrors.NewServerTimeout(schema.GroupResource{Group: "split.smi-spec.io", Resource: "trafficsplits"}, "get", 0)
	})
	err = r.SetWeight(10)
	assert.NotNil(t, err)
	assert.True(t, k8serrors.IsServerTimeout(err))
}

func TestReconcileRolloutDoesNotOwnTrafficSplitError(t *testing.T) {
	ro := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	objMeta := objectMeta("traffic-split-name", ro, schema.GroupVersionKind{})

	t.Run("v1alpha1", func(t *testing.T) {
		ts1 := trafficSplitV1Alpha1(ro, objMeta, "root-service", int32(10))
		ts1.OwnerReferences = nil

		client := fake.NewSimpleClientset(ts1)
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetWeight(10)
		assert.EqualError(t, err, "Rollout does not own TrafficSplit `traffic-split-name`")
	})

	t.Run("v1alpha2", func(t *testing.T) {
		ts2 := trafficSplitV1Alpha2(ro, objMeta, "root-service", int32(10))
		ts2.OwnerReferences = nil
		defaults.SetSMIAPIVersion("v1alpha2")
		defer defaults.SetSMIAPIVersion(defaults.DefaultSMITrafficSplitVersion)

		client := fake.NewSimpleClientset(ts2)
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetWeight(10)
		assert.EqualError(t, err, "Rollout does not own TrafficSplit `traffic-split-name`")
	})

	t.Run("v1alpha3", func(t *testing.T) {
		ts3 := trafficSplitV1Alpha3(ro, objMeta, "root-service", int32(10))
		ts3.OwnerReferences = nil
		defaults.SetSMIAPIVersion("v1alpha3")
		defer defaults.SetSMIAPIVersion(defaults.DefaultSMITrafficSplitVersion)

		client := fake.NewSimpleClientset(ts3)
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetWeight(10)
		assert.EqualError(t, err, "Rollout does not own TrafficSplit `traffic-split-name`")
	})
}

func TestCreateTrafficSplitForMultipleBackends(t *testing.T) {
	ro := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	weightDestinations := []v1alpha1.WeightDestination{
		{
			ServiceName:     "ex-svc-1",
			PodTemplateHash: "",
			Weight:          5,
		},
		{
			ServiceName:     "ex-svc-2",
			PodTemplateHash: "",
			Weight:          5,
		},
	}

	t.Run("v1alpha1", func(t *testing.T) {
		client := fake.NewSimpleClientset()
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetWeight(10, weightDestinations...)
		assert.Nil(t, err)

		actions := client.Actions()
		assert.Len(t, actions, 2)
		assert.Equal(t, "get", actions[0].GetVerb())
		assert.Equal(t, "create", actions[1].GetVerb())

		// Get newly created TrafficSplit
		obj := actions[1].(core.CreateAction).GetObject()
		ts1 := &smiv1alpha1.TrafficSplit{}
		converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
		objMap, _ := converter.ToUnstructured(obj)
		runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ts1)

		// check canary backend
		assert.Equal(t, "canary-service", ts1.Spec.Backends[0].Service)
		assert.Equal(t, int64(10), ts1.Spec.Backends[0].Weight.Value())

		// check experiment service backends
		assert.Equal(t, weightDestinations[0].ServiceName, ts1.Spec.Backends[1].Service)
		assert.Equal(t, int64(weightDestinations[0].Weight), ts1.Spec.Backends[1].Weight.Value())

		assert.Equal(t, weightDestinations[1].ServiceName, ts1.Spec.Backends[2].Service)
		assert.Equal(t, int64(weightDestinations[1].Weight), ts1.Spec.Backends[2].Weight.Value())

		// check stable backend
		assert.Equal(t, "stable-service", ts1.Spec.Backends[3].Service)
		assert.Equal(t, int64(80), ts1.Spec.Backends[3].Weight.Value())
	})

	t.Run("v1alpha2", func(t *testing.T) {
		defaults.SetSMIAPIVersion("v1alpha2")
		defer defaults.SetSMIAPIVersion(defaults.DefaultSMITrafficSplitVersion)

		client := fake.NewSimpleClientset()
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetWeight(10, weightDestinations...)
		assert.Nil(t, err)

		actions := client.Actions()
		assert.Len(t, actions, 2)
		assert.Equal(t, "get", actions[0].GetVerb())
		assert.Equal(t, "create", actions[1].GetVerb())

		// Get newly created TrafficSplit
		obj := actions[1].(core.CreateAction).GetObject()
		ts2 := &smiv1alpha2.TrafficSplit{}
		converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
		objMap, _ := converter.ToUnstructured(obj)
		runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ts2)

		// check canary backend
		assert.Equal(t, "canary-service", ts2.Spec.Backends[0].Service)
		assert.Equal(t, 10, ts2.Spec.Backends[0].Weight)

		// check experiment service backends
		assert.Equal(t, weightDestinations[0].ServiceName, ts2.Spec.Backends[1].Service)
		assert.Equal(t, int(weightDestinations[0].Weight), ts2.Spec.Backends[1].Weight)

		assert.Equal(t, weightDestinations[1].ServiceName, ts2.Spec.Backends[2].Service)
		assert.Equal(t, int(weightDestinations[1].Weight), ts2.Spec.Backends[2].Weight)

		// check stable backend
		assert.Equal(t, "stable-service", ts2.Spec.Backends[3].Service)
		assert.Equal(t, 80, ts2.Spec.Backends[3].Weight)
	})

	t.Run("v1alpha3", func(t *testing.T) {
		defaults.SetSMIAPIVersion("v1alpha3")
		defer defaults.SetSMIAPIVersion(defaults.DefaultSMITrafficSplitVersion)

		client := fake.NewSimpleClientset()
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetWeight(10, weightDestinations...)
		assert.Nil(t, err)

		actions := client.Actions()
		assert.Len(t, actions, 2)
		assert.Equal(t, "get", actions[0].GetVerb())
		assert.Equal(t, "create", actions[1].GetVerb())

		// Get newly created TrafficSplit
		obj := actions[1].(core.CreateAction).GetObject()
		ts3 := &smiv1alpha3.TrafficSplit{}
		converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
		objMap, _ := converter.ToUnstructured(obj)
		runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ts3)

		// check canary backend
		assert.Equal(t, "canary-service", ts3.Spec.Backends[0].Service)
		assert.Equal(t, 10, ts3.Spec.Backends[0].Weight)

		// check experiment service backends
		assert.Equal(t, weightDestinations[0].ServiceName, ts3.Spec.Backends[1].Service)
		assert.Equal(t, int(weightDestinations[0].Weight), ts3.Spec.Backends[1].Weight)

		assert.Equal(t, weightDestinations[1].ServiceName, ts3.Spec.Backends[2].Service)
		assert.Equal(t, int(weightDestinations[1].Weight), ts3.Spec.Backends[2].Weight)

		// check stable backend
		assert.Equal(t, "stable-service", ts3.Spec.Backends[3].Service)
		assert.Equal(t, 80, ts3.Spec.Backends[3].Weight)
	})
}

func TestReconcileSetHeaderRoute(t *testing.T) {
	t.Run("not implemented", func(t *testing.T) {
		ro := fakeRollout("stable-service", "canary-service", "", "")
		client := fake.NewSimpleClientset()
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name: "set-header",
			Match: []v1alpha1.HeaderRoutingMatch{{
				HeaderName: "header-name",
				HeaderValue: &v1alpha1.StringMatch{
					Exact: "value",
				},
			}},
		})
		assert.Nil(t, err)

		err = r.RemoveManagedRoutes()
		assert.Nil(t, err)

		actions := client.Actions()
		assert.Len(t, actions, 0)
	})
}

func TestReconcileSetMirrorRoute(t *testing.T) {
	t.Run("not implemented", func(t *testing.T) {
		ro := fakeRollout("stable-service", "canary-service", "", "")
		client := fake.NewSimpleClientset()
		r, err := NewReconciler(ReconcilerConfig{
			Rollout:        ro,
			Client:         client,
			Recorder:       record.NewFakeEventRecorder(),
			ControllerKind: schema.GroupVersionKind{},
		})
		assert.Nil(t, err)

		err = r.SetMirrorRoute(&v1alpha1.SetMirrorRoute{
			Name: "mirror-route",
			Match: []v1alpha1.RouteMatch{{
				Method: &v1alpha1.StringMatch{Exact: "GET"},
			}},
		})
		assert.Nil(t, err)

		err = r.RemoveManagedRoutes()
		assert.Nil(t, err)

		actions := client.Actions()
		assert.Len(t, actions, 0)
	})
}
