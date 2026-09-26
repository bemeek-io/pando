package local_test

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
	localadapter "github.com/bemeek-io/pando/internal/adapter/ai/local"
	"github.com/bemeek-io/pando/internal/adapter/api"
)

// withConfig is withFake with extra configuration.
func withConfig(t *testing.T, extra map[string]any, replies ...string) (*localadapter.Adapter, *fakeServer) {
	t.Helper()
	fake := &fakeServer{t: t, replies: replies}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	cfg := map[string]any{"base_url": server.URL + "/v1", "model": "qwen2.5:7b"}
	for k, v := range extra {
		cfg[k] = v
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	a := localadapter.New()
	require.NoError(t, a.Configure(context.Background(), raw))
	return a, fake
}

// TestInfoDescribesTheLocalAdapter asserts the form requires a model, since
// no model is served by every local server.
func TestInfoDescribesTheLocalAdapter(t *testing.T) {
	a := localadapter.New()
	require.Equal(t, localadapter.Kind, a.Kind())
	require.Equal(t, api.CategoryAI, a.Category())

	info := localadapter.Info()
	require.Equal(t, api.CategoryAI, info.Category)
	require.Equal(t, localadapter.Kind, info.Kind)
	byKey := map[string]api.Field{}
	for _, f := range info.Fields {
		byKey[f.Key] = f
	}
	require.True(t, byKey["model"].Required)
	require.True(t, byKey["api_key"].Credential)
	require.Equal(t, localadapter.DefaultBaseURL, byKey["base_url"].Default)
}

// TestR343_ALocalModelDraftsAccess asserts R-343 for a local model.
func TestR343_ALocalModelDraftsAccess(t *testing.T) {
	a, fake := withFake(t, completion(toolCall("c1", "submit",
		`{"role":{"name":"Release manager","scope":"app","verbs":["app.deploy"]},"reply":"Drafted one role."}`)))

	got, err := a.DraftAccess(context.Background(), api.AccessRequest{
		Description: "People who ship releases",
		Verbs:       []api.VerbInfo{{Name: "app.deploy", Scope: "app"}},
		Model:       "llama3:8b",
	})
	require.NoError(t, err)
	require.Equal(t, "Release manager", got.Role.Name)
	require.Equal(t, "llama3:8b", got.Model, "the assignment's model ran")
	require.Equal(t, "llama3:8b", fake.bodies[0]["model"])
}

// TestR344_ALocalModelDraftsPolicy asserts R-344 for a local model.
func TestR344_ALocalModelDraftsPolicy(t *testing.T) {
	a, _ := withFake(t, completion(toolCall("c1", "submit",
		`{"changes":{"disabled_verbs":["app.exec"]},"reply":"Turned off terminal access."}`)))

	got, err := a.DraftPolicy(context.Background(), api.PolicyRequest{
		Description: "Turn off terminal access", Current: json.RawMessage(`{}`),
	})
	require.NoError(t, err)
	require.JSONEq(t, `["app.exec"]`, string(got.Changes["disabled_verbs"]))
	require.Equal(t, "qwen2.5:7b", got.Model)
}

// TestR345_ALocalModelSearchesTheAuditLog asserts R-345 for a local model.
func TestR345_ALocalModelSearchesTheAuditLog(t *testing.T) {
	a, _ := withFake(t, completion(toolCall("c1", "submit", `{"filter":{"actions":["app.create"]},"note":""}`)))

	got, err := a.SearchAudit(context.Background(), api.AuditSearchRequest{
		Question: "apps added?", Now: time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"app.create"}, got.Filter.Actions)
}

// TestR338_ALocalModelAnswersQuestions asserts R-338 for a local model.
func TestR338_ALocalModelAnswersQuestions(t *testing.T) {
	a, _ := withFake(t, completion(toolCall("c1", "submit_findings",
		`{"amendments":[{"kind":"answer_question","key":"port","value":"3000",`+
			`"reason":"server.js listens on 3000.","evidence":["server.js"]}]}`)))

	req := aitest.Request()
	req.Questions = []api.Question{{Key: "port", Kind: api.QuestionPort, Prompt: "Which port?"}}
	got, err := a.AnswerQuestions(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, got.Amendments, 1)
	require.Equal(t, api.AmendAnswerQuestion, got.Amendments[0].Kind)
}

// TestR336_ALocalModelRevisesAndReplies asserts R-336 for a local model: a
// revision says something back to the person who asked.
func TestR336_ALocalModelRevisesAndReplies(t *testing.T) {
	a, _ := withFake(t, completion(toolCall("c1", "submit_findings",
		`{"amendments":[],"reply":"Nothing in the repository mentions Redis."}`)))

	req := aitest.Request()
	req.Instruction = "It needs Redis."
	got, err := a.RevisePlan(context.Background(), req)
	require.NoError(t, err)
	require.Contains(t, got.Reply, "Redis")
}

// TestR336_ALocalModelRefusesWhenItCannotScreen asserts R-336: not
// configured, set not to screen, or no repository, it does not pretend.
func TestR336_ALocalModelRefusesWhenItCannotScreen(t *testing.T) {
	_, err := localadapter.New().RepairPlan(context.Background(), aitest.Request())
	require.ErrorContains(t, err, "not configured")
	_, err = localadapter.New().DraftPolicy(context.Background(), api.PolicyRequest{})
	require.ErrorContains(t, err, "not configured")
	require.ErrorContains(t, localadapter.New().HealthCheck(context.Background()), "not configured")

	off, _ := withConfig(t, map[string]any{"screen_plans": false})
	caps, err := off.Capabilities(context.Background())
	require.NoError(t, err)
	require.False(t, caps.Does(api.AIFunctionRepairPlan))
	require.True(t, caps.Does(api.AIFunctionAnswerReference), "the administrative functions stay on")
	_, err = off.RepairPlan(context.Background(), aitest.Request())
	require.ErrorContains(t, err, "set not to assist detection")

	a, _ := withFake(t)
	req := aitest.Request()
	req.Source = nil
	_, err = a.RepairPlan(context.Background(), req)
	require.ErrorContains(t, err, "no readable copy of the repository")
}

// A server error, and a completion with no choice in it, each fail the call.
func TestALocalServerThatFailsFailsTheCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After-Ms", "1")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"message":"model crashed"}}`)
	}))
	t.Cleanup(server.Close)
	a := localadapter.New()
	require.NoError(t, a.Configure(context.Background(),
		json.RawMessage(`{"base_url":"`+server.URL+`/v1","model":"qwen2.5:7b"}`)))
	_, err := a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{Question: "x"})
	require.ErrorContains(t, err, "local:")
	require.ErrorContains(t, err, "500")

	a, _ = withFake(t, `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"qwen2.5:7b","choices":[]}`)
	_, err = a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{Question: "x"})
	require.ErrorContains(t, err, "returned no answer")
}

// A model that answers in prose twice is a failure, not an empty answer.
func TestALocalModelThatNeverAnswersIsAFailure(t *testing.T) {
	a, fake := withFake(t, completion(reply("Sure.")), completion(reply("Sure.")))
	_, err := a.DraftPolicy(context.Background(), api.PolicyRequest{Description: "x"})
	require.ErrorContains(t, err, "without submitting")
	msgs := fake.bodies[1]["messages"].([]any)
	require.Contains(t, msgs[len(msgs)-1].(map[string]any)["content"], "Submit your answer now",
		"it was asked once more before giving up")
}

// A call to a read tool written out as text is run like a real call, and its
// result goes back to the model as a message; malformed findings are handed
// back to be fixed rather than failing the screening.
func TestALocalModelsWrittenOutReadCallIsRun(t *testing.T) {
	findings := `{"amendments":[],"notes":["It binds loopback."]}`
	a, fake := withFake(t,
		completion(reply(`{"name":"list_files","arguments":"{\"pattern\":\"*.js\"}"}`)),
		completion(toolCall("c1", "submit_findings", `{"amendments": 5}`)),
		completion(reply(findings)))

	got, err := a.RepairPlan(context.Background(), aitest.Request())
	require.NoError(t, err)
	require.Equal(t, []string{"It binds loopback."}, got.Notes)

	msgs := fake.bodies[1]["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	require.Equal(t, "user", last["role"])
	require.Contains(t, last["content"], "server.js", "the listing went back to the model")

	msgs = fake.bodies[2]["messages"].([]any)
	require.Contains(t, msgs[len(msgs)-1].(map[string]any)["content"], "That did not work")
}

// A call written out as text naming a tool the task does not have is answered
// as an error, and the conversation continues.
func TestALocalModelsUnknownToolIsHandedBack(t *testing.T) {
	a, fake := withFake(t,
		completion(reply(`{"name":"answer","arguments":{"summary":"x"}}`)),
		completion(toolCall("c1", "submit", `{"summary":"Nothing matched."}`)))

	got, err := a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{Question: "x"})
	require.NoError(t, err)
	require.Equal(t, "Nothing matched.", got.Summary)
	msgs := fake.bodies[1]["messages"].([]any)
	require.Contains(t, msgs[len(msgs)-1].(map[string]any)["content"], "Answer with submit")
}

// TestR190_ALocalServerIsSentOnlyItsOwnKey asserts R-190: a stored credential,
// or one read from the variable named for it, is what the server receives.
func TestR190_ALocalServerIsSentOnlyItsOwnKey(t *testing.T) {
	answer := completion(toolCall("c1", "submit", `{"summary":"Nothing matched."}`))

	a, fake := withConfig(t, map[string]any{"credentials": map[string]string{"api_key": "k"}}, answer)
	_, err := a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{Question: "x"})
	require.NoError(t, err)
	require.Equal(t, []string{"Bearer k"}, fake.auth)

	t.Setenv("PANDO_TEST_LOCAL_KEY", "from-env")
	a, fake = withConfig(t, map[string]any{"api_key_env": "PANDO_TEST_LOCAL_KEY"}, answer)
	_, err = a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{Question: "x"})
	require.NoError(t, err)
	require.Equal(t, []string{"Bearer from-env"}, fake.auth)

	a, fake = withFake(t, answer)
	_, err = a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{Question: "x"})
	require.NoError(t, err)
	require.Equal(t, []string{"Bearer local"}, fake.auth, "no key configured sends a placeholder, never a real one")
}

// TestR190_AnInlineKeyIsRefused asserts R-190: a key in the stored, unencrypted
// configuration is refused rather than used.
func TestR190_AnInlineKeyIsRefused(t *testing.T) {
	err := localadapter.New().Configure(context.Background(),
		json.RawMessage(`{"model":"qwen2.5:7b","api_key":"plain"}`))
	require.ErrorContains(t, err, "unencrypted")

	err = localadapter.New().Configure(context.Background(), json.RawMessage(`[1]`))
	require.ErrorContains(t, err, "local: reading configuration")
	err = localadapter.New().Configure(context.Background(), json.RawMessage(`{"model":5}`))
	require.ErrorContains(t, err, "local: reading configuration")
}

// A server that is not there says so, naming where it looked.
func TestALocalServerThatIsDownFailsItsHealthCheck(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL + "/v1"
	server.Close()

	a := localadapter.New()
	require.NoError(t, a.Configure(context.Background(),
		json.RawMessage(`{"base_url":"`+url+`","model":"qwen2.5:7b","timeout_seconds":5}`)))
	err := a.HealthCheck(context.Background())
	require.ErrorContains(t, err, "did not answer")
	require.ErrorContains(t, err, url)
}
