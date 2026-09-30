package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var identityKinds = map[string]any{"providers": []any{}, "kinds": []map[string]any{{
	"category": "identity", "kind": "oidc", "name": "OpenID Connect", "id_prefix": "idp_",
	"fields": []map[string]any{
		{"key": "issuer", "label": "Issuer URL", "type": "string", "required": true},
		{"key": "client_id", "label": "Client ID", "type": "string", "required": true},
		{"key": "client_secret", "label": "Client secret", "type": "string", "credential": true},
		{"key": "groups_claim", "label": "Groups claim", "type": "string"},
	},
	"presets": []map[string]any{{"id": "okta", "label": "Okta",
		"values": map[string]string{"issuer": "https://{your-okta-domain}", "groups_claim": "groups"}}},
}}}

// R-043 and R-261: an identity provider can be connected from the CLI as from
// the console — a preset's settings, the client secret read without echoing,
// and a placeholder left in a preset refused before anything is sent.
func TestR043_AnIdentityProviderCanBeAddedFromTheCLI(t *testing.T) {
	api := newAPI(t).
		reply("GET /identity-providers", identityKinds).
		reply("POST /identity-providers", map[string]any{"id": "idp_1", "callback_url": "https://pando.example.com/api/v1/auth/providers/idp_1/callback"})

	got := run(t, api, "s3cret\n", "identity-provider", "add", "oidc", "--preset", "okta",
		"--set", "issuer=https://example.okta.com", "--set", "client_id=0oa1", "--jit")
	require.NoError(t, got.err, got.errOut)
	require.JSONEq(t, `{
		"kind": "oidc", "name": "Okta", "enabled": false, "jit_provisioning": true, "link_by_email": false,
		"config": {"issuer": "https://example.okta.com", "client_id": "0oa1", "groups_claim": "groups"},
		"credentials": {"client_secret": "s3cret"}
	}`, api.bodyFor("POST /identity-providers"))
	require.Contains(t, got.out, "/api/v1/auth/providers/idp_1/callback")
	require.Contains(t, got.out, "pando idp test idp_1")
	require.NotContains(t, got.out, "s3cret")

	require.ErrorContains(t, run(t, api, "\n", "identity-provider", "add", "oidc", "--preset", "okta", "--set", "client_id=x").err, "placeholder")
	require.ErrorContains(t, run(t, api, "", "identity-provider", "add", "oidc", "--name", "X", "--set", "client_secret=x").err, "is a secret")
	require.ErrorContains(t, run(t, api, "", "identity-provider", "add", "ldap").err, "no \"ldap\"")
}

// O-1: linking from the CLI says when an account became an alias.
func TestO1_LinkingFromTheCLISaysWhatBecameAnAlias(t *testing.T) {
	api := newAPI(t).reply("POST /users/usr_1/identities", map[string]any{"aliased_user_id": "usr_2"})
	got := run(t, api, "", "identity-provider", "link", "usr_1", "idp_1", "00u1", "--replace")
	require.NoError(t, got.err, got.errOut)
	require.JSONEq(t, `{"adapter_id":"idp_1","external_id":"00u1","replace_account":true}`, api.bodyFor("POST /users/usr_1/identities"))
	require.Contains(t, got.out, "usr_2 is now a suspended alias")
}

// R-047 and R-261: the provider list says, per provider, how soon each can
// revoke — the same number the console shows.
func TestR047_TheCLIListsEachProvidersRevocationWindow(t *testing.T) {
	api := newAPI(t).reply("GET /identity-providers", map[string]any{"providers": []map[string]any{
		{"id": "idp_local", "name": "Local users", "kind": "local", "enabled": true,
			"revocation": map[string]any{"mode": "push", "window_seconds": 120}},
		{"id": "idp_1", "name": "Okta", "kind": "oidc", "enabled": false, "scim_enabled": true,
			"callback_url": "https://pando.example.com/cb", "revocation": map[string]any{"mode": "push", "window_seconds": 120}},
		{"id": "idp_2", "name": "Broken", "kind": "saml", "problem": "That is not SAML identity provider metadata."},
	}})
	got := run(t, api, "", "identity-provider", "list")
	require.NoError(t, got.err, got.errOut)
	require.Contains(t, got.out, "120s (push)")
	require.Contains(t, got.out, "https://pando.example.com/cb")
	require.Contains(t, got.out, "not usable: That is not SAML")
}

// A change keeps every setting it does not name, and asks for a secret rather
// than taking one on the command line.
func TestR043_ChangingAProviderFromTheCLIKeepsTheRest(t *testing.T) {
	api := newAPI(t).
		reply("GET /identity-providers/idp_1", map[string]any{"kind": "oidc",
			"config": map[string]any{"issuer": "https://example.okta.com", "client_id": "0oa1"}}).
		reply("GET /identity-providers", identityKinds)

	got := run(t, api, "n3w\n", "identity-provider", "set", "idp_1",
		"--set", "groups_claim=roles", "--secret", "--enable", "--jit", "true", "--link-by-email", "false", "--name", "Okta prod")
	require.NoError(t, got.err, got.errOut)
	require.JSONEq(t, `{
		"name": "Okta prod", "enabled": true, "jit_provisioning": true, "link_by_email": false,
		"config": {"issuer": "https://example.okta.com", "client_id": "0oa1", "groups_claim": "roles"},
		"credentials": {"client_secret": "n3w"}
	}`, api.bodyFor("PATCH /identity-providers/idp_1"))
	require.Contains(t, got.out, "next sign-in")

	require.ErrorContains(t, run(t, api, "", "identity-provider", "set", "idp_1").err, "nothing to change")
	require.ErrorContains(t, run(t, api, "", "identity-provider", "set", "idp_1", "--enable", "--disable").err, "together")
	require.ErrorContains(t, run(t, api, "", "identity-provider", "set", "idp_1", "--jit", "sometimes").err, "true or false")
	require.ErrorContains(t, run(t, api, "", "identity-provider", "set", "idp_1", "--set", "nokey").err, "KEY=VALUE")
	require.ErrorContains(t, run(t, api, "", "identity-provider", "set", "idp_1", "--set", "colour=red").err, "no setting")
}

// The rest of `pando idp`: remove, check, test, SCIM tokens, unlinking and
// group links, each saying what it did.
func TestR048_TheCLIManagesSCIMTokensAndLinks(t *testing.T) {
	api := newAPI(t).
		reply("POST /identity-providers/idp_1/check", map[string]any{"ok": true}).
		reply("POST /identity-providers/idp_1/scim-token", map[string]any{"token": "pando_scim_x", "scim_base_url": "https://p/api/v1/scim/v2"}).
		fail("POST /identity-providers/idp_2/check", 200, map[string]any{"ok": false, "message": "Pando could not reach it.", "remedy": "Check the URL."})

	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"remove", "idp_1"}, "Removed idp_1"},
		{[]string{"check", "idp_1"}, "The provider answers"},
		{[]string{"test", "idp_1"}, "/api/v1/identity-providers/idp_1/test"},
		{[]string{"scim-token", "idp_1"}, "Token:         pando_scim_x"},
		{[]string{"scim-token", "idp_1", "--off"}, "SCIM is off"},
		{[]string{"unlink", "usr_1", "idp_1", "00u1"}, "Unlinked 00u1"},
		{[]string{"link-group", "grp_1", "grp_2"}, "Linked grp_2 and grp_1"},
		{[]string{"link-group", "grp_1", "grp_2", "--remove"}, "Unlinked grp_2"},
	} {
		got := run(t, api, "", "identity-provider", c.args...)
		require.NoError(t, got.err, "%v: %s", c.args, got.errOut)
		require.Contains(t, got.out, c.want, c.args)
	}
	require.True(t, api.sawPath("/identity-providers/idp_1/scim-token"))
	require.ErrorContains(t, run(t, api, "", "identity-provider", "check", "idp_2").err, "Check the URL")
}

// A SAML provider is added with the addresses to give it; an unknown preset is
// refused, and so is a provider with no name.
func TestR043_AddingASAMLProviderFromTheCLIPrintsItsAddresses(t *testing.T) {
	api := newAPI(t).
		reply("GET /identity-providers", map[string]any{"kinds": []map[string]any{{
			"kind": "saml", "name": "SAML 2.0",
			"fields": []map[string]any{
				{"key": "idp_metadata_url", "label": "Metadata URL", "type": "string"},
				{"key": "trust_email", "label": "Trust email", "type": "bool"},
			},
		}}}).
		reply("POST /identity-providers", map[string]any{"id": "idp_s", "callback_url": "https://p/acs", "entity_id": "https://p/md"})

	got := run(t, api, "", "identity-provider", "add", "saml", "--name", "Entra",
		"--set", "idp_metadata_url=https://login.example.com/md", "--set", "trust_email=true", "--enable")
	require.NoError(t, got.err, got.errOut)
	require.Contains(t, got.out, "ACS URL:   https://p/acs")
	require.Contains(t, got.out, "Entity ID: https://p/md")
	require.NotContains(t, got.out, "pando idp test")
	require.JSONEq(t, `{"kind":"saml","name":"Entra","enabled":true,"jit_provisioning":false,"link_by_email":false,
		"config":{"idp_metadata_url":"https://login.example.com/md","trust_email":true}}`, api.bodyFor("POST /identity-providers"))

	require.ErrorContains(t, run(t, api, "", "identity-provider", "add", "saml", "--preset", "okta").err, "no preset")
	require.ErrorContains(t, run(t, api, "", "identity-provider", "add", "saml").err, "--name")
	require.ErrorContains(t, run(t, api, "", "identity-provider", "add", "saml", "--name", "X", "--set", "trust_email=often").err, "true or false")
}
