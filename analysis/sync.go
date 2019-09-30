package analysis

import (
	patchtypes "k8s.io/apimachinery/pkg/types"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/diff"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

func (c *AnalysisController) persistAnalysisRunStatus(orig *v1alpha1.AnalysisRun, newStatus *v1alpha1.AnalysisRunStatus) error {
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
		return err
	}
	if !modified {
		logCtx.Info("No status changes. Skipping patch")
		return nil
	}
	logCtx.Debugf("AnalysisRun Patch: %s", patch)
	_, err = c.argoProjClientset.ArgoprojV1alpha1().AnalysisRuns(orig.Namespace).Patch(orig.Name, patchtypes.MergePatchType, patch)
	if err != nil {
		logCtx.Warningf("Error updating analysisRun: %v", err)
		return err
	}
	logCtx.Info("Patch status successfully")
	return nil
}
