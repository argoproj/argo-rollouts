package rollout

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/argoproj/argo-rollouts/utils/replicaset"
)

// RolloutPodRestarter describes the components needed for the controller to restart all the pods of
// a rollout.
type RolloutPodRestarter struct {
	client kubernetes.Interface
}

func (p *RolloutPodRestarter) Reconcile(roCtx rolloutContext) error {
	rollout := roCtx.Rollout()
	if !replicaset.NeedsRestart(rollout) {
		return nil
	}
	roCtx.Log().Info("Reconcile pod restarts")
	s := NewSortReplicaSetsByPriority(roCtx)
	for _, rs := range s.allRSs {
		if rs.Status.AvailableReplicas != *rs.Spec.Replicas {
			roCtx.Log().WithField("ReplicaSet", rs.Name).Info("cannot restart pods as not all ReplicasSets are fully available")
			return nil
		}
	}
	sort.Sort(s)
	for _, rs := range s.allRSs {
		deletedPod, err := p.reconcilePodsInReplicaSet(roCtx, rs)
		if err != nil {
			return err
		}
		if deletedPod {
			return nil
		}
	}
	roCtx.Log().Info("all pods have been restarted and setting restartedAt status")
	roCtx.SetRestartedAt()
	return nil
}

func (p RolloutPodRestarter) reconcilePodsInReplicaSet(roCtx rolloutContext, rs *appsv1.ReplicaSet) (bool, error) {
	restartedAt := roCtx.Rollout().Spec.RestartAt
	pods, err := p.client.CoreV1().Pods(rs.Namespace).List(metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(rs.Spec.Selector),
	})
	if err != nil {
		return false, err
	}
	for _, pod := range pods.Items {
		if restartedAt.After(pod.CreationTimestamp.Time) {
			roCtx.Log().WithField("Pod", pod.Name).Info("restarting Pod that's older than restartAt Time")
			err := p.client.CoreV1().Pods(pod.Namespace).Delete(pod.Name, &metav1.DeleteOptions{})
			return true, err
		}
	}
	return false, nil
}

func NewSortReplicaSetsByPriority(roCtx rolloutContext) SortReplicaSetsByPriority {
	newRS := roCtx.NewRS()
	newRSName := ""
	if newRS != nil {
		newRSName = newRS.Name
	}
	stableRS := roCtx.StableRS()
	stableRSName := ""
	if stableRS != nil {
		stableRSName = stableRS.Name
	}
	return SortReplicaSetsByPriority{
		allRSs:   roCtx.AllRSs(),
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
