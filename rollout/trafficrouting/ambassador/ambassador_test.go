package ambassador_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/ambassador"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/record"
)

const (
	baseMapping = `
apiVersion: getambassador.io/v2
kind:  Mapping
metadata:
  name: myapp-mapping
  namespace: default
spec:
  prefix: /myapp/
  rewrite: /myapp/
  service: myapp:8080`

	baseMappingNoPort = `
apiVersion: getambassador.io/v2
kind:  Mapping
metadata:
  name: myapp-mapping
  namespace: default
spec:
  prefix: /myapp/
  rewrite: /myapp/
  service: myapp`

	baseMappingWithWeight = `
apiVersion: getambassador.io/v2
kind:  Mapping
metadata:
  name: myapp-mapping
  namespace: default
spec:
  prefix: /myapp/
  rewrite: /myapp/
  service: myapp:8080
  weight: 20`

	baseV3Mapping = `
apiVersion: x.getambassador.io/v3alpha1
kind:  AmbassadorMapping
metadata:
  name: myapp-mapping
  namespace: default
spec:
  hostname: 'example.com'
  prefix: /myapp/
  rewrite: /myapp/
  service: myapp:8080`

	canaryMapping = `
apiVersion: getambassador.io/v2
kind:  Mapping
metadata:
  name: myapp-mapping-canary
  namespace: default
spec:
  prefix: /myapp/
  rewrite: /myapp/
  service: myapp:8080
  weight: 20`

	canaryMappingWithZeroWeight = `
apiVersion: getambassador.io/v2
kind:  Mapping
metadata:
  name: myapp-mapping-canary
  namespace: default
spec:
  prefix: /myapp/
  rewrite: /myapp/
  service: myapp:8080
  weight: 0`
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
		reconciler *ambassador.Reconciler
	}

	setup := func() *fixture {
		r := rollout("main-service", "canary-service", []string{"myapp-mapping"})
		fakeClient := &fakeClient{}
		rec := record.NewFakeEventRecorder()
		l, _ := test.NewNullLogger()
		return &fixture{
			rollout:    r,
			fakeClient: fakeClient,
			recorder:   rec,
			reconciler: &ambassador.Reconciler{
				Rollout:  r,
				Client:   fakeClient,
				Recorder: rec,
				Log:      l.WithContext(context.TODO()),
			},
		}
	}
	t.Run("SetWeight", func(t *testing.T) {
		t.Run("will create canary mapping and set weight successfully", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()
			getReturns := []*getReturn{
				{err: k8serrors.NewNotFound(schema.GroupResource{}, "canary-mapping")},
				{obj: toUnstructured(t, baseMapping)},
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
			assert.Equal(t, 2, len(f.fakeClient.getInvokations))
			assert.Equal(t, "myapp-mapping-canary", f.fakeClient.getInvokations[0].name)
			assert.Equal(t, "myapp-mapping", f.fakeClient.getInvokations[1].name)
			assert.Equal(t, 1, len(f.fakeClient.createInvokations))
			assert.Equal(t, int64(13), ambassador.GetMappingWeight(f.fakeClient.createInvokations[0].obj))
			assert.Equal(t, "canary-service:8080", ambassador.GetMappingService(f.fakeClient.createInvokations[0].obj))
			assert.Equal(t, 0, len(f.fakeClient.updateInvokations))
			assert.Equal(t, 0, len(f.fakeClient.deleteInvokations))
		})
		t.Run("will create canary ambassadormapping and set weight successfully", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()
			getReturns := []*getReturn{
				{err: k8serrors.NewNotFound(schema.GroupResource{}, "canary-mapping")},
				{obj: toUnstructured(t, baseV3Mapping)},
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
			assert.Equal(t, 2, len(f.fakeClient.getInvokations))
			assert.Equal(t, "myapp-mapping-canary", f.fakeClient.getInvokations[0].name)
			assert.Equal(t, "myapp-mapping", f.fakeClient.getInvokations[1].name)
			assert.Equal(t, 1, len(f.fakeClient.createInvokations))
			assert.Equal(t, int64(13), ambassador.GetMappingWeight(f.fakeClient.createInvokations[0].obj))
			assert.Equal(t, "canary-service:8080", ambassador.GetMappingService(f.fakeClient.createInvokations[0].obj))
			assert.Equal(t, 0, len(f.fakeClient.updateInvokations))
			assert.Equal(t, 0, len(f.fakeClient.deleteInvokations))
		})
		t.Run("will create canary mapping with no service port", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()
			getReturns := []*getReturn{
				{err: k8serrors.NewNotFound(schema.GroupResource{}, "canary-mapping")},
				{obj: toUnstructured(t, baseMappingNoPort)},
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
			assert.Equal(t, 2, len(f.fakeClient.getInvokations))
			assert.Equal(t, "myapp-mapping-canary", f.fakeClient.getInvokations[0].name)
			assert.Equal(t, "myapp-mapping", f.fakeClient.getInvokations[1].name)
			assert.Equal(t, 1, len(f.fakeClient.createInvokations))
			assert.Equal(t, int64(13), ambassador.GetMappingWeight(f.fakeClient.createInvokations[0].obj))
			assert.Equal(t, "canary-service", ambassador.GetMappingService(f.fakeClient.createInvokations[0].obj))
			assert.Equal(t, 0, len(f.fakeClient.updateInvokations))
			assert.Equal(t, 0, len(f.fakeClient.deleteInvokations))
		})
		t.Run("will update canary mapping according to provided weight", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()
			getReturns := []*getReturn{
				{obj: toUnstructured(t, canaryMapping)},
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
			assert.Equal(t, "myapp-mapping-canary", f.fakeClient.getInvokations[0].name)
			assert.Equal(t, 0, len(f.fakeClient.createInvokations))
			assert.Equal(t, 1, len(f.fakeClient.updateInvokations))
			assert.Equal(t, 0, len(f.fakeClient.deleteInvokations))
		})
		t.Run("will cleanup canary mapping if current weight is zero", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()
			getReturns := []*getReturn{
				{obj: toUnstructured(t, canaryMappingWithZeroWeight)},
			}
			createReturns := []*createReturn{
				{nil, nil},
			}
			f.fakeClient.getReturns = getReturns
			f.fakeClient.createReturns = createReturns

			// when
			err := f.reconciler.SetWeight(0)

			// then
			assert.NoError(t, err)
			assert.Equal(t, 1, len(f.fakeClient.getInvokations))
			assert.Equal(t, "myapp-mapping-canary", f.fakeClient.getInvokations[0].name)
			assert.Equal(t, 0, len(f.fakeClient.createInvokations))
			assert.Equal(t, 1, len(f.fakeClient.updateInvokations))
			assert.Equal(t, 1, len(f.fakeClient.deleteInvokations))
		})
		t.Run("will return error if base mapping defines the weight", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()
			getReturns := []*getReturn{
				{err: k8serrors.NewNotFound(schema.GroupResource{}, "canary-mapping")},
				{obj: toUnstructured(t, baseMappingWithWeight)},
			}
			f.fakeClient.getReturns = getReturns

			// when
			err := f.reconciler.SetWeight(20)

			// then
			assert.Error(t, err)
			assert.Equal(t, 2, len(f.fakeClient.getInvokations))
			assert.Equal(t, "myapp-mapping-canary", f.fakeClient.getInvokations[0].name)
			assert.Equal(t, "myapp-mapping", f.fakeClient.getInvokations[1].name)
			assert.Equal(t, 0, len(f.fakeClient.createInvokations))
			assert.Equal(t, 0, len(f.fakeClient.updateInvokations))
			assert.Equal(t, 0, len(f.fakeClient.deleteInvokations))
		})
		t.Run("will return error if base mapping not found", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()
			getReturns := []*getReturn{
				{err: k8serrors.NewNotFound(schema.GroupResource{}, "canary-mapping")},
				{err: k8serrors.NewNotFound(schema.GroupResource{}, "base-mapping")},
			}
			f.fakeClient.getReturns = getReturns

			// when
			err := f.reconciler.SetWeight(20)

			// then
			assert.Error(t, err)
			assert.Equal(t, 2, len(f.fakeClient.getInvokations))
			assert.Equal(t, "myapp-mapping-canary", f.fakeClient.getInvokations[0].name)
			assert.Equal(t, "myapp-mapping", f.fakeClient.getInvokations[1].name)
			assert.Equal(t, 0, len(f.fakeClient.createInvokations))
			assert.Equal(t, 0, len(f.fakeClient.updateInvokations))
			assert.Equal(t, 0, len(f.fakeClient.deleteInvokations))
		})
		t.Run("will respect kube resource name size when creating the canary mapping", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()
			providedMappingName := "very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-"
			expectedName := "very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mapping-name-very-long-mappin-canary"
			f.rollout.Spec.Strategy.Canary.TrafficRouting.Ambassador.Mappings = []string{providedMappingName}

			getReturns := []*getReturn{
				{obj: toUnstructured(t, canaryMapping)},
			}
			f.fakeClient.getReturns = getReturns

			// when
			err := f.reconciler.SetWeight(20)

			// then
			assert.NoError(t, err)
			assert.Equal(t, 1, len(f.fakeClient.getInvokations))
			assert.Equal(t, expectedName, f.fakeClient.getInvokations[0].name)
		})
		t.Run("will create multiple canary mappings when provided multiple base mappings", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()
			mappings := []string{"base-mapping-01", "base-mapping-02", "base-mapping-03"}
			f.rollout.Spec.Strategy.Canary.TrafficRouting.Ambassador.Mappings = mappings
			getReturns := []*getReturn{
				{obj: toUnstructured(t, baseMapping)},
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
			assert.Equal(t, 3, len(f.fakeClient.getInvokations))
			assert.Equal(t, 0, len(f.fakeClient.createInvokations))
			assert.Equal(t, 3, len(f.fakeClient.updateInvokations))
			assert.Equal(t, int64(13), ambassador.GetMappingWeight(f.fakeClient.updateInvokations[0].obj))
			assert.Equal(t, int64(13), ambassador.GetMappingWeight(f.fakeClient.updateInvokations[1].obj))
			assert.Equal(t, int64(13), ambassador.GetMappingWeight(f.fakeClient.updateInvokations[2].obj))
			assert.Equal(t, 0, len(f.fakeClient.deleteInvokations))
		})
		t.Run("will return errors from all mapping creations", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()
			mappings := []string{"base-mapping-01", "base-mapping-02", "base-mapping-03"}
			f.rollout.Spec.Strategy.Canary.TrafficRouting.Ambassador.Mappings = mappings
			getReturns := []*getReturn{
				{err: errors.New("error getting mapping 1")},
				{err: errors.New("error getting mapping 2")},
				{err: errors.New("error getting mapping 3")},
			}
			createReturns := []*createReturn{
				{nil, nil},
			}
			f.fakeClient.getReturns = getReturns
			f.fakeClient.createReturns = createReturns

			// when
			err := f.reconciler.SetWeight(13)

			// then
			assert.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), "3 errors found"))
			assert.Equal(t, 3, len(f.fakeClient.getInvokations))
			assert.Equal(t, 0, len(f.fakeClient.createInvokations))
			assert.Equal(t, 0, len(f.fakeClient.updateInvokations))
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
			assert.Equal(t, ambassador.Type, tp)
		})
	})
	t.Run("VerifyWeight", func(t *testing.T) {
		t.Run("verify weight will always return nil", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()

			// when
			verified, err := f.reconciler.VerifyWeight(0)

			// then
			assert.Nil(t, err)
			assert.Nil(t, verified)
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

func TestReconcilerSetHeaderRoute(t *testing.T) {
	type fixture struct {
		rollout    *v1alpha1.Rollout
		fakeClient *fakeClient
		recorder   record.EventRecorder
		reconciler *ambassador.Reconciler
	}

	setup := func() *fixture {
		r := rollout("main-service", "canary-service", []string{"myapp-mapping"})
		fakeClient := &fakeClient{}
		rec := record.NewFakeEventRecorder()
		l, _ := test.NewNullLogger()
		return &fixture{
			rollout:    r,
			fakeClient: fakeClient,
			recorder:   rec,
			reconciler: &ambassador.Reconciler{
				Rollout:  r,
				Client:   fakeClient,
				Recorder: rec,
				Log:      l.WithContext(context.TODO()),
			},
		}
	}
	t.Run("SetHeaderRoute", func(t *testing.T) {
		t.Run("will always return nil", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()

			// when
			err := f.reconciler.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
				Name: "set-header",
				Match: []v1alpha1.HeaderRoutingMatch{{
					HeaderName: "header-name",
					HeaderValue: &v1alpha1.StringMatch{
						Exact: "value",
					},
				}},
			})

			// then
			assert.Nil(t, err)

			err = f.reconciler.RemoveManagedRoutes()
			assert.Nil(t, err)
		})
	})
}

func TestReconcilerSetMirrorRoute(t *testing.T) {
	type fixture struct {
		rollout    *v1alpha1.Rollout
		fakeClient *fakeClient
		recorder   record.EventRecorder
		reconciler *ambassador.Reconciler
	}

	setup := func() *fixture {
		r := rollout("main-service", "canary-service", []string{"myapp-mapping"})
		fakeClient := &fakeClient{}
		rec := record.NewFakeEventRecorder()
		l, _ := test.NewNullLogger()
		return &fixture{
			rollout:    r,
			fakeClient: fakeClient,
			recorder:   rec,
			reconciler: &ambassador.Reconciler{
				Rollout:  r,
				Client:   fakeClient,
				Recorder: rec,
				Log:      l.WithContext(context.TODO()),
			},
		}
	}
	t.Run("SetMirrorRoute", func(t *testing.T) {
		t.Run("will always return nil", func(t *testing.T) {
			// given
			t.Parallel()
			f := setup()

			// when
			err := f.reconciler.SetMirrorRoute(&v1alpha1.SetMirrorRoute{
				Name: "mirror-route",
				Match: []v1alpha1.RouteMatch{{
					Method: &v1alpha1.StringMatch{Exact: "GET"},
				}},
			})

			// then
			assert.Nil(t, err)

			err = f.reconciler.RemoveManagedRoutes()
			assert.Nil(t, err)
		})
	})
}

func TestGetMappingService(t *testing.T) {
	t.Run("will return empty string if service not found", func(t *testing.T) {
		// given
		t.Parallel()
		manifest := "kind: Deployment"

		// when
		service := ambassador.GetMappingService(toUnstructured(t, manifest))

		// then
		assert.Equal(t, "", service)
	})
}

func TestGetMappingGVR(t *testing.T) {
	t.Run("will return default gvr if apiVersion not provided", func(t *testing.T) {
		// when
		gvr := ambassador.GetMappingGVR()

		// then
		assert.Equal(t, "getambassador.io", gvr.Group)
		assert.Equal(t, "v2", gvr.Version)
		assert.Equal(t, "mappings", gvr.Resource)
	})
	t.Run("will get gvr successfully", func(t *testing.T) {
		// given
		defaults.SetAmbassadorAPIVersion("v1")

		// when
		gvr := ambassador.GetMappingGVR()

		// then
		assert.Equal(t, "getambassador.io", gvr.Group)
		assert.Equal(t, "v1", gvr.Version)
		assert.Equal(t, "mappings", gvr.Resource)
	})
	t.Run("will get valid gvr even if apiVersion has the wrong domain", func(t *testing.T) {
		// given
		apiVersion := "invalid.com/v1alpha1"
		defaults.SetAmbassadorAPIVersion(apiVersion)

		// when
		gvr := ambassador.GetMappingGVR()

		// then
		assert.Equal(t, "invalid.com", gvr.Group)
		assert.Equal(t, "v1alpha1", gvr.Version)
		assert.Equal(t, "mappings", gvr.Resource)
		assert.Equal(t, apiVersion, defaults.GetAmbassadorAPIVersion())
	})
	t.Run("will get correct gvr for x.getambassador.io api group", func(t *testing.T) {
		// given
		apiVersion := "x.getambassador.io/v3alpha1"
		defaults.SetAmbassadorAPIVersion(apiVersion)

		// when
		gvr := ambassador.GetMappingGVR()

		// then
		assert.Equal(t, "x.getambassador.io", gvr.Group)
		assert.Equal(t, "v3alpha1", gvr.Version)
		assert.Equal(t, "ambassadormappings", gvr.Resource)
		assert.Equal(t, apiVersion, defaults.GetAmbassadorAPIVersion())
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

func rollout(stableSvc, canarySvc string, mappings []string) *v1alpha1.Rollout {
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
						Ambassador: &v1alpha1.AmbassadorTrafficRouting{
							Mappings: mappings,
						},
					},
				},
			},
		},
	}
}
