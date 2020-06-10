package smi

import (
	"fmt"

	smiv1alpha1 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	smiv1alpha2 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha2"
	smiv1alpha3 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha3"
	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	// TODO: Create default and list of supported versions
	switch apiVersion := r.cfg.ApiVersion; apiVersion {
	case "v1alpha1":
		r.getFunc = func(trafficSplitName string) (TrafficSplits, error){
			ts1, err := r.cfg.Client.SplitV1alpha1().TrafficSplits(r.cfg.Rollout.Namespace).Get(trafficSplitName, metav1.GetOptions{})
			ts := TrafficSplits{}
			if ts1 != nil {
				ts.ts1 = *ts1
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
			ts := TrafficSplits{}
			if ts2 != nil {
				ts.ts2 = *ts2
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
			ts := TrafficSplits{}
			if ts3 != nil {
				ts.ts3 = *ts3
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
	existingTrafficSplit, err := r.getFunc(trafficSplitName)

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
	isControlledBy := r.isControlledBy(existingTrafficSplit)
	if !isControlledBy {
		msg := fmt.Sprintf("Rollout does not own TrafficSplit '%s'", trafficSplitName)
		return fmt.Errorf(msg)
	}
	err = r.patchFunc(existingTrafficSplit, r.trafficSplits)
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

	objectMeta := objectMeta(trafficSplitName, r.cfg.Rollout, r.cfg.ControllerKind)

	switch apiVersion := r.cfg.ApiVersion; apiVersion {
	case "v1alpha1":
		r.trafficSplits.ts1 = trafficSplitV1Alpha1(r.cfg.Rollout, objectMeta, rootSvc, desiredWeight)
	case "v1alpha2":
		r.trafficSplits.ts2 = trafficSplitV1Alpha2(r.cfg.Rollout, objectMeta, rootSvc, desiredWeight)
	case "v1alpha3":
		r.trafficSplits.ts3 = trafficSplitV1Alpha3(r.cfg.Rollout, objectMeta, rootSvc, desiredWeight)
	}
}

func objectMeta(trafficSplitName string, ro *v1alpha1.Rollout, controllerKind schema.GroupVersionKind) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      trafficSplitName,
		Namespace: ro.Namespace,
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(ro, controllerKind),
		},
	}
}

func trafficSplitV1Alpha1(ro *v1alpha1.Rollout, objectMeta metav1.ObjectMeta, rootSvc string, desiredWeight int32) smiv1alpha1.TrafficSplit {
	return smiv1alpha1.TrafficSplit{
		ObjectMeta: objectMeta,
		Spec:       smiv1alpha1.TrafficSplitSpec{
			Service:  rootSvc,
			Backends: []smiv1alpha1.TrafficSplitBackend{
				{
					Service: ro.Spec.Strategy.Canary.CanaryService,
					Weight:  resource.NewQuantity(int64(desiredWeight), resource.DecimalExponent),
				},
				{
					Service: ro.Spec.Strategy.Canary.StableService,
					Weight:  resource.NewQuantity(int64(100-desiredWeight), resource.DecimalExponent),
				},
			},
		},
	}
}

func trafficSplitV1Alpha2(ro *v1alpha1.Rollout, objectMeta metav1.ObjectMeta, rootSvc string, desiredWeight int32) smiv1alpha2.TrafficSplit {
	return smiv1alpha2.TrafficSplit{
		ObjectMeta: objectMeta,
		Spec:       smiv1alpha2.TrafficSplitSpec{
			Service:  rootSvc,
			Backends: []smiv1alpha2.TrafficSplitBackend{
				{
					Service: ro.Spec.Strategy.Canary.CanaryService,
					Weight: int(desiredWeight),
				},
				{
					Service: ro.Spec.Strategy.Canary.StableService,
					Weight:  int(100-desiredWeight),
				},
			},
		},
	}
}

func trafficSplitV1Alpha3(ro *v1alpha1.Rollout, objectMeta metav1.ObjectMeta, rootSvc string, desiredWeight int32) smiv1alpha3.TrafficSplit {
	return smiv1alpha3.TrafficSplit{
		ObjectMeta: objectMeta,
		Spec:       smiv1alpha3.TrafficSplitSpec{
			Service:  rootSvc,
			Backends: []smiv1alpha3.TrafficSplitBackend{
				{
					Service: ro.Spec.Strategy.Canary.CanaryService,
					Weight: int(desiredWeight),
				},
				{
					Service: ro.Spec.Strategy.Canary.StableService,
					Weight:  int(100-desiredWeight),
				},
			},
		},
	}
}
