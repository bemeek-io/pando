package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestR261_AIFunctionsAreReachableFromTheCLI asserts R-261 and R-259: each
// ai subcommand is one call to its endpoint.
func TestR261_AIFunctionsAreReachableFromTheCLI(t *testing.T) {
	api := newAPI(t).
		reply("PUT /ai/functions/search_audit", map[string]any{"function": "search_audit"}).
		reply("DELETE /ai/functions/search_audit", map[string]any{}).
		reply("GET /ai/functions", map[string]any{"functions": []map[string]any{
			{"function": "repair_plan", "adapter_id": "ai_anthropic", "effective_model": "claude-opus-5-5", "on": true,
				"source": map[string]any{"kind": "file", "key": "adapters.ai_anthropic.functions.repair_plan"}},
			{"function": "search_audit", "on": false},
		}}).
		reply("POST /ai/audit/search", map[string]any{
			"summary": "Two apps were created.", "matched": 2, "filter": map[string]any{"actions": []string{"app.create"}},
		}).
		reply("POST /ai/reference/answer", map[string]any{"answer": "Use pando group create.", "cites": []string{"pando group create"}})

	got := run(t, api, "", "ai", "assign", "search_audit", "ai_openai", "--model", "small")
	require.NoError(t, got.err, got.errOut)
	require.JSONEq(t, `{"adapter_id":"ai_openai","model":"small"}`, api.calls[0].body)

	got = run(t, api, "", "ai", "unassign", "search_audit")
	require.NoError(t, got.err, got.errOut)
	require.Equal(t, "DELETE", api.calls[1].method)

	got = run(t, api, "", "ai", "functions")
	require.NoError(t, got.err, got.errOut)
	require.Contains(t, got.out, "adapters.ai_anthropic.functions.repair_plan")
	require.Contains(t, got.out, "off")

	got = run(t, api, "", "ai", "audit", "which", "apps", "were", "added?")
	require.NoError(t, got.err, got.errOut)
	require.JSONEq(t, `{"question":"which apps were added?"}`, api.calls[3].body)
	require.Contains(t, got.out, "Two apps were created.")
	require.Contains(t, got.out, "app.create")

	got = run(t, api, "", "ai", "ask", "how do I make a group?")
	require.NoError(t, got.err, got.errOut)
	require.Contains(t, got.out, "see: pando group create")
}
