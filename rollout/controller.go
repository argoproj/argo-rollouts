package rollout

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"

	smiclientset "github.com/servicemeshinterface/smi-sdk-go/pkg/gen/client/split/clientset/versioned"
	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
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
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubectl/pkg/util/slice"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/utils/pointer"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	register "github.com/argoproj/argo-rollouts/pkg/apis/rollouts"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/validation"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	informers "github.com/argoproj/argo-rollouts/pkg/client/informers/externalversions/rollouts/v1alpha1"
	listers "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/ambassador"
	"github.com/argoproj/argo-rollouts/rollout/trafficrouting/istio"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/conditions"
	controllerutil "github.com/argoproj/argo-rollouts/utils/controller"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	istioutil "github.com/argoproj/argo-rollouts/utils/istio"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	"github.com/argoproj/argo-rollouts/utils/record"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	serviceutil "github.com/argoproj/argo-rollouts/utils/service"
	unstructuredutil "github.com/argoproj/argo-rollouts/utils/unstructured"
)

type TemplateRefResolver interface {
	Resolve(r *v1alpha1.Rollout) error
}

// Controller is the controller implementation for Rollout resources
type Controller struct {
	reconcilerBase

	// namespace which namespace(s) operates on
	namespace string
	// rsControl is used for adopting/releasing replica sets.
	replicaSetControl controller.RSControlInterface

	metricsServer *metrics.MetricsServer

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	rolloutWorkqueue workqueue.RateLimitingInterface
	serviceWorkqueue workqueue.RateLimitingInterface
	ingressWorkqueue workqueue.RateLimitingInterface
}

// ControllerConfig describes the data required to instantiate a new rollout controller
type ControllerConfig struct {
	Namespace                       string
	KubeClientSet                   kubernetes.Interface
	ArgoProjClientset               clientset.Interface
	DynamicClientSet                dynamic.Interface
	RefResolver                     TemplateRefResolver
	SmiClientSet                    smiclientset.Interface
	ExperimentInformer              informers.ExperimentInformer
	AnalysisRunInformer             informers.AnalysisRunInformer
	AnalysisTemplateInformer        informers.AnalysisTemplateInformer
	ClusterAnalysisTemplateInformer informers.ClusterAnalysisTemplateInformer
	ReplicaSetInformer              appsinformers.ReplicaSetInformer
	ServicesInformer                coreinformers.ServiceInformer
	IngressInformer                 extensionsinformers.IngressInformer
	RolloutsInformer                informers.RolloutInformer
	IstioPrimaryDynamicClient       dynamic.Interface
	IstioVirtualServiceInformer     cache.SharedIndexInformer
	IstioDestinationRuleInformer    cache.SharedIndexInformer
	ResyncPeriod                    time.Duration
	RolloutWorkQueue                workqueue.RateLimitingInterface
	ServiceWorkQueue                workqueue.RateLimitingInterface
	IngressWorkQueue                workqueue.RateLimitingInterface
	MetricsServer                   *metrics.MetricsServer
	Recorder                        record.EventRecorder
}

// reconcilerBase is a shared datastructure containing all clients and configuration necessary to
// reconcile a rollout. This is shared between the controller and the rolloutContext
type reconcilerBase struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// argoprojclientset is a clientset for our own API group
	argoprojclientset clientset.Interface
	// dynamicclientset is a dynamic clientset for interacting with unstructured resources.
	// It is used to interact with TrafficRouting resources
	dynamicclientset dynamic.Interface
	smiclientset     smiclientset.Interface

	refResolver TemplateRefResolver

	replicaSetLister              appslisters.ReplicaSetLister
	replicaSetSynced              cache.InformerSynced
	rolloutsInformer              cache.SharedIndexInformer
	rolloutsLister                listers.RolloutLister
	rolloutsSynced                cache.InformerSynced
	rolloutsIndexer               cache.Indexer
	servicesLister                v1.ServiceLister
	ingressesLister               extensionslisters.IngressLister
	experimentsLister             listers.ExperimentLister
	analysisRunLister             listers.AnalysisRunLister
	analysisTemplateLister        listers.AnalysisTemplateLister
	clusterAnalysisTemplateLister listers.ClusterAnalysisTemplateLister
	IstioController               *istio.IstioController

	podRestarter RolloutPodRestarter

	// used for unit testing
	enqueueRollout              func(obj interface{})                                                          //nolint:structcheck
	enqueueRolloutAfter         func(obj interface{}, duration time.Duration)                                  //nolint:structcheck
	newTrafficRoutingReconciler func(roCtx *rolloutContext) ([]trafficrouting.TrafficRoutingReconciler, error) //nolint:structcheck

	// recorder is an event recorder for recording Event resources to the Kubernetes API.
	recorder     record.EventRecorder
	resyncPeriod time.Duration
}

// NewController returns a new rollout controller
func NewController(cfg ControllerConfig) *Controller {

	replicaSetControl := controller.RealRSControl{
		KubeClient: cfg.KubeClientSet,
		Recorder:   cfg.Recorder.K8sRecorder(),
	}

	podRestarter := RolloutPodRestarter{
		client:       cfg.KubeClientSet,
		resyncPeriod: cfg.ResyncPeriod,
		enqueueAfter: func(obj interface{}, duration time.Duration) {
			controllerutil.EnqueueAfter(obj, duration, cfg.RolloutWorkQueue)
		},
	}

	base := reconcilerBase{
		kubeclientset:                 cfg.KubeClientSet,
		argoprojclientset:             cfg.ArgoProjClientset,
		dynamicclientset:              cfg.DynamicClientSet,
		smiclientset:                  cfg.SmiClientSet,
		replicaSetLister:              cfg.ReplicaSetInformer.Lister(),
		replicaSetSynced:              cfg.ReplicaSetInformer.Informer().HasSynced,
		rolloutsInformer:              cfg.RolloutsInformer.Informer(),
		rolloutsIndexer:               cfg.RolloutsInformer.Informer().GetIndexer(),
		rolloutsLister:                cfg.RolloutsInformer.Lister(),
		rolloutsSynced:                cfg.RolloutsInformer.Informer().HasSynced,
		servicesLister:                cfg.ServicesInformer.Lister(),
		ingressesLister:               cfg.IngressInformer.Lister(),
		experimentsLister:             cfg.ExperimentInformer.Lister(),
		analysisRunLister:             cfg.AnalysisRunInformer.Lister(),
		analysisTemplateLister:        cfg.AnalysisTemplateInformer.Lister(),
		clusterAnalysisTemplateLister: cfg.ClusterAnalysisTemplateInformer.Lister(),
		recorder:                      cfg.Recorder,
		resyncPeriod:                  cfg.ResyncPeriod,
		podRestarter:                  podRestarter,
		refResolver:                   cfg.RefResolver,
	}

	controller := &Controller{
		reconcilerBase:    base,
		namespace:         cfg.Namespace,
		replicaSetControl: replicaSetControl,
		rolloutWorkqueue:  cfg.RolloutWorkQueue,
		serviceWorkqueue:  cfg.ServiceWorkQueue,
		ingressWorkqueue:  cfg.IngressWorkQueue,
		metricsServer:     cfg.MetricsServer,
	}
	controller.enqueueRollout = func(obj interface{}) {
		controllerutil.EnqueueRateLimited(obj, cfg.RolloutWorkQueue)
	}
	controller.enqueueRolloutAfter = func(obj interface{}, duration time.Duration) {
		controllerutil.EnqueueAfter(obj, duration, cfg.RolloutWorkQueue)
	}

	controller.IstioController = istio.NewIstioController(istio.IstioControllerConfig{
		ArgoprojClientSet:       cfg.ArgoProjClientset,
		DynamicClientSet:        cfg.IstioPrimaryDynamicClient,
		EnqueueRollout:          controller.enqueueRollout,
		RolloutsInformer:        cfg.RolloutsInformer,
		VirtualServiceInformer:  cfg.IstioVirtualServiceInformer,
		DestinationRuleInformer: cfg.IstioDestinationRuleInformer,
	})
	controller.newTrafficRoutingReconciler = controller.NewTrafficRoutingReconciler

	log.Info("Setting up event handlers")
	// Set up an event handler for when rollout resources change
	cfg.RolloutsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueRollout,
		UpdateFunc: func(old, new interface{}) {
			oldRollout := unstructuredutil.ObjectToRollout(old)
			newRollout := unstructuredutil.ObjectToRollout(new)
			if oldRollout != nil && newRollout != nil {
				// Check if rollout services/destinationrules were modified, if so we enqueue the
				// removed Service and/or DestinationRules so that the rollouts-pod-template-hash
				// can be cleared from each
				for _, key := range removedKeys("Service", oldRollout, newRollout, serviceutil.GetRolloutServiceKeys) {
					controller.serviceWorkqueue.AddRateLimited(key)
				}
				for _, key := range removedKeys("DestinationRule", oldRollout, newRollout, istioutil.GetRolloutDesinationRuleKeys) {
					controller.IstioController.EnqueueDestinationRule(key)
				}
			}
			controller.enqueueRollout(new)
		},
		DeleteFunc: func(obj interface{}) {
			if ro := unstructuredutil.ObjectToRollout(obj); ro != nil {
				logCtx := logutil.WithRollout(ro)
				logCtx.Info("rollout deleted")
				// Rollout is deleted, queue up the referenced Service and/or DestinationRules so
				// that the rollouts-pod-template-hash can be cleared from each
				for _, s := range serviceutil.GetRolloutServiceKeys(ro) {
					controller.serviceWorkqueue.AddRateLimited(s)
				}
				for _, key := range istioutil.GetRolloutDesinationRuleKeys(ro) {
					controller.IstioController.EnqueueDestinationRule(key)
				}
			}
		},
	})

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
			oldAR := unstructuredutil.ObjectToAnalysisRun(old)
			newAR := unstructuredutil.ObjectToAnalysisRun(new)
			if oldAR == nil || newAR == nil {
				return
			}
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

// removedKeys returns list of indexer keys which have been removed from the old rollout
func removedKeys(name string, old, new *v1alpha1.Rollout, keyFunc func(ro *v1alpha1.Rollout) []string) []string {
	oldKeys := keyFunc(old)
	newKeys := keyFunc(new)
	var removedKeys []string
	for _, oldKey := range oldKeys {
		if !slice.ContainsString(newKeys, oldKey, nil) {
			removedKeys = append(removedKeys, oldKey)
		}
	}
	if len(removedKeys) > 0 {
		logCtx := logutil.WithRollout(old)
		logCtx.Infof("%s index keys removed: %v", name, removedKeys)
	}
	return removedKeys
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

	go c.IstioController.Run(stopCh)

	<-stopCh
	log.Info("Shutting down workers")

	return nil
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Phase block of the Rollout resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	ctx := context.TODO()
	startTime := time.Now()
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}
	rollout, err := c.rolloutsLister.Rollouts(namespace).Get(name)
	if k8serrors.IsNotFound(err) {
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
	logCtx = logutil.WithVersionFields(logCtx, r)
	logCtx.Info("Started syncing rollout")

	if r.ObjectMeta.DeletionTimestamp != nil {
		logCtx.Info("No reconciliation as rollout marked for deletion")
		return nil
	}

	defer func() {
		duration := time.Since(startTime)
		c.metricsServer.IncRolloutReconcile(r, duration)
		logCtx.WithField("time_ms", duration.Seconds()*1e3).Info("Reconciliation completed")
	}()

	resolveErr := c.refResolver.Resolve(r)
	roCtx, err := c.newRolloutContext(r)
	if err != nil {
		logCtx.Errorf("newRolloutContext err %v", err)
		return err
	}
	if resolveErr != nil {
		roCtx.createInvalidRolloutCondition(resolveErr, r)
		return resolveErr
	}

	// In order to work with HPA, the rollout.Spec.Replica field cannot be nil. As a result, the controller will update
	// the rollout to have the replicas field set to the default value. see https://github.com/argoproj/argo-rollouts/issues/119
	if rollout.Spec.Replicas == nil {
		logCtx.Info("Defaulting .spec.replica to 1")
		r.Spec.Replicas = pointer.Int32Ptr(defaults.DefaultReplicas)
		_, err := c.argoprojclientset.ArgoprojV1alpha1().Rollouts(r.Namespace).Update(ctx, r, metav1.UpdateOptions{})
		return err
	}

	err = roCtx.reconcile()
	if roCtx.newRollout != nil {
		c.writeBackToInformer(roCtx.newRollout)
	}
	if err != nil {
		logCtx.Errorf("roCtx.reconcile err %v", err)
	}
	return err
}

// writeBackToInformer writes a just recently updated Rollout back into the informer cache.
// This prevents the situation where the controller operates on a stale rollout and repeats work
func (c *Controller) writeBackToInformer(ro *v1alpha1.Rollout) {
	logCtx := logutil.WithRollout(ro)
	logCtx = logutil.WithVersionFields(logCtx, ro)
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(ro)
	if err != nil {
		logCtx.Errorf("failed to convert rollout to unstructured: %v", err)
		return
	}
	un := unstructured.Unstructured{Object: obj}
	err = c.rolloutsInformer.GetStore().Update(&un)
	if err != nil {
		logCtx.Errorf("failed to update informer store: %v", err)
		return
	}
	logCtx.Info("persisted to informer")
}

func (c *Controller) newRolloutContext(rollout *v1alpha1.Rollout) (*rolloutContext, error) {
	rsList, err := c.getReplicaSetsForRollouts(rollout)
	if err != nil {
		return nil, err
	}

	newRS := replicasetutil.FindNewReplicaSet(rollout, rsList)
	olderRSs := replicasetutil.FindOldReplicaSets(rollout, rsList)
	stableRS := replicasetutil.GetStableRS(rollout, newRS, olderRSs)
	otherRSs := replicasetutil.GetOtherRSs(rollout, newRS, stableRS, rsList)

	exList, err := c.getExperimentsForRollout(rollout)
	if err != nil {
		return nil, err
	}
	currentEx := experimentutil.GetCurrentExperiment(rollout, exList)
	otherExs := experimentutil.GetOldExperiments(rollout, exList)

	arList, err := c.getAnalysisRunsForRollout(rollout)
	if err != nil {
		return nil, err
	}
	currentArs, otherArs := analysisutil.FilterCurrentRolloutAnalysisRuns(arList, rollout)

	logCtx := logutil.WithRollout(rollout)
	roCtx := rolloutContext{
		rollout:    rollout,
		log:        logCtx,
		newRS:      newRS,
		stableRS:   stableRS,
		olderRSs:   olderRSs,
		otherRSs:   otherRSs,
		allRSs:     rsList,
		currentArs: currentArs,
		otherArs:   otherArs,
		currentEx:  currentEx,
		otherExs:   otherExs,
		newStatus: v1alpha1.RolloutStatus{
			RestartedAt: rollout.Status.RestartedAt,
		},
		pauseContext: &pauseContext{
			rollout: rollout,
			log:     logCtx,
		},
		reconcilerBase: c.reconcilerBase,
	}
	return &roCtx, nil
}

func (c *rolloutContext) getRolloutValidationErrors() error {
	rolloutValidationErrors := validation.ValidateRollout(c.rollout)
	if len(rolloutValidationErrors) > 0 {
		return rolloutValidationErrors[0]
	}

	refResources, err := c.getRolloutReferencedResources()
	if err != nil {
		return err
	}

	rolloutValidationErrors = validation.ValidateRolloutReferencedResources(c.rollout, *refResources)
	if len(rolloutValidationErrors) > 0 {
		return rolloutValidationErrors[0]
	}
	return nil
}

func (c *rolloutContext) createInvalidRolloutCondition(validationError error, r *v1alpha1.Rollout) error {
	prevCond := conditions.GetRolloutCondition(r.Status, v1alpha1.InvalidSpec)
	invalidSpecCond := prevCond
	errorMessage := fmt.Sprintf("The Rollout \"%s\" is invalid: %s", r.Name, validationError.Error())
	if prevCond == nil || prevCond.Message != errorMessage {
		invalidSpecCond = conditions.NewRolloutCondition(v1alpha1.InvalidSpec, corev1.ConditionTrue, conditions.InvalidSpecReason, errorMessage)
	}
	c.log.Error(errorMessage)
	if r.Status.ObservedGeneration != strconv.Itoa(int(r.Generation)) || !reflect.DeepEqual(invalidSpecCond, prevCond) {
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

func (c *rolloutContext) getRolloutReferencedResources() (*validation.ReferencedResources, error) {
	refResources := validation.ReferencedResources{}
	services, err := c.getReferencedServices()
	if err != nil {
		return nil, err
	}
	refResources.ServiceWithType = *services

	analysisTemplates, err := c.getReferencedRolloutAnalyses()
	if err != nil {
		return nil, err
	}
	refResources.AnalysisTemplatesWithType = *analysisTemplates

	ingresses, err := c.getReferencedIngresses()
	if err != nil {
		return nil, err
	}
	refResources.Ingresses = *ingresses

	virtualServices, err := c.IstioController.GetReferencedVirtualServices(c.rollout)
	if err != nil {
		return nil, err
	}
	refResources.VirtualServices = *virtualServices

	ambassadorMappings, err := c.getAmbassadorMappings()
	if err != nil {
		return nil, err
	}
	refResources.AmbassadorMappings = ambassadorMappings

	return &refResources, nil
}

func (c *rolloutContext) getAmbassadorMappings() ([]unstructured.Unstructured, error) {
	mappings := []unstructured.Unstructured{}
	if c.rollout.Spec.Strategy.Canary != nil {
		canary := c.rollout.Spec.Strategy.Canary
		if canary.TrafficRouting != nil && canary.TrafficRouting.Ambassador != nil {
			a := canary.TrafficRouting.Ambassador
			fldPath := field.NewPath("spec", "strategy", "canary", "trafficRouting", "ambassador", "mappings")
			if len(a.Mappings) == 0 {
				return nil, field.Invalid(fldPath, nil, "must provide at least one mapping")
			}
			for _, mappingName := range a.Mappings {
				mapping, err := c.dynamicclientset.Resource(ambassador.GetMappingGVR()).
					Namespace(c.rollout.Namespace).
					Get(context.Background(), mappingName, metav1.GetOptions{})
				if err != nil {
					if k8serrors.IsNotFound(err) {
						return nil, field.Invalid(fldPath, mappingName, err.Error())
					}
					return nil, err
				}
				mappings = append(mappings, *mapping)
			}
		}
	}
	return mappings, nil
}

func (c *rolloutContext) getReferencedServices() (*[]validation.ServiceWithType, error) {
	services := []validation.ServiceWithType{}
	if c.rollout.Spec.Strategy.BlueGreen != nil {
		if c.rollout.Spec.Strategy.BlueGreen.ActiveService != "" {
			activeSvc, err := c.servicesLister.Services(c.rollout.Namespace).Get(c.rollout.Spec.Strategy.BlueGreen.ActiveService)
			if k8serrors.IsNotFound(err) {
				fldPath := validation.GetServiceWithTypeFieldPath(validation.ActiveService)
				return nil, field.Invalid(fldPath, c.rollout.Spec.Strategy.BlueGreen.ActiveService, err.Error())
			}
			if err != nil {
				return nil, err
			}
			services = append(services, validation.ServiceWithType{
				Service: activeSvc,
				Type:    validation.ActiveService,
			})
		}
		if c.rollout.Spec.Strategy.BlueGreen.PreviewService != "" {
			previewSvc, err := c.servicesLister.Services(c.rollout.Namespace).Get(c.rollout.Spec.Strategy.BlueGreen.PreviewService)
			if k8serrors.IsNotFound(err) {
				fldPath := validation.GetServiceWithTypeFieldPath(validation.PreviewService)
				return nil, field.Invalid(fldPath, c.rollout.Spec.Strategy.BlueGreen.PreviewService, err.Error())
			}
			if err != nil {
				return nil, err
			}
			services = append(services, validation.ServiceWithType{
				Service: previewSvc,
				Type:    validation.PreviewService,
			})
		}
	} else if c.rollout.Spec.Strategy.Canary != nil {
		if c.rollout.Spec.Strategy.Canary.StableService != "" {
			stableSvc, err := c.servicesLister.Services(c.rollout.Namespace).Get(c.rollout.Spec.Strategy.Canary.StableService)
			if k8serrors.IsNotFound(err) {
				fldPath := validation.GetServiceWithTypeFieldPath(validation.StableService)
				return nil, field.Invalid(fldPath, c.rollout.Spec.Strategy.Canary.StableService, err.Error())
			}
			if err != nil {
				return nil, err
			}
			services = append(services, validation.ServiceWithType{
				Service: stableSvc,
				Type:    validation.StableService,
			})
		}
		if c.rollout.Spec.Strategy.Canary.CanaryService != "" {
			canarySvc, err := c.servicesLister.Services(c.rollout.Namespace).Get(c.rollout.Spec.Strategy.Canary.CanaryService)
			if k8serrors.IsNotFound(err) {
				fldPath := validation.GetServiceWithTypeFieldPath(validation.CanaryService)
				return nil, field.Invalid(fldPath, c.rollout.Spec.Strategy.Canary.CanaryService, err.Error())
			}
			if err != nil {
				return nil, err
			}
			services = append(services, validation.ServiceWithType{
				Service: canarySvc,
				Type:    validation.CanaryService,
			})
		}
	}
	return &services, nil
}

func (c *rolloutContext) getReferencedRolloutAnalyses() (*[]validation.AnalysisTemplatesWithType, error) {
	analysisTemplates := make([]validation.AnalysisTemplatesWithType, 0)
	if c.rollout.Spec.Strategy.BlueGreen != nil {
		blueGreen := c.rollout.Spec.Strategy.BlueGreen
		if blueGreen.PrePromotionAnalysis != nil {
			// CanaryStepIndex will be ignored
			templates, err := c.getReferencedAnalysisTemplates(c.rollout, blueGreen.PrePromotionAnalysis, validation.PrePromotionAnalysis, 0)
			if err != nil {
				return nil, err
			}
			templates.Args = blueGreen.PrePromotionAnalysis.Args
			analysisTemplates = append(analysisTemplates, *templates)
		}

		if blueGreen.PostPromotionAnalysis != nil {
			// CanaryStepIndex will be ignored
			templates, err := c.getReferencedAnalysisTemplates(c.rollout, blueGreen.PostPromotionAnalysis, validation.PostPromotionAnalysis, 0)
			if err != nil {
				return nil, err
			}
			templates.Args = blueGreen.PostPromotionAnalysis.Args
			analysisTemplates = append(analysisTemplates, *templates)
		}
	} else if c.rollout.Spec.Strategy.Canary != nil {
		canary := c.rollout.Spec.Strategy.Canary
		for i, step := range canary.Steps {
			if step.Analysis != nil {
				templates, err := c.getReferencedAnalysisTemplates(c.rollout, step.Analysis, validation.InlineAnalysis, i)
				if err != nil {
					return nil, err
				}
				templates.Args = step.Analysis.Args
				analysisTemplates = append(analysisTemplates, *templates)
			}
		}
		if canary.Analysis != nil {
			templates, err := c.getReferencedAnalysisTemplates(c.rollout, &canary.Analysis.RolloutAnalysis, validation.BackgroundAnalysis, 0)
			if err != nil {
				return nil, err
			}
			templates.Args = canary.Analysis.Args
			analysisTemplates = append(analysisTemplates, *templates)
		}
	}
	return &analysisTemplates, nil
}

func (c *rolloutContext) getReferencedAnalysisTemplates(rollout *v1alpha1.Rollout, rolloutAnalysis *v1alpha1.RolloutAnalysis, templateType validation.AnalysisTemplateType, canaryStepIndex int) (*validation.AnalysisTemplatesWithType, error) {
	templates := make([]*v1alpha1.AnalysisTemplate, 0)
	clusterTemplates := make([]*v1alpha1.ClusterAnalysisTemplate, 0)
	fldPath := validation.GetAnalysisTemplateWithTypeFieldPath(templateType, canaryStepIndex)

	for _, templateRef := range rolloutAnalysis.Templates {
		if templateRef.ClusterScope {
			template, err := c.clusterAnalysisTemplateLister.Get(templateRef.TemplateName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					return nil, field.Invalid(fldPath, templateRef.TemplateName, fmt.Sprintf("ClusterAnalysisTemplate '%s' not found", templateRef.TemplateName))
				}
				return nil, err
			}
			clusterTemplates = append(clusterTemplates, template)
		} else {
			template, err := c.analysisTemplateLister.AnalysisTemplates(c.rollout.Namespace).Get(templateRef.TemplateName)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					return nil, field.Invalid(fldPath, templateRef.TemplateName, fmt.Sprintf("AnalysisTemplate '%s' not found", templateRef.TemplateName))
				}
				return nil, err
			}
			templates = append(templates, template)
		}
	}

	return &validation.AnalysisTemplatesWithType{
		AnalysisTemplates:        templates,
		ClusterAnalysisTemplates: clusterTemplates,
		TemplateType:             templateType,
		CanaryStepIndex:          canaryStepIndex,
	}, nil
}

func (c *rolloutContext) getReferencedIngresses() (*[]v1beta1.Ingress, error) {
	ingresses := []v1beta1.Ingress{}
	canary := c.rollout.Spec.Strategy.Canary
	fldPath := field.NewPath("spec", "strategy", "canary", "trafficRouting")
	if canary != nil && canary.TrafficRouting != nil {
		if canary.TrafficRouting.ALB != nil {
			ingress, err := c.ingressesLister.Ingresses(c.rollout.Namespace).Get(canary.TrafficRouting.ALB.Ingress)
			if k8serrors.IsNotFound(err) {
				return nil, field.Invalid(fldPath.Child("alb", "ingress"), canary.TrafficRouting.ALB.Ingress, err.Error())
			}
			if err != nil {
				return nil, err
			}
			ingresses = append(ingresses, *ingress)
		} else if canary.TrafficRouting.Nginx != nil {
			ingress, err := c.ingressesLister.Ingresses(c.rollout.Namespace).Get(canary.TrafficRouting.Nginx.StableIngress)
			if k8serrors.IsNotFound(err) {
				return nil, field.Invalid(fldPath.Child("nginx", "stableIngress"), canary.TrafficRouting.Nginx.StableIngress, err.Error())
			}
			if err != nil {
				return nil, err
			}
			ingresses = append(ingresses, *ingress)
		}
	}
	return &ingresses, nil
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
