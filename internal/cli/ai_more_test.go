package cli_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/cli"
)

// The draft commands send the description as one string and print what came
// back as JSON, applying nothing (R-343, R-344).
func TestR261_AIDraftCommandsPrintTheDraftAsJSON(t *testing.T) {
	api := newAPI(t).
		reply("POST /ai/access/draft", map[string]any{
			"role":  map[string]any{"name": "deployers", "verbs": []string{"app.deploy"}},
			"group": map[string]any{"name": "release"},
		}).
		reply("POST /ai/policy/draft", map[string]any{
			"policy": map[string]any{"max_memory_mb": 512},
			"note":   "Memory is capped for every app.",
		})

	got := run(t, api, "", "ai", "draft-access", "people", "who", "deploy")
	require.NoError(t, got.err, got.errOut)
	require.JSONEq(t, `{"description":"people who deploy"}`, api.bodyFor("POST /ai/access/draft"))
	var access map[string]any
	require.NoError(t, json.Unmarshal([]byte(got.out), &access), got.out)
	require.Equal(t, "release", access["group"].(map[string]any)["name"])

	got = run(t, api, "", "ai", "draft-policy", "cap memory at 512 MB")
	require.NoError(t, got.err, got.errOut)
	require.JSONEq(t, `{"description":"cap memory at 512 MB"}`, api.bodyFor("POST /ai/policy/draft"))
	var policy map[string]any
	require.NoError(t, json.Unmarshal([]byte(got.out), &policy), got.out)
	require.Equal(t, "Memory is capped for every app.", policy["note"])
}

// A function set in the console shows "console"; one never set shows "-".
func TestAIFunctionsSaysWhereEachAssignmentWasSet(t *testing.T) {
	api := newAPI(t).reply("GET /ai/functions", map[string]any{"functions": []map[string]any{
		{"function": "search_audit", "adapter_id": "ai_openai", "on": true,
			"source": map[string]any{"kind": "database", "key": "search_audit"}},
		{"function": "draft_policy", "on": false},
	}})

	got := run(t, api, "", "ai", "functions")
	require.NoError(t, got.err, got.errOut)
	require.Regexp(t, `search_audit\s+ai_openai\s+-\s+on\s+console`, got.out)
	require.Regexp(t, `draft_policy\s+-\s+-\s+off\s+-`, got.out)
}

// The note that says what the search left out is printed under the summary.
func TestAIAuditPrintsTheNote(t *testing.T) {
	api := newAPI(t).reply("POST /ai/audit/search", map[string]any{
		"summary": "Ben deployed twice.",
		"note":    "Only the most recent 200 records were read.",
		"matched": 2,
		"filter":  map[string]any{"actor": "usr_01HQ8"},
	})

	got := run(t, api, "", "ai", "audit", "what did ben do?")
	require.NoError(t, got.err, got.errOut)
	require.Contains(t, got.out, "Ben deployed twice.\nOnly the most recent 200 records were read.\n")
	require.Contains(t, got.out, "2 matched. Filters:")
	require.Contains(t, got.out, "usr_01HQ8")
}

// An error from the API reaches the person running the command, message
// intact, from every ai subcommand.
func TestAICommandsSurfaceAPIErrors(t *testing.T) {
	unavailable := map[string]string{
		"code":    "ADAPTER_UNAVAILABLE",
		"message": "No AI adapter handles this function.",
	}
	cases := []struct {
		key  string
		args []string
	}{
		{"GET /ai/functions", []string{"functions"}},
		{"PUT /ai/functions/search_audit", []string{"assign", "search_audit", "ai_openai"}},
		{"DELETE /ai/functions/search_audit", []string{"unassign", "search_audit"}},
		{"POST /ai/reference/answer", []string{"ask", "how?"}},
		{"POST /ai/audit/search", []string{"audit", "who?"}},
		{"POST /ai/access/draft", []string{"draft-access", "deployers"}},
		{"POST /ai/policy/draft", []string{"draft-policy", "small apps"}},
	}
	for _, tc := range cases {
		t.Run(tc.args[0], func(t *testing.T) {
			api := newAPI(t).fail(tc.key, http.StatusBadGateway, unavailable)
			got := run(t, api, "", "ai", tc.args...)
			require.ErrorContains(t, got.err, "No AI adapter handles this function.")
			var apiErr *cli.APIError
			require.ErrorAs(t, got.err, &apiErr)
			require.Equal(t, http.StatusBadGateway, apiErr.Status)
			require.Empty(t, got.out, "nothing is printed on failure")
		})
	}
}

// Each subcommand refuses the wrong number of arguments before calling the
// API.
func TestAICommandsCheckTheirArguments(t *testing.T) {
	for _, args := range [][]string{
		{"functions", "extra"},
		{"assign", "search_audit"},
		{"unassign"},
		{"unassign", "a", "b"},
		{"ask"},
		{"audit"},
		{"draft-access"},
		{"draft-policy"},
	} {
		api := newAPI(t)
		got := run(t, api, "", "ai", args...)
		require.Error(t, got.err, "ai %v", args)
		require.Empty(t, api.calls, "ai %v called the API", args)
	}
}

// Without a server or a stored login, every subcommand says so rather than
// calling anywhere.
func TestAICommandsNeedALogin(t *testing.T) {
	t.Setenv(cli.EnvServer, "")
	t.Setenv(cli.EnvToken, "")
	for _, args := range [][]string{
		{"functions"},
		{"assign", "search_audit", "ai_openai"},
		{"unassign", "search_audit"},
		{"ask", "how?"},
		{"audit", "who?"},
		{"draft-access", "deployers"},
		{"draft-policy", "small apps"},
	} {
		isolateConfig(t)
		ai := aiCommand(t)
		ai.SetArgs(args)
		require.ErrorIs(t, ai.Execute(), cli.ErrNotLoggedIn, "ai %v", args)
	}
}

// aiCommand is a fresh "pando ai" with its output discarded.
func aiCommand(t *testing.T) *cobra.Command {
	t.Helper()
	for _, c := range cli.Commands() {
		if c.Name() == "ai" {
			c.SetOut(io.Discard)
			c.SetErr(io.Discard)
			c.SilenceUsage, c.SilenceErrors = true, true
			return c
		}
	}
	t.Fatal("no ai command")
	return nil
}
