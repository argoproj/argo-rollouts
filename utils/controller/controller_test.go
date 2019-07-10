package controller

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"

	"github.com/argoproj/argo-rollouts/controller/metrics"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	"github.com/argoproj/argo-rollouts/utils/log"
)

func TestProcessNextWorkItemShutDownQueue(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Rollouts")
	syncHandler := func(key string) error {
		return nil
	}
	q.ShutDown()
	assert.False(t, processNextWorkItem(q, log.RolloutKey, syncHandler, nil))
}

func TestProcessNextWorkItemNoTStringKey(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Rollouts")
	q.Add(1)
	syncHandler := func(key string) error {
		return nil
	}
	assert.True(t, processNextWorkItem(q, log.RolloutKey, syncHandler, nil))
}

func TestProcessNextWorkItemNoValidKey(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Rollouts")
	q.Add("invalid.key")
	syncHandler := func(key string) error {
		return nil
	}
	assert.True(t, processNextWorkItem(q, log.RolloutKey, syncHandler, nil))
}

func TestProcessNextWorkItemNormalSync(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Rollouts")
	q.Add("valid/key")
	syncHandler := func(key string) error {
		return nil
	}
	assert.True(t, processNextWorkItem(q, log.RolloutKey, syncHandler, nil))
}

func TestProcessNextWorkItemSyncHandlerReturnError(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Rollouts")
	q.Add("valid/key")
	metricServer := metrics.NewMetricsServer("localhost:8080", nil)
	syncHandler := func(key string) error {
		return fmt.Errorf("error message")
	}
	assert.True(t, processNextWorkItem(q, log.RolloutKey, syncHandler, metricServer))
}

func TestEnqueue(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Rollouts")
	r := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName",
			Namespace: "testNamespace",
		},
	}
	Enqueue(r, q)
	assert.Equal(t, 1, q.Len())
}

func TestEnqueueAfter(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Rollouts")
	r := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName",
			Namespace: "testNamespace",
		},
	}
	EnqueueAfter(r, time.Duration(1), q)
	assert.Equal(t, 0, q.Len())
	time.Sleep(2 * time.Second)
	assert.Equal(t, 1, q.Len())
}

func TestEnqueueRateLimited(t *testing.T) {
	q := workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Rollouts")
	r := &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName",
			Namespace: "testNamespace",
		},
	}
	EnqueueRateLimited(r, q)
	assert.Equal(t, 0, q.Len())
	time.Sleep(time.Second)
	assert.Equal(t, 1, q.Len())

}