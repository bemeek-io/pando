package httpapi

import (
	"net/http"
	"strings"

	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/reference"
)

// The API's own description (R-261).
//
// chi knows every route's method and pattern and nothing about what any of them
// is for, so the summaries live here, beside the router, and
// `TestEveryRouteIsDocumented` walks the real router and fails when this table
// and that router disagree in either direction. Adding an endpoint without
// describing it fails the build; so does describing one that no longer exists.
// That is the whole mechanism, and it is the reason this file is worth
// maintaining: the reference cannot rot quietly, because rot is a red test.
//
// The verb on each row is the authorization verb the handler checks (design 06
// §5). Empty means the endpoint asks for nothing beyond being signed in — which
// is an answer somebody needs, not an omission.
var routeDocs = []reference.Route{
	// --- session and account ---------------------------------------------
	{Method: "POST", Path: "/api/v1/sessions", Group: "Session", Summary: "Sign in with a username and password. Sets the session cookie."},
	{Method: "DELETE", Path: "/api/v1/sessions", Group: "Session", Summary: "Sign out, ending this session."},
	{Method: "GET", Path: "/api/v1/me", Group: "Session", Summary: "Who the caller is, and the install-level verbs they hold."},
	{Method: "POST", Path: "/api/v1/me/password", Group: "Session", Summary: "Change your own password. Yours only, whatever verbs you hold."},
	{Method: "GET", Path: "/api/v1/me/apps", Group: "Session", Summary: "The apps you can open, which is a different list from the apps you can administer (R-070, R-071). `favorite` marks the ones you have pinned, `section_id` the section you filed each under, and `sections` lists your sections."},
	{Method: "PUT", Path: "/api/v1/me/favorites/{appID}", Group: "Session", Summary: "Mark an app you can open as a favorite, pinning it to the top of your launcher. Yours only; it grants nothing (R-341)."},
	{Method: "DELETE", Path: "/api/v1/me/favorites/{appID}", Group: "Session", Summary: "Unpin an app from your favorites."},
	{Method: "POST", Path: "/api/v1/me/sections", Group: "Session", Summary: "Make a section in your launcher: a named, collapsible grouping of apps. Yours only; it grants nothing (R-342)."},
	{Method: "PATCH", Path: "/api/v1/me/sections/{sectionID}", Group: "Session", Summary: "Rename one of your sections."},
	{Method: "DELETE", Path: "/api/v1/me/sections/{sectionID}", Group: "Session", Summary: "Delete one of your sections. Its apps go back to Your apps."},
	{Method: "PUT", Path: "/api/v1/me/sections/{sectionID}/apps/{appID}", Group: "Session", Summary: "File an app you can open into one of your sections, moving it out of any other."},
	{Method: "DELETE", Path: "/api/v1/me/sections/{sectionID}/apps/{appID}", Group: "Session", Summary: "Take an app out of a section, back to Your apps."},

	// --- tokens -----------------------------------------------------------
	{Method: "GET", Path: "/api/v1/tokens", Group: "Tokens", Summary: "Your own tokens. Never anyone else's."},
	{Method: "POST", Path: "/api/v1/tokens", Group: "Tokens", Summary: "Mint a delegated token. It acts as you, is bounded by your live grants, and dies with your account (R-058, R-059). The secret is shown once."},
	{Method: "GET", Path: "/api/v1/tokens/service", Group: "Tokens", Summary: "The installation's service tokens.", Verb: string(authz.InstallUsersManage)},
	{Method: "POST", Path: "/api/v1/tokens/service", Group: "Tokens", Summary: "Mint a service token: its own principal, holding only what is shared with it, outliving whoever created it (R-060). The secret is shown once.", Verb: string(authz.InstallUsersManage)},
	{Method: "DELETE", Path: "/api/v1/tokens/{tokenID}", Group: "Tokens", Summary: "Revoke a token. Yours, or anyone's with install.users.manage."},

	// --- apps -------------------------------------------------------------
	{Method: "GET", Path: "/api/v1/apps", Group: "Apps", Summary: "The apps you can administer."},
	{Method: "POST", Path: "/api/v1/apps", Group: "Apps", Summary: "Create an app from a repository. Returns immediately in draft while detection runs.", Verb: string(authz.AppCreate)},
	{Method: "GET", Path: "/api/v1/apps/{appID}", Group: "Apps", Summary: "One app: name, source, state and pinned spec.", Verb: string(authz.AppView)},
	{Method: "PATCH", Path: "/api/v1/apps/{appID}", Group: "Apps", Summary: "Rename an app or change its source.", Verb: string(authz.AppSpecEdit)},
	{Method: "DELETE", Path: "/api/v1/apps/{appID}", Group: "Apps", Summary: "Delete an app. With storage, `backup=true` keeps a final copy and `force=true` discards it; without either, the request is refused so the decision is taken rather than assumed (R-204, R-205).", Verb: string(authz.AppDelete)},
	{Method: "GET", Path: "/api/v1/apps/{appID}/icon", Group: "Apps", Summary: "The image on the app's launcher tile. Anyone who can open the app can load it; `icon_updated_at` on the app says whether there is one and when it changed (R-340)."},
	{Method: "PUT", Path: "/api/v1/apps/{appID}/icon", Group: "Apps", Summary: "Set the app's tile image. The body is the image itself — PNG, JPEG, WebP or GIF, at most 256 KB. SVG is refused (R-340).", Verb: string(authz.AppSpecEdit)},
	{Method: "DELETE", Path: "/api/v1/apps/{appID}/icon", Group: "Apps", Summary: "Remove the app's tile image, so the tile goes back to the map generated for it.", Verb: string(authz.AppSpecEdit)},
	{Method: "GET", Path: "/api/v1/apps/{appID}/status", Group: "Apps", Summary: "What the app is doing now: its state, and each part separately — running, restarting and how often, health, exit code — so a single crash-looping part is visible rather than averaged into one word.", Verb: string(authz.AppView)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/start", Group: "Apps", Summary: "Set the app's desired state to running. The reconciler converges to it, so it survives a restart.", Verb: string(authz.AppRestart)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/stop", Group: "Apps", Summary: "Set the app's desired state to stopped.", Verb: string(authz.AppRestart)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/restart", Group: "Apps", Summary: "Restart the running workloads without changing anything.", Verb: string(authz.AppRestart)},
	{Method: "GET", Path: "/api/v1/apps/{appID}/logs", Group: "Apps", Summary: "The app's own output, from the runtime. `tail` sets how many lines; `workload` picks which part of the app, defaulting to the primary one.", Verb: string(authz.AppLogsRead)},
	{Method: "GET", Path: "/api/v1/apps/{appID}/exec", Group: "Apps", Summary: "A terminal in the running app, over a websocket. Refused when host policy has turned exec off, including for the owner (R-085).", Verb: string(authz.AppExec)},

	// --- detection --------------------------------------------------------
	{Method: "GET", Path: "/api/v1/apps/{appID}/detection", Group: "Detection", Summary: "What Pando worked out about the repository: the winning bid, its evidence, the runners-up and any outstanding questions.", Verb: string(authz.AppView)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/detection/rerun", Group: "Detection", Summary: "Run detection again, against the current commit.", Verb: string(authz.AppSpecEdit)},
	{Method: "GET", Path: "/api/v1/apps/{appID}/detection/diff", Group: "Detection", Summary: "What accepting the proposal would change about the running app.", Verb: string(authz.AppView)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/detection/answers", Group: "Detection", Summary: "Answer detection's questions. Each answer is a fact detection could not find, not a preference.", Verb: string(authz.AppSpecEdit)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/detection/accept", Group: "Detection", Summary: "Accept the proposal, writing a spec revision and pinning it. Accepting over a configured app needs `confirm`.", Verb: string(authz.AppSpecEdit)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/source", Group: "Detection", Summary: "Upload a source archive for an app that has no reachable repository.", Verb: string(authz.AppSpecEdit)},

	// --- specs ------------------------------------------------------------
	{Method: "GET", Path: "/api/v1/apps/{appID}/specs", Group: "Configuration", Summary: "Every spec revision, and which one is pinned. Revisions are append-only (R-152).", Verb: string(authz.AppView)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/specs", Group: "Configuration", Summary: "Write a new spec revision. It does not deploy and does not become pinned.", Verb: string(authz.AppSpecEdit)},
	{Method: "GET", Path: "/api/v1/apps/{appID}/specs/{rev}", Group: "Configuration", Summary: "One revision, in full.", Verb: string(authz.AppView)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/specs/{rev}/pin", Group: "Configuration", Summary: "Pin a revision: what the reconciler converges to, and what the next deploy ships.", Verb: string(authz.AppSpecEdit)},
	{Method: "GET", Path: "/api/v1/apps/{appID}/specs/{a}/diff/{b}", Group: "Configuration", Summary: "The classified difference between two revisions — what a deploy of it would restart, rebuild or leave alone.", Verb: string(authz.AppView)},
	{Method: "GET", Path: "/api/v1/apps/{appID}/export", Group: "Configuration", Summary: "The app's configuration as a document, with every secret redacted (R-194).", Verb: string(authz.AppView)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/plan", Group: "Configuration", Summary: "What a deploy would do, and every reason it would refuse — before anything is created.", Verb: string(authz.AppView)},
	{Method: "GET", Path: "/api/v1/apps/{appID}/slots", Group: "Configuration", Summary: "The things the app says it needs, and what fills each one (R-130).", Verb: string(authz.AppView)},
	{Method: "PUT", Path: "/api/v1/apps/{appID}/slots/{key}", Group: "Configuration", Summary: "Fill a slot: provision one, bind to something already running, or set a value. Takes effect at the next deploy.", Verb: string(authz.AppSpecEdit)},
	{Method: "GET", Path: "/api/v1/apps/{appID}/volumes", Group: "Storage", Summary: "The storage this app keeps. It outlives the app (R-204).", Verb: string(authz.AppView)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/volumes", Group: "Storage", Summary: "Declare a path the app keeps between deploys. Anything written outside one is discarded at the next deploy (R-201).", Verb: string(authz.AppSpecEdit)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/restore", Group: "Storage", Summary: "Restore this app's storage from one of its backups.", Verb: string(authz.AppDeploy)},

	// --- deploys ----------------------------------------------------------
	{Method: "GET", Path: "/api/v1/apps/{appID}/deployments", Group: "Deploys", Summary: "Every deploy of this app, newest first.", Verb: string(authz.AppView)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/deployments", Group: "Deploys", Summary: "Deploy. Returns 202 with a deployment ID; the build runs behind it. Retrying with the same idempotency key replays the first answer rather than deploying twice (R-262).", Verb: string(authz.AppDeploy)},
	{Method: "GET", Path: "/api/v1/apps/{appID}/deployments/{depID}", Group: "Deploys", Summary: "One deploy: what it shipped, and how it ended.", Verb: string(authz.AppView)},
	{Method: "GET", Path: "/api/v1/apps/{appID}/deployments/{depID}/logs", Group: "Deploys", Summary: "The deploy's output as server-sent events, flushed per line while it runs (R-170).", Verb: string(authz.AppLogsRead)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/deployments/rollback", Group: "Deploys", Summary: "Deploy the last revision that ran successfully.", Verb: string(authz.AppDeploy)},

	// --- secrets ----------------------------------------------------------
	{Method: "GET", Path: "/api/v1/apps/{appID}/secrets", Group: "Secrets", Summary: "Which secrets this app has, and where each came from. Never their values.", Verb: string(authz.AppView)},
	{Method: "PUT", Path: "/api/v1/apps/{appID}/secrets/{key}", Group: "Secrets", Summary: "Set a secret. Rotating one recreates the workload rather than leaving it running with the old value (R-193).", Verb: string(authz.AppSecretsWrite)},
	{Method: "DELETE", Path: "/api/v1/apps/{appID}/secrets/{key}", Group: "Secrets", Summary: "Remove a secret.", Verb: string(authz.AppSecretsWrite)},
	{Method: "GET", Path: "/api/v1/apps/{appID}/secrets/{key}/value", Group: "Secrets", Summary: "Read one secret's value. Its own verb, separate from managing the app, and audited every time (R-083).", Verb: string(authz.AppSecretsRead)},

	// --- security ---------------------------------------------------------
	{Method: "GET", Path: "/api/v1/apps/{appID}/security", Group: "Security", Summary: "The app's security score, what it was taken from, and the findings behind it (R-310).", Verb: string(authz.AppView)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/security/scan", Group: "Security", Summary: "Scan the app now. A write, not a refresh: the score decides whether the next deploy is allowed (R-312, R-314).", Verb: string(authz.AppDeploy)},

	// --- sharing ----------------------------------------------------------
	{Method: "GET", Path: "/api/v1/apps/{appID}/grants", Group: "Sharing", Summary: "Who can reach this app, and who can administer it — two planes, listed separately (R-070, R-071).", Verb: string(authz.AppView)},
	{Method: "POST", Path: "/api/v1/apps/{appID}/grants", Group: "Sharing", Summary: "Share the app with a user, a group, a token, or with everyone. The anonymous grant is a real row, refused where host policy forbids it (R-075, R-076).", Verb: string(authz.AppGrantsManage)},
	{Method: "DELETE", Path: "/api/v1/apps/{appID}/grants/{grantID}", Group: "Sharing", Summary: "Take a grant away.", Verb: string(authz.AppGrantsManage)},

	// --- accounts, groups, roles -----------------------------------------
	{Method: "GET", Path: "/api/v1/users", Group: "Identity", Summary: "The accounts on this installation.", Verb: string(authz.InstallView)},
	{Method: "POST", Path: "/api/v1/users", Group: "Identity", Summary: "Create an account.", Verb: string(authz.InstallUsersManage)},
	{Method: "GET", Path: "/api/v1/users/{userID}", Group: "Identity", Summary: "One account. Your own needs no verb.", Verb: string(authz.InstallView)},
	{Method: "PATCH", Path: "/api/v1/users/{userID}", Group: "Identity", Summary: "Change an account: display name, email, or suspension. Suspension is not deletion (R-049).", Verb: string(authz.InstallUsersManage)},
	{Method: "DELETE", Path: "/api/v1/users/{userID}", Group: "Identity", Summary: "Delete an account, with the destruction rules that follow from it (R-282).", Verb: string(authz.InstallUsersManage)},
	{Method: "PUT", Path: "/api/v1/users/{userID}/role", Group: "Identity", Summary: "Give an account an installation role. Deliberately not a field on PATCH: changing someone's status and changing their power are different acts.", Verb: string(authz.InstallUsersManage)},
	{Method: "DELETE", Path: "/api/v1/users/{userID}/role", Group: "Identity", Summary: "Take an installation role away. The last administrator cannot be demoted.", Verb: string(authz.InstallUsersManage)},
	{Method: "GET", Path: "/api/v1/groups", Group: "Identity", Summary: "Groups, whether Pando's own or an identity adapter's (R-078).", Verb: string(authz.InstallView)},
	{Method: "POST", Path: "/api/v1/groups", Group: "Identity", Summary: "Create a group.", Verb: string(authz.InstallUsersManage)},
	{Method: "PUT", Path: "/api/v1/groups/{groupID}/members", Group: "Identity", Summary: "Set a group's members.", Verb: string(authz.InstallUsersManage)},
	{Method: "DELETE", Path: "/api/v1/groups/{groupID}", Group: "Identity", Summary: "Delete a group.", Verb: string(authz.InstallUsersManage)},
	{Method: "GET", Path: "/api/v1/roles", Group: "Identity", Summary: "The roles that can be granted, built in and custom. Built-in roles are immutable (R-081).", Verb: string(authz.InstallView)},
	{Method: "POST", Path: "/api/v1/roles", Group: "Identity", Summary: "Compose a custom role from verbs (R-082).", Verb: string(authz.InstallUsersManage)},
	{Method: "DELETE", Path: "/api/v1/roles/{roleID}", Group: "Identity", Summary: "Delete a custom role.", Verb: string(authz.InstallUsersManage)},
	{Method: "GET", Path: "/api/v1/verbs", Group: "Identity", Summary: "Every verb, by scope, for composing a role. There is no implication graph: holding one says nothing about another (R-082).", Verb: string(authz.InstallView)},

	// --- installation -----------------------------------------------------
	{Method: "GET", Path: "/api/v1/adapters", Group: "Installation", Summary: "The adapters configured here and what they can currently do — live capabilities, not stored configuration. Names which credentials are set, never their values.", Verb: string(authz.InstallView)},
	{Method: "POST", Path: "/api/v1/adapters", Group: "Installation", Summary: "Configure an adapter. Settings go in config; credentials such as an API key go in credentials, which is write-only and stored encrypted.", Verb: string(authz.InstallAdaptersManage)},
	{Method: "GET", Path: "/api/v1/capacity", Group: "Installation", Summary: "What the host has, and what is committed to apps (R-242).", Verb: string(authz.InstallView)},
	{Method: "GET", Path: "/api/v1/policy", Group: "Installation", Summary: "Host policy. Reading the rules you work under is not the same privilege as changing them (R-274).", Verb: string(authz.InstallView)},
	{Method: "PUT", Path: "/api/v1/policy", Group: "Installation", Summary: "Replace host policy. Policy is a floor, never an override (R-272).", Verb: string(authz.InstallPolicyManage)},
	{Method: "POST", Path: "/api/v1/policy/preview", Group: "Installation", Summary: "Which apps a candidate policy would block, before it is saved.", Verb: string(authz.InstallPolicyManage)},
	{Method: "GET", Path: "/api/v1/audit", Group: "Installation", Summary: "The audit log. Append-only: no endpoint edits or deletes an event, and the database refuses it too (R-027).", Verb: string(authz.InstallAuditRead)},
	{Method: "GET", Path: "/api/v1/backups", Group: "Installation", Summary: "The backups this installation holds.", Verb: string(authz.InstallBackupManage)},
	{Method: "POST", Path: "/api/v1/backups", Group: "Installation", Summary: "Take a backup now.", Verb: string(authz.InstallBackupManage)},
	{Method: "POST", Path: "/api/v1/backups/{backupID}/verify", Group: "Installation", Summary: "Check a backup before it is needed, rather than at the moment of disaster (R-216).", Verb: string(authz.InstallBackupManage)},
	{Method: "POST", Path: "/api/v1/backups/{backupID}/restore", Group: "Installation", Summary: "Restore from a backup. Verified first: an incomplete one is refused rather than half-applied (R-215).", Verb: string(authz.InstallBackupManage)},

	// --- the reference itself --------------------------------------------
	{Method: "GET", Path: "/api/v1/reference", Group: "Reference", Summary: "This document: every endpoint, every CLI command, every MCP tool and every error code, built from the running binary."},
}

// handleReference serves the API's own description.
//
// It asks for nothing, not even a session. The document holds no data about
// this installation — no app, no account, no policy, nothing an anonymous
// visitor could not read in the repository, where `docs/api.md` is the same
// document. What it holds is the shape of the API, and R-261 makes that the
// product: a client that has to authenticate before it can learn how to
// authenticate is a product with one client.
//
// Nothing in it is a credential and nothing in it is a capability. The verbs it
// names are the ones every endpoint already returns in its own error envelope
// when a caller lacks them.
func (s *Server) handleReference(w http.ResponseWriter, r *http.Request) {
	JSON(w, http.StatusOK, reference.Build(routeDocs))
}

// Reference returns the assembled document, for the generator that writes
// `docs/` from it.
func Reference() reference.Document { return reference.Build(routeDocs) }

// normalizeRoutePath makes a chi pattern comparable with a documented path.
//
// chi reports a subrouter's index as "/api/v1/apps/" and a documented path says
// "/api/v1/apps", because that is what a person types.
func normalizeRoutePath(path string) string {
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		return strings.TrimSuffix(path, "/")
	}
	return path
}
