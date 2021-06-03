package queue

import (
	"time"

	"k8s.io/client-go/util/workqueue"
)

// DefaultArgoRolloutsRateLimiter is the default queue rate limiter.
// Similar to workqueue.DefaultControllerRateLimiter() but the max limit is 10 seconds instead of 16 minutes
func DefaultArgoRolloutsRateLimiter() workqueue.RateLimiter {
	return workqueue.NewItemExponentialFailureRateLimiter(time.Millisecond, 10*time.Second)
}
