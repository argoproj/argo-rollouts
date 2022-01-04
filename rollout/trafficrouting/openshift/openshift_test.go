package openshift

import (
	"context"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	openshiftclientset "github.com/openshift/client-go/route/clientset/versioned"
	"github.com/openshift/client-go/route/clientset/versioned/fake"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/record"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var routeNames = []string{"route1", "route2", "route3"}

func TestReconcilerSetWeight(t *testing.T) {
	t.Parallel()
	rollout := newRollout()
	rec := record.NewFakeEventRecorder()
	tests := []struct {
		title          string
		clientset      openshiftclientset.Interface
		weight         int
		expectedRoutes []*routev1.Route
	}{

		{
			title:     "Fail: No routes",
			clientset: fake.NewSimpleClientset(),
			weight:    50,
		},
		{
			title:     "Fail: Wrong route name",
			clientset: fake.NewSimpleClientset(&routev1.Route{ObjectMeta: metav1.ObjectMeta{Name: "new-route"}}),
			weight:    50,
		},
		{
			title:          "Succeed: Create Alternate Backends",
			clientset:      fake.NewSimpleClientset(runtimeObj(routesNoCanary(100))...),
			weight:         25,
			expectedRoutes: routesWithCanary(25),
		},
		{
			title:          "Succeed: Update Alternate Backends",
			clientset:      fake.NewSimpleClientset(runtimeObj(routesWithCanary(40))...),
			weight:         60,
			expectedRoutes: routesWithCanary(60),
		},
		{
			title:          "Succeed: Delete Alternate Backends",
			clientset:      fake.NewSimpleClientset(runtimeObj(routesWithCanary(80))...),
			weight:         0,
			expectedRoutes: routesNoCanary(100),
		},
	}
	for _, test := range tests {
		t.Run(test.title, func(t *testing.T) {
			r := NewReconciler(ReconcilerConfig{
				Rollout:  rollout,
				Client:   test.clientset,
				Recorder: rec,
			})
			err := r.SetWeight(int32(test.weight))
			if test.expectedRoutes != nil {
				assert.NoError(t, err)
				routes := make([]*routev1.Route, 0)
				for _, routeName := range routeNames {
					route, err := r.getRoute(context.TODO(), routeName)
					assert.NoError(t, err)
					routes = append(routes, route)
				}
				assert.Equal(t, test.expectedRoutes, routes)
			} else {
				assert.Error(t, err)
			}
		})

	}
}

func newRollout() *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: "stable",
					CanaryService: "canary",
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Openshift: &v1alpha1.OpenshiftTrafficRouting{
							Routes: routeNames,
						},
					},
				},
			},
		},
	}
}

func routesNoCanary(weight int32) []*routev1.Route {
	routes := make([]*routev1.Route, 0)
	for _, name := range routeNames {
		routes = append(routes, &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "default",
			},
			Spec: routev1.RouteSpec{
				To: routev1.RouteTargetReference{
					Kind:   "Service",
					Name:   "stable",
					Weight: &weight,
				},
			},
		})

	}
	return routes
}

func routesWithCanary(weight int32) []*routev1.Route {
	routes := routesNoCanary(100 - weight)
	for _, route := range routes {
		route.Spec.AlternateBackends = []routev1.RouteTargetReference{{
			Kind:   "Service",
			Name:   "canary",
			Weight: &weight,
		}}
	}
	return routes
}

func runtimeObj(routes []*routev1.Route) []runtime.Object {
	objects := make([]runtime.Object, 0)
	for _, route := range routes {
		objects = append(objects, route)
	}
	return objects
}
