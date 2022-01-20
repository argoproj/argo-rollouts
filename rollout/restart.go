package rollout

import (
	"context"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policy "k8s.io/api/policy/v1beta1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/defaults"
	"github.com/argoproj/argo-rollouts/utils/replicaset"
	replicasetutil "github.com/argoproj/argo-rollouts/utils/replicaset"
	timeutil "github.com/argoproj/argo-rollouts/utils/time"
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

// Reconcile gets all pods of a Rollout and confirms that have creationTimestamps newer than
// spec.restartAt. If not, iterates pods and deletes pods which do not have a deletion timestamp,
// and were created before spec.restartedAt. If the rollout is a canary rollout, it can restart
// multiple pods, up to maxUnavailable or 1, whichever is greater.
func (p *RolloutPodRestarter) Reconcile(roCtx *rolloutContext) error {
	ctx := context.TODO()
	logCtx := roCtx.log.WithField("Reconciler", "PodRestarter")
	p.checkEnqueueRollout(roCtx)
	if !replicaset.NeedsRestart(roCtx.rollout) {
		return nil
	}
	s := NewSortReplicaSetsByPriority(roCtx)
	sort.Sort(s)
	rolloutPods, err := p.getRolloutPods(ctx, roCtx.rollout, s.allRSs)
	if err != nil {
		return err
	}
	// total replicas can be higher than spec.replicas (e.g. when we are a canary weight that is not
	// evenly divisible by the spec.replicas)
	totalReplicas := replicasetutil.GetReplicaCountForReplicaSets(s.allRSs)
	replicas := defaults.GetReplicasOrDefault(roCtx.rollout.Spec.Replicas)
	available := getAvailablePodCount(rolloutPods, roCtx.rollout.Spec.MinReadySeconds)
	maxUnavailable := replicasetutil.MaxUnavailable(roCtx.rollout)
	// maxUnavailable might be 0. we ignore this because need to be able to restart at least 1
	concurrentRestart := maxInt(maxUnavailable, int32(1))

	// we take the higher of totalReplicas vs. replicas when calculating effectiveMinAvailable
	// to handle the case where we are at a non-divisible canary weight
	effMinAvailable := maxInt(replicas, totalReplicas) - concurrentRestart

	canRestart := available - effMinAvailable
	logCtx.Infof("Reconcile pod restart (replicas:%d, totalReplicas:%d, available:%d, maxUnavailable:%d, effectiveMinAvailable:%d, concurrentRestart:%d, canRestart:%d)",
		replicas, totalReplicas, available, maxUnavailable, effMinAvailable, concurrentRestart, canRestart)

	restartedAt := roCtx.rollout.Spec.RestartAt
	needsRestart := 0
	restarted := 0
	for _, pod := range rolloutPods {
		if pod.CreationTimestamp.After(restartedAt.Time) || pod.CreationTimestamp.Equal(restartedAt) {
			continue
		}
		needsRestart += 1
		if canRestart <= 0 {
			continue
		}
		if pod.DeletionTimestamp != nil {
			continue
		}
		newLogCtx := logCtx.WithField("Pod", pod.Name).WithField("CreatedAt", pod.CreationTimestamp.Format(time.RFC3339)).WithField("RestartAt", restartedAt.Format(time.RFC3339))
		newLogCtx.Info("restarting Pod that's older than restartAt Time")
		evictTarget := policy.Eviction{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pod.Name,
				Namespace: pod.Namespace,
			},
		}
		err := p.client.CoreV1().Pods(pod.Namespace).Evict(ctx, &evictTarget)
		if err != nil {
			if k8serrors.IsTooManyRequests(err) {
				// A PodDisruptionBudget prevented us from evicting the pod.
				// Continue and allow rollout requeue to try again later.
				newLogCtx.Warn(err)
				continue
			}
			return err
		}
		canRestart -= 1
		restarted += 1
	}
	remaining := needsRestart - restarted

	if remaining != 0 {
		logCtx.Infof("%d/%d pods require restart. restarted %d. retrying in %v", needsRestart, len(rolloutPods), restarted, restartPodCheckTime)
		p.enqueueAfter(roCtx.rollout, restartPodCheckTime)
	} else {
		logCtx.Infof("all %d pods are current. setting restartedAt", len(rolloutPods))
		roCtx.SetRestartedAt()
	}
	return nil
}

func maxInt(left, right int32) int32 {
	if left > right {
		return left
	}
	return right
}

func minInt(left, right int32) int32 {
	if left < right {
		return left
	}
	return right
}

// getRolloutPods returns all pods associated with a rollout
func (p *RolloutPodRestarter) getRolloutPods(ctx context.Context, ro *v1alpha1.Rollout, allRSs []*appsv1.ReplicaSet) ([]*corev1.Pod, error) {
	pods, err := p.client.CoreV1().Pods(ro.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(ro.Spec.Selector),
	})
	if err != nil {
		return nil, err
	}
	rolloutReplicaSetUIDS := make(map[types.UID]bool)
	for _, rs := range allRSs {
		rolloutReplicaSetUIDS[rs.UID] = true
	}
	var rolloutPods []*corev1.Pod
	for i, pod := range pods.Items {
		for _, ownerRef := range pod.OwnerReferences {
			if _, ok := rolloutReplicaSetUIDS[ownerRef.UID]; ok {
				rolloutPods = append(rolloutPods, &pods.Items[i])
			}
		}
	}
	return rolloutPods, nil
}

func getAvailablePodCount(pods []*corev1.Pod, minReadySeconds int32) int32 {
	var available int32
	now := timeutil.MetaNow()
	for _, pod := range pods {
		if podutil.IsPodAvailable(pod, minReadySeconds, now) && pod.DeletionTimestamp == nil {
			available += 1
		}
	}
	return available
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
