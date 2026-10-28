package statefulset

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/rolloutplugin"
	"github.com/argoproj/argo-rollouts/utils/resourceversion"
)

const (
	// FieldManager is the field manager name used for Server-Side Apply
	FieldManager = "argo-rollouts-statefulset-plugin"

	// blockedPartition holds every pod on CurrentRevision. It is far above any replica count so
	// pods added by a scale-up are also created on CurrentRevision, not on the UpdateRevision.
	blockedPartition int32 = math.MaxInt32
)

// This is a built-in plugin that runs in-process, avoiding RPC overhead
type Plugin struct {
	logCtx     *log.Entry
	restConfig *rest.Config
	client     client.Client
	// apiReader reads from the API server, for managedFields, which the cache strips.
	apiReader client.Reader

	// versionTracker records the resourceVersion of this plugin's latest partition patch per
	// StatefulSet UID, until the cache catches up with it. A cached StatefulSet from before the
	// patch still looks fully observed (observedGeneration == generation), so its stale status
	// could otherwise be reported as Ready, e.g. completing an abort whose rollback hasn't started.
	// versionTrackerOnce lazily creates it, so plugins built as struct literals get it too.
	versionTrackerOnce sync.Once
	versionTracker     *resourceversion.Tracker
}

// NewPlugin builds the plugin from the args of its resourcePlugins ConfigMap entry,
// Supported flags:
//
//	--loglevel   log level for this plugin only (defaults to the controller's level)
//	--logformat  log format for this plugin only, text|json (defaults to the controller's format)
//
// restConfig is the controller's, so the plugin's client honors its kubeconfig, context, QPS and
// burst flags. The client is created by Init.
func NewPlugin(logCtx *log.Entry, restConfig *rest.Config, args []string) (*Plugin, error) {
	fs := flag.NewFlagSet("statefulset", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	logLevel := fs.String("loglevel", "", "Set the logging level for the statefulset plugin. One of: debug|info|warn|error")
	logFormat := fs.String("logformat", "", "Set the logging format for the statefulset plugin. One of: text|json")
	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("invalid args for statefulset plugin: %w", err)
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("invalid args for statefulset plugin: unexpected positional arguments %v", fs.Args())
	}
	if *logLevel == "" && *logFormat == "" {
		return &Plugin{logCtx: logCtx, restConfig: restConfig}, nil
	}
	level := logCtx.Logger.GetLevel()
	if *logLevel != "" {
		var err error
		if level, err = log.ParseLevel(*logLevel); err != nil {
			return nil, fmt.Errorf("invalid args for statefulset plugin: %w", err)
		}
	}
	formatter := logCtx.Logger.Formatter
	switch strings.ToLower(*logFormat) {
	case "":
	case "json":
		formatter = &log.JSONFormatter{}
	case "text":
		formatter = &log.TextFormatter{FullTimestamp: true}
	default:
		return nil, fmt.Errorf("invalid args for statefulset plugin: unknown logformat %q, must be one of: text|json", *logFormat)
	}
	return &Plugin{logCtx: withLogger(logCtx, level, formatter), restConfig: restConfig}, nil
}

// withLogger returns a copy of logCtx backed by its own logger so the level and format apply
// to this plugin only;
func withLogger(logCtx *log.Entry, level log.Level, formatter log.Formatter) *log.Entry {
	base := logCtx.Logger
	l := &log.Logger{
		Out:          base.Out,
		Formatter:    formatter,
		Hooks:        base.Hooks,
		ReportCaller: base.ReportCaller,
		ExitFunc:     base.ExitFunc,
		Level:        level,
	}
	return log.NewEntry(l).WithFields(logCtx.Data)
}

func (p *Plugin) WatchedGVK() (schema.GroupVersionKind, error) {
	return appsv1.SchemeGroupVersion.WithKind("StatefulSet"), nil
}

// Validate rejects RolloutPlugins this plugin can't manage: the workloadRef must be an apps/v1
// StatefulSet, and spec.plugin.config must be empty since this plugin defines no config fields currently.
func (p *Plugin) Validate(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	ref := rolloutPlugin.Spec.WorkloadRef
	gvk := appsv1.SchemeGroupVersion.WithKind("StatefulSet")
	if ref.APIVersion != gvk.GroupVersion().String() || ref.Kind != gvk.Kind {
		return fmt.Errorf("spec.workloadRef must reference a %s %s, got %s %s", gvk.GroupVersion().String(), gvk.Kind, ref.APIVersion, ref.Kind)
	}
	if cfg := strings.TrimSpace(string(rolloutPlugin.Spec.Plugin.Config)); cfg != "" && cfg != "null" && cfg != "{}" {
		return fmt.Errorf("spec.plugin.config is not supported by the statefulset plugin currently, got %s", cfg)
	}
	return nil
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
	if err := p.client.Patch(ctx, stsPatch, client.Apply, client.ForceOwnership, client.FieldOwner(FieldManager)); err != nil {
		return err
	}
	// The patch response carries the StatefulSet's post-write resourceVersion.
	p.recordWrite(stsPatch)
	return nil
}

func (p *Plugin) getVersionTracker() *resourceversion.Tracker {
	p.versionTrackerOnce.Do(func() {
		p.versionTracker = resourceversion.NewTracker()
	})
	return p.versionTracker
}

// recordWrite remembers the resourceVersion returned by a successful write to sts, so its status
// isn't reported Ready until the cache has observed it.
func (p *Plugin) recordWrite(sts client.Object) {
	p.getVersionTracker().Record(string(sts.GetUID()), sts.GetResourceVersion())
}

// getStatefulSet reads the RolloutPlugin's StatefulSet from the plugin's cache.
func (p *Plugin) getStatefulSet(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) (*appsv1.StatefulSet, error) {
	if rolloutPlugin.Namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	sts := &appsv1.StatefulSet{}
	key := client.ObjectKey{Namespace: rolloutPlugin.Namespace, Name: rolloutPlugin.Spec.WorkloadRef.Name}
	if err := p.client.Get(ctx, key, sts); err != nil {
		return nil, fmt.Errorf("failed to get StatefulSet: %w", err)
	}
	return sts, nil
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

// desiredPartition returns the partition for weight% traffic. The StatefulSet controller counts
// the partition from spec.ordinals.start (pod index, not ordinal), so the start is not added here.
// A weight that updates no pods blocks the StatefulSet entirely.
func desiredPartition(replicas, weight int32) int32 {
	updated := updatedCountForWeight(replicas, weight)
	if updated <= 0 {
		return blockedPartition
	}
	return replicas - updated
}

func partitionOf(sts *appsv1.StatefulSet) int32 {
	if sts.Spec.UpdateStrategy.RollingUpdate != nil && sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
		return *sts.Spec.UpdateStrategy.RollingUpdate.Partition
	}
	return 0
}

func replicasOf(sts *appsv1.StatefulSet) int32 {
	if sts.Spec.Replicas != nil {
		return *sts.Spec.Replicas
	}
	return 1
}

// blockPartition patches the StatefulSet's partition to blockedPartition, so any pod the
// StatefulSet controller creates or recreates lands on CurrentRevision.
func (p *Plugin) blockPartition(ctx context.Context, sts *appsv1.StatefulSet) (bool, error) {
	if partitionOf(sts) == blockedPartition {
		return false, nil
	}
	if err := p.patchPartition(ctx, sts.Name, sts.Namespace, blockedPartition); err != nil {
		return false, err
	}
	return true, nil
}

// getPod returns the pod at the given index (ordinal start+index), or nil if it doesn't exist.
func (p *Plugin) getPod(ctx context.Context, sts *appsv1.StatefulSet, index int32) (*corev1.Pod, error) {
	podName := fmt.Sprintf("%s-%d", sts.Name, ordinalStart(sts)+index)
	pod := &corev1.Pod{}
	err := p.client.Get(ctx, client.ObjectKey{Name: podName, Namespace: sts.Namespace}, pod)
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s: %w", podName, err)
	}
	return pod, nil
}

// deletePod deletes the pod only if it is still the same object (UID precondition), so a stale
// cache can't delete the replacement the StatefulSet controller already created under the same name.
func (p *Plugin) deletePod(ctx context.Context, pod *corev1.Pod, reason string) error {
	p.logCtx.WithFields(log.Fields{"pod": pod.Name, "revision": pod.Labels[appsv1.StatefulSetRevisionLabel]}).Info(reason)
	uid := pod.UID
	if err := p.client.Delete(ctx, pod, client.Preconditions{UID: &uid}); err != nil && !errors.IsNotFound(err) && !errors.IsConflict(err) {
		return fmt.Errorf("failed to delete pod %s: %w", pod.Name, err)
	}
	return nil
}

// syncPodsToPartition does at most one pod deletion per call, highest index first, so the pods
// match the partition:
//   - a pod below the partition not on CurrentRevision (left by a lower setWeight, a scale-up, or
//     an earlier canary revision) is deleted, and the StatefulSet controller recreates it on
//     CurrentRevision;
//   - with OrderedReady pod management, a not-Ready pod at or above the partition on an outdated
//     revision is deleted. The StatefulSet controller won't replace a not-Ready pod itself, so
//     without this a broken canary blocks every later revision (the manual step Kubernetes
//     documents for a forced rollback).
//
// It waits while any pod is missing, terminating or not Ready, so it never takes down more than
// one pod at a time.
func (p *Plugin) syncPodsToPartition(ctx context.Context, sts *appsv1.StatefulSet, partition int32) error {
	currentRevision, updateRevision := sts.Status.CurrentRevision, sts.Status.UpdateRevision
	if sts.Status.ObservedGeneration != sts.Generation || currentRevision == "" || updateRevision == "" {
		return nil
	}
	orderedReady := sts.Spec.PodManagementPolicy != appsv1.ParallelPodManagement
	for i := replicasOf(sts) - 1; i >= 0; i-- {
		pod, err := p.getPod(ctx, sts, i)
		if err != nil {
			return err
		}
		if pod == nil || pod.DeletionTimestamp != nil {
			return nil
		}
		revision := pod.Labels[appsv1.StatefulSetRevisionLabel]
		ready := podReady(pod)
		if i >= partition && orderedReady && revision != updateRevision && !ready {
			return p.deletePod(ctx, pod, "Deleting not-Ready pod on an outdated revision so the StatefulSet can update it")
		}
		if i < partition && currentRevision != updateRevision && revision != currentRevision {
			return p.deletePod(ctx, pod, "Deleting pod below the partition to roll it back to CurrentRevision")
		}
		if !ready {
			return nil
		}
	}
	return nil
}

// podsBelowPartitionPending reports whether a pod below the partition is not yet back on
// CurrentRevision, i.e. syncPodsToPartition still has pods to roll back.
func (p *Plugin) podsBelowPartitionPending(ctx context.Context, sts *appsv1.StatefulSet, partition int32) (bool, error) {
	if sts.Status.CurrentRevision == sts.Status.UpdateRevision {
		return false, nil
	}
	for i := min(partition, replicasOf(sts)) - 1; i >= 0; i-- {
		pod, err := p.getPod(ctx, sts, i)
		if err != nil {
			return false, err
		}
		if pod == nil || pod.DeletionTimestamp != nil {
			return true, nil
		}
		if pod.Labels[appsv1.StatefulSetRevisionLabel] != sts.Status.CurrentRevision {
			return true, nil
		}
	}
	return false, nil
}

// Init creates the plugin's client from the controller's rest config. It reads StatefulSets and
// pods from the plugin's own cache, scoped to the controller's watch namespace and stopped with
// ctx, and writes to the API server.
func (p *Plugin) Init(ctx context.Context, namespace string) error {
	if p.restConfig == nil {
		return fmt.Errorf("statefulset plugin requires a Kubernetes rest config")
	}

	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add appsv1 to scheme: %w", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add corev1 to scheme: %w", err)
	}

	stripManagedFields := cache.TransformStripManagedFields()
	cacheOpts := cache.Options{
		Scheme: scheme,
		// Pods are read only for their labels, UID, deletion timestamp and status, so their spec
		// isn't cached; managedFields aren't read for any object.
		DefaultTransform: func(obj any) (any, error) {
			if pod, ok := obj.(*corev1.Pod); ok {
				pod.Spec = corev1.PodSpec{}
			}
			return stripManagedFields(obj)
		},
	}
	if namespace != metav1.NamespaceAll {
		cacheOpts.DefaultNamespaces = map[string]cache.Config{namespace: {}}
	}
	readCache, err := cache.New(p.restConfig, cacheOpts)
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}
	go func() {
		if err := readCache.Start(ctx); err != nil {
			p.logCtx.WithError(err).Error("Cache failed to start")
		}
	}()
	// Informers are added on first use; this only waits for the cache to start, since reads
	// fail before it has.
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if !readCache.WaitForCacheSync(startCtx) {
		return fmt.Errorf("cache did not start")
	}

	k8sClient, err := client.New(p.restConfig, client.Options{
		Scheme: scheme,
		Cache:  &client.CacheOptions{Reader: readCache},
	})
	if err != nil {
		return fmt.Errorf("failed to create k8s client: %w", err)
	}
	apiReader, err := client.New(p.restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("failed to create k8s API reader: %w", err)
	}
	p.client = k8sClient
	p.apiReader = apiReader
	p.logCtx.WithField("namespace", namespace).Info("StatefulSet plugin initialized")
	return nil
}

// GetResourceStatus gets the current status of the StatefulSet
func (p *Plugin) GetResourceStatus(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) (*rolloutplugin.ResourceStatus, error) {
	sts, err := p.getStatefulSet(ctx, rolloutPlugin)
	if err != nil {
		return nil, err
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

	// Ready requires the status to reflect the latest spec (observedGeneration caught up, and
	// the cache caught up with our own last patch), not just a stale ReadyReplicas count from
	// before the last change. While the revisions differ it also requires every pod to be on one
	// of them: a pod left on an earlier canary revision counts in neither, and an abort must roll
	// it back too.
	ready := p.getVersionTracker().Observed(string(sts.UID), sts.ResourceVersion) &&
		sts.Status.ObservedGeneration == sts.Generation && sts.Status.ReadyReplicas == replicas &&
		(currentRevision == updateRevision || sts.Status.CurrentReplicas+sts.Status.UpdatedReplicas == replicas)

	status := &rolloutplugin.ResourceStatus{
		Replicas:                replicas,
		UpdatedReplicas:         updatedReplicas,
		ReadyReplicas:           sts.Status.ReadyReplicas,
		AvailableReplicas:       sts.Status.AvailableReplicas,
		CurrentRevision:         currentRevision,
		UpdatedRevision:         updateRevision,
		Ready:                   ready,
		ObservedResourceVersion: sts.ResourceVersion,
	}

	p.logCtx.WithFields(log.Fields{
		"replicas":        replicas,
		"partition":       partition,
		"updatedReplicas": updatedReplicas,
		"readyReplicas":   sts.Status.ReadyReplicas,
		"ready":           ready,
	}).Debug("StatefulSet status retrieved")

	return status, nil
}

// SetWeight sets the partition for weight% using Server-Side Apply. It is called on every
// reconcile of a canary step, so once the partition is in place it also rolls back pods left on
// the UpdateRevision below it (a lower weight or a scale-up) and unblocks a stuck rollout,
// one pod per call.
func (p *Plugin) SetWeight(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, weight int32) error {
	sts, err := p.getStatefulSet(ctx, rolloutPlugin)
	if err != nil {
		return err
	}

	partition := desiredPartition(replicasOf(sts), weight)
	currentPartition := partitionOf(sts)
	if currentPartition == partition {
		return p.syncPodsToPartition(ctx, sts, partition)
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
func (p *Plugin) VerifyWeight(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin, weight int32) (bool, error) {
	sts, err := p.getStatefulSet(ctx, rolloutPlugin)
	if err != nil {
		return false, err
	}

	replicas := replicasOf(sts)
	partition := partitionOf(sts)
	expectedPartition := desiredPartition(replicas, weight)

	if partition != expectedPartition {
		p.logCtx.WithFields(log.Fields{
			"expected": expectedPartition,
			"actual":   partition,
		}).Info("Partition mismatch")
		return false, nil
	}

	// The StatefulSet controller must have observed the partition patch, otherwise its status
	// still describes the previous spec.
	if sts.Status.ObservedGeneration != sts.Generation {
		p.logCtx.WithFields(log.Fields{
			"generation":         sts.Generation,
			"observedGeneration": sts.Status.ObservedGeneration,
		}).Info("StatefulSet status not yet observed latest generation")
		return false, nil
	}

	// Pods below the partition must be back on CurrentRevision, otherwise more pods than the
	// weight allows still run a new revision.
	if pending, err := p.podsBelowPartitionPending(ctx, sts, partition); err != nil || pending {
		return false, err
	}

	expectedUpdated := updatedCountForWeight(replicas, weight)

	availableUpdated, err := p.availableUpdatedPods(ctx, sts, expectedPartition, replicas)
	if err != nil {
		return false, err
	}

	p.logCtx.WithFields(log.Fields{
		"expectedPartition": expectedPartition,
		"actualPartition":   partition,
		"expectedUpdated":   expectedUpdated,
		"updatedReplicas":   sts.Status.UpdatedReplicas,
		"availableUpdated":  availableUpdated,
		"totalReplicas":     replicas,
	}).Info("Weight verification")

	return availableUpdated >= expectedUpdated, nil
}

// availableUpdatedPods counts pods with index >= partition that are on the StatefulSet's
// UpdateRevision and Available.
func (p *Plugin) availableUpdatedPods(ctx context.Context, sts *appsv1.StatefulSet, partition, replicas int32) (int32, error) {
	updateRevision := sts.Status.UpdateRevision
	if updateRevision == "" {
		return 0, nil
	}
	now := metav1.Now()
	var available int32
	for i := max(partition, 0); i < replicas; i++ {
		pod, err := p.getPod(ctx, sts, i)
		if err != nil {
			return 0, err
		}
		if pod == nil || pod.Labels[appsv1.StatefulSetRevisionLabel] != updateRevision || pod.DeletionTimestamp != nil {
			continue
		}
		if podutil.IsPodAvailable(pod, sts.Spec.MinReadySeconds, now) {
			available++
		}
	}
	return available, nil
}

// PromoteFull rolls every pod to the revision being promoted (partition 0). The controller calls
// it on every reconcile until the rollout completes; once the StatefulSet has converged it puts
// the blocking partition back, so the next template change waits for the controller instead of
// rolling out to every pod immediately.
func (p *Plugin) PromoteFull(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	sts, err := p.getStatefulSet(ctx, rolloutPlugin)
	if err != nil {
		return fmt.Errorf("failed to promote StatefulSet: %w", err)
	}
	logCtx := p.logCtx.WithFields(log.Fields{"name": sts.Name, "namespace": sts.Namespace})

	// Until the StatefulSet controller observes the latest spec, its revisions may describe an
	// older template.
	if sts.Status.ObservedGeneration != sts.Generation {
		return nil
	}

	// A newer revision than the one being promoted must not be rolled out to every pod; hold it
	// until the controller starts a rollout for it.
	if sts.Status.UpdateRevision != rolloutPlugin.Status.UpdatedRevision {
		if _, err := p.blockPartition(ctx, sts); err != nil {
			return fmt.Errorf("failed to promote StatefulSet: %w", err)
		}
		return nil
	}

	replicas := replicasOf(sts)
	converged := sts.Status.CurrentRevision == sts.Status.UpdateRevision &&
		sts.Status.UpdatedReplicas == replicas && sts.Status.ReadyReplicas == replicas
	if converged {
		if patched, err := p.blockPartition(ctx, sts); err != nil {
			return fmt.Errorf("failed to promote StatefulSet: %w", err)
		} else if patched {
			logCtx.Info("Rollout promoted, partition blocked until the next rollout")
		}
		return nil
	}

	if partitionOf(sts) != 0 {
		logCtx.Info("Promoting rollout")
		if err := p.patchPartition(ctx, sts.Name, sts.Namespace, 0); err != nil {
			return fmt.Errorf("failed to promote StatefulSet: %w", err)
		}
		logCtx.WithField("partition", 0).Info("Successfully promoted rollout")
		return nil
	}
	return p.syncPodsToPartition(ctx, sts, 0)
}

// Abort rolls the StatefulSet back to CurrentRevision. It does at most one unit of work per
// call (patch the blocking partition, delete one stale pod, or wait on one pod's readiness) and
// returns without error when there's nothing left to do this call — so a multi-replica rollback
// never blocks Reconcile for more than one pod's worth of work. The caller determines overall
// completion. Progress is re-derived from live cluster state on every call rather than tracked separately,
// so it survives a controller restart mid-abort.
func (p *Plugin) Abort(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	sts, err := p.getStatefulSet(ctx, rolloutPlugin)
	if err != nil {
		return err
	}

	// Block further rollout so recreated pods land on CurrentRevision.
	if patched, err := p.blockPartition(ctx, sts); err != nil {
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

	// Highest-index pod not yet rolled back to targetRevision.
	for i := replicasOf(sts) - 1; i >= 0; i-- {
		pod, err := p.getPod(ctx, sts, i)
		if err != nil {
			return fmt.Errorf("abort: %w", err)
		}
		if pod == nil {
			// StatefulSet controller hasn't recreated it yet.
			return nil
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

		return p.deletePod(ctx, pod, "Deleting pod on new revision to force rollback")
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

// Restart blocks the StatefulSet's partition so the restarted rollout begins from step 0 with
// no pods updated.
func (p *Plugin) Restart(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	sts, err := p.getStatefulSet(ctx, rolloutPlugin)
	if err != nil {
		return err
	}
	p.logCtx.WithFields(log.Fields{
		"name":      sts.Name,
		"namespace": sts.Namespace,
	}).Info("Restarting StatefulSet for restart")

	if _, err := p.blockPartition(ctx, sts); err != nil {
		return fmt.Errorf("failed to restart StatefulSet: %w", err)
	}

	p.logCtx.Info("Successfully restarted StatefulSet")
	return nil
}

// DeletionCleanup drops this plugin's Server-Side Apply ownership of the partition and leaves its
// value as it is. Another owner (e.g. Argo CD, ignoring fields this plugin manages) then sees the
// partition as drift and can restore its own value.
func (p *Plugin) DeletionCleanup(ctx context.Context, rolloutPlugin *v1alpha1.RolloutPlugin) error {
	ref := rolloutPlugin.Spec.WorkloadRef
	if ref.APIVersion != appsv1.SchemeGroupVersion.String() || ref.Kind != "StatefulSet" {
		return nil
	}
	sts := &appsv1.StatefulSet{}
	if err := p.apiReader.Get(ctx, client.ObjectKey{Namespace: rolloutPlugin.Namespace, Name: ref.Name}, sts); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get StatefulSet: %w", err)
	}
	managedFields := slices.DeleteFunc(slices.Clone(sts.ManagedFields), func(e metav1.ManagedFieldsEntry) bool {
		return e.Manager == FieldManager
	})
	if len(managedFields) == len(sts.ManagedFields) {
		return nil
	}
	if len(managedFields) == 0 {
		// The API server ignores an empty list; a single empty entry clears them all.
		managedFields = []metav1.ManagedFieldsEntry{{}}
	}
	// A non-apply write that only changes managedFields replaces them as given and records no
	// new owner.
	patch := client.MergeFromWithOptions(sts.DeepCopy(), client.MergeFromWithOptimisticLock{})
	sts.ManagedFields = managedFields
	if err := p.client.Patch(ctx, sts, patch); err != nil {
		return fmt.Errorf("failed to drop partition ownership: %w", err)
	}
	p.recordWrite(sts)
	p.logCtx.WithFields(log.Fields{"name": sts.Name, "namespace": sts.Namespace}).Info("Dropped partition ownership")
	return nil
}

// Ensure Plugin implements the controller's ResourcePlugin interface
var _ rolloutplugin.ResourcePlugin = &Plugin{}
