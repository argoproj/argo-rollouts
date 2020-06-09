package smi

import (
	"fmt"

	smiv1alpha1 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	smiv1alpha2 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha2"
	smiv1alpha3 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha3"
	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime/schema"
	patchtypes "k8s.io/apimachinery/pkg/types"
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
	trafficSplits TrafficSplits
	getFunc func(trafficSplitName string) (TrafficSplits, error)
	createFunc func(ts TrafficSplits) error
	patchFunc func(existing TrafficSplits, desired TrafficSplits) error
	isControlledBy func(ts TrafficSplits) bool
}

type TrafficSplits struct {
	ts1 smiv1alpha1.TrafficSplit
	ts2 smiv1alpha2.TrafficSplit
	ts3 smiv1alpha3.TrafficSplit
}

// NewReconciler returns a reconciler struct that brings the SMI into the desired state
func NewReconciler(cfg ReconcilerConfig) *Reconciler {
	r:= &Reconciler{
		cfg: cfg,
		log: logutil.WithRollout(cfg.Rollout),
		trafficSplits: TrafficSplits{},
	}
	switch apiVersion := r.cfg.ApiVersion; apiVersion {
	case "v1alpha1":
		r.getFunc = func(trafficSplitName string) (TrafficSplits, error){
			ts1, err := r.cfg.Client.SplitV1alpha1().TrafficSplits(r.cfg.Rollout.Namespace).Get(trafficSplitName, metav1.GetOptions{})
			ts := TrafficSplits{
				ts1: *ts1,
			}
			return ts, err
		}
		r.createFunc = func(ts TrafficSplits) error {
			_, err := r.cfg.Client.SplitV1alpha1().TrafficSplits(r.cfg.Rollout.Namespace).Create(&ts.ts1)
			return err
		}
		r.patchFunc = func(existing TrafficSplits, desired TrafficSplits) error {
			patch, modified, err := diff.CreateTwoWayMergePatch(
				smiv1alpha1.TrafficSplit{
					Spec: existing.ts1.Spec,
				},
				smiv1alpha1.TrafficSplit{
					Spec: desired.ts1.Spec,
				},
				smiv1alpha1.TrafficSplit{},
			)
			if err != nil {
				panic(err)
			}
			if !modified {
				r.log.Infof("Traffic Split `%s` was not modified", existing.ts1.Name)
				return nil
			}
			_, err = r.cfg.Client.SplitV1alpha1().TrafficSplits(r.cfg.Rollout.Namespace).Patch(existing.ts1.Name, patchtypes.MergePatchType, patch)
			return err
		}
		r.isControlledBy = func(ts TrafficSplits) bool {
			return metav1.IsControlledBy(&ts.ts1, r.cfg.Rollout)
		}
	case "v1alpha2":
		r.getFunc = func(trafficSplitName string) (TrafficSplits, error){
			ts2, err := r.cfg.Client.SplitV1alpha2().TrafficSplits(r.cfg.Rollout.Namespace).Get(trafficSplitName, metav1.GetOptions{})
			ts := TrafficSplits{
				ts2: *ts2,
			}
			return ts, err
		}
		r.createFunc = func(ts TrafficSplits) error {
			_, err := r.cfg.Client.SplitV1alpha2().TrafficSplits(r.cfg.Rollout.Namespace).Create(&ts.ts2)
			return err
		}
		r.patchFunc = func(existing TrafficSplits, desired TrafficSplits) error {
			patch, modified, err := diff.CreateTwoWayMergePatch(
				smiv1alpha2.TrafficSplit{
					Spec: existing.ts2.Spec,
				},
				smiv1alpha2.TrafficSplit{
					Spec: desired.ts2.Spec,
				},
				smiv1alpha2.TrafficSplit{},
			)
			if err != nil {
				panic(err)
			}
			if !modified {
				r.log.Infof("Traffic Split `%s` was not modified", existing.ts2.Name)
				return nil
			}
			_, err = r.cfg.Client.SplitV1alpha2().TrafficSplits(r.cfg.Rollout.Namespace).Patch(existing.ts2.Name, patchtypes.MergePatchType, patch)
			return err
		}
		r.isControlledBy = func(ts TrafficSplits) bool {
			return metav1.IsControlledBy(&ts.ts2, r.cfg.Rollout)
		}
	case "v1alpha3":
		r.getFunc = func(trafficSplitName string) (TrafficSplits, error){
			ts3, err := r.cfg.Client.SplitV1alpha3().TrafficSplits(r.cfg.Rollout.Namespace).Get(trafficSplitName, metav1.GetOptions{})
			ts := TrafficSplits{
				ts3: *ts3,
			}
			return ts, err
		}
		r.createFunc = func(ts TrafficSplits) error {
			_, err := r.cfg.Client.SplitV1alpha3().TrafficSplits(r.cfg.Rollout.Namespace).Create(&ts.ts3)
			return err
		}
		r.patchFunc = func(existing TrafficSplits, desired TrafficSplits) error {
			patch, modified, err := diff.CreateTwoWayMergePatch(
				smiv1alpha3.TrafficSplit{
					Spec: existing.ts3.Spec,
				},
				smiv1alpha3.TrafficSplit{
					Spec: desired.ts3.Spec,
				},
				smiv1alpha3.TrafficSplit{},
			)
			if err != nil {
				panic(err)
			}
			if !modified {
				r.log.Infof("Traffic Split `%s` was not modified", existing.ts3.Name)
				return nil
			}
			_, err = r.cfg.Client.SplitV1alpha3().TrafficSplits(r.cfg.Rollout.Namespace).Patch(existing.ts3.Name, patchtypes.MergePatchType, patch)
			return err
		}
		r.isControlledBy = func(ts TrafficSplits) bool {
			return metav1.IsControlledBy(&ts.ts3, r.cfg.Rollout)
		}
	}
	return r
}

// Type indicates this reconciler is an SMI reconciler
func (r *Reconciler) Type() string {
	return Type
}

// Create and modify traffic splits based on the desired weight
func (r *Reconciler) Reconcile(desiredWeight int32) error {
	trafficSplitName := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.SMI.TrafficSplitName
	if trafficSplitName == "" {
		trafficSplitName = r.cfg.Rollout.Name
	}
	r.initializeTrafficSplits(trafficSplitName, desiredWeight)

	// Check if Traffic Split exists in namespace
	trafficSplit, err := r.getFunc(trafficSplitName)

	if k8serrors.IsNotFound(err) {
		// Create new Traffic Split
		err = r.createFunc(r.trafficSplits)
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
	isControlledBy := r.isControlledBy(trafficSplit)
	if !isControlledBy {
		msg := fmt.Sprintf("Rollout does not own TrafficSplit '%s'", trafficSplitName)
		return fmt.Errorf(msg)
	}
	err = r.patchFunc(trafficSplit, r.trafficSplits)
	if err == nil {
		msg := fmt.Sprintf("Traffic Split '%s' modified", trafficSplitName)
		r.cfg.Recorder.Event(r.cfg.Rollout, corev1.EventTypeNormal, "TrafficSplitModified", msg)
		r.log.Info(msg)
	}
	return err
}

func (r *Reconciler) initializeTrafficSplits(trafficSplitName string, desiredWeight int32) {
	// If root service not set, then set root service to be stable service
	rootSvc := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.SMI.RootService
	if rootSvc == "" {
		rootSvc = r.cfg.Rollout.Spec.Strategy.Canary.StableService
	}

	objectMeta := metav1.ObjectMeta{
		Name:      trafficSplitName,
		Namespace: r.cfg.Rollout.Namespace,
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(r.cfg.Rollout, r.cfg.ControllerKind),
		},
	}
	switch apiVersion := r.cfg.ApiVersion; apiVersion {
	case "v1alpha1":
		r.trafficSplits.ts1 = smiv1alpha1.TrafficSplit{
		ObjectMeta: objectMeta,
		Spec:       smiv1alpha1.TrafficSplitSpec{
			Service:  rootSvc,
			Backends: []smiv1alpha1.TrafficSplitBackend{
				{
					Service: r.cfg.Rollout.Spec.Strategy.Canary.CanaryService,
					Weight:  resource.NewQuantity(int64(desiredWeight), resource.DecimalExponent),
				},
				{
					Service: r.cfg.Rollout.Spec.Strategy.Canary.StableService,
					Weight:  resource.NewQuantity(int64(100-desiredWeight), resource.DecimalExponent),
				},
			},
		},
	}
	case "v2alpha2":
		r.trafficSplits.ts2 = smiv1alpha2.TrafficSplit{
			ObjectMeta: objectMeta,
			Spec:       smiv1alpha2.TrafficSplitSpec{
				Service:  rootSvc,
				Backends: []smiv1alpha2.TrafficSplitBackend{
					{
						Service: r.cfg.Rollout.Spec.Strategy.Canary.CanaryService,
						Weight:  int(desiredWeight),
					},
					{
						Service: r.cfg.Rollout.Spec.Strategy.Canary.StableService,
						Weight:  int(desiredWeight),
					},
				},
			},
		}
	case "v3alpha3":
		r.trafficSplits.ts3 = smiv1alpha3.TrafficSplit{
			ObjectMeta: objectMeta,
			Spec:       smiv1alpha3.TrafficSplitSpec{
				Service:  rootSvc,
				Backends: []smiv1alpha3.TrafficSplitBackend{
					{
						Service: r.cfg.Rollout.Spec.Strategy.Canary.CanaryService,
						Weight:  int(desiredWeight),
					},
					{
						Service: r.cfg.Rollout.Spec.Strategy.Canary.StableService,
						Weight:  int(desiredWeight),
					},
				},
			},
		}
	}
}

//func createTrafficSplit(rollout *v1alpha1.Rollout, desiredWeight int32, controllerKind schema.GroupVersionKind) *smiv1alpha1.TrafficSplit {
//	// Service weights formatted for Traffic Split spec
//	canaryWeight := resource.NewQuantity(int64(desiredWeight), resource.DecimalExponent)
//	stableWeight := resource.NewQuantity(int64(100-desiredWeight), resource.DecimalExponent)
//
//	// If root service not set, then set root service to be stable service
//	rootSvc := rollout.Spec.Strategy.Canary.TrafficRouting.SMI.RootService
//	if rootSvc == "" {
//		rootSvc = rollout.Spec.Strategy.Canary.StableService
//	}
//
//	return &smiv1alpha1.TrafficSplit{
//		ObjectMeta: metav1.ObjectMeta{
//			Name:      rollout.Spec.Strategy.Canary.TrafficRouting.SMI.TrafficSplitName,
//			Namespace: rollout.Namespace,
//			OwnerReferences: []metav1.OwnerReference{
//				*metav1.NewControllerRef(rollout, controllerKind),
//			},
//		},
//		Spec: smiv1alpha1.TrafficSplitSpec{
//			Service: rootSvc,
//			Backends: []smiv1alpha1.TrafficSplitBackend{
//				{
//					Service: rollout.Spec.Strategy.Canary.CanaryService,
//					Weight:  canaryWeight,
//				},
//				{
//					Service: rollout.Spec.Strategy.Canary.StableService,
//					Weight:  stableWeight,
//				},
//			},
//		},
//	}
//}
