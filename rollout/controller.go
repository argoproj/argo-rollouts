package rollout

import (
	"encoding/json"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/validation"

	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	extensionsinformers "k8s.io/client-go/informers/extensions/v1beta1"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	v1 "k8s.io/client-go/listers/core/v1"
	extensionslisters "k8s.io/client-go/listers/extensions/v1beta1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/cmd/kubeadm/app/util"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	register "github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	listers "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	serviceutil "github.com/argoproj/argo-rollouts/utils/service"
)

const (
	virtualServiceIndexName = "byVirtualService"
)

// Controller is the controller implementation for Rollout resources
type Controller struct {
	// namespace which namespace(s) operates on
	namespace string
	// rsControl is used for adopting/releasing replica sets.
	replicaSetControl controller.RSControlInterface

	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// argoprojclientset is a clientset for our own API group
	argoprojclientset clientset.Interface
	// dynamicclientset is a dynamic clientset for interacting with unstructured resources.
	// It is used to interact with TrafficRouting resources
	dynamicclientset           dynamic.Interface
	smiclientset               smiclientset.Interface
	defaultIstioVersion        string
	defaultTrafficSplitVersion string

	replicaSetLister              appslisters.ReplicaSetLister
	replicaSetSynced              cache.InformerSynced
	rolloutsLister                listers.RolloutLister
	rolloutsSynced                cache.InformerSynced
	rolloutsIndexer               cache.Indexer
	servicesLister                v1.ServiceLister
	ingressesLister               extensionslisters.IngressLister
	experimentsLister             listers.ExperimentLister
	analysisRunLister             listers.AnalysisRunLister
	analysisTemplateLister        listers.AnalysisTemplateLister
	clusterAnalysisTemplateLister listers.ClusterAnalysisTemplateLister
	metricsServer                 *metrics.MetricsServer

	podRestarter RolloutPodRestarter

	// used for unit testing
	enqueueRollout              func(obj interface{})
	enqueueRolloutAfter         func(obj interface{}, duration time.Duration)
	newTrafficRoutingReconciler func(roCtx rolloutContext) (TrafficRoutingReconciler, error)

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	rolloutWorkqueue workqueue.RateLimitingInterface
	serviceWorkqueue workqueue.RateLimitingInterface
	ingressWorkqueue workqueue.RateLimitingInterface
	// recorder is an event recorder for recording Event resources to the
	// Kubernetes API.
	recorder     record.EventRecorder
	resyncPeriod time.Duration
}

// ControllerConfig describes the data required to instantiate a new rollout controller
type ControllerConfig struct {
	Namespace                       string
	KubeClientSet                   kubernetes.Interface
	ArgoProjClientset               clientset.Interface
	DynamicClientSet                dynamic.Interface
	SmiClientSet                    smiclientset.Interface
	ExperimentInformer              informers.ExperimentInformer
	AnalysisRunInformer             informers.AnalysisRunInformer
	AnalysisTemplateInformer        informers.AnalysisTemplateInformer
	ClusterAnalysisTemplateInformer informers.ClusterAnalysisTemplateInformer
	ReplicaSetInformer              appsinformers.ReplicaSetInformer
	ServicesInformer                coreinformers.ServiceInformer
	IngressInformer                 extensionsinformers.IngressInformer
	RolloutsInformer                informers.RolloutInformer
	ResyncPeriod                    time.Duration
	RolloutWorkQueue                workqueue.RateLimitingInterface
	ServiceWorkQueue                workqueue.RateLimitingInterface
	IngressWorkQueue                workqueue.RateLimitingInterface
	MetricsServer                   *metrics.MetricsServer
	Recorder                        record.EventRecorder
	DefaultIstioVersion             string
	DefaultTrafficSplitVersion      string
}

// NewController returns a new rollout controller
func NewController(cfg ControllerConfig) *Controller {

	replicaSetControl := controller.RealRSControl{
		KubeClient: cfg.KubeClientSet,
		Recorder:   cfg.Recorder,
	}

	podRestarter := RolloutPodRestarter{
		client:       cfg.KubeClientSet,
		resyncPeriod: cfg.ResyncPeriod,
		enqueueAfter: func(obj interface{}, duration time.Duration) {
			controllerutil.EnqueueAfter(obj, duration, cfg.RolloutWorkQueue)
		},
	}

	controller := &Controller{
		namespace:                     cfg.Namespace,
		kubeclientset:                 cfg.KubeClientSet,
		argoprojclientset:             cfg.ArgoProjClientset,
		dynamicclientset:              cfg.DynamicClientSet,
		smiclientset:                  cfg.SmiClientSet,
		defaultIstioVersion:           cfg.DefaultIstioVersion,
		defaultTrafficSplitVersion:    cfg.DefaultTrafficSplitVersion,
		replicaSetControl:             replicaSetControl,
		replicaSetLister:              cfg.ReplicaSetInformer.Lister(),
		replicaSetSynced:              cfg.ReplicaSetInformer.Informer().HasSynced,
		rolloutsIndexer:               cfg.RolloutsInformer.Informer().GetIndexer(),
		rolloutsLister:                cfg.RolloutsInformer.Lister(),
		rolloutsSynced:                cfg.RolloutsInformer.Informer().HasSynced,
		rolloutWorkqueue:              cfg.RolloutWorkQueue,
		serviceWorkqueue:              cfg.ServiceWorkQueue,
		ingressWorkqueue:              cfg.IngressWorkQueue,
		servicesLister:                cfg.ServicesInformer.Lister(),
		ingressesLister:               cfg.IngressInformer.Lister(),
		experimentsLister:             cfg.ExperimentInformer.Lister(),
		analysisRunLister:             cfg.AnalysisRunInformer.Lister(),
		analysisTemplateLister:        cfg.AnalysisTemplateInformer.Lister(),
		clusterAnalysisTemplateLister: cfg.ClusterAnalysisTemplateInformer.Lister(),
		recorder:                      cfg.Recorder,
		resyncPeriod:                  cfg.ResyncPeriod,
		metricsServer:                 cfg.MetricsServer,
		podRestarter:                  podRestarter,
	}
	controller.enqueueRollout = func(obj interface{}) {
		controllerutil.EnqueueRateLimited(obj, cfg.RolloutWorkQueue)
	}
	controller.enqueueRolloutAfter = func(obj interface{}, duration time.Duration) {
		controllerutil.EnqueueAfter(obj, duration, cfg.RolloutWorkQueue)
	}

	controller.newTrafficRoutingReconciler = controller.NewTrafficRoutingReconciler

	log.Info("Setting up event handlers")
	// Set up an event handler for when rollout resources change
	cfg.RolloutsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRollout,
		UpdateFunc: func(old, new interface{}) {
			if r, ok := old.(*v1alpha1.Rollout); ok {
				for _, s := range serviceutil.GetRolloutServiceKeys(r) {
					controller.serviceWorkqueue.AddRateLimited(s)
				}
			}
			controller.enqueueRollout(new)
		},
		DeleteFunc: func(obj interface{}) {
			if r, ok := obj.(*v1alpha1.Rollout); ok {
				for _, s := range serviceutil.GetRolloutServiceKeys(r) {
					controller.serviceWorkqueue.AddRateLimited(s)
				}
			}
		},
	})

	util.CheckErr(cfg.RolloutsInformer.Informer().AddIndexers(cache.Indexers{
		virtualServiceIndexName: func(obj interface{}) (strings []string, e error) {
			if rollout, ok := obj.(*v1alpha1.Rollout); ok {
				return istio.GetRolloutVirtualServiceKeys(rollout), nil
			}
			return
		},
	}))

	cfg.ReplicaSetInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controllerutil.EnqueueParentObject(obj, register.RolloutKind, controller.enqueueRollout)
		},
		UpdateFunc: func(old, new interface{}) {
			newRS := new.(*appsv1.ReplicaSet)
			oldRS := old.(*appsv1.ReplicaSet)
			if newRS.ResourceVersion == oldRS.ResourceVersion {
				// Periodic resync will send update events for all known replicas.
				// Two different versions of the same Replica will always have different RVs.
				return
			}
			controllerutil.EnqueueParentObject(new, register.RolloutKind, controller.enqueueRollout)
		},
		DeleteFunc: func(obj interface{}) {
			controllerutil.EnqueueParentObject(obj, register.RolloutKind, controller.enqueueRollout)
		},
	})

	cfg.AnalysisRunInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			controllerutil.EnqueueParentObject(obj, register.RolloutKind, controller.enqueueRollout)
		},
		UpdateFunc: func(old, new interface{}) {
			newAR := new.(*v1alpha1.AnalysisRun)
			oldAR := old.(*v1alpha1.AnalysisRun)
			if newAR.Status.Phase == oldAR.Status.Phase {
				// Only enqueue rollout if the status changed
				return
			}
			controllerutil.EnqueueParentObject(new, register.RolloutKind, controller.enqueueRollout)
		},
		DeleteFunc: func(obj interface{}) {
			controllerutil.EnqueueParentObject(obj, register.RolloutKind, controller.enqueueRollout)
		},
	})
	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	log.Info("Starting Rollout workers")
	for i := 0; i < threadiness; i++ {
		go wait.Until(func() {
			controllerutil.RunWorker(c.rolloutWorkqueue, logutil.RolloutKey, c.syncHandler, c.metricsServer)
		}, time.Second, stopCh)
	}
	log.Info("Started Rollout workers")

	gvk := schema.ParseGroupResource("virtualservices.networking.istio.io").WithVersion(c.defaultIstioVersion)
	go controllerutil.WatchResourceWithExponentialBackoff(stopCh, c.dynamicclientset, c.namespace, gvk, c.rolloutWorkqueue, c.rolloutsIndexer, virtualServiceIndexName)

	<-stopCh
	log.Info("Shutting down workers")

	return nil
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Phase block of the Rollout resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	startTime := time.Now()
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	log.WithField(logutil.RolloutKey, name).WithField(logutil.NamespaceKey, namespace).Infof("Started syncing rollout at (%v)", startTime)
	rollout, err := c.rolloutsLister.Rollouts(namespace).Get(name)
	if k8serrors.IsNotFound(err) {
		log.WithField(logutil.RolloutKey, name).WithField(logutil.NamespaceKey, namespace).Info("Rollout has been deleted")
		return nil
	}
	if err != nil {
		return err
	}

	// Remarshal the rollout to normalize all fields so that when we calculate hashes against the
	// rollout spec and pod template spec, the hash will be consistent. See issue #70
	// This also returns a copy of the rollout to prevent mutation of the informer cache.
	r := remarshalRollout(rollout)
	logCtx := logutil.WithRollout(r)

	// TODO(dthomson) remove in v0.9.0
	migrated := c.migrateCanaryStableRS(r)
	if migrated {
		logutil.WithRollout(r).Info("Migrated stableRS field")
		return nil
	}

	if r.ObjectMeta.DeletionTimestamp != nil {
		logCtx.Info("No reconciliation as rollout marked for deletion")
		return nil
	}

	// In order to work with HPA, the rollout.Spec.Replica field cannot be nil. As a result, the controller will update
	// the rollout to have the replicas field set to the default value. see https://github.com/argoproj/argo-rollouts/issues/119
	if rollout.Spec.Replicas == nil {
		logCtx.Info("Setting .Spec.Replica to 1 from nil")
		r.Spec.Replicas = pointer.Int32Ptr(defaults.DefaultReplicas)
		_, err := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(r.Namespace).Update(r)
		return err
	}
	defer func() {
		duration := time.Since(startTime)
		c.metricsServer.IncRolloutReconcile(r, duration)
		logCtx.WithField("time_ms", duration.Seconds()*1e3).Info("Reconciliation completed")
	}()

	// TODO: put into helper functions
	rolloutValidationErrors := validation.ValidateRollout(rollout)
	if len(rolloutValidationErrors) == 0 {
		referencedResources, rolloutValidationReferencesErrors, err := c.getRolloutReferencedResources(rollout)
		if err != nil {
			return err
		}
		rolloutValidationErrors = append(rolloutValidationErrors, rolloutValidationReferencesErrors...)
		if len(rolloutValidationErrors) == 0 {
			rolloutValidationErrors = append(rolloutValidationErrors, validation.ValidateRolloutReferencedResources(r, referencedResources)...)
		}
	}


	if len(rolloutValidationErrors) > 0 {
		validationError := rolloutValidationErrors[0]
		prevCond := conditions.GetRolloutCondition(rollout.Status, v1alpha1.InvalidSpec)
		invalidSpecCond := prevCond
		if prevCond == nil || prevCond.Message != validationError.Detail {
			invalidSpecCond = conditions.NewRolloutCondition(v1alpha1.InvalidSpec, corev1.ConditionTrue, conditions.InvalidSpecReason, validationError.Detail)
		}
		logutil.WithRollout(r).Error("Spec submitted is invalid")
		generation := conditions.ComputeGenerationHash(r.Spec)
		if r.Status.ObservedGeneration != generation || !reflect.DeepEqual(invalidSpecCond, prevCond) {
			newStatus := r.Status.DeepCopy()
			// SetRolloutCondition only updates the condition when the status and/or reason changes, but
			// the controller should update the invalidSpec if there is a change in why the spec is invalid
			if prevCond != nil && prevCond.Message != invalidSpecCond.Message {
				conditions.RemoveRolloutCondition(newStatus, v1alpha1.InvalidSpec)
			}
			err := c.patchCondition(r, newStatus, invalidSpecCond)
			if err != nil {
				return err
			}
		}
		return nil
	}

	// List ReplicaSets owned by this Rollout, while reconciling ControllerRef
	// through adoption/orphaning.
	rsList, err := c.getReplicaSetsForRollouts(r)
	if err != nil {
		return err
	}

	err = c.checkPausedConditions(r)
	if err != nil {
		return err
	}

	isScalingEvent, err := c.isScalingEvent(r, rsList)
	if err != nil {
		return err
	}

	if getPauseCondition(r, v1alpha1.PauseReasonInconclusiveAnalysis) != nil || r.Spec.Paused || isScalingEvent {
		return c.syncReplicasOnly(r, rsList, isScalingEvent)
	}

	if rollout.Spec.Strategy.BlueGreen != nil {
		return c.rolloutBlueGreen(r, rsList)
	}
	if rollout.Spec.Strategy.Canary != nil {
		return c.rolloutCanary(r, rsList)
	}
	return fmt.Errorf("no rollout strategy selected")
}

// TODO: don't put errors in fields.Error -> possible operational problem
// Note: Listers do not return errors except 'Not Found', which will be translated as a field error
// This method will likely never return an error
func (c *Controller) getRolloutReferencedResources(r *v1alpha1.Rollout) (validation.ReferencedResources, field.ErrorList, error) {
	// TODO: create in-line function?
	//errorReturner := func() {}
	allErrs := field.ErrorList{}
	referencedResources := validation.ReferencedResources{}
	if r.Spec.Strategy.BlueGreen != nil {
		blueGreen := r.Spec.Strategy.BlueGreen
		_, _, err := c.getPreviewAndActiveServices(r)
		if err != nil {
			return err
		}
		for _, template := range blueGreen.PrePromotionAnalysis.Templates {
			if template.ClusterScope {
				clusterAnalysisTemplate, err := c.clusterAnalysisTemplateLister.Get(template.TemplateName)
				if err != nil {
					if !k8serrors.IsNotFound(err) {
						return referencedResources, nil, err
					} else {
						// TODO: Create field error
						//allErrs = append(allErrs, field.Error{})
					}
				}
				referencedResources.ClusterAnalysisTemplates = append(referencedResources.ClusterAnalysisTemplates, *clusterAnalysisTemplate)
			} else {
				analysisTemplate, err := c.analysisTemplateLister.AnalysisTemplates(r.Namespace).Get(template.TemplateName)
				if err != nil {
					return err
				}
				referencedResources.AnalysisTemplates = append(referencedResources.AnalysisTemplates, *analysisTemplate)
			}
		}
		for _, template := range blueGreen.PostPromotionAnalysis.Templates {
			if template.ClusterScope {
				clusterAnalysisTemplate, err := c.clusterAnalysisTemplateLister.Get(template.TemplateName)
				if err != nil {
					return err
				}
				referencedResources.ClusterAnalysisTemplates = append(referencedResources.ClusterAnalysisTemplates, *clusterAnalysisTemplate)
			} else {
				analysisTemplate, err := c.analysisTemplateLister.AnalysisTemplates(r.Namespace).Get(template.TemplateName)
				if err != nil {
					return err
				}
				referencedResources.AnalysisTemplates = append(referencedResources.AnalysisTemplates, *analysisTemplate)
			}
		}

	}

	if r.Spec.Strategy.Canary != nil {
		canary := r.Spec.Strategy.Canary
		if r.Spec.Strategy.Canary.CanaryService != "" {
			_, err := c.getReferencedService(r, r.Spec.Strategy.Canary.CanaryService)
			if err != nil {
				return err
			}
		}
		if r.Spec.Strategy.Canary.StableService != "" {
			_, err := c.getReferencedService(r, r.Spec.Strategy.Canary.StableService)
			if err != nil {
				return err
			}
		}
		for _, step := range canary.Steps {
			for _, template := range step.Analysis.Templates {
				if template.ClusterScope {
					clusterAnalysisTemplate, err := c.clusterAnalysisTemplateLister.Get(template.TemplateName)
					if err != nil {
						return err
					}
					referencedResources.ClusterAnalysisTemplates = append(referencedResources.ClusterAnalysisTemplates, *clusterAnalysisTemplate)
				} else {
					analysisTemplate, err := c.analysisTemplateLister.AnalysisTemplates(r.Namespace).Get(template.TemplateName)
					if err != nil {
						return err
					}
					referencedResources.AnalysisTemplates = append(referencedResources.AnalysisTemplates, *analysisTemplate)
				}
			}
		}

		trafficRouting := canary.TrafficRouting
		if trafficRouting.ALB != nil {
			ingress, err := c.ingressesLister.Ingresses(r.Namespace).Get(trafficRouting.ALB.Ingress)
			if err != nil {
				return err
			}
			referencedResources.Ingresses = append(referencedResources.Ingresses, *ingress)
		} else if trafficRouting.Nginx != nil {
			ingress, err := c.ingressesLister.Ingresses(r.Namespace).Get(trafficRouting.Nginx.StableIngress)
			if err != nil {
				return err
			}
			referencedResources.Ingresses = append(referencedResources.Ingresses, *ingress)
		} else if trafficRouting.Istio != nil {
			vsvcName := trafficRouting.Istio.VirtualService.Name
			gvk := schema.ParseGroupResource("virtualservices.networking.istio.io").WithVersion(c.defaultIstioVersion)
			vsvc, err := c.dynamicclientset.Resource(gvk).Namespace(r.Namespace).Get(vsvcName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			referencedResources.VirtualServices = append(referencedResources.VirtualServices, *vsvc)
		}
	} // TODO: if errors finding objects, don't run ValidateRolloutReferencedResources
	return referencedResources, allErrs
}

func (c *Controller) migrateCanaryStableRS(rollout *v1alpha1.Rollout) bool {
	if rollout.Spec.Strategy.Canary == nil {
		return false
	}
	if rollout.Status.StableRS == "" && rollout.Status.Canary.StableRS == "" {
		return false
	}
	if rollout.Status.StableRS != "" && rollout.Status.Canary.StableRS != "" {
		return false
	}
	stableRS := rollout.Status.StableRS
	if rollout.Status.Canary.StableRS != "" {
		stableRS = rollout.Status.Canary.StableRS
	}
	rollout.Status.Canary.StableRS = stableRS
	rollout.Status.StableRS = stableRS
	_, err := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(rollout.Namespace).Update(rollout)
	if err != nil {
		logutil.WithRollout(rollout).Errorf("Unable to migrate Rollout's status.canary.stableRS to status.stableRS: %s", err.Error())
	}
	return true
}

func remarshalRollout(r *v1alpha1.Rollout) *v1alpha1.Rollout {
	rolloutBytes, err := json.Marshal(r)
	if err != nil {
		panic(err)
	}
	var remarshalled v1alpha1.Rollout
	err = json.Unmarshal(rolloutBytes, &remarshalled)
	if err != nil {
		panic(err)
	}
	return &remarshalled
}
