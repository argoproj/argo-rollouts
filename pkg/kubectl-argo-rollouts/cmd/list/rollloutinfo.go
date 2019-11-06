package list

import (
	"fmt"
	"strconv"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
)

const (
	headerFmtString = "NAME\tSTRATEGY\tSTATUS\tSTEP\tSET-WEIGHT\tREADY\tDESIRED\tUP-TO-DATE\tAVAILABLE\n"
	// column values are padded to be roughly equal to the character lengths of the headers, which
	// gives a greater chance of visual table alignment. Some exceptions are made we anticipate
	// longer values (e.g. Progressing for status)
	columnFmtString = "%-10s\t%-9s\t%-12s\t%-4s\t%-10s\t%-5s\t%-7d\t%-10d\t%-9d"
)

// rolloutInfo contains the columns which are printed as part of a list command
type rolloutInfo struct {
	namespace    string
	name         string
	strategy     string
	status       string
	step         string
	setWeight    string
	readyCurrent string
	desired      int32
	upToDate     int32
	available    int32
}

// rolloutInfoKey is used as a map key to get a rolloutInfo by namespace/name
type rolloutInfoKey struct {
	ns string
	n  string
}

func newRolloutInfo(ro v1alpha1.Rollout) rolloutInfo {
	ri := rolloutInfo{}
	ri.name = ro.Name
	ri.namespace = ro.Namespace
	ri.strategy = "unknown"
	ri.step = "-"
	ri.setWeight = "-"

	if ro.Spec.Strategy.Canary != nil {
		ri.strategy = "Canary"
		if ro.Status.CurrentStepIndex != nil && len(ro.Spec.Strategy.Canary.Steps) > 0 {
			ri.step = fmt.Sprintf("%d/%d", *ro.Status.CurrentStepIndex, len(ro.Spec.Strategy.Canary.Steps))
		}
		// NOTE that this is desired weight, not the actual current weight
		ri.setWeight = strconv.Itoa(int(replicasetutil.GetCurrentSetWeight(&ro)))

		// TODO(jessesuen) in the future, we want to calculate the actual weight
		// if ro.Phase.AvailableReplicas == 0 {
		// 	ri.weight = "0"
		// } else {
		// 	ri.weight = fmt.Sprintf("%d", (ro.Phase.UpdatedReplicas*100)/ro.Phase.AvailableReplicas)
		// }
	} else if ro.Spec.Strategy.BlueGreen != nil {
		ri.strategy = "BlueGreen"
	}
	ri.status = rolloutStatus(&ro)

	ri.desired = 1
	if ro.Spec.Replicas != nil {
		ri.desired = *ro.Spec.Replicas
	}
	ri.readyCurrent = fmt.Sprintf("%d/%d", ro.Status.ReadyReplicas, ro.Status.Replicas)
	ri.upToDate = ro.Status.UpdatedReplicas
	ri.available = ro.Status.AvailableReplicas
	return ri
}

func (ri *rolloutInfo) key() rolloutInfoKey {
	return rolloutInfoKey{
		ns: ri.namespace,
		n:  ri.name,
	}
}

func (ri *rolloutInfo) String(timestamp, namespace bool) string {
	fmtString := columnFmtString
	args := []interface{}{ri.name, ri.strategy, ri.status, ri.step, ri.setWeight, ri.readyCurrent, ri.desired, ri.upToDate, ri.available}
	if namespace {
		fmtString = "%-9s\t" + fmtString
		args = append([]interface{}{ri.namespace}, args...)
	}
	if timestamp {
		fmtString = "%-20s\t" + fmtString
		timestampStr := time.Now().UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z")
		args = append([]interface{}{timestampStr}, args...)
	}
	return fmt.Sprintf(fmtString, args...)
}

// rolloutStatus returns a status string to print in the STATUS column
func rolloutStatus(ro *v1alpha1.Rollout) string {
	for _, condition := range ro.Status.Conditions {
		if condition.Type == v1alpha1.InvalidSpec {
			return string(condition.Type)
		}
		if condition.Type == v1alpha1.RolloutProgressing && condition.Reason == "ProgressDeadlineExceeded" {
			return "Degraded"
		}
	}
	if ro.Spec.Paused {
		return "Paused"
	}
	if ro.Status.UpdatedReplicas < ro.Status.Replicas {
		// more replicas need to be updated
		return "Progressing"
	}
	if ro.Status.Replicas > ro.Status.UpdatedReplicas {
		// old replicas are pending termination
		return "Progressing"
	}
	if ro.Status.AvailableReplicas < ro.Status.UpdatedReplicas {
		// updated replicas are still becoming available
		return "Progressing"
	}
	if ro.Spec.Strategy.BlueGreen != nil {
		if ro.Status.BlueGreen.ActiveSelector != "" && ro.Status.BlueGreen.ActiveSelector == ro.Status.CurrentPodHash {
			return "Healthy"
		}
		// service cutover pending
		return "Progressing"
	} else if ro.Spec.Strategy.Canary != nil {
		if ro.Status.Canary.StableRS != "" && ro.Status.Canary.StableRS == ro.Status.CurrentPodHash {
			return "Healthy"
		}
		// Waiting for rollout to finish steps
		return "Progressing"
	}
	return "Unknown"
}
