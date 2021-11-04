package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	notificationapi "github.com/argoproj/notifications-engine/pkg/api"
	notificationcontroller "github.com/argoproj/notifications-engine/pkg/controller"
	smifake "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned/fake"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	discoveryfake "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/analysis"
	"github.com/argoproj/argo-rollouts/controller/metrics"
	experimentsController "github.com/argoproj/argo-rollouts/experiments"
	"github.com/argoproj/argo-rollouts/ingress"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	rolloutController "github.com/argoproj/argo-rollouts/rollout"
	"github.com/argoproj/argo-rollouts/service"
	ingressutil "github.com/argoproj/argo-rollouts/utils/ingress"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	"github.com/argoproj/argo-rollouts/utils/queue"
	"github.com/argoproj/argo-rollouts/utils/record"
)

var (
	alwaysReady        = func() bool { return true }
	noResyncPeriodFunc = func() time.Duration { return 0 }
)

type fixture struct {
	t *testing.T

	client     *fake.Clientset
	kubeclient *k8sfake.Clientset
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{}
	f.t = t

	f.client = fake.NewSimpleClientset()
	f.kubeclient = k8sfake.NewSimpleClientset()
	return f
}

func (f *fixture) newManager(t *testing.T) *Manager {
	rolloutWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Rollouts")
	serviceWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Services")
	ingressWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Ingresses")
	experimentWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "Experiments")
	analysisRunWorkqueue := workqueue.NewNamedRateLimitingQueue(queue.DefaultArgoRolloutsRateLimiter(), "AnalysisRuns")

	cm := &Manager{

		kubeClientSet: f.kubeclient,

		ingressWorkqueue:     ingressWorkqueue,
		serviceWorkqueue:     serviceWorkqueue,
		rolloutWorkqueue:     rolloutWorkqueue,
		experimentWorkqueue:  experimentWorkqueue,
		analysisRunWorkqueue: analysisRunWorkqueue,

		rolloutSynced:                 alwaysReady,
		serviceSynced:                 alwaysReady,
		ingressSynced:                 alwaysReady,
		jobSynced:                     alwaysReady,
		experimentSynced:              alwaysReady,
		analysisRunSynced:             alwaysReady,
		analysisTemplateSynced:        alwaysReady,
		replicasSetSynced:             alwaysReady,
		configMapSynced:               alwaysReady,
		secretSynced:                  alwaysReady,
		clusterAnalysisTemplateSynced: alwaysReady,

		healthzServer: NewHealthzServer(fmt.Sprintf(listenAddr, 8080)),
	}

	metricsAddr := fmt.Sprintf(listenAddr, 8090)
	cm.metricsServer = metrics.NewMetricsServer(metrics.ServerConfig{
		Addr: metricsAddr,
	}, false)

	i := informers.NewSharedInformerFactory(f.client, noResyncPeriodFunc())
	k8sI := kubeinformers.NewSharedInformerFactory(f.kubeclient, noResyncPeriodFunc())
	tgbGVR := schema.GroupVersionResource{
		Group:    "elbv2.k8s.aws",
		Version:  "v1beta1",
		Resource: "targetgroupbindings",
	}
	vsvcGVR := istioutil.GetIstioVirtualServiceGVR()
	scheme := runtime.NewScheme()
	listMapping := map[schema.GroupVersionResource]string{
		tgbGVR:  "TargetGroupBindingList",
		vsvcGVR: vsvcGVR.Resource + "List",
	}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listMapping)
	dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)
	istioVirtualServiceInformer := dynamicInformerFactory.ForResource(istioutil.GetIstioVirtualServiceGVR()).Informer()
	istioDestinationRuleInformer := dynamicInformerFactory.ForResource(istioutil.GetIstioDestinationRuleGVR()).Informer()

	mode, err := ingressutil.DetermineIngressMode("extensions/v1beta1", &discoveryfake.FakeDiscovery{})
	assert.NoError(t, err)
	ingressWrapper, err := ingressutil.NewIngressWrapper(mode, f.kubeclient, k8sI)
	assert.NoError(t, err)

	cm.rolloutController = rolloutController.NewController(rolloutController.ControllerConfig{
		Namespace:                       metav1.NamespaceAll,
		KubeClientSet:                   f.kubeclient,
		ArgoProjClientset:               f.client,
		DynamicClientSet:                dynamicClient,
		ExperimentInformer:              i.Argoproj().V1alpha1().Experiments(),
		AnalysisRunInformer:             i.Argoproj().V1alpha1().AnalysisRuns(),
		AnalysisTemplateInformer:        i.Argoproj().V1alpha1().AnalysisTemplates(),
		ClusterAnalysisTemplateInformer: i.Argoproj().V1alpha1().ClusterAnalysisTemplates(),
		ReplicaSetInformer:              k8sI.Apps().V1().ReplicaSets(),
		ServicesInformer:                k8sI.Core().V1().Services(),
		IngressWrapper:                  ingressWrapper,
		RolloutsInformer:                i.Argoproj().V1alpha1().Rollouts(),
		IstioPrimaryDynamicClient:       dynamicClient,
		IstioVirtualServiceInformer:     istioVirtualServiceInformer,
		IstioDestinationRuleInformer:    istioDestinationRuleInformer,
		ResyncPeriod:                    noResyncPeriodFunc(),
		RolloutWorkQueue:                rolloutWorkqueue,
		ServiceWorkQueue:                serviceWorkqueue,
		IngressWorkQueue:                ingressWorkqueue,
		MetricsServer:                   cm.metricsServer,
		Recorder:                        record.NewFakeEventRecorder(),
	})

	cm.analysisController = analysis.NewController(analysis.ControllerConfig{
		KubeClientSet:        f.kubeclient,
		ArgoProjClientset:    f.client,
		AnalysisRunInformer:  i.Argoproj().V1alpha1().AnalysisRuns(),
		JobInformer:          k8sI.Batch().V1().Jobs(),
		ResyncPeriod:         noResyncPeriodFunc(),
		AnalysisRunWorkQueue: analysisRunWorkqueue,
		MetricsServer:        cm.metricsServer,
		Recorder:             record.NewFakeEventRecorder(),
	})

	cm.ingressController = ingress.NewController(ingress.ControllerConfig{
		Client:           f.kubeclient,
		IngressWrap:      ingressWrapper,
		IngressWorkQueue: ingressWorkqueue,

		RolloutsInformer: i.Argoproj().V1alpha1().Rollouts(),
		RolloutWorkQueue: rolloutWorkqueue,
		ALBClasses:       []string{"alb"},
		NGINXClasses:     []string{"nginx"},
		MetricsServer:    cm.metricsServer,
	})

	cm.serviceController = service.NewController(service.ControllerConfig{
		Kubeclientset:     f.kubeclient,
		Argoprojclientset: f.client,
		RolloutsInformer:  i.Argoproj().V1alpha1().Rollouts(),
		ServicesInformer:  k8sI.Core().V1().Services(),
		RolloutWorkqueue:  rolloutWorkqueue,
		ServiceWorkqueue:  serviceWorkqueue,
		ResyncPeriod:      0,
		MetricsServer:     cm.metricsServer,
	})

	cm.experimentController = experimentsController.NewController(experimentsController.ControllerConfig{
		KubeClientSet:                   f.kubeclient,
		ArgoProjClientset:               f.client,
		ReplicaSetInformer:              k8sI.Apps().V1().ReplicaSets(),
		ExperimentsInformer:             i.Argoproj().V1alpha1().Experiments(),
		AnalysisRunInformer:             i.Argoproj().V1alpha1().AnalysisRuns(),
		AnalysisTemplateInformer:        i.Argoproj().V1alpha1().AnalysisTemplates(),
		ClusterAnalysisTemplateInformer: i.Argoproj().V1alpha1().ClusterAnalysisTemplates(),
		ServiceInformer:                 k8sI.Core().V1().Services(),
		ResyncPeriod:                    noResyncPeriodFunc(),
		RolloutWorkQueue:                rolloutWorkqueue,
		ExperimentWorkQueue:             experimentWorkqueue,
		MetricsServer:                   cm.metricsServer,
		Recorder:                        record.NewFakeEventRecorder(),
	})

	apiFactory := notificationapi.NewFactory(record.NewAPIFactorySettings(), "default", k8sI.Core().V1().Secrets().Informer(), k8sI.Core().V1().ConfigMaps().Informer())
	// rolloutsInformer := rolloutinformers.NewRolloutInformer(f.client, "", time.Minute, cache.Indexers{})
	cm.notificationsController = notificationcontroller.NewController(dynamicClient.Resource(v1alpha1.RolloutGVR), i.Argoproj().V1alpha1().Rollouts().Informer(), apiFactory,
		notificationcontroller.WithToUnstructured(func(obj metav1.Object) (*unstructured.Unstructured, error) {
			return nil, nil
		}),
	)

	return cm
}

func newElectorConfig(kubeclientset kubernetes.Interface, id string, electOpts LeaderElectionOptions) *leaderelection.LeaderElectionConfig {
	lec := leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{Name: defaultLeaderElectionLeaseLockName, Namespace: electOpts.LeaderElectionNamespace}, Client: kubeclientset.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{Identity: id},
		},
		ReleaseOnCancel: true,
		LeaseDuration:   electOpts.LeaderElectionLeaseDuration,
		RenewDeadline:   electOpts.LeaderElectionRenewDeadline,
		RetryPeriod:     electOpts.LeaderElectionRetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				log.Info("Starting leading")
			},
			OnStoppedLeading: func() {
				log.Infof("Stopped leading controller: %s", id)
				return
			},
			OnNewLeader: func(identity string) {
				if identity == id {
					return
				}
				log.Infof("New leader elected: %s", identity)
			},
		},
	}
	return &lec
}

func TestNewManager(t *testing.T) {
	f := newFixture(t)

	i := informers.NewSharedInformerFactory(f.client, noResyncPeriodFunc())
	k8sI := kubeinformers.NewSharedInformerFactory(f.kubeclient, noResyncPeriodFunc())

	scheme := runtime.NewScheme()
	listMapping := map[schema.GroupVersionResource]string{}
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listMapping)
	dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)
	istioVirtualServiceInformer := dynamicInformerFactory.ForResource(istioutil.GetIstioVirtualServiceGVR()).Informer()
	istioDestinationRuleInformer := dynamicInformerFactory.ForResource(istioutil.GetIstioDestinationRuleGVR()).Informer()

	mode, err := ingressutil.DetermineIngressMode("extensions/v1beta1", &discoveryfake.FakeDiscovery{})
	assert.NoError(t, err)
	ingressWrapper, err := ingressutil.NewIngressWrapper(mode, f.kubeclient, k8sI)
	assert.NoError(t, err)

	k8sRequestProvider := &metrics.K8sRequestsCountProvider{}
	cm := NewManager(
		"default",
		f.kubeclient,
		f.client,
		dynamicClient,
		smifake.NewSimpleClientset(),
		&discoveryfake.FakeDiscovery{},
		k8sI.Apps().V1().ReplicaSets(),
		k8sI.Core().V1().Services(),
		ingressWrapper,
		k8sI.Batch().V1().Jobs(),
		i.Argoproj().V1alpha1().Rollouts(),
		i.Argoproj().V1alpha1().Experiments(),
		i.Argoproj().V1alpha1().AnalysisRuns(),
		i.Argoproj().V1alpha1().AnalysisTemplates(),
		i.Argoproj().V1alpha1().ClusterAnalysisTemplates(),
		dynamicClient,
		istioVirtualServiceInformer,
		istioDestinationRuleInformer,
		k8sI.Core().V1().ConfigMaps(),
		k8sI.Core().V1().Secrets(),
		noResyncPeriodFunc(),
		"test",
		8090,
		8080,
		k8sRequestProvider,
		nil,
		nil,
	)

	assert.NotNil(t, cm)
}

func TestPrimaryController(t *testing.T) {
	f := newFixture(t)

	stopCh := make(chan struct{})

	cm := f.newManager(t)
	electOpts := NewLeaderElectionOptions()
	go cm.Run(1, 1, 1, 1, 1, electOpts, stopCh)
	time.Sleep(2 * time.Second)
	close(stopCh)

	// Test primary controller shutdown secondary metrics server
	cm.secondaryMetricsServer = metrics.NewMetricsServer(metrics.ServerConfig{}, false)
	time.Sleep(2 * time.Second)
	stopCh = make(chan struct{})
	go cm.Run(1, 1, 1, 1, 1, electOpts, stopCh)
	time.Sleep(2 * time.Second)
	close(stopCh)
}

func TestTwoControllers(t *testing.T) {
	f := newFixture(t)

	stopCh := make(chan struct{})
	primary := f.newManager(t)
	electOpts := NewLeaderElectionOptions()
	go primary.Run(1, 1, 1, 1, 1, electOpts, stopCh)
	time.Sleep(1 * time.Second)

	secondary := f.newManager(t)
	secondary.healthzServer = NewHealthzServer(fmt.Sprintf(listenAddr, 8081))
	secondary.metricsServer.Addr = (fmt.Sprintf(listenAddr, 8091))
	go secondary.Run(1, 1, 1, 1, 1, electOpts, stopCh)
	time.Sleep(1 * time.Second)

	var verifyEndpoints func(url string)
	verifyEndpoints = func(url string) {
		_, err := http.Get(url)
		assert.NoErrorf(t, err, "error connecting to %s", url)
		rr := httptest.NewRecorder()
		assert.Equal(t, rr.Code, http.StatusOK)
	}

	verifyEndpoints("http://localhost:8080/healthz")
	verifyEndpoints("http://localhost:8090/metrics")
	verifyEndpoints("http://localhost:8081/healthz")
	verifyEndpoints("http://localhost:8091/metrics")

	// stop all controllers
	time.Sleep(1 * time.Second)
	close(stopCh)
}

func TestSecondaryController(t *testing.T) {
	f := newFixture(t)

	stopCh := make(chan struct{})

	cm := f.newManager(t)

	electOpts := NewLeaderElectionOptions()
	lec := newElectorConfig(f.kubeclient, "holder-123-456", *electOpts)
	le, err := leaderelection.NewLeaderElector(*lec)
	assert.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go le.Run(ctx)
	time.Sleep(1 * time.Second)
	assert.True(t, le.IsLeader())

	go cm.Run(1, 1, 1, 1, 1, electOpts, stopCh)
	time.Sleep(2 * time.Second)
	assert.True(t, le.IsLeader())
	close(stopCh)
	time.Sleep(1 * time.Second)

	// Test secondary metrics server has been started
	stopCh = make(chan struct{})
	go cm.Run(1, 1, 1, 1, 1, electOpts, stopCh)
	time.Sleep(2 * time.Second)
	assert.True(t, le.IsLeader())
	close(stopCh)
	time.Sleep(1 * time.Second)

	// Test secondary metrics server listen port is taken
	metricsServer := metrics.NewMetricsServer(metrics.ServerConfig{
		Addr: fmt.Sprintf(listenAddr, DefaultMetricsPort),
	}, false)
	go metricsServer.ListenAndServe()
	time.Sleep(1 * time.Second)
	cm.secondaryMetricsServer = nil
	stopCh = make(chan struct{})
	go cm.Run(1, 1, 1, 1, 1, electOpts, stopCh)
	time.Sleep(2 * time.Second)
	close(stopCh)
}
