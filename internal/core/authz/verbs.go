package authz

// Verb is a control-plane permission.
//
// Data-plane use is not a verb. Reaching an app through the proxy is binary
// (R-070) and is checked by CheckData, which is a different function taking
// different arguments for exactly that reason.
type Verb string

const (
	// Install-scoped verbs. Held through a grant with no app (O-17), which is
	// what makes "an administrator" a principal with a grant rather than a
	// separate mechanism beside grants and roles.
	//
	// Namespaced `install.*` so the scope is visible at every call site: a verb
	// that reads `install.` cannot be mistaken for one that needs an app, which
	// is the mistake that would reintroduce the hole these close.
	InstallView           Verb = "install.view"
	InstallUsersManage    Verb = "install.users.manage"
	InstallPolicyManage   Verb = "install.policy.manage"
	InstallAdaptersManage Verb = "install.adapters.manage"
	InstallAuditRead      Verb = "install.audit.read"

	// InstallBackupManage covers taking, verifying and restoring backups.
	// Its own verb rather than part of install.policy.manage: a restore
	// replaces the entire install, and folding that into the verb that edits a
	// source allowlist would hand it to everyone who could edit one.
	InstallBackupManage Verb = "install.backup.manage"

	// AppCreate is install-scoped despite its name: there is no app yet when it
	// is checked. Sequence A step 1 has always called it install-level.
	AppCreate Verb = "app.create"

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
	InstallView,
	InstallUsersManage,
	InstallPolicyManage,
	InstallAdaptersManage,
	InstallAuditRead,
	InstallBackupManage,
	AppCreate,

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

	// RoleAdministrator is install-scoped and holds no app verbs. An
	// administrator is not an owner of every app: they can create apps, manage
	// users, policy and adapters, and read the audit log, and to manage a
	// particular app they need a grant on it like anyone else. That is R-031 —
	// every app has an owner of record — surviving the arrival of an admin role
	// rather than being quietly overridden by it.
	RoleAdministrator = "role_administrator"

	// RoleCreator is install-scoped and holds one verb, app.create. A creator
	// manages the apps they make because making one writes them its owner
	// (R-073) — not because this role says anything about apps — so they
	// manage those and nothing else in the installation.
	RoleCreator = "role_creator"
)

// Role is a named set of verbs.
//
// Tagged because this type is serialized directly by the API, and every other
// type on the wire is lower_snake_case. An untagged struct would put Go field
// names into the API surface — where they would then be a compatibility
// promise, and renaming a field would be a breaking change to a client.
type Role struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Builtin bool   `json:"builtin"`
	Verbs   []Verb `json:"verbs"`
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

// InstallScoped reports whether a verb is held install-wide rather than on one
// app.
//
// Checked at the boundary between CheckControl and CheckInstall so that a verb
// cannot be authorized in the wrong scope. Passing an install verb to
// CheckControl, or an app verb to CheckInstall, is a programming error and is
// refused rather than evaluated — an install verb evaluated against an app
// would look for a grant that cannot exist and deny, which is safe; an app verb
// evaluated install-wide would look for a grant that *can* exist and allow,
// which is not.
func InstallScoped(v Verb) bool {
	switch v {
	case InstallView, InstallUsersManage, InstallPolicyManage,
		InstallAdaptersManage, InstallAuditRead, InstallBackupManage, AppCreate:
		return true
	default:
		return false
	}
}
