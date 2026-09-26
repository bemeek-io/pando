package anthropic_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// TestR259_AnAssignedModelIsTheModelThatRuns asserts R-259: a call runs on
// the model its assignment names, and reports it.
func TestR259_AnAssignedModelIsTheModelThatRuns(t *testing.T) {
	a, fake := withFake(t, message("tool_use",
		toolUse("t1", "submit", `{"answer":"Use POST /api/v1/groups.","cites":["POST /api/v1/groups"],"covered":true}`)))

	got, err := a.AnswerReference(context.Background(), api.ReferenceRequest{
		Question: "How can I make a group?", Reference: "POST /api/v1/groups creates a group.", Model: "claude-haiku-4-5",
	})
	require.NoError(t, err)
	require.Equal(t, "claude-haiku-4-5", got.Model)
	require.True(t, got.Covered)
	require.Equal(t, []string{"POST /api/v1/groups"}, got.Cites)

	require.Len(t, fake.bodies, 1)
	require.Equal(t, "claude-haiku-4-5", fake.bodies[0]["model"])
	// Asked, never forced: current models refuse forced tool use with a 400.
	choice := fake.bodies[0]["tool_choice"].(map[string]any)
	require.Equal(t, "auto", choice["type"])
	require.Equal(t, true, choice["disable_parallel_tool_use"], "one answer, through the tool")
}

// A model that answers in prose is asked once more to use the tool, in the
// same conversation, and its second answer is read.
func TestAnAnswerInProseIsAskedForAgainThroughTheTool(t *testing.T) {
	a, fake := withFake(t,
		message("end_turn", `{"type":"text","text":"Use POST /api/v1/groups."}`),
		message("tool_use", toolUse("t1", "submit", `{"answer":"Use POST /api/v1/groups.","cites":[],"covered":true}`)))

	got, err := a.AnswerReference(context.Background(), api.ReferenceRequest{Question: "groups?", Reference: "POST /api/v1/groups"})
	require.NoError(t, err)
	require.Equal(t, "Use POST /api/v1/groups.", got.Answer)
	require.Len(t, fake.bodies, 2)
	require.Len(t, fake.bodies[1]["messages"].([]any), 3, "the first answer and the nudge were appended")
}

// TestR343_AccessDraftIsReadFromTheSubmittedTool asserts R-343 at the
// adapter: the draft is what the model submitted, and the verb catalog is
// the schema's only choice of verb.
func TestR343_AccessDraftIsReadFromTheSubmittedTool(t *testing.T) {
	a, fake := withFake(t, message("tool_use", toolUse("t1", "submit",
		`{"role":{"name":"Release manager","scope":"app","verbs":["app.deploy"]},"reply":"Drafted one role."}`)))

	got, err := a.DraftAccess(context.Background(), api.AccessRequest{
		Description: "People who ship releases",
		Verbs:       []api.VerbInfo{{Name: "app.deploy", Scope: "app"}, {Name: "app.view", Scope: "app"}},
	})
	require.NoError(t, err)
	require.Equal(t, "Release manager", got.Role.Name)
	require.Equal(t, []string{"app.deploy"}, got.Role.Verbs)
	require.Equal(t, "claude-opus-5-5", got.Model, "the adapter's own model when the assignment names none")

	tools := fake.bodies[0]["tools"].([]any)
	schema := tools[0].(map[string]any)["input_schema"].(map[string]any)
	role := schema["properties"].(map[string]any)["role"].(map[string]any)
	verbs := role["properties"].(map[string]any)["verbs"].(map[string]any)["items"].(map[string]any)
	require.Equal(t, []any{"app.deploy", "app.view"}, verbs["enum"])
}

// TestR345_AuditSearchIsGivenTheTimeAndReturnsAFilter asserts R-345 at the
// adapter: relative times are resolved against the time core supplies.
func TestR345_AuditSearchIsGivenTheTimeAndReturnsAFilter(t *testing.T) {
	a, fake := withFake(t, message("tool_use", toolUse("t1", "submit",
		`{"filter":{"actions":["app.create"],"since":"2026-08-26T00:00:00Z"},"note":""}`)))

	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	got, err := a.SearchAudit(context.Background(), api.AuditSearchRequest{Question: "apps added last month", Now: now})
	require.NoError(t, err)
	require.Equal(t, []string{"app.create"}, got.Filter.Actions)
	require.NotNil(t, got.Filter.Since)
	require.Equal(t, time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC), got.Filter.Since.UTC())

	sent := fake.bodies[0]["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	require.Contains(t, sent, "2026-09-26T12:00:00Z")
}

func TestAnAnswerWithoutTheToolIsAnError(t *testing.T) {
	a, _ := withFake(t,
		message("end_turn", `{"type":"text","text":"Sure!"}`),
		message("end_turn", `{"type":"text","text":"Sure!"}`))
	_, err := a.DraftPolicy(context.Background(), api.PolicyRequest{Description: "x"})
	require.ErrorContains(t, err, "without submitting")
}
