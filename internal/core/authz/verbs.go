package authz

// Verb is a control-plane permission.
//
// Data-plane use is not a verb. Reaching an app through the proxy is binary
// (R-070) and is checked by CheckData, which is a different function taking
// different arguments for exactly that reason.
type Verb string

const (
	AppView             Verb = "app.view"
	AppLogsRead         Verb = "app.logs.read"
	AppDeploy           Verb = "app.deploy"
	AppRestart          Verb = "app.restart"
	AppSpecEdit         Verb = "app.spec.edit"
	AppSecretsWrite     Verb = "app.secrets.write"
	AppSecretsRead      Verb = "app.secrets.read"
	AppExec             Verb = "app.exec"
	AppGrantsManage     Verb = "app.grants.manage"
	AppRoutingOverride  Verb = "app.routing.override"
	AppResourceOverride Verb = "app.resources.override"
	AppEgressOverride   Verb = "app.egress.override"
	AppDelete           Verb = "app.delete"
)

// Verbs is the catalog (design 06 §5, R-080).
//
// There is no implication graph: holding app.delete does not imply app.view.
// Implication graphs are where authorization bugs live, because the graph is
// consulted in one place and forgotten in another. The console suggests
// sensible combinations instead, which is a UI concern and cannot go wrong
// silently.
var Verbs = []Verb{
	AppView,
	AppLogsRead,
	AppDeploy,
	AppRestart,
	AppSpecEdit,
	AppSecretsWrite,
	AppSecretsRead,
	AppExec,
	AppGrantsManage,
	AppRoutingOverride,
	AppResourceOverride,
	AppEgressOverride,
	AppDelete,
}

var verbSet = func() map[Verb]struct{} {
	m := make(map[Verb]struct{}, len(Verbs))
	for _, v := range Verbs {
		m[v] = struct{}{}
	}
	return m
}()

// IsVerb reports whether v is in the catalog. Custom roles are arbitrary
// subsets of it (R-082), so this is what validates one.
func IsVerb(v Verb) bool {
	_, ok := verbSet[v]
	return ok
}

// Built-in role IDs, seeded by migration and protected by trigger (R-081).
const (
	RoleViewer   = "role_viewer"
	RoleOperator = "role_operator"
	RoleOwner    = "role_owner"
)

// Role is a named set of verbs.
type Role struct {
	ID      string
	Name    string
	Builtin bool
	Verbs   []Verb
}

// Has reports whether the role grants v.
func (r Role) Has(v Verb) bool {
	for _, got := range r.Verbs {
		if got == v {
			return true
		}
	}
	return false
}
