package rbac

// Resource constants — the Argo Rollouts API objects RBAC can grant over.
const (
	ResourceRollouts                 = "rollouts"
	ResourceAnalysisRuns             = "analysisruns"
	ResourceAnalysisTemplates        = "analysistemplates"
	ResourceClusterAnalysisTemplates = "clusteranalysistemplates"
	ResourceExperiments              = "experiments"
)

// Action constants — standard CRUD plus rollout lifecycle verbs.
const (
	ActionGet      = "get"
	ActionCreate   = "create"
	ActionUpdate   = "update"
	ActionDelete   = "delete"
	ActionPromote  = "promote"
	ActionAbort    = "abort"
	ActionRetry    = "retry"
	ActionRestart  = "restart"
	ActionPause    = "pause"
	ActionSkip     = "skip"
	ActionSetImage = "setimage"
	ActionUndo     = "undo"
)

// ResourcesList enumerates all valid resources.
var ResourcesList = []string{
	ResourceRollouts,
	ResourceAnalysisRuns,
	ResourceAnalysisTemplates,
	ResourceClusterAnalysisTemplates,
	ResourceExperiments,
}

// ActionsList enumerates all valid actions.
var ActionsList = []string{
	ActionGet, ActionCreate, ActionUpdate, ActionDelete,
	ActionPromote, ActionAbort, ActionRetry, ActionRestart,
	ActionPause, ActionSkip, ActionSetImage, ActionUndo,
}

// ModelConf is the Casbin model. Mirrors argo-cd semantics: role grouping (g),
// glob matching on resource/action/object, allow-override effect.
const ModelConf = `
[request_definition]
r = sub, res, act, obj

[policy_definition]
p = sub, res, act, obj, eff

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eff == allow)) && !some(where (p.eff == deny))

[matchers]
m = g(r.sub, p.sub) && globMatch(r.res, p.res) && globMatch(r.act, p.act) && globMatch(r.obj, p.obj)
`

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// IsValidResource reports whether r is a known RBAC resource.
func IsValidResource(r string) bool { return contains(ResourcesList, r) }

// IsValidAction reports whether a is a known RBAC action.
func IsValidAction(a string) bool { return contains(ActionsList, a) }
