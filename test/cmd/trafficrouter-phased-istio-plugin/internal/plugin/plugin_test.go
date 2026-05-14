package plugin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	testutil "github.com/argoproj/argo-rollouts/test/util"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
)

// --- helpers ---

func buildRoute(name string, canaryWeight int64) map[string]interface{} {
	return map[string]interface{}{
		"name": name,
		"route": []interface{}{
			map[string]interface{}{
				"destination": map[string]interface{}{"subset": "stable"},
				"weight":      int64(100 - canaryWeight),
			},
			map[string]interface{}{
				"destination": map[string]interface{}{"subset": "canary"},
				"weight":      canaryWeight,
			},
		},
	}
}

// buildVS creates a VirtualService with latest-route and stable-route at the given canary weights.
func buildVS(latestCanary, stableCanary int64) *unstructured.Unstructured {
	gvr := istioutil.GetIstioVirtualServiceGVR()
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": gvr.Group + "/" + gvr.Version,
			"kind":       "VirtualService",
			"metadata": map[string]interface{}{
				"name":      "my-service",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"http": []interface{}{
					buildRoute("latest-route", latestCanary),
					buildRoute("stable-route", stableCanary),
				},
			},
		},
	}
}

func buildRollout(phases ...Phase) *v1alpha1.Rollout {
	if len(phases) == 0 {
		phases = []Phase{{Route: "latest-route"}, {Route: "stable-route"}}
	}
	cfg := PluginConfig{
		VirtualService: VSRef{Name: "my-service", Namespace: "default"},
		DestinationRule: DRRef{
			Name:             "my-dr",
			Namespace:        "default",
			CanarySubsetName: "canary",
			StableSubsetName: "stable",
		},
		Phases: phases,
	}
	cfgBytes, _ := json.Marshal(cfg)
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{Name: "my-rollout", Namespace: "default"},
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Plugins: map[string]json.RawMessage{
							PluginName: cfgBytes,
						},
					},
				},
			},
		},
	}
}

func newTestPlugin(vs *unstructured.Unstructured) *RpcPlugin {
	client := testutil.NewFakeDynamicClient(vs)
	return &RpcPlugin{
		LogCtx:        logrus.NewEntry(logrus.New()),
		dynamicClient: client,
	}
}

// readCanaryWeight reads the current canary weight for routeName from the live VS in the fake client.
func readCanaryWeight(t *testing.T, p *RpcPlugin, routeName string) int64 {
	t.Helper()
	vs, err := p.dynamicClient.Resource(istioutil.GetIstioVirtualServiceGVR()).Namespace("default").Get(context.TODO(), "my-service", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get VS: %v", err)
	}
	httpRoutes, _, _ := unstructured.NestedSlice(vs.Object, "spec", "http")
	w := routeCanaryWeight(httpRoutes, routeName, "canary")
	if w == -1 {
		t.Fatalf("route %q not found in VS", routeName)
	}
	return w
}

// --- routeCanaryWeight unit tests ---

func TestRouteCanaryWeight(t *testing.T) {
	routes := []interface{}{
		buildRoute("latest-route", 30),
		buildRoute("stable-route", 0),
	}

	cases := []struct {
		name         string
		routeName    string
		canarySubset string
		want         int64
	}{
		{
			name:         "route found returns canary weight",
			routeName:    "latest-route",
			canarySubset: "canary",
			want:         30,
		},
		{
			name:         "second route at zero",
			routeName:    "stable-route",
			canarySubset: "canary",
			want:         0,
		},
		{
			name:         "route not found returns -1",
			routeName:    "nonexistent",
			canarySubset: "canary",
			want:         -1,
		},
		{
			name:         "wrong subset name returns -1",
			routeName:    "latest-route",
			canarySubset: "wrong-subset",
			want:         -1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := routeCanaryWeight(routes, tc.routeName, tc.canarySubset)
			if got != tc.want {
				t.Errorf("routeCanaryWeight(%q, %q) = %d, want %d", tc.routeName, tc.canarySubset, got, tc.want)
			}
		})
	}
}

func TestRouteCanaryWeightFloat64(t *testing.T) {
	// JSON-decoded numbers arrive as float64; the function must handle this.
	routes := []interface{}{
		map[string]interface{}{
			"name": "my-route",
			"route": []interface{}{
				map[string]interface{}{
					"destination": map[string]interface{}{"subset": "canary"},
					"weight":      float64(42),
				},
			},
		},
	}
	got := routeCanaryWeight(routes, "my-route", "canary")
	if got != 42 {
		t.Errorf("expected 42, got %d", got)
	}
}

// --- applyRouteWeights unit tests ---

func TestApplyRouteWeights(t *testing.T) {
	cases := []struct {
		name          string
		routeName     string
		desiredWeight int32
		wantCanary    int64
		wantStable    int64
		wantErr       bool
	}{
		{
			name:          "sets canary and stable",
			routeName:     "latest-route",
			desiredWeight: 20,
			wantCanary:    20,
			wantStable:    80,
		},
		{
			name:          "sets to 100",
			routeName:     "latest-route",
			desiredWeight: 100,
			wantCanary:    100,
			wantStable:    0,
		},
		{
			name:          "resets to 0",
			routeName:     "latest-route",
			desiredWeight: 0,
			wantCanary:    0,
			wantStable:    100,
		},
		{
			name:          "unknown route returns error",
			routeName:     "nonexistent",
			desiredWeight: 50,
			wantErr:       true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			routes := []interface{}{
				buildRoute("latest-route", 10),
				buildRoute("stable-route", 0),
			}

			err := applyRouteWeights(routes, tc.routeName, "canary", "stable", tc.desiredWeight)

			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotCanary := routeCanaryWeight(routes, tc.routeName, "canary")
			if gotCanary != tc.wantCanary {
				t.Errorf("canary weight = %d, want %d", gotCanary, tc.wantCanary)
			}
			// verify stable = 100 - canary
			if gotCanary+tc.wantStable != 100 {
				t.Errorf("canary(%d) + stable(%d) != 100", gotCanary, tc.wantStable)
			}
		})
	}
}

func TestApplyRouteWeightsDoesNotTouchOtherRoutes(t *testing.T) {
	routes := []interface{}{
		buildRoute("latest-route", 0),
		buildRoute("stable-route", 0),
	}

	if err := applyRouteWeights(routes, "latest-route", "canary", "stable", 50); err != nil {
		t.Fatal(err)
	}

	if w := routeCanaryWeight(routes, "stable-route", "canary"); w != 0 {
		t.Errorf("stable-route canary weight should be unchanged at 0, got %d", w)
	}
}

// --- SetWeight phase detection integration tests ---

func TestSetWeightPhaseDetection(t *testing.T) {
	cases := []struct {
		name                string
		latestCanaryBefore  int64
		stableCanaryBefore  int64
		desiredWeight       int32
		wantLatestCanary    int64
		wantStableCanary    int64
	}{
		{
			name:               "phase 1 active: both at 0, advances latest-route",
			latestCanaryBefore: 0,
			stableCanaryBefore: 0,
			desiredWeight:      10,
			wantLatestCanary:   10,
			wantStableCanary:   0,
		},
		{
			name:               "phase 1 active: continues advancing latest-route",
			latestCanaryBefore: 50,
			stableCanaryBefore: 0,
			desiredWeight:      80,
			wantLatestCanary:   80,
			wantStableCanary:   0,
		},
		{
			name:               "phase 1 active: completes latest-route",
			latestCanaryBefore: 80,
			stableCanaryBefore: 0,
			desiredWeight:      100,
			wantLatestCanary:   100,
			wantStableCanary:   0,
		},
		{
			name:               "phase 2 active: latest-route complete, advances stable-route",
			latestCanaryBefore: 100,
			stableCanaryBefore: 0,
			desiredWeight:      5,
			wantLatestCanary:   100,
			wantStableCanary:   5,
		},
		{
			name:               "phase 2 active: continues advancing stable-route",
			latestCanaryBefore: 100,
			stableCanaryBefore: 25,
			desiredWeight:      75,
			wantLatestCanary:   100,
			wantStableCanary:   75,
		},
		{
			name:               "all phases complete: setWeight is a no-op",
			latestCanaryBefore: 100,
			stableCanaryBefore: 100,
			desiredWeight:      50,
			wantLatestCanary:   100,
			wantStableCanary:   100,
		},
		{
			name:               "desiredWeight 0 resets all routes",
			latestCanaryBefore: 100,
			stableCanaryBefore: 25,
			desiredWeight:      0,
			wantLatestCanary:   0,
			wantStableCanary:   0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs := buildVS(tc.latestCanaryBefore, tc.stableCanaryBefore)
			p := newTestPlugin(vs)
			ro := buildRollout()

			rpcErr := p.SetWeight(ro, tc.desiredWeight, nil)
			if rpcErr.HasError() {
				t.Fatalf("SetWeight returned error: %s", rpcErr.Error())
			}

			gotLatest := readCanaryWeight(t, p, "latest-route")
			gotStable := readCanaryWeight(t, p, "stable-route")

			if gotLatest != tc.wantLatestCanary {
				t.Errorf("latest-route canary = %d, want %d", gotLatest, tc.wantLatestCanary)
			}
			if gotStable != tc.wantStableCanary {
				t.Errorf("stable-route canary = %d, want %d", gotStable, tc.wantStableCanary)
			}
		})
	}
}

// --- phaseRouteWeight unit tests ---

func TestPhaseRouteWeight(t *testing.T) {
	cases := []struct {
		name          string
		desiredWeight int32
		start         int32
		end           int32
		want          int32
	}{
		{name: "below range returns 0", desiredWeight: 0, start: 0, end: 50, want: 0},
		{name: "at start returns 0", desiredWeight: 0, start: 0, end: 50, want: 0},
		{name: "midpoint of first half", desiredWeight: 25, start: 0, end: 50, want: 50},
		{name: "at end of first half returns 100", desiredWeight: 50, start: 0, end: 50, want: 100},
		{name: "above end returns 100", desiredWeight: 75, start: 0, end: 50, want: 100},
		{name: "at start of second half returns 0", desiredWeight: 50, start: 50, end: 100, want: 0},
		{name: "midpoint of second half", desiredWeight: 75, start: 50, end: 100, want: 50},
		{name: "at end of second half returns 100", desiredWeight: 100, start: 50, end: 100, want: 100},
		{name: "unequal split 30/70: phase1 at boundary", desiredWeight: 30, start: 0, end: 30, want: 100},
		{name: "unequal split 30/70: phase2 midpoint", desiredWeight: 65, start: 30, end: 100, want: 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := phaseRouteWeight(tc.desiredWeight, tc.start, tc.end)
			if got != tc.want {
				t.Errorf("phaseRouteWeight(%d, %d, %d) = %d, want %d", tc.desiredWeight, tc.start, tc.end, got, tc.want)
			}
		})
	}
}

// --- SetWeight proportional mode integration tests ---

func TestSetWeightProportional(t *testing.T) {
	cases := []struct {
		name             string
		desiredWeight    int32
		wantLatestCanary int64
		wantStableCanary int64
	}{
		{
			name:             "desiredWeight 0: both routes at 0",
			desiredWeight:    0,
			wantLatestCanary: 0,
			wantStableCanary: 0,
		},
		{
			name:             "desiredWeight 25: phase1 at 50%, phase2 at 0%",
			desiredWeight:    25,
			wantLatestCanary: 50,
			wantStableCanary: 0,
		},
		{
			name:             "desiredWeight 50: phase1 complete, phase2 at 0%",
			desiredWeight:    50,
			wantLatestCanary: 100,
			wantStableCanary: 0,
		},
		{
			name:             "desiredWeight 75: phase1 complete, phase2 at 50%",
			desiredWeight:    75,
			wantLatestCanary: 100,
			wantStableCanary: 50,
		},
		{
			name:             "desiredWeight 100: both phases complete",
			desiredWeight:    100,
			wantLatestCanary: 100,
			wantStableCanary: 100,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs := buildVS(0, 0)
			p := newTestPlugin(vs)
			ro := buildRollout(Phase{Route: "latest-route", Weight: 50}, Phase{Route: "stable-route", Weight: 50})

			rpcErr := p.SetWeight(ro, tc.desiredWeight, nil)
			if rpcErr.HasError() {
				t.Fatalf("SetWeight returned error: %s", rpcErr.Error())
			}

			gotLatest := readCanaryWeight(t, p, "latest-route")
			gotStable := readCanaryWeight(t, p, "stable-route")

			if gotLatest != tc.wantLatestCanary {
				t.Errorf("latest-route canary = %d, want %d", gotLatest, tc.wantLatestCanary)
			}
			if gotStable != tc.wantStableCanary {
				t.Errorf("stable-route canary = %d, want %d", gotStable, tc.wantStableCanary)
			}
		})
	}
}

// --- VerifyWeight proportional mode tests ---

func TestVerifyWeightProportional(t *testing.T) {
	cases := []struct {
		name              string
		latestCanaryNow   int64
		stableCanaryNow   int64
		desiredWeight     int32
		wantVerified      bool
	}{
		{
			name:            "both at 0, desiredWeight 0: verified",
			latestCanaryNow: 0,
			stableCanaryNow: 0,
			desiredWeight:   0,
			wantVerified:    true,
		},
		{
			name:            "phase1 at 50, phase2 at 0, desiredWeight 25: verified",
			latestCanaryNow: 50,
			stableCanaryNow: 0,
			desiredWeight:   25,
			wantVerified:    true,
		},
		{
			name:            "phase1 at 50, phase2 at 0, desiredWeight 50: not verified (phase1 should be 100)",
			latestCanaryNow: 50,
			stableCanaryNow: 0,
			desiredWeight:   50,
			wantVerified:    false,
		},
		{
			name:            "phase1 at 100, phase2 at 0, desiredWeight 50: verified",
			latestCanaryNow: 100,
			stableCanaryNow: 0,
			desiredWeight:   50,
			wantVerified:    true,
		},
		{
			name:            "phase1 at 100, phase2 at 50, desiredWeight 75: verified",
			latestCanaryNow: 100,
			stableCanaryNow: 50,
			desiredWeight:   75,
			wantVerified:    true,
		},
		{
			name:            "both at 100, desiredWeight 100: verified",
			latestCanaryNow: 100,
			stableCanaryNow: 100,
			desiredWeight:   100,
			wantVerified:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vs := buildVS(tc.latestCanaryNow, tc.stableCanaryNow)
			p := newTestPlugin(vs)
			ro := buildRollout(Phase{Route: "latest-route", Weight: 50}, Phase{Route: "stable-route", Weight: 50})

			verified, rpcErr := p.VerifyWeight(ro, tc.desiredWeight, nil)
			if rpcErr.HasError() {
				t.Fatalf("VerifyWeight returned error: %s", rpcErr.Error())
			}

			gotVerified := verified == pluginTypes.Verified
			if gotVerified != tc.wantVerified {
				t.Errorf("VerifyWeight verified=%v, want %v", gotVerified, tc.wantVerified)
			}
		})
	}
}
