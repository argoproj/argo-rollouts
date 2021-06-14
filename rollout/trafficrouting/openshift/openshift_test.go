package openshift_test

import (
	"context"
	"sync"
	"testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/openshift"
	"github.com/argoproj/argo-rollouts/utils/record"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
)

const (
	routeNoAlt = `
apiVersion: route.openshift.io/v1
kind:  Route
metadata:
  name: myapp-mapping
spec:
  to:
    kind: Service
    name: stable-service
    weight: 100
`
	routeWithAlt = `
apiVersion: route.openshift.io/v1
kind:  Route
metadata:
  name: myapp-mapping
spec:
  to:
    kind: Service
    name: stable-service
    weight: 100
  alternateBackends:
  - kind: Service
    name: canary-service
    weight: 100
`
)

type fakeClient struct {
	getInvokations    []*getInvokation
	getReturns        []*getReturn
	createInvokations []*createInvokation
	createReturns     []*createReturn
	updateInvokations []*updateInvokation
	updateReturns     []error
	deleteInvokations []*deleteInvokation
	deleteReturns     []error
	mu                sync.Mutex
}

type deleteInvokation struct {
	name string
}

type updateInvokation struct {
	obj *unstructured.Unstructured
}

type getInvokation struct {
	name string
}

type createInvokation struct {
	obj          *unstructured.Unstructured
	options      metav1.CreateOptions
	subresources []string
}

type createReturn struct {
	obj *unstructured.Unstructured
	err error
}

type getReturn struct {
	obj *unstructured.Unstructured
	err error
}

func (f *fakeClient) Get(ctx context.Context, name string, options metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	invokation := &getInvokation{name: name}
	f.mu.Lock()
	f.getInvokations = append(f.getInvokations, invokation)
	f.mu.Unlock()
	if len(f.getReturns) == 0 {
		return nil, nil
	}
	ret := f.getReturns[0]
	if len(f.getReturns) >= len(f.getInvokations) {
		ret = f.getReturns[len(f.getInvokations)-1]
	}
	return ret.obj, ret.err
}

func (f *fakeClient) Create(ctx context.Context, obj *unstructured.Unstructured, options metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	invokation := &createInvokation{
		obj:          obj,
		options:      options,
		subresources: subresources,
	}
	f.mu.Lock()
	f.createInvokations = append(f.createInvokations, invokation)
	f.mu.Unlock()
	if len(f.createReturns) == 0 {
		return nil, nil
	}
	ret := f.createReturns[0]
	if len(f.createReturns) >= len(f.createInvokations) {
		ret = f.createReturns[len(f.createInvokations)-1]
	}
	return ret.obj, ret.err
}

func (f *fakeClient) Update(ctx context.Context, obj *unstructured.Unstructured, options metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	invokation := &updateInvokation{obj: obj}
	f.mu.Lock()
	f.updateInvokations = append(f.updateInvokations, invokation)
	f.mu.Unlock()
	if len(f.updateReturns) == 0 {
		return nil, nil
	}
	err := f.updateReturns[0]
	if len(f.updateReturns) >= len(f.updateInvokations) {
		err = f.updateReturns[len(f.updateInvokations)-1]
	}
	return nil, err
}

func (f *fakeClient) Delete(ctx context.Context, name string, options metav1.DeleteOptions, subresources ...string) error {
	invokation := &deleteInvokation{name: name}
	f.mu.Lock()
	f.deleteInvokations = append(f.deleteInvokations, invokation)
	f.mu.Unlock()
	if len(f.deleteReturns) == 0 {
		return nil
	}
	err := f.deleteReturns[0]
	if len(f.deleteReturns) >= len(f.deleteInvokations) {
		err = f.deleteReturns[len(f.deleteInvokations)-1]
	}
	return err
}

func TestReconciler_SetWeight(t *testing.T) {
	type fixture struct {
		rollout    *v1alpha1.Rollout
		fakeClient *fakeClient
		recorder   record.EventRecorder
		reconciler *openshift.Reconciler
	}

	setup := func() *fixture {
		r := rollout("main-service", "canary-service", []string{"main-route"})
		fakeClient := &fakeClient{}
		rec := record.NewFakeEventRecorder()
		l, _ := test.NewNullLogger()
		return &fixture{
			rollout:    r,
			fakeClient: fakeClient,
			recorder:   rec,
			reconciler: &openshift.Reconciler{
				Rollout:  r,
				Client:   fakeClient,
				Recorder: rec,
				Log:      l.WithContext(context.TODO()),
			},
		}
	}
	t.Run("SetWeight", func(t *testing.T) {
		t.Run("will create and update alternateBackends of route", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()
			getReturns := []*getReturn{
				{obj: toUnstructured(t, routeNoAlt)},
			}
			createReturns := []*createReturn{
				{nil, nil},
			}
			f.fakeClient.getReturns = getReturns
			f.fakeClient.createReturns = createReturns

			// when
			err := f.reconciler.SetWeight(13)

			// then
			assert.NoError(t, err)
			assert.Equal(t, 1, len(f.fakeClient.getInvokations))
			assert.Equal(t, "main-route", f.fakeClient.getInvokations[0].name)
			assert.Equal(t, 0, len(f.fakeClient.createInvokations))
			assert.Equal(t, 1, len(f.fakeClient.updateInvokations))
			assert.Equal(t, 0, len(f.fakeClient.deleteInvokations))
		})
		t.Run("will update alternateBackends according to provided weight", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()
			getReturns := []*getReturn{
				{obj: toUnstructured(t, routeWithAlt)},
			}
			createReturns := []*createReturn{
				{nil, nil},
			}
			f.fakeClient.getReturns = getReturns
			f.fakeClient.createReturns = createReturns

			// when
			err := f.reconciler.SetWeight(13)

			// then
			assert.NoError(t, err)
			assert.Equal(t, 1, len(f.fakeClient.getInvokations))
			assert.Equal(t, "main-route", f.fakeClient.getInvokations[0].name)
			assert.Equal(t, 0, len(f.fakeClient.createInvokations))
			assert.Equal(t, 1, len(f.fakeClient.updateInvokations))
			assert.Equal(t, 0, len(f.fakeClient.deleteInvokations))
		})
	})
	t.Run("Type", func(t *testing.T) {
		t.Run("will validate returned type", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()

			// when
			tp := f.reconciler.Type()

			// then
			assert.Equal(t, openshift.Type, tp)
		})
	})
	t.Run("VerifyWeight", func(t *testing.T) {
		t.Run("verify weight will always return true", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()

			// when
			verified, err := f.reconciler.VerifyWeight(0)

			// then
			assert.Nil(t, err)
			assert.True(t, verified)
		})
	})
	t.Run("UpdateHash", func(t *testing.T) {
		t.Run("will always return nil", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()

			// when
			err := f.reconciler.UpdateHash("", "")

			// then
			assert.Nil(t, err)
		})
	})
}

func TestGetRouteGVR(t *testing.T) {
	t.Run("will return default gvr if apiVersion not provided", func(t *testing.T) {
		// when
		gvr := openshift.GetRouteGVR()

		// then
		assert.Equal(t, "route.openshift.io", gvr.Group)
		assert.Equal(t, "v1", gvr.Version)
		assert.Equal(t, "routes", gvr.Resource)
	})
	t.Run("will get gvr successfully", func(t *testing.T) {
		// given
		openshift.SetAPIVersion("v2")

		// when
		gvr := openshift.GetRouteGVR()

		// then
		assert.Equal(t, "route.openshift.io", gvr.Group)
		assert.Equal(t, "v2", gvr.Version)
		assert.Equal(t, "routes", gvr.Resource)
	})
	t.Run("will get valid gvr even if apiVersion has the wrong domain", func(t *testing.T) {
		// given
		apiVersion := "invalid.com/v1alpha1"
		openshift.SetAPIVersion(apiVersion)

		// when
		gvr := openshift.GetRouteGVR()

		// then
		assert.Equal(t, "route.openshift.io", gvr.Group)
		assert.Equal(t, "v1alpha1", gvr.Version)
		assert.Equal(t, "routes", gvr.Resource)
		assert.Equal(t, apiVersion, openshift.GetAPIVersion())
	})
}

func toUnstructured(t *testing.T, manifest string) *unstructured.Unstructured {
	t.Helper()
	obj := &unstructured.Unstructured{}

	dec := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	_, _, err := dec.Decode([]byte(manifest), nil, obj)
	if err != nil {
		t.Fatal(err)
	}
	return obj
}

func rollout(stableSvc, canarySvc string, routes []string) *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: "default",
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					StableService: stableSvc,
					CanaryService: canarySvc,
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Openshift: &v1alpha1.OpenshiftTrafficRouting{
							Routes: routes,
						},
					},
				},
			},
		},
	}
}
