package rollout

import (
	"context"
	"maps"
	"strconv"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/utils/labels"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	patchtypes "k8s.io/apimachinery/pkg/types"
)

type ARType string

const (
	PrePromotionk    ARType = "PrePromotion"
	PostPromotion    ARType = "PostPromotion"
	CanarySteps      ARType = "CanarySteps"
	CanaryBackground ARType = "CanaryBackground"
)

type cancelOpts struct {
	step               *v1alpha1.CanaryStep
	stepIndex          *int32
	backgroundAnalysis *v1alpha1.RolloutAnalysisBackground
}

type CancelOption func(*cancelOpts)

func WithBackgroundAnalysis(backgroundAnalysis *v1alpha1.RolloutAnalysisBackground) CancelOption {
	return func(opts *cancelOpts) {
		opts.backgroundAnalysis = backgroundAnalysis
	}
}

func WithStep(step *v1alpha1.CanaryStep) CancelOption {
	return func(opts *cancelOpts) {
		opts.step = step
	}
}
func WithStepIndex(index *int32) CancelOption {
	return func(opts *cancelOpts) {
		opts.stepIndex = index
	}
}

type shouldReturnCurOpts struct {
	pauseConditions []v1alpha1.PauseCondition
	abort           bool
}

type ShouldReturnCurOption func(*shouldReturnCurOpts)

func WithAbort(abort bool) ShouldReturnCurOption {
	return func(opts *shouldReturnCurOpts) {
		opts.abort = abort
	}
}
func WithConditions(conditions []v1alpha1.PauseCondition) ShouldReturnCurOption {
	return func(opts *shouldReturnCurOpts) {
		opts.pauseConditions = conditions
	}
}

type OptionalLabels struct {
	Labels []labels.Label[string, string]
}

type LabelsOption func(*OptionalLabels)

func WithStepIndexLabel(index string) LabelsOption {
	return func(options *OptionalLabels) {
		options.Labels = append(options.Labels, labels.NewLabel(v1alpha1.RolloutCanaryStepIndexLabel, index))
	}
}

type CurrentAnalysisRun interface {
	CurrentStatus() *v1alpha1.RolloutAnalysisRunStatus
	ShouldCancel(cancelOptions ...CancelOption) bool
	ShouldReturnCur(options ...ShouldReturnCurOption) bool
	NeedsNew(controllerPause bool, pauseConditions []v1alpha1.PauseCondition, abortedAt *metav1.Time) bool
	Infix() string
	ARType() string
	AnalysisRun() *v1alpha1.AnalysisRun
	RolloutAnalysis() *v1alpha1.RolloutAnalysis
	Labels(podHash, instanceID string, options ...LabelsOption) map[string]string
}

type NewAnalysisRunOpts struct {
	index string
}

type NewAnalysisRunOption func(*NewAnalysisRunOpts)

func WithIndex(index *int32) NewAnalysisRunOption {
	return func(options *NewAnalysisRunOpts) {
		if index == nil {
			options.index = "0"
		} else {
			options.index = strconv.Itoa(int(*index))
		}
	}
}

func NewAnalysisRun(ar *v1alpha1.AnalysisRun, artype string, options ...NewAnalysisRunOption) CurrentAnalysisRun {
	opts := &NewAnalysisRunOpts{}
	for _, option := range options {
		option(opts)
	}
	switch artype {
	case v1alpha1.RolloutTypePrePromotionLabel:
		return &BlueGreenPrePromotionAR{
			BaseRun: BaseRun{
				AnalysisType: v1alpha1.RolloutTypePrePromotionLabel,
				Run:          ar,
				infix:        "pre",
			},
		}
	case v1alpha1.RolloutTypePostPromotionLabel:
		return &BlueGreenPostPromotionAR{
			BaseRun: BaseRun{
				AnalysisType: v1alpha1.RolloutTypePostPromotionLabel,
				Run:          ar,
				infix:        "post",
			},
		}
	case v1alpha1.RolloutTypeStepLabel:
		return &CanaryStepAR{
			BaseRun: BaseRun{
				AnalysisType: v1alpha1.RolloutTypeStepLabel,
				Run:          ar,
				infix:        opts.index,
			},
		}
	case v1alpha1.RolloutTypeBackgroundRunLabel:
		return &CanaryBackgroundAR{
			BaseRun: BaseRun{
				AnalysisType: v1alpha1.RolloutTypeBackgroundRunLabel,
				Run:          ar,
				infix:        "",
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
	AnalysisType    string
	infix           string
	Run             *v1alpha1.AnalysisRun
}

func (ar *BaseRun) ARType() string {
	return ar.AnalysisType
}

func (ar *BaseRun) Infix() string {
	return ar.infix
}

func (ar *BaseRun) CurrentStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return &v1alpha1.RolloutAnalysisRunStatus{
		Name:    ar.Run.Name,
		Status:  ar.Run.Status.Phase,
		Message: ar.Run.Status.Message,
	}
}

func (ar *BaseRun) ShouldCancel(cancelOptions ...CancelOption) bool {
	return false
}

func (ar *BaseRun) ShouldReturnCur(options ...ShouldReturnCurOption) bool {
	opts := &shouldReturnCurOpts{}
	for _, option := range options {
		option(opts)
	}

	return pauseConditionsInclude(opts.pauseConditions, v1alpha1.PauseReasonInconclusiveAnalysis)
}

func (ar *BaseRun) NeedsNew(controllerPause bool, pauseConditions []v1alpha1.PauseCondition, abortedAt *metav1.Time) bool {
	// is this correct in terms of boolean ordering?
	return ar == nil ||
		validPause(controllerPause, pauseConditions) && ar.Run.Status.Phase == v1alpha1.AnalysisPhaseInconclusive ||
		abortedAt != nil
}

func (ar *BaseRun) RolloutAnalysis() *v1alpha1.RolloutAnalysis {
	return ar.rolloutAnalysis
}

func (ar *BaseRun) AnalysisRun() *v1alpha1.AnalysisRun {
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
		v1alpha1.RolloutTypeLabel:             ar.AnalysisType,
	}
	if instanceID != "" {
		labels[v1alpha1.LabelKeyControllerInstanceID] = instanceID
	}
	// this overrides any existing label in labels with the same key from optional labels.
	// is this the behavior we want?
	maps.Copy(labels, optionalLabels)
	return labels

}

type BlueGreenPrePromotionAR struct {
	BaseRun
}

func (ar *BlueGreenPrePromotionAR) ShouldCancel(cancelOptions ...CancelOption) bool {
	// tackle should skip pre promotion option here
	return ar.rolloutAnalysis == nil
}

type BlueGreenPostPromotionAR struct {
	BaseRun
}

func (ar *BlueGreenPostPromotionAR) ShouldCancel(cancelOptions ...CancelOption) bool {
	// tackle should skip post promotion option here
	return ar.rolloutAnalysis == nil
}

type CanaryStepAR struct {
	BaseRun
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

func (ar *CanaryStepAR) Labels(podHash, instanceID string, options ...LabelsOption) map[string]string {
	return ar.BaseRun.Labels(podHash, instanceID, WithStepIndexLabel(ar.BaseRun.infix))
}

type CanaryBackgroundAR struct {
	BaseRun
}

func (ar *CanaryBackgroundAR) ShouldCancel(options ...CancelOption) bool {
	opts := &cancelOpts{}
	for _, option := range options {
		option(opts)
	}

	return opts.backgroundAnalysis == nil || len(opts.backgroundAnalysis.Templates) == 0
}

type CurrentAnalysisRuns struct {
	CurrentBlueGreenPrePromotion  *BlueGreenPrePromotionAR
	CurrentBlueGreenPostPromotion *BlueGreenPostPromotionAR
	CurrentCanaryStep             *CanaryStepAR
	CurrentCanaryBackground       *CanaryBackgroundAR
}

type AnalysisContext struct {
	CurrentAnalysisRuns
	otherArs []*v1alpha1.AnalysisRun
	log      *log.Entry
}

func (ac *AnalysisContext) UpdateCurrentAnalysisRuns(ar *v1alpha1.AnalysisRun, artype string, options ...NewAnalysisRunOption) *AnalysisContext {
	switch artype {
	case v1alpha1.RolloutTypePrePromotionLabel:
		ac.CurrentAnalysisRuns.CurrentBlueGreenPrePromotion = &BlueGreenPrePromotionAR{
			BaseRun: BaseRun{
				AnalysisType: v1alpha1.RolloutTypePrePromotionLabel,
				Run:          ar,
				infix:        "pre",
			},
		}
	case v1alpha1.RolloutTypePostPromotionLabel:
		ac.CurrentAnalysisRuns.CurrentBlueGreenPostPromotion = &BlueGreenPostPromotionAR{
			BaseRun: BaseRun{
				AnalysisType: v1alpha1.RolloutTypePostPromotionLabel,
				Run:          ar,
				infix:        "post",
			},
		}
	case v1alpha1.RolloutTypeStepLabel:
		ac.CurrentAnalysisRuns.CurrentCanaryStep = &CanaryStepAR{
			BaseRun: BaseRun{
				AnalysisType: v1alpha1.RolloutTypeStepLabel,
				Run:          ar,
			},
		}
	case v1alpha1.RolloutTypeBackgroundRunLabel:
		ac.CurrentAnalysisRuns.CurrentCanaryBackground = &CanaryBackgroundAR{
			BaseRun: BaseRun{
				AnalysisType: v1alpha1.RolloutTypeBackgroundRunLabel,
				Run:          ar,
				infix:        "",
			},
		}
	}

	return ac
}

func NewAnalysisContext(analysisRuns []*v1alpha1.AnalysisRun, r *v1alpha1.Rollout) *AnalysisContext {
	ac := &AnalysisContext{}
	otherArs := []*v1alpha1.AnalysisRun{}
	getArName := func(s *v1alpha1.RolloutAnalysisRunStatus) string {
		if s == nil {
			return ""
		}
		return s.Name
	}
	for i := range analysisRuns {
		ar := analysisRuns[i]
		if ar != nil {
			switch ar.Name {
			case getArName(r.Status.Canary.CurrentStepAnalysisRunStatus):
				ac.UpdateCurrentAnalysisRuns(ar, v1alpha1.RolloutTypeStepLabel)
			case getArName(r.Status.Canary.CurrentBackgroundAnalysisRunStatus):
				ac.UpdateCurrentAnalysisRuns(ar, v1alpha1.RolloutTypeBackgroundRunLabel)
			case getArName(r.Status.BlueGreen.PrePromotionAnalysisRunStatus):
				ac.UpdateCurrentAnalysisRuns(ar, v1alpha1.RolloutTypePrePromotionLabel)
			case getArName(r.Status.BlueGreen.PostPromotionAnalysisRunStatus):
				ac.UpdateCurrentAnalysisRuns(ar, v1alpha1.RolloutTypePostPromotionLabel)
			default:
				otherArs = append(otherArs, ar)
			}
		}
	}
	ac.otherArs = otherArs
	return ac
}

// func NewAnalysisContext(currentArs CurrentAnalysisRuns, otherArs []*v1alpha1.AnalysisRun) *AnalysisContext {
// 	return &AnalysisContext{
// 		CurrentAnalysisRuns: currentArs,
// 		otherArs:            otherArs,
// 	}
// }

func (c *AnalysisContext) AllCurrentAnalysisRuns() []CurrentAnalysisRun {
	return []CurrentAnalysisRun{
		c.CurrentBlueGreenPrePromotion,
		c.CurrentBlueGreenPostPromotion,
		c.CurrentCanaryStep,
		c.CurrentCanaryBackground,
	}
}
func (c *AnalysisContext) CurrentAnalysisRunsToArray() []*v1alpha1.AnalysisRun {
	currentAnalysisRuns := []*v1alpha1.AnalysisRun{}
	if c.CurrentBlueGreenPrePromotion.Run != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.CurrentBlueGreenPrePromotion.Run)
	}
	if c.CurrentBlueGreenPostPromotion.Run != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.CurrentBlueGreenPostPromotion.Run)
	}
	if c.CurrentCanaryStep.Run != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.CurrentCanaryStep.Run)
	}
	if c.CurrentCanaryBackground.Run != nil {
		currentAnalysisRuns = append(currentAnalysisRuns, c.CurrentCanaryBackground.Run)
	}
	return currentAnalysisRuns
}

func (c *AnalysisContext) AllAnalysisRuns() []*v1alpha1.AnalysisRun {
	return append(c.CurrentAnalysisRunsToArray(), c.otherArs...)
}

func (ac *AnalysisContext) BlueGreenPrePromotionARStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return ac.CurrentBlueGreenPrePromotion.CurrentStatus()
}

func (ac *AnalysisContext) BlueGreenPostPromotionARStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return ac.CurrentBlueGreenPostPromotion.CurrentStatus()
}

func (ac *AnalysisContext) CanaryStepARStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return ac.CurrentCanaryStep.CurrentStatus()
}

func (ac *AnalysisContext) CanaryBackgroundARStatus() *v1alpha1.RolloutAnalysisRunStatus {
	return ac.CurrentCanaryBackground.CurrentStatus()
}

func (c *AnalysisContext) cancelAllAnalysisRuns(client clientset.Interface) error {
	return c.cancelAnalysisRuns(client, c.AllAnalysisRuns())
}
func (c *AnalysisContext) cancelOldAnalysisRuns(analysisRuns []*v1alpha1.AnalysisRun) error {
	return nil
}

func (c *AnalysisContext) cancelAnalysisRuns(client clientset.Interface, analysisRuns []*v1alpha1.AnalysisRun) error {
	ctx := context.TODO()
	for _, ar := range analysisRuns {
		isNotCompleted := ar == nil || !ar.Status.Phase.Completed()
		if !ar.Spec.Terminate && isNotCompleted {
			c.log.WithField(logutil.AnalysisRunKey, ar.Name).Infof("Canceling the analysis run '%s'", ar.Name)
			_, err := client.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Patch(ctx, ar.Name, patchtypes.MergePatchType, []byte(cancelAnalysisRun), metav1.PatchOptions{})
			if err != nil {
				if k8serrors.IsNotFound(err) {
					c.log.Warnf("AnalysisRun '%s' not found", ar.Name)
					continue
				}
				return err
			}
		}
	}
	return nil
}

func (c *AnalysisContext) deleteAnalysisRuns(client clientset.Interface, ars []*v1alpha1.AnalysisRun) error {
	ctx := context.TODO()
	for i := range ars {
		ar := ars[i]
		if ar.DeletionTimestamp != nil {
			continue
		}
		c.log.Infof("Trying to cleanup analysis run '%s'", ar.Name)
		err := client.ArgoprojV1alpha1().AnalysisRuns(ar.Namespace).Delete(ctx, ar.Name, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
