package time

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Now is a wrapper around time.Now() and used to override behavior in tests.
var Now = time.Now

// MetaNow is a wrapper around metav1.Now() and used to override behavior in tests.
var MetaNow = func() metav1.Time {
	return metav1.Time{Time: Now()}
}

// MetaTime is a wrapper around metav1.Time and used to override behavior in tests.
var MetaTime = func(time time.Time) metav1.Time {
	return metav1.Time{Time: time}
}
