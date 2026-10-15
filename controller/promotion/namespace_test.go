package promotion

import "testing"

func TestPromotionTargetNamespaceBoundary(t *testing.T) {
	for _, test := range []struct {
		name      string
		targets   []Target
		wantError bool
	}{
		{"implicit namespace", []Target{{Name: "api"}}, false},
		{"explicit same namespace", []Target{{Name: "api", Namespace: "example"}}, false},
		{"cross namespace", []Target{{Name: "api", Namespace: "other"}}, true},
		{"duplicate target", []Target{{Name: "api"}, {Name: "api", Namespace: "example"}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateTargetNamespaces("example", test.targets); (err != nil) != test.wantError {
				t.Fatalf("validateTargetNamespaces() = %v, want error %v", err, test.wantError)
			}
		})
	}
}
