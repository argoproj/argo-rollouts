package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	kubetesting "k8s.io/client-go/testing"

	rolloutapi "github.com/argoproj/argo-rollouts/pkg/apiclient/rollout"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	fakeroclient "github.com/argoproj/argo-rollouts/pkg/client/clientset/versioned/fake"
)

const testNamespace = "default"

// newTestNamespace returns Rollouts which each own one ReplicaSet with two Pods.
func newTestNamespace(rollouts int) ([]*v1alpha1.Rollout, []*appsv1.ReplicaSet, []*corev1.Pod) {
	var ros []*v1alpha1.Rollout
	var rss []*appsv1.ReplicaSet
	var pods []*corev1.Pod
	for r := 0; r < rollouts; r++ {
		ro := &v1alpha1.Rollout{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("rollout-%d", r),
				Namespace: testNamespace,
				UID:       types.UID(fmt.Sprintf("ro-%d", r)),
			},
			Spec: v1alpha1.RolloutSpec{
				Strategy: v1alpha1.RolloutStrategy{Canary: &v1alpha1.CanaryStrategy{}},
			},
		}
		rs := &appsv1.ReplicaSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      ro.Name + "-rs",
				Namespace: testNamespace,
				UID:       types.UID(fmt.Sprintf("rs-%d", r)),
				OwnerReferences: []metav1.OwnerReference{
					{APIVersion: v1alpha1.SchemeGroupVersion.String(), Kind: "Rollout", Name: ro.Name, UID: ro.UID},
				},
			},
		}
		ros = append(ros, ro)
		rss = append(rss, rs)
		for p := 0; p < 2; p++ {
			pods = append(pods, &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-%d", rs.Name, p),
					Namespace: testNamespace,
					UID:       types.UID(fmt.Sprintf("pod-%d-%d", r, p)),
					OwnerReferences: []metav1.OwnerReference{
						{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID},
					},
				},
			})
		}
	}
	return ros, rss, pods
}

func testNamespaceObjects(ros []*v1alpha1.Rollout, rss []*appsv1.ReplicaSet, pods []*corev1.Pod) []runtime.Object {
	var objs []runtime.Object
	for _, o := range ros {
		objs = append(objs, o)
	}
	for _, o := range rss {
		objs = append(objs, o)
	}
	for _, o := range pods {
		objs = append(objs, o)
	}
	return objs
}

type fakeRolloutInfosStream struct {
	grpc.ServerStream
	ctx context.Context

	mu     sync.Mutex
	events []*rolloutapi.RolloutWatchEvent
}

func (f *fakeRolloutInfosStream) Context() context.Context {
	return f.ctx
}

func (f *fakeRolloutInfosStream) Send(e *rolloutapi.RolloutWatchEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return nil
}

func (f *fakeRolloutInfosStream) sent() []*rolloutapi.RolloutWatchEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*rolloutapi.RolloutWatchEvent{}, f.events...)
}

func TestIndexByOwnerUID(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{{UID: "a"}, {UID: "b"}},
		},
	}

	uids, err := indexByOwnerUID(pod)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, uids)

	uids, err = indexByOwnerUID(&corev1.Pod{})
	require.NoError(t, err)
	assert.Empty(t, uids)

	uids, err = indexByOwnerUID("not an object")
	require.NoError(t, err)
	assert.Empty(t, uids)
}

func TestInformerCacheIsSharedAndEvictedWhenIdle(t *testing.T) {
	ros, rss, pods := newTestNamespace(2)
	s := newTestServer(t, testNamespaceObjects(ros, rss, pods)...)
	s.informers.idleTimeout = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	first, releaseFirst, err := s.informers.acquire(ctx, testNamespace)
	require.NoError(t, err)
	second, releaseSecond, err := s.informers.acquire(ctx, testNamespace)
	require.NoError(t, err)
	assert.Same(t, first, second)

	releaseFirst()
	releaseFirst()
	time.Sleep(4 * s.informers.idleTimeout)
	third, releaseThird, err := s.informers.acquire(ctx, testNamespace)
	require.NoError(t, err)
	assert.Same(t, first, third, "cache must be kept while it is still in use")

	releaseSecond()
	releaseThird()
	require.Eventually(t, func() bool {
		s.informers.mu.Lock()
		defer s.informers.mu.Unlock()
		return len(s.informers.namespaces) == 0
	}, 5*time.Second, 10*time.Millisecond)

	fourth, releaseFourth, err := s.informers.acquire(ctx, testNamespace)
	require.NoError(t, err)
	defer releaseFourth()
	assert.NotSame(t, first, fourth)
	assert.Len(t, fourth.rollouts(), len(ros))
}

func TestInformerCacheAcquireReturnsListError(t *testing.T) {
	kubeClient := k8sfake.NewSimpleClientset()
	kubeClient.PrependReactor("list", "pods", func(kubetesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("pods is forbidden")
	})
	s := NewServer(ServerOptions{
		KubeClientset:     kubeClient,
		RolloutsClientset: fakeroclient.NewSimpleClientset(),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := s.ListRolloutInfos(ctx, &rolloutapi.RolloutInfoListQuery{Namespace: testNamespace})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pods is forbidden")
}

func TestWatchRolloutInfos(t *testing.T) {
	ros, rss, pods := newTestNamespace(3)
	s := newTestServer(t, testNamespaceObjects(ros, rss, pods)...)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stream := &fakeRolloutInfosStream{ctx: ctx}
	done := make(chan error, 1)
	go func() {
		done <- s.WatchRolloutInfos(&rolloutapi.RolloutInfoListQuery{Namespace: testNamespace}, stream)
	}()

	eventually := func(count int) []*rolloutapi.RolloutWatchEvent {
		t.Helper()
		require.Eventually(t, func() bool {
			return len(stream.sent()) >= count
		}, 5*time.Second, 10*time.Millisecond)
		time.Sleep(2 * rolloutInfosCoalesceInterval)
		events := stream.sent()
		require.Len(t, events, count)
		return events
	}

	events := eventually(len(ros))
	for i, event := range events {
		assert.Equal(t, "Updated", event.Type)
		assert.Equal(t, ros[i].Name, event.RolloutInfo.ObjectMeta.Name)
		require.Len(t, event.RolloutInfo.ReplicaSets, 1)
		assert.Len(t, event.RolloutInfo.ReplicaSets[0].Pods, 2)
	}

	rolloutIf := s.Options.RolloutsClientset.ArgoprojV1alpha1().Rollouts(testNamespace)
	updated := ros[1].DeepCopy()
	for i := 1; i <= 5; i++ {
		updated.Labels = map[string]string{"update": fmt.Sprintf("%d", i)}
		_, err := rolloutIf.Update(ctx, updated, metav1.UpdateOptions{})
		require.NoError(t, err)
	}
	events = eventually(len(ros) + 1)
	last := events[len(events)-1]
	assert.Equal(t, ros[1].Name, last.RolloutInfo.ObjectMeta.Name)
	assert.Equal(t, "5", last.RolloutInfo.ObjectMeta.Labels["update"])

	err := s.Options.KubeClientset.CoreV1().Pods(testNamespace).Delete(ctx, pods[0].Name, metav1.DeleteOptions{})
	require.NoError(t, err)
	events = eventually(len(ros) + 2)
	last = events[len(events)-1]
	assert.Equal(t, ros[0].Name, last.RolloutInfo.ObjectMeta.Name)
	require.Len(t, last.RolloutInfo.ReplicaSets, 1)
	assert.Len(t, last.RolloutInfo.ReplicaSets[0].Pods, 1)

	second := &fakeRolloutInfosStream{ctx: ctx}
	go func() {
		_ = s.WatchRolloutInfos(&rolloutapi.RolloutInfoListQuery{Namespace: testNamespace}, second)
	}()
	require.Eventually(t, func() bool {
		return len(second.sent()) == len(ros)
	}, 5*time.Second, 10*time.Millisecond)
	s.informers.mu.Lock()
	assert.Len(t, s.informers.namespaces, 1)
	assert.Equal(t, 2, s.informers.namespaces[testNamespace].refs)
	s.informers.mu.Unlock()

	cancel()
	require.NoError(t, <-done)
}

func TestSendRolloutInfoUpdates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan *rolloutapi.RolloutInfo)
	var mu sync.Mutex
	var sent []string
	go sendRolloutInfoUpdates(ctx, updates, func(ri *rolloutapi.RolloutInfo) {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, ri.Message)
	})
	get := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string{}, sent...)
	}

	updates <- &rolloutapi.RolloutInfo{Message: "first"}
	require.Eventually(t, func() bool { return len(get()) == 1 }, time.Second, 5*time.Millisecond)

	time.Sleep(3 * rolloutInfoMinSendInterval)
	assert.Equal(t, []string{"first"}, get(), "an unchanged RolloutInfo must not be sent again")

	updates <- &rolloutapi.RolloutInfo{Message: "second"}
	updates <- &rolloutapi.RolloutInfo{Message: "third"}
	updates <- &rolloutapi.RolloutInfo{Message: "fourth"}
	require.Eventually(t, func() bool { return len(get()) == 3 }, time.Second, 5*time.Millisecond)
	time.Sleep(2 * rolloutInfoMinSendInterval)
	assert.Equal(t, []string{"first", "second", "fourth"}, get())
}
