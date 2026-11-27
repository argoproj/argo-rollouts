package analysis

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/diff"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

func (c *Controller) persistAnalysisRunStatus(ctx context.Context, orig *v1alpha1.AnalysisRun, newStatus v1alpha1.AnalysisRunStatus, beforePatch func()) (*v1alpha1.AnalysisRun, error) {
	logCtx := logutil.WithAnalysisRun(orig)
	patch, modified, err := diff.CreateTwoWayMergePatch(
		&v1alpha1.AnalysisRun{
			Status: orig.Status,
		},
		&v1alpha1.AnalysisRun{
			Status: newStatus,
		}, v1alpha1.AnalysisRun{})
	if err != nil {
		logCtx.Errorf("Error constructing AnalysisRun status patch: %v", err)
		return nil, err
	}
	if !modified {
		logCtx.Info("No status changes. Skipping patch")
		return orig, nil
	}
	logCtx.Debugf("AnalysisRun Patch: %s", patch)
	if beforePatch != nil {
		beforePatch()
	}
	persistedRun, err := c.argoProjClientset.ArgoprojV1alpha1().AnalysisRuns(orig.Namespace).Patch(ctx, orig.Name, patchtypes.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		logCtx.Warningf("Error updating analysisRun: %v", err)
		return nil, err
	}
	logCtx.Info("Patch status successfully")
	return persistedRun, nil
}
