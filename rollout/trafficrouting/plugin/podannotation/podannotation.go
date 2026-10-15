package podannotation

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	appsv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/annotations"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
)

const (
	PluginName                = "argoproj/pod-annotation-traffic-router"
	defaultMaxPodWeight       = 10
	defaultWeightCalculation  = "aggregatePerPod"
	defaultWeightPlaceholder  = "__ROLLOUT_WEIGHT__"
	rolloutPodTemplateHashKey = v1alpha1.DefaultRolloutUniqueLabelKey
)

type Config struct {
	AnnotationKey          string `json:"annotationKey"`
	BaseSelector           string `json:"baseSelector"`
	MaxPodWeight           int    `json:"maxPodWeight"`
	TrafficWeightScale     int    `json:"trafficWeightScale"`
	WeightCalculation      string `json:"weightCalculation"`
	WeightPlaceholder      string `json:"weightPlaceholder"`
	PrettyJSON             *bool  `json:"prettyJSON"`
	StableWeightTemplate   string `json:"stableWeightTemplate"`
	CanaryWeightTemplate   string `json:"canaryWeightTemplate"`
	PromotedWeightTemplate string `json:"promotedWeightTemplate"`
	InactiveWeightTemplate string `json:"inactiveWeightTemplate"`
	RoleLabelKey           string `json:"roleLabelKey"`
	StableRole             string `json:"stableRole"`
	CanaryRole             string `json:"canaryRole"`
	InactiveRole           string `json:"inactiveRole"`
	PatchPods              *bool  `json:"patchPods"`
	PatchReplicaSets       *bool  `json:"patchReplicaSets"`
}

type Reconciler struct {
	kubeClient kubernetes.Interface
}

type legacyAdoptionConfig struct {
	baseSelector    string
	annotationKey   string
	activeWeight    string
	candidateWeight string
	legacyWeight    string
	inactiveWeight  string
}

func NewReconciler(kubeClient kubernetes.Interface) *Reconciler {
	return &Reconciler{kubeClient: kubeClient}
}

func (r *Reconciler) InitPlugin() pluginTypes.RpcError {
	if r.kubeClient != nil {
		return pluginTypes.RpcError{}
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return rpcError(err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return rpcError(err)
	}
	r.kubeClient = client
	return pluginTypes.RpcError{}
}

func (r *Reconciler) UpdateHash(rollout *v1alpha1.Rollout, canaryHash, stableHash string, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	if len(additionalDestinations) > 0 {
		return rpcError(fmt.Errorf("%s does not support additional traffic destinations", PluginName))
	}
	config, err := configForRollout(rollout)
	if err != nil {
		return rpcError(err)
	}
	if LegacyAdoptionPending(rollout) {
		if err := r.ReconcileLegacyAdoptionPending(context.Background(), rollout); err != nil {
			return rpcError(err)
		}
		return pluginTypes.RpcError{}
	}
	if legacyAdoptionPromoting(rollout) {
		return pluginTypes.RpcError{}
	}
	if err := r.reconcilePromotedLegacyInactive(context.Background(), rollout); err != nil {
		return rpcError(err)
	}
	if overrideHash := trafficOverrideHash(rollout); overrideHash != "" {
		_, candidateHash, nextRevision := trafficOverrideNextRevision(rollout, canaryHash)
		if nextRevision {
			if err := r.applyNextRevisionAfterTrafficRollback(context.Background(), rollout, config, overrideHash, candidateHash); err != nil {
				return rpcError(err)
			}
			return pluginTypes.RpcError{}
		}
		if err := r.applyTrafficOverride(context.Background(), rollout, config, overrideHash); err != nil {
			return rpcError(err)
		}
		return pluginTypes.RpcError{}
	}
	if legacyAdoptionBaselinePromoted(rollout) {
		if err := r.PromoteLegacyAdoption(context.Background(), rollout); err != nil {
			return rpcError(err)
		}
		return pluginTypes.RpcError{}
	}
	if stableHash != "" {
		if err := r.patchRole(context.Background(), rollout.Namespace, config, stableHash, config.StableRole); err != nil {
			return rpcError(err)
		}
	}
	if canaryHash != "" && canaryHash != stableHash {
		if err := r.patchRole(context.Background(), rollout.Namespace, config, canaryHash, config.CanaryRole); err != nil {
			return rpcError(err)
		}
	}
	return pluginTypes.RpcError{}
}

func (r *Reconciler) SetWeight(rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	if len(additionalDestinations) > 0 {
		return rpcError(fmt.Errorf("%s does not support additional traffic destinations", PluginName))
	}
	config, err := configForRollout(rollout)
	if err != nil {
		return rpcError(err)
	}
	if LegacyAdoptionPending(rollout) {
		if err := r.ReconcileLegacyAdoptionPending(context.Background(), rollout); err != nil {
			return rpcError(err)
		}
		return pluginTypes.RpcError{}
	}
	if legacyAdoptionPromoting(rollout) {
		return pluginTypes.RpcError{}
	}
	if err := r.reconcilePromotedLegacyInactive(context.Background(), rollout); err != nil {
		return rpcError(err)
	}
	if overrideHash := trafficOverrideHash(rollout); overrideHash != "" {
		_, candidateHash, nextRevision := trafficOverrideNextRevision(rollout, "")
		if nextRevision {
			if err := r.applyNextRevisionAfterTrafficRollback(context.Background(), rollout, config, overrideHash, candidateHash); err != nil {
				return rpcError(err)
			}
			return pluginTypes.RpcError{}
		}
		if err := r.applyTrafficOverride(context.Background(), rollout, config, overrideHash); err != nil {
			return rpcError(err)
		}
		return pluginTypes.RpcError{}
	}
	if legacyAdoptionBaselinePromoted(rollout) {
		if err := r.PromoteLegacyAdoption(context.Background(), rollout); err != nil {
			return rpcError(err)
		}
		return pluginTypes.RpcError{}
	}
	if err := r.applyDesiredWeight(context.Background(), rollout, config, desiredWeight); err != nil {
		return rpcError(err)
	}
	return pluginTypes.RpcError{}
}

func (r *Reconciler) VerifyWeight(rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) (pluginTypes.RpcVerified, pluginTypes.RpcError) {
	if len(additionalDestinations) > 0 {
		return pluginTypes.NotVerified, rpcError(fmt.Errorf("%s does not support additional traffic destinations", PluginName))
	}
	config, err := configForRollout(rollout)
	if err != nil {
		return pluginTypes.NotVerified, rpcError(err)
	}
	if LegacyAdoptionPending(rollout) {
		verified, err := r.VerifyLegacyAdoptionPending(context.Background(), rollout)
		if err != nil {
			return pluginTypes.NotVerified, rpcError(err)
		}
		if verified {
			return pluginTypes.Verified, pluginTypes.RpcError{}
		}
		return pluginTypes.NotVerified, pluginTypes.RpcError{}
	}
	if legacyAdoptionPromoting(rollout) {
		return pluginTypes.NotVerified, pluginTypes.RpcError{}
	}
	if verified, err := r.verifyPromotedLegacyInactive(context.Background(), rollout); err != nil {
		return pluginTypes.NotVerified, rpcError(err)
	} else if !verified {
		return pluginTypes.NotVerified, pluginTypes.RpcError{}
	}
	if overrideHash := trafficOverrideHash(rollout); overrideHash != "" {
		_, candidateHash, nextRevision := trafficOverrideNextRevision(rollout, "")
		if nextRevision {
			verified, err := r.verifyNextRevisionAfterTrafficRollback(context.Background(), rollout, config, overrideHash, candidateHash)
			if err != nil {
				return pluginTypes.NotVerified, rpcError(err)
			}
			if verified {
				return pluginTypes.Verified, pluginTypes.RpcError{}
			}
			return pluginTypes.NotVerified, pluginTypes.RpcError{}
		}
		verified, err := r.verifyTrafficOverride(context.Background(), rollout, config, overrideHash)
		if err != nil {
			return pluginTypes.NotVerified, rpcError(err)
		}
		if verified {
			return pluginTypes.Verified, pluginTypes.RpcError{}
		}
		return pluginTypes.NotVerified, pluginTypes.RpcError{}
	}
	if legacyAdoptionBaselinePromoted(rollout) {
		verified, err := r.VerifyLegacyAdoptionPromoted(context.Background(), rollout)
		if err != nil {
			return pluginTypes.NotVerified, rpcError(err)
		}
		if verified {
			return pluginTypes.Verified, pluginTypes.RpcError{}
		}
		return pluginTypes.NotVerified, pluginTypes.RpcError{}
	}
	verified, err := r.verifyDesiredWeight(context.Background(), rollout, config, desiredWeight)
	if err != nil {
		return pluginTypes.NotVerified, rpcError(err)
	}
	if verified {
		return pluginTypes.Verified, pluginTypes.RpcError{}
	}
	return pluginTypes.NotVerified, pluginTypes.RpcError{}
}

func (r *Reconciler) RemoveManagedRoutes(rollout *v1alpha1.Rollout) pluginTypes.RpcError {
	if LegacyAdoptionPending(rollout) {
		if err := r.ReconcileLegacyAdoptionPending(context.Background(), rollout); err != nil {
			return rpcError(err)
		}
		return pluginTypes.RpcError{}
	}
	if legacyAdoptionPromoting(rollout) {
		return pluginTypes.RpcError{}
	}
	if err := r.reconcilePromotedLegacyInactive(context.Background(), rollout); err != nil {
		return rpcError(err)
	}
	if rollout.Status.CurrentPodHash == "" || rollout.Status.StableRS == "" {
		return pluginTypes.RpcError{}
	}
	config, err := configForRollout(rollout)
	if err != nil {
		return rpcError(err)
	}
	if overrideHash := trafficOverrideHash(rollout); overrideHash != "" {
		_, candidateHash, nextRevision := trafficOverrideNextRevision(rollout, "")
		if nextRevision {
			if err := r.applyNextRevisionAfterTrafficRollback(context.Background(), rollout, config, overrideHash, candidateHash); err != nil {
				return rpcError(err)
			}
			return pluginTypes.RpcError{}
		}
		if err := r.applyTrafficOverride(context.Background(), rollout, config, overrideHash); err != nil {
			return rpcError(err)
		}
		return pluginTypes.RpcError{}
	}
	if legacyAdoptionBaselinePromoted(rollout) {
		if err := r.PromoteLegacyAdoption(context.Background(), rollout); err != nil {
			return rpcError(err)
		}
		return pluginTypes.RpcError{}
	}
	if rollout.Status.Abort && rollout.Status.CurrentPodHash != rollout.Status.StableRS {
		if err := r.applyAborted(context.Background(), rollout, config); err != nil {
			return rpcError(err)
		}
		return pluginTypes.RpcError{}
	}
	if rollout.Status.CurrentPodHash != rollout.Status.StableRS {
		return pluginTypes.RpcError{}
	}
	if err := r.applyFullyPromoted(context.Background(), rollout, config); err != nil {
		return rpcError(err)
	}
	return pluginTypes.RpcError{}
}

// LegacyAdoptionPending reports whether production traffic must remain on the
// pre-existing Deployment Pods until an explicit RolloutPromotion request.
func LegacyAdoptionPending(rollout *v1alpha1.Rollout) bool {
	return rollout != nil && !legacyAdoptionDurablyPromoted(rollout) && strings.TrimSpace(rollout.GetAnnotations()[annotations.LegacyAdoptionStateAnnotation]) == annotations.LegacyAdoptionStatePending
}

func legacyAdoptionPromoted(rollout *v1alpha1.Rollout) bool {
	return rollout != nil && (legacyAdoptionDurablyPromoted(rollout) || strings.TrimSpace(rollout.GetAnnotations()[annotations.LegacyAdoptionStateAnnotation]) == annotations.LegacyAdoptionStatePromoted)
}

func legacyAdoptionPromoting(rollout *v1alpha1.Rollout) bool {
	return rollout != nil && !legacyAdoptionDurablyPromoted(rollout) && strings.TrimSpace(rollout.GetAnnotations()[annotations.LegacyAdoptionStateAnnotation]) == annotations.LegacyAdoptionStatePromoting
}

func legacyAdoptionDurablyPromoted(rollout *v1alpha1.Rollout) bool {
	if rollout == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(rollout.GetAnnotations()[annotations.LegacyAdoptionPromotedAnnotation]), "true")
}

func legacyAdoptionBaselinePromoted(rollout *v1alpha1.Rollout) bool {
	return legacyAdoptionPromoted(rollout) && strings.TrimSpace(rollout.Status.CurrentPodHash) != "" && rollout.Status.CurrentPodHash == rollout.Status.StableRS
}

func (r *Reconciler) reconcilePromotedLegacyInactive(ctx context.Context, rollout *v1alpha1.Rollout) error {
	if !legacyAdoptionPromoted(rollout) {
		return nil
	}
	config, err := legacyAdoptionConfigForRollout(rollout)
	if err != nil {
		return err
	}
	legacySelector := appendSelector(config.baseSelector, "!"+rolloutPodTemplateHashKey)
	return r.patchBySelector(ctx, rollout.Namespace, legacySelector, legacyPodPatchConfig(config), config.inactiveWeight, "")
}

func (r *Reconciler) verifyPromotedLegacyInactive(ctx context.Context, rollout *v1alpha1.Rollout) (bool, error) {
	if !legacyAdoptionPromoted(rollout) {
		return true, nil
	}
	config, err := legacyAdoptionConfigForRollout(rollout)
	if err != nil {
		return false, err
	}
	legacySelector := appendSelector(config.baseSelector, "!"+rolloutPodTemplateHashKey)
	legacyPods, err := r.kubeClient.CoreV1().Pods(rollout.Namespace).List(ctx, metav1.ListOptions{LabelSelector: legacySelector})
	if err != nil {
		return false, err
	}
	for i := range legacyPods.Items {
		if legacyPods.Items[i].DeletionTimestamp == nil {
			return r.verifySelector(ctx, rollout.Namespace, legacySelector, legacyPodPatchConfig(config), config.inactiveWeight)
		}
	}
	// Once the source Deployment has been scaled to zero, an empty legacy
	// selector is the strongest possible proof that legacy traffic is inactive.
	// The generic selector verifier intentionally rejects empty workloads, so
	// handle this adoption-specific terminal state here.
	return true, nil
}

// VerifyLegacyAdoptionPromoted checks that the Rollout-owned baseline carries
// production traffic and every legacy Deployment Pod is inactive.
func (r *Reconciler) VerifyLegacyAdoptionPromoted(ctx context.Context, rollout *v1alpha1.Rollout) (bool, error) {
	config, err := legacyAdoptionConfigForRollout(rollout)
	if err != nil {
		return false, err
	}
	activeSelector := appendSelector(config.baseSelector, fmt.Sprintf("%s=%s", rolloutPodTemplateHashKey, rollout.Status.CurrentPodHash))
	activeVerified, err := r.verifySelector(ctx, rollout.Namespace, activeSelector, legacyPatchConfig(config), config.activeWeight)
	if err != nil || !activeVerified {
		return activeVerified, err
	}
	return r.verifyPromotedLegacyInactive(ctx, rollout)
}

func legacyAdoptionConfigForRollout(rollout *v1alpha1.Rollout) (legacyAdoptionConfig, error) {
	if rollout == nil {
		return legacyAdoptionConfig{}, fmt.Errorf("rollout is required")
	}
	values := rollout.GetAnnotations()
	config := legacyAdoptionConfig{
		baseSelector:    strings.TrimSpace(values[annotations.LegacyAdoptionBaseSelectorAnnotation]),
		annotationKey:   strings.TrimSpace(values[annotations.LegacyAdoptionWeightAnnotationAnnotation]),
		activeWeight:    values[annotations.LegacyAdoptionActiveWeightAnnotation],
		candidateWeight: values[annotations.LegacyAdoptionCandidateWeightAnnotation],
		legacyWeight:    values[annotations.LegacyAdoptionLegacyWeightAnnotation],
		inactiveWeight:  values[annotations.LegacyAdoptionInactiveWeightAnnotation],
	}
	for field, value := range map[string]string{
		"base selector":    config.baseSelector,
		"annotation key":   config.annotationKey,
		"active weight":    config.activeWeight,
		"candidate weight": config.candidateWeight,
		"legacy weight":    config.legacyWeight,
		"inactive weight":  config.inactiveWeight,
	} {
		if strings.TrimSpace(value) == "" {
			return legacyAdoptionConfig{}, fmt.Errorf("rollout %s/%s legacy adoption %s is required", rollout.Namespace, rollout.Name, field)
		}
	}
	return config, nil
}

func legacyPatchConfig(config legacyAdoptionConfig) Config {
	return Config{
		AnnotationKey:    config.annotationKey,
		BaseSelector:     config.baseSelector,
		PatchPods:        boolPointer(true),
		PatchReplicaSets: boolPointer(true),
	}
}

func legacyPodPatchConfig(config legacyAdoptionConfig) Config {
	patchConfig := legacyPatchConfig(config)
	patchConfig.PatchReplicaSets = boolPointer(false)
	return patchConfig
}

// ReconcileLegacyAdoptionPending keeps legacy Deployment Pods on production
// traffic and the Rollout-owned baseline on preview-only traffic.
func (r *Reconciler) ReconcileLegacyAdoptionPending(ctx context.Context, rollout *v1alpha1.Rollout) error {
	config, err := legacyAdoptionConfigForRollout(rollout)
	if err != nil {
		return err
	}
	if strings.TrimSpace(rollout.Status.CurrentPodHash) == "" {
		return fmt.Errorf("rollout %s/%s legacy adoption has no currentPodHash", rollout.Namespace, rollout.Name)
	}
	patchConfig := legacyPatchConfig(config)
	legacyConfig := legacyPodPatchConfig(config)
	legacySelector := appendSelector(config.baseSelector, "!"+rolloutPodTemplateHashKey)
	candidateSelector := appendSelector(config.baseSelector, fmt.Sprintf("%s=%s", rolloutPodTemplateHashKey, rollout.Status.CurrentPodHash))
	otherSelector := appendSelector(config.baseSelector, rolloutPodTemplateHashKey)
	otherSelector = appendSelector(otherSelector, fmt.Sprintf("%s notin (%s)", rolloutPodTemplateHashKey, rollout.Status.CurrentPodHash))
	if err := r.patchBySelector(ctx, rollout.Namespace, legacySelector, legacyConfig, config.legacyWeight, ""); err != nil {
		return err
	}
	if err := r.patchBySelector(ctx, rollout.Namespace, candidateSelector, patchConfig, config.candidateWeight, ""); err != nil {
		return err
	}
	return r.patchBySelector(ctx, rollout.Namespace, otherSelector, patchConfig, config.inactiveWeight, "")
}

// PromoteLegacyAdoption performs the explicit first cut without changing the
// Rollout PodTemplate or recreating either legacy or baseline Pods.
func (r *Reconciler) PromoteLegacyAdoption(ctx context.Context, rollout *v1alpha1.Rollout) error {
	config, err := legacyAdoptionConfigForRollout(rollout)
	if err != nil {
		return err
	}
	if strings.TrimSpace(rollout.Status.CurrentPodHash) == "" {
		return fmt.Errorf("rollout %s/%s legacy adoption has no currentPodHash", rollout.Namespace, rollout.Name)
	}
	patchConfig := legacyPatchConfig(config)
	legacyConfig := legacyPodPatchConfig(config)
	activeSelector := appendSelector(config.baseSelector, fmt.Sprintf("%s=%s", rolloutPodTemplateHashKey, rollout.Status.CurrentPodHash))
	legacySelector := appendSelector(config.baseSelector, "!"+rolloutPodTemplateHashKey)
	otherSelector := appendSelector(config.baseSelector, rolloutPodTemplateHashKey)
	otherSelector = appendSelector(otherSelector, fmt.Sprintf("%s notin (%s)", rolloutPodTemplateHashKey, rollout.Status.CurrentPodHash))
	if err := r.patchBySelector(ctx, rollout.Namespace, activeSelector, patchConfig, config.activeWeight, ""); err != nil {
		return err
	}
	if err := r.patchBySelector(ctx, rollout.Namespace, legacySelector, legacyConfig, config.inactiveWeight, ""); err != nil {
		return err
	}
	return r.patchBySelector(ctx, rollout.Namespace, otherSelector, patchConfig, config.inactiveWeight, "")
}

// RestoreLegacyAdoption restores production traffic to the pre-existing
// Deployment Pods before a Helm rollback is allowed to delete the Rollout.
func (r *Reconciler) RestoreLegacyAdoption(ctx context.Context, rollout *v1alpha1.Rollout) error {
	config, err := legacyAdoptionConfigForRollout(rollout)
	if err != nil {
		return err
	}
	patchConfig := legacyPatchConfig(config)
	legacyConfig := legacyPodPatchConfig(config)
	legacySelector := appendSelector(config.baseSelector, "!"+rolloutPodTemplateHashKey)
	managedSelector := appendSelector(config.baseSelector, rolloutPodTemplateHashKey)
	if err := r.patchBySelector(ctx, rollout.Namespace, legacySelector, legacyConfig, config.legacyWeight, ""); err != nil {
		return err
	}
	return r.patchBySelector(ctx, rollout.Namespace, managedSelector, patchConfig, config.inactiveWeight, "")
}

// RestoreLegacyAdoptionForDeletion returns traffic and Deployment ownership to
// the legacy workload before Helm removes an adopted Rollout. Legacy
// ReplicaSet templates are never mutated, so restoring the Deployment cannot
// create a synthetic revision or replacement Pod.
func (r *Reconciler) RestoreLegacyAdoptionForDeletion(ctx context.Context, rollout *v1alpha1.Rollout) error {
	config, err := legacyAdoptionConfigForRollout(rollout)
	if err != nil {
		return err
	}
	if rollout.Spec.WorkloadRef == nil || rollout.Spec.WorkloadRef.Kind != "Deployment" || strings.TrimSpace(rollout.Spec.WorkloadRef.Name) == "" {
		return fmt.Errorf("rollout %s/%s legacy adoption cleanup requires a Deployment workloadRef", rollout.Namespace, rollout.Name)
	}

	restorePaused := false
	if raw := strings.TrimSpace(rollout.GetAnnotations()[annotations.LegacyAdoptionDeploymentPausedAnnotation]); raw != "" {
		value, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			return fmt.Errorf("rollout %s/%s has invalid legacy Deployment paused value %q: %w", rollout.Namespace, rollout.Name, raw, parseErr)
		}
		restorePaused = value
	}
	legacySelector := appendSelector(config.baseSelector, "!"+rolloutPodTemplateHashKey)
	patchConfig := legacyPatchConfig(config)
	if err := r.patchBySelector(ctx, rollout.Namespace, legacySelector, legacyPodPatchConfig(config), config.legacyWeight, ""); err != nil {
		return err
	}
	managedSelector := appendSelector(config.baseSelector, rolloutPodTemplateHashKey)
	if err := r.patchBySelector(ctx, rollout.Namespace, managedSelector, patchConfig, config.inactiveWeight, ""); err != nil {
		return err
	}
	if err := r.restoreLegacyPodAnnotationFromTemplates(ctx, rollout.Namespace, legacySelector, rollout.Spec.WorkloadRef.Name, config.annotationKey); err != nil {
		return fmt.Errorf("restore legacy Pod traffic annotation: %w", err)
	}

	deployments := r.kubeClient.AppsV1().Deployments(rollout.Namespace)
	if _, err := setDeploymentPaused(ctx, deployments, rollout.Spec.WorkloadRef.Name, restorePaused); err != nil && !k8serrors.IsNotFound(err) {
		return fmt.Errorf("restore legacy Deployment paused state: %w", err)
	}
	return nil
}

// restoreLegacyPodAnnotationFromTemplates restores the exact pre-adoption
// annotation value without mutating a ReplicaSet template or recreating a Pod.
// The legacy ReplicaSet remains the authoritative declarative source because
// adoption only patches live legacy Pods.
func (r *Reconciler) restoreLegacyPodAnnotationFromTemplates(ctx context.Context, namespace, selector, deploymentName, annotationKey string) error {
	pods, err := r.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}
	deployment, err := r.kubeClient.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	var fallbackValue string
	var fallbackExists bool
	if deployment != nil {
		fallbackValue, fallbackExists = deployment.Spec.Template.Annotations[annotationKey]
	}
	replicaSets := make(map[string]*appsv1.ReplicaSet)

	for i := range pods.Items {
		pod := &pods.Items[i]
		desiredValue, desiredExists := fallbackValue, fallbackExists
		if replicaSetName := controllingReplicaSetName(pod); replicaSetName != "" {
			replicaSet, found := replicaSets[replicaSetName]
			if !found {
				replicaSet, err = r.kubeClient.AppsV1().ReplicaSets(namespace).Get(ctx, replicaSetName, metav1.GetOptions{})
				if err != nil && !k8serrors.IsNotFound(err) {
					return err
				}
				replicaSets[replicaSetName] = replicaSet
			}
			if replicaSet != nil {
				desiredValue, desiredExists = replicaSet.Spec.Template.Annotations[annotationKey]
			}
		}

		currentValue, currentExists := pod.Annotations[annotationKey]
		if currentExists == desiredExists && (!desiredExists || currentValue == desiredValue) {
			continue
		}
		annotationPatch := map[string]interface{}{annotationKey: nil}
		if desiredExists {
			annotationPatch[annotationKey] = desiredValue
		}
		patch, err := json.Marshal(map[string]interface{}{
			"metadata": map[string]interface{}{
				"annotations": annotationPatch,
			},
		})
		if err != nil {
			return err
		}
		if _, err := r.kubeClient.CoreV1().Pods(namespace).Patch(ctx, pod.Name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil && !k8serrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func controllingReplicaSetName(pod *corev1.Pod) string {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "ReplicaSet" && owner.Controller != nil && *owner.Controller {
			return owner.Name
		}
	}
	return ""
}

func setDeploymentPaused(ctx context.Context, deployments appsv1client.DeploymentInterface, name string, paused bool) (*appsv1.Deployment, error) {
	var result *appsv1.Deployment
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := deployments.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if current.Spec.Paused == paused {
			result = current
			return nil
		}
		current = current.DeepCopy()
		current.Spec.Paused = paused
		result, err = deployments.Update(ctx, current, metav1.UpdateOptions{})
		return err
	})
	return result, err
}

func (r *Reconciler) VerifyLegacyAdoptionPending(ctx context.Context, rollout *v1alpha1.Rollout) (bool, error) {
	config, err := legacyAdoptionConfigForRollout(rollout)
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(rollout.Status.CurrentPodHash) == "" {
		return false, nil
	}
	patchConfig := legacyPatchConfig(config)
	legacyConfig := legacyPodPatchConfig(config)
	legacySelector := appendSelector(config.baseSelector, "!"+rolloutPodTemplateHashKey)
	candidateSelector := appendSelector(config.baseSelector, fmt.Sprintf("%s=%s", rolloutPodTemplateHashKey, rollout.Status.CurrentPodHash))
	legacyVerified, err := r.verifySelector(ctx, rollout.Namespace, legacySelector, legacyConfig, config.legacyWeight)
	if err != nil || !legacyVerified {
		return legacyVerified, err
	}
	return r.verifySelector(ctx, rollout.Namespace, candidateSelector, patchConfig, config.candidateWeight)
}

func (r *Reconciler) applyAborted(ctx context.Context, rollout *v1alpha1.Rollout, config Config) error {
	stablePayload, err := renderWeight(config.StableWeightTemplate, config.MaxPodWeight, config.WeightPlaceholder, *config.PrettyJSON)
	if err != nil {
		return err
	}
	canaryPayload, err := renderWeight(config.CanaryWeightTemplate, 0, config.WeightPlaceholder, *config.PrettyJSON)
	if err != nil {
		return err
	}
	if err := r.patchGroup(ctx, rollout.Namespace, config, rollout.Status.StableRS, stablePayload, config.StableRole); err != nil {
		return err
	}
	if err := r.patchGroup(ctx, rollout.Namespace, config, rollout.Status.CurrentPodHash, canaryPayload, config.CanaryRole); err != nil {
		return err
	}
	return r.patchInactive(ctx, rollout.Namespace, config, rollout.Status.StableRS, rollout.Status.CurrentPodHash)
}

func trafficOverrideHash(rollout *v1alpha1.Rollout) string {
	if rollout == nil || rollout.Annotations == nil {
		return ""
	}
	return strings.TrimSpace(rollout.Annotations[annotations.TrafficOverrideHashAnnotation])
}

func trafficOverrideNextRevision(rollout *v1alpha1.Rollout, candidateHash string) (string, string, bool) {
	if rollout == nil || rollout.Annotations == nil {
		return "", "", false
	}
	targetHash := strings.TrimSpace(rollout.Annotations[annotations.TrafficOverrideHashAnnotation])
	sourceHash := strings.TrimSpace(rollout.Annotations[annotations.TrafficOverrideSourceHashAnnotation])
	if candidateHash == "" {
		candidateHash = strings.TrimSpace(rollout.Status.CurrentPodHash)
	}
	if targetHash == "" || sourceHash == "" || candidateHash == "" {
		return targetHash, candidateHash, false
	}
	return targetHash, candidateHash, candidateHash != targetHash && candidateHash != sourceHash
}

func (r *Reconciler) applyTrafficOverride(ctx context.Context, rollout *v1alpha1.Rollout, config Config, targetHash string) error {
	activePayload, err := renderWeight(config.PromotedWeightTemplate, config.MaxPodWeight, config.WeightPlaceholder, *config.PrettyJSON)
	if err != nil {
		return err
	}
	if err := r.patchGroup(ctx, rollout.Namespace, config, targetHash, activePayload, config.StableRole); err != nil {
		return err
	}
	return r.patchInactive(ctx, rollout.Namespace, config, targetHash, "")
}

// applyNextRevisionAfterTrafficRollback keeps the restored baseline on
// production traffic while exposing the later desired revision only through
// its preview hosts. The forward promotion clears the override before any
// non-zero production weight is applied to the candidate.
func (r *Reconciler) applyNextRevisionAfterTrafficRollback(ctx context.Context, rollout *v1alpha1.Rollout, config Config, targetHash, candidateHash string) error {
	stablePayload, err := renderWeight(config.StableWeightTemplate, config.MaxPodWeight, config.WeightPlaceholder, *config.PrettyJSON)
	if err != nil {
		return err
	}
	canaryPayload, err := renderWeight(config.CanaryWeightTemplate, 0, config.WeightPlaceholder, *config.PrettyJSON)
	if err != nil {
		return err
	}
	if err := r.patchGroup(ctx, rollout.Namespace, config, targetHash, stablePayload, config.StableRole); err != nil {
		return err
	}
	if err := r.patchGroup(ctx, rollout.Namespace, config, candidateHash, canaryPayload, config.CanaryRole); err != nil {
		return err
	}
	return r.patchInactive(ctx, rollout.Namespace, config, targetHash, candidateHash)
}

func (r *Reconciler) verifyNextRevisionAfterTrafficRollback(ctx context.Context, rollout *v1alpha1.Rollout, config Config, targetHash, candidateHash string) (bool, error) {
	stablePayload, err := renderWeight(config.StableWeightTemplate, config.MaxPodWeight, config.WeightPlaceholder, *config.PrettyJSON)
	if err != nil {
		return false, err
	}
	canaryPayload, err := renderWeight(config.CanaryWeightTemplate, 0, config.WeightPlaceholder, *config.PrettyJSON)
	if err != nil {
		return false, err
	}
	verified, err := r.verifyGroup(ctx, rollout.Namespace, config, targetHash, stablePayload)
	if err != nil || !verified {
		return verified, err
	}
	verified, err = r.verifyGroup(ctx, rollout.Namespace, config, candidateHash, canaryPayload)
	if err != nil || !verified {
		return verified, err
	}

	selector := appendSelector(config.BaseSelector, rolloutPodTemplateHashKey)
	replicaSets, err := r.kubeClient.AppsV1().ReplicaSets(rollout.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return false, err
	}
	for i := range replicaSets.Items {
		rs := &replicaSets.Items[i]
		hash := rs.Labels[rolloutPodTemplateHashKey]
		if hash == "" || hash == targetHash || hash == candidateHash || rs.Spec.Replicas == nil || *rs.Spec.Replicas == 0 {
			continue
		}
		verified, err = r.verifyGroup(ctx, rollout.Namespace, config, hash, config.InactiveWeightTemplate)
		if err != nil || !verified {
			return verified, err
		}
	}
	return true, nil
}

func (r *Reconciler) verifyTrafficOverride(ctx context.Context, rollout *v1alpha1.Rollout, config Config, targetHash string) (bool, error) {
	activePayload, err := renderWeight(config.PromotedWeightTemplate, config.MaxPodWeight, config.WeightPlaceholder, *config.PrettyJSON)
	if err != nil {
		return false, err
	}
	verified, err := r.verifyGroup(ctx, rollout.Namespace, config, targetHash, activePayload)
	if err != nil || !verified {
		return verified, err
	}
	selector := appendSelector(config.BaseSelector, rolloutPodTemplateHashKey)
	replicaSets, err := r.kubeClient.AppsV1().ReplicaSets(rollout.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return false, err
	}
	for i := range replicaSets.Items {
		rs := &replicaSets.Items[i]
		hash := rs.Labels[rolloutPodTemplateHashKey]
		if hash == "" || hash == targetHash || rs.Spec.Replicas == nil || *rs.Spec.Replicas == 0 {
			continue
		}
		verified, err = r.verifyGroup(ctx, rollout.Namespace, config, hash, config.InactiveWeightTemplate)
		if err != nil || !verified {
			return verified, err
		}
	}
	return true, nil
}

func (r *Reconciler) SetHeaderRoute(_ *v1alpha1.Rollout, _ *v1alpha1.SetHeaderRoute) pluginTypes.RpcError {
	return rpcError(fmt.Errorf("%s does not support header routes", PluginName))
}

func (r *Reconciler) SetMirrorRoute(_ *v1alpha1.Rollout, _ *v1alpha1.SetMirrorRoute) pluginTypes.RpcError {
	return rpcError(fmt.Errorf("%s does not support mirror routes", PluginName))
}

func (r *Reconciler) Type() string {
	return PluginName
}

func (r *Reconciler) applyDesiredWeight(ctx context.Context, rollout *v1alpha1.Rollout, config Config, desiredWeight int32) error {
	if desiredWeight < 0 || desiredWeight > int32(config.TrafficWeightScale) {
		return fmt.Errorf("desired canary weight must be between 0 and %d, got %d", config.TrafficWeightScale, desiredWeight)
	}
	if rollout.Status.CurrentPodHash == "" || rollout.Status.StableRS == "" {
		return fmt.Errorf("rollout %s/%s has empty currentPodHash or stableRS", rollout.Namespace, rollout.Name)
	}
	if rollout.Status.CurrentPodHash == rollout.Status.StableRS {
		return r.applyFullyPromoted(ctx, rollout, config)
	}

	canaryHash := rollout.Status.CurrentPodHash
	stableHash := rollout.Status.StableRS
	stablePods, err := r.listPodsForHash(ctx, rollout.Namespace, config.BaseSelector, stableHash)
	if err != nil {
		return err
	}
	canaryPods, err := r.listPodsForHash(ctx, rollout.Namespace, config.BaseSelector, canaryHash)
	if err != nil {
		return err
	}
	desiredPercent := int(math.Round(float64(desiredWeight) * 100 / float64(config.TrafficWeightScale)))
	stableWeight, canaryWeight, err := calculateWeights(config, len(stablePods), len(canaryPods), desiredPercent)
	if err != nil {
		return fmt.Errorf("rollout %s/%s: %w", rollout.Namespace, rollout.Name, err)
	}
	stablePayload, err := renderWeight(config.StableWeightTemplate, stableWeight, config.WeightPlaceholder, *config.PrettyJSON)
	if err != nil {
		return fmt.Errorf("render stable weight: %w", err)
	}
	canaryPayload, err := renderWeight(config.CanaryWeightTemplate, canaryWeight, config.WeightPlaceholder, *config.PrettyJSON)
	if err != nil {
		return fmt.Errorf("render canary weight: %w", err)
	}
	if err := r.patchGroup(ctx, rollout.Namespace, config, stableHash, stablePayload, config.StableRole); err != nil {
		return err
	}
	if err := r.patchGroup(ctx, rollout.Namespace, config, canaryHash, canaryPayload, config.CanaryRole); err != nil {
		return err
	}
	return r.patchInactive(ctx, rollout.Namespace, config, stableHash, canaryHash)
}

func (r *Reconciler) applyFullyPromoted(ctx context.Context, rollout *v1alpha1.Rollout, config Config) error {
	activePayload, err := renderWeight(config.PromotedWeightTemplate, config.MaxPodWeight, config.WeightPlaceholder, *config.PrettyJSON)
	if err != nil {
		return err
	}
	if err := r.patchGroup(ctx, rollout.Namespace, config, rollout.Status.CurrentPodHash, activePayload, config.StableRole); err != nil {
		return err
	}
	return r.patchInactive(ctx, rollout.Namespace, config, rollout.Status.CurrentPodHash, "")
}

func (r *Reconciler) verifyDesiredWeight(ctx context.Context, rollout *v1alpha1.Rollout, config Config, desiredWeight int32) (bool, error) {
	if rollout.Status.CurrentPodHash == "" || rollout.Status.StableRS == "" {
		return false, nil
	}
	if rollout.Status.CurrentPodHash == rollout.Status.StableRS {
		payload, err := renderWeight(config.PromotedWeightTemplate, config.MaxPodWeight, config.WeightPlaceholder, *config.PrettyJSON)
		if err != nil {
			return false, err
		}
		return r.verifyGroup(ctx, rollout.Namespace, config, rollout.Status.CurrentPodHash, payload)
	}
	stablePods, err := r.listPodsForHash(ctx, rollout.Namespace, config.BaseSelector, rollout.Status.StableRS)
	if err != nil {
		return false, err
	}
	canaryPods, err := r.listPodsForHash(ctx, rollout.Namespace, config.BaseSelector, rollout.Status.CurrentPodHash)
	if err != nil {
		return false, err
	}
	desiredPercent := int(math.Round(float64(desiredWeight) * 100 / float64(config.TrafficWeightScale)))
	stableWeight, canaryWeight, err := calculateWeights(config, len(stablePods), len(canaryPods), desiredPercent)
	if err != nil {
		return false, err
	}
	stablePayload, err := renderWeight(config.StableWeightTemplate, stableWeight, config.WeightPlaceholder, *config.PrettyJSON)
	if err != nil {
		return false, err
	}
	canaryPayload, err := renderWeight(config.CanaryWeightTemplate, canaryWeight, config.WeightPlaceholder, *config.PrettyJSON)
	if err != nil {
		return false, err
	}
	stableVerified, err := r.verifyGroup(ctx, rollout.Namespace, config, rollout.Status.StableRS, stablePayload)
	if err != nil || !stableVerified {
		return stableVerified, err
	}
	return r.verifyGroup(ctx, rollout.Namespace, config, rollout.Status.CurrentPodHash, canaryPayload)
}

func (r *Reconciler) patchInactive(ctx context.Context, namespace string, config Config, activeHashA, activeHashB string) error {
	if activeHashB == "" {
		selector := appendSelector(config.BaseSelector, rolloutPodTemplateHashKey)
		replicaSets, err := r.kubeClient.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}
		for i := range replicaSets.Items {
			replicaSet := &replicaSets.Items[i]
			hash := replicaSet.Labels[rolloutPodTemplateHashKey]
			if hash == "" || hash == activeHashA || replicaSet.Spec.Replicas == nil || *replicaSet.Spec.Replicas == 0 {
				continue
			}
			if err := r.patchGroup(ctx, namespace, config, hash, config.InactiveWeightTemplate, config.InactiveRole); err != nil {
				return err
			}
		}
		return nil
	}

	selector := appendSelector(config.BaseSelector, rolloutPodTemplateHashKey)
	selector = appendSelector(selector, fmt.Sprintf("%s notin (%s,%s)", rolloutPodTemplateHashKey, activeHashA, activeHashB))
	return r.patchBySelector(ctx, namespace, selector, config, config.InactiveWeightTemplate, config.InactiveRole)
}

func (r *Reconciler) patchGroup(ctx context.Context, namespace string, config Config, hash, payload, role string) error {
	selector := appendSelector(config.BaseSelector, fmt.Sprintf("%s=%s", rolloutPodTemplateHashKey, hash))
	return r.patchBySelector(ctx, namespace, selector, config, payload, role)
}

func (r *Reconciler) patchRole(ctx context.Context, namespace string, config Config, hash, role string) error {
	if config.RoleLabelKey == "" || role == "" {
		return nil
	}
	selector := appendSelector(config.BaseSelector, fmt.Sprintf("%s=%s", rolloutPodTemplateHashKey, hash))
	return r.patchBySelector(ctx, namespace, selector, config, "", role)
}

func (r *Reconciler) patchBySelector(ctx context.Context, namespace, selector string, config Config, payload, role string) error {
	podMetadata := map[string]interface{}{}
	rsMetadata := map[string]interface{}{}
	if payload != "" {
		podMetadata["annotations"] = map[string]string{config.AnnotationKey: payload}
		rsMetadata["annotations"] = map[string]string{config.AnnotationKey: payload}
	}
	if config.RoleLabelKey != "" && role != "" {
		podMetadata["labels"] = map[string]string{config.RoleLabelKey: role}
		rsMetadata["labels"] = map[string]string{config.RoleLabelKey: role}
	}
	podPatch, err := json.Marshal(map[string]interface{}{"metadata": podMetadata})
	if err != nil {
		return err
	}
	rsPatch, err := json.Marshal(map[string]interface{}{"spec": map[string]interface{}{"template": map[string]interface{}{"metadata": rsMetadata}}})
	if err != nil {
		return err
	}

	if *config.PatchPods {
		pods, err := r.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}
		for i := range pods.Items {
			if metadataMatches(pods.Items[i].Annotations, pods.Items[i].Labels, config, payload, role) {
				continue
			}
			if _, err := r.kubeClient.CoreV1().Pods(namespace).Patch(ctx, pods.Items[i].Name, types.MergePatchType, podPatch, metav1.PatchOptions{}); err != nil {
				return err
			}
		}
	}
	if *config.PatchReplicaSets {
		replicaSets, err := r.kubeClient.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return err
		}
		for i := range replicaSets.Items {
			if metadataMatches(replicaSets.Items[i].Spec.Template.Annotations, replicaSets.Items[i].Spec.Template.Labels, config, payload, role) {
				continue
			}
			if _, err := r.kubeClient.AppsV1().ReplicaSets(namespace).Patch(ctx, replicaSets.Items[i].Name, types.MergePatchType, rsPatch, metav1.PatchOptions{}); err != nil {
				return err
			}
		}
	}
	return nil
}

func metadataMatches(annotations, labels map[string]string, config Config, payload, role string) bool {
	if payload != "" && !annotationValueMatches(annotations[config.AnnotationKey], payload) {
		return false
	}
	if config.RoleLabelKey != "" && role != "" && labels[config.RoleLabelKey] != role {
		return false
	}
	return true
}

func annotationValueMatches(actual, expected string) bool {
	if actual == expected {
		return true
	}

	var actualJSON interface{}
	var expectedJSON interface{}
	if json.Unmarshal([]byte(actual), &actualJSON) != nil || json.Unmarshal([]byte(expected), &expectedJSON) != nil {
		return false
	}
	return reflect.DeepEqual(actualJSON, expectedJSON)
}

func (r *Reconciler) verifyGroup(ctx context.Context, namespace string, config Config, hash, payload string) (bool, error) {
	selector := appendSelector(config.BaseSelector, fmt.Sprintf("%s=%s", rolloutPodTemplateHashKey, hash))
	return r.verifySelector(ctx, namespace, selector, config, payload)
}

func (r *Reconciler) verifySelector(ctx context.Context, namespace, selector string, config Config, payload string) (bool, error) {
	if *config.PatchPods {
		pods, err := r.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return false, err
		}
		activePods := 0
		for i := range pods.Items {
			if pods.Items[i].DeletionTimestamp != nil {
				continue
			}
			activePods++
			if !annotationValueMatches(pods.Items[i].Annotations[config.AnnotationKey], payload) {
				return false, nil
			}
		}
		if activePods == 0 {
			return false, nil
		}
	}
	if *config.PatchReplicaSets {
		replicaSets, err := r.kubeClient.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return false, err
		}
		if len(replicaSets.Items) == 0 {
			return false, nil
		}
		for i := range replicaSets.Items {
			if !annotationValueMatches(replicaSets.Items[i].Spec.Template.Annotations[config.AnnotationKey], payload) {
				return false, nil
			}
		}
	}
	return true, nil
}

func (r *Reconciler) listPodsForHash(ctx context.Context, namespace, baseSelector, hash string) ([]corev1.Pod, error) {
	selector := appendSelector(baseSelector, fmt.Sprintf("%s=%s", rolloutPodTemplateHashKey, hash))
	pods, err := r.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	active := make([]corev1.Pod, 0, len(pods.Items))
	for i := range pods.Items {
		if pods.Items[i].DeletionTimestamp == nil {
			active = append(active, pods.Items[i])
		}
	}
	return active, nil
}

// ManagedAnnotationKey returns the Pod annotation exclusively managed by this
// traffic router. The rollout controller must not also reconcile the same key
// through canaryMetadata, otherwise static metadata and dynamic traffic weights
// continuously overwrite each other.
func ManagedAnnotationKey(rollout *v1alpha1.Rollout) string {
	if rollout == nil || rollout.Spec.Strategy.Canary == nil || rollout.Spec.Strategy.Canary.TrafficRouting == nil {
		return ""
	}
	raw, found := rollout.Spec.Strategy.Canary.TrafficRouting.Plugins[PluginName]
	if !found {
		return ""
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return ""
	}
	return strings.TrimSpace(config.AnnotationKey)
}

func configForRollout(rollout *v1alpha1.Rollout) (Config, error) {
	if rollout.Spec.Strategy.Canary == nil || rollout.Spec.Strategy.Canary.TrafficRouting == nil {
		return Config{}, fmt.Errorf("rollout %s/%s does not configure canary traffic routing", rollout.Namespace, rollout.Name)
	}
	raw, found := rollout.Spec.Strategy.Canary.TrafficRouting.Plugins[PluginName]
	if !found {
		return Config{}, fmt.Errorf("rollout %s/%s does not configure plugin %s", rollout.Namespace, rollout.Name, PluginName)
	}
	config := Config{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return Config{}, fmt.Errorf("invalid %s config: %w", PluginName, err)
		}
	}
	if strings.TrimSpace(config.AnnotationKey) == "" {
		return Config{}, fmt.Errorf("%s annotationKey is required", PluginName)
	}
	if strings.TrimSpace(config.BaseSelector) == "" {
		return Config{}, fmt.Errorf("%s baseSelector is required", PluginName)
	}
	if config.MaxPodWeight == 0 {
		config.MaxPodWeight = defaultMaxPodWeight
	}
	if config.MaxPodWeight < 1 || config.MaxPodWeight > 100 {
		return Config{}, fmt.Errorf("%s maxPodWeight must be between 1 and 100", PluginName)
	}
	if config.TrafficWeightScale == 0 {
		config.TrafficWeightScale = 100
	}
	if config.TrafficWeightScale < 1 {
		return Config{}, fmt.Errorf("%s trafficWeightScale must be greater than 0", PluginName)
	}
	if config.WeightCalculation == "" {
		config.WeightCalculation = defaultWeightCalculation
	}
	if config.WeightCalculation != "aggregatePerPod" && config.WeightCalculation != "direct" {
		return Config{}, fmt.Errorf("%s weightCalculation must be aggregatePerPod or direct", PluginName)
	}
	if config.WeightPlaceholder == "" {
		config.WeightPlaceholder = defaultWeightPlaceholder
	}
	if config.StableWeightTemplate == "" {
		config.StableWeightTemplate = config.WeightPlaceholder
	}
	if config.CanaryWeightTemplate == "" {
		config.CanaryWeightTemplate = config.WeightPlaceholder
	}
	if config.PromotedWeightTemplate == "" {
		config.PromotedWeightTemplate = config.CanaryWeightTemplate
	}
	if config.PrettyJSON == nil {
		config.PrettyJSON = boolPointer(false)
	}
	if config.InactiveWeightTemplate == "" {
		inactiveTemplate, err := renderWeight(config.CanaryWeightTemplate, 0, config.WeightPlaceholder, *config.PrettyJSON)
		if err != nil {
			return Config{}, fmt.Errorf("%s invalid canaryWeightTemplate: %w", PluginName, err)
		}
		config.InactiveWeightTemplate = inactiveTemplate
	}
	for name, template := range map[string]string{
		"stableWeightTemplate":   config.StableWeightTemplate,
		"canaryWeightTemplate":   config.CanaryWeightTemplate,
		"promotedWeightTemplate": config.PromotedWeightTemplate,
	} {
		if _, err := renderWeight(template, config.MaxPodWeight, config.WeightPlaceholder, *config.PrettyJSON); err != nil {
			return Config{}, fmt.Errorf("%s invalid %s: %w", PluginName, name, err)
		}
	}
	if config.RoleLabelKey != "" {
		if config.StableRole == "" {
			config.StableRole = "stable"
		}
		if config.CanaryRole == "" {
			config.CanaryRole = "canary"
		}
		if config.InactiveRole == "" {
			config.InactiveRole = "inactive"
		}
	}
	if config.PatchPods == nil {
		config.PatchPods = boolPointer(true)
	}
	if config.PatchReplicaSets == nil {
		config.PatchReplicaSets = boolPointer(true)
	}
	if !*config.PatchPods && !*config.PatchReplicaSets {
		return Config{}, fmt.Errorf("%s must patch at least one of Pods or ReplicaSets", PluginName)
	}
	return config, nil
}

func calculateWeights(config Config, stablePods, canaryPods, desiredPercent int) (int, int, error) {
	if config.WeightCalculation == "direct" {
		if desiredPercent < 0 || desiredPercent > 100 {
			return 0, 0, fmt.Errorf("desiredPercent must be between 0 and 100")
		}
		stable := int(math.Round(float64(config.MaxPodWeight) * float64(100-desiredPercent) / 100))
		canary := int(math.Round(float64(config.MaxPodWeight) * float64(desiredPercent) / 100))
		return stable, canary, nil
	}
	return calculatePerPodWeights(stablePods, canaryPods, desiredPercent, config.MaxPodWeight)
}

func calculatePerPodWeights(stablePods, canaryPods, desiredPercent, maxWeight int) (int, int, error) {
	if desiredPercent < 0 || desiredPercent > 100 {
		return 0, 0, fmt.Errorf("desiredPercent must be between 0 and 100")
	}
	if desiredPercent < 100 && stablePods == 0 {
		return 0, 0, fmt.Errorf("stable ReplicaSet has no active Pods")
	}
	if desiredPercent > 0 && canaryPods == 0 {
		return 0, 0, fmt.Errorf("canary ReplicaSet has no active Pods")
	}
	if desiredPercent == 0 {
		return maxWeight, 0, nil
	}
	if desiredPercent == 100 {
		return 0, maxWeight, nil
	}

	bestStable, bestCanary := 1, 1
	bestError := math.MaxFloat64
	bestTotal := -1
	for stableWeight := 1; stableWeight <= maxWeight; stableWeight++ {
		for canaryWeight := 1; canaryWeight <= maxWeight; canaryWeight++ {
			stableTotal := float64(stablePods * stableWeight)
			canaryTotal := float64(canaryPods * canaryWeight)
			actual := 100 * canaryTotal / (stableTotal + canaryTotal)
			errorValue := math.Abs(actual - float64(desiredPercent))
			total := stablePods*stableWeight + canaryPods*canaryWeight
			if errorValue < bestError-1e-9 || (math.Abs(errorValue-bestError) <= 1e-9 && total > bestTotal) {
				bestStable, bestCanary = stableWeight, canaryWeight
				bestError, bestTotal = errorValue, total
			}
		}
	}
	return bestStable, bestCanary, nil
}

func renderWeight(template string, weight int, placeholder string, prettyJSON bool) (string, error) {
	template = strings.TrimSpace(template)
	if template == "" {
		return strconv.Itoa(weight), nil
	}
	if placeholder == "" {
		placeholder = defaultWeightPlaceholder
	}
	quotedPlaceholder, err := json.Marshal(placeholder)
	if err != nil {
		return "", err
	}
	replacement := strconv.Itoa(weight)
	rendered := strings.ReplaceAll(template, string(quotedPlaceholder), replacement)
	rendered = strings.ReplaceAll(rendered, placeholder, replacement)
	if rendered == template {
		return "", fmt.Errorf("weight template does not contain placeholder %q", placeholder)
	}
	if prettyJSON && json.Valid([]byte(rendered)) {
		var value interface{}
		if err := json.Unmarshal([]byte(rendered), &value); err != nil {
			return "", err
		}
		pretty, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return "", err
		}
		return string(pretty), nil
	}
	return rendered, nil
}

func boolPointer(value bool) *bool {
	return &value
}

func appendSelector(baseSelector, expression string) string {
	if strings.TrimSpace(baseSelector) == "" {
		return expression
	}
	return fmt.Sprintf("%s,%s", baseSelector, expression)
}

func rpcError(err error) pluginTypes.RpcError {
	if err == nil {
		return pluginTypes.RpcError{}
	}
	return pluginTypes.RpcError{ErrorString: err.Error()}
}
