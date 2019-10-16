package rollout

import (
	log "github.com/sirupsen/logrus"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

type rolloutContext interface {
	Rollout() *v1alpha1.Rollout
	Log() *log.Entry
	NewRS() *appsv1.ReplicaSet
	AllRSs() []*appsv1.ReplicaSet
}

type blueGreenContext struct {
	rollout  *v1alpha1.Rollout
	log      *log.Entry
	newRS    *appsv1.ReplicaSet
	olderRSs []*appsv1.ReplicaSet
	allRSs   []*appsv1.ReplicaSet
}

type canaryContext struct {
	rollout  *v1alpha1.Rollout
	log      *log.Entry
	newRS    *appsv1.ReplicaSet
	stableRS *appsv1.ReplicaSet
	olderRSs []*appsv1.ReplicaSet
	allRSs   []*appsv1.ReplicaSet
}

func newBlueGreenCtx(r *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, olderRSs []*appsv1.ReplicaSet) *blueGreenContext {
	allRSs := append(olderRSs, newRS)
	return &blueGreenContext{
		rollout:  r,
		log:      logutil.WithRollout(r),
		newRS:    newRS,
		olderRSs: olderRSs,
		allRSs:   allRSs,
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

func newCanaryCtx(r *v1alpha1.Rollout, newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet, olderRSs []*appsv1.ReplicaSet) *canaryContext {
	allRSs := append(olderRSs, newRS)
	if stableRS != nil {
		allRSs = append(allRSs, stableRS)
	}
	return &canaryContext{
		rollout:  r,
		log:      logutil.WithRollout(r),
		newRS:    newRS,
		stableRS: stableRS,
		olderRSs: olderRSs,
		allRSs:   allRSs,
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
