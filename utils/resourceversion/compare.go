package resourceversion

import (
	"fmt"
	"strings"
)

// InvalidResourceVersion is returned when a resource version is not a well-formed positive integer.
type InvalidResourceVersion struct {
	RV string
}

func (i InvalidResourceVersion) Error() string {
	return fmt.Sprintf("resource version is not well formed: %s", i.RV)
}

// CompareResourceVersion compares two ResourceVersions for objects of the same resource.
// Returns -1 if a < b, 0 if a == b, +1 if a > b.
//
// Compares as integers, not lexically (e.g. "9" < "10"). Mirrors
// k8s.io/apimachinery/pkg/util/resourceversion once we upgrade past v0.34.
// TODO: Remove this once we upgrade past v0.34.
func CompareResourceVersion(a, b string) (int, error) {
	if !isWellFormed(a) {
		return 0, InvalidResourceVersion{RV: a}
	}
	if !isWellFormed(b) {
		return 0, InvalidResourceVersion{RV: b}
	}
	aLen := len(a)
	bLen := len(b)
	switch {
	case aLen < bLen:
		return -1, nil
	case aLen > bLen:
		return 1, nil
	default:
		return strings.Compare(a, b), nil
	}
}

func isWellFormed(s string) bool {
	if len(s) == 0 {
		return false
	}
	if s[0] == '0' {
		return false
	}
	for i := range s {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
