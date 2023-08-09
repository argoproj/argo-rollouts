package controller

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	notificationapi "github.com/argoproj/notifications-engine/pkg/api"
	notificationcontroller "github.com/argoproj/notifications-engine/pkg/controller"
	smifake "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	discoveryfake "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	kubeinformers "k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"

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
		wg:                                   &sync.WaitGroup{},
		healthzServer:                        NewHealthzServer(fmt.Sprintf(listenAddr, 8080)),
		rolloutSynced:                        alwaysReady,
		experimentSynced:                     alwaysReady,
		analysisRunSynced:                    alwaysReady,
		analysisTemplateSynced:               alwaysReady,
		clusterAnalysisTemplateSynced:        alwaysReady,
		serviceSynced:                        alwaysReady,
		ingressSynced:                        alwaysReady,
		jobSynced:                            alwaysReady,
		replicasSetSynced:                    alwaysReady,
		configMapSynced:                      alwaysReady,
		secretSynced:                         alwaysReady,
		rolloutWorkqueue:                     rolloutWorkqueue,
		serviceWorkqueue:                     serviceWorkqueue,
		ingressWorkqueue:                     ingressWorkqueue,
		experimentWorkqueue:                  experimentWorkqueue,
		analysisRunWorkqueue:                 analysisRunWorkqueue,
		kubeClientSet:                        f.kubeclient,
		namespace:                            "",
		namespaced:                           false,
		notificationSecretInformerFactory:    kubeinformers.NewSharedInformerFactoryWithOptions(f.kubeclient, noResyncPeriodFunc()),
		notificationConfigMapInformerFactory: kubeinformers.NewSharedInformerFactoryWithOptions(f.kubeclient, noResyncPeriodFunc()),
	}

	metricsAddr := fmt.Sprintf(listenAddr, 8090)
	cm.metricsServer = metrics.NewMetricsServer(metrics.ServerConfig{
		Addr:               metricsAddr,
		K8SRequestProvider: &metrics.K8sRequestsCountProvider{},
	})

	i := informers.NewSharedInformerFactory(f.client, noResyncPeriodFunc())
	k8sI := kubeinformers.NewSharedInformerFactory(f.kubeclient, noResyncPeriodFunc())
	tgbGVR := schema.GroupVersionResource{
		Group:    "elbv2.k8s.aws",
		Version:  "v1beta1",
		Resource: "targetgroupbindings",
	}
	vsvcGVR := istioutil.GetIstioVirtualServiceGVR()
	destGVR := istioutil.GetIstioDestinationRuleGVR()
	scheme := runtime.NewScheme()
	listMapping := map[schema.GroupVersionResource]string{
		tgbGVR:  "TargetGroupBindingList",
		vsvcGVR: vsvcGVR.Resource + "List",
		destGVR: destGVR.Resource + "List",
	}

	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme, listMapping)
	dynamicInformerFactory := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)
	istioVirtualServiceInformer := dynamicInformerFactory.ForResource(istioutil.GetIstioVirtualServiceGVR()).Informer()
	istioDestinationRuleInformer := dynamicInformerFactory.ForResource(istioutil.GetIstioDestinationRuleGVR()).Informer()

	cm.dynamicInformerFactory = dynamicInformerFactory
	cm.clusterDynamicInformerFactory = dynamicInformerFactory
	cm.kubeInformerFactory = k8sI
	cm.jobInformerFactory = k8sI
	cm.istioPrimaryDynamicClient = dynamicClient
	cm.istioDynamicInformerFactory = dynamicInformerFactory

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
		k8sI,
		k8sI,
		noResyncPeriodFunc(),
		"test",
		8090,
		8080,
		k8sRequestProvider,
		nil,
		nil,
		dynamicInformerFactory,
		nil,
		nil,
		false,
		nil,
		nil,
	)

	assert.NotNil(t, cm)
}

func TestPrimaryController(t *testing.T) {
	f := newFixture(t)

	cm := f.newManager(t)
	electOpts := NewLeaderElectionOptions()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Second)
		cancel()
	}()
	cm.Run(ctx, 1, 1, 1, 1, 1, electOpts)
}

func TestPrimaryControllerSingleInstanceWithShutdown(t *testing.T) {
	f := newFixture(t)

	cm := f.newManager(t)
	electOpts := NewLeaderElectionOptions()
	electOpts.LeaderElect = false
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Second)
		cancel()
	}()
	cm.Run(ctx, 1, 1, 1, 1, 1, electOpts)
}
