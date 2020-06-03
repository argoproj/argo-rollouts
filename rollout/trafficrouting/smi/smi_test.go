package smi

import (
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	smiv1alpha1 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	fake "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
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
	//r.cfg.Client.(*fake.Clientset).Fake.AddReactor("create", "trafficsplits", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
	//	return true, nil, errors.New("fake error")
	//})
	err := r.Reconcile(10)
	assert.Nil(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 2)
	assert.Equal(t, "get", actions[0].GetVerb())
	assert.Equal(t, "create", actions[1].GetVerb())

	//trafficSplit := trafficSplit("traffic-split-name", rollout, 10)
	//assert.Equal(t, trafficSplit, actions[1])
}

//func TestReconcileTrafficSplitFoundError(t *testing.T) {
//	// create TrafficSplit object for Client to discover
//	client := fake.NewSimpleClientset()
//}

// CHECK LOGS
// USE LOG REDACTOR EXAMPLE
//func TestReconcilePatchExistingTrafficSplit(t *testing.T) {
//	rollout := fakeRollout("stable-service", "canary-service", "root-service", "traffic-split-name")
//	client := fake.NewSimpleClientset()
//	r := NewReconciler(ReconcilerConfig{
//		Rollout:        rollout,
//		Client:         client,
//		Recorder:       &record.FakeRecorder{},
//		ControllerKind: controllerKind,
//	})

//}

// *** Example Error: API Server not available (use k8sErrors)
// Error: Not authenticated
func TestReconcilePatchExistingTrafficSplitError(t *testing.T) {

}

func TestReconcilePatchExistingTrafficSplitNoModification(t *testing.T) {

}
