package metrics

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
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
        # The image below can be flip from 0.1 to 0.2
        image: gcr.io/heptio-images/ks-guestbook-demo:0.1
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
)
const expectedResponse = `
# HELP rollout_info Information about rollout.
# TYPE rollout_info gauge
rollout_info{name="guestbook-bluegreen",namespace="default",phase="Progressing",strategy="blueGreen"} 1
# HELP rollout_info_replicas_available The number of available replicas per rollout.
# TYPE rollout_info_replicas_available gauge
rollout_info_replicas_available{name="guestbook-bluegreen",namespace="default"} 1
# HELP rollout_info_replicas_desired The number of desired replicas per rollout.
# TYPE rollout_info_replicas_desired gauge
rollout_info_replicas_desired{name="guestbook-bluegreen",namespace="default"} 1
# HELP rollout_info_replicas_unavailable The number of unavailable replicas per rollout.
# TYPE rollout_info_replicas_unavailable gauge
rollout_info_replicas_unavailable{name="guestbook-bluegreen",namespace="default"} 0`

func newFakeRollout(fakeRollout string) *v1alpha1.Rollout {
	var rollout v1alpha1.Rollout
	err := yaml.Unmarshal([]byte(fakeRollout), &rollout)
	if err != nil {
		panic(err)
	}
	return &rollout
}

func TestCollectRollouts(t *testing.T) {
	combinations := []testCombination{
		{
			resource:         fakeRollout,
			expectedResponse: expectedResponse,
		},
	}

	for _, combination := range combinations {
		testRolloutDescribe(t, combination.resource, combination.expectedResponse)
	}
}

func TestCollectRolloutsListFails(t *testing.T) {
	buf := bytes.NewBufferString("")
	logrus.SetOutput(buf)
	registry := prometheus.NewRegistry()
	fakeLister := fakeRolloutLister{
		error: fmt.Errorf("Error with lister"),
	}
	registry.MustRegister(NewRolloutCollector(fakeLister))
	mux := http.NewServeMux()
	mux.Handle(MetricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	testHttpResponse(t, mux, "")
	assert.Contains(t, buf.String(), "Error with lister")
}

func testRolloutDescribe(t *testing.T, fakeRollout string, expectedResponse string) {
	registry := prometheus.NewRegistry()
	fakeLister := fakeRolloutLister{
		rollouts: []*v1alpha1.Rollout{newFakeRollout(fakeRollout)},
	}
	registry.MustRegister(NewRolloutCollector(fakeLister))
	mux := http.NewServeMux()
	mux.Handle(MetricsPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	testHttpResponse(t, mux, expectedResponse)
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

	provider := &K8sRequestsCountProvider{}

	metricsServ := NewMetricsServer(ServerConfig{
		RolloutLister:      fakeRolloutLister{},
		ExperimentLister:   fakeExperimentLister{},
		AnalysisRunLister:  fakeAnalysisRunLister{},
		K8SRequestProvider: provider,
	})
	ro := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ro-test",
			Namespace: "ro-namespace",
		},
	}
	metricsServ.IncRolloutReconcile(ro, time.Millisecond)
	testHttpResponse(t, metricsServ.Handler, expectedResponse)
}

func TestGetStrategyType(t *testing.T) {
	bgRollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				BlueGreen: &v1alpha1.BlueGreenStrategy{},
			},
		},
	}
	assert.Equal(t, "blueGreen", getStrategyType(bgRollout))

	canaryRollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{},
			},
		},
	}
	assert.Equal(t, "canary", getStrategyType(canaryRollout))

	noStrategyRollout := &v1alpha1.Rollout{
		Spec: v1alpha1.RolloutSpec{
			Strategy: v1alpha1.RolloutStrategy{},
		},
	}
	assert.Equal(t, "none", getStrategyType(noStrategyRollout))
}
