package cli_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// R-261: the audit log's filters are the API's, so the CLI passes each one on
// under its API name, and says how to read the next page.
func TestAuditPassesEveryFilterAndPages(t *testing.T) {
	api := newAPI(t).reply("GET /audit", map[string]any{
		"events": []map[string]any{{
			"occurred_at": "2026-09-21T09:00:00Z", "action": "app.update",
			"principal_id": "usr_1", "target_kind": "app", "target_id": "app_1",
		}},
		"next_before": "41",
	})

	got := run(t, api, "", "audit",
		"--action", "app.", "--actor", "usr_1", "--actor-kind", "user", "--app", "app_1",
		"--target-kind", "app", "--target", "app_1", "--involving", "usr_2",
		"--since", "2026-09-20T00:00:00Z", "--until", "2026-09-22T00:00:00Z",
		"--limit", "50", "--before", "99")
	require.NoError(t, got.err, got.errOut)

	require.Len(t, api.calls, 1)
	u, err := url.Parse(api.calls[0].path)
	require.NoError(t, err)
	q := u.Query()
	for key, want := range map[string]string{
		"action": "app.", "principal_id": "usr_1", "principal_kind": "user", "app_id": "app_1",
		"target_kind": "app", "target_id": "app_1", "involving": "usr_2",
		"since": "2026-09-20T00:00:00Z", "until": "2026-09-22T00:00:00Z",
		"limit": "50", "before": "99",
	} {
		require.Equal(t, want, q.Get(key), key)
	}

	require.Contains(t, got.out, "app.update")
	require.True(t, strings.Contains(got.errOut, "--before 41"), got.errOut)
}
