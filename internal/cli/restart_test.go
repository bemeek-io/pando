package cli_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// R-261: restarting is an endpoint, so it is a command too — a saved adapter
// can be put into effect without a shell on the host.
func TestRestartAsksTheAPIAndSaysSo(t *testing.T) {
	api := newAPI(t).
		reply("GET /adapters", map[string]any{"adapters": []any{}, "started_at": "2026-09-22T17:40:29Z"}).
		reply("POST /restart", map[string]any{"restarting": true})

	got := run(t, api, "", "restart", "--wait", "0")
	require.NoError(t, got.err)
	require.Contains(t, got.out, "Pando is restarting.")
	require.True(t, api.sawPath("/restart"))
}

// Waiting ends when Pando answers with a start time other than the one it had.
func TestRestartWaitsUntilPandoIsBack(t *testing.T) {
	api := newAPI(t)
	before := true
	api.handle("GET /adapters", func() any {
		if before {
			before = false
			return map[string]any{"adapters": []any{}, "started_at": "2026-09-22T17:40:29Z"}
		}
		return map[string]any{"adapters": []any{}, "started_at": "2026-09-22T17:40:38Z"}
	})
	api.reply("POST /restart", map[string]any{"restarting": true})

	got := run(t, api, "", "restart")
	require.NoError(t, got.err)
	require.Contains(t, got.out, "done.")
}

// A start time that never changes is Pando not coming back, and the command
// says where to look rather than waiting forever.
func TestRestartSaysWhenPandoDoesNotComeBack(t *testing.T) {
	api := newAPI(t).
		reply("GET /adapters", map[string]any{"adapters": []any{}, "started_at": "2026-09-22T17:40:29Z"}).
		reply("POST /restart", map[string]any{"restarting": true})

	got := run(t, api, "", "restart", "--wait", "2s")
	require.Error(t, got.err)
	require.Contains(t, got.err.Error(), "has not come back")
	require.Contains(t, got.err.Error(), "docker compose logs pando")
}

// Refused for whoever may not manage adapters, in the API's own words.
func TestRestartReportsARefusal(t *testing.T) {
	api := newAPI(t).
		reply("GET /adapters", map[string]any{"adapters": []any{}, "started_at": "2026-09-22T17:40:29Z"}).
		fail("POST /restart", http.StatusForbidden, map[string]string{
			"code": "PERM_VERB_REQUIRED", "message": "You need install.adapters.manage."})

	got := run(t, api, "", "restart")
	require.Error(t, got.err)
	require.Contains(t, got.err.Error(), "install.adapters.manage")
}
