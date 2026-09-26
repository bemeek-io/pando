package anthropic_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	anthropicadapter "github.com/bemeek-io/pando/internal/adapter/ai/anthropic"
	"github.com/bemeek-io/pando/internal/adapter/api"
)

// TestInfoDescribesTheAnthropicAdapter asserts the form that configures one
// asks for the key as a credential, never as plain configuration.
func TestInfoDescribesTheAnthropicAdapter(t *testing.T) {
	info := anthropicadapter.Info()
	require.Equal(t, api.CategoryAI, info.Category)
	require.Equal(t, anthropicadapter.Kind, info.Kind)
	require.Equal(t, "ai_", info.IDPrefix)

	byKey := map[string]api.Field{}
	for _, f := range info.Fields {
		byKey[f.Key] = f
	}
	require.True(t, byKey["api_key"].Credential, "the key is stored encrypted")
	require.Equal(t, anthropicadapter.DefaultModel, byKey["model"].Default)
	require.Contains(t, byKey, "base_url")
	require.Contains(t, byKey, "api_key_env")
}

// TestR345_AnAuditSummaryIsReadFromTheSubmittedTool asserts R-345 at the
// adapter: the summary is what the model submitted, over the records sent.
func TestR345_AnAuditSummaryIsReadFromTheSubmittedTool(t *testing.T) {
	a, fake := withFake(t, message("tool_use",
		toolUse("t1", "submit", `{"summary":"Ben deployed billing twice this week."}`)))

	got, err := a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{
		Question: "Who deployed billing?",
		Records:  []api.AuditRecordView{{Action: "app.deploy", PrincipalID: "usr_1", AppID: "app_1"}},
	})
	require.NoError(t, err)
	require.Equal(t, "Ben deployed billing twice this week.", got.Summary)
	require.Equal(t, anthropicadapter.DefaultModel, got.Model)

	sent := fake.bodies[0]["messages"].([]any)[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	require.Contains(t, sent, "app.deploy", "the records reached the model")
}

// TestR344_APolicyDraftIsReadFromTheSubmittedTool asserts R-344 at the
// adapter: the changes are what the model submitted, keyed by field.
func TestR344_APolicyDraftIsReadFromTheSubmittedTool(t *testing.T) {
	a, fake := withFake(t, message("tool_use", toolUse("t1", "submit",
		`{"changes":{"disabled_verbs":["app.exec"]},"reply":"Turned off terminal access for the whole installation."}`)))

	got, err := a.DraftPolicy(context.Background(), api.PolicyRequest{
		Description: "Turn off terminal access",
		Current:     json.RawMessage(`{}`),
		Fields:      []api.PolicyField{{Key: "disabled_verbs", Type: "list"}},
		Model:       "claude-haiku-4-5",
	})
	require.NoError(t, err)
	require.JSONEq(t, `["app.exec"]`, string(got.Changes["disabled_verbs"]))
	require.Contains(t, got.Reply, "terminal access")
	require.Equal(t, "claude-haiku-4-5", got.Model)

	require.Equal(t, "claude-haiku-4-5", fake.bodies[0]["model"])
	system := fake.bodies[0]["system"].([]any)[0].(map[string]any)["text"].(string)
	require.Contains(t, system, "host policy")
}
