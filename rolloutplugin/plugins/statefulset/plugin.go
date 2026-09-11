package statefulset

import (
	"context"
	"fmt"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rolloutplugin"
)

const (
	// FieldManager is the field manager name used for Server-Side Apply
	FieldManager = "argo-rollouts-statefulset-plugin"
)

// This is a built-in plugin that runs in-process, avoiding RPC overhead
type Plugin struct {
	logCtx *log.Entry
	client client.Client
}

func NewPlugin(logCtx *log.Entry) *Plugin {
	return &Plugin{logCtx: logCtx}
}

func (p *Plugin) WatchedGVK() (schema.GroupVersionKind, error) {
	return appsv1.SchemeGroupVersion.WithKind("StatefulSet"), nil
}

// WatchObject supplies a concrete Go type so SetupWithManager can register a typed watch.
func (p *Plugin) WatchObject() client.Object {
	return &appsv1.StatefulSet{}
}

// WatchPredicate filters StatefulSet watch events down to ones that actually indicate progress
// (spec change, revision change, or replica-count change), so periodic resyncs and other
// status-only churn don't trigger a reconcile.
func (p *Plugin) WatchPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldSts, ok1 := e.ObjectOld.(*appsv1.StatefulSet)
			newSts, ok2 := e.ObjectNew.(*appsv1.StatefulSet)
			if !ok1 || !ok2 {
				return true
			}

			// Skip if ResourceVersion is the same (periodic resync)
			if oldSts.ResourceVersion == newSts.ResourceVersion {
				return false
			}

			// Trigger reconcile if spec changed (generation changed)
			if oldSts.Generation != newSts.Generation {
				return true
			}

			// Trigger reconcile if revision changed (rollout in progress)
			if oldSts.Status.CurrentRevision != newSts.Status.CurrentRevision ||
				oldSts.Status.UpdateRevision != newSts.Status.UpdateRevision {
				return true
			}

			// Trigger reconcile if replica counts changed
			if oldSts.Status.ReadyReplicas != newSts.Status.ReadyReplicas ||
				oldSts.Status.UpdatedReplicas != newSts.Status.UpdatedReplicas ||
				oldSts.Status.AvailableReplicas != newSts.Status.AvailableReplicas {
				return true
			}

			// Skip other status-only updates
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
	}
}

// patchPartition sets spec.updateStrategy.rollingUpdate.partition on the StatefulSet using
// Server-Side Apply. The patch carries ONLY the partition field so it does not fight with
// other owners (e.g. ArgoCD) over the rest of the spec.
func (p *Plugin) patchPartition(ctx context.Context, name, namespace string, partition int32) error {
	stsPatch := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "apps/v1",
			"kind":       "StatefulSet",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"updateStrategy": map[string]any{
					"rollingUpdate": map[string]any{
						"partition": partition,
					},
				},
			},
		},
	}
	return p.client.Patch(ctx, stsPatch, client.Apply, client.ForceOwnership, client.FieldOwner(FieldManager))
}

// rejectOnDelete errors out on a StatefulSet with updateStrategy: OnDelete. apps/v1 rejects a
// rollingUpdate.partition patch against such a StatefulSet, and partition-based canary has no
// meaning under OnDelete (the StatefulSet controller never rolls pods on its own).
func rejectOnDelete(sts *appsv1.StatefulSet) error {
	if sts.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType {
		return fmt.Errorf("StatefulSet %s/%s uses updateStrategy OnDelete, which RolloutPlugin's statefulset plugin does not support (requires RollingUpdate)", sts.Namespace, sts.Name)
	}
	return nil
}

// ordinalStart returns the StatefulSet's starting ordinal (0 unless spec.ordinals.start is set).
func ordinalStart(sts *appsv1.StatefulSet) int32 {
	if sts.Spec.Ordinals != nil {
		return sts.Spec.Ordinals.Start
	}
	return 0
}

// ceilDiv returns ceil(numerator/denominator) for non-negative int32s, without float conversion.
func ceilDiv(numerator, denominator int32) int32 {
	return (numerator + denominator - 1) / denominator
}

// updatedCountForWeight returns how many pods should be on the new revision for weight% traffic.
// Rounded up so any weight > 0 updates at least one pod (a floored count would round small
// weights down to zero pods updated, while still reporting the step as verified).
func updatedCountForWeight(replicas, weight int32) int32 {
	return ceilDiv(replicas*weight, 100)
}

// desiredPartition returns the partition value for weight% traffic on a StatefulSet with the
// given ordinal start and replica count.
func desiredPartition(start, replicas, weight int32) int32 {
	return start + replicas - updatedCountForWeight(replicas, weight)
}

// blockPartition patches the StatefulSet's partition above the highest real ordinal
// (start+replicas, not just replicas — a pod's ordinal is start+i), so any pod the StatefulSet
// controller recreates lands on CurrentRevision instead of the still-referenced UpdateRevision.
func (p *Plugin) blockPartition(ctx context.Context, sts *appsv1.StatefulSet, replicas int32) (bool, error) {
	blockingPartition := ordinalStart(sts) + replicas
	currentPartition := int32(0)
	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		currentPartition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
	}
	if currentPartition == blockingPartition {
		return false, nil
	}
	if err := p.patchPartition(ctx, sts.Name, sts.Namespace, blockingPartition); err != nil {
		return false, err
	}
	return true, nil
}

// Init initializes the plugin by creating a k8s client with informer cache.
func (p *Plugin) Init(namespace string) error {
	p.logCtx.Info("Initializing StatefulSet plugin")

	config, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get k8s config: %w", err)
	}

	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add appsv1 to scheme: %w", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add corev1 to scheme: %w", err)
	}

	cacheOpts := cache.Options{
		Scheme: scheme,
	}

	if namespace != "" && namespace != metav1.NamespaceAll {
		p.logCtx.WithField("namespace", namespace).Info("Creating namespaced cache for StatefulSet plugin")
		cacheOpts.DefaultNamespaces = map[string]cache.Config{
			namespace: {},
		}
	} else {
		p.logCtx.Info("Creating cluster-wide cache for StatefulSet plugin")
	}

	cacheInstance, err := cache.New(config, cacheOpts)
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}

	// The cache must run for the lifetime of the plugin (the whole controller process),
	// so it is started on a background context that is never cancelled.
	go func() {
		if err := cacheInstance.Start(context.Background()); err != nil {
			p.logCtx.WithError(err).Error("Cache failed to start")
		}
	}()

	// Wait for cache to sync with a timeout context
	syncCtx, syncCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer syncCancel()

	p.logCtx.Info("Waiting for cache to sync...")
	if !cacheInstance.WaitForCacheSync(syncCtx) {
		return fmt.Errorf("failed to sync cache within timeout")
	}
	p.logCtx.Info("Cache synced successfully")

	// client that reads from cache and writes to API server
	k8sClient, err := client.New(config, client.Options{
		Scheme: scheme,
		Cache: &client.CacheOptions{
			Reader: cacheInstance,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}

	p.client = k8sClient
	p.logCtx.Info("StatefulSet plugin initialized successfully with cache")

	return nil
}

// GetResourceStatus gets the current status of the StatefulSet
func (p *Plugin) GetResourceStatus(ctx context.Context, namespace string, workloadRef v1alpha1.WorkloadRef) (*rolloutplugin.ResourceStatus, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}

	sts := &appsv1.StatefulSet{}
	err := p.client.Get(ctx, client.ObjectKey{
		Name:      workloadRef.Name,
		Namespace: namespace,
	}, sts)
	if err != nil {
		return nil, fmt.Errorf("failed to get StatefulSet from cache: %w", err)
	}

	if err := rejectOnDelete(sts); err != nil {
		return nil, err
	}

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	partition := int32(0)
	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		partition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
	}

	// Actual rolled-out pods, not the just-patched partition target (which would report the
	// update as done before any pod has actually rolled). Clamped since a scale-down while
	// partition > replicas could otherwise report more updated pods than exist.
	updatedReplicas := min(sts.Status.UpdatedReplicas, replicas)

	currentRevision := sts.Status.CurrentRevision
	updateRevision := sts.Status.UpdateRevision

	// Ready requires the status to reflect the latest spec (observedGeneration caught up),
	// not just a stale ReadyReplicas count from before the last change.
	ready := sts.Status.ObservedGeneration == sts.Generation && sts.Status.ReadyReplicas == replicas

	status := &rolloutplugin.ResourceStatus{
		Replicas:          replicas,
		UpdatedReplicas:   updatedReplicas,
		ReadyReplicas:     sts.Status.ReadyReplicas,
		AvailableReplicas: sts.Status.AvailableReplicas,
		CurrentRevision:   currentRevision,
		UpdatedRevision:   updateRevision,
		Ready:             ready,
	}

	p.logCtx.WithFields(log.Fields{
		"replicas":        replicas,
		"partition":       partition,
		"updatedReplicas": updatedReplicas,
		"readyReplicas":   sts.Status.ReadyReplicas,
		"ready":           ready,
	}).Debug("StatefulSet status retrieved from cache")

	return status, nil
}

// SetWeight sets the canary weight by adjusting the partition field using Server-Side Apply
func (p *Plugin) SetWeight(ctx context.Context, namespace string, workloadRef v1alpha1.WorkloadRef, weight int32) error {

	// Get the StatefulSet from cache
	sts := &appsv1.StatefulSet{}
	err := p.client.Get(ctx, client.ObjectKey{
		Name:      workloadRef.Name,
		Namespace: namespace,
	}, sts)
	if err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	partition := desiredPartition(ordinalStart(sts), replicas, weight)

	currentPartition := int32(0)
	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		currentPartition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
	}

	if currentPartition == partition {
		// Partition already set, no need to patch
		return nil
	}

	p.logCtx.WithFields(log.Fields{
		"currentPartition": currentPartition,
		"desiredPartition": partition,
		"weight":           weight,
	}).Debug("Partition needs update")

	if err := p.patchPartition(ctx, sts.Name, sts.Namespace, partition); err != nil {
		return fmt.Errorf("failed to update StatefulSet partition: %w", err)
	}

	p.logCtx.WithField("partition", partition).Info("Successfully set partition")
	return nil
}

// VerifyWeight verifies that the canary weight has been achieved
func (p *Plugin) VerifyWeight(ctx context.Context, namespace string, workloadRef v1alpha1.WorkloadRef, weight int32) (bool, error) {

	// Get the StatefulSet from cache
	sts := &appsv1.StatefulSet{}
	err := p.client.Get(ctx, client.ObjectKey{
		Name:      workloadRef.Name,
		Namespace: namespace,
	}, sts)
	if err != nil {
		return false, fmt.Errorf("failed to get StatefulSet from cache: %w", err)
	}

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	partition := int32(0)
	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		partition = *sts.Spec.UpdateStrategy.RollingUpdate.Partition
	}

	expectedPartition := desiredPartition(ordinalStart(sts), replicas, weight)

	if partition != expectedPartition {
		p.logCtx.WithFields(log.Fields{
			"expected": expectedPartition,
			"actual":   partition,
		}).Info("Partition mismatch")
		return false, nil
	}

	expectedUpdated := updatedCountForWeight(replicas, weight)

	// Get actual updated replicas from StatefulSet status
	actualUpdated := sts.Status.UpdatedReplicas

	p.logCtx.WithFields(log.Fields{
		"expectedPartition": expectedPartition,
		"actualPartition":   partition,
		"expectedUpdated":   expectedUpdated,
		"actualUpdated":     actualUpdated,
		"readyReplicas":     sts.Status.ReadyReplicas,
		"totalReplicas":     replicas,
	}).Info("Weight verification")

	verified := partition == expectedPartition && actualUpdated >= expectedUpdated

	return verified, nil
}

// PromoteFull completes the rollout by setting partition to 0
func (p *Plugin) PromoteFull(ctx context.Context, namespace string, workloadRef v1alpha1.WorkloadRef) error {
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": namespace,
	}).Info("Promoting rollout")

	partition := int32(0)
	if err := p.patchPartition(ctx, workloadRef.Name, namespace, partition); err != nil {
		return fmt.Errorf("failed to promote StatefulSet: %w", err)
	}

	p.logCtx.WithField("partition", partition).Info("Successfully promoted rollout")
	return nil
}

// Abort rolls the StatefulSet back to CurrentRevision. It does at most one unit of work per
// call (patch the blocking partition, delete one stale pod, or wait on one pod's readiness) and
// returns without error when there's nothing left to do this call — so a multi-replica rollback
// never blocks Reconcile for more than one pod's worth of work. The caller determines overall
// completion. Progress is re-derived from live cluster state on every call rather than tracked separately,
// so it survives a controller restart mid-abort.
func (p *Plugin) Abort(ctx context.Context, namespace string, workloadRef v1alpha1.WorkloadRef) error {
	sts := &appsv1.StatefulSet{}
	if err := p.client.Get(ctx, client.ObjectKey{
		Name:      workloadRef.Name,
		Namespace: namespace,
	}, sts); err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}
	start := ordinalStart(sts)

	// Block further rollout so recreated pods land on CurrentRevision.
	if patched, err := p.blockPartition(ctx, sts, replicas); err != nil {
		return fmt.Errorf("failed to update StatefulSet during abort: %w", err)
	} else if patched {
		return nil
	}

	targetRevision := sts.Status.CurrentRevision
	if targetRevision == "" {
		// Not yet reported by the StatefulSet controller; wait rather than delete pods against
		// an unknown target revision.
		return nil
	}

	// Highest-ordinal pod not yet rolled back to targetRevision.
	for i := start + replicas - 1; i >= start; i-- {
		podName := fmt.Sprintf("%s-%d", sts.Name, i)
		pod := &corev1.Pod{}
		err := p.client.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod)
		if errors.IsNotFound(err) {
			// StatefulSet controller hasn't recreated it yet.
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to get pod %s during abort: %w", podName, err)
		}

		if pod.Labels[appsv1.StatefulSetRevisionLabel] == targetRevision {
			if !podReady(pod) {
				return nil // recreated on the right revision, waiting for it to become Ready
			}
			continue // already rolled back and ready
		}

		if pod.DeletionTimestamp != nil {
			return nil // deletion in flight, wait for the replacement
		}

		p.logCtx.WithField("pod", podName).Info("Deleting pod on new revision to force rollback")
		if err := p.client.Delete(ctx, pod); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete pod %s during abort: %w", podName, err)
		}
		return nil
	}

	p.logCtx.Info("Abort rollback complete, all pods on CurrentRevision and Ready")
	return nil
}

// podReady reports whether a pod has PodReady=True.
func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// Restart returns the StatefulSet to baseline state (partition = replicas) for restarts
func (p *Plugin) Restart(ctx context.Context, namespace string, workloadRef v1alpha1.WorkloadRef) error {
	p.logCtx.WithFields(log.Fields{
		"name":      workloadRef.Name,
		"namespace": namespace,
	}).Info("Restarting StatefulSet for restart")

	sts := &appsv1.StatefulSet{}
	err := p.client.Get(ctx, client.ObjectKey{
		Name:      workloadRef.Name,
		Namespace: namespace,
	}, sts)
	if err != nil {
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}

	replicas := int32(1)
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	if _, err := p.blockPartition(ctx, sts, replicas); err != nil {
		return fmt.Errorf("failed to restart StatefulSet: %w", err)
	}

	p.logCtx.WithFields(log.Fields{
		"replicas": replicas,
	}).Info("Successfully restarted StatefulSet")

	return nil
}

// Ensure Plugin implements the controller's ResourcePlugin interface
var _ rolloutplugin.ResourcePlugin = &Plugin{}
