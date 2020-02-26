package metrics

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ghodss/yaml"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informer "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	lister "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
)

// assertMetricsPrinted asserts every line in the expected lines appears in the body
func assertMetricsPrinted(t *testing.T, expectedLines, body string) {
	for _, line := range strings.Split(expectedLines, "\n") {
		assert.Contains(t, body, line)
	}
}

func newFakeRollout(fakeRollout string) *v1alpha1.Rollout {
	var rollout v1alpha1.Rollout
	err := yaml.Unmarshal([]byte(fakeRollout), &rollout)
	if err != nil {
		panic(err)
	}
	return &rollout
}

func newFakeLister(fakeRollout ...string) (context.CancelFunc, lister.RolloutLister) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var fakeRollouts []runtime.Object
	for _, name := range fakeRollout {
		if name == "" {
			continue
		}
		fakeRollouts = append(fakeRollouts, newFakeRollout(name))
	}
	appClientset := clientset.NewSimpleClientset(fakeRollouts...)
	factory := informer.NewSharedInformerFactoryWithOptions(appClientset, 0)
	rolloutInformer := factory.Argoproj().V1alpha1().Rollouts().Informer()
	go rolloutInformer.Run(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), rolloutInformer.HasSynced) {
		log.Fatal("Timed out waiting for caches to sync")
	}
	return cancel, factory.Argoproj().V1alpha1().Rollouts().Lister()
}

func testRolloutDescribe(t *testing.T, fakeRollout string, expectedResponse string) {
	cancel, rolloutLister := newFakeLister(fakeRollout)
	defer cancel()
	metricsServ := NewMetricsServer("localhost:8080", rolloutLister, &K8sRequestsCountProvider{})
	req, err := http.NewRequest("GET", "/metrics", nil)
	assert.NoError(t, err)
	rr := httptest.NewRecorder()
	metricsServ.Handler.ServeHTTP(rr, req)
	assert.Equal(t, rr.Code, http.StatusOK)
	body := rr.Body.String()
	log.Println(body)
	assertMetricsPrinted(t, expectedResponse, body)
}

type testCombination struct {
	rollout          string
	expectedResponse string
}

const (
	noRollouts  = ""
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
`
)
const expectedResponse = `# HELP rollout_created_time Creation time in unix timestamp for an rollout.
# TYPE rollout_created_time gauge
rollout_created_time{name="guestbook-bluegreen",namespace="default",strategy="blueGreen"} -6.21355968e+10
`

func TestMetrics(t *testing.T) {
	combinations := []testCombination{
		{
			rollout:          fakeRollout,
			expectedResponse: expectedResponse,
		},
	}

	for _, combination := range combinations {
		testRolloutDescribe(t, combination.rollout, combination.expectedResponse)
	}
}
