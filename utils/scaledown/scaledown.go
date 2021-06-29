package scaledown

import (
	"time"

	log "github.com/sirupsen/logrus"
	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
)

func ScaleDownOldReplicaSetHelper(rs *v1.ReplicaSet, annotationedRSs int32, scaleDownRevisionLimit int32) (int32, *time.Duration) {
	if scaleDownAtStr, ok := rs.Annotations[v1alpha1.DefaultReplicaSetScaleDownDeadlineAnnotationKey]; ok {
		annotationedRSs++
		scaleDownAtTime, err := time.Parse(time.RFC3339, scaleDownAtStr)
		if err != nil {
			log.Warnf("Unable to read scaleDownAt label on rs '%s'", rs.Name)
		} else if annotationedRSs > scaleDownRevisionLimit {
			log.Infof("At ScaleDownDelayRevisionLimit (%d) and scaling down the rest", scaleDownRevisionLimit)
		} else {
			now := metav1.Now()
			scaleDownAt := metav1.NewTime(scaleDownAtTime)
			if scaleDownAt.After(now.Time) {
				log.Infof("RS '%s' has not reached the scaleDownTime", rs.Name)
				remainingTime := scaleDownAt.Sub(now.Time)
				return annotationedRSs, &remainingTime
			}
		}
	}
	return annotationedRSs, nil
}
