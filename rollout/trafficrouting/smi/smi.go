package smi

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	smiv1alpha1 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	//smiv1alpha2 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha2"
	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	patchtypes "k8s.io/apimachinery/pkg/types"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/diff"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

const (
	// Type holds this controller type
	Type = "SMI"
)

// ReconcilerConfig describes static configuration data for the SMI reconciler
//noinspection GoUnresolvedReference
type ReconcilerConfig struct {
	Rollout        *v1alpha1.Rollout
	Client         smiclientset.Interface
	Recorder       record.EventRecorder
	ControllerKind schema.GroupVersionKind
	ApiVersion     string
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

// Create and modify traffic splits based on the desired weight
func (r *Reconciler) Reconcile(desiredWeight int32) error {
	trafficSplitName := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.SMI.TrafficSplitName
	newTrafficSplit := createTrafficSplit(r.cfg.Rollout, desiredWeight, r.cfg.ControllerKind)
	client := r.cfg.Client.SplitV1alpha1()

	// Check if Traffic Split exists in namespace
	trafficSplit, err := client.TrafficSplits(r.cfg.Rollout.Namespace).Get(trafficSplitName, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		// Create new Traffic Split
		_, err = client.TrafficSplits(r.cfg.Rollout.Namespace).Create(newTrafficSplit)
		if err == nil {
			msg := fmt.Sprintf("Traffic Split `%s` created", trafficSplitName)
			r.cfg.Recorder.Event(r.cfg.Rollout, corev1.EventTypeNormal, "TrafficSplitCreated", msg)
			r.log.Info(msg)
		} else {
			msg := fmt.Sprintf("Unable to create Traffic Split `%s`", trafficSplitName)
			r.cfg.Recorder.Event(r.cfg.Rollout, corev1.EventTypeWarning, "TrafficSplitCreated", msg)
		}
		return err
	}

	if err != nil {
		return err
	}

	// Patch existing Traffic Split
	isControlledBy := metav1.IsControlledBy(trafficSplit, r.cfg.Rollout)
	if !isControlledBy {
		msg := fmt.Sprintf("Rollout does not own TrafficSplit '%s'", trafficSplitName)
		return fmt.Errorf(msg)
	}
	patch, modified, err := diff.CreateTwoWayMergePatch( // Use interfaces? 1st 2 args are interfaces -> can cast to correct version
		smiv1alpha1.TrafficSplit{
			Spec: trafficSplit.Spec,
		},
		smiv1alpha1.TrafficSplit{
			Spec: newTrafficSplit.Spec,
		},
		smiv1alpha1.TrafficSplit{},
	)
	if err != nil {
		panic(err)
	}
	if !modified {
		r.log.Infof("Traffic Split `%s` was not modified", trafficSplitName)
		return nil
	}
	_, err = client.TrafficSplits(r.cfg.Rollout.Namespace).Patch(trafficSplitName, patchtypes.MergePatchType, patch)
	msg := fmt.Sprintf("Traffic Split `%s` modified", trafficSplitName)
	r.cfg.Recorder.Event(r.cfg.Rollout, corev1.EventTypeNormal, "TrafficSplitModified", msg)
	r.log.Info(msg)
	return err
}

func createTrafficSplit(rollout *v1alpha1.Rollout, desiredWeight int32, controllerKind schema.GroupVersionKind) *smiv1alpha1.TrafficSplit {
	// Service weights formatted for Traffic Split spec
	canaryWeight := resource.NewQuantity(int64(desiredWeight), resource.DecimalExponent)
	stableWeight := resource.NewQuantity(int64(100-desiredWeight), resource.DecimalExponent)

	// If root service not set, then set root service to be stable service
	rootSvc := rollout.Spec.Strategy.Canary.TrafficRouting.SMI.RootService
	if rootSvc == "" {
		rootSvc = rollout.Spec.Strategy.Canary.StableService
	}

	return &smiv1alpha1.TrafficSplit{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rollout.Spec.Strategy.Canary.TrafficRouting.SMI.TrafficSplitName,
			Namespace: rollout.Namespace,
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
