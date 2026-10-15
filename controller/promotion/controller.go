package promotion

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/controller/promotion/traffic"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	rollouttyped "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/typed/rollouts/v1alpha1"
	rolloutundo "github.com/argoproj/argo-rollouts/pkg/kubectl-argo-rollouts/cmd/undo"
	"github.com/argoproj/argo-rollouts/utils/annotations"
)

const (
	KindRolloutPromotion = "RolloutPromotion"

	PhasePending   = "Pending"
	PhaseRunning   = "Running"
	PhaseSucceeded = "Succeeded"
	PhaseFailed    = "Failed"

	ConditionCompleted = "Completed"

	defaultMode         = "together"
	defaultAction       = "promote"
	defaultStrategy     = "auto"
	defaultWaitEnabled  = true
	defaultWaitAttempts = 180
	defaultWaitSeconds  = 5
	rollbackPollPeriod  = time.Second
	legacyTrafficTarget = "__legacy_deployment__"

	rolloutHashLabel = v1alpha1.DefaultRolloutUniqueLabelKey
)

type trafficRollbackRequestedError struct {
	requestID string
}

func (e *trafficRollbackRequestedError) Error() string {
	return fmt.Sprintf("traffic rollback request %q interrupted the forward promotion", e.requestID)
}

type preparationFailedError struct {
	err error
}

func (e *preparationFailedError) Error() string {
	return e.err.Error()
}

type Target struct {
	Name                      string
	Namespace                 string
	Strategy                  string
	ActiveService             string
	PreviewService            string
	ExpectedRolloutUID        string
	ExpectedPodTemplateHash   string
	Readiness                 *v1alpha1.RolloutPromotionReadiness
	ExpectedCurrentPodHash    string
	ExpectedStableRS          string
	ExpectedActiveServiceHash string
	ExpectedCurrentStepIndex  *int32
	ToRevision                int64
	RollbackTarget            string
	WeightSwitch              *WeightSwitch
	Traffic                   *v1alpha1.RolloutPromotionTraffic
	TrafficSnapshot           *v1alpha1.RolloutPromotionTrafficState
}

type WeightSwitch struct {
	Enabled             bool
	ApplyOnPromotion    bool
	ApplyOnPromotionSet bool
	AnnotationKey       string
	BaseSelector        string
	ActiveWeight        string
	InactiveWeight      string
	ReapplyDelaySeconds int
}

type WaitOptions struct {
	Enabled  bool
	Attempts int
	Seconds  int
}

type TrafficRollbackOptions struct {
	Enabled      bool
	Mode         string
	HistoryLimit int
}

type RequestSpec struct {
	RequestID              string
	Mode                   string
	Action                 string
	Strategy               string
	Targets                []Target
	Wait                   WaitOptions
	CanaryCompletionPolicy string
	TrafficRollback        TrafficRollbackOptions
}

type Controller struct {
	namespace string

	kubeClient       kubernetes.Interface
	dynamicClient    dynamic.Interface
	rolloutsClient   rolloutclient.Interface
	informer         cache.SharedIndexInformer
	queue            workqueue.RateLimitingInterface
	trafficProviders traffic.Registry
}

type Option func(*Controller)

// WithTrafficProviders supplies compiled-in adapters without changing the
// orchestration state machine. The caller must not mutate the registry afterward.
func WithTrafficProviders(providers traffic.Registry) Option {
	return func(c *Controller) {
		c.trafficProviders = traffic.Registry{}
		for name, provider := range providers {
			c.trafficProviders[name] = provider
		}
	}
}

func NewController(namespace string, kubeClient kubernetes.Interface, dynamicClient dynamic.Interface, rolloutsClient rolloutclient.Interface, informer cache.SharedIndexInformer, options ...Option) *Controller {
	c := &Controller{
		namespace:        namespace,
		kubeClient:       kubeClient,
		dynamicClient:    dynamicClient,
		rolloutsClient:   rolloutsClient,
		informer:         informer,
		queue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "rolloutpromotions"),
		trafficProviders: traffic.NewRegistry(kubeClient),
	}

	for _, option := range options {
		option(c)
	}
	_, _ = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: c.enqueue,
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldPromotion, oldOK := oldObj.(*v1alpha1.RolloutPromotion)
			newPromotion, newOK := newObj.(*v1alpha1.RolloutPromotion)
			if !oldOK || !newOK {
				c.enqueue(newObj)
				return
			}
			oldRollback := strings.TrimSpace(oldPromotion.GetAnnotations()[annotations.TrafficRollbackRequestAnnotation])
			newRollback := strings.TrimSpace(newPromotion.GetAnnotations()[annotations.TrafficRollbackRequestAnnotation])
			if oldPromotion.GetGeneration() != newPromotion.GetGeneration() || oldRollback != newRollback || newPromotion.GetDeletionTimestamp() != nil {
				c.enqueue(newObj)
			}
		},
	})

	return c
}

func (c *Controller) Run(ctx context.Context, workers int) error {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	log.Infof("Starting RolloutPromotion controller with %d worker(s)", workers)
	go c.informer.Run(ctx.Done())

	if ok := cache.WaitForCacheSync(ctx.Done(), c.informer.HasSynced); !ok {
		return fmt.Errorf("failed to sync RolloutPromotion cache")
	}

	for i := 0; i < workers; i++ {
		go wait.UntilWithContext(ctx, c.runWorker, time.Second)
	}

	<-ctx.Done()
	log.Info("Stopping RolloutPromotion controller")
	return nil
}

func (c *Controller) enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.queue.Add(key)
}

func (c *Controller) runWorker(ctx context.Context) {
	for c.processNextWorkItem(ctx) {
	}
}

func (c *Controller) processNextWorkItem(ctx context.Context) bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	if err := c.reconcile(ctx, key.(string)); err != nil {
		utilruntime.HandleError(fmt.Errorf("error syncing RolloutPromotion %q: %w", key, err))
		c.queue.AddRateLimited(key)
		return true
	}

	c.queue.Forget(key)
	return true
}

func (c *Controller) reconcile(ctx context.Context, key string) error {
	obj, exists, err := c.informer.GetIndexer().GetByKey(key)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	cachedPromotion, ok := obj.(*v1alpha1.RolloutPromotion)
	if !ok {
		return fmt.Errorf("expected *v1alpha1.RolloutPromotion but got %T", obj)
	}
	// Status updates generated during a reconcile also enqueue this key. Read
	// the live object before making idempotency decisions so a stale informer
	// snapshot cannot replay a request that the previous reconcile completed.
	promotion, err := c.promotionClient(cachedPromotion.GetNamespace()).Get(ctx, cachedPromotion.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if promotion.GetDeletionTimestamp() != nil {
		return nil
	}

	spec, err := normalizeSpec(promotion.Spec)
	if err == nil {
		err = validateTargetNamespaces(promotion.Namespace, spec.Targets)
	}
	if err != nil {
		_ = c.patchStatus(ctx, promotion, failedStatus(promotion, err.Error()))
		return nil
	}
	if spec.RequestID == "" {
		spec.RequestID = fmt.Sprintf("generation-%d", promotion.GetGeneration())
	}
	rollbackRequest := strings.TrimSpace(promotion.GetAnnotations()[annotations.TrafficRollbackRequestAnnotation])
	rollbackPending := spec.TrafficRollback.Enabled && rollbackRequest != "" && rollbackRequest != promotion.Status.LastTrafficRollbackRequest
	// Older controllers could persist the request marker before verification.
	// A committed, unfinished rollback must still resume after a restart.
	if operation := promotion.Status.Operation; spec.TrafficRollback.Enabled && rollbackRequest != "" && operation != nil && operation.Kind == v1alpha1.RolloutPromotionOperationTrafficRollback && operation.RequestID == rollbackRequest && !isTerminalOperationPhase(operation.Phase) {
		rollbackPending = true
	}
	operationID, operationKind, operationRequestID := desiredOperation(spec, rollbackRequest, rollbackPending)
	operation := promotion.Status.Operation
	if operation != nil && operation.ID == operationID && isTerminalOperationPhase(operation.Phase) {
		if err := c.reconcileTerminalForwardLegacyAdoption(ctx, promotion); err != nil {
			return err
		}
		if promotion.Status.ObservedGeneration != promotion.GetGeneration() {
			return c.patchStatus(ctx, promotion, map[string]interface{}{"observedGeneration": promotion.GetGeneration()})
		}
		return nil
	}
	// Backward compatibility for objects completed before operation checkpoints
	// were introduced.
	if operation == nil && !rollbackPending && promotion.Status.ObservedGeneration == promotion.GetGeneration() && isTerminalPhase(promotion.Status.Phase) {
		return nil
	}
	if !rollbackPending && containsString(promotion.Status.ProcessedRequestIDs, spec.RequestID) {
		return c.patchStatus(ctx, promotion, map[string]interface{}{"observedGeneration": promotion.GetGeneration()})
	}
	if operation != nil && operation.ID != operationID && !isTerminalOperationPhase(operation.Phase) && !rollbackPending && (operation.CommitTime != nil || operation.Phase != v1alpha1.RolloutPromotionOperationPrepared) {
		status := statusMap(promotion)
		status["phase"] = PhaseRunning
		status["conditions"] = conditions("Unknown", "OperationConflict", fmt.Sprintf("operation %s is still %s; refusing to start %s", operation.ID, operation.Phase, operationID))
		setOperation(status, operation.ID, operation.RequestID, operation.Kind, operation.Action, v1alpha1.RolloutPromotionOperationStalled, operation.ObservedGeneration, operation.Attempt, operation.CommitTime, "a different request was received while the committed operation is unfinished")
		if patchErr := c.patchStatus(ctx, promotion, status); patchErr != nil {
			return patchErr
		}
		return nil
	}

	log.WithFields(log.Fields{
		"namespace":       promotion.GetNamespace(),
		"name":            promotion.GetName(),
		"requestID":       spec.RequestID,
		"mode":            spec.Mode,
		"action":          spec.Action,
		"strategy":        spec.Strategy,
		"targets":         targetNames(spec.Targets),
		"trafficRollback": rollbackPending,
	}).Info("Reconciling RolloutPromotion")

	if !rollbackPending && (spec.Action == "promote" || spec.Action == "promoteFull") {
		alreadyPromoted, states, observeErr := c.forwardPromotionAlreadyComplete(ctx, promotion.GetNamespace(), spec.Targets)
		if observeErr == nil && alreadyPromoted {
			return c.completeAlreadyPromoted(ctx, promotion, spec, operationID, operationKind, operationRequestID, states)
		}
	}

	if operation == nil || operation.ID != operationID || isTerminalOperationPhase(operation.Phase) || operation.CommitTime == nil {
		promotion, err = c.commitOperation(ctx, promotion, spec, operationID, operationKind, operationRequestID, rollbackPending)
		if err != nil {
			var waiting *readinessPendingError
			if errors.As(err, &waiting) {
				c.queue.AddAfter(key, time.Duration(spec.Wait.Seconds)*time.Second)
				return nil
			}
			var preparationErr *preparationFailedError
			if errors.As(err, &preparationErr) {
				return nil
			}
			return err
		}
		operation = promotion.Status.Operation
	}

	status := statusMap(promotion)
	attempt := int32(1)
	var commitTime *metav1.Time
	if operation != nil {
		attempt = operation.Attempt + 1
		commitTime = operation.CommitTime
	}
	status["phase"] = PhaseRunning
	status["completionTime"] = nil
	status["conditions"] = conditions("False", "OperationApplying", fmt.Sprintf("committed operation %s is applying", operationID))
	setOperation(status, operationID, operationRequestID, operationKind, spec.Action, v1alpha1.RolloutPromotionOperationApplying, promotion.GetGeneration(), attempt, commitTime, "applying committed operation")
	if err = c.patchStatus(ctx, promotion, status); err != nil {
		return err
	}
	promotion, err = c.promotionClient(promotion.GetNamespace()).Get(ctx, promotion.GetName(), metav1.GetOptions{})
	if err != nil {
		return err
	}
	status = statusMap(promotion)

	if rollbackPending {
		status, err = c.executePreviousTrafficSnapshot(ctx, promotion, spec, status)
	} else {
		if spec.Action == "promote" || spec.Action == "promoteFull" {
			for i := range spec.Targets {
				if spec.Targets[i].Readiness != nil {
					if checkpoint := targetCheckpoint(promotion, spec.Targets[i]); checkpoint != nil {
						spec.Targets[i].ExpectedCurrentPodHash = checkpoint.BaselineCurrentPodHash
					}
				}
			}
			err = c.checkCommittedIdentity(ctx, promotion.GetNamespace(), spec.Targets)
			if err == nil {
				err = c.clearTrafficOverrides(ctx, promotion.GetNamespace(), spec.Targets)
			}
			if err == nil {
				status, err = c.execute(ctx, promotion, spec, status)
			}
		} else {
			status, err = c.execute(ctx, promotion, spec, status)
		}
	}

	var interruptedRollback *trafficRollbackRequestedError
	if errors.As(err, &interruptedRollback) {
		status["phase"] = PhaseRunning
		status["conditions"] = conditions("Unknown", "OperationInterrupted", interruptedRollback.Error())
		setOperation(status, operationID, operationRequestID, operationKind, spec.Action, v1alpha1.RolloutPromotionOperationStalled, promotion.GetGeneration(), attempt, commitTime, interruptedRollback.Error())
		return c.patchStatus(ctx, promotion, status)
	}
	if err != nil {
		status["phase"] = PhaseRunning
		status["completionTime"] = nil
		status["conditions"] = conditions("Unknown", "OperationStalled", err.Error())
		setOperation(status, operationID, operationRequestID, operationKind, spec.Action, v1alpha1.RolloutPromotionOperationStalled, promotion.GetGeneration(), attempt, commitTime, err.Error())
		if patchErr := c.patchStatus(ctx, promotion, status); patchErr != nil {
			return patchErr
		}
		return err
	}

	setOperation(status, operationID, operationRequestID, operationKind, spec.Action, v1alpha1.RolloutPromotionOperationVerifying, promotion.GetGeneration(), attempt, commitTime, "all target actions applied; persisting verification checkpoint")
	if err = c.patchStatus(ctx, promotion, status); err != nil {
		return err
	}
	status["phase"] = PhaseSucceeded
	status["completionTime"] = metav1.Now().Format(time.RFC3339)
	if rollbackPending {
		status["lastTrafficRollbackRequest"] = rollbackRequest
		status["conditions"] = conditions("True", "TrafficRollbackCompleted", "traffic was restored to the previous retained snapshot")
	} else {
		status["conditions"] = conditions("True", "PromotionCompleted", fmt.Sprintf("rollout action %s completed for all targets", spec.Action))
	}
	status["processedRequestIDs"] = appendProcessedRequest(promotion.Status.ProcessedRequestIDs, spec.RequestID)
	setOperation(status, operationID, operationRequestID, operationKind, spec.Action, v1alpha1.RolloutPromotionOperationSucceeded, promotion.GetGeneration(), attempt, commitTime, "operation completed")
	return c.patchStatus(ctx, promotion, status)
}

func desiredOperation(spec RequestSpec, rollbackRequest string, rollbackPending bool) (string, string, string) {
	if rollbackPending {
		return "traffic-rollback/" + rollbackRequest, v1alpha1.RolloutPromotionOperationTrafficRollback, rollbackRequest
	}
	return "forward/" + spec.RequestID, v1alpha1.RolloutPromotionOperationForward, spec.RequestID
}

func (c *Controller) forwardPromotionAlreadyComplete(ctx context.Context, defaultNamespace string, targets []Target) (bool, []map[string]interface{}, error) {
	if len(targets) == 0 {
		return false, nil, nil
	}
	states := make([]map[string]interface{}, 0, len(targets))
	for _, target := range targets {
		namespace := target.Namespace
		if namespace == "" {
			namespace = defaultNamespace
		}
		state, err := c.observeTarget(ctx, namespace, target)
		if err != nil {
			return false, nil, err
		}
		if err := validateRequestedStrategy(target.Strategy, state); err != nil {
			return false, nil, err
		}
		phase, _ := state["rolloutPhase"].(string)
		trafficOverrideHash, _ := state["trafficOverrideHash"].(string)
		activeService, _ := state["activeService"].(string)
		activeServiceHash, _ := state["activeServiceHash"].(string)
		currentHash, _ := state["currentPodHash"].(string)
		serviceConverged := activeService == "" || (activeServiceHash != "" && activeServiceHash == currentHash)
		if phase != "Healthy" || trafficOverrideHash != "" || !serviceConverged || !targetAlreadyPromotedToExpected(target, state) {
			return false, states, nil
		}
		states = append(states, state)
	}
	return true, states, nil
}

func (c *Controller) completeAlreadyPromoted(ctx context.Context, promotion *v1alpha1.RolloutPromotion, spec RequestSpec, operationID, operationKind, operationRequestID string, states []map[string]interface{}) error {
	message := fmt.Sprintf("all target Rollouts are already promoted with no pending revision: %s", strings.Join(targetNames(spec.Targets), ", "))
	log.WithFields(log.Fields{
		"namespace": promotion.GetNamespace(),
		"name":      promotion.GetName(),
		"requestID": spec.RequestID,
		"targets":   targetNames(spec.Targets),
	}).Warn(message)

	now := metav1.Now()
	status := statusMap(promotion)
	status["observedGeneration"] = promotion.GetGeneration()
	status["phase"] = PhaseSucceeded
	status["startTime"] = now.Format(time.RFC3339)
	status["completionTime"] = now.Format(time.RFC3339)
	status["conditions"] = conditions("True", "AlreadyPromoted", message)
	status["targets"] = []interface{}{}
	for i, target := range spec.Targets {
		state := map[string]interface{}{}
		if i < len(states) {
			for key, value := range states[i] {
				state[key] = value
			}
		}
		state["actionAlreadyComplete"] = true
		state["actionApplied"] = false
		status = upsertTargetStatus(status, target, PhaseSucceeded, state, nil)
	}
	status["processedRequestIDs"] = appendProcessedRequest(promotion.Status.ProcessedRequestIDs, spec.RequestID)
	setOperation(status, operationID, operationRequestID, operationKind, spec.Action, v1alpha1.RolloutPromotionOperationSucceeded, promotion.GetGeneration(), 0, nil, message)
	return c.patchStatus(ctx, promotion, status)
}

func (c *Controller) commitOperation(ctx context.Context, promotion *v1alpha1.RolloutPromotion, spec RequestSpec, operationID, operationKind, operationRequestID string, rollbackPending bool) (*v1alpha1.RolloutPromotion, error) {
	status := statusMap(promotion)
	status["observedGeneration"] = promotion.GetGeneration()
	status["phase"] = PhaseRunning
	if promotion.Status.Operation == nil || promotion.Status.Operation.ID != operationID || promotion.Status.StartTime == nil {
		status["startTime"] = metav1.Now().Format(time.RFC3339)
	}
	status["completionTime"] = nil
	status["conditions"] = conditions("False", "OperationPreparing", fmt.Sprintf("validating operation %s before commit", operationID))
	status["targets"] = targetStatuses(spec.Targets, v1alpha1.RolloutPromotionOperationPrepared, "")
	setOperation(status, operationID, operationRequestID, operationKind, spec.Action, v1alpha1.RolloutPromotionOperationPrepared, promotion.GetGeneration(), 0, nil, "validating targets before commit")

	var err error
	if rollbackPending {
		var captured bool
		promotion, status, captured, err = c.ensureTrafficSnapshotForRollback(ctx, promotion, spec, status)
		_ = captured
	} else {
		if spec.Action == "promote" || spec.Action == "promoteFull" {
			err = c.checkForwardReadiness(ctx, promotion.GetNamespace(), spec)
		}
		if err == nil && spec.TrafficRollback.Enabled && (spec.Action == "promote" || spec.Action == "promoteFull") {
			status, err = c.captureTrafficSnapshot(ctx, promotion, spec, status)
		}
		if err == nil {
			if spec.Action == "promote" || spec.Action == "promoteFull" {
				err = c.preflightForwardPromotion(ctx, promotion.GetNamespace(), spec.Targets)
			} else {
				err = c.preflightControlAction(ctx, promotion.GetNamespace(), spec)
			}
		}
	}
	if err == nil {
		for _, target := range spec.Targets {
			namespace := target.Namespace
			if namespace == "" {
				namespace = promotion.GetNamespace()
			}
			state, observeErr := c.observeTarget(ctx, namespace, target)
			if observeErr != nil {
				err = observeErr
				break
			}
			state["baselineCurrentPodHash"] = state["currentPodHash"]
			state["previousStepIndex"] = state["currentStepIndex"]
			if spec.Action == "rollback" {
				resolved, resolveErr := c.resolveRollbackRevision(ctx, namespace, target, state)
				if resolveErr != nil {
					err = resolveErr
					break
				}
				state["resolvedToRevision"] = resolved
			}
			status = upsertTargetStatus(status, target, v1alpha1.RolloutPromotionOperationPrepared, state, nil)
		}
	}
	if err != nil {
		var waiting *readinessPendingError
		if errors.As(err, &waiting) {
			started, _ := time.Parse(time.RFC3339, status["startTime"].(string))
			if time.Since(started) < time.Duration(spec.Wait.Attempts)*time.Duration(spec.Wait.Seconds)*time.Second {
				status["phase"] = PhasePending
				status["conditions"] = conditions("False", "WaitingForReadiness", err.Error())
				setOperation(status, operationID, operationRequestID, operationKind, spec.Action, v1alpha1.RolloutPromotionOperationPrepared, promotion.GetGeneration(), 0, nil, err.Error())
				if patchErr := c.patchStatus(ctx, promotion, status); patchErr != nil {
					return promotion, patchErr
				}
				return promotion, waiting
			}
			err = fmt.Errorf("readiness deadline exceeded: %w", err)
		}
		status["phase"] = PhaseFailed
		status["completionTime"] = metav1.Now().Format(time.RFC3339)
		status["conditions"] = conditions("False", "OperationPreparationFailed", err.Error())
		if !rollbackPending {
			status["processedRequestIDs"] = appendProcessedRequest(promotion.Status.ProcessedRequestIDs, spec.RequestID)
		}
		setOperation(status, operationID, operationRequestID, operationKind, spec.Action, v1alpha1.RolloutPromotionOperationFailed, promotion.GetGeneration(), 0, nil, err.Error())
		if patchErr := c.patchStatus(ctx, promotion, status); patchErr != nil {
			return promotion, patchErr
		}
		return promotion, &preparationFailedError{err: err}
	}

	commitTime := metav1.Now()
	status["conditions"] = conditions("False", "OperationCommitted", fmt.Sprintf("operation %s is durably committed", operationID))
	setOperation(status, operationID, operationRequestID, operationKind, spec.Action, v1alpha1.RolloutPromotionOperationCommitted, promotion.GetGeneration(), 0, &commitTime, "operation committed before target mutation")
	if err := c.patchStatus(ctx, promotion, status); err != nil {
		return promotion, err
	}
	live, err := c.promotionClient(promotion.GetNamespace()).Get(ctx, promotion.GetName(), metav1.GetOptions{})
	if err != nil {
		return promotion, err
	}
	return live, nil
}

func statusMap(promotion *v1alpha1.RolloutPromotion) map[string]interface{} {
	status := map[string]interface{}{}
	if promotion == nil {
		return status
	}
	raw, err := json.Marshal(promotion.Status)
	if err != nil {
		return status
	}
	_ = json.Unmarshal(raw, &status)
	return status
}

func setOperation(status map[string]interface{}, id, requestID, kind, action, phase string, generation int64, attempt int32, commitTime *metav1.Time, message string) {
	now := metav1.Now()
	operation := map[string]interface{}{
		"id":                 id,
		"requestID":          requestID,
		"kind":               kind,
		"action":             action,
		"observedGeneration": generation,
		"phase":              phase,
		"attempt":            attempt,
		"lastTransitionTime": now.Format(time.RFC3339),
		"message":            message,
	}
	if commitTime != nil {
		operation["commitTime"] = commitTime.Format(time.RFC3339)
	}
	if isTerminalOperationPhase(phase) {
		operation["completionTime"] = now.Format(time.RFC3339)
	}
	status["operation"] = operation
}

func isTerminalOperationPhase(phase string) bool {
	return phase == v1alpha1.RolloutPromotionOperationSucceeded || phase == v1alpha1.RolloutPromotionOperationFailed
}

func (c *Controller) ensureTrafficSnapshotForRollback(ctx context.Context, promotion *v1alpha1.RolloutPromotion, spec RequestSpec, status map[string]interface{}) (*v1alpha1.RolloutPromotion, map[string]interface{}, bool, error) {
	if len(promotion.Status.TrafficSnapshots) > 0 {
		return promotion, status, false, nil
	}
	if spec.Action != "promote" && spec.Action != "promoteFull" {
		return promotion, status, false, fmt.Errorf("RolloutPromotion %s/%s has no pre-promotion traffic snapshot", promotion.GetNamespace(), promotion.GetName())
	}

	updatedStatus, err := c.captureTrafficSnapshot(ctx, promotion, spec, status)
	if err != nil {
		return promotion, updatedStatus, false, fmt.Errorf("capture initial traffic baseline for rollback: %w", err)
	}
	snapshots, ok := updatedStatus["trafficSnapshots"].([]v1alpha1.RolloutPromotionTrafficSnapshot)
	if !ok || len(snapshots) == 0 {
		return promotion, updatedStatus, false, fmt.Errorf("capture initial traffic baseline for rollback produced no snapshot")
	}
	withSnapshot := promotion.DeepCopy()
	withSnapshot.Status.TrafficSnapshots = snapshots
	return withSnapshot, updatedStatus, true, nil
}

func (c *Controller) execute(ctx context.Context, promotion *v1alpha1.RolloutPromotion, spec RequestSpec, status map[string]interface{}) (map[string]interface{}, error) {
	if spec.Action != defaultAction {
		return c.executeControlAction(ctx, promotion, spec, status)
	}

	switch spec.Mode {
	case "together":
		initialStates := make([]map[string]interface{}, len(spec.Targets))
		for i, target := range spec.Targets {
			state, err := c.resumePromoteTarget(ctx, promotion, target)
			initialStates[i] = state
			status = upsertTargetStatus(status, target, promotedTargetPhase(err), state, err)
			_ = c.patchStatus(ctx, promotion, status)
			if err != nil {
				return status, err
			}
		}
		if spec.Wait.Enabled {
			for i, target := range spec.Targets {
				state, err := c.waitTarget(ctx, promotion, promotion.GetNamespace(), target, spec.Wait, initialStates[i])
				if err == nil {
					state, err = c.completeCanaryAfterFullWeight(ctx, promotion, spec, target, state)
				}
				if err == nil {
					state, err = c.applyTargetTraffic(ctx, promotion.GetNamespace(), target, state)
				}
				status = upsertTargetStatus(status, target, terminalTargetPhase(err), state, err)
				_ = c.patchStatus(ctx, promotion, status)
				if err != nil {
					return status, err
				}
			}
		} else {
			for _, target := range spec.Targets {
				namespace := target.Namespace
				if namespace == "" {
					namespace = promotion.GetNamespace()
				}
				state, err := c.observeTarget(ctx, namespace, target)
				if err == nil {
					state, err = c.applyTargetTraffic(ctx, promotion.GetNamespace(), target, state)
				}
				status = upsertTargetStatus(status, target, terminalTargetPhase(err), state, err)
				_ = c.patchStatus(ctx, promotion, status)
				if err != nil {
					return status, err
				}
			}
		}
	case "sequential":
		for _, target := range spec.Targets {
			state, err := c.resumePromoteTarget(ctx, promotion, target)
			status = upsertTargetStatus(status, target, promotedTargetPhase(err), state, err)
			_ = c.patchStatus(ctx, promotion, status)
			if err != nil {
				return status, err
			}
			if spec.Wait.Enabled {
				state, err = c.waitTarget(ctx, promotion, promotion.GetNamespace(), target, spec.Wait, state)
				if err == nil {
					state, err = c.completeCanaryAfterFullWeight(ctx, promotion, spec, target, state)
				}
				if err == nil {
					state, err = c.applyTargetTraffic(ctx, promotion.GetNamespace(), target, state)
				}
				status = upsertTargetStatus(status, target, terminalTargetPhase(err), state, err)
				_ = c.patchStatus(ctx, promotion, status)
				if err != nil {
					return status, err
				}
			} else {
				namespace := target.Namespace
				if namespace == "" {
					namespace = promotion.GetNamespace()
				}
				state, err = c.observeTarget(ctx, namespace, target)
				if err == nil {
					state, err = c.applyTargetTraffic(ctx, promotion.GetNamespace(), target, state)
				}
				status = upsertTargetStatus(status, target, terminalTargetPhase(err), state, err)
				_ = c.patchStatus(ctx, promotion, status)
				if err != nil {
					return status, err
				}
			}
		}
	default:
		return status, fmt.Errorf("unsupported spec.mode %q, expected together or sequential", spec.Mode)
	}

	return status, nil
}

func (c *Controller) executeControlAction(ctx context.Context, promotion *v1alpha1.RolloutPromotion, spec RequestSpec, status map[string]interface{}) (map[string]interface{}, error) {
	executeAndWait := func(target Target) error {
		state, err := c.resumeControlTarget(ctx, promotion, spec, target)
		status = upsertTargetStatus(status, target, promotedTargetPhase(err), state, err)
		_ = c.patchStatus(ctx, promotion, status)
		if err != nil {
			return err
		}
		if spec.Wait.Enabled {
			state, err = c.waitTargetAction(ctx, promotion, promotion.GetNamespace(), spec, target, state)
		}
		if err == nil && (spec.Action == "promoteFull" || spec.Action == "abort") {
			state, err = c.applyControlActionTraffic(ctx, promotion.GetNamespace(), spec.Action, target, state)
		}
		status = upsertTargetStatus(status, target, terminalTargetPhase(err), state, err)
		_ = c.patchStatus(ctx, promotion, status)
		return err
	}

	switch spec.Mode {
	case "together":
		initialStates := make([]map[string]interface{}, len(spec.Targets))
		for i, target := range spec.Targets {
			state, err := c.resumeControlTarget(ctx, promotion, spec, target)
			initialStates[i] = state
			status = upsertTargetStatus(status, target, promotedTargetPhase(err), state, err)
			_ = c.patchStatus(ctx, promotion, status)
			if err != nil {
				return status, err
			}
		}
		if spec.Wait.Enabled {
			for i, target := range spec.Targets {
				state, err := c.waitTargetAction(ctx, promotion, promotion.GetNamespace(), spec, target, initialStates[i])
				if err == nil && (spec.Action == "promoteFull" || spec.Action == "abort") {
					state, err = c.applyControlActionTraffic(ctx, promotion.GetNamespace(), spec.Action, target, state)
				}
				status = upsertTargetStatus(status, target, terminalTargetPhase(err), state, err)
				_ = c.patchStatus(ctx, promotion, status)
				if err != nil {
					return status, err
				}
			}
		}
	case "sequential":
		for _, target := range spec.Targets {
			if err := executeAndWait(target); err != nil {
				return status, err
			}
		}
	default:
		return status, fmt.Errorf("unsupported spec.mode %q, expected together or sequential", spec.Mode)
	}

	return status, nil
}

func (c *Controller) captureTrafficSnapshot(ctx context.Context, promotion *v1alpha1.RolloutPromotion, spec RequestSpec, status map[string]interface{}) (map[string]interface{}, error) {
	for _, snapshot := range promotion.Status.TrafficSnapshots {
		if snapshot.RequestID == spec.RequestID {
			return status, nil
		}
	}
	snapshot := v1alpha1.RolloutPromotionTrafficSnapshot{
		ID:        fmt.Sprintf("%s-%d", spec.RequestID, promotion.GetGeneration()),
		RequestID: spec.RequestID,
		CreatedAt: metav1.Now(),
		Targets:   make([]v1alpha1.RolloutPromotionTrafficSnapshotTarget, 0, len(spec.Targets)),
	}
	for _, target := range spec.Targets {
		if trafficConfig(target) == nil {
			return status, fmt.Errorf("rollout %s traffic snapshot requires a traffic provider", target.Name)
		}
		namespace := target.Namespace
		if namespace == "" {
			namespace = promotion.GetNamespace()
		}
		state, err := c.observeTarget(ctx, namespace, target)
		if err != nil {
			return status, err
		}
		if err := validateRequestedStrategy(target.Strategy, state); err != nil {
			return status, err
		}
		activeHash := ""
		config := trafficConfig(target)
		if adoptionPending, _ := state["legacyAdoptionPending"].(bool); adoptionPending {
			rollout, err := c.getTypedRollout(ctx, namespace, target.Name)
			if err != nil {
				return status, err
			}
			activeHash = legacyTrafficTarget
			activeWeight := rollout.GetAnnotations()[annotations.LegacyAdoptionLegacyWeightAnnotation]
			if config.Provider != traffic.PodAnnotation || strings.TrimSpace(activeWeight) == "" {
				return status, fmt.Errorf("rollout %s/%s legacy adoption has no legacy active weight", namespace, target.Name)
			}
			config.Config["activeWeight"] = activeWeight
			if err := c.validateLegacyTrafficTarget(ctx, namespace, config.Config["baseSelector"]); err != nil {
				return status, err
			}
		} else {
			var err error
			activeHash, err = resolveActiveTrafficHash(state)
			if err != nil {
				return status, fmt.Errorf("rollout %s/%s: %w", namespace, target.Name, err)
			}
			if err := c.validateRetainedTrafficTarget(ctx, namespace, target, activeHash); err != nil {
				return status, err
			}
		}
		target.Traffic = config
		provider, input, err := c.trafficTarget(ctx, namespace, target, activeHash)
		if err != nil {
			return status, err
		}
		saved, err := provider.Capture(ctx, input)
		if err != nil {
			return status, err
		}
		snapshot.Targets = append(snapshot.Targets, v1alpha1.RolloutPromotionTrafficSnapshotTarget{
			Name: target.Name, Namespace: namespace, Strategy: target.Strategy,
			ActiveHash: activeHash, Traffic: saved,
		})
	}
	snapshots := append([]v1alpha1.RolloutPromotionTrafficSnapshot(nil), promotion.Status.TrafficSnapshots...)
	snapshots = append(snapshots, snapshot)
	if len(snapshots) > spec.TrafficRollback.HistoryLimit {
		snapshots = snapshots[len(snapshots)-spec.TrafficRollback.HistoryLimit:]
	}
	status["trafficSnapshots"] = snapshots
	return status, nil
}

func resolveActiveTrafficHash(state map[string]interface{}) (string, error) {
	keys := []string{"trafficOverrideHash"}
	strategy, _ := state["strategy"].(string)
	if strategy == v1alpha1.RolloutPromotionStrategyCanary {
		keys = append(keys, "stableRS", "activeServiceHash", "currentPodHash")
	} else {
		keys = append(keys, "activeServiceHash", "activeSelector", "stableRS", "currentPodHash")
	}
	for _, key := range keys {
		if value, _ := state[key].(string); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
	}
	return "", fmt.Errorf("cannot determine the ReplicaSet currently carrying production traffic")
}

func (c *Controller) executePreviousTrafficSnapshot(ctx context.Context, promotion *v1alpha1.RolloutPromotion, spec RequestSpec, status map[string]interface{}) (map[string]interface{}, error) {
	if spec.TrafficRollback.Mode != v1alpha1.RolloutPromotionTrafficRollbackPrevious {
		return status, fmt.Errorf("unsupported traffic rollback mode %q", spec.TrafficRollback.Mode)
	}
	if len(promotion.Status.TrafficSnapshots) == 0 {
		return status, fmt.Errorf("RolloutPromotion %s/%s has no pre-promotion traffic snapshot", promotion.GetNamespace(), promotion.GetName())
	}
	snapshot := promotion.Status.TrafficSnapshots[len(promotion.Status.TrafficSnapshots)-1]
	targets := make([]Target, 0, len(snapshot.Targets))
	for _, saved := range snapshot.Targets {
		targets = append(targets, targetFromTrafficSnapshot(saved))
	}
	if err := validateTargetNamespaces(promotion.Namespace, targets); err != nil {
		return status, err
	}
	// Validate every snapshot before pinning any target. A provider/identity error
	// in a later target must not partially change an earlier target.
	for _, target := range targets {
		namespace := target.Namespace
		if namespace == "" {
			namespace = promotion.Namespace
		}
		provider, input, err := c.trafficTarget(ctx, namespace, target, target.ExpectedStableRS)
		if err != nil {
			return status, err
		}
		if target.TrafficSnapshot != nil {
			if err := provider.ValidateRestore(ctx, input, target.TrafficSnapshot); err != nil {
				return status, err
			}
		}
		if target.ExpectedStableRS != legacyTrafficTarget {
			if err := c.validateRetainedTrafficTarget(ctx, namespace, target, target.ExpectedStableRS); err != nil {
				return status, err
			}
		} else if err := c.validateLegacyTrafficTarget(ctx, namespace, input.Config["baseSelector"]); err != nil {
			return status, err
		}
	}
	status["activeTrafficSnapshotID"] = snapshot.ID

	executeTarget := func(target Target) error {
		state, err := c.rollbackTrafficTarget(ctx, promotion.GetNamespace(), target, target.ExpectedStableRS)
		status = upsertTargetStatus(status, target, terminalTargetPhase(err), state, err)
		_ = c.patchStatus(ctx, promotion, status)
		return err
	}

	switch spec.Mode {
	case "together":
		states := make([]map[string]interface{}, len(targets))
		for i, target := range targets {
			state, err := c.pinTrafficRollbackTarget(ctx, promotion.GetNamespace(), target, target.ExpectedStableRS)
			states[i] = state
			status = upsertTargetStatus(status, target, promotedTargetPhase(err), state, err)
			_ = c.patchStatus(ctx, promotion, status)
			if err != nil {
				return status, err
			}
		}
		for i, target := range targets {
			state, err := c.applyPinnedTrafficRollback(ctx, promotion.GetNamespace(), target, states[i])
			status = upsertTargetStatus(status, target, terminalTargetPhase(err), state, err)
			_ = c.patchStatus(ctx, promotion, status)
			if err != nil {
				return status, err
			}
		}
	case "sequential":
		for _, target := range targets {
			if err := executeTarget(target); err != nil {
				return status, err
			}
		}
	default:
		return status, fmt.Errorf("unsupported spec.mode %q, expected together or sequential", spec.Mode)
	}
	return status, nil
}

func targetFromTrafficSnapshot(saved v1alpha1.RolloutPromotionTrafficSnapshotTarget) Target {
	if saved.Traffic != nil {
		return Target{
			Name: saved.Name, Namespace: saved.Namespace, Strategy: saved.Strategy,
			ExpectedStableRS: saved.ActiveHash, ExpectedRolloutUID: saved.Traffic.RolloutUID,
			Traffic:         &v1alpha1.RolloutPromotionTraffic{Provider: saved.Traffic.Provider, Config: saved.Traffic.Config},
			TrafficSnapshot: saved.Traffic,
		}
	}
	return Target{
		Name:             saved.Name,
		Namespace:        saved.Namespace,
		Strategy:         saved.Strategy,
		ExpectedStableRS: saved.ActiveHash,
		WeightSwitch: &WeightSwitch{
			Enabled:             true,
			AnnotationKey:       saved.AnnotationKey,
			BaseSelector:        saved.BaseSelector,
			ActiveWeight:        saved.ActiveWeight,
			InactiveWeight:      saved.InactiveWeight,
			ReapplyDelaySeconds: int(saved.ReapplyDelaySeconds),
		},
	}
}

func (c *Controller) rollbackTrafficTarget(ctx context.Context, defaultNamespace string, target Target, targetHash string) (map[string]interface{}, error) {
	state, err := c.pinTrafficRollbackTarget(ctx, defaultNamespace, target, targetHash)
	if err != nil {
		return state, err
	}
	return c.applyPinnedTrafficRollback(ctx, defaultNamespace, target, state)
}

func (c *Controller) pinTrafficRollbackTarget(ctx context.Context, defaultNamespace string, target Target, targetHash string) (map[string]interface{}, error) {
	namespace := target.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}
	state, err := c.observeTarget(ctx, namespace, target)
	if err != nil {
		return state, err
	}
	if err := validateRequestedStrategy(target.Strategy, state); err != nil {
		return state, err
	}
	targetHash = strings.TrimSpace(targetHash)
	if targetHash == "" {
		return state, fmt.Errorf("rollout %s/%s traffic snapshot has an empty active hash", namespace, target.Name)
	}
	provider, input, err := c.trafficTarget(ctx, namespace, target, targetHash)
	if err != nil {
		return state, err
	}
	if target.TrafficSnapshot != nil {
		if err := provider.ValidateRestore(ctx, input, target.TrafficSnapshot); err != nil {
			return state, err
		}
	}
	if targetHash == legacyTrafficTarget {
		if _, ok := provider.(traffic.Adoption); !ok {
			return state, fmt.Errorf("traffic provider does not support legacy adoption rollback")
		}
		if err := c.validateLegacyTrafficTarget(ctx, namespace, input.Config["baseSelector"]); err != nil {
			return state, err
		}
		state["trafficRollbackTargetHash"] = targetHash
		state["trafficRollbackSourceHash"] = state["currentPodHash"]
		return state, nil
	}
	if err := c.validateRetainedTrafficTarget(ctx, namespace, target, targetHash); err != nil {
		return state, err
	}
	sourceHash, _ := state["currentPodHash"].(string)
	if err := c.patchTrafficOverride(ctx, namespace, target.Name, targetHash, sourceHash); err != nil {
		return state, err
	}
	state["trafficRollbackTargetHash"] = targetHash
	state["trafficRollbackSourceHash"] = sourceHash
	return state, nil
}

func (c *Controller) applyPinnedTrafficRollback(ctx context.Context, defaultNamespace string, target Target, state map[string]interface{}) (map[string]interface{}, error) {
	targetHash, _ := state["trafficRollbackTargetHash"].(string)
	if targetHash == "" {
		return state, fmt.Errorf("rollout %s has no pinned traffic rollback target", target.Name)
	}
	if targetHash == legacyTrafficTarget {
		namespace := target.Namespace
		if namespace == "" {
			namespace = defaultNamespace
		}
		rollout, err := c.getTypedRollout(ctx, namespace, target.Name)
		if err != nil {
			return state, err
		}
		if err := c.patchLegacyAdoptionState(ctx, namespace, target.Name, annotations.LegacyAdoptionStatePromoting); err != nil {
			return state, err
		}
		reconciler := c.adoptionProvider()
		if reconciler == nil {
			return state, fmt.Errorf("legacy adoption provider is unavailable")
		}
		if err := reconciler.RestoreAdoption(ctx, rollout); err != nil {
			return state, err
		}
		if delay := reapplyDelay(target); delay > 0 {
			select {
			case <-ctx.Done():
				return state, ctx.Err()
			case <-time.After(time.Duration(delay) * time.Second):
			}
			if err := reconciler.RestoreAdoption(ctx, rollout); err != nil {
				return state, err
			}
		}
		if err := c.patchLegacyAdoptionState(ctx, namespace, target.Name, annotations.LegacyAdoptionStatePending); err != nil {
			return state, err
		}
		state["trafficRollbackTargetHash"] = targetHash
		state["trafficRollbackApplied"] = true
		return state, nil
	}
	updated, err := c.applyTargetTrafficForHash(ctx, defaultNamespace, target, state, targetHash)
	if err != nil {
		return updated, err
	}
	updated["trafficRollbackTargetHash"] = targetHash
	updated["trafficRollbackApplied"] = true
	return updated, nil
}

func (c *Controller) validateLegacyTrafficTarget(ctx context.Context, namespace, baseSelector string) error {
	selector := appendSelector(strings.TrimSpace(baseSelector), "!"+rolloutHashLabel)
	pods, err := c.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	readyPods := 0
	for i := range pods.Items {
		if pods.Items[i].DeletionTimestamp == nil && podReady(&pods.Items[i]) {
			readyPods++
		}
	}
	if readyPods == 0 {
		return fmt.Errorf("legacy traffic target in namespace %s has no Ready Pods for selector %q; refusing rollback without Pod recreation", namespace, selector)
	}
	return nil
}

func (c *Controller) validateRetainedTrafficTarget(ctx context.Context, namespace string, target Target, targetHash string) error {
	ro, err := c.rolloutClient(namespace).Get(ctx, target.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	selector := fmt.Sprintf("%s=%s", rolloutHashLabel, targetHash)
	replicaSets, err := c.kubeClient.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	expectedPods := int32(0)
	availablePods := int32(0)
	ownedReplicaSets := 0
	ownedUIDs := map[types.UID]bool{}
	for i := range replicaSets.Items {
		rs := &replicaSets.Items[i]
		if !metav1.IsControlledBy(rs, ro) {
			continue
		}
		ownedReplicaSets++
		ownedUIDs[rs.UID] = true
		if rs.Spec.Replicas != nil {
			expectedPods += *rs.Spec.Replicas
		}
		availablePods += rs.Status.AvailableReplicas
	}
	if ownedReplicaSets == 0 || expectedPods == 0 {
		return fmt.Errorf("rollout %s/%s retained traffic target %s has no running owned ReplicaSet; refusing to recreate Pods", namespace, target.Name, targetHash)
	}
	if availablePods < expectedPods {
		return fmt.Errorf("rollout %s/%s retained traffic target %s is not fully available (%d/%d); refusing traffic rollback without Pod recreation", namespace, target.Name, targetHash, availablePods, expectedPods)
	}
	pods, err := c.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	readyPods := int32(0)
	for i := range pods.Items {
		owner := metav1.GetControllerOf(&pods.Items[i])
		if owner != nil && ownedUIDs[owner.UID] && pods.Items[i].DeletionTimestamp == nil && podReady(&pods.Items[i]) {
			readyPods++
		}
	}
	if readyPods < expectedPods {
		return fmt.Errorf("rollout %s/%s retained traffic target %s has only %d/%d Ready Pods; refusing traffic rollback without Pod recreation", namespace, target.Name, targetHash, readyPods, expectedPods)
	}
	return nil
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func (c *Controller) patchTrafficOverride(ctx context.Context, namespace, name, targetHash, sourceHash string) error {
	data, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]interface{}{
				annotations.TrafficOverrideHashAnnotation:       targetHash,
				annotations.TrafficOverrideSourceHashAnnotation: sourceHash,
			},
		},
	})
	if err != nil {
		return err
	}
	_, err = c.rolloutClient(namespace).Patch(ctx, name, types.MergePatchType, data, metav1.PatchOptions{})
	return err
}

func (c *Controller) preflightForwardPromotion(ctx context.Context, defaultNamespace string, targets []Target) error {
	// Validate every target before the durable commit point. A stale request must
	// fail closed before any traffic override or Rollout pause is changed.
	for _, target := range targets {
		namespace := target.Namespace
		if namespace == "" {
			namespace = defaultNamespace
		}
		if trafficConfig(target) != nil {
			if _, _, err := c.trafficTarget(ctx, namespace, target, ""); err != nil {
				return err
			}
		}
		state, err := c.observeTarget(ctx, namespace, target)
		if err != nil {
			return err
		}
		if err := validateRequestedStrategy(target.Strategy, state); err != nil {
			return err
		}
		if !targetAlreadyPromotedToExpected(target, state) {
			if err := validateTargetExpectations(target, state); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) clearTrafficOverrides(ctx context.Context, defaultNamespace string, targets []Target) error {
	for _, target := range targets {
		namespace := target.Namespace
		if namespace == "" {
			namespace = defaultNamespace
		}
		ro, err := c.rolloutClient(namespace).Get(ctx, target.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		annotationsMap := ro.GetAnnotations()
		if annotationsMap == nil || annotationsMap[annotations.TrafficOverrideHashAnnotation] == "" {
			continue
		}
		data, err := json.Marshal(map[string]interface{}{
			"metadata": map[string]interface{}{
				"annotations": map[string]interface{}{
					annotations.TrafficOverrideHashAnnotation:       nil,
					annotations.TrafficOverrideSourceHashAnnotation: nil,
				},
			},
		})
		if err != nil {
			return err
		}
		if _, err = c.rolloutClient(namespace).Patch(ctx, target.Name, types.MergePatchType, data, metav1.PatchOptions{}); err != nil {
			return err
		}
		if aborted, _, _ := unstructured.NestedBool(ro.Object, "status", "abort"); aborted {
			if err := c.patchRolloutStatus(ctx, namespace, target.Name, map[string]interface{}{"abort": false}); err != nil {
				return err
			}
		}
	}
	return nil
}

// prepareForwardPromotion is retained for focused unit tests and callers which
// explicitly want the legacy validate-and-clear operation. Reconcile uses the
// two phases separately so validation completes before the durable commit.
func (c *Controller) prepareForwardPromotion(ctx context.Context, defaultNamespace string, targets []Target) error {
	if err := c.preflightForwardPromotion(ctx, defaultNamespace, targets); err != nil {
		return err
	}
	return c.clearTrafficOverrides(ctx, defaultNamespace, targets)
}

func (c *Controller) preflightControlAction(ctx context.Context, defaultNamespace string, spec RequestSpec) error {
	for _, target := range spec.Targets {
		namespace := target.Namespace
		if namespace == "" {
			namespace = defaultNamespace
		}
		if trafficConfig(target) != nil {
			if _, _, err := c.trafficTarget(ctx, namespace, target, ""); err != nil {
				return err
			}
		}
		state, err := c.observeTarget(ctx, namespace, target)
		if err != nil {
			return err
		}
		if err := validateRequestedStrategy(target.Strategy, state); err != nil {
			return err
		}
		if err := validateTargetExpectationsForAction(spec.Action, target, state); err != nil {
			return err
		}
		if spec.Action == "rollback" {
			if _, err := c.resolveRollbackRevision(ctx, namespace, target, state); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Controller) resumePromoteTarget(ctx context.Context, promotion *v1alpha1.RolloutPromotion, target Target) (map[string]interface{}, error) {
	namespace := target.Namespace
	if namespace == "" {
		namespace = promotion.GetNamespace()
	}
	checkpoint := targetCheckpoint(promotion, target)
	state, err := c.observeTarget(ctx, namespace, target)
	if err != nil {
		return state, err
	}
	if adoptionState, _ := state["legacyAdoptionState"].(string); adoptionState == annotations.LegacyAdoptionStatePromoted {
		currentHash, _ := state["currentPodHash"].(string)
		stableHash, _ := state["stableRS"].(string)
		if currentHash != "" && currentHash == stableHash {
			if err := c.ensurePromotedLegacyAdoption(ctx, namespace, target.Name); err != nil {
				return state, err
			}
			state, err = c.observeTarget(ctx, namespace, target)
			if err != nil {
				return state, err
			}
		}
	}
	if checkpoint != nil {
		if target.Readiness != nil {
			// Bind fields that were unavailable at render time to the durable
			// checkpoint, never to a later revision observed after a restart.
			target.ExpectedCurrentPodHash = checkpoint.BaselineCurrentPodHash
			if err := validatePromotionIdentity(target, state); err != nil {
				return state, err
			}
		}
		state["previousStepIndex"] = int64(checkpoint.PreviousStepIndex)
		state["baselineCurrentPodHash"] = checkpoint.BaselineCurrentPodHash
		legacyAdoptionPending, _ := state["legacyAdoptionPending"].(bool)
		if !legacyAdoptionPending && (checkpoint.ActionApplied || checkpoint.Phase == "Promoted" || checkpoint.Phase == PhaseSucceeded || forwardActionObservedAfterCommit(checkpoint, state)) {
			state["actionApplied"] = true
			return state, nil
		}
	}
	state, err = c.promoteTarget(ctx, promotion.GetNamespace(), target)
	if err == nil {
		state["actionApplied"] = true
	}
	return state, err
}

func forwardActionObservedAfterCommit(checkpoint *v1alpha1.RolloutPromotionTargetStatus, state map[string]interface{}) bool {
	if pending, _ := state["legacyAdoptionPending"].(bool); pending {
		return false
	}
	if promoted, _ := state["promoted"].(bool); promoted {
		return true
	}
	strategy, _ := state["strategy"].(string)
	pauseConditions, _ := state["pauseConditions"].(int)
	specPaused, _ := state["specPaused"].(bool)
	currentHash, _ := state["currentPodHash"].(string)
	stableHash, _ := state["stableRS"].(string)
	if strategy == "canary" {
		currentStep, _ := state["currentStepIndex"].(int64)
		if currentStep > int64(checkpoint.PreviousStepIndex) {
			return true
		}
		return currentStep == int64(checkpoint.PreviousStepIndex) && pauseConditions == 0 && !specPaused
	}
	return currentHash != "" && currentHash != stableHash && pauseConditions == 0 && !specPaused
}

func (c *Controller) resumeControlTarget(ctx context.Context, promotion *v1alpha1.RolloutPromotion, spec RequestSpec, target Target) (map[string]interface{}, error) {
	namespace := target.Namespace
	if namespace == "" {
		namespace = promotion.GetNamespace()
	}
	checkpoint := targetCheckpoint(promotion, target)
	state, err := c.observeTarget(ctx, namespace, target)
	if err != nil {
		return state, err
	}
	baselineHash := ""
	if checkpoint != nil {
		baselineHash = checkpoint.BaselineCurrentPodHash
		state["baselineCurrentPodHash"] = baselineHash
		if checkpoint.ResolvedToRevision > 0 {
			target.ToRevision = checkpoint.ResolvedToRevision
			target.RollbackTarget = ""
			state["resolvedToRevision"] = checkpoint.ResolvedToRevision
		}
		if checkpoint.ActionApplied || checkpoint.Phase == "Promoted" || checkpoint.Phase == PhaseSucceeded || controlActionObservedAfterCommit(spec.Action, baselineHash, state) {
			state["actionApplied"] = true
			return state, nil
		}
	}
	state, err = c.controlTarget(ctx, promotion.GetNamespace(), spec, target)
	if err == nil {
		state["actionApplied"] = true
	}
	return state, err
}

func controlActionObservedAfterCommit(action, baselineHash string, state map[string]interface{}) bool {
	if targetActionCompleted(action, baselineHash, state) {
		return true
	}
	switch action {
	case "abort":
		aborted, _ := state["aborted"].(bool)
		return aborted
	case "retry":
		aborted, _ := state["aborted"].(bool)
		return !aborted
	case "promoteFull":
		currentHash, _ := state["currentPodHash"].(string)
		stableHash, _ := state["stableRS"].(string)
		pauseConditions, _ := state["pauseConditions"].(int)
		return currentHash != "" && currentHash != stableHash && pauseConditions == 0
	case "rollback":
		currentHash, _ := state["currentPodHash"].(string)
		return baselineHash != "" && currentHash != "" && currentHash != baselineHash
	default:
		return false
	}
}

func targetCheckpoint(promotion *v1alpha1.RolloutPromotion, target Target) *v1alpha1.RolloutPromotionTargetStatus {
	if promotion == nil {
		return nil
	}
	for i := range promotion.Status.Targets {
		checkpoint := &promotion.Status.Targets[i]
		if checkpoint.Name == target.Name && (target.Namespace == "" || checkpoint.Namespace == "" || checkpoint.Namespace == target.Namespace) {
			return checkpoint
		}
	}
	return nil
}

func (c *Controller) promoteTarget(ctx context.Context, defaultNamespace string, target Target) (map[string]interface{}, error) {
	namespace := target.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	state, err := c.observeTarget(ctx, namespace, target)
	if err != nil {
		return state, err
	}
	if err := validateRequestedStrategy(target.Strategy, state); err != nil {
		return state, err
	}
	if err := validateForwardReadiness(target, state, "promote"); err != nil {
		return state, err
	}
	if adoptionPending, _ := state["legacyAdoptionPending"].(bool); adoptionPending {
		if err := validateTargetExpectations(target, state); err != nil {
			return state, err
		}
		previousStepIndex, _ := state["currentStepIndex"].(int64)
		if err := c.patchLegacyAdoptionState(ctx, namespace, target.Name, annotations.LegacyAdoptionStatePromoting); err != nil {
			return state, err
		}
		rollout, err := c.getTypedRollout(ctx, namespace, target.Name)
		if err != nil {
			return state, err
		}
		reconciler := c.adoptionProvider()
		if reconciler == nil {
			return state, fmt.Errorf("legacy adoption provider is unavailable")
		}
		if err := reconciler.PromoteAdoption(ctx, rollout); err != nil {
			return state, fmt.Errorf("promote legacy adoption traffic for rollout %s/%s: %w", namespace, target.Name, err)
		}
		if delay := reapplyDelay(target); delay > 0 {
			select {
			case <-ctx.Done():
				return state, ctx.Err()
			case <-time.After(time.Duration(delay) * time.Second):
			}
			if err := reconciler.PromoteAdoption(ctx, rollout); err != nil {
				return state, fmt.Errorf("reapply legacy adoption traffic for rollout %s/%s: %w", namespace, target.Name, err)
			}
		}
		if err := c.patchLegacyAdoptionState(ctx, namespace, target.Name, annotations.LegacyAdoptionStatePromoted); err != nil {
			return state, err
		}
		if err := c.ensurePromotedLegacyAdoption(ctx, namespace, target.Name); err != nil {
			return state, err
		}
		state, err = c.observeTarget(ctx, namespace, target)
		if err != nil {
			return state, err
		}
		state["previousStepIndex"] = previousStepIndex
		return state, nil
	}
	if targetAlreadyPromotedToExpected(target, state) {
		state["actionAlreadyComplete"] = true
		return state, nil
	}
	if err := validateTargetExpectations(target, state); err != nil {
		return state, err
	}

	previousStepIndex, _ := state["currentStepIndex"].(int64)
	statusUpdate := map[string]interface{}{"pauseConditions": nil}
	if strategy, _ := state["strategy"].(string); strategy == "canary" {
		pauseConditions, _ := state["pauseConditions"].(int)
		specPaused, _ := state["specPaused"].(bool)
		stepCount, _ := state["stepCount"].(int64)
		controllerPause, _ := state["controllerPause"].(bool)
		analysisPhase, _ := state["currentStepAnalysisPhase"].(string)
		if analysisPhase == "Inconclusive" && pauseConditions > 0 && controllerPause {
			statusUpdate["controllerPause"] = false
			if previousStepIndex < stepCount {
				statusUpdate["currentStepIndex"] = previousStepIndex + 1
			}
		} else if pauseConditions == 0 && !specPaused {
			return state, fmt.Errorf("rollout %s/%s is not paused at canary step %d; refusing to promote", namespace, target.Name, previousStepIndex)
		}
	}
	statusPatch, err := json.Marshal(map[string]interface{}{"status": statusUpdate})
	if err != nil {
		return state, err
	}
	specPatch := []byte(`{"spec":{"paused":false}}`)
	unifiedPatch, err := json.Marshal(map[string]interface{}{
		"spec":   map[string]interface{}{"paused": false},
		"status": statusUpdate,
	})
	if err != nil {
		return state, err
	}

	if _, err := c.rolloutClient(namespace).Patch(ctx, target.Name, types.MergePatchType, statusPatch, metav1.PatchOptions{}, "status"); err != nil {
		log.WithError(err).WithField("rollout", fmt.Sprintf("%s/%s", namespace, target.Name)).Warn("status subresource patch failed, falling back to unified patch")
		if _, fallbackErr := c.rolloutClient(namespace).Patch(ctx, target.Name, types.MergePatchType, unifiedPatch, metav1.PatchOptions{}); fallbackErr != nil {
			return nil, fallbackErr
		}
	} else if _, err := c.rolloutClient(namespace).Patch(ctx, target.Name, types.MergePatchType, specPatch, metav1.PatchOptions{}); err != nil {
		return nil, err
	}

	state, err = c.observeTarget(ctx, namespace, target)
	if err != nil {
		return state, err
	}
	state["previousStepIndex"] = previousStepIndex
	return state, nil
}

func (c *Controller) getTypedRollout(ctx context.Context, namespace, name string) (*v1alpha1.Rollout, error) {
	unstructuredRollout, err := c.rolloutClient(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	var rollout v1alpha1.Rollout
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredRollout.Object, &rollout); err != nil {
		return nil, fmt.Errorf("decode rollout %s/%s: %w", namespace, name, err)
	}
	return &rollout, nil
}

func (c *Controller) patchLegacyAdoptionState(ctx context.Context, namespace, name, state string) error {
	annotationPatch := map[string]interface{}{
		annotations.LegacyAdoptionStateAnnotation: state,
	}
	if state == annotations.LegacyAdoptionStatePromoted {
		annotationPatch[annotations.LegacyAdoptionPromotedAnnotation] = "true"
	} else {
		annotationPatch[annotations.LegacyAdoptionPromotedAnnotation] = nil
	}
	data, err := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": annotationPatch,
		},
	})
	if err != nil {
		return err
	}
	_, err = c.rolloutClient(namespace).Patch(ctx, name, types.MergePatchType, data, metav1.PatchOptions{})
	return err
}

// reconcileTerminalForwardLegacyAdoption repairs first-adoption state that an
// older Helm release may have overwritten after traffic was promoted. The
// durable successful operation and per-target checkpoints make this safe to
// replay after controller restarts without advancing a Rollout or recreating
// Pods.
func (c *Controller) reconcileTerminalForwardLegacyAdoption(ctx context.Context, promotion *v1alpha1.RolloutPromotion) error {
	operation := promotion.Status.Operation
	if operation == nil || operation.Kind != v1alpha1.RolloutPromotionOperationForward || operation.Phase != v1alpha1.RolloutPromotionOperationSucceeded {
		return nil
	}

	for i := range promotion.Status.Targets {
		checkpoint := &promotion.Status.Targets[i]
		if !checkpoint.ActionApplied || !checkpoint.Verified || checkpoint.CurrentPodHash == "" || checkpoint.CurrentPodHash != checkpoint.StableRS {
			continue
		}
		namespace := checkpoint.Namespace
		if namespace == "" {
			namespace = promotion.GetNamespace()
		}
		rollout, err := c.getTypedRollout(ctx, namespace, checkpoint.Name)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		rolloutAnnotations := rollout.GetAnnotations()
		if strings.TrimSpace(rolloutAnnotations[annotations.LegacyAdoptionBaseSelectorAnnotation]) == "" {
			continue
		}
		if checkpoint.RolloutUID != "" && string(rollout.GetUID()) != checkpoint.RolloutUID {
			continue
		}
		operationTime := operation.CompletionTime
		if operationTime == nil {
			operationTime = operation.CommitTime
		}
		creationTimestamp := rollout.GetCreationTimestamp()
		if operationTime != nil && !creationTimestamp.IsZero() && creationTimestamp.After(operationTime.Time) {
			continue
		}
		if rollout.Status.CurrentPodHash != checkpoint.CurrentPodHash || rollout.Status.StableRS != checkpoint.StableRS {
			continue
		}
		state := strings.TrimSpace(rolloutAnnotations[annotations.LegacyAdoptionStateAnnotation])
		durable := strings.EqualFold(strings.TrimSpace(rolloutAnnotations[annotations.LegacyAdoptionPromotedAnnotation]), "true")
		if !durable && state != annotations.LegacyAdoptionStatePending && state != annotations.LegacyAdoptionStatePromoting && state != annotations.LegacyAdoptionStatePromoted {
			continue
		}
		if !durable || state != annotations.LegacyAdoptionStatePromoted {
			if err := c.patchLegacyAdoptionState(ctx, namespace, checkpoint.Name, annotations.LegacyAdoptionStatePromoted); err != nil {
				return err
			}
		}
		if err := c.ensurePromotedLegacyAdoption(ctx, namespace, checkpoint.Name); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) ensurePromotedLegacyAdoption(ctx context.Context, namespace, name string) error {
	rollout, err := c.getTypedRollout(ctx, namespace, name)
	if err != nil {
		return err
	}
	reconciler := c.adoptionProvider()
	if reconciler == nil {
		return fmt.Errorf("legacy adoption provider is unavailable")
	}
	if err := reconciler.PromoteAdoption(ctx, rollout); err != nil {
		return fmt.Errorf("reconcile promoted legacy adoption traffic for rollout %s/%s: %w", namespace, name, err)
	}
	verified, err := reconciler.VerifyAdoption(ctx, rollout)
	if err != nil {
		return fmt.Errorf("verify promoted legacy adoption traffic for rollout %s/%s: %w", namespace, name, err)
	}
	if !verified {
		return fmt.Errorf("rollout %s/%s promoted legacy adoption traffic has not converged", namespace, name)
	}
	return nil
}

func targetAlreadyPromotedToExpected(target Target, state map[string]interface{}) bool {
	if validatePromotionIdentity(target, state) != nil {
		return false
	}
	if target.Readiness != nil && !observedLatestGeneration(state) {
		return false
	}
	if target.Readiness != nil {
		if state["rolloutPhase"] != "Healthy" || state["trafficOverrideHash"] != "" {
			return false
		}
		if service, _ := state["activeService"].(string); service != "" && state["activeServiceHash"] != state["currentPodHash"] {
			return false
		}
	}
	promoted, _ := state["promoted"].(bool)
	if !promoted {
		return false
	}
	currentHash, _ := state["currentPodHash"].(string)
	rolloutUID, _ := state["rolloutUID"].(string)
	if target.ExpectedRolloutUID != "" && rolloutUID != target.ExpectedRolloutUID {
		return false
	}
	if target.ExpectedCurrentPodHash != "" && currentHash != target.ExpectedCurrentPodHash {
		return false
	}
	return currentHash != ""
}

func validateTargetExpectations(target Target, state map[string]interface{}) error {
	if err := validateTargetIdentityExpectations(target, state); err != nil {
		return err
	}
	namespace, _ := state["namespace"].(string)
	if target.ExpectedCurrentStepIndex != nil {
		actual, _ := state["currentStepIndex"].(int64)
		if actual != int64(*target.ExpectedCurrentStepIndex) {
			return fmt.Errorf("rollout %s/%s safety check failed: expectedCurrentStepIndex expected %d but got %d", namespace, target.Name, *target.ExpectedCurrentStepIndex, actual)
		}
	}
	return nil
}

func validateTargetIdentityExpectations(target Target, state map[string]interface{}) error {
	checks := []struct {
		field    string
		expected string
		stateKey string
	}{
		{
			field:    "expectedRolloutUID",
			expected: target.ExpectedRolloutUID,
			stateKey: "rolloutUID",
		},
		{
			field:    "expectedCurrentPodHash",
			expected: target.ExpectedCurrentPodHash,
			stateKey: "currentPodHash",
		},
		{
			field:    "expectedStableRS",
			expected: target.ExpectedStableRS,
			stateKey: "stableRS",
		},
		{
			field:    "expectedActiveServiceHash",
			expected: target.ExpectedActiveServiceHash,
			stateKey: "activeServiceHash",
		},
	}

	namespace, _ := state["namespace"].(string)
	for _, check := range checks {
		if check.expected == "" {
			continue
		}
		actual, _ := state[check.stateKey].(string)
		if actual != check.expected {
			return fmt.Errorf("rollout %s/%s safety check failed: %s expected %q but got %q", namespace, target.Name, check.field, check.expected, actual)
		}
	}
	return nil
}

func validateTargetExpectationsForAction(action string, target Target, state map[string]interface{}) error {
	if action != "rollback" && targetActionCompleted(action, "", state) {
		return validateTargetIdentityExpectations(target, state)
	}
	return validateTargetExpectations(target, state)
}

func (c *Controller) waitTarget(ctx context.Context, promotion *v1alpha1.RolloutPromotion, defaultNamespace string, target Target, opts WaitOptions, initialState map[string]interface{}) (map[string]interface{}, error) {
	namespace := target.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	attempts := opts.Attempts
	if attempts <= 0 {
		attempts = defaultWaitAttempts
	}
	interval := time.Duration(opts.Seconds) * time.Second
	if interval <= 0 {
		interval = time.Duration(defaultWaitSeconds) * time.Second
	}

	var lastState map[string]interface{}
	var previousStepIndex int64
	var waitForCanaryStep bool
	if strategy, _ := initialState["strategy"].(string); strategy == "canary" {
		previousStepIndex, _ = initialState["previousStepIndex"].(int64)
		waitForCanaryStep = true
	}
	for i := 0; i < attempts; i++ {
		if err := c.checkTrafficRollbackRequest(ctx, promotion); err != nil {
			return lastState, err
		}

		state, err := c.observeTarget(ctx, namespace, target)
		if err != nil {
			return state, err
		}
		lastState = state
		if target.Readiness != nil {
			if err := validatePromotionIdentity(target, state); err != nil {
				return state, err
			}
		}
		if err := c.checkTrafficRollbackRequest(ctx, promotion); err != nil {
			return lastState, err
		}
		if waitForCanaryStep {
			currentStepIndex, _ := state["currentStepIndex"].(int64)
			pauseConditions, _ := state["pauseConditions"].(int)
			if promoted, _ := state["promoted"].(bool); promoted {
				return state, nil
			}
			if currentStepIndex > previousStepIndex && pauseConditions > 0 {
				return state, nil
			}
		} else if promoted, _ := state["promoted"].(bool); promoted {
			return state, nil
		}
		if err := c.waitForTrafficRollbackOrInterval(ctx, promotion, interval); err != nil {
			return lastState, err
		}
	}

	return lastState, fmt.Errorf("rollout %s/%s promotion did not complete after %d attempts", namespace, target.Name, attempts)
}

func shouldCompleteCanaryAfterFullWeight(policy string, state map[string]interface{}) (bool, error) {
	if policy != v1alpha1.RolloutPromotionCanaryCompletionFullWeight {
		return false, nil
	}
	if strategy, _ := state["strategy"].(string); strategy != v1alpha1.RolloutPromotionStrategyCanary {
		return false, nil
	}
	if promoted, _ := state["promoted"].(bool); promoted {
		return false, nil
	}
	pauseConditions, _ := state["pauseConditions"].(int)
	currentStepIndex, _ := state["currentStepIndex"].(int64)
	stepCount, _ := state["stepCount"].(int64)
	canaryWeight, canaryWeightFound := state["canaryWeight"].(int64)
	if pauseConditions == 0 || stepCount == 0 || currentStepIndex != stepCount-1 {
		return false, nil
	}
	if !canaryWeightFound || canaryWeight != 100 {
		return false, fmt.Errorf("canary completion policy %s reached its final pause without 100%% traffic; observed step=%d/%d canaryWeight=%v", policy, currentStepIndex, stepCount, state["canaryWeight"])
	}
	if verified, found := state["canaryWeightVerified"].(bool); found && !verified {
		return false, nil
	}
	return true, nil
}

func (c *Controller) completeCanaryAfterFullWeight(ctx context.Context, promotion *v1alpha1.RolloutPromotion, spec RequestSpec, target Target, state map[string]interface{}) (map[string]interface{}, error) {
	namespace := target.Namespace
	if namespace == "" {
		namespace = promotion.GetNamespace()
	}
	if spec.CanaryCompletionPolicy != v1alpha1.RolloutPromotionCanaryCompletionFullWeight {
		return state, nil
	}

	attempts := spec.Wait.Attempts
	if attempts <= 0 {
		attempts = defaultWaitAttempts
	}
	interval := time.Duration(spec.Wait.Seconds) * time.Second
	if interval <= 0 {
		interval = time.Duration(defaultWaitSeconds) * time.Second
	}
	for i := 0; i < attempts; i++ {
		if target.Readiness != nil {
			if err := validatePromotionIdentity(target, state); err != nil {
				return state, err
			}
		}
		complete, err := shouldCompleteCanaryAfterFullWeight(spec.CanaryCompletionPolicy, state)
		if err != nil {
			return state, err
		}
		if complete {
			if err := c.patchRolloutStatus(ctx, namespace, target.Name, map[string]interface{}{"promoteFull": true}); err != nil {
				return state, err
			}
			if err := c.patchRolloutSpec(ctx, namespace, target.Name, map[string]interface{}{"paused": false}); err != nil {
				return state, err
			}
			completionSpec := spec
			completionSpec.Action = v1alpha1.RolloutPromotionActionPromoteFull
			return c.waitTargetAction(ctx, promotion, promotion.GetNamespace(), completionSpec, target, state)
		}
		if promoted, _ := state["promoted"].(bool); promoted {
			return state, nil
		}
		pauseConditions, _ := state["pauseConditions"].(int)
		currentPauseTimed, _ := state["currentPauseTimed"].(bool)
		if pauseConditions > 0 && !currentPauseTimed {
			return state, nil
		}
		if err := c.waitForTrafficRollbackOrInterval(ctx, promotion, interval); err != nil {
			return state, err
		}
		state, err = c.observeTarget(ctx, namespace, target)
		if err != nil {
			return state, err
		}
	}
	return state, fmt.Errorf("rollout %s/%s did not reach its final 100%% canary pause after %d attempts", namespace, target.Name, attempts)
}

func (c *Controller) controlTarget(ctx context.Context, defaultNamespace string, spec RequestSpec, target Target) (map[string]interface{}, error) {
	namespace := target.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}

	state, err := c.observeTarget(ctx, namespace, target)
	if err != nil {
		return state, err
	}
	if err := validateRequestedStrategy(target.Strategy, state); err != nil {
		return state, err
	}
	if err := validateTargetExpectationsForAction(spec.Action, target, state); err != nil {
		return state, err
	}
	state["baselineCurrentPodHash"] = state["currentPodHash"]
	if spec.Action != "rollback" && targetActionCompleted(spec.Action, "", state) {
		return state, nil
	}

	switch spec.Action {
	case "promoteFull":
		if currentHash, _ := state["currentPodHash"].(string); currentHash != state["stableRS"] {
			if err := c.patchRolloutStatus(ctx, namespace, target.Name, map[string]interface{}{"promoteFull": true}); err != nil {
				return state, err
			}
			if err := c.patchRolloutSpec(ctx, namespace, target.Name, map[string]interface{}{"paused": false}); err != nil {
				return state, err
			}
		}
	case "abort":
		if err := c.patchRolloutStatus(ctx, namespace, target.Name, map[string]interface{}{"abort": true}); err != nil {
			return state, err
		}
	case "retry":
		if err := c.patchRolloutStatus(ctx, namespace, target.Name, map[string]interface{}{"abort": false}); err != nil {
			return state, err
		}
	case "rollback":
		resolvedToRevision, err := c.resolveRollbackRevision(ctx, namespace, target, state)
		if err != nil {
			return state, err
		}
		state["resolvedToRevision"] = resolvedToRevision
		result, err := rolloutundo.RunUndoRollout(c.rolloutClient(namespace), c.kubeClient, target.Name, resolvedToRevision)
		if err != nil {
			return state, err
		}
		if strings.Contains(result, "skipped rollback") {
			state["actionAlreadyComplete"] = true
		}
	default:
		return state, fmt.Errorf("unsupported spec.action %q", spec.Action)
	}

	updated, err := c.observeTarget(ctx, namespace, target)
	if err != nil {
		return state, err
	}
	updated["baselineCurrentPodHash"] = state["baselineCurrentPodHash"]
	if alreadyComplete, _ := state["actionAlreadyComplete"].(bool); alreadyComplete {
		updated["actionAlreadyComplete"] = true
	}
	if resolvedToRevision, ok := state["resolvedToRevision"]; ok {
		updated["resolvedToRevision"] = resolvedToRevision
	}
	return updated, nil
}

func (c *Controller) resolveRollbackRevision(ctx context.Context, namespace string, target Target, state map[string]interface{}) (int64, error) {
	if target.ToRevision > 0 {
		return target.ToRevision, nil
	}
	if target.RollbackTarget != v1alpha1.RolloutPromotionRollbackTargetStable {
		return 0, fmt.Errorf("rollout %s/%s has unsupported rollbackTarget %q", namespace, target.Name, target.RollbackTarget)
	}

	stableHash, _ := state["stableRS"].(string)
	if stableHash == "" {
		return 0, fmt.Errorf("rollout %s/%s cannot resolve rollbackTarget stable: status.stableRS is empty", namespace, target.Name)
	}
	ro, err := c.rolloutClient(namespace).Get(ctx, target.Name, metav1.GetOptions{})
	if err != nil {
		return 0, err
	}
	replicaSets, err := c.kubeClient.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("list ReplicaSets for rollout %s/%s: %w", namespace, target.Name, err)
	}

	var resolvedRevision int64
	for i := range replicaSets.Items {
		rs := &replicaSets.Items[i]
		if !metav1.IsControlledBy(rs, ro) || rs.Labels[rolloutHashLabel] != stableHash {
			continue
		}
		revision, err := strconv.ParseInt(rs.Annotations[annotations.RevisionAnnotation], 10, 64)
		if err == nil && revision > resolvedRevision {
			resolvedRevision = revision
		}
	}
	if resolvedRevision <= 0 {
		return 0, fmt.Errorf("rollout %s/%s cannot map stableRS %s to an owned ReplicaSet revision", namespace, target.Name, stableHash)
	}
	return resolvedRevision, nil
}

func (c *Controller) waitTargetAction(ctx context.Context, promotion *v1alpha1.RolloutPromotion, defaultNamespace string, spec RequestSpec, target Target, initialState map[string]interface{}) (map[string]interface{}, error) {
	namespace := target.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}
	attempts := spec.Wait.Attempts
	if attempts <= 0 {
		attempts = defaultWaitAttempts
	}
	interval := time.Duration(spec.Wait.Seconds) * time.Second
	if interval <= 0 {
		interval = time.Duration(defaultWaitSeconds) * time.Second
	}
	if alreadyComplete, _ := initialState["actionAlreadyComplete"].(bool); alreadyComplete {
		return c.observeTarget(ctx, namespace, target)
	}

	baselineHash, _ := initialState["baselineCurrentPodHash"].(string)
	var lastState map[string]interface{}
	for i := 0; i < attempts; i++ {
		if err := c.checkTrafficRollbackRequest(ctx, promotion); err != nil {
			return lastState, err
		}
		state, err := c.observeTarget(ctx, namespace, target)
		if err != nil {
			return state, err
		}
		lastState = state
		if err := c.checkTrafficRollbackRequest(ctx, promotion); err != nil {
			return lastState, err
		}
		if targetActionCompleted(spec.Action, baselineHash, state) {
			return state, nil
		}
		if err := c.waitForTrafficRollbackOrInterval(ctx, promotion, interval); err != nil {
			return lastState, err
		}
	}
	return lastState, fmt.Errorf("rollout %s/%s action %s did not complete after %d attempts", namespace, target.Name, spec.Action, attempts)
}

func (c *Controller) checkTrafficRollbackRequest(ctx context.Context, promotion *v1alpha1.RolloutPromotion) error {
	if promotion == nil || c.rolloutsClient == nil {
		return nil
	}
	live, err := c.promotionClient(promotion.GetNamespace()).Get(ctx, promotion.GetName(), metav1.GetOptions{})
	if err != nil {
		return err
	}
	requestID := strings.TrimSpace(live.GetAnnotations()[annotations.TrafficRollbackRequestAnnotation])
	if requestID == "" || requestID == live.Status.LastTrafficRollbackRequest {
		return nil
	}
	trafficRollback := live.Spec.TrafficRollback
	if trafficRollback == nil || (trafficRollback.Enabled != nil && !*trafficRollback.Enabled) {
		return nil
	}
	return &trafficRollbackRequestedError{requestID: requestID}
}

func (c *Controller) waitForTrafficRollbackOrInterval(ctx context.Context, promotion *v1alpha1.RolloutPromotion, interval time.Duration) error {
	if promotion == nil || c.rolloutsClient == nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
			return nil
		}
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()
	pollPeriod := rollbackPollPeriod
	if interval < pollPeriod {
		pollPeriod = interval
	}
	ticker := time.NewTicker(pollPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case <-ticker.C:
			if err := c.checkTrafficRollbackRequest(ctx, promotion); err != nil {
				return err
			}
		}
	}
}

func targetActionCompleted(action, baselineHash string, state map[string]interface{}) bool {
	switch action {
	case "abort":
		aborted, _ := state["aborted"].(bool)
		if !aborted {
			return false
		}
		if strategy, _ := state["strategy"].(string); strategy == "canary" {
			if canaryWeight, ok := state["canaryWeight"].(int64); ok && canaryWeight != 0 {
				return false
			}
		}
		return true
	case "retry":
		aborted, _ := state["aborted"].(bool)
		return !aborted
	case "promoteFull":
		promoted, _ := state["promoted"].(bool)
		return promoted
	case "rollback":
		currentHash, _ := state["currentPodHash"].(string)
		promoted, _ := state["promoted"].(bool)
		return baselineHash != "" && currentHash != "" && currentHash != baselineHash && promoted
	default:
		return false
	}
}

func (c *Controller) patchRolloutStatus(ctx context.Context, namespace, name string, status map[string]interface{}) error {
	data, err := json.Marshal(map[string]interface{}{"status": status})
	if err != nil {
		return err
	}
	_, err = c.rolloutClient(namespace).Patch(ctx, name, types.MergePatchType, data, metav1.PatchOptions{}, "status")
	return err
}

func (c *Controller) patchRolloutSpec(ctx context.Context, namespace, name string, spec map[string]interface{}) error {
	data, err := json.Marshal(map[string]interface{}{"spec": spec})
	if err != nil {
		return err
	}
	_, err = c.rolloutClient(namespace).Patch(ctx, name, types.MergePatchType, data, metav1.PatchOptions{})
	return err
}

func validateRequestedStrategy(requested string, state map[string]interface{}) error {
	if requested == "" || requested == "auto" {
		return nil
	}
	actual, _ := state["strategy"].(string)
	if actual != requested {
		return fmt.Errorf("rollout %s/%s strategy safety check failed: expected %q but got %q", state["namespace"], state["name"], requested, actual)
	}
	return nil
}

func (c *Controller) applyTargetTraffic(ctx context.Context, defaultNamespace string, target Target, state map[string]interface{}) (map[string]interface{}, error) {
	config := trafficConfig(target)
	if config == nil || (config.ApplyOnPromotion != nil && !*config.ApplyOnPromotion) {
		return state, nil
	}
	promoted, _ := state["promoted"].(bool)
	if !promoted {
		namespace := target.Namespace
		if namespace == "" {
			namespace = defaultNamespace
		}
		return state, fmt.Errorf("rollout %s/%s weightSwitch requires promoted=true before patching Pod weights", namespace, target.Name)
	}

	activeHash, _ := state["activeServiceHash"].(string)
	currentHash, _ := state["currentPodHash"].(string)
	if activeHash == "" {
		activeHash = currentHash
	}
	if activeHash == "" {
		namespace := target.Namespace
		if namespace == "" {
			namespace = defaultNamespace
		}
		return state, fmt.Errorf("rollout %s/%s weightSwitch could not determine active hash", namespace, target.Name)
	}
	return c.applyTargetTrafficForHash(ctx, defaultNamespace, target, state, activeHash)
}

func (c *Controller) applyControlActionTraffic(ctx context.Context, defaultNamespace, action string, target Target, state map[string]interface{}) (map[string]interface{}, error) {
	if action != "abort" {
		return c.applyTargetTraffic(ctx, defaultNamespace, target, state)
	}
	if trafficConfig(target) == nil {
		return state, nil
	}
	stableHash, _ := state["stableRS"].(string)
	if strings.TrimSpace(stableHash) == "" {
		namespace := target.Namespace
		if namespace == "" {
			namespace = defaultNamespace
		}
		return state, fmt.Errorf("rollout %s/%s abort weightSwitch could not determine stable hash", namespace, target.Name)
	}
	return c.applyTargetTrafficForHash(ctx, defaultNamespace, target, state, stableHash)
}

func (c *Controller) applyTargetTrafficForHash(ctx context.Context, defaultNamespace string, target Target, state map[string]interface{}, activeHash string) (map[string]interface{}, error) {
	namespace := target.Namespace
	if namespace == "" {
		namespace = defaultNamespace
	}
	provider, input, err := c.trafficTarget(ctx, namespace, target, activeHash)
	if err != nil {
		return state, err
	}
	var result traffic.Result
	if target.TrafficSnapshot != nil {
		result, err = provider.Restore(ctx, input, target.TrafficSnapshot)
	} else {
		result, err = provider.Apply(ctx, input)
	}
	if err != nil {
		return state, err
	}
	verified, err := provider.Verify(ctx, input)
	if err != nil {
		return state, err
	}
	if !verified {
		return state, fmt.Errorf("rollout %s/%s traffic provider has not converged", namespace, target.Name)
	}
	state["trafficProvider"] = trafficConfig(target).Provider
	state["trafficApplied"] = true
	state["trafficVerified"] = true
	state["weightSwitchApplied"] = true
	state["weightSwitchActiveHash"] = activeHash
	state["weightSwitchActivePods"] = result.ActivePods
	state["weightSwitchActiveReplicaSets"] = result.ActiveReplicaSets
	state["weightSwitchInactivePods"] = result.InactivePods
	state["weightSwitchInactiveReplicaSets"] = result.InactiveReplicaSets
	return state, nil
}

func appendSelector(baseSelector, expression string) string {
	if strings.TrimSpace(baseSelector) == "" {
		return expression
	}
	return fmt.Sprintf("%s,%s", baseSelector, expression)
}

func (c *Controller) observeTarget(ctx context.Context, namespace string, target Target) (map[string]interface{}, error) {
	ro, err := c.rolloutClient(namespace).Get(ctx, target.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	currentHash, _, _ := unstructured.NestedString(ro.Object, "status", "currentPodHash")
	activeHash, _, _ := unstructured.NestedString(ro.Object, "status", "blueGreen", "activeSelector")
	stableHash, _, _ := unstructured.NestedString(ro.Object, "status", "stableRS")
	phase, _, _ := unstructured.NestedString(ro.Object, "status", "phase")
	pauseConditions, _, _ := unstructured.NestedSlice(ro.Object, "status", "pauseConditions")
	aborted, _, _ := unstructured.NestedBool(ro.Object, "status", "abort")
	controllerPause, _, _ := unstructured.NestedBool(ro.Object, "status", "controllerPause")
	specPaused, _, _ := unstructured.NestedBool(ro.Object, "spec", "paused")
	currentStepIndex, _, _ := unstructured.NestedInt64(ro.Object, "status", "currentStepIndex")
	canarySteps, _, _ := unstructured.NestedSlice(ro.Object, "spec", "strategy", "canary", "steps")
	currentPauseTimed := false
	currentStepIsPause := false
	if currentStepIndex >= 0 && currentStepIndex < int64(len(canarySteps)) {
		if step, ok := canarySteps[currentStepIndex].(map[string]interface{}); ok {
			if pause, ok := step["pause"].(map[string]interface{}); ok {
				currentStepIsPause = true
				if duration, found := pause["duration"]; found && strings.TrimSpace(fmt.Sprint(duration)) != "" {
					currentPauseTimed = true
				}
			}
		}
	}
	currentStepAnalysisPhase, _, _ := unstructured.NestedString(ro.Object, "status", "canary", "currentStepAnalysisRunStatus", "status")
	canaryWeight, canaryWeightFound, _ := unstructured.NestedInt64(ro.Object, "status", "canary", "weights", "canary", "weight")
	canaryWeightVerified, canaryWeightVerifiedFound, _ := unstructured.NestedBool(ro.Object, "status", "canary", "weights", "verified")
	trafficOverrideHash := strings.TrimSpace(ro.GetAnnotations()[annotations.TrafficOverrideHashAnnotation])
	trafficOverrideSourceHash := strings.TrimSpace(ro.GetAnnotations()[annotations.TrafficOverrideSourceHashAnnotation])
	legacyAdoptionState := strings.TrimSpace(ro.GetAnnotations()[annotations.LegacyAdoptionStateAnnotation])
	legacyAdoptionDurablyPromoted := strings.EqualFold(strings.TrimSpace(ro.GetAnnotations()[annotations.LegacyAdoptionPromotedAnnotation]), "true")
	if legacyAdoptionDurablyPromoted {
		legacyAdoptionState = annotations.LegacyAdoptionStatePromoted
	}
	legacyAdoptionPending := !legacyAdoptionDurablyPromoted && (legacyAdoptionState == annotations.LegacyAdoptionStatePending || legacyAdoptionState == annotations.LegacyAdoptionStatePromoting)

	strategy := ""
	if blueGreen, found, _ := unstructured.NestedMap(ro.Object, "spec", "strategy", "blueGreen"); found && blueGreen != nil {
		strategy = "blueGreen"
	}
	if canary, found, _ := unstructured.NestedMap(ro.Object, "spec", "strategy", "canary"); found && canary != nil {
		strategy = "canary"
	}

	activeService := target.ActiveService
	if activeService == "" {
		if strategy == "canary" {
			activeService, _, _ = unstructured.NestedString(ro.Object, "spec", "strategy", "canary", "stableService")
		} else {
			activeService, _, _ = unstructured.NestedString(ro.Object, "spec", "strategy", "blueGreen", "activeService")
		}
	}
	previewService := target.PreviewService
	if previewService == "" {
		if strategy == "canary" {
			previewService, _, _ = unstructured.NestedString(ro.Object, "spec", "strategy", "canary", "canaryService")
		} else {
			previewService, _, _ = unstructured.NestedString(ro.Object, "spec", "strategy", "blueGreen", "previewService")
		}
	}

	serviceHash := ""
	if activeService != "" {
		svc, err := c.kubeClient.CoreV1().Services(namespace).Get(ctx, activeService, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return nil, err
		}
		if err == nil && svc.Spec.Selector != nil {
			serviceHash = svc.Spec.Selector[rolloutHashLabel]
		}
	}

	noPause := len(pauseConditions) == 0
	servicePromoted := serviceHash != "" && serviceHash == currentHash
	statusPromoted := activeHash != "" && activeHash == currentHash
	promoted := false
	if strategy == "canary" {
		promoted = currentHash != "" && stableHash == currentHash && noPause && !aborted && currentStepIndex >= int64(len(canarySteps)) && !legacyAdoptionPending
	} else {
		promoted = currentHash != "" && stableHash == currentHash && noPause && (servicePromoted || statusPromoted) && !legacyAdoptionPending
	}

	state := map[string]interface{}{
		"name":                      target.Name,
		"namespace":                 namespace,
		"rolloutUID":                string(ro.GetUID()),
		"strategy":                  strategy,
		"activeService":             activeService,
		"previewService":            previewService,
		"currentPodHash":            currentHash,
		"activeSelector":            activeHash,
		"stableRS":                  stableHash,
		"activeServiceHash":         serviceHash,
		"rolloutPhase":              phase,
		"pauseConditions":           len(pauseConditions),
		"currentStepIndex":          currentStepIndex,
		"stepCount":                 int64(len(canarySteps)),
		"currentPauseTimed":         currentPauseTimed,
		"aborted":                   aborted,
		"controllerPause":           controllerPause,
		"specPaused":                specPaused,
		"currentStepAnalysisPhase":  currentStepAnalysisPhase,
		"promoted":                  promoted,
		"trafficOverrideHash":       trafficOverrideHash,
		"trafficOverrideSourceHash": trafficOverrideSourceHash,
		"legacyAdoptionState":       legacyAdoptionState,
		"legacyAdoptionPending":     legacyAdoptionPending,
	}
	if target.Readiness != nil || target.ExpectedPodTemplateHash != "" {
		template, _, _ := unstructured.NestedMap(ro.Object, "spec", "template")
		ref, hasRef, _ := unstructured.NestedMap(ro.Object, "spec", "workloadRef")
		if hasRef && ref != nil {
			resource := schema.GroupVersionResource{Group: "apps", Version: "v1"}
			path := []string{"spec", "template"}
			switch fmt.Sprintf("%v/%v", ref["apiVersion"], ref["kind"]) {
			case "apps/v1/Deployment":
				resource.Resource = "deployments"
			case "apps/v1/ReplicaSet":
				resource.Resource = "replicasets"
			case "v1/PodTemplate":
				resource = schema.GroupVersionResource{Version: "v1", Resource: "podtemplates"}
				path = []string{"template"}
			default:
				return nil, fmt.Errorf("unsupported workloadRef for promotion readiness: %v", ref)
			}
			workload, getErr := c.dynamicClient.Resource(resource).Namespace(namespace).Get(ctx, fmt.Sprint(ref["name"]), metav1.GetOptions{})
			if getErr != nil {
				return nil, getErr
			}
			template, _, _ = unstructured.NestedMap(workload.Object, path...)
			state["workloadGeneration"] = workload.GetGeneration()
			state["workloadObservedGeneration"], _, _ = unstructured.NestedFieldNoCopy(ro.Object, "status", "workloadObservedGeneration")
		}
		encoded, marshalErr := json.Marshal(template)
		if marshalErr != nil {
			return nil, marshalErr
		}
		state["podTemplateHash"] = fmt.Sprintf("%x", sha256.Sum256(encoded))
		state["generation"] = ro.GetGeneration()
		state["observedGeneration"], _, _ = unstructured.NestedFieldNoCopy(ro.Object, "status", "observedGeneration")
		state["promotionPaused"] = false
		for _, condition := range pauseConditions {
			pause, _ := condition.(map[string]interface{})
			if (strategy == "canary" && currentStepIsPause && pause["reason"] == "CanaryPauseStep") || (strategy == "blueGreen" && pause["reason"] == "BlueGreenPause") {
				state["promotionPaused"] = true
			}
		}
	}
	if canaryWeightFound {
		state["canaryWeight"] = canaryWeight
	}
	if canaryWeightVerifiedFound {
		state["canaryWeightVerified"] = canaryWeightVerified
	}
	return state, nil
}

func (c *Controller) patchStatus(ctx context.Context, promotion *v1alpha1.RolloutPromotion, status map[string]interface{}) error {
	rawStatus, err := json.Marshal(status)
	if err != nil {
		return err
	}
	var typedStatus v1alpha1.RolloutPromotionStatus
	if err := json.Unmarshal(rawStatus, &typedStatus); err != nil {
		return fmt.Errorf("encode RolloutPromotion status: %w", err)
	}
	normalized, err := json.Marshal(typedStatus)
	if err != nil {
		return err
	}
	patch := map[string]interface{}{}
	if err := json.Unmarshal(normalized, &patch); err != nil {
		return err
	}
	// Merge-patch must explicitly clear nil/zero checkpoint fields. Omitting
	// them would leave the previous request's commitTime on a new Prepared
	// operation, which could make a readiness wait look already committed.
	for key, value := range status {
		if value == nil {
			patch[key] = nil
		}
	}
	if typedStatus.Operation != nil {
		op := patch["operation"].(map[string]interface{})
		op["attempt"] = typedStatus.Operation.Attempt
		if typedStatus.Operation.CommitTime == nil {
			op["commitTime"] = nil
		}
		if typedStatus.Operation.CompletionTime == nil {
			op["completionTime"] = nil
		}
	}
	data, err := json.Marshal(map[string]interface{}{"status": patch})
	if err != nil {
		return err
	}
	_, err = c.promotionClient(promotion.GetNamespace()).Patch(ctx, promotion.GetName(), types.MergePatchType, data, metav1.PatchOptions{}, "status")
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (c *Controller) rolloutClient(namespace string) dynamic.ResourceInterface {
	return c.dynamicClient.Resource(v1alpha1.RolloutGVR).Namespace(namespace)
}

func (c *Controller) promotionClient(namespace string) rollouttyped.RolloutPromotionInterface {
	return c.rolloutsClient.ArgoprojV1alpha1().RolloutPromotions(namespace)
}

func parseSpec(obj *unstructured.Unstructured) (RequestSpec, error) {
	var promotion v1alpha1.RolloutPromotion
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &promotion); err != nil {
		return RequestSpec{}, fmt.Errorf("decode RolloutPromotion: %w", err)
	}
	return normalizeSpec(promotion.Spec)
}

// A request must not exercise the controller's privileges in another namespace.
func validateTargetNamespaces(namespace string, targets []Target) error {
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		if target.Namespace != "" && target.Namespace != namespace {
			return fmt.Errorf("target %s must be in the RolloutPromotion namespace %s", target.Name, namespace)
		}
		if seen[target.Name] {
			return fmt.Errorf("duplicate rollout target %s", target.Name)
		}
		seen[target.Name] = true
	}
	return nil
}

func normalizeSpec(apiSpec v1alpha1.RolloutPromotionSpec) (RequestSpec, error) {
	requestID := strings.TrimSpace(apiSpec.RequestID)
	mode := apiSpec.Mode
	if mode == "" {
		mode = defaultMode
	}
	mode = strings.TrimSpace(mode)
	if mode != "together" && mode != "sequential" {
		return RequestSpec{}, fmt.Errorf("unsupported spec.mode %q, expected together or sequential", mode)
	}

	action := apiSpec.Action
	if action == "" {
		action = defaultAction
	}
	action = strings.TrimSpace(action)
	switch action {
	case "promote", "promoteFull", "abort", "retry", "rollback":
	default:
		return RequestSpec{}, fmt.Errorf("unsupported spec.action %q, expected promote, promoteFull, abort, retry, or rollback", action)
	}

	strategy := apiSpec.Strategy
	if strategy == "" {
		strategy = defaultStrategy
	}
	strategy = strings.TrimSpace(strategy)
	if strategy != "auto" && strategy != "blueGreen" && strategy != "canary" {
		return RequestSpec{}, fmt.Errorf("unsupported spec.strategy %q, expected auto, blueGreen, or canary", strategy)
	}

	waitEnabled := defaultWaitEnabled
	if apiSpec.Wait.Enabled != nil {
		waitEnabled = *apiSpec.Wait.Enabled
	}
	waitAttempts := defaultWaitAttempts
	if apiSpec.Wait.Attempts > 0 {
		waitAttempts = int(apiSpec.Wait.Attempts)
	}
	waitSeconds := defaultWaitSeconds
	if apiSpec.Wait.Seconds > 0 {
		waitSeconds = int(apiSpec.Wait.Seconds)
	}
	canaryCompletionPolicy := v1alpha1.RolloutPromotionCanaryCompletionNextPause
	if apiSpec.Canary != nil && strings.TrimSpace(apiSpec.Canary.CompletionPolicy) != "" {
		canaryCompletionPolicy = strings.TrimSpace(apiSpec.Canary.CompletionPolicy)
	}
	if canaryCompletionPolicy != v1alpha1.RolloutPromotionCanaryCompletionNextPause && canaryCompletionPolicy != v1alpha1.RolloutPromotionCanaryCompletionFullWeight {
		return RequestSpec{}, fmt.Errorf("unsupported spec.canary.completionPolicy %q, expected nextPause or completeAfterFullWeight", canaryCompletionPolicy)
	}
	if canaryCompletionPolicy == v1alpha1.RolloutPromotionCanaryCompletionFullWeight && !waitEnabled {
		return RequestSpec{}, fmt.Errorf("spec.wait.enabled must be true when spec.canary.completionPolicy is completeAfterFullWeight")
	}
	trafficRollback := TrafficRollbackOptions{Mode: v1alpha1.RolloutPromotionTrafficRollbackPrevious, HistoryLimit: v1alpha1.RolloutPromotionDefaultTrafficHistoryLimit}
	if apiSpec.TrafficRollback != nil {
		trafficRollback.Enabled = true
		if apiSpec.TrafficRollback.Enabled != nil {
			trafficRollback.Enabled = *apiSpec.TrafficRollback.Enabled
		}
		if mode := strings.TrimSpace(apiSpec.TrafficRollback.Mode); mode != "" {
			trafficRollback.Mode = mode
		}
		if apiSpec.TrafficRollback.HistoryLimit > 0 {
			trafficRollback.HistoryLimit = int(apiSpec.TrafficRollback.HistoryLimit)
		}
		if trafficRollback.Enabled {
			if trafficRollback.Mode != v1alpha1.RolloutPromotionTrafficRollbackPrevious {
				return RequestSpec{}, fmt.Errorf("unsupported spec.trafficRollback.mode %q, expected previousSnapshot", trafficRollback.Mode)
			}
		}
	}

	if len(apiSpec.Rollouts) == 0 {
		return RequestSpec{}, fmt.Errorf("spec.rollouts must contain at least one target")
	}

	targets := make([]Target, 0, len(apiSpec.Rollouts))
	for i, apiTarget := range apiSpec.Rollouts {
		name := apiTarget.Name
		if strings.TrimSpace(name) == "" {
			return RequestSpec{}, fmt.Errorf("spec.rollouts[%d].name is required", i)
		}
		targetStrategy := apiTarget.Strategy
		targetStrategy = strings.TrimSpace(targetStrategy)
		if targetStrategy == "" {
			targetStrategy = strategy
		}
		if targetStrategy != "auto" && targetStrategy != "blueGreen" && targetStrategy != "canary" {
			return RequestSpec{}, fmt.Errorf("unsupported spec.rollouts[%d].strategy %q, expected auto, blueGreen, or canary", i, targetStrategy)
		}
		toRevision := apiTarget.ToRevision
		if apiTarget.Readiness != nil {
			if apiTarget.Readiness.RequireCurrentPodHash && apiTarget.ExpectedCurrentPodHash == "" && (apiTarget.ExpectedPodTemplateHash == "" || apiTarget.ExpectedRolloutUID == "") {
				return RequestSpec{}, fmt.Errorf("spec.rollouts[%d].readiness requires an expectedCurrentPodHash or both expectedRolloutUID and expectedPodTemplateHash", i)
			}
			if action == "promote" && targetStrategy == "canary" && apiTarget.Readiness.RequirePaused && apiTarget.ExpectedCurrentStepIndex == nil {
				return RequestSpec{}, fmt.Errorf("spec.rollouts[%d].readiness.requirePaused requires expectedCurrentStepIndex for canary", i)
			}
			for _, step := range apiTarget.Readiness.AllowedCurrentStepIndexes {
				if step < 0 {
					return RequestSpec{}, fmt.Errorf("spec.rollouts[%d].readiness.allowedCurrentStepIndexes must be nonnegative", i)
				}
			}
		}
		rollbackTarget := strings.TrimSpace(apiTarget.RollbackTarget)
		if rollbackTarget != "" && rollbackTarget != v1alpha1.RolloutPromotionRollbackTargetStable {
			return RequestSpec{}, fmt.Errorf("unsupported spec.rollouts[%d].rollbackTarget %q, expected stable", i, rollbackTarget)
		}
		if action == "rollback" {
			if toRevision > 0 && rollbackTarget != "" {
				return RequestSpec{}, fmt.Errorf("spec.rollouts[%d].toRevision and rollbackTarget are mutually exclusive", i)
			}
			if toRevision <= 0 && rollbackTarget == "" {
				return RequestSpec{}, fmt.Errorf("spec.rollouts[%d] requires a positive toRevision or rollbackTarget=stable for rollback", i)
			}
		} else if rollbackTarget != "" {
			return RequestSpec{}, fmt.Errorf("spec.rollouts[%d].rollbackTarget is only valid for rollback", i)
		}
		weightSwitch, err := normalizeWeightSwitch(apiTarget.WeightSwitch)
		if err != nil {
			return RequestSpec{}, fmt.Errorf("spec.rollouts[%d].weightSwitch: %w", i, err)
		}
		if weightSwitch != nil && weightSwitch.Enabled {
			// A Helm rollback restores an older non-promote spec before its
			// post-rollback hook asks this same CR to restore a retained traffic
			// snapshot. Keep the switch configuration available for that request;
			// normal non-promote actions still never apply it directly.
			if action != "promote" && action != "promoteFull" && !trafficRollback.Enabled {
				return RequestSpec{}, fmt.Errorf("spec.rollouts[%d].weightSwitch is only valid for promote or promoteFull", i)
			}
			if !waitEnabled {
				return RequestSpec{}, fmt.Errorf("spec.wait.enabled must be true when spec.rollouts[%d].weightSwitch is enabled", i)
			}
		}
		if apiTarget.Traffic != nil && apiTarget.WeightSwitch != nil {
			return RequestSpec{}, fmt.Errorf("spec.rollouts[%d]: traffic and weightSwitch are mutually exclusive", i)
		}
		if apiTarget.Traffic != nil {
			if strings.TrimSpace(apiTarget.Traffic.Provider) == "" {
				return RequestSpec{}, fmt.Errorf("traffic.provider is required")
			}
			if !waitEnabled {
				return RequestSpec{}, fmt.Errorf("spec.wait.enabled must be true when traffic is configured")
			}
			if action != "promote" && action != "promoteFull" && !trafficRollback.Enabled {
				return RequestSpec{}, fmt.Errorf("traffic requires promote or promoteFull unless trafficRollback is enabled")
			}
		}
		if trafficRollback.Enabled && apiTarget.Traffic == nil && (weightSwitch == nil || !weightSwitch.Enabled) {
			return RequestSpec{}, fmt.Errorf("spec.rollouts[%d] requires traffic or enabled weightSwitch when spec.trafficRollback is enabled", i)
		}
		targets = append(targets, Target{
			Name:                      strings.TrimSpace(name),
			Namespace:                 strings.TrimSpace(apiTarget.Namespace),
			Strategy:                  targetStrategy,
			ActiveService:             strings.TrimSpace(apiTarget.ActiveService),
			PreviewService:            strings.TrimSpace(apiTarget.PreviewService),
			ExpectedRolloutUID:        strings.TrimSpace(apiTarget.ExpectedRolloutUID),
			ExpectedPodTemplateHash:   strings.TrimSpace(apiTarget.ExpectedPodTemplateHash),
			Readiness:                 apiTarget.Readiness.DeepCopy(),
			ExpectedCurrentPodHash:    strings.TrimSpace(apiTarget.ExpectedCurrentPodHash),
			ExpectedStableRS:          strings.TrimSpace(apiTarget.ExpectedStableRS),
			ExpectedActiveServiceHash: strings.TrimSpace(apiTarget.ExpectedActiveServiceHash),
			ExpectedCurrentStepIndex:  apiTarget.ExpectedCurrentStepIndex,
			ToRevision:                toRevision,
			RollbackTarget:            rollbackTarget,
			WeightSwitch:              weightSwitch,
			Traffic:                   apiTarget.Traffic.DeepCopy(),
		})
	}

	return RequestSpec{
		RequestID: requestID,
		Mode:      mode,
		Action:    action,
		Strategy:  strategy,
		Targets:   targets,
		Wait: WaitOptions{
			Enabled:  waitEnabled,
			Attempts: waitAttempts,
			Seconds:  waitSeconds,
		},
		CanaryCompletionPolicy: canaryCompletionPolicy,
		TrafficRollback:        trafficRollback,
	}, nil
}

func normalizeWeightSwitch(apiWeightSwitch *v1alpha1.RolloutPromotionWeightSwitch) (*WeightSwitch, error) {
	if apiWeightSwitch == nil {
		return nil, nil
	}

	enabled := true
	if apiWeightSwitch.Enabled != nil {
		enabled = *apiWeightSwitch.Enabled
	}
	if !enabled {
		return &WeightSwitch{Enabled: false}, nil
	}
	applyOnPromotion := true
	if apiWeightSwitch.ApplyOnPromotion != nil {
		applyOnPromotion = *apiWeightSwitch.ApplyOnPromotion
	}

	reapplyDelaySeconds := int(apiWeightSwitch.ReapplyDelaySeconds)
	if reapplyDelaySeconds < 0 {
		return nil, fmt.Errorf("reapplyDelaySeconds must be greater than or equal to 0")
	}

	annotationKey := apiWeightSwitch.AnnotationKey
	if strings.TrimSpace(annotationKey) == "" {
		return nil, fmt.Errorf("annotationKey is required when weightSwitch is enabled")
	}
	activeWeight := apiWeightSwitch.ActiveWeight
	if activeWeight == "" {
		return nil, fmt.Errorf("activeWeight is required when weightSwitch is enabled")
	}
	inactiveWeight := apiWeightSwitch.InactiveWeight
	if inactiveWeight == "" {
		return nil, fmt.Errorf("inactiveWeight is required when weightSwitch is enabled")
	}
	baseSelector := apiWeightSwitch.BaseSelector

	return &WeightSwitch{
		Enabled:             true,
		ApplyOnPromotion:    applyOnPromotion,
		ApplyOnPromotionSet: apiWeightSwitch.ApplyOnPromotion != nil,
		AnnotationKey:       strings.TrimSpace(annotationKey),
		BaseSelector:        strings.TrimSpace(baseSelector),
		ActiveWeight:        activeWeight,
		InactiveWeight:      inactiveWeight,
		ReapplyDelaySeconds: reapplyDelaySeconds,
	}, nil
}

func baseStatus(obj *v1alpha1.RolloutPromotion, phase, reason, message string) map[string]interface{} {
	now := metav1.Now().Format(time.RFC3339)
	return map[string]interface{}{
		"observedGeneration": obj.GetGeneration(),
		"phase":              phase,
		"startTime":          now,
		"conditions":         conditions("False", reason, message),
	}
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func appendProcessedRequest(values []string, value string) []string {
	if value == "" || containsString(values, value) {
		return values
	}
	result := append(append([]string(nil), values...), value)
	const maxProcessedRequests = 50
	if len(result) > maxProcessedRequests {
		result = result[len(result)-maxProcessedRequests:]
	}
	return result
}

func failedStatus(obj *v1alpha1.RolloutPromotion, message string) map[string]interface{} {
	status := baseStatus(obj, PhaseFailed, "InvalidSpec", message)
	status["completionTime"] = metav1.Now().Format(time.RFC3339)
	return status
}

func conditions(status, reason, message string) []interface{} {
	return []interface{}{
		map[string]interface{}{
			"type":               ConditionCompleted,
			"status":             status,
			"reason":             reason,
			"message":            message,
			"lastTransitionTime": metav1.Now().Format(time.RFC3339),
		},
	}
}

func targetStatuses(targets []Target, phase, message string) []interface{} {
	statuses := make([]interface{}, 0, len(targets))
	for _, target := range targets {
		entry := map[string]interface{}{
			"name":  target.Name,
			"phase": phase,
		}
		if target.Namespace != "" {
			entry["namespace"] = target.Namespace
		}
		if target.Strategy != "" {
			entry["strategy"] = target.Strategy
		}
		if target.ActiveService != "" {
			entry["activeService"] = target.ActiveService
		}
		if target.PreviewService != "" {
			entry["previewService"] = target.PreviewService
		}
		if target.ExpectedRolloutUID != "" {
			entry["expectedRolloutUID"] = target.ExpectedRolloutUID
		}
		if target.ExpectedCurrentPodHash != "" {
			entry["expectedCurrentPodHash"] = target.ExpectedCurrentPodHash
		}
		if target.ExpectedStableRS != "" {
			entry["expectedStableRS"] = target.ExpectedStableRS
		}
		if target.ExpectedActiveServiceHash != "" {
			entry["expectedActiveServiceHash"] = target.ExpectedActiveServiceHash
		}
		if target.ExpectedCurrentStepIndex != nil {
			entry["expectedCurrentStepIndex"] = *target.ExpectedCurrentStepIndex
		}
		if target.RollbackTarget != "" {
			entry["rollbackTarget"] = target.RollbackTarget
		}
		if target.ToRevision > 0 {
			entry["toRevision"] = target.ToRevision
		}
		if target.WeightSwitch != nil && target.WeightSwitch.Enabled {
			entry["weightSwitchEnabled"] = true
		}
		if config := trafficConfig(target); config != nil {
			entry["trafficProvider"] = config.Provider
		}
		if message != "" {
			entry["message"] = message
		}
		statuses = append(statuses, entry)
	}
	return statuses
}

func upsertTargetStatus(status map[string]interface{}, target Target, phase string, state map[string]interface{}, err error) map[string]interface{} {
	targets, _ := status["targets"].([]interface{})
	if targets == nil {
		targets = []interface{}{}
	}

	entry := map[string]interface{}{
		"name":  target.Name,
		"phase": phase,
	}
	if phase == "Promoted" || phase == PhaseSucceeded {
		entry["actionApplied"] = true
	}
	if phase == PhaseSucceeded {
		entry["verified"] = true
	}
	if target.Namespace != "" {
		entry["namespace"] = target.Namespace
	}
	if target.Strategy != "" {
		entry["strategy"] = target.Strategy
	}
	if target.ActiveService != "" {
		entry["activeService"] = target.ActiveService
	}
	if target.PreviewService != "" {
		entry["previewService"] = target.PreviewService
	}
	if target.ExpectedRolloutUID != "" {
		entry["expectedRolloutUID"] = target.ExpectedRolloutUID
	}
	if target.ExpectedCurrentPodHash != "" {
		entry["expectedCurrentPodHash"] = target.ExpectedCurrentPodHash
	}
	if target.ExpectedStableRS != "" {
		entry["expectedStableRS"] = target.ExpectedStableRS
	}
	if target.ExpectedActiveServiceHash != "" {
		entry["expectedActiveServiceHash"] = target.ExpectedActiveServiceHash
	}
	if target.ExpectedCurrentStepIndex != nil {
		entry["expectedCurrentStepIndex"] = *target.ExpectedCurrentStepIndex
	}
	if target.RollbackTarget != "" {
		entry["rollbackTarget"] = target.RollbackTarget
	}
	if target.ToRevision > 0 {
		entry["toRevision"] = target.ToRevision
	}
	if target.WeightSwitch != nil && target.WeightSwitch.Enabled {
		entry["weightSwitchEnabled"] = true
	}
	if config := trafficConfig(target); config != nil {
		entry["trafficProvider"] = config.Provider
	}
	for key, value := range state {
		entry[key] = value
	}
	if err != nil {
		entry["message"] = err.Error()
	}

	replaced := false
	for i, raw := range targets {
		existing, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if existing["name"] == target.Name {
			if target.Readiness != nil {
				for _, key := range []string{"baselineCurrentPodHash", "previousStepIndex", "actionApplied", "resolvedToRevision"} {
					if _, supplied := entry[key]; !supplied {
						if value, present := existing[key]; present {
							entry[key] = value
						}
					}
				}
			}
			targets[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		targets = append(targets, entry)
	}
	status["targets"] = targets
	return status
}

func targetNames(targets []Target) []string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Name)
	}
	return names
}

func terminalTargetPhase(err error) string {
	if err != nil {
		return PhaseFailed
	}
	return PhaseSucceeded
}

func promotedTargetPhase(err error) string {
	if err != nil {
		return PhaseFailed
	}
	return "Promoted"
}

func isTerminalPhase(phase string) bool {
	return phase == PhaseSucceeded || phase == PhaseFailed
}

func ServiceHash(service *corev1.Service) string {
	if service == nil || service.Spec.Selector == nil {
		return ""
	}
	return service.Spec.Selector[rolloutHashLabel]
}
