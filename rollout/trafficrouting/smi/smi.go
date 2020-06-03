package smi

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/diff"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	smiv1alpha1 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	"github.com/sirupsen/logrus"
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

// TODO: Make code compatible with multiple TrafficSplit versions
func (r *Reconciler) Reconcile(desiredWeight int32) error {
	trafficSplitName := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.SMI.TrafficSplitName

	// Service weights formatted for Traffic Split spec
	canaryWeight := resource.NewQuantity(int64(desiredWeight), resource.DecimalExponent)
	stableWeight := resource.NewQuantity(int64(100-desiredWeight), resource.DecimalExponent)

	// If root service not set, then set root service to be stable service
	rootSvc := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.SMI.RootService
	if rootSvc == "" {
		rootSvc = r.cfg.Rollout.Spec.Strategy.Canary.StableService
	}

	trafficSplitSpec := smiv1alpha1.TrafficSplitSpec{
		Service: rootSvc,
		Backends: []smiv1alpha1.TrafficSplitBackend{
			{
				Service: r.cfg.Rollout.Spec.Strategy.Canary.CanaryService,
				Weight:  canaryWeight,
			},
			{
				Service: r.cfg.Rollout.Spec.Strategy.Canary.StableService,
				Weight:  stableWeight,
			},
		},
	}

	client := r.cfg.Client.SplitV1alpha1()

	// Check if Traffic Split exists in namespace
	trafficSplit, err := client.TrafficSplits(r.cfg.Rollout.Namespace).Get(trafficSplitName, metav1.GetOptions{})
	//trafficSplit, err := client.TrafficSplits(r.cfg.Rollout.Namespace).Get(trafficSplitName, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		msg := fmt.Sprintf("Traffic Split `%s` not found", trafficSplitName)
		r.cfg.Recorder.Event(r.cfg.Rollout, corev1.EventTypeNormal, "TrafficSplitNotFound", msg)
		// TODO: check for double-logging
		// Create new Traffic Split
		trafficSplit = &smiv1alpha1.TrafficSplit{
			ObjectMeta: metav1.ObjectMeta{
				Name:      trafficSplitName,
				Namespace: r.cfg.Rollout.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(r.cfg.Rollout, r.cfg.ControllerKind),
				},
			},
			Spec: trafficSplitSpec,
		}
		_, err = client.TrafficSplits(r.cfg.Rollout.Namespace).Create(trafficSplit)
		return err
	}

	if err != nil {
		return err
	}

	// Patch existing Traffic Split
	if trafficSplit != nil {
		isControlledBy := metav1.IsControlledBy(trafficSplit, r.cfg.Rollout)
		if !isControlledBy {
			msg := fmt.Sprintf("Rollout does not own TrafficSplit %s", trafficSplitName)
			return fmt.Errorf(msg)
		}
		patch, modified, err := diff.CreateTwoWayMergePatch(
			smiv1alpha1.TrafficSplit{
				Spec: trafficSplit.Spec,
			},
			smiv1alpha1.TrafficSplit{
				Spec: trafficSplitSpec,
			},
			smiv1alpha1.TrafficSplit{},
		)
		if err != nil {
			return err // Throw panic
		}
		if !modified {
			return nil // Add log ("TrafficSplit not modified" - check log invoked)
		}
		_, err = client.TrafficSplits(r.cfg.Rollout.Namespace).Patch(trafficSplitName, patchtypes.MergePatchType, patch)
		return err
	}

	return nil
}
