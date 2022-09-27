package appmesh

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tj/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	k8stesting "k8s.io/client-go/testing"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	testutil "github.com/argoproj/argo-rollouts/test/util"
	"github.com/argoproj/argo-rollouts/utils/record"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

const (
	sampleOldCanaryHash = "canary-old"
	sampleNewCanaryHash = "canary-new"
	sampleOldStableHash = "stable-old"
	sampleNewStableHash = "stable-new"
)

func fakeRollout() *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rollout",
			Namespace: "myns",
		},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						AppMesh: &v1alpha1.AppMeshTrafficRouting{
							VirtualService: &v1alpha1.AppMeshVirtualService{
								Name: "mysvc",
							},
							VirtualNodeGroup: &v1alpha1.AppMeshVirtualNodeGroup{
								CanaryVirtualNodeRef: &v1alpha1.AppMeshVirtualNodeReference{
									Name: "mysvc-canary-vn",
								},
								StableVirtualNodeRef: &v1alpha1.AppMeshVirtualNodeReference{
									Name: "mysvc-stable-vn",
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestSetWeightWithMissingVsvc(t *testing.T) {
	client := testutil.NewFakeDynamicClient()
	ro := fakeRollout()
	cfg := ReconcilerConfig{
		Rollout:  ro,
		Client:   client,
		Recorder: record.NewFakeEventRecorder(),
	}
	r := NewReconciler(cfg)
	err := r.SetWeight(0)
	assert.EqualError(t, err, ErrVirtualServiceMissing)
	actions := client.Actions()
	assert.Len(t, actions, 1)
	assert.True(t, actions[0].Matches("get", "virtualservices"))
}

func TestSetWeightVsvcWithVnodeProvider(t *testing.T) {
	vsvc := unstructuredutil.StrToUnstructuredUnsafe(vsvcWithVnode)
	client := testutil.NewFakeDynamicClient(vsvc)
	ro := fakeRollout()
	cfg := ReconcilerConfig{
		Rollout:  ro,
		Client:   client,
		Recorder: record.NewFakeEventRecorder(),
	}
	r := NewReconciler(cfg)
	err := r.SetWeight(0)
	assert.EqualError(t, err, ErrVirtualServiceNotUsingVirtualRouter)
	actions := client.Actions()
	assert.Len(t, actions, 1)
	assert.True(t, actions[0].Matches("get", "virtualservices"))
}

func TestSetWeightForVsvcWithMissingVrouter(t *testing.T) {
	vsvc := unstructuredutil.StrToUnstructuredUnsafe(vsvcWithVrouter)
	client := testutil.NewFakeDynamicClient(vsvc)
	cfg := ReconcilerConfig{
		Rollout:  fakeRollout(),
		Client:   client,
		Recorder: record.NewFakeEventRecorder(),
	}
	r := NewReconciler(cfg)
	err := r.SetWeight(50)
	assert.EqualError(t, err, ErrVirtualRouterMissing)
	actions := client.Actions()
	assert.Len(t, actions, 2)
	assert.True(t, actions[0].Matches("get", "virtualservices"))
	assert.True(t, actions[1].Matches("get", "virtualrouters"))
}

func TestSetWeightForVsvcWithVrouter(t *testing.T) {
	type args struct {
		vsvc      *unstructured.Unstructured
		vrouter   *unstructured.Unstructured
		routeType string
		rollout   *v1alpha1.Rollout
	}

	fixtures := []struct {
		name string
		args args
	}{
		{
			name: "http",
			args: args{
				vsvc:      unstructuredutil.StrToUnstructuredUnsafe(vsvcWithVrouter),
				vrouter:   unstructuredutil.StrToUnstructuredUnsafe(vrouterWithHTTPRoutes),
				routeType: "httpRoute",
				rollout:   fakeRollout(),
			},
		},
		{
			name: "tcp",
			args: args{
				vsvc:      unstructuredutil.StrToUnstructuredUnsafe(vsvcWithVrouter),
				vrouter:   unstructuredutil.StrToUnstructuredUnsafe(vrouterWithTCPRoutes),
				routeType: "tcpRoute",
				rollout:   fakeRollout(),
			},
		},
		{
			name: "http2",
			args: args{
				vsvc:      unstructuredutil.StrToUnstructuredUnsafe(vsvcWithVrouter),
				vrouter:   unstructuredutil.StrToUnstructuredUnsafe(vrouterWithHTTP2Routes),
				routeType: "http2Route",
				rollout:   fakeRollout(),
			},
		},
		{
			name: "grpc",
			args: args{
				vsvc:      unstructuredutil.StrToUnstructuredUnsafe(vsvcWithVrouter),
				vrouter:   unstructuredutil.StrToUnstructuredUnsafe(vrouterWithGRPCRoutes),
				routeType: "grpcRoute",
				rollout:   fakeRollout(),
			},
		},
	}

	for _, wantUpdate := range []bool{true, false} {
		for _, f := range fixtures {
			fixture := f
			t.Run(fmt.Sprintf("%s-%t", fixture.name, wantUpdate), func(t *testing.T) {
				t.Parallel()
				client := testutil.NewFakeDynamicClient(fixture.args.vsvc, fixture.args.vrouter)
				cfg := ReconcilerConfig{
					Rollout:  fixture.args.rollout,
					Client:   client,
					Recorder: record.NewFakeEventRecorder(),
				}
				r := NewReconciler(cfg)
				desiredWeight := 0
				if wantUpdate {
					desiredWeight = 55
				}
				err := r.SetWeight(int32(desiredWeight))
				assert.Nil(t, err)
				actions := client.Actions()
				if wantUpdate {
					assert.Len(t, actions, 3)
					assert.True(t, actions[0].Matches("get", "virtualservices"))
					assert.True(t, actions[1].Matches("get", "virtualrouters"))
					assert.True(t, actions[2].Matches("update", "virtualrouters"))
					assertSetWeightAction(t, actions[2], int64(desiredWeight), fixture.args.routeType)
				} else {
					assert.Len(t, actions, 2)
					assert.True(t, actions[0].Matches("get", "virtualservices"))
					assert.True(t, actions[1].Matches("get", "virtualrouters"))
				}
			})
		}
	}
}

func TestSetWeightWithUpdateVirtualRouterError(t *testing.T) {
	vsvc := unstructuredutil.StrToUnstructuredUnsafe(vsvcWithVrouter)
	vrouter := unstructuredutil.StrToUnstructuredUnsafe(vrouterWithHTTPRoutes)
	client := testutil.NewFakeDynamicClient(vsvc, vrouter)
	updateError := errors.New("Failed to update virtual-router")
	client.PrependReactor("update", "virtualrouters", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, ret, updateError
	})
	cfg := ReconcilerConfig{
		Rollout:  fakeRollout(),
		Client:   client,
		Recorder: record.NewFakeEventRecorder(),
	}
	r := NewReconciler(cfg)
	desiredWeight := 50
	err := r.SetWeight(int32(desiredWeight))
	assert.Equal(t, updateError.Error(), err.Error())
	actions := client.Actions()
	assert.Len(t, actions, 3)
	assert.True(t, actions[0].Matches("get", "virtualservices"))
	assert.True(t, actions[1].Matches("get", "virtualrouters"))
	assert.True(t, actions[2].Matches("update", "virtualrouters"))
	assertSetWeightAction(t, actions[2], int64(desiredWeight), "httpRoute")
}

func TestSetWeightWithInvalidRoutes(t *testing.T) {
	type args struct {
		routes             []interface{}
		fieldPathWithError string
	}

	fixtures := []struct {
		name string
		args args
	}{
		{
			name: "missing routes",
			args: args{
				routes:             nil,
				fieldPathWithError: field.NewPath("spec", "routes").String(),
			},
		},
		{
			name: "route with malformed content",
			args: args{
				routes: []interface{}{
					"malformed-content",
				},
				fieldPathWithError: field.NewPath("spec", "routes").Index(0).String(),
			},
		},
		{
			name: "route with no name",
			args: args{
				routes: []interface{}{
					map[string]interface{}{
						"httpRoute": map[string]interface{}{},
					},
				},
				fieldPathWithError: field.NewPath("spec", "routes").Index(0).Child("name").String(),
			},
		},
		{
			name: "route with bad route-type",
			args: args{
				routes: []interface{}{
					map[string]interface{}{
						"name":     "primary",
						"badRoute": map[string]interface{}{},
					},
				},
				fieldPathWithError: field.NewPath("spec", "routes").Index(0).String(),
			},
		},
		{
			name: "route with no targets",
			args: args{
				routes: []interface{}{
					map[string]interface{}{
						"name":      "primary",
						"httpRoute": map[string]interface{}{},
					},
				},
				fieldPathWithError: field.NewPath("spec", "routes").Index(0).Child("httpRoute").Child("action").Child("weightedTargets").String(),
			},
		},
	}

	for _, f := range fixtures {
		fixture := f
		t.Run(f.name, func(t *testing.T) {
			var err error
			vsvc := unstructuredutil.StrToUnstructuredUnsafe(vsvcWithVrouter)
			vrouter := unstructuredutil.StrToUnstructuredUnsafe(vrouterWithHTTPRoutes)
			if fixture.args.routes == nil {
				unstructured.RemoveNestedField(vrouter.Object, "spec", "routes")
			} else {
				err = unstructured.SetNestedSlice(vrouter.Object, fixture.args.routes, "spec", "routes")
			}
			assert.Nil(t, err)
			client := testutil.NewFakeDynamicClient(vsvc, vrouter)
			cfg := ReconcilerConfig{
				Rollout:  fakeRollout(),
				Client:   client,
				Recorder: record.NewFakeEventRecorder(),
			}
			r := NewReconciler(cfg)
			desiredWeight := 55
			err = r.SetWeight(int32(desiredWeight))
			assert.NotNil(t, err)
			assert.Equal(t, (err.(*field.Error)).Field, fixture.args.fieldPathWithError)
			actions := client.Actions()
			assert.Len(t, actions, 2)
			assert.True(t, actions[0].Matches("get", "virtualservices"))
			assert.True(t, actions[1].Matches("get", "virtualrouters"))
		})
	}
}

func TestSetWeightForRolloutWithRouteFilter(t *testing.T) {
	type args struct {
		vsvc         *unstructured.Unstructured
		vrouter      *unstructured.Unstructured
		routeType    string
		rollout      *v1alpha1.Rollout
		routeFilters []string
		wantUpdate   bool
	}

	fixtures := []struct {
		name string
		args args
	}{
		{
			name: "with matched route-filter",
			args: args{
				vsvc:         unstructuredutil.StrToUnstructuredUnsafe(vsvcWithVrouter),
				vrouter:      unstructuredutil.StrToUnstructuredUnsafe(vrouterWithHTTPRoutes),
				routeType:    "httpRoute",
				rollout:      fakeRollout(),
				routeFilters: []string{"primary"},
				wantUpdate:   true,
			},
		},
		{
			name: "with mismatched route-filter",
			args: args{
				vsvc:         unstructuredutil.StrToUnstructuredUnsafe(vsvcWithVrouter),
				vrouter:      unstructuredutil.StrToUnstructuredUnsafe(vrouterWithHTTPRoutes),
				routeType:    "httpRoute",
				rollout:      fakeRollout(),
				routeFilters: []string{"unknown"},
				wantUpdate:   false,
			},
		},
		{
			name: "with multiple route-filter",
			args: args{
				vsvc:         unstructuredutil.StrToUnstructuredUnsafe(vsvcWithVrouter),
				vrouter:      unstructuredutil.StrToUnstructuredUnsafe(vrouterWithHTTPRoutes),
				routeType:    "httpRoute",
				rollout:      fakeRollout(),
				routeFilters: []string{"unknown", "primary"},
				wantUpdate:   true,
			},
		},
	}

	for _, f := range fixtures {
		fixture := f
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			client := testutil.NewFakeDynamicClient(fixture.args.vsvc, fixture.args.vrouter)
			ro := fixture.args.rollout
			ro.Spec.Strategy.Canary.TrafficRouting.AppMesh.VirtualService.Routes = fixture.args.routeFilters
			cfg := ReconcilerConfig{
				Rollout:  fixture.args.rollout,
				Client:   client,
				Recorder: record.NewFakeEventRecorder(),
			}
			r := NewReconciler(cfg)
			desiredWeight := 55
			err := r.SetWeight(int32(desiredWeight))
			assert.Nil(t, err)
			actions := client.Actions()
			if fixture.args.wantUpdate {
				assert.Len(t, actions, 3)
				assert.True(t, actions[0].Matches("get", "virtualservices"))
				assert.True(t, actions[1].Matches("get", "virtualrouters"))
				assert.True(t, actions[2].Matches("update", "virtualrouters"))
				assertSetWeightAction(t, actions[2], int64(desiredWeight), fixture.args.routeType)
			} else {
				assert.Len(t, actions, 2)
				assert.True(t, actions[0].Matches("get", "virtualservices"))
				assert.True(t, actions[1].Matches("get", "virtualrouters"))
			}
		})
	}
}

func TestUpdateHash(t *testing.T) {
	type args struct {
		newCanaryHash      string
		newStableHash      string
		existingCanaryHash string
		existingStableHash string
		expectedCanaryHash string
		expectedStableHash string
		rollout            *v1alpha1.Rollout
	}

	fixtures := []struct {
		name string
		args args
	}{
		{
			name: "with no existing hashes",
			args: args{
				newCanaryHash:      sampleNewCanaryHash,
				expectedCanaryHash: sampleNewCanaryHash,
				newStableHash:      sampleNewStableHash,
				expectedStableHash: sampleNewStableHash,
				rollout:            fakeRollout(),
			},
		},
		{
			name: "with different existing hashes",
			args: args{
				newCanaryHash:      sampleNewCanaryHash,
				existingCanaryHash: sampleOldCanaryHash,
				expectedCanaryHash: sampleNewCanaryHash,
				newStableHash:      sampleNewStableHash,
				existingStableHash: sampleOldStableHash,
				expectedStableHash: sampleNewStableHash,
				rollout:            fakeRollout(),
			},
		},
		{
			name: "with existing hashes cleared",
			args: args{
				newCanaryHash:      "",
				existingCanaryHash: sampleOldCanaryHash,
				expectedCanaryHash: defaultCanaryHash,
				newStableHash:      "",
				existingStableHash: sampleOldStableHash,
				expectedStableHash: defaultStableHash,
				rollout:            fakeRollout(),
			},
		},
		{
			name: "with canaryHash == stableHash",
			args: args{
				newCanaryHash:      "12345",
				existingCanaryHash: sampleOldCanaryHash,
				expectedCanaryHash: defaultCanaryHash,
				existingStableHash: sampleOldStableHash,
				newStableHash:      "12345",
				expectedStableHash: "12345",
				rollout:            fakeRollout(),
			},
		},
	}

	for _, f := range fixtures {
		fixture := f
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			canaryVnode := createVnodeWithHash(baselineCanaryVnode, fixture.args.existingCanaryHash)
			stableVnode := createVnodeWithHash(baselineStableVnode, fixture.args.existingStableHash)
			client := testutil.NewFakeDynamicClient(canaryVnode, stableVnode)
			cfg := ReconcilerConfig{
				Rollout:  fixture.args.rollout,
				Client:   client,
				Recorder: record.NewFakeEventRecorder(),
			}
			r := NewReconciler(cfg)

			err := r.UpdateHash(fixture.args.newCanaryHash, fixture.args.newStableHash)
			assert.Nil(t, err)
			actions := client.Actions()
			assert.Len(t, actions, 4)
			assert.True(t, actions[0].Matches("get", "virtualnodes"))
			assert.True(t, actions[1].Matches("update", "virtualnodes"))
			assertUpdateHashAction(t, actions[1], fixture.args.expectedStableHash)
			assert.True(t, actions[2].Matches("get", "virtualnodes"))
			assert.True(t, actions[3].Matches("update", "virtualnodes"))
			assertUpdateHashAction(t, actions[3], fixture.args.expectedCanaryHash)
		})
	}
}

func TestSetHeaderRoute(t *testing.T) {
	t.Run("not implemented check", func(t *testing.T) {
		t.Parallel()
		client := testutil.NewFakeDynamicClient()
		cfg := ReconcilerConfig{
			Rollout:  fakeRollout(),
			Client:   client,
			Recorder: record.NewFakeEventRecorder(),
		}
		r := NewReconciler(cfg)

		err := r.SetHeaderRoute(&v1alpha1.SetHeaderRoute{
			Name: "set-header",
			Match: []v1alpha1.HeaderRoutingMatch{{
				HeaderName: "header-name",
				HeaderValue: &v1alpha1.StringMatch{
					Exact: "value",
				},
			}},
		})
		assert.Nil(t, err)

		err = r.RemoveManagedRoutes()
		assert.Nil(t, err)

		actions := client.Actions()
		assert.Len(t, actions, 0)
	})
}

func TestSetMirrorRoute(t *testing.T) {
	t.Run("not implemented check", func(t *testing.T) {
		t.Parallel()
		client := testutil.NewFakeDynamicClient()
		cfg := ReconcilerConfig{
			Rollout:  fakeRollout(),
			Client:   client,
			Recorder: record.NewFakeEventRecorder(),
		}
		r := NewReconciler(cfg)

		err := r.SetMirrorRoute(&v1alpha1.SetMirrorRoute{
			Name: "mirror-route",
			Match: []v1alpha1.RouteMatch{{
				Method: &v1alpha1.StringMatch{Exact: "GET"},
			}},
		})
		assert.Nil(t, err)

		err = r.RemoveManagedRoutes()
		assert.Nil(t, err)

		actions := client.Actions()
		assert.Len(t, actions, 0)
	})
}

func TestUpdateHashWhenGetStableVirtualNodeFails(t *testing.T) {
	canaryHash := sampleNewCanaryHash
	stableHash := sampleNewStableHash

	canaryVnode := unstructuredutil.StrToUnstructuredUnsafe(baselineCanaryVnode)
	client := testutil.NewFakeDynamicClient(canaryVnode)
	cfg := ReconcilerConfig{
		Rollout:  fakeRollout(),
		Client:   client,
		Recorder: record.NewFakeEventRecorder(),
	}
	r := NewReconciler(cfg)

	err := r.UpdateHash(canaryHash, stableHash)
	assert.Equal(t, ErrVirtualNodeMissing, err.Error())
	actions := client.Actions()
	assert.Len(t, actions, 1)
	assert.True(t, actions[0].Matches("get", "virtualnodes"))
}

func TestUpdateHashWhenGetCanaryVirtualNodeFails(t *testing.T) {
	canaryHash := sampleNewCanaryHash
	stableHash := sampleNewStableHash

	stableVnode := unstructuredutil.StrToUnstructuredUnsafe(baselineStableVnode)
	client := testutil.NewFakeDynamicClient(stableVnode)
	cfg := ReconcilerConfig{
		Rollout:  fakeRollout(),
		Client:   client,
		Recorder: record.NewFakeEventRecorder(),
	}
	r := NewReconciler(cfg)

	err := r.UpdateHash(canaryHash, stableHash)
	assert.Equal(t, ErrVirtualNodeMissing, err.Error())
	actions := client.Actions()
	assert.Len(t, actions, 3)
	assert.True(t, actions[0].Matches("get", "virtualnodes"))
	assert.True(t, actions[1].Matches("update", "virtualnodes"))
	assertUpdateHashAction(t, actions[1], stableHash)
	assert.True(t, actions[2].Matches("get", "virtualnodes"))
}

func TestUpdateHashWhenUpdateStableVirtualNodeFails(t *testing.T) {
	canaryHash := sampleNewCanaryHash
	stableHash := sampleNewStableHash

	canaryVnode := unstructuredutil.StrToUnstructuredUnsafe(baselineCanaryVnode)
	stableVnode := unstructuredutil.StrToUnstructuredUnsafe(baselineStableVnode)
	client := testutil.NewFakeDynamicClient(canaryVnode, stableVnode)
	updateError := errors.New("Failed to update virtual-node")
	client.PrependReactor("update", "virtualnodes", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		updateAction := action.(k8stesting.UpdateAction)
		uVnode, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(updateAction.GetObject())
		matchLabels, _, _ := unstructured.NestedMap(uVnode, "spec", "podSelector", "matchLabels")
		if matchLabels[v1alpha1.DefaultRolloutUniqueLabelKey].(string) == stableHash {
			return true, nil, updateError
		}
		return false, ret, nil
	})
	cfg := ReconcilerConfig{
		Rollout:  fakeRollout(),
		Client:   client,
		Recorder: record.NewFakeEventRecorder(),
	}
	r := NewReconciler(cfg)

	err := r.UpdateHash(canaryHash, stableHash)
	assert.Equal(t, updateError.Error(), err.Error())
	actions := client.Actions()
	assert.Len(t, actions, 2)
	assert.True(t, actions[0].Matches("get", "virtualnodes"))
	assert.True(t, actions[1].Matches("update", "virtualnodes"))
	assertUpdateHashAction(t, actions[1], stableHash)
}

func TestUpdateHashWhenUpdateCanaryVirtualNodeFails(t *testing.T) {
	canaryHash := sampleNewCanaryHash
	stableHash := sampleNewStableHash

	canaryVnode := unstructuredutil.StrToUnstructuredUnsafe(baselineCanaryVnode)
	stableVnode := unstructuredutil.StrToUnstructuredUnsafe(baselineStableVnode)
	client := testutil.NewFakeDynamicClient(canaryVnode, stableVnode)
	updateError := errors.New("Failed to update virtual-node")
	client.PrependReactor("update", "virtualnodes", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		updateAction := action.(k8stesting.UpdateAction)
		uVnode, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(updateAction.GetObject())
		matchLabels, _, _ := unstructured.NestedMap(uVnode, "spec", "podSelector", "matchLabels")
		if matchLabels[v1alpha1.DefaultRolloutUniqueLabelKey].(string) == canaryHash {
			return true, nil, updateError
		}
		return false, ret, nil
	})
	cfg := ReconcilerConfig{
		Rollout:  fakeRollout(),
		Client:   client,
		Recorder: record.NewFakeEventRecorder(),
	}
	r := NewReconciler(cfg)

	err := r.UpdateHash(canaryHash, stableHash)
	assert.Equal(t, updateError.Error(), err.Error())
	actions := client.Actions()
	assert.Len(t, actions, 4)
	assert.True(t, actions[0].Matches("get", "virtualnodes"))
	assert.True(t, actions[1].Matches("update", "virtualnodes"))
	assertUpdateHashAction(t, actions[1], stableHash)
	assert.True(t, actions[2].Matches("get", "virtualnodes"))
	assert.True(t, actions[3].Matches("update", "virtualnodes"))
	assertUpdateHashAction(t, actions[3], canaryHash)

}

func TestUpdateHashWithVirtualNodeMissingMatchLabels(t *testing.T) {
	canaryVnode := unstructuredutil.StrToUnstructuredUnsafe(baselineCanaryVnode)
	unstructured.SetNestedMap(canaryVnode.Object, make(map[string]interface{}), "spec", "podSelector")
	stableVnode := unstructuredutil.StrToUnstructuredUnsafe(baselineStableVnode)
	unstructured.SetNestedMap(stableVnode.Object, make(map[string]interface{}), "spec", "podSelector")
	client := testutil.NewFakeDynamicClient(canaryVnode, stableVnode)
	cfg := ReconcilerConfig{
		Rollout:  fakeRollout(),
		Client:   client,
		Recorder: record.NewFakeEventRecorder(),
	}
	r := NewReconciler(cfg)

	canaryHash := sampleNewCanaryHash
	stableHash := sampleNewStableHash
	err := r.UpdateHash(canaryHash, stableHash)
	assert.Nil(t, err)
	actions := client.Actions()
	assert.Len(t, actions, 4)
	assert.True(t, actions[0].Matches("get", "virtualnodes"))
	assert.True(t, actions[1].Matches("update", "virtualnodes"))
	assertUpdateHashAction(t, actions[1], stableHash)
	assert.True(t, actions[2].Matches("get", "virtualnodes"))
	assert.True(t, actions[3].Matches("update", "virtualnodes"))
	assertUpdateHashAction(t, actions[3], canaryHash)
}

func createVnodeWithHash(vnodeStr string, hash string) *unstructured.Unstructured {
	vnode := unstructuredutil.StrToUnstructuredUnsafe(vnodeStr)
	ml, _ := getPodSelectorMatchLabels(vnode)
	ml[v1alpha1.DefaultRolloutUniqueLabelKey] = hash
	setPodSelectorMatchLabels(vnode, ml)
	return vnode
}

func assertUpdateHashAction(t *testing.T, action k8stesting.Action, hash string) {
	updateAction := action.(k8stesting.UpdateAction)
	uVnode, err := runtime.DefaultUnstructuredConverter.ToUnstructured(updateAction.GetObject())
	assert.Nil(t, err)
	matchLabels, found, err := unstructured.NestedMap(uVnode, "spec", "podSelector", "matchLabels")
	assert.True(t, found, "Virtual-node's podSelector is missing matchLabels")
	assert.Nil(t, err)
	assert.Equal(t, matchLabels[v1alpha1.DefaultRolloutUniqueLabelKey].(string), hash)
}

func assertSetWeightAction(t *testing.T, action k8stesting.Action, desiredWeight int64, routeType string) {
	updateAction := action.(k8stesting.UpdateAction)
	uVr, err := runtime.DefaultUnstructuredConverter.ToUnstructured(updateAction.GetObject())
	assert.Nil(t, err)
	routesI, _, err := unstructured.NestedSlice(uVr, "spec", "routes")
	assert.Nil(t, err)
	for _, routeI := range routesI {
		route, _ := routeI.(map[string]interface{})
		weightedTargetsI, found, err := unstructured.NestedSlice(route, routeType, "action", "weightedTargets")
		assert.Nil(t, err)
		assert.True(t, found, "Did not find weightedTargets in route")
		assert.Len(t, weightedTargetsI, 2)
		for _, wtI := range weightedTargetsI {
			wt, _ := wtI.(map[string]interface{})
			vnodeName, _, err := unstructured.NestedString(wt, "virtualNodeRef", "name")
			assert.Nil(t, err)
			weight, err := toInt64(wt["weight"])
			assert.Nil(t, err)
			if strings.Contains(vnodeName, "canary") {
				assert.Equal(t, weight, desiredWeight)
			} else {
				assert.Equal(t, weight, 100-desiredWeight)
			}
		}
	}
}

const vsvcWithVnode = `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualService
metadata:
  name: mysvc
  namespace: myns
spec:
  awsName: mysvc.myns.svc.cluster.local
  provider:
    virtualNode:
      virtualNodeRef:
        name: mysvc-vnode`

const vsvcWithVrouter = `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualService
metadata:
  namespace: myns
  name: mysvc
spec:
  awsName: mysvc.myns.svc.cluster.local
  provider:
    virtualRouter:
      virtualRouterRef:
        name: mysvc-vrouter`

const vrouterWithHTTPRoutes = `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualRouter
metadata:
  namespace: myns
  name: mysvc-vrouter
spec:
  listeners:
    - portMapping:
        port: 8080
        protocol: http
  routes:
    - name: primary
      httpRoute:
        match:
          prefix: /
        action:
          weightedTargets:
            - virtualNodeRef:
                name: mysvc-canary-vn
              weight: 0
            - virtualNodeRef:
                name: mysvc-stable-vn
              weight: 100`

const vrouterWithGRPCRoutes = `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualRouter
metadata:
  namespace: myns
  name: mysvc-vrouter
spec:
  listeners:
    - portMapping:
        port: 8080
        protocol: http
  routes:
    - name: primary
      grpcRoute:
        match:
          methodName: GetItem
          serviceName: MySvc
        action:
          weightedTargets:
            - virtualNodeRef:
                name: mysvc-canary-vn
              weight: 0
            - virtualNodeRef:
                name: mysvc-stable-vn
              weight: 100`

const vrouterWithHTTP2Routes = `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualRouter
metadata:
  namespace: myns
  name: mysvc-vrouter
spec:
  listeners:
    - portMapping:
        port: 8080
        protocol: http
  routes:
    - name: primary
      http2Route:
        match:
          prefix: /
        action:
          weightedTargets:
            - virtualNodeRef:
                name: mysvc-canary-vn
              weight: 0
            - virtualNodeRef:
                name: mysvc-stable-vn
              weight: 100`

const vrouterWithTCPRoutes = `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualRouter
metadata:
  namespace: myns
  name: mysvc-vrouter
spec:
  listeners:
    - portMapping:
        port: 8080
        protocol: http
  routes:
    - name: primary
      tcpRoute:
        action:
          weightedTargets:
            - virtualNodeRef:
                name: mysvc-canary-vn
              weight: 0
            - virtualNodeRef:
                name: mysvc-stable-vn
              weight: 100`

const baselineCanaryVnode = `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualNode
metadata:
  namespace: myns
  name: mysvc-canary-vn
spec:
  podSelector:
    matchLabels:
      app: mysvc-pod
  listeners:
    - portMapping:
        port: 8080
        protocol: http
  serviceDiscovery:
    dns:
      hostname: mysvc.myns.svc.cluster.local`

const baselineStableVnode = `
apiVersion: appmesh.k8s.aws/v1beta2
kind: VirtualNode
metadata:
  namespace: myns
  name: mysvc-stable-vn
spec:
  podSelector:
    matchLabels:
      app: mysvc-pod
  listeners:
    - portMapping:
        port: 8080
        protocol: http
  serviceDiscovery:
    dns:
      hostname: mysvc.myns.svc.cluster.local`
