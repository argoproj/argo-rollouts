package rollout

import (
	"context"
	"fmt"
	"maps"
	"strconv"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	clientset "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned"
	"github.com/argoproj/argo-rollouts/utils/labels"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
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

// msg := fmt.Sprintf("%s Analysis Run '%s' Status New: '%s' Previous: '%s'", arType, ar.Name, ar.Status.Phase, prevStatusStr)
// c.recorder.Eventf(c.rollout, record.EventOptions{EventType: eventType, EventReason: "AnalysisRun" + string(ar.Status.Phase)}, msg)
type AnalysisRunEvent struct {
	msg         string
	EventType   string
	EventReason string
}

type cancelOpts struct {
	step               *v1alpha1.CanaryStep
	stepIndex          *int32
	backgroundAnalysis *v1alpha1.RolloutAnalysisBackground
}

type CancelOption func(*cancelOpts)

func WithBackgroundAnalysis(canaryStrat *v1alpha1.CanaryStrategy) CancelOption {
	var analysis *v1alpha1.RolloutAnalysisBackground
	if canaryStrat != nil {
		analysis = canaryStrat.Analysis
	}
	return func(opts *cancelOpts) {
		opts.backgroundAnalysis = analysis
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

func WithStepIndexLabel(index *int32) LabelsOption {
	if index == nil {
		return func(options *OptionalLabels) {}
	}
	return func(options *OptionalLabels) {
		options.Labels = append(
			options.Labels,
			labels.NewLabel(
				v1alpha1.RolloutCanaryStepIndexLabel,
				strconv.Itoa(int(*index)),
			),
		)
	}
}

type InfixOpts struct {
	index *int32
}

type InfixOption func(*InfixOpts)

func InfixWithIndex(index *int32) InfixOption {
	return func(opts *InfixOpts) {
		opts.index = index
	}
}

type CurrentAnalysisRun interface {
	CurrentStatus() *v1alpha1.RolloutAnalysisRunStatus
	ShouldCancel(cancelOptions ...CancelOption) bool
	ShouldReturnCur(options ...ShouldReturnCurOption) bool
	NeedsNew(controllerPause bool, pauseConditions []v1alpha1.PauseCondition, abortedAt *metav1.Time) bool
	Infix(options ...InfixOption) string
	ARType() string
	AnalysisRun() *v1alpha1.AnalysisRun
	RolloutAnalysis() *v1alpha1.RolloutAnalysis
	Labels(podHash, instanceID string, options ...LabelsOption) map[string]string
	IsPresent() bool
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
			},
		}
	case v1alpha1.RolloutTypePostPromotionLabel:
		return &BlueGreenPostPromotionAR{
			BaseRun: BaseRun{
				AnalysisType: v1alpha1.RolloutTypePostPromotionLabel,
				Run:          ar,
			},
		}
	case v1alpha1.RolloutTypeStepLabel:
		return &CanaryStepAR{
			BaseRun: BaseRun{
				AnalysisType: v1alpha1.RolloutTypeStepLabel,
				Run:          ar,
			},
		}
	case v1alpha1.RolloutTypeBackgroundRunLabel:
		return &CanaryBackgroundAR{
			BaseRun: BaseRun{
				AnalysisType: v1alpha1.RolloutTypeBackgroundRunLabel,
				Run:          ar,
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
	Run             *v1alpha1.AnalysisRun
}

func (ar *BaseRun) IsPresent() bool {
	return ar != nil && ar.Run != nil
}

func (ar *BaseRun) ARType() string {
	if ar == nil {
		return ""
	}
	return ar.AnalysisType
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
	return ar == nil ||
		validPause(controllerPause, pauseConditions) && ar.Run.Status.Phase == v1alpha1.AnalysisPhaseInconclusive ||
		abortedAt != nil
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

func (ar *BlueGreenPrePromotionAR) Infix(options ...InfixOption) string {
	return "pre"

}

func (ar *BlueGreenPrePromotionAR) ARType() string {
	return v1alpha1.RolloutTypePrePromotionLabel
}

func (ar *BlueGreenPrePromotionAR) IsPresent() bool {
	return ar != nil && ar.Run != nil
}

func (ar *BlueGreenPrePromotionAR) AnalysisRun() *v1alpha1.AnalysisRun {
	if ar == nil {
		return nil
	}
	return ar.Run
}
func (ar *BlueGreenPrePromotionAR) ShouldCancel(cancelOptions ...CancelOption) bool {
	return ar == nil || ar.rolloutAnalysis == nil
}

func (ar *BlueGreenPrePromotionAR) RolloutAnalysis() *v1alpha1.RolloutAnalysis {
	if ar == nil {
		return nil
	}
	return ar.rolloutAnalysis
}

func (ar *BlueGreenPrePromotionAR) Labels(podHash, instanceID string, options ...LabelsOption) map[string]string {
	if ar == nil {
		baseRun := BaseRun{}
		return baseRun.Labels(podHash, instanceID, options...)
	}
	return ar.BaseRun.Labels(podHash, instanceID, options...)
}

type BlueGreenPostPromotionAR struct {
	BaseRun
}

func (ar *BlueGreenPostPromotionAR) Infix(options ...InfixOption) string {
	return "post"
}

func (ar *BlueGreenPostPromotionAR) ARType() string {
	return v1alpha1.RolloutTypePostPromotionLabel
}

func (ar *BlueGreenPostPromotionAR) AnalysisRun() *v1alpha1.AnalysisRun {
	if ar == nil {
		return nil
	}
	return ar.Run
}

func (ar *BlueGreenPostPromotionAR) IsPresent() bool {
	return ar != nil && ar.Run != nil
}

func (ar *BlueGreenPostPromotionAR) ShouldCancel(cancelOptions ...CancelOption) bool {
	return ar == nil || ar.rolloutAnalysis == nil
}

func (ar *BlueGreenPostPromotionAR) RolloutAnalysis() *v1alpha1.RolloutAnalysis {
	if ar == nil {
		return nil
	}
	return ar.rolloutAnalysis
}

func (ar *BlueGreenPostPromotionAR) Labels(podHash, instanceID string, options ...LabelsOption) map[string]string {
	if ar == nil {
		baseRun := BaseRun{}
		return baseRun.Labels(podHash, instanceID, options...)
	}
	return ar.BaseRun.Labels(podHash, instanceID, options...)
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

func (ar *CanaryStepAR) AnalysisRun() *v1alpha1.AnalysisRun {
	if ar == nil {
		return nil
	}
	return ar.Run
}

func (ar *CanaryStepAR) IsPresent() bool {
	return ar != nil && ar.Run != nil
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
	if ar == nil {
		baseRun := BaseRun{}
		return baseRun.Labels(podHash, instanceID, options...)
	}
	return ar.BaseRun.Labels(podHash, instanceID, options...)
}

func (ar *CanaryStepAR) RolloutAnalysis() *v1alpha1.RolloutAnalysis {
	if ar == nil {
		return nil
	}
	return ar.rolloutAnalysis
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

func (ar *CanaryBackgroundAR) IsPresent() bool {
	return ar != nil && ar.Run != nil
}

func (ar *CanaryBackgroundAR) AnalysisRun() *v1alpha1.AnalysisRun {
	if ar == nil {
		return nil
	}
	return ar.Run
}

func (ar *CanaryBackgroundAR) ShouldCancel(options ...CancelOption) bool {
	opts := &cancelOpts{}
	for _, option := range options {
		option(opts)
	}

	return opts.backgroundAnalysis == nil || len(opts.backgroundAnalysis.Templates) == 0
}

func (ar *CanaryBackgroundAR) ShouldReturnCur(options ...ShouldReturnCurOption) bool {
	opts := &shouldReturnCurOpts{}
	for _, option := range options {
		option(opts)
	}

	return pauseConditionsInclude(opts.pauseConditions, v1alpha1.PauseReasonInconclusiveAnalysis)
}

func (ar *CanaryBackgroundAR) NeedsNew(controllerPause bool, pauseConditions []v1alpha1.PauseCondition, abortedAt *metav1.Time) bool {
	return ar == nil ||
		validPause(controllerPause, pauseConditions) && ar.Run.Status.Phase == v1alpha1.AnalysisPhaseInconclusive ||
		abortedAt != nil
}

func (ar *CanaryBackgroundAR) RolloutAnalysis() *v1alpha1.RolloutAnalysis {
	if ar == nil {
		return nil
	}
	return ar.rolloutAnalysis
}

func (ar *CanaryBackgroundAR) Labels(podHash, instanceID string, options ...LabelsOption) map[string]string {
	if ar == nil {
		baseRun := BaseRun{}
		return baseRun.Labels(podHash, instanceID, options...)
	}
	return ar.BaseRun.Labels(podHash, instanceID, options...)
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
			},
		}
	case v1alpha1.RolloutTypePostPromotionLabel:
		ac.CurrentAnalysisRuns.CurrentBlueGreenPostPromotion = &BlueGreenPostPromotionAR{
			BaseRun: BaseRun{
				AnalysisType: v1alpha1.RolloutTypePostPromotionLabel,
				Run:          ar,
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
			},
		}
	}

	return ac
}

func NewAnalysisContext(analysisRuns []*v1alpha1.AnalysisRun, r *v1alpha1.Rollout) *AnalysisContext {
	fmt.Println("NewAnalysisContext", analysisRuns, r)
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

func (ac *AnalysisContext) BlueGreenPrePromotionAR() *v1alpha1.AnalysisRun {
	if ac.CurrentBlueGreenPrePromotion == nil {
		return nil
	}
	return ac.CurrentBlueGreenPrePromotion.AnalysisRun()
}

func (ac *AnalysisContext) BlueGreenPostPromotionAR() *v1alpha1.AnalysisRun {
	if ac.CurrentBlueGreenPostPromotion == nil {
		return nil
	}
	return ac.CurrentBlueGreenPostPromotion.AnalysisRun()
}

func (ac *AnalysisContext) CanaryStepAR() *v1alpha1.AnalysisRun {
	if ac.CurrentCanaryStep == nil {
		return nil
	}
	return ac.CurrentCanaryStep.AnalysisRun()
}

func (ac *AnalysisContext) CanaryBackgroundAR() *v1alpha1.AnalysisRun {
	if ac.CurrentCanaryBackground == nil {
		return nil
	}
	return ac.CurrentCanaryBackground.AnalysisRun()
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

func (c *AnalysisContext) emitAnalysisRunStatusChanges(prevStatus *v1alpha1.RolloutAnalysisRunStatus, ar *v1alpha1.AnalysisRun, arType string) *AnalysisRunEvent {
	if ar.Status.Phase != "" {
		if prevStatus == nil || prevStatus.Name == ar.Name && prevStatus.Status != ar.Status.Phase {
			prevStatusStr := "NoPreviousStatus"
			if prevStatus != nil {
				prevStatusStr = string(prevStatus.Status)
			}

			eventType := corev1.EventTypeNormal
			if ar.Status.Phase == v1alpha1.AnalysisPhaseFailed || ar.Status.Phase == v1alpha1.AnalysisPhaseError {
				eventType = corev1.EventTypeWarning
			}
			msg := fmt.Sprintf("%s Analysis Run '%s' Status New: '%s' Previous: '%s'", arType, ar.Name, ar.Status.Phase, prevStatusStr)
			return &AnalysisRunEvent{
				msg:         msg,
				EventType:   eventType,
				EventReason: "AnalysisRun" + string(ar.Status.Phase),
			}
		}
	}
	return nil
}

// reconcileAnalysisRunStatusChanges for each analysisRun type, the controller checks if the analysis run status has changed
// for that type
func (c *AnalysisContext) reconcileAnalysisRunStatusChanges(previousStatuses map[string]*v1alpha1.RolloutAnalysisRunStatus) []*AnalysisRunEvent {
	events := make([]*AnalysisRunEvent, 0)
	for _, run := range c.AllCurrentAnalysisRuns() {
		if run.IsPresent() {
			event := c.emitAnalysisRunStatusChanges(previousStatuses[run.ARType()], run.AnalysisRun(), run.ARType())
			if event != nil {
				events = append(events, event)
			}
		}
	}
	return events
}
