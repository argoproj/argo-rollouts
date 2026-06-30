package rbac

// BuiltinRoles are the roles shipped by default.
var BuiltinRoles = []string{"role:admin", "role:readonly", "role:operator"}

// BuiltinPolicyCSV is the default Casbin policy. Format per line:
//
//	p, <sub>, <res>, <act>, <obj>, <eff>
//	g, <sub>, <role>
//
// readonly: get on everything.
// operator: readonly + lifecycle verbs (no create/delete/setimage/undo).
// admin: wildcard.
//
// NOTE: Object globs must be written as `*/*` or `<ns>/*`, never a bare `*`,
// because globMatch does not cross `/` — a bare `*` silently matches nothing
// on `ns/name`-style object strings.
//
// NOTE: User policy supplied via SetUserPolicy is appended AFTER this
// built-in policy, so admin-supplied configmap rules can extend built-in
// roles. This is intentional — the configmap is admin-controlled.
const BuiltinPolicyCSV = `
p, role:readonly, *, get, */*, allow

p, role:operator, *, get, */*, allow
p, role:operator, rollouts, promote, */*, allow
p, role:operator, rollouts, abort, */*, allow
p, role:operator, rollouts, retry, */*, allow
p, role:operator, rollouts, restart, */*, allow
p, role:operator, rollouts, pause, */*, allow
p, role:operator, rollouts, skip, */*, allow

p, role:admin, *, *, */*, allow
`
