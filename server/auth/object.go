package auth

// objectFromRequest derives the RBAC object ("namespace/name") for an RPC
// request by duck-typing its getter methods, so this package does not depend on
// the rollout protobuf types. Name resolves from GetName, then GetRollout, then
// "*" (namespace-wide, e.g. list/watch). Namespace resolves from GetNamespace.
func objectFromRequest(req interface{}) string {
	namespace := ""
	if r, ok := req.(interface{ GetNamespace() string }); ok {
		namespace = r.GetNamespace()
	}

	name := ""
	if r, ok := req.(interface{ GetName() string }); ok {
		name = r.GetName()
	}
	if name == "" {
		if r, ok := req.(interface{ GetRollout() string }); ok {
			name = r.GetRollout()
		}
	}
	if name == "" {
		name = "*"
	}

	return namespace + "/" + name
}
