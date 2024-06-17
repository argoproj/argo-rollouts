package metrics

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	fakeRollout = `
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: guestbook-bluegreen
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: guestbook
  template:
    metadata:
      labels:
        app: guestbook
    spec:
      containers:
      - name: guestbook
        image: argoproj/rollouts-demo:blue
        ports:
        - containerPort: 80
  minReadySeconds: 30
  revisionHistoryLimit: 3
  strategy:
    blueGreen:
      activeService: active-service
      previewService: preview-service
status:
  replicas: 1
  availableReplicas: 1
`

	fakeCanaryRollout = `
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: guestbook-canary
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: guestbook
  template:
    metadata:
      labels:
        app: guestbook
    spec:
      containers:
      - name: guestbook
        image: argoproj/rollouts-demo:blue
        ports:
        - containerPort: 80
  minReadySeconds: 30
  revisionHistoryLimit: 3
  strategy:
    canary:
      trafficRouting:
        smi: {}
status:
  replicas: 1
  availableReplicas: 1
`
)

func newFakeRollout(fakeRollout string, cond *v1alpha1.RolloutCondition) *v1alpha1.Rollout {
	var rollout v1alpha1.Rollout
	err := yaml.Unmarshal([]byte(fakeRollout), &rollout)
	if err != nil {
		panic(err)
	}
	rollout.Status.Conditions = append(rollout.Status.Conditions, *cond)
	return &rollout
}

func TestCollectRollouts(t *testing.T) {
	combinations := []struct {
		fakeRollout      string
		fakeCondition    *v1alpha1.RolloutCondition
		expectedResponse string
	}{
		{
			fakeRollout,
			conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionFalse, "Progressing", ""),
			`
# HELP rollout_info Information about rollout.
# TYPE rollout_info gauge
rollout_info{name="guestbook-bluegreen",namespace="default",phase="Progressing",strategy="blueGreen",traffic_router=""} 1
# HELP rollout_info_replicas_available The number of available replicas per rollout.
# TYPE rollout_info_replicas_available gauge
rollout_info_replicas_available{name="guestbook-bluegreen",namespace="default"} 1
# HELP rollout_info_replicas_desired The number of desired replicas per rollout.
# TYPE rollout_info_replicas_desired gauge
rollout_info_replicas_desired{name="guestbook-bluegreen",namespace="default"} 1
# HELP rollout_info_replicas_unavailable The number of unavailable replicas per rollout.
# TYPE rollout_info_replicas_unavailable gauge
rollout_info_replicas_unavailable{name="guestbook-bluegreen",namespace="default"} 0
# HELP rollout_info_replicas_unavailable The number of unavailable replicas per rollout.
# TYPE rollout_info_replicas_unavailable gauge
rollout_info_replicas_unavailable{name="guestbook-bluegreen",namespace="default"} 0
# HELP rollout_info_replicas_updated The number of updated replicas per rollout.
# TYPE rollout_info_replicas_updated gauge
rollout_info_replicas_updated{name="guestbook-bluegreen",namespace="default"} 0`,
		},

		{
			fakeRollout,
			conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionFalse, conditions.FailedRSCreateReason, "test"),
			`
# HELP rollout_info Information about rollout.
# TYPE rollout_info gauge
rollout_info{name="guestbook-bluegreen",namespace="default",phase="Error",strategy="blueGreen",traffic_router=""} 1
# HELP rollout_info_replicas_available The number of available replicas per rollout.
# TYPE rollout_info_replicas_available gauge
rollout_info_replicas_available{name="guestbook-bluegreen",namespace="default"} 1
# HELP rollout_info_replicas_desired The number of desired replicas per rollout.
# TYPE rollout_info_replicas_desired gauge
rollout_info_replicas_desired{name="guestbook-bluegreen",namespace="default"} 1
# HELP rollout_info_replicas_unavailable The number of unavailable replicas per rollout.
# TYPE rollout_info_replicas_unavailable gauge
rollout_info_replicas_unavailable{name="guestbook-bluegreen",namespace="default"} 0
# HELP rollout_info_replicas_unavailable The number of unavailable replicas per rollout.
# TYPE rollout_info_replicas_unavailable gauge
rollout_info_replicas_unavailable{name="guestbook-bluegreen",namespace="default"} 0
# HELP rollout_info_replicas_updated The number of updated replicas per rollout.
# TYPE rollout_info_replicas_updated gauge
rollout_info_replicas_updated{name="guestbook-bluegreen",namespace="default"} 0`,
		},
		{
			fakeCanaryRollout,
			conditions.NewRolloutCondition(v1alpha1.RolloutProgressing, corev1.ConditionFalse, "Progressing", "test"),
			`
# HELP rollout_info Information about rollout.
# TYPE rollout_info gauge
rollout_info{name="guestbook-canary",namespace="default",phase="Progressing",strategy="canary",traffic_router="SMI"} 1
# HELP rollout_info_replicas_available The number of available replicas per rollout.
# TYPE rollout_info_replicas_available gauge
rollout_info_replicas_available{name="guestbook-canary",namespace="default"} 1
# HELP rollout_info_replicas_desired The number of desired replicas per rollout.
# TYPE rollout_info_replicas_desired gauge
rollout_info_replicas_desired{name="guestbook-canary",namespace="default"} 1
# HELP rollout_info_replicas_unavailable The number of unavailable replicas per rollout.
# TYPE rollout_info_replicas_unavailable gauge
rollout_info_replicas_unavailable{name="guestbook-canary",namespace="default"} 0
# HELP rollout_info_replicas_unavailable The number of unavailable replicas per rollout.
# TYPE rollout_info_replicas_unavailable gauge
rollout_info_replicas_unavailable{name="guestbook-canary",namespace="default"} 0
# HELP rollout_info_replicas_updated The number of updated replicas per rollout.
# TYPE rollout_info_replicas_updated gauge
rollout_info_replicas_updated{name="guestbook-canary",namespace="default"} 0`,
		},
	}

	for _, combination := range combinations {
		testRolloutDescribe(t, combination.fakeRollout, combination.fakeCondition, combination.expectedResponse)
	}
}

func testRolloutDescribe(t *testing.T, fakeRollout string, cond *v1alpha1.RolloutCondition, expectedResponse string) {
	registry := prometheus.NewRegistry()
	config := newFakeServerConfig(newFakeRollout(fakeRollout, cond))
	registry.MustRegister(NewRolloutCollector(config.RolloutLister))
	mux := http.NewServeMux()
	mux.Handle(MetricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	testHttpResponse(t, mux, expectedResponse, assert.Contains)
}

func TestIncRolloutReconcile(t *testing.T) {
	expectedResponse := `
# HELP rollout_reconcile Rollout reconciliation performance.
# TYPE rollout_reconcile histogram
rollout_reconcile_bucket{name="ro-test",namespace="ro-namespace",le="0.01"} 1
rollout_reconcile_bucket{name="ro-test",namespace="ro-namespace",le="0.15"} 1
rollout_reconcile_bucket{name="ro-test",namespace="ro-namespace",le="0.25"} 1
rollout_reconcile_bucket{name="ro-test",namespace="ro-namespace",le="0.5"} 1
rollout_reconcile_bucket{name="ro-test",namespace="ro-namespace",le="1"} 1
rollout_reconcile_bucket{name="ro-test",namespace="ro-namespace",le="+Inf"} 1
rollout_reconcile_sum{name="ro-test",namespace="ro-namespace"} 0.001
rollout_reconcile_count{name="ro-test",namespace="ro-namespace"} 1
`

	metricsServ := NewMetricsServer(newFakeServerConfig())
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ro-test",
			Namespace: "ro-namespace",
		},
	}
	metricsServ.IncRolloutReconcile(ro, time.Millisecond)
	testHttpResponse(t, metricsServ.Handler, expectedResponse, assert.Contains)
}

func TestGetStrategyAndTrafficRouter(t *testing.T) {
	var tests = []struct {
		strategy              v1alpha1.RolloutStrategy
		expectedStrategy      string
		expectedTrafficRouter string
	}{
		{
			strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{},
			},
			expectedStrategy:      "blueGreen",
			expectedTrafficRouter: "",
		},
		{
			strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
			},
			expectedStrategy:      "canary",
			expectedTrafficRouter: "",
		},
		{
			strategy:              v1alpha1.RolloutStrategy{},
			expectedStrategy:      "none",
			expectedTrafficRouter: "",
		},
		{
			strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						SMI: &v1alpha1.SMITrafficRouting{},
					},
				},
			},
			expectedStrategy:      "canary",
			expectedTrafficRouter: "SMI",
		},
		{
			strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Istio: &v1alpha1.IstioTrafficRouting{},
					},
				},
			},
			expectedStrategy:      "canary",
			expectedTrafficRouter: "Istio",
		},
		{
			strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						ALB: &v1alpha1.ALBTrafficRouting{},
					},
				},
			},
			expectedStrategy:      "canary",
			expectedTrafficRouter: "ALB",
		},
		{
			strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Ambassador: &v1alpha1.AmbassadorTrafficRouting{},
					},
				},
			},
			expectedStrategy:      "canary",
			expectedTrafficRouter: "Ambassador",
		},
		{
			strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Nginx: &v1alpha1.NginxTrafficRouting{},
					},
				},
			},
			expectedStrategy:      "canary",
			expectedTrafficRouter: "Nginx",
		},
		{
			strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						AppMesh: &v1alpha1.AppMeshTrafficRouting{},
					},
				},
			},
			expectedStrategy:      "canary",
			expectedTrafficRouter: "AppMesh",
		},
	}

	for _, test := range tests {
		ro := &v1alpha1.Rollout{
			Spec: v1alpha1.RolloutSpec{
				Strategy: test.strategy,
			},
		}
		strategy, trafficRouter := getStrategyAndTrafficRouter(ro)
		assert.Equal(t, test.expectedStrategy, strategy)
		assert.Equal(t, test.expectedTrafficRouter, trafficRouter)
	}
}

func TestIncRolloutEvents(t *testing.T) {
	expectedResponse := `
# HELP rollout_events_total Count of rollout events
# TYPE rollout_events_total counter
rollout_events_total{name="ro-test-1",namespace="ro-namespace",reason="BarEvent",type="Normal"} 1
rollout_events_total{name="ro-test-1",namespace="ro-namespace",reason="FooEvent",type="Normal"} 1
rollout_events_total{name="ro-test-2",namespace="ro-namespace",reason="BazEvent",type="Warning"} 2
`

	metricsServ := NewMetricsServer(newFakeServerConfig())
	MetricRolloutEventsTotal.WithLabelValues("ro-namespace", "ro-test-1", corev1.EventTypeNormal, "FooEvent").Inc()
	MetricRolloutEventsTotal.WithLabelValues("ro-namespace", "ro-test-1", corev1.EventTypeNormal, "BarEvent").Inc()
	MetricRolloutEventsTotal.WithLabelValues("ro-namespace", "ro-test-2", corev1.EventTypeWarning, "BazEvent").Inc()
	MetricRolloutEventsTotal.WithLabelValues("ro-namespace", "ro-test-2", corev1.EventTypeWarning, "BazEvent").Inc()
	testHttpResponse(t, metricsServ.Handler, expectedResponse, assert.Contains)
}
