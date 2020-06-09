package smi

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	smiv1alpha1 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
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
		ControllerKind: controllerKind,
	})
	assert.Equal(t, Type, r.Type())
}

func TestReconcileCreateNewTrafficSplit(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	client := fake.NewSimpleClientset()
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: controllerKind,
	})
	err := r.Reconcile(10)
	assert.Nil(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 2)
	assert.Equal(t, "get", actions[0].GetVerb())
	assert.Equal(t, "create", actions[1].GetVerb())

	obj := actions[1].(core.CreateAction).GetObject()
	ts := &smiv1alpha1.TrafficSplit{}
	converter := runtime.NewTestUnstructuredConverter(equality.Semantic)
	objMap, _ := converter.ToUnstructured(obj)
	runtime.NewTestUnstructuredConverter(equality.Semantic).FromUnstructured(objMap, ts)

	expectedTS := createTrafficSplit(rollout, 10, controllerKind)

	assert.Equal(t, expectedTS.TypeMeta, ts.TypeMeta)
	assert.Equal(t, expectedTS.ObjectMeta, ts.ObjectMeta)
	assert.Equal(t, expectedTS.Spec.Service, ts.Spec.Service)
	assert.Equal(t, expectedTS.Spec.Backends[0].Service, ts.Spec.Backends[0].Service)
	assert.Equal(t, expectedTS.Spec.Backends[0].Weight.Value(), ts.Spec.Backends[0].Weight.Value())
	assert.Equal(t, expectedTS.Spec.Backends[1].Service, ts.Spec.Backends[1].Service)
	assert.Equal(t, expectedTS.Spec.Backends[1].Weight.Value(), ts.Spec.Backends[1].Weight.Value())
}

func TestReconcilePatchExistingTrafficSplit(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	trafficSplit := createTrafficSplit(rollout, 5, controllerKind)
	client := fake.NewSimpleClientset(trafficSplit)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: controllerKind,
	})

	err := r.Reconcile(10)
	assert.Nil(t, err)

	actions := client.Actions()
	assert.Len(t, actions, 2)
	assert.Equal(t, "get", actions[0].GetVerb())
	assert.Equal(t, "patch", actions[1].GetVerb())

	patchAction := actions[1].(core.PatchAction)
	ts := &smiv1alpha1.TrafficSplit{}
	err = json.Unmarshal(patchAction.GetPatch(), &ts)
	if err != nil {
		panic(err)
	}
	canaryWeight, isInt64 := ts.Spec.Backends[0].Weight.AsInt64()
	assert.True(t, isInt64)
	stableWeight, isInt64 := ts.Spec.Backends[1].Weight.AsInt64()
	assert.True(t, isInt64)

	assert.Equal(t, int64(10), canaryWeight)
	assert.Equal(t, int64(90), stableWeight)
}

func TestReconcilePatchExistingTrafficSplitNoChange(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	trafficSplit := createTrafficSplit(rollout, 10, controllerKind)
	client := fake.NewSimpleClientset(trafficSplit)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: controllerKind,
	})
	buf := bytes.NewBufferString("")
	logger := log.New()
	logger.SetOutput(buf)
	r.log.Logger = logger
	err := r.Reconcile(10)
	assert.Nil(t, err)
	logMessage := buf.String()
	assert.True(t, strings.Contains(logMessage, "Traffic Split `traffic-split-name` was not modified"))
}

func TestReconcileGetTrafficSplitError(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	client := fake.NewSimpleClientset()
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: controllerKind,
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
	rollout := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	trafficSplit := createTrafficSplit(rollout, 10, controllerKind)
	trafficSplit.OwnerReferences = nil

	client := fake.NewSimpleClientset(trafficSplit)
	r := NewReconciler(ReconcilerConfig{
		Rollout:        rollout,
		Client:         client,
		Recorder:       &record.FakeRecorder{},
		ControllerKind: controllerKind,
	})
	err := r.Reconcile(10)
	assert.EqualError(t, err, "Rollout does not own TrafficSplit 'traffic-split-name'")
}
