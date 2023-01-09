package openshift

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	routev1 "github.com/openshift/api/route/v1"
	openshiftclientset "github.com/openshift/client-go/route/clientset/versioned"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
)

// Openshift reconciler type
const Type = "Openshift"

// ReconcilerConfig describes static configuration data
type ReconcilerConfig struct {
	Rollout  *v1alpha1.Rollout
	Client   openshiftclientset.Interface
	Recorder record.EventRecorder
}

// Reconciler implements a TrafficRoutingReconciler
type Reconciler struct {
	Cfg ReconcilerConfig
	Log *logrus.Entry
}

// NewReconciler will build and return an OpenShift Reconciler
func NewReconciler(cfg ReconcilerConfig) *Reconciler {
	return &Reconciler{
		Cfg: cfg,
		Log: logutil.WithRollout(cfg.Rollout),
	}
}

// SetWeight changes the route configuration according to desiredWeight
func (r *Reconciler) SetWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) error {
	r.sendNormalEvent("WeightUpdate", fmt.Sprintf("Set weight to %d", desiredWeight))
	ctx := context.TODO()
	routeNameList := r.Cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Openshift.Routes
	doneChan := make(chan struct{})
	errChan := make(chan error)
	defer close(errChan)
	var wg sync.WaitGroup
	wg.Add(len(routeNameList))
	for _, routeName := range routeNameList {
		go func(routeName string) {
			defer wg.Done()
			err := r.updateRoute(ctx, routeName, desiredWeight)
			if err != nil {
				errChan <- err
			}
		}(routeName)
	}
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	errs := []error{}
	done := false
	for !done {
		select {
		case <-doneChan:
			done = true
		case err := <-errChan:
			errs = append(errs, err)
		}
	}
	return formatErrors(errs)
}

func formatErrors(errs []error) error {
	errorsCount := len(errs)
	if errorsCount == 0 {
		return nil
	} else if errorsCount == 1 {
		return errs[0]
	}
	var errMsg strings.Builder
	errMsg.WriteString(fmt.Sprintf("%d errors found: ", errorsCount))
	for _, err := range errs {
		errMsg.WriteString(fmt.Sprintf("[%s]", err))
	}
	return errors.New(errMsg.String())
}

func (r *Reconciler) getRoute(ctx context.Context, routeName string) (*routev1.Route, error) {
	return r.Cfg.Client.RouteV1().Routes(r.Cfg.Rollout.GetNamespace()).Get(ctx, routeName, metav1.GetOptions{})
}

// Update default backend weight,
// remove alternateBackends if weight is 0,
// otherwise update alternateBackends
func (r *Reconciler) updateRoute(ctx context.Context, routeName string, desiredWeight int32) error {
	// get the route
	route, err := r.getRoute(ctx, routeName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			msg := fmt.Sprintf("Route %q not found", routeName)
			r.sendWarningEvent("OpenshiftRouteNotFound", msg)
		}
		return err
	}

	// update default backend weight if weight is different
	altWeight := 100 - desiredWeight
	if route.Spec.To.Weight == &altWeight {
		r.Cfg.Recorder.Eventf(r.Cfg.Rollout, record.EventOptions{EventReason: "UpdatedRoute"}, "Route `%s` already set to desiredWeight '%d'", routeName, desiredWeight)
		return nil
	}

	r.Log.Infof("updating default backend weight to %d", altWeight)
	route.Spec.To.Weight = &altWeight
	if desiredWeight == 0 {
		r.Log.Infof("deleting alternateBackends")
		route.Spec.AlternateBackends = nil
	} else {
		r.Log.Infof("updating alternate backend weight to %d", desiredWeight)
		route.Spec.AlternateBackends = []routev1.RouteTargetReference{{
			Kind:   "Service",
			Name:   r.Cfg.Rollout.Spec.Strategy.Canary.CanaryService,
			Weight: &desiredWeight,
		}}
	}
	_, err = r.Cfg.Client.RouteV1().Routes(r.Cfg.Rollout.GetNamespace()).Update(ctx, route, metav1.UpdateOptions{})
	if err == nil {
		r.Cfg.Recorder.Eventf(r.Cfg.Rollout, record.EventOptions{EventReason: "UpdatedRoute"}, "Route `%s` set to desiredWeight '%d'", routeName, desiredWeight)
	}
	return err
}

// Verifies weight of routes given by rollout
func (r *Reconciler) VerifyWeight(desiredWeight int32, additionalDestinations ...v1alpha1.WeightDestination) (*bool, error) {
	return nil, nil
}

// Openshift reconciler type
func (r *Reconciler) Type() string {
	return Type
}

func (r *Reconciler) sendNormalEvent(id, msg string) {
	r.sendEvent(corev1.EventTypeNormal, id, msg)
}

func (r *Reconciler) sendWarningEvent(id, msg string) {
	r.sendEvent(corev1.EventTypeWarning, id, msg)
}

func (r *Reconciler) sendEvent(eventType, id, msg string) {
	r.Cfg.Recorder.Eventf(r.Cfg.Rollout, record.EventOptions{EventType: eventType, EventReason: id}, msg)
}

// UpdateHash informs a traffic routing reconciler about new canary/stable pod hashes
func (r *Reconciler) UpdateHash(canaryHash, stableHash string, additionalDestinations ...v1alpha1.WeightDestination) error {
	return nil
}

func (r *Reconciler) RemoveManagedRoutes() error {
	return nil
}

func (r *Reconciler) SetMirrorRoute(setMirrorRoute *v1alpha1.SetMirrorRoute) error {
	return nil
}

func (r *Reconciler) SetHeaderRoute(headerRouting *v1alpha1.SetHeaderRoute) error {
	return nil
}
