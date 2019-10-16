package rollout

import (
	log "github.com/sirupsen/logrus"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	logutil "github.com/argoproj/argo-rollouts/utils/log"
)

type rolloutContext interface {
	Rollout() *v1alpha1.Rollout
	Log() *log.Entry
}

type blueGreenContext struct {
	rollout *v1alpha1.Rollout
	log     *log.Entry
}

type canaryContext struct {
	rollout *v1alpha1.Rollout
	log     *log.Entry
}

func newBlueGreenCtx(r *v1alpha1.Rollout) *blueGreenContext {
	return &blueGreenContext{
		rollout: r,
		log:     logutil.WithRollout(r),
	}
}

func (bgCtx *blueGreenContext) Rollout() *v1alpha1.Rollout {
	return bgCtx.rollout
}

func (bgCtx *blueGreenContext) Log() *log.Entry {
	return bgCtx.log
}

func newCanaryCtx(r *v1alpha1.Rollout) *canaryContext {
	return &canaryContext{
		rollout: r,
		log:     logutil.WithRollout(r),
	}
}

func (cCtx *canaryContext) Rollout() *v1alpha1.Rollout {
	return cCtx.rollout
}

func (cCtx *canaryContext) Log() *log.Entry {
	return cCtx.log
}
