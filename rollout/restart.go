package rollout

import (
	"sort"
	"time"

	"github.com/argoproj/argo-rollouts/utils/defaults"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/utils/replicaset"
)

const (
	// restartPodCheckTime prevents the Rollout from not making any progress with restarting Pods. When pods can be restarted
	// faster than the old pods can be scaled down, the parent's ReplicaSet's availableReplicas does not change. A rollout
	// relies on changes to the availableReplicas of the ReplicaSet to detect when the controller should requeue and continue
	// deleting pods. In this situation, the rollout does not requeue and won't make any more progress restarting pods until
	// the resync period passes or another change is made to the Rollout. The controller requeue Rollouts with deleted
	// Polls every 30 seconds to make sure the rollout is not stuck.
	restartPodCheckTime = 30 * time.Second
)

// RolloutPodRestarter describes the components needed for the controller to restart all the pods of
// a rollout.
type RolloutPodRestarter struct {
	client       kubernetes.Interface
	resyncPeriod time.Duration
	enqueueAfter func(obj interface{}, duration time.Duration)
}

// checkEnqueueRollout enqueues a Rollout if the Rollout's restartedAt is within the next resync
func (p RolloutPodRestarter) checkEnqueueRollout(roCtx *rolloutContext) {
	logCtx := roCtx.log.WithField("Reconciler", "PodRestarter")
	now := nowFn().UTC()
	if roCtx.rollout.Spec.RestartAt == nil || now.After(roCtx.rollout.Spec.RestartAt.Time) {
		return
	}
	nextResync := now.Add(p.resyncPeriod)
	// Only enqueue if the Restart time is before the next sync period
	if nextResync.After(roCtx.rollout.Spec.RestartAt.Time) {
		timeRemaining := roCtx.rollout.Spec.RestartAt.Sub(now)
		logCtx.Infof("Enqueueing Rollout in %s seconds for restart", timeRemaining.String())
		p.enqueueAfter(roCtx.rollout, timeRemaining)
	}
}

func (p *RolloutPodRestarter) Reconcile(roCtx *rolloutContext) error {
	logCtx := roCtx.log.WithField("Reconciler", "PodRestarter")
	p.checkEnqueueRollout(roCtx)
	if !replicaset.NeedsRestart(roCtx.rollout) {
		return nil
	}
	logCtx.Info("Reconcile pod restarts")
	s := NewSortReplicaSetsByPriority(roCtx)
	for _, rs := range s.allRSs {
		rsReplicas := defaults.GetReplicasOrDefault(rs.Spec.Replicas)
		if rs.Status.AvailableReplicas != rsReplicas {
			logCtx.WithField("ReplicaSet", rs.Name).Info("cannot restart pods as not all ReplicasSets are fully available")
			return nil
		}
	}
	sort.Sort(s)
	for _, rs := range s.allRSs {
		finishedRestartingPods, err := p.restartReplicaSetPod(roCtx, rs)
		if err != nil {
			return err
		}
		if !finishedRestartingPods {
			return nil
		}
	}
	logCtx.Info("all pods have been restarted and setting restartedAt status")
	roCtx.SetRestartedAt()
	return nil
}

func (p RolloutPodRestarter) getPodsOwnedByReplicaSet(rs *appsv1.ReplicaSet) ([]*corev1.Pod, error) {
	pods, err := p.client.CoreV1().Pods(rs.Namespace).List(metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(rs.Spec.Selector),
	})
	if err != nil {
		return nil, err
	}
	var podOwnedByRS []*corev1.Pod
	for i := range pods.Items {
		pod := pods.Items[i]
		if metav1.IsControlledBy(&pod, rs) {
			podOwnedByRS = append(podOwnedByRS, &pod)
		}
	}
	return podOwnedByRS, nil
}

// restartReplicaSetPod gets all the pods for a ReplicaSet and confirms that they are all newer than the restartAt time.
// If all the pods do not have a deletion timestamp and are newer than the restartAt time, the method returns true
// indicating that the ReplicaSet's pods needs no more restarts. If any of the pods have a deletion timestamp, the
// restarter cannot restart any more pods since it needs to wait for the current pod finish its deletion. In this case,
// the restarter enqueues itself to check if the pod has been deleted and returns false. If the restarter deletes
// a pod, it returns false as the restarter needs to make sure the pod is deleted before marking the ReplicaSet done.
func (p RolloutPodRestarter) restartReplicaSetPod(roCtx *rolloutContext, rs *appsv1.ReplicaSet) (bool, error) {
	logCtx := roCtx.log.WithField("Reconciler", "PodRestarter")
	restartedAt := roCtx.rollout.Spec.RestartAt
	pods, err := p.getPodsOwnedByReplicaSet(rs)
	if err != nil {
		return false, err
	}

	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			logCtx.Info("cannot reconcile any more pods as pod with deletionTimestamp exists")
			p.enqueueAfter(roCtx.rollout, restartPodCheckTime)
			return false, nil
		}
	}

	for _, pod := range pods {
		if restartedAt.After(pod.CreationTimestamp.Time) && pod.DeletionTimestamp == nil {
			newLogCtx := logCtx.WithField("Pod", pod.Name).WithField("CreatedAt", pod.CreationTimestamp.Format(time.RFC3339)).WithField("RestartAt", restartedAt.Format(time.RFC3339))
			newLogCtx.Info("restarting Pod that's older than restartAt Time")
			err := p.client.CoreV1().Pods(pod.Namespace).Delete(pod.Name, &metav1.DeleteOptions{})
			return false, err
		}
	}
	return true, nil
}

func NewSortReplicaSetsByPriority(roCtx *rolloutContext) SortReplicaSetsByPriority {
	newRSName := ""
	if roCtx.newRS != nil {
		newRSName = roCtx.newRS.Name
	}
	stableRS := roCtx.stableRS
	stableRSName := ""
	if stableRS != nil {
		stableRSName = stableRS.Name
	}
	return SortReplicaSetsByPriority{
		allRSs:   roCtx.allRSs,
		newRS:    newRSName,
		stableRS: stableRSName,
	}
}

// SortReplicaSetsByPriority sorts the ReplicaSets with the following Priority:
// 1. Stable RS
// 2. New RS
// 3. Older ReplicaSets
type SortReplicaSetsByPriority struct {
	allRSs   []*appsv1.ReplicaSet
	newRS    string
	stableRS string
}

func (s SortReplicaSetsByPriority) Len() int {
	return len(s.allRSs)
}

func (s SortReplicaSetsByPriority) Swap(i, j int) {
	rs := s.allRSs[i]
	s.allRSs[i] = s.allRSs[j]
	s.allRSs[j] = rs
}

func (s SortReplicaSetsByPriority) Less(i, j int) bool {
	iRS := s.allRSs[i]
	jRS := s.allRSs[j]
	if iRS.Name == s.stableRS {
		return true
	}
	if jRS.Name == s.stableRS {
		return false
	}
	if iRS.Name == s.newRS {
		return true
	}
	if jRS.Name == s.newRS {
		return false
	}

	return iRS.CreationTimestamp.Before(&jRS.CreationTimestamp)
}
