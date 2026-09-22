package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// A time that is neither a time nor a duration is refused before any request,
// naming the flag; and a refusal from the server reaches the person.
func TestAuditRefusesBadTimesAndReportsTheServer(t *testing.T) {
	api := newAPI(t)

	got := run(t, api, "", "audit", "--since", "yesterday")
	require.ErrorContains(t, got.err, "--since")
	got = run(t, api, "", "audit", "--until", "soon")
	require.ErrorContains(t, got.err, "--until")
	require.Empty(t, api.calls)

	// A token's action reads as the token for its owner; a target reads with
	// its kind; a last page prints no cursor.
	api.reply("GET /audit", map[string]any{
		"events": []map[string]any{{
			"occurred_at": "2026-09-21T09:00:00Z", "action": "app.deploy",
			"principal_id": "tok_1", "on_behalf_of": "usr_1", "target_kind": "app", "target_id": "app_1",
		}},
	})
	got = run(t, api, "", "audit", "--since", "1h")
	require.NoError(t, got.err, got.errOut)
	require.Contains(t, got.out, "tok_1 for usr_1")
	require.Contains(t, got.out, "app app_1")
	require.NotContains(t, got.errOut, "--before")

	refused := newAPI(t).fail("GET /audit", http.StatusForbidden, map[string]string{"code": "PERM_DENIED", "message": "This needs install.audit.read."})
	require.ErrorContains(t, run(t, refused, "", "audit").err, "install.audit.read")
}
