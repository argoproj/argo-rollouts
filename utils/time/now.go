package time

import (
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	timeNowFunc = time.Now

	// Acquire this mutex when accessing now function
	nowLock sync.RWMutex
)

// Now is a wrapper around time.Now() and used to override behavior in tests.
// Now invokes time.Now(), or its replacement function
func Now() time.Time {
	nowLock.RLock()
	defer nowLock.RUnlock()

	return timeNowFunc()
}

// Replace the function used to return the current time (defaults to time.Now() )
func SetNowTimeFunc(f func() time.Time) {
	nowLock.Lock()
	defer nowLock.Unlock()

	timeNowFunc = f

}

// MetaNow is a wrapper around metav1.Now() and used to override behavior in tests.
var MetaNow = func() metav1.Time {
	return metav1.Time{Time: Now()}
}

// MetaTime is a wrapper around metav1.Time and used to override behavior in tests.
var MetaTime = func(time time.Time) metav1.Time {
	return metav1.Time{Time: time}
}
