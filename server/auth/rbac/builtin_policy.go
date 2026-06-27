package rbac

// BuiltinRoles are the roles shipped by default.
var BuiltinRoles = []string{"role:admin", "role:readonly", "role:operator"}

// BuiltinPolicyCSV is the default Casbin policy. Format per line:
//   p, <sub>, <res>, <act>, <obj>, <eff>
//   g, <sub>, <role>
// readonly: get on everything.
// operator: readonly + lifecycle verbs (no create/delete/setimage/undo).
// admin: wildcard.
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
