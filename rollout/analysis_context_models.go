package rollout

import (
	"maps"
	"strconv"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/labels"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BlueGreenPostPromotionAR struct {
	BaseRun
}

func NewAnalysisRun(ar *v1alpha1.AnalysisRun, artype string) CurrentAnalysisRun {
	switch artype {
	case v1alpha1.RolloutTypePrePromotionLabel:
		return &BlueGreenPrePromotionAR{
			BaseRun: BaseRun{
				Run: ar,
			},
		}
	case v1alpha1.RolloutTypePostPromotionLabel:
		return &BlueGreenPostPromotionAR{
			BaseRun: BaseRun{
				Run: ar,
			},
		}
	case v1alpha1.RolloutTypeStepLabel:
		return &CanaryStepAR{
			BaseRun: BaseRun{
				Run: ar,
			},
		}
	case v1alpha1.RolloutTypeBackgroundRunLabel:
		return &CanaryBackgroundAR{
			BaseRun: BaseRun{
				Run: ar,
			},
		}
	default:
		return &BaseRun{
			Run: ar,
		}
	}
}

type BaseRun struct {
	rolloutAnalysis *v1alpha1.RolloutAnalysis
	Run             *v1alpha1.AnalysisRun
}

func (ar *BaseRun) IsPresent() bool {
	return ar != nil && ar.Run != nil
}

func (ar *BaseRun) ARType() string {
	return ""
}

func (ar *BaseRun) Infix(options ...InfixOption) string {
	return ""
}

func (ar *BaseRun) CurrentStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return &v1alpha1.RolloutAnalysisRunStatus{
		Name:    ar.Run.Name,
		Status:  ar.Run.Status.Phase,
		Message: ar.Run.Status.Message,
	}
}

func (ar *BaseRun) ShouldCancel(cancelOptions ...CancelOption) bool {
	opts := &cancelOpts{}
	for _, option := range cancelOptions {
		option(opts)
	}
	return opts.analysis == nil || opts.shouldSkip
}

func (ar *BaseRun) ShouldReturnCur(options ...ShouldReturnCurOption) bool {
	opts := &shouldReturnCurOpts{}
	for _, option := range options {
		option(opts)
	}

	return pauseConditionsInclude(opts.pauseConditions, v1alpha1.PauseReasonInconclusiveAnalysis)
}

func (ar *BaseRun) NeedsNew(controllerPause bool, pauseConditions []v1alpha1.PauseCondition, abortedAt *metav1.Time) bool {
	return ar.Run == nil ||
		validPause(controllerPause, pauseConditions) && ar.Run.Status.Phase == v1alpha1.AnalysisPhaseInconclusive ||
		abortedAt != nil
}

func (ar *BaseRun) OutsideAnalysisBoundaries(options ...OutsideAnalysisBoundariesOption) bool {
	return false
}

func (ar *BaseRun) RolloutAnalysis() *v1alpha1.RolloutAnalysis {
	if ar == nil {
		return nil
	}
	return ar.rolloutAnalysis
}

func (ar *BaseRun) AnalysisRun() *v1alpha1.AnalysisRun {
	if ar == nil {
		return nil
	}
	return ar.Run
}

func (ar *BaseRun) Labels(podHash, instanceID string, options ...LabelsOption) map[string]string {
	opts := &OptionalLabels{}
	for _, option := range options {
		option(opts)
	}

	optionalLabels := labels.MergeLabels(opts.Labels)
	labels := map[string]string{
		v1alpha1.DefaultRolloutUniqueLabelKey: podHash,
		v1alpha1.RolloutTypeLabel:             ar.ARType(),
	}
	if instanceID != "" {
		labels[v1alpha1.LabelKeyControllerInstanceID] = instanceID
	}
	// this overrides any existing label in labels with the same key from optional labels.
	// is this the behavior we want?
	maps.Copy(labels, optionalLabels)
	return labels
}

func (ar *BaseRun) UpdateRun(run *v1alpha1.AnalysisRun) {
	ar.Run = run
}

type BlueGreenPrePromotionAR struct {
	BaseRun
}

func (ar *BlueGreenPrePromotionAR) Infix(options ...InfixOption) string {
	return "pre"

}

func (ar *BlueGreenPrePromotionAR) ARType() string {
	return v1alpha1.RolloutTypePrePromotionLabel
}

func (ar *BlueGreenPostPromotionAR) Infix(options ...InfixOption) string {
	return "post"
}

func (ar *BlueGreenPostPromotionAR) ARType() string {
	return v1alpha1.RolloutTypePostPromotionLabel
}

type CanaryStepAR struct {
	BaseRun
}

func (ar *CanaryStepAR) Infix(options ...InfixOption) string {
	opts := &InfixOpts{}
	for _, option := range options {
		option(opts)
	}
	if opts.index == nil {
		return ""
	}
	return strconv.Itoa(int(*opts.index))
}

func (ar *CanaryStepAR) ARType() string {
	return v1alpha1.RolloutTypeStepLabel
}

func (ar *CanaryStepAR) ShouldCancel(options ...CancelOption) bool {
	opts := &cancelOpts{}
	for _, option := range options {
		option(opts)
	}
	return opts.step == nil || opts.step.Analysis == nil || opts.stepIndex == nil || (ar != nil && ar.Run != nil && ar.Run.GetLabels()[v1alpha1.RolloutCanaryStepIndexLabel] != strconv.Itoa(int(*opts.stepIndex)))
}

func (ar *CanaryStepAR) ShouldReturnCur(options ...ShouldReturnCurOption) bool {
	opts := &shouldReturnCurOpts{}
	for _, option := range options {
		option(opts)
	}

	return len(opts.pauseConditions) > 0 || opts.abort
}

type CanaryBackgroundAR struct {
	BaseRun
}

func (ar *CanaryBackgroundAR) Infix(options ...InfixOption) string {
	return ""
}

func (ar *CanaryBackgroundAR) ARType() string {
	return v1alpha1.RolloutTypeBackgroundRunLabel
}

func (ar *CanaryBackgroundAR) ShouldCancel(options ...CancelOption) bool {
	opts := &cancelOpts{}
	for _, option := range options {
		option(opts)
	}

	return opts.backgroundAnalysis == nil || len(opts.backgroundAnalysis.Templates) == 0
}

func (ar *CanaryBackgroundAR) OutsideAnalysisBoundaries(options ...OutsideAnalysisBoundariesOption) bool {
	opts := &OutsideAnalysisBoundariesOpts{}
	for _, option := range options {
		option(opts)
	}
	return opts.isBeforeStartingStep || opts.isFullyPromoted || opts.isJustCreated
}
