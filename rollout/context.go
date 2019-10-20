package rollout

import (
	log "github.com/sirupsen/logrus"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	analysisutil "github.com/argoproj/argo-rollouts/utils/analysis"
	experimentutil "github.com/argoproj/argo-rollouts/utils/experiment"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

type rolloutContext interface {
	Rollout() *v1alpha1.Rollout
	Log() *log.Entry
	NewRS() *appsv1.ReplicaSet
	AllRSs() []*appsv1.ReplicaSet

	CurrentAnalysisRuns() []*v1alpha1.AnalysisRun
	OtherAnalysisRuns() []*v1alpha1.AnalysisRun
	CurrentExperiment() *v1alpha1.Experiment
	OtherExperiments() []*v1alpha1.Experiment

	PauseContext() *pauseContext
}

type blueGreenContext struct {
	rollout *v1alpha1.Rollout
	log     *log.Entry

	newRS    *appsv1.ReplicaSet
	olderRSs []*appsv1.ReplicaSet
	allRSs   []*appsv1.ReplicaSet

	pauseContext *pauseContext
}

type canaryContext struct {
	rollout *v1alpha1.Rollout
	log     *log.Entry

	newRS    *appsv1.ReplicaSet
	stableRS *appsv1.ReplicaSet
	olderRSs []*appsv1.ReplicaSet
	allRSs   []*appsv1.ReplicaSet

	currentArs []*v1alpha1.AnalysisRun
	otherArs   []*v1alpha1.AnalysisRun

	currentEx *v1alpha1.Experiment
	otherExs  []*v1alpha1.Experiment

	pauseContext *pauseContext
}

func newBlueGreenCtx(r *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, olderRSs []*appsv1.ReplicaSet) *blueGreenContext {
	allRSs := append(olderRSs, newRS)
	logCtx := logutil.WithRollout(r)
	return &blueGreenContext{
		rollout: r,
		log:     logCtx,

		newRS:    newRS,
		olderRSs: olderRSs,
		allRSs:   allRSs,

		pauseContext: &pauseContext{
			log:             logCtx,
			controllerPause: r.Status.ControllerPause,
			pauseStartTime:  r.Status.PauseStartTime,
			pauseConditions: r.Status.PauseConditions,
		},
	}
}

func (bgCtx *blueGreenContext) Rollout() *v1alpha1.Rollout {
	return bgCtx.rollout
}

func (bgCtx *blueGreenContext) Log() *log.Entry {
	return bgCtx.log
}

func (bgCtx *blueGreenContext) NewRS() *appsv1.ReplicaSet {
	return bgCtx.newRS
}

func (bgCtx *blueGreenContext) OlderRSs() []*appsv1.ReplicaSet {
	return bgCtx.olderRSs
}

func (bgCtx *blueGreenContext) AllRSs() []*appsv1.ReplicaSet {
	return bgCtx.allRSs
}

func (bgCtx *blueGreenContext) CurrentExperiment() *v1alpha1.Experiment {
	return nil
}
func (bgCtx *blueGreenContext) CurrentAnalysisRuns() []*v1alpha1.AnalysisRun {
	return nil
}

func (bgCtx *blueGreenContext) OtherAnalysisRuns() []*v1alpha1.AnalysisRun {
	return nil
}

func (bgCtx *blueGreenContext) OtherExperiments() []*v1alpha1.Experiment {
	return nil
}

func (bgCtx *blueGreenContext) PauseContext() *pauseContext {
	return bgCtx.pauseContext
}

func newCanaryCtx(r *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, olderRSs []*appsv1.ReplicaSet, exList []*v1alpha1.Experiment, arList []*v1alpha1.AnalysisRun) *canaryContext {
	allRSs := append(olderRSs, newRS)
	if stableRS != nil {
		allRSs = append(allRSs, stableRS)
	}

	currentArs, otherArs := analysisutil.FilterCurrentRolloutAnalysisRuns(arList, r)
	currentEx := experimentutil.GetCurrentExperiment(r, exList)
	otherExs := experimentutil.GetOldExperiments(r, exList)
	logCtx := logutil.WithRollout(r)
	return &canaryContext{
		rollout:  r,
		log:      logCtx,
		newRS:    newRS,
		stableRS: stableRS,
		olderRSs: olderRSs,
		allRSs:   allRSs,

		currentArs: currentArs,
		otherArs:   otherArs,

		currentEx: currentEx,
		otherExs:  otherExs,

		pauseContext: &pauseContext{
			log:             logCtx,
			controllerPause: r.Status.ControllerPause,
			pauseStartTime:  r.Status.PauseStartTime,
			pauseConditions: r.Status.PauseConditions,
		},
	}
}

func (cCtx *canaryContext) Rollout() *v1alpha1.Rollout {
	return cCtx.rollout
}

func (cCtx *canaryContext) Log() *log.Entry {
	return cCtx.log
}

func (cCtx *canaryContext) NewRS() *appsv1.ReplicaSet {
	return cCtx.newRS
}

func (cCtx *canaryContext) StableRS() *appsv1.ReplicaSet {
	return cCtx.stableRS
}

func (cCtx *canaryContext) OlderRSs() []*appsv1.ReplicaSet {
	return cCtx.olderRSs
}

func (cCtx *canaryContext) AllRSs() []*appsv1.ReplicaSet {
	return cCtx.allRSs
}

func (cCtx *canaryContext) SetCurrentAnalysisRuns(ars []*v1alpha1.AnalysisRun) {
	cCtx.currentArs = ars
}

func (cCtx *canaryContext) CurrentAnalysisRuns() []*v1alpha1.AnalysisRun {
	return cCtx.currentArs
}
func (cCtx *canaryContext) OtherAnalysisRuns() []*v1alpha1.AnalysisRun {
	return cCtx.otherArs
}

func (cCtx *canaryContext) SetCurrentExperiment(ex *v1alpha1.Experiment) {
	cCtx.currentEx = ex
}

func (cCtx *canaryContext) CurrentExperiment() *v1alpha1.Experiment {
	return cCtx.currentEx
}

func (cCtx *canaryContext) OtherExperiments() []*v1alpha1.Experiment {
	return cCtx.otherExs
}

func (cCtx *canaryContext) PauseContext() *pauseContext {
	return cCtx.pauseContext
}
