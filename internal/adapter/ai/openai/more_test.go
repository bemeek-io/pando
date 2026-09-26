package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/ai/aikit/aitest"
	openaiadapter "github.com/bemeek-io/pando/internal/adapter/ai/openai"
	"github.com/bemeek-io/pando/internal/adapter/api"
)

// configure returns an adapter configured with cfg against a server that
// answers every request with status and body.
func configure(t *testing.T, status int, body string, cfg map[string]any) *openaiadapter.Adapter {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Retried by the SDK; told to retry at once so the test does not wait.
		w.Header().Set("Retry-After-Ms", "1")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	cfg["base_url"] = server.URL
	if _, ok := cfg["credentials"]; !ok {
		cfg["credentials"] = map[string]string{"api_key": "sk-test"}
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	a := openaiadapter.New()
	require.NoError(t, a.Configure(context.Background(), raw))
	return a
}

// TestInfoDescribesTheOpenAIAdapter asserts its identity, and that the form
// asks for the key as a credential.
func TestInfoDescribesTheOpenAIAdapter(t *testing.T) {
	a := openaiadapter.New()
	require.Equal(t, openaiadapter.Kind, a.Kind())
	require.Equal(t, api.CategoryAI, a.Category())

	info := openaiadapter.Info()
	require.Equal(t, api.CategoryAI, info.Category)
	require.Equal(t, openaiadapter.Kind, info.Kind)
	require.Equal(t, "ai_", info.IDPrefix)
	byKey := map[string]api.Field{}
	for _, f := range info.Fields {
		byKey[f.Key] = f
	}
	require.True(t, byKey["api_key"].Credential)
	require.Equal(t, openaiadapter.DefaultModel, byKey["model"].Default)
}

// TestR343_OpenAIDraftsAccess asserts R-343 for OpenAI.
func TestR343_OpenAIDraftsAccess(t *testing.T) {
	a, fake := withFake(t, response("resp_1", call("c1", "submit",
		`{"role":{"name":"Release manager","scope":"app","verbs":["app.deploy"]},"reply":"Drafted one role."}`)))

	got, err := a.DraftAccess(context.Background(), api.AccessRequest{
		Description: "People who ship releases",
		Verbs:       []api.VerbInfo{{Name: "app.deploy", Scope: "app"}},
		Model:       "gpt-5.4-mini",
	})
	require.NoError(t, err)
	require.Equal(t, "Release manager", got.Role.Name)
	require.Equal(t, []string{"app.deploy"}, got.Role.Verbs)
	require.Equal(t, "gpt-5.4-mini", got.Model)
	require.Equal(t, "gpt-5.4-mini", fake.bodies[0]["model"])
}

// TestR344_OpenAIDraftsPolicy asserts R-344 for OpenAI.
func TestR344_OpenAIDraftsPolicy(t *testing.T) {
	a, _ := withFake(t, response("resp_1", call("c1", "submit",
		`{"changes":{"disabled_verbs":["app.exec"]},"reply":"Turned off terminal access."}`)))

	got, err := a.DraftPolicy(context.Background(), api.PolicyRequest{
		Description: "Turn off terminal access", Current: json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	require.JSONEq(t, `["app.exec"]`, string(got.Changes["disabled_verbs"]))
	require.Equal(t, openaiadapter.DefaultModel, got.Model)
}

// TestR345_OpenAISearchesAndSummarizesTheAuditLog asserts R-345 for OpenAI:
// a question becomes a filter, and the records a summary.
func TestR345_OpenAISearchesAndSummarizesTheAuditLog(t *testing.T) {
	a, fake := withFake(t,
		response("resp_1", call("c1", "submit", `{"filter":{"actions":["app.create"]},"note":""}`)),
		response("resp_2", call("c2", "submit", `{"summary":"Nothing matched."}`)))

	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	search, err := a.SearchAudit(context.Background(), api.AuditSearchRequest{Question: "apps added?", Now: now})
	require.NoError(t, err)
	require.Equal(t, []string{"app.create"}, search.Filter.Actions)
	require.Contains(t, fake.bodies[0]["input"], "2026-09-26T12:00:00Z")

	sum, err := a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{Question: "apps added?"})
	require.NoError(t, err)
	require.Equal(t, "Nothing matched.", sum.Summary)
	require.Equal(t, openaiadapter.DefaultModel, sum.Model)
}

// TestR338_OpenAIAnswersQuestions asserts R-338 for OpenAI: the answer comes
// back as an answer_question amendment.
func TestR338_OpenAIAnswersQuestions(t *testing.T) {
	a, fake := withFake(t, response("resp_1", call("c1", "submit_findings",
		`{"amendments":[{"kind":"answer_question","key":"port","value":"3000",`+
			`"reason":"server.js listens on 3000.","evidence":["server.js"]}]}`)))

	req := aitest.Request()
	req.Questions = []api.Question{{Key: "port", Kind: api.QuestionPort, Prompt: "Which port?"}}
	got, err := a.AnswerQuestions(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, got.Amendments, 1)
	require.Equal(t, api.AmendAnswerQuestion, got.Amendments[0].Kind)
	require.Contains(t, fake.bodies[0]["instructions"], "answering questions")
}

// TestR336_OpenAIRevisesAndReplies asserts R-336 for OpenAI: a revision says
// something back to the person who asked.
func TestR336_OpenAIRevisesAndReplies(t *testing.T) {
	a, fake := withFake(t, response("resp_1", call("c1", "submit_findings",
		`{"amendments":[],"reply":"Nothing in the repository mentions Redis, so the plan is unchanged."}`)))

	req := aitest.Request()
	req.Instruction = "It needs Redis."
	got, err := a.RevisePlan(context.Background(), req)
	require.NoError(t, err)
	require.Empty(t, got.Amendments)
	require.Contains(t, got.Reply, "Redis")
	require.Contains(t, fake.bodies[0]["input"], "> It needs Redis.")
}

// TestR336_OpenAIScreeningRefusesWhenItCannot asserts R-336: an adapter that
// is not configured, is set not to screen, or has no repository does not
// pretend to screen.
func TestR336_OpenAIScreeningRefusesWhenItCannot(t *testing.T) {
	_, err := openaiadapter.New().RepairPlan(context.Background(), aitest.Request())
	require.ErrorContains(t, err, "not configured")
	_, err = openaiadapter.New().SummarizeAudit(context.Background(), api.AuditSummaryRequest{})
	require.ErrorContains(t, err, "not configured")
	require.ErrorContains(t, openaiadapter.New().HealthCheck(context.Background()), "not configured")

	off := configure(t, http.StatusOK, `{}`, map[string]any{"screen_plans": false})
	caps, err := off.Capabilities(context.Background())
	require.NoError(t, err)
	require.False(t, caps.Does(api.AIFunctionRepairPlan))
	require.True(t, caps.Does(api.AIFunctionDraftAccess), "the administrative functions stay on")
	_, err = off.RepairPlan(context.Background(), aitest.Request())
	require.ErrorContains(t, err, "set not to assist detection")

	a, _ := withFake(t)
	req := aitest.Request()
	req.Source = nil
	_, err = a.RepairPlan(context.Background(), req)
	require.ErrorContains(t, err, "no readable copy of the repository")
}

// A call to a tool the task does not have is answered as an error the model
// can see, and the conversation continues to the real answer.
func TestOpenAIAWrongToolNameIsHandedBack(t *testing.T) {
	a, fake := withFake(t,
		response("resp_1", call("c1", "answer", `{"summary":"x"}`)),
		response("resp_2", call("c2", "submit", `{"summary":"Nothing matched."}`)))

	got, err := a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{Question: "x"})
	require.NoError(t, err)
	require.Equal(t, "Nothing matched.", got.Summary)
	out := fake.bodies[1]["input"].([]any)[0].(map[string]any)["output"].(string)
	require.Contains(t, out, "That did not work")
	require.Contains(t, out, "Answer with submit")
}

// An answer that does not decode fails the call rather than returning an
// empty draft as if it were one.
func TestOpenAIAnUnreadableAnswerIsAnError(t *testing.T) {
	a, _ := withFake(t, response("resp_1", call("c1", "submit", `{"summary": 5}`)))
	_, err := a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{Question: "x"})
	require.ErrorContains(t, err, "could not be read")

	a, _ = withFake(t, response("resp_1", call("c1", "submit_findings", `{"amendments": 5}`)))
	_, err = a.RepairPlan(context.Background(), aitest.Request())
	require.ErrorContains(t, err, "could not be read")
}

// A server error fails the call.
func TestOpenAIAServerErrorIsAnError(t *testing.T) {
	a := configure(t, http.StatusInternalServerError, `{"error":{"message":"overloaded","type":"server_error"}}`,
		map[string]any{})
	_, err := a.DraftPolicy(context.Background(), api.PolicyRequest{Description: "x"})
	require.ErrorContains(t, err, "openai:")
	require.ErrorContains(t, err, "500")
}

// A model the API does not serve fails the health check, as it would a call.
func TestOpenAIHealthCheckFailsForAnUnknownModel(t *testing.T) {
	a := configure(t, http.StatusNotFound, `{"error":{"message":"The model does not exist","type":"invalid_request_error"}}`,
		map[string]any{"model": "gpt-nope"})
	err := a.HealthCheck(context.Background())
	require.ErrorContains(t, err, "openai:")
	require.ErrorContains(t, err, "404")
}

// TestR190_OpenAIReadsItsKeyFromTheNamedVariable asserts R-190: a key can be
// kept out of storage altogether by naming the variable that holds it, and
// the timeout and base URL options are honored alongside.
func TestR190_OpenAIReadsItsKeyFromTheNamedVariable(t *testing.T) {
	t.Setenv("PANDO_TEST_OPENAI_KEY", "sk-test")
	fake := &fakeAPI{t: t, replies: []string{response("resp_1", call("c1", "submit", `{"summary":"Nothing matched."}`))}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	a := openaiadapter.New()
	cfg, err := json.Marshal(map[string]any{
		"api_key_env":     "PANDO_TEST_OPENAI_KEY",
		"base_url":        server.URL,
		"timeout_seconds": 30,
		"max_files":       5,
		"max_bytes":       4096,
	})
	require.NoError(t, err)
	require.NoError(t, a.Configure(context.Background(), cfg))

	caps, err := a.Capabilities(context.Background())
	require.NoError(t, err)
	require.Equal(t, 5, caps.MaxFiles)
	require.Equal(t, int64(4096), caps.MaxBytes)

	got, err := a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{Question: "x"})
	require.NoError(t, err, "the fake requires Bearer sk-test, which came from the variable")
	require.Equal(t, "Nothing matched.", got.Summary)

	t.Setenv("PANDO_TEST_OPENAI_KEY", "")
	err = openaiadapter.New().Configure(context.Background(), json.RawMessage(`{"api_key_env":"PANDO_TEST_OPENAI_KEY"}`))
	require.ErrorContains(t, err, "no API key", "a named variable that is empty is no key")
}

// Configuration that is not JSON is refused with the adapter's name on it.
func TestOpenAIUnreadableConfigurationIsRefused(t *testing.T) {
	err := openaiadapter.New().Configure(context.Background(), json.RawMessage(`[1]`))
	require.ErrorContains(t, err, "openai: reading configuration")
	err = openaiadapter.New().Configure(context.Background(), json.RawMessage(`{"model":5}`))
	require.ErrorContains(t, err, "openai: reading configuration")
}
