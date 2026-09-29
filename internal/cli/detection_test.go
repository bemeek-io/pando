package cli_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/cli"
)

// sequence answers each call with the next reply, and the last from then on.
func sequence(replies ...any) func() any {
	n := 0
	return func() any {
		r := replies[min(n, len(replies)-1)]
		n++
		return r
	}
}

const portQuestion = "This app appears to be a Node.js service. Pando could not determine which port " +
	"it serves HTTP on. Valid answer: a port number such as 3000."

// TestR261_AppDetectionWaitsReportingEachStage asserts R-261 for issue #80:
// the CLI can wait for detection through the API's long poll, as the console
// and MCP do, says each stage as it is reached, and ends by saying what to do
// next — the questions verbatim (R-105), and exit code 2 because a person has
// to act.
func TestR261_AppDetectionWaitsReportingEachStage(t *testing.T) {
	api := newAPI(t).handle("GET /apps/app_01HQ8/detection", sequence(
		map[string]any{"status": "running", "stage": "fetching"},
		map[string]any{"status": "running", "stage": "trying"},
		map[string]any{
			"status": "needs_answers",
			"detection": map[string]any{"questions": []map[string]any{
				{"key": "port", "prompt": portQuestion},
				{"key": "name", "prompt": "Suggested already."},
			}},
			"unanswered": []string{"port"},
		},
	))

	got := run(t, api, "", "app", "detection", "app_01HQ8", "--wait")

	require.Error(t, got.err)
	require.Equal(t, 2, cli.ExitCodeOf(got.err), "a person has questions to answer")
	require.Contains(t, got.errOut, "Fetching the source...")
	require.Contains(t, got.errOut, "Trying a run of the app...")
	require.Contains(t, got.out, portQuestion, "printed exactly as the server wrote it")
	require.NotContains(t, got.out, "Suggested already.", "the server says which are still open")
	require.Contains(t, got.out, "paste a question into whatever wrote this app")

	for _, c := range api.calls {
		require.Equal(t, "/api/v1/apps/app_01HQ8/detection?wait=30", c.path, "every read is a long poll")
	}
}

// A wait that ends ready exits 0, and one that ends failed or blocked exits 1
// with the reason Pando recorded.
func TestAppDetectionWaitExitsByOutcome(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply map[string]any
		code  int
		says  string
	}{
		{"ready", map[string]any{
			"status":    "ready",
			"detection": map[string]any{"winning_bid": map[string]string{"detector": "Go", "strategy": "buildpack"}},
		}, 0, "Ready: recognized it as Go, built with buildpack."},
		{"failed", map[string]any{
			"status": "failed",
			"detection": map[string]any{"error": map[string]string{
				"code": "STATE_INVALID", "message": "The repository could not be cloned.",
			}},
		}, 1, "The repository could not be cloned."},
		{"blocked", map[string]any{
			"status": "blocked",
			"detection": map[string]any{"blocked": map[string]string{
				"code": "PLAN_PORT_EXHAUSTED", "message": "Every port in this install's range is taken.",
			}},
		}, 1, "Every port in this install's range is taken."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newAPI(t).reply("GET /apps/app_01HQ8/detection", tc.reply)
			got := run(t, api, "", "app", "detection", "app_01HQ8", "--wait")
			if tc.code == 0 {
				require.NoError(t, got.err)
			} else {
				require.Error(t, got.err)
				require.Equal(t, tc.code, cli.ExitCodeOf(got.err))
			}
			require.Contains(t, got.out, tc.says)
		})
	}
}

// Without --wait it answers at once, saying where detection is, for how long,
// and how to wait for it.
func TestAppDetectionShowsARunningDetection(t *testing.T) {
	api := newAPI(t).reply("GET /apps/app_01HQ8/detection", map[string]any{
		"status": "running", "stage": "trying", "elapsed_seconds": 80,
	})

	got := run(t, api, "", "app", "detection", "app_01HQ8")
	require.NoError(t, got.err)
	require.Contains(t, got.out, "Running: trying a run of the app (1m20s so far).")
	require.Contains(t, got.out, "`pando app detection app_01HQ8 --wait`")
	require.True(t, api.sawPath("/apps/app_01HQ8/detection"), "no wait asked for")
}

// `app add` points at the command that says when detection is done, and with
// --wait is that command.
func TestAppAddPointsAtDetectionAndCanWaitForIt(t *testing.T) {
	api := newAPI(t).reply("POST /apps", map[string]any{"id": "app_01HQ8"})
	got := run(t, api, "", "app", "add", "https://github.com/ben/notes.git")
	require.NoError(t, got.err)
	require.Contains(t, got.out, "`pando app detection app_01HQ8 --wait`")
	require.False(t, api.sawPath("/apps/app_01HQ8/detection?wait=30"), "no wait unless asked")

	api = newAPI(t).
		reply("POST /apps", map[string]any{"id": "app_01HQ8"}).
		reply("GET /apps/app_01HQ8/detection", map[string]any{
			"status":    "ready",
			"detection": map[string]any{"winning_bid": map[string]string{"detector": "Go", "strategy": "buildpack"}},
		})
	got = run(t, api, "", "app", "add", "https://github.com/ben/notes.git", "--wait")
	require.NoError(t, got.err)
	require.True(t, api.sawPath("/apps/app_01HQ8/detection?wait=30"))
	require.Contains(t, got.out, "Ready: recognized it as Go")
}

// A draft in the list says why it is still a draft.
func TestAppListSaysWhereADraftsDetectionIs(t *testing.T) {
	api := newAPI(t).reply("GET /apps", map[string]any{
		"apps": []map[string]any{
			{"id": "app_01HQ8", "name": "notes", "state": "draft",
				"detection": map[string]string{"status": "running", "stage": "trying"}},
			{"id": "app_01HQ9", "name": "wiki", "state": "running",
				"detection": map[string]string{"status": "ready"}},
		},
	})

	got := run(t, api, "", "app", "list")
	require.NoError(t, got.err)
	require.Contains(t, got.out, "draft (detection running, trying)")
	require.NotContains(t, got.out, "running (detection", "only a draft needs the reason")
}

func TestExitCodeOf(t *testing.T) {
	require.Equal(t, 1, cli.ExitCodeOf(errors.New("anything")))
	require.Equal(t, 2, cli.ExitCodeOf(&cli.ExitCode{Code: 2, Err: errors.New("questions")}))
}
