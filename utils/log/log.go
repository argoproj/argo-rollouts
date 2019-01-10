package log

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/argoproj/rollout-controller/pkg/apis/rollouts/v1alpha1"
)

const (
	// RolloutKey defines the key for the rollout field
	RolloutKey = "rollout"
)

// WithRollout returns a logging context with the rollout field set
func WithRollout(rollout *v1alpha1.Rollout) *log.Entry {
	return log.WithField(RolloutKey, fmt.Sprintf("%s/%s", rollout.Namespace, rollout.Name))
}
