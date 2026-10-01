package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// R-261: deploy approval is reachable from a terminal, as it is from the
// console and an agent.
func TestR154_ApprovalsListShowsWhatIsWaiting(t *testing.T) {
	api := newAPI(t).reply("GET /approvals", map[string]any{
		"approvals": []map[string]any{{
			"id": "dep_01", "app_id": "app_01HQ8", "app_name": "notes", "status": "awaiting_approval",
			"approvals_required": 2, "approval_expires_at": "2026-10-08T00:00:00Z",
			"approval_reasons": []map[string]string{{"reason": "install", "message": "This installation requires approval for every deploy."}},
			"approvals":        []map[string]string{{"decision": "approve"}},
			"can_decide":       true,
		}},
	})

	got := run(t, api, "", "approvals", "list")
	require.NoError(t, got.err)
	require.Contains(t, got.out, "notes (app_01HQ8)")
	require.Contains(t, got.out, "dep_01")
	require.Contains(t, got.out, "1 of 2")
	require.Contains(t, got.out, "install")

	api = newAPI(t).reply("GET /approvals", map[string]any{"approvals": []any{}})
	got = run(t, api, "", "approvals", "list")
	require.NoError(t, got.err)
	require.Contains(t, got.out, "Nothing is waiting")
}

func TestR154_ApproveAndRejectCallTheirEndpoints(t *testing.T) {
	api := newAPI(t).reply("POST /apps/app_01HQ8/deployments/dep_01/approve",
		map[string]any{"id": "dep_01", "status": "pending"})
	got := run(t, api, "", "approvals", "approve", "app_01HQ8", "dep_01", "--comment", "looks right")
	require.NoError(t, got.err)
	require.JSONEq(t, `{"comment":"looks right"}`, api.bodyFor("POST /apps/app_01HQ8/deployments/dep_01/approve"))
	require.Contains(t, got.out, "Deploying")

	api = newAPI(t).reply("POST /apps/app_01HQ8/deployments/dep_01/approve",
		map[string]any{"id": "dep_01", "status": "awaiting_approval", "approvals_required": 2,
			"approvals": []map[string]string{{"decision": "approve"}}})
	got = run(t, api, "", "approvals", "approve", "app_01HQ8", "dep_01")
	require.NoError(t, got.err)
	require.Contains(t, got.out, "1 of the 2")

	api = newAPI(t).reply("POST /apps/app_01HQ8/deployments/dep_01/reject",
		map[string]any{"id": "dep_01", "status": "rejected"})
	got = run(t, api, "", "approvals", "reject", "app_01HQ8", "dep_01")
	require.NoError(t, got.err)
	require.JSONEq(t, `{}`, api.bodyFor("POST /apps/app_01HQ8/deployments/dep_01/reject"))
	require.Contains(t, got.out, "Rejected")
}

// A deploy that needs approval says so, and says how it gets approved,
// rather than telling somebody to watch logs that will never come.
func TestR154_ADeployThatNeedsApprovalSaysSo(t *testing.T) {
	api := newAPI(t).reply("POST /apps/app_01HQ8/deployments", map[string]any{
		"id": "dep_01", "status": "awaiting_approval", "approvals_required": 1,
		"approval_reasons": []map[string]string{{"reason": "app_spec", "message": "This app requires approval for its own deploys."}},
	})
	got := run(t, api, "", "deploy", "app_01HQ8")
	require.NoError(t, got.err)
	require.Contains(t, got.out, "needs approval")
	require.Contains(t, got.out, "This app requires approval for its own deploys.")
	require.Contains(t, got.out, "pando approvals approve app_01HQ8 dep_01")
	require.NotContains(t, got.out, "pando logs")
}
