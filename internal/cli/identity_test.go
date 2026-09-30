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
