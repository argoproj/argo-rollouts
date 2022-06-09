package appmesh

import (
	"context"
	"errors"
	"fmt"

	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/dynamic"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
)

const (
	Type                                   = "AppMesh"
	ErrVirtualServiceNotUsingVirtualRouter = "Virtual-service is not associated with virtual-router"
	ErrVirtualServiceMissing               = "Virtual-service is missing"
	ErrVirtualRouterMissing                = "Virtual-router is missing"
	ErrVirtualNodeMissing                  = "Virtual-node is missing"
	ErrNotWellFormed                       = "not well-formed"
	defaultCanaryHash                      = "canary-hash"
	defaultStableHash                      = "stable-hash"
)

var (
	// Only following route-types are supported when it comes to traffic splitting
	supportedRouteTypes = []string{"httpRoute", "tcpRoute", "http2Route", "grpcRoute"}
)

// ReconcilerConfig describes static configuration data for the AppMesh reconciler
type ReconcilerConfig struct {
	Rollout  *v1alpha1.Rollout
	Client   dynamic.Interface
	Recorder record.EventRecorder
}

// Reconciler holds required fields to reconcile AppMesh resources
type Reconciler struct {
	rollout  *v1alpha1.Rollout
	client   *ResourceClient
	recorder record.EventRecorder
	log      *logrus.Entry
}

// NewReconciler returns a trafficrouting reconciler to work with services using App Mesh custom resources such as
// virtual-services, virtual-routers and virtual-nodes to perform traffic-splitting functionality. This reconciler
// only works with appmesh.k8s.aws/v1beta2 custom resources. This reconciler uses dynamic client to avoid hard dependency
// on App Mesh controller.
func NewReconciler(cfg ReconcilerConfig) *Reconciler {
	reconciler := Reconciler{
		rollout:  cfg.Rollout,
		client:   NewResourceClient(cfg.Client),
		recorder: cfg.Recorder,
		log:      logutil.WithRollout(cfg.Rollout),
	}
	return &reconciler
}

// UpdateHash informs a traffic routing reconciler about new canary/stable pod hashes. UpdateHash initializes
// virtual-nodes with appropriate match-labels in pod-selector. It will mutate the pod-selector in two ways.
// Firstly it will update a label with name v1alpha1.DefaultRolloutUniqueLabelKey if one exists. Secondly it will add a
// new label with name v1alpha1.DefaultRolloutUniqueLabelKey if one does not exist.
func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
	ctx := context.TODO()

	r.log.Debugf("UpdateHash: canaryHash (%s), stableHash (%s)", canaryHash, stableHash)

	if stableHash == "" {
		stableHash = defaultStableHash
	}
	rStableVnodeRef := r.rollout.Spec.Strategy.Canary.TrafficRouting.AppMesh.VirtualNodeGroup.StableVirtualNodeRef
	err := r.updateVirtualNodeWithHash(ctx, rStableVnodeRef, stableHash)
	if err != nil {
		return err
	}

	r.log.Debugf("UpdateHash: updated stable virtual-node (%s) pod-selector to (%s)", rStableVnodeRef.Name, stableHash)

	// If both hashes are same then virtual-nodes will end up with exact same podSelector. This is not allowed by the
	// admission hook installed by appmesh controller. For now assuming that both hashes correspond to stable virtual-node
	// when this happens and resetting canaryHash
	if canaryHash == stableHash || canaryHash == "" {
		canaryHash = defaultCanaryHash
	}
	rCanaryVnodeRef := r.rollout.Spec.Strategy.Canary.TrafficRouting.AppMesh.VirtualNodeGroup.CanaryVirtualNodeRef
	err = r.updateVirtualNodeWithHash(ctx, rCanaryVnodeRef, canaryHash)
	if err != nil {
		return err
	}
	r.log.Debugf("UpdateHash: updated canary virtual-node (%s) pod-selector to (%s)", rCanaryVnodeRef.Name, canaryHash)

	return nil
}

// SetWeight sets the canary weight to the desired weight. SetWeight relates to a step in rollout process where
// traffic-routing shifts weight to/from stable and canary (traffic-splitting) based on the configuration. In the
// context of App Mesh, traffic-splitting is performed by adding a virtual-router with route(s) for virtual-service.
// This route includes a match condition and an action. Action is defined as weighted-targets where each target is a
// virtual-node. SetWeight adjusts the weights on this route.
func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	ctx := context.TODO()

	r.log.Debugf("SetWeight: setting desired-weight to %d", desiredWeight)

	rVirtualService := r.rollout.Spec.Strategy.Canary.TrafficRouting.AppMesh.VirtualService
	uVsvc, err := r.client.GetVirtualServiceCR(ctx, r.rollout.Namespace, rVirtualService.Name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			r.recorder.Warnf(r.rollout, record.EventOptions{EventReason: "VirtualServiceNotFound"}, "VirtualService `%s` not found in namespace `%s`", rVirtualService.Name, r.rollout.Namespace)
			return errors.New(ErrVirtualServiceMissing)
		}
		return err
	}

	uVr, err := r.client.GetVirtualRouterCRForVirtualService(ctx, uVsvc)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			r.recorder.Warnf(r.rollout, record.EventOptions{EventReason: "VirtualRouterNotFound"}, "VirtualRouter for `%s` not found in namespace `%s`", rVirtualService.Name, r.rollout.Namespace)
			return errors.New(ErrVirtualRouterMissing)
		}
		return err
	}

	err = r.reconcileVirtualRouter(ctx, rVirtualService.Routes, uVr, desiredWeight)
	if err != nil {
		return err
	}

	r.log.Debugf("SetWeight: updated virtual router (%s) with desiredWeight (%d)", uVr.GetName(), desiredWeight)

	return nil
}

func (r *Reconciler) SetHeaderRoute(headerRouting *v1alpha1.SetHeaderRoute) error {
	return nil
}

type routeReconcileContext struct {
	route           map[string]interface{}
	routeIndex      int
	routeFldPath    *field.Path
	rCanaryVnodeRef *v1alpha1.AppMeshVirtualNodeReference
	rStableVnodeRef *v1alpha1.AppMeshVirtualNodeReference
	routesFilterMap map[string]bool
	desiredWeight   int32
}

func (r *Reconciler) reconcileVirtualRouter(ctx context.Context, rRoutes []string, uVr *unstructured.Unstructured, desiredWeight int32) error {
	uVrCopy := uVr.DeepCopy()

	rCanaryVnodeRef := r.rollout.Spec.Strategy.Canary.TrafficRouting.AppMesh.VirtualNodeGroup.CanaryVirtualNodeRef
	rStableVnodeRef := r.rollout.Spec.Strategy.Canary.TrafficRouting.AppMesh.VirtualNodeGroup.StableVirtualNodeRef
	requiresUpdate := false

	routesFilterMap := make(map[string]bool)
	for _, r := range rRoutes {
		routesFilterMap[r] = true
	}

	routesFldPath := field.NewPath("spec", "routes")
	routesI, found, err := unstructured.NestedSlice(uVrCopy.Object, "spec", "routes")
	if !found || err != nil {
		return field.Invalid(routesFldPath, uVrCopy.GetName(), fmt.Sprintf("No routes found"))
	}

	for idx, routeI := range routesI {
		routeFldPath := routesFldPath.Index(idx)
		route, ok := routeI.(map[string]interface{})
		if !ok {
			return field.Invalid(routeFldPath, uVrCopy.GetName(), ErrNotWellFormed)
		}

		reconCtx := &routeReconcileContext{
			route:           route,
			routeIndex:      idx,
			routeFldPath:    routeFldPath,
			rCanaryVnodeRef: rCanaryVnodeRef,
			rStableVnodeRef: rStableVnodeRef,
			routesFilterMap: routesFilterMap,
			desiredWeight:   desiredWeight,
		}
		ru, err := r.reconcileRoute(ctx, uVrCopy, reconCtx)
		if err != nil {
			return err
		}
		requiresUpdate = requiresUpdate || ru
	}

	//update virtual-router with updated routes
	err = unstructured.SetNestedSlice(uVrCopy.Object, routesI, "spec", "routes")
	if err != nil {
		return err
	}
	if requiresUpdate {
		_, err = r.client.UpdateVirtualRouterCR(ctx, uVrCopy)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) reconcileRoute(ctx context.Context, uVr *unstructured.Unstructured, routeCtx *routeReconcileContext) (bool, error) {
	routeName, ok := routeCtx.route["name"].(string)
	if !ok {
		return false, field.Invalid(routeCtx.routeFldPath.Child("name"), uVr.GetName(), ErrNotWellFormed)
	}

	if len(routeCtx.routesFilterMap) > 0 {
		// filter out the routes that are not specified in route filter
		if _, ok := routeCtx.routesFilterMap[routeName]; !ok {
			return false, nil
		}
	}

	routeRule, routeType, err := GetRouteRule(routeCtx.route)
	if err != nil && routeRule == nil {
		return false, field.Invalid(routeCtx.routeFldPath, uVr.GetName(), ErrNotWellFormed)
	}

	weightedTargetsFldPath := routeCtx.routeFldPath.Child(routeType).Child("action").Child("weightedTargets")
	weightedTargets, found, err := unstructured.NestedSlice(routeRule, "action", "weightedTargets")
	if !found || err != nil {
		return false, field.Invalid(weightedTargetsFldPath, uVr.GetName(), ErrNotWellFormed)
	}

	requiresUpdate := false
	for idx, wtI := range weightedTargets {
		wtFldPath := weightedTargetsFldPath.Index(idx)
		wt, ok := wtI.(map[string]interface{})
		if !ok {
			return false, field.Invalid(wtFldPath, uVr.GetName(), ErrNotWellFormed)
		}
		wtVnRefFldPath := wtFldPath.Child("virtualNodeRef")
		wtVnRef, ok := wt["virtualNodeRef"].(map[string]interface{})
		if !ok {
			return false, field.Invalid(wtVnRefFldPath, uVr.GetName(), ErrNotWellFormed)
		}
		wtVnName, _ := wtVnRef["name"].(string)
		wtVnNamespace := defaultIfEmpty(wtVnRef["namespace"], r.rollout.Namespace)
		// weight in AppMesh CRD is int64
		//https://aws.github.io/aws-app-mesh-controller-for-k8s/reference/api_spec/#appmesh.k8s.aws/v1beta2.WeightedTarget
		weight, err := toInt64(wt["weight"])
		if err != nil {
			return false, field.Invalid(wtFldPath.Child("weight"), uVr.GetName(), ErrNotWellFormed)
		}
		if wtVnName == routeCtx.rStableVnodeRef.Name && wtVnNamespace == r.rollout.Namespace {
			if weight != int64(100-routeCtx.desiredWeight) {
				requiresUpdate = true
				wt["weight"] = int64(100 - routeCtx.desiredWeight)
			}
		} else if wtVnName == routeCtx.rCanaryVnodeRef.Name && wtVnNamespace == r.rollout.Namespace {
			if weight != int64(routeCtx.desiredWeight) {
				requiresUpdate = true
				wt["weight"] = int64(routeCtx.desiredWeight)
			}
		}
		r.log.Debugf("SetWeight: updating weight of virtualNode (%s.%s) with existing weight of (%d) to (%d)", wtVnName, wtVnNamespace, weight, wt["weight"])
	}

	if requiresUpdate {
		//update route with new weighted targets
		err = unstructured.SetNestedSlice(routeCtx.route, weightedTargets, routeType, "action", "weightedTargets")
		if err != nil {
			return false, err
		}
	}

	return requiresUpdate, nil
}

func (r *Reconciler) updateVirtualNodeWithHash(ctx context.Context, vnodeRef *v1alpha1.AppMeshVirtualNodeReference, hash string) error {
	uVnode, err := r.client.GetVirtualNodeCR(ctx, r.rollout.Namespace, vnodeRef.Name)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			r.recorder.Warnf(r.rollout, record.EventOptions{EventReason: "VirtualNodeNotFound"}, "VirtualNode `%s` not found in namespace `%s`", vnodeRef.Name, r.rollout.Namespace)
			return errors.New(ErrVirtualNodeMissing)
		}
		return err
	}

	newVnode := uVnode.DeepCopy()
	annotations := newVnode.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[v1alpha1.ManagedByRolloutsKey] = r.rollout.Name
	ml, err := getPodSelectorMatchLabels(newVnode)
	if err != nil {
		return err
	}
	if hash != "" {
		ml[v1alpha1.DefaultRolloutUniqueLabelKey] = hash
	} else {
		delete(ml, v1alpha1.DefaultRolloutUniqueLabelKey)
	}

	err = setPodSelectorMatchLabels(newVnode, ml)
	if err != nil {
		return err
	}

	_, err = r.client.UpdateVirtualNodeCR(ctx, newVnode)
	if err != nil {
		return err
	}

	return nil
}

// VerifyWeight returns true if the canary is at the desired weight and additonalDestinations are at the weights specified
// Returns nil if weight verification is not supported or not applicable
func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	return nil, nil
}

// Type returns the type of the traffic routing reconciler
func (r *Reconciler) Type() string {
	return Type
}

func getPodSelectorMatchLabels(vnode *unstructured.Unstructured) (map[string]interface{}, error) {
	m, found, err := unstructured.NestedMap(vnode.Object, "spec", "podSelector", "matchLabels")
	if err != nil {
		return nil, err
	}
	if !found || m == nil {
		return make(map[string]interface{}), nil
	}
	return m, nil
}

func setPodSelectorMatchLabels(vnode *unstructured.Unstructured, ml map[string]interface{}) error {
	return unstructured.SetNestedMap(vnode.Object, ml, "spec", "podSelector", "matchLabels")
}

func toInt64(obj interface{}) (int64, error) {
	switch i := obj.(type) {
	case float64:
		return int64(i), nil
	case float32:
		return int64(i), nil
	case int64:
		return i, nil
	case int32:
		return int64(i), nil
	case int16:
		return int64(i), nil
	case int8:
		return int64(i), nil
	case uint64:
		return int64(i), nil
	case uint32:
		return int64(i), nil
	case uint16:
		return int64(i), nil
	case uint8:
		return int64(i), nil
	case int:
		return int64(i), nil
	case uint:
		return int64(i), nil
	default:
		return 0, fmt.Errorf("toInt64: unknown value %v that is incompatible with int64", obj)
	}
}

func GetRouteRule(route map[string]interface{}) (map[string]interface{}, string, error) {
	var routeRule map[string]interface{}
	var routeType string
	for _, rType := range supportedRouteTypes {
		r, found, err := unstructured.NestedMap(route, rType)
		if err != nil {
			return nil, "", err
		}
		if found {
			routeRule = r
			routeType = rType
			break
		}
	}

	if routeRule == nil {
		return nil, "", errors.New("Route has unsupported route type")
	}

	return routeRule, routeType, nil
}

func (r *Reconciler) SetMirrorRoute(setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	return nil
}

func (r *Reconciler) RemoveManagedRoutes() error {
	return nil
}
