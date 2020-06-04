package smi

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	core "k8s.io/client-go/testing"
	k8stesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	smiv1alpha1 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	fake "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
)

var controllerKind = schema.GroupVersionKind{Group: "foo", Version: "v1", Kind: "Bar"}

func trafficSplit(name string, rollout *v1alpha1.Rollout, desiredWeight int32) *smiv1alpha1.TrafficSplit {
	canaryWeight := resource.NewQuantity(int64(desiredWeight), resource.DecimalExponent)
	stableWeight := resource.NewQuantity(int64(100-desiredWeight), resource.DecimalExponent)

	rootSvc := rollout.Spec.Strategy.Canary.TrafficRouting.SMI.RootService
	if rootSvc == "" {
		rootSvc = rollout.Spec.Strategy.Canary.StableService
	}

	return &smiv1alpha1.TrafficSplit{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceDefault,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(rollout, controllerKind),
			},
		},
		Spec: smiv1alpha1.TrafficSplitSpec{
			Service: rootSvc,
			Backends: []smiv1alpha1.TrafficSplitBackend{
				{
					Service: rollout.Spec.Strategy.Canary.CanaryService,
					Weight:  canaryWeight,
				},
				{
					Service: rollout.Spec.Strategy.Canary.StableService,
					Weight:  stableWeight,
				},
			},
		},
	}
}

// Create and test actions for client
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

	expectedTS := trafficSplit("traffic-split-name", rollout, 10)

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
	trafficSplit := trafficSplit("traffic-split-name", rollout, 5)
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
	canaryWeight, _ := ts.Spec.Backends[0].Weight.AsInt64()
	stableWeight, _ := ts.Spec.Backends[1].Weight.AsInt64()

	assert.Equal(t, int64(10), canaryWeight)
	assert.Equal(t, int64(90), stableWeight)
}

func TestReconcilePatchExistingTrafficSplitNoChange(t *testing.T) {
	rollout := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
	trafficSplit := trafficSplit("traffic-split-name", rollout, 10)
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
	trafficSplit := trafficSplit("traffic-split-name", rollout, 10)
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
