package openshift

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	routev1 "github.com/openshift/api/route/v1"
	openshiftclientset "github.com/openshift/client-go/route/clientset/versioned"

	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
)

// Type defines the Openshift traffic routing type.
const (
	Type = "Openshift"
)

// ReconcilerConfig describes static configuration data
type ReconcilerConfig struct {
	Rollout  *v1alpha1.Rollout
	Client   openshiftclientset.Interface
	Recorder record.EventRecorder
}

// Reconciler implements a TrafficRoutingReconciler
type Reconciler struct {
	cfg ReconcilerConfig
	log *logrus.Entry
}

// NewReconciler will build and return an OpenShift Reconciler
func NewReconciler(cfg ReconcilerConfig) *Reconciler {
	return &Reconciler{
		cfg: cfg,
		log: logutil.WithRollout(cfg.Rollout),
	}
}

// SetWeight will change the route to either...
//	include a canary alternate backend with desired weight
//	change an existing canary backend with desired weight
//	delete a canary alternate backend
func (r *Reconciler) SetWeight(desiredWeight int32) error {
	r.sendNormalEvent("AlternateBackendWeightUpdate", fmt.Sprintf("Set alternateBackends weight to %d", desiredWeight))
	ctx := context.TODO()
	routeNameList := r.cfg.Rollout.Spec.Strategy.Canary.TrafficRouting.Openshift.Routes
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

// update Route according to following:
//	update default backend weight
//	remove alternateBackends if weight is 0
//	otherwise update alternateBackends
func (r *Reconciler) updateRoute(ctx context.Context, routeName string, desiredWeight int32) error {
	routesClient := r.cfg.Client.RouteV1().Routes(r.cfg.Rollout.GetNamespace())
	// get the route
	route, err := routesClient.Get(ctx, routeName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			msg := fmt.Sprintf("Route %q not found", routeName)
			r.sendWarningEvent("OpenshiftRouteNotFound", msg)
		}
		return err
	}

	// update default backend weight
	altWeight := 100 - desiredWeight
	r.log.Infof("updating default backend weight to %d", altWeight)
	route.Spec.To.Weight = &altWeight
	if desiredWeight == 0 {
		r.log.Infof("deleting alternateBackends")
		route.Spec.AlternateBackends = nil
	} else {
		r.log.Infof("updating alternate backend weight to %d", desiredWeight)
		route.Spec.AlternateBackends = []routev1.RouteTargetReference{{
			Kind:   "Service",
			Name:   r.cfg.Rollout.Spec.Strategy.Canary.CanaryService,
			Weight: &desiredWeight,
		}}
	}

	_, err = routesClient.Update(ctx, route, metav1.UpdateOptions{})

	return err
}

func (r *Reconciler) VerifyWeight(desiredWeight int32) (bool, error) {
	return true, nil
}

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
	r.cfg.Recorder.Eventf(r.cfg.Rollout, record.EventOptions{EventType: eventType, EventReason: id}, msg)
}

// UpdateHash informs a traffic routing reconciler about new canary/stable pod hashes
func (r *Reconciler) UpdateHash(canaryHash, stableHash string) error {
	return nil
}
