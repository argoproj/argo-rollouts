package smi

import (
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	//"google.golang.org/genproto/googleapis/appengine/v1"
	//corev1 "k8s.io/api/core/v1"
	//"k8s.io/apimachinery/pkg/api/resource"
	//metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sirupsen/logrus"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	smiutil "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
)


const (
	// Type holds this controller type
	Type = "SMI"
)

// ReconcilerConfig describes static configuration data for the SMI reconciler
type ReconcilerConfig struct {
	Rollout        *v1alpha1.Rollout
	//Client         kubernetes.Interface
	//Recorder       record.EventRecorder
	//ControllerKind schema.GroupVersionKind
}

// Reconciler holds required fields to reconcile SMI resources
type Reconciler struct {
	cfg ReconcilerConfig
	log *logrus.Entry
}

// NewReconciler returns a reconciler struct that brings the SMI into the desired state
func NewReconciler(cfg ReconcilerConfig) *Reconciler {
	return &Reconciler{
		cfg: cfg,
		log: logutil.WithRollout(cfg.Rollout),
	}
}

// Type indicates this reconciler is an SMI reconciler
func (r *Reconciler) Type() string {
	return Type
}

func (r *Reconciler) Reconcile(desiredWeight int32) error {
	trafficSplitName := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.SMI.TrafficSplitName
	canarySvc := r.cfg.Rollout.Spec.Strategy.Canary.CanaryService
	stableSvc := r.cfg.Rollout.Spec.Strategy.Canary.StableService
	rootSvc := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.SMI.RootService

	trafficSplit := smiutil.TrafficSplit{
		Spec: smiutil.TrafficSplitSpec{
			//Service: rootSvc,
			Backends: []smiutil.TrafficSplitBackend{
				{
					Service: canarySvc,
					//Weight: desiredWeight,
				},
				{
					Service: stableSvc,
					//Weight: 100-desiredWeight,
				},
			},
		},
	}

	if rootSvc != "" {
		trafficSplit.Spec.Service = rootSvc
	} else {
		trafficSplit.Spec.Service = stableSvc
	}

	if trafficSplitName != "" {
		trafficSplit.Name = trafficSplitName
	}

	//trafficSplitSpec := smiutil.TrafficSplitSpec{Service: "", Backends:[]smiutil.TrafficSplitBackend{}}
	if trafficSplitName != "" {
		// Look for existing trafficSplit
		// Modify with RO spec fields
		return nil
	}

	// Create TrafficSplit
	return nil
}