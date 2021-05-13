package smi

import (
	"context"
	"fmt"

	smiv1alpha1 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha1"
	smiv1alpha2 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha2"
	smiv1alpha3 "github.com/servicemeshinterface/smi-sdk-go/pkg/apis/split/v1alpha3"
	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	patchtypes "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/diff"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
)

const (
	// Type holds this controller type
	Type = "SMI"
)

// ReconcilerConfig describes static configuration data for the SMI reconciler
type ReconcilerConfig struct {
	Rollout        *v1alpha1.Rollout
	Client         smiclientset.Interface
	Recorder       record.EventRecorder
	ControllerKind schema.GroupVersionKind
}

// Reconciler holds required fields to reconcile SMI resources
type Reconciler struct {
	cfg                        ReconcilerConfig
	log                        *logrus.Entry
	getTrafficSplit            func(trafficSplitName string) (VersionedTrafficSplits, error)
	createTrafficSplit         func(ts VersionedTrafficSplits) error
	patchTrafficSplit          func(existing VersionedTrafficSplits, desired VersionedTrafficSplits) error
	trafficSplitIsControlledBy func(ts VersionedTrafficSplits) bool
}

type VersionedTrafficSplits struct {
	ts1 *smiv1alpha1.TrafficSplit
	ts2 *smiv1alpha2.TrafficSplit
	ts3 *smiv1alpha3.TrafficSplit
}

var smiAPIVersion = defaults.DefaultSMITrafficSplitVersion

func SetSMIAPIVersion(apiVersion string) {
	smiAPIVersion = apiVersion
}

// NewReconciler returns a reconciler struct that brings the SMI into the desired state
func NewReconciler(cfg ReconcilerConfig) (*Reconciler, error) {
	r := &Reconciler{
		cfg: cfg,
		log: logutil.WithRollout(cfg.Rollout),
	}
	ctx := context.TODO()
	switch smiAPIVersion {
	case "v1alpha1":
		r.getTrafficSplit = func(trafficSplitName string) (VersionedTrafficSplits, error) {
			ts1, err := r.cfg.Client.SplitV1alpha1().TrafficSplits(r.cfg.Rollout.Namespace).Get(ctx, trafficSplitName, metav1.GetOptions{})
			ts := VersionedTrafficSplits{}
			if ts1 != nil {
				ts.ts1 = ts1
			}
			return ts, err
		}
		r.createTrafficSplit = func(ts VersionedTrafficSplits) error {
			_, err := r.cfg.Client.SplitV1alpha1().TrafficSplits(r.cfg.Rollout.Namespace).Create(ctx, ts.ts1, metav1.CreateOptions{})
			return err
		}
		r.patchTrafficSplit = func(existing VersionedTrafficSplits, desired VersionedTrafficSplits) error {
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
			_, err = r.cfg.Client.SplitV1alpha1().TrafficSplits(r.cfg.Rollout.Namespace).Patch(ctx, existing.ts1.Name, patchtypes.MergePatchType, patch, metav1.PatchOptions{})
			return err
		}
		r.trafficSplitIsControlledBy = func(ts VersionedTrafficSplits) bool {
			return metav1.IsControlledBy(ts.ts1, r.cfg.Rollout)
		}
	case "v1alpha2":
		r.getTrafficSplit = func(trafficSplitName string) (VersionedTrafficSplits, error) {
			ts2, err := r.cfg.Client.SplitV1alpha2().TrafficSplits(r.cfg.Rollout.Namespace).Get(ctx, trafficSplitName, metav1.GetOptions{})
			ts := VersionedTrafficSplits{}
			if ts2 != nil {
				ts.ts2 = ts2
			}
			return ts, err
		}
		r.createTrafficSplit = func(ts VersionedTrafficSplits) error {
			_, err := r.cfg.Client.SplitV1alpha2().TrafficSplits(r.cfg.Rollout.Namespace).Create(ctx, ts.ts2, metav1.CreateOptions{})
			return err
		}
		r.patchTrafficSplit = func(existing VersionedTrafficSplits, desired VersionedTrafficSplits) error {
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
			_, err = r.cfg.Client.SplitV1alpha2().TrafficSplits(r.cfg.Rollout.Namespace).Patch(ctx, existing.ts2.Name, patchtypes.MergePatchType, patch, metav1.PatchOptions{})
			return err
		}
		r.trafficSplitIsControlledBy = func(ts VersionedTrafficSplits) bool {
			return metav1.IsControlledBy(ts.ts2, r.cfg.Rollout)
		}
	case "v1alpha3":
		r.getTrafficSplit = func(trafficSplitName string) (VersionedTrafficSplits, error) {
			ts3, err := r.cfg.Client.SplitV1alpha3().TrafficSplits(r.cfg.Rollout.Namespace).Get(ctx, trafficSplitName, metav1.GetOptions{})
			ts := VersionedTrafficSplits{}
			if ts3 != nil {
				ts.ts3 = ts3
			}
			return ts, err
		}
		r.createTrafficSplit = func(ts VersionedTrafficSplits) error {
			_, err := r.cfg.Client.SplitV1alpha3().TrafficSplits(r.cfg.Rollout.Namespace).Create(ctx, ts.ts3, metav1.CreateOptions{})
			return err
		}
		r.patchTrafficSplit = func(existing VersionedTrafficSplits, desired VersionedTrafficSplits) error {
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
			_, err = r.cfg.Client.SplitV1alpha3().TrafficSplits(r.cfg.Rollout.Namespace).Patch(ctx, existing.ts3.Name, patchtypes.MergePatchType, patch, metav1.PatchOptions{})
			return err
		}
		r.trafficSplitIsControlledBy = func(ts VersionedTrafficSplits) bool {
			return metav1.IsControlledBy(ts.ts3, r.cfg.Rollout)
		}
	default:
		err := fmt.Errorf("Unsupported TrafficSplit API version `%s`", smiAPIVersion)
		return nil, err
	}
	return r, nil
}

func (r *Reconciler) VerifyWeight(desiredWeight int32) (bool, error) {
	return true, nil
}

// Type indicates this reconciler is an SMI reconciler
func (r *Reconciler) Type() string {
	return Type
}

// SetWeight creates and modifies traffic splits based on the desired weight
func (r *Reconciler) SetWeight(desiredWeight int32) error {
	// If TrafficSplitName not set, then set to Rollout name
	trafficSplitName := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.SMI.TrafficSplitName
	if trafficSplitName == "" {
		trafficSplitName = r.cfg.Rollout.Name
	}
	trafficSplits := r.generateTrafficSplits(trafficSplitName, desiredWeight)

	// Check if Traffic Split exists in namespace
	existingTrafficSplit, err := r.getTrafficSplit(trafficSplitName)

	if k8serrors.IsNotFound(err) {
		// Create new Traffic Split
		err = r.createTrafficSplit(trafficSplits)
		if err == nil {
			r.cfg.Recorder.Eventf(r.cfg.Rollout, record.EventOptions{EventReason: "TrafficSplitCreated"}, "TrafficSplit `%s` created", trafficSplitName)
		} else {
			r.cfg.Recorder.Eventf(r.cfg.Rollout, record.EventOptions{EventReason: "TrafficSplitNotCreated"}, "TrafficSplit `%s` failed creation: %v", trafficSplitName, err)
		}
		return err
	}

	if err != nil {
		return err
	}

	// Patch existing Traffic Split
	isControlledBy := r.trafficSplitIsControlledBy(existingTrafficSplit)
	if !isControlledBy {
		return fmt.Errorf("Rollout does not own TrafficSplit `%s`", trafficSplitName)
	}
	err = r.patchTrafficSplit(existingTrafficSplit, trafficSplits)
	if err == nil {
		r.cfg.Recorder.Eventf(r.cfg.Rollout, record.EventOptions{EventReason: "TrafficSplitModified"}, "TrafficSplit `%s` modified", trafficSplitName)
	}
	return err
}

func (r *Reconciler) generateTrafficSplits(trafficSplitName string, desiredWeight int32) VersionedTrafficSplits {
	// If root service not set, then set root service to be stable service
	rootSvc := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.SMI.RootService
	if rootSvc == "" {
		rootSvc = r.cfg.Rollout.Spec.Strategy.Canary.StableService
	}

	trafficSplits := VersionedTrafficSplits{}

	objectMeta := objectMeta(trafficSplitName, r.cfg.Rollout, r.cfg.ControllerKind)

	switch smiAPIVersion {
	case "v1alpha1":
		trafficSplits.ts1 = trafficSplitV1Alpha1(r.cfg.Rollout, objectMeta, rootSvc, desiredWeight)
	case "v1alpha2":
		trafficSplits.ts2 = trafficSplitV1Alpha2(r.cfg.Rollout, objectMeta, rootSvc, desiredWeight)
	case "v1alpha3":
		trafficSplits.ts3 = trafficSplitV1Alpha3(r.cfg.Rollout, objectMeta, rootSvc, desiredWeight)
	}
	return trafficSplits
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

func trafficSplitV1Alpha1(ro *v1alpha1.Rollout, objectMeta metav1.ObjectMeta, rootSvc string, desiredWeight int32) *smiv1alpha1.TrafficSplit {
	return &smiv1alpha1.TrafficSplit{
		ObjectMeta: objectMeta,
		Spec: smiv1alpha1.TrafficSplitSpec{
			Service: rootSvc,
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

func trafficSplitV1Alpha2(ro *v1alpha1.Rollout, objectMeta metav1.ObjectMeta, rootSvc string, desiredWeight int32) *smiv1alpha2.TrafficSplit {
	return &smiv1alpha2.TrafficSplit{
		ObjectMeta: objectMeta,
		Spec: smiv1alpha2.TrafficSplitSpec{
			Service: rootSvc,
			Backends: []smiv1alpha2.TrafficSplitBackend{
				{
					Service: ro.Spec.Strategy.Canary.CanaryService,
					Weight:  int(desiredWeight),
				},
				{
					Service: ro.Spec.Strategy.Canary.StableService,
					Weight:  int(100 - desiredWeight),
				},
			},
		},
	}
}

func trafficSplitV1Alpha3(ro *v1alpha1.Rollout, objectMeta metav1.ObjectMeta, rootSvc string, desiredWeight int32) *smiv1alpha3.TrafficSplit {
	return &smiv1alpha3.TrafficSplit{
		ObjectMeta: objectMeta,
		Spec: smiv1alpha3.TrafficSplitSpec{
			Service: rootSvc,
			Backends: []smiv1alpha3.TrafficSplitBackend{
				{
					Service: ro.Spec.Strategy.Canary.CanaryService,
					Weight:  int(desiredWeight),
				},
				{
					Service: ro.Spec.Strategy.Canary.StableService,
					Weight:  int(100 - desiredWeight),
				},
			},
		},
	}
}

// UpdateHash informs a traffic routing reconciler about new canary/stable pod hashes
func (r *Reconciler) UpdateHash(canaryHash, stableHash string) error {
	return nil
}
