package viewcontroller

import (
	"context"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/dynamic/dynamiclister"
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

// viewController is a mini controller which allows printing of live updates to rollouts
// Allows subscribers to receive updates about
type viewController struct {
	name      string
	namespace string

	kubeInformerFactory     informers.SharedInformerFactory
	dynamicInformerFactory dynamicinformer.DynamicSharedInformerFactory

	replicaSetLister  appslisters.ReplicaSetNamespaceLister
	podLister         corelisters.PodNamespaceLister
	rolloutLister     cache.GenericNamespaceLister
	experimentLister  cache.GenericNamespaceLister
	analysisRunLister cache.GenericNamespaceLister

	cacheSyncs []cache.InformerSynced

	workqueue workqueue.RateLimitingInterface
	prevObj   interface{}
	getObj    func() (interface{}, error)
	callbacks []func(interface{})
}

type RolloutViewController struct {
	*viewController
}

type ExperimentViewController struct {
	*viewController
}

type RolloutInfoCallback func(*info.RolloutInfo)

type ExperimentInfoCallback func(*info.ExperimentInfo)

func NewRolloutViewController(namespace string, name string, kubeClient kubernetes.Interface, dynamicClient dynamic.Interface) *RolloutViewController {
	vc := newViewController(namespace, name, kubeClient, dynamicClient)
	vc.cacheSyncs = append(
		vc.cacheSyncs,
		vc.dynamicInformerFactory.ForResource(v1alpha1.RolloutGVR).Informer().HasSynced,
	)
	rvc := RolloutViewController{
		viewController: vc,
	}
	vc.getObj = func() (interface{}, error) {
		return rvc.GetRolloutInfo()
	}
	return &rvc
}

func NewExperimentViewController(namespace string, name string, kubeClient kubernetes.Interface, dynamicClient dynamic.Interface) *ExperimentViewController {
	vc := newViewController(namespace, name, kubeClient, dynamicClient)
	evc := ExperimentViewController{
		viewController: vc,
	}
	vc.getObj = func() (interface{}, error) {
		return evc.GetExperimentInfo()
	}
	return &evc
}

func newViewController(namespace string, name string, kubeClient kubernetes.Interface, dynamicClient dynamic.Interface) *viewController {
	kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 0, kubeinformers.WithNamespace(namespace))
	dynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dynamicClient, 0, namespace, nil)
	///rolloutsInformerFactory := rolloutinformers.NewSharedInformerFactoryWithOptions(rolloutClient, 0, rolloutinformers.WithNamespace(namespace))

	controller := viewController{
		name:                    name,
		namespace:               namespace,
		kubeInformerFactory:     kubeInformerFactory,
		dynamicInformerFactory: dynamicInformerFactory,
		replicaSetLister:        kubeInformerFactory.Apps().V1().ReplicaSets().Lister().ReplicaSets(namespace),
		podLister:               kubeInformerFactory.Core().V1().Pods().Lister().Pods(namespace),
		rolloutLister:           dynamicInformerFactory.ForResource(v1alpha1.RolloutGVR).Lister().ByNamespace(namespace),
		experimentLister:        dynamicInformerFactory.ForResource(v1alpha1.ExperimentGVR).Lister().ByNamespace(namespace),
		analysisRunLister:       dynamicInformerFactory.ForResource(v1alpha1.AnalysisRunGVR).Lister().ByNamespace(namespace),
		workqueue:               workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}

	controller.cacheSyncs = append(controller.cacheSyncs,
		kubeInformerFactory.Apps().V1().ReplicaSets().Informer().HasSynced,
		kubeInformerFactory.Core().V1().Pods().Informer().HasSynced,
		dynamicInformerFactory.ForResource(v1alpha1.ExperimentGVR).Informer().HasSynced,
		dynamicInformerFactory.ForResource(v1alpha1.AnalysisRunGVR).Informer().HasSynced,
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
	dynamicInformerFactory.ForResource(v1alpha1.RolloutGVR).Informer().AddEventHandler(enqueueRolloutHandlerFuncs)
	dynamicInformerFactory.ForResource(v1alpha1.ExperimentGVR).Informer().AddEventHandler(enqueueRolloutHandlerFuncs)
	dynamicInformerFactory.ForResource(v1alpha1.AnalysisRunGVR).Informer().AddEventHandler(enqueueRolloutHandlerFuncs)
	return &controller
}

func (c *viewController) Start(ctx context.Context) {
	c.kubeInformerFactory.Start(ctx.Done())
	c.dynamicInformerFactory.Start(ctx.Done())
	cache.WaitForCacheSync(ctx.Done(), c.cacheSyncs...)
}

func (c *viewController) Run(ctx context.Context) error {
	go wait.Until(func() {
		for c.processNextWorkItem() {
		}
	}, time.Second, ctx.Done())
	<-ctx.Done()
	return nil
}

func (c *viewController) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()
	if shutdown {
		return false
	}
	defer c.workqueue.Done(obj)

	newObj, err := c.getObj()
	if err != nil {
		log.Warn(err.Error())
		return true
	}
	if !reflect.DeepEqual(c.prevObj, newObj) {
		for _, cb := range c.callbacks {
			cb(newObj)
		}
		c.prevObj = newObj
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
	cb := func(i interface{}) {
		callback(i.(*info.RolloutInfo))
	}
	c.callbacks = append(c.callbacks, cb)
}

func (c *ExperimentViewController) GetExperimentInfo() (*info.ExperimentInfo, error) {
	exp, err := c.experimentLister.Get(c.name)
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
	allAnalysisRuns, err := c.analysisRunLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	expInfo := info.NewExperimentInfo(exp, allReplicaSets, allAnalysisRuns, allPods)
	return expInfo, nil
}

func (c *ExperimentViewController) RegisterCallback(callback ExperimentInfoCallback) {
	cb := func(i interface{}) {
		callback(i.(*info.ExperimentInfo))
	}
	c.callbacks = append(c.callbacks, cb)
}
