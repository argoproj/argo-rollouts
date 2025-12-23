package analysis

import (
	"context"
	"fmt"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	patchtypes "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	listers "github.com/argoproj/argo-rollouts/pkg/client/listers/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	"github.com/argoproj/argo-rollouts/utils/annotations"
)

const (
	cancelAnalysisRunPatch = `{
		"spec": {
			"terminate": true
		}
	}`
)

// Helper provides analysis run management functionality that can be shared
// between the Rollout controller and RolloutPlugin controller
type Helper struct {
	argoProjClientset      clientset.Interface
	analysisRunLister      listers.AnalysisRunLister
	analysisTemplateLister listers.AnalysisTemplateLister
	clusterTemplateLister  listers.ClusterAnalysisTemplateLister
}

// NewHelper creates a new analysis helper
func NewHelper(
	argoProjClientset clientset.Interface,
	analysisRunLister listers.AnalysisRunLister,
	analysisTemplateLister listers.AnalysisTemplateLister,
	clusterTemplateLister listers.ClusterAnalysisTemplateLister,
) *Helper {
	return &Helper{
		argoProjClientset:      argoProjClientset,
		analysisRunLister:      analysisRunLister,
		analysisTemplateLister: analysisTemplateLister,
		clusterTemplateLister:  clusterTemplateLister,
	}
}

// GetAnalysisRunsForOwner gets all AnalysisRuns owned by the given owner (Rollout or RolloutPlugin)
func (h *Helper) GetAnalysisRunsForOwner(ctx context.Context, ownerName string, namespace string, ownerUID types.UID, statusRefs []v1alpha1.RolloutAnalysisRunStatus) ([]*v1alpha1.AnalysisRun, error) {
	analysisRuns, err := h.analysisRunLister.AnalysisRuns(namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}

	ownedByOwner := make([]*v1alpha1.AnalysisRun, 0)
	seen := make(map[string]bool)

	for i := range analysisRuns {
		ar := analysisRuns[i]
		controllerRef := metav1.GetControllerOf(ar)
		if controllerRef != nil && controllerRef.UID == ownerUID {
			ownedByOwner = append(ownedByOwner, ar)
			seen[ar.Name] = true
		}
	}

	// Check for analysis runs referenced in status but not found in lister
	for _, arStatus := range statusRefs {
		if arStatus.Name == "" || seen[arStatus.Name] {
			continue
		}
		// Perform a get to see if it truly exists
		ar, err := h.argoProjClientset.ArgoprojV1alpha1().AnalysisRuns(namespace).Get(ctx, arStatus.Name, metav1.GetOptions{})
		if err == nil && ar != nil {
			// Found analysis run missing from informer cache
			ownedByOwner = append(ownedByOwner, ar)
		}
	}

	return ownedByOwner, nil
}

// CreateAnalysisRun creates a new AnalysisRun from the given analysis configuration
func (h *Helper) CreateAnalysisRun(
	ctx context.Context,
	rolloutAnalysis *v1alpha1.RolloutAnalysis,
	args []v1alpha1.Argument,
	namespace string,
	podHash string,
	infix string,
	labels map[string]string,
	annotations map[string]string,
	ownerRef metav1.OwnerReference,
) (*v1alpha1.AnalysisRun, error) {
	templates, clusterTemplates, err := h.getTemplatesFromRefs(ctx, rolloutAnalysis.Templates, namespace)
	if err != nil {
		return nil, err
	}

	if len(templates) == 0 && len(clusterTemplates) == 0 {
		return nil, fmt.Errorf("no templates found")
	}

	// Generate unique name with pod hash
	name := fmt.Sprintf("%s-%s-%s", ownerRef.Name, infix, podHash)
	if len(name) > 253 {
		name = name[:253]
	}

	runLabels := make(map[string]string)
	for k, v := range labels {
		runLabels[k] = v
	}

	runAnnotations := make(map[string]string)
	if rolloutAnalysis.AnalysisRunMetadata != nil {
		for k, v := range rolloutAnalysis.AnalysisRunMetadata.Annotations {
			runAnnotations[k] = v
		}
	}
	for k, v := range annotations {
		runAnnotations[k] = v
	}

	run, err := analysisutil.NewAnalysisRunFromTemplates(
		templates,
		clusterTemplates,
		args,
		rolloutAnalysis.DryRun,
		rolloutAnalysis.MeasurementRetention,
		runLabels,
		runAnnotations,
		name,
		"",
		namespace,
	)
	if err != nil {
		return nil, err
	}

	run.OwnerReferences = []metav1.OwnerReference{ownerRef}

	createdRun, err := h.argoProjClientset.ArgoprojV1alpha1().AnalysisRuns(namespace).Create(ctx, run, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	return createdRun, nil
}

// CancelAnalysisRuns terminates the given analysis runs
func (h *Helper) CancelAnalysisRuns(ctx context.Context, analysisRuns []*v1alpha1.AnalysisRun) error {
	for i := range analysisRuns {
		ar := analysisRuns[i]
		if ar == nil || ar.Status.Phase.Completed() {
			continue
		}

		_, err := h.argoProjClientset.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Patch(
			ctx,
			ar.Name,
			patchtypes.MergePatchType,
			[]byte(cancelAnalysisRunPatch),
			metav1.PatchOptions{},
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteAnalysisRuns deletes the given analysis runs based on history limits
func (h *Helper) DeleteAnalysisRuns(ctx context.Context, namespace string, selector labels.Selector, limit int) error {
	arList, err := h.analysisRunLister.AnalysisRuns(namespace).List(selector)
	if err != nil {
		return err
	}

	if len(arList) <= limit {
		return nil
	}

	// Sort by creation timestamp and delete oldest
	analysisutil.SortAnalysisRunByPodHash(arList)
	for i := 0; i < len(arList)-limit; i++ {
		err := h.argoProjClientset.ArgoprojV1alpha1().AnalysisRuns(namespace).Delete(
			ctx,
			arList[i].Name,
			metav1.DeleteOptions{},
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// getTemplatesFromRefs gets AnalysisTemplates and ClusterAnalysisTemplates from references
func (h *Helper) getTemplatesFromRefs(
	ctx context.Context,
	templateRefs []v1alpha1.AnalysisTemplateRef,
	namespace string,
) ([]*v1alpha1.AnalysisTemplate, []*v1alpha1.ClusterAnalysisTemplate, error) {
	templates := make([]*v1alpha1.AnalysisTemplate, 0)
	clusterTemplates := make([]*v1alpha1.ClusterAnalysisTemplate, 0)

	for _, templateRef := range templateRefs {
		if templateRef.ClusterScope {
			tmpl, err := h.clusterTemplateLister.Get(templateRef.TemplateName)
			if err != nil {
				return nil, nil, err
			}
			clusterTemplates = append(clusterTemplates, tmpl)
		} else {
			tmpl, err := h.analysisTemplateLister.AnalysisTemplates(namespace).Get(templateRef.TemplateName)
			if err != nil {
				return nil, nil, err
			}
			templates = append(templates, tmpl)
		}
	}

	return templates, clusterTemplates, nil
}

// NeedsNewAnalysisRun determines if a new analysis run should be created
func NeedsNewAnalysisRun(currentAr *v1alpha1.AnalysisRun, generation int64) bool {
	if currentAr == nil {
		return true
	}
	if currentAr.Status.Phase.Completed() {
		return true
	}
	// Check if analysis run is for a different generation
	if genStr, ok := currentAr.Annotations[annotations.RevisionAnnotation]; ok {
		if arGen, err := strconv.ParseInt(genStr, 10, 64); err == nil && arGen != generation {
			return true
		}
	}
	return false
}

// GetAnalysisRunStatus creates a RolloutAnalysisRunStatus from an AnalysisRun
func GetAnalysisRunStatus(ar *v1alpha1.AnalysisRun) *v1alpha1.RolloutAnalysisRunStatus {
	if ar == nil {
		return nil
	}
	return &v1alpha1.RolloutAnalysisRunStatus{
		Name:    ar.Name,
		Status:  ar.Status.Phase,
		Message: ar.Status.Message,
	}
}

// GetHistoryLimits returns the history limits for successful and unsuccessful runs
func GetHistoryLimits(analysis *v1alpha1.AnalysisRunStrategy) (int32, int32) {
	successfulLimit := int32(5)   // default
	unsuccessfulLimit := int32(5) // default

	if analysis != nil {
		if analysis.SuccessfulRunHistoryLimit != nil {
			successfulLimit = *analysis.SuccessfulRunHistoryLimit
		}
		if analysis.UnsuccessfulRunHistoryLimit != nil {
			unsuccessfulLimit = *analysis.UnsuccessfulRunHistoryLimit
		}
	}

	return successfulLimit, unsuccessfulLimit
}
