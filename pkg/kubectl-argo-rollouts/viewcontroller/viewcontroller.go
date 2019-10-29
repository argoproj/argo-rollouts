package viewcontroller

import (
	"context"
	"reflect"
	"time"

	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	rolloutclientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	rolloutinformers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions"
	rolloutlisters "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/info"
)

// RolloutViewController is a mini controller which allows printing of live updates to rollouts
// Allows subscribers to receive updates about
type RolloutViewController struct {
	name      string
	namespace string

	kubeInformerFactory     informers.SharedInformerFactory
	rolloutsInformerFactory rolloutinformers.SharedInformerFactory

	replicaSetLister  appslisters.ReplicaSetNamespaceLister
	podLister         corelisters.PodNamespaceLister
	rolloutLister     rolloutlisters.RolloutNamespaceLister
	experimentLister  rolloutlisters.ExperimentNamespaceLister
	analysisRunLister rolloutlisters.AnalysisRunNamespaceLister

	cacheSyncs []cache.InformerSynced

	workqueue       workqueue.RateLimitingInterface
	callbacks       []RolloutInfoCallback
	prevRolloutInfo info.RolloutInfo
}

type RolloutInfoCallback func(*info.RolloutInfo)

func NewController(namespace string, name string, kubeClient kubernetes.Interface, rolloutClient rolloutclientset.Interface) *RolloutViewController {
	kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 0, kubeinformers.WithNamespace(namespace))
	rolloutsInformerFactory := rolloutinformers.NewSharedInformerFactoryWithOptions(rolloutClient, 0, rolloutinformers.WithNamespace(namespace))

	controller := RolloutViewController{
		name:                    name,
		namespace:               namespace,
		kubeInformerFactory:     kubeInformerFactory,
		rolloutsInformerFactory: rolloutsInformerFactory,
		replicaSetLister:        kubeInformerFactory.Apps().V1().ReplicaSets().Lister().ReplicaSets(namespace),
		podLister:               kubeInformerFactory.Core().V1().Pods().Lister().Pods(namespace),
		rolloutLister:           rolloutsInformerFactory.Argoproj().V1alpha1().Rollouts().Lister().Rollouts(namespace),
		experimentLister:        rolloutsInformerFactory.Argoproj().V1alpha1().Experiments().Lister().Experiments(namespace),
		analysisRunLister:       rolloutsInformerFactory.Argoproj().V1alpha1().AnalysisRuns().Lister().AnalysisRuns(namespace),
		workqueue:               workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	controller.cacheSyncs = append(controller.cacheSyncs,
		kubeInformerFactory.Apps().V1().ReplicaSets().Informer().HasSynced,
		kubeInformerFactory.Core().V1().Pods().Informer().HasSynced,
		rolloutsInformerFactory.Argoproj().V1alpha1().Rollouts().Informer().HasSynced,
		rolloutsInformerFactory.Argoproj().V1alpha1().Experiments().Informer().HasSynced,
		rolloutsInformerFactory.Argoproj().V1alpha1().AnalysisRuns().Informer().HasSynced,
	)

	enqueueRolloutHandlerFuncs := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controller.workqueue.Add(controller.name)
		},
		UpdateFunc: func(old, new interface{}) {
			controller.workqueue.Add(controller.name)
		},
		DeleteFunc: func(obj interface{}) {
			controller.workqueue.Add(controller.name)
		},
	}

	// changes to any of these resources will enqueue the rollout for refreshing
	kubeInformerFactory.Apps().V1().ReplicaSets().Informer().AddEventHandler(enqueueRolloutHandlerFuncs)
	kubeInformerFactory.Core().V1().Pods().Informer().AddEventHandler(enqueueRolloutHandlerFuncs)
	rolloutsInformerFactory.Argoproj().V1alpha1().Rollouts().Informer().AddEventHandler(enqueueRolloutHandlerFuncs)
	rolloutsInformerFactory.Argoproj().V1alpha1().Experiments().Informer().AddEventHandler(enqueueRolloutHandlerFuncs)
	rolloutsInformerFactory.Argoproj().V1alpha1().AnalysisRuns().Informer().AddEventHandler(enqueueRolloutHandlerFuncs)

	return &controller
}

func (c *RolloutViewController) Start(ctx context.Context) {
	c.kubeInformerFactory.Start(ctx.Done())
	c.rolloutsInformerFactory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), c.cacheSyncs...)
}

func (c *RolloutViewController) Run(ctx context.Context) error {
	go wait.Until(func() {
		for c.processNextWorkItem() {
		}
	}, time.Second, ctx.Done())
	<-ctx.Done()
	return nil
}

func (c *RolloutViewController) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}
	defer c.workqueue.Done(obj)

	newRolloutInfo, err := c.GetRolloutInfo()
	if err != nil {
		log.Warn(err.Error())
		return true
	}
	if !reflect.DeepEqual(c.prevRolloutInfo, *newRolloutInfo) {
		for _, cb := range c.callbacks {
			cb(newRolloutInfo)
		}
		c.prevRolloutInfo = *newRolloutInfo
	}
	return true
}

func (c *RolloutViewController) GetRolloutInfo() (*info.RolloutInfo, error) {
	ro, err := c.rolloutLister.Get(c.name)
	if err != nil {
		return nil, err
	}

	allReplicaSets, err := c.replicaSetLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	allPods, err := c.podLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	allExps, err := c.experimentLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	allAnalysisRuns, err := c.analysisRunLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	roInfo := info.NewRolloutInfo(ro, allReplicaSets, allPods, allExps, allAnalysisRuns)
	return roInfo, nil
}

func (c *RolloutViewController) RegisterCallback(callback RolloutInfoCallback) {
	c.callbacks = append(c.callbacks, callback)
}
