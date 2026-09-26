package policy

// What each policy field does, in the words the Policy screen uses for it.
//
// Handed to the policy drafting function (R-344) so it can find the setting a
// person means. Without it the model sees field names only, and "turn off
// terminal access" became an edit to disabled_verbs described as one — the
// right change, named in a way nobody who uses the Policy screen would
// recognize. TestEveryPolicyFieldIsDescribed keeps this complete.
var descriptions = map[string]string{
	"source_allowlist": "Where apps may be created from: repository hosts or URL prefixes. Empty means anywhere.",
	"disabled_verbs": "Permissions nobody may use, install-wide, including an app's owner. " +
		"app.exec here is the Policy screen's \"Turn off terminal access for the whole installation\".",
	"agent_disabled_verbs": "Verbs an agent's token may not use: denied to CLI and MCP tokens only, not to people.",
	"allow_anonymous_grants": "Older form of public_sharing: false means apps cannot be shared with everyone. " +
		"Prefer public_sharing.",
	"public_sharing": "Whether an app may be shared with anyone on the internet: allowed, passcode_only " +
		"(only behind the app's passcode), or none.",
	"min_build_isolation":   "Minimum isolation for builds, as an isolation class number (10 is a container).",
	"min_runtime_isolation": "Minimum isolation for running apps, as an isolation class number (10 is a container).",
	"egress_allowlist":      "Where apps may connect out to, by default. An app's own list replaces this one.",
	"require_backup_before_destroy": "Require a backup before anything is destroyed: an app's storage is " +
		"backed up before the app or its volumes are deleted.",
	"max_token_lifetime_days": "Longest a token may live, in days. 0 means no limit.",
	"max_log_disk_bytes":      "Total disk for app logs across every app, in bytes. 0 means no limit.",
	"disable_ai_screening": "Turn off AI screening of deployment plans: AI is never sent a repository to " +
		"repair a plan or answer detection's questions.",
	"min_security_score": "Minimum security score a deploy must reach, 0 to 100. 0 means off.",
	"insecure_action": "What happens to a running app that falls below the minimum security score: " +
		"warn, or stop (after the grace period).",
	"insecure_grace_hours":      "Grace period, in hours, before stop applies to an app below the minimum score.",
	"ignore_unfixable_findings": "Ignore findings with no fix available, in both the security score and the list.",
}

// Describe says what a policy field does, in the Policy screen's words.
func Describe(key string) string { return descriptions[key] }
