package local_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/ai/aikit/aitest"
	localadapter "github.com/bemeek-io/pando/internal/adapter/ai/local"
	"github.com/bemeek-io/pando/internal/adapter/api"
)

// fakeServer stands in for an OpenAI-compatible server such as Ollama.
type fakeServer struct {
	t       *testing.T
	mu      sync.Mutex
	replies []string
	bodies  []map[string]any
	auth    []string
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/v1/models" {
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"qwen2.5:7b","object":"model","created":1,"owned_by":"library"}]}`)
		return
	}
	require.Equal(f.t, "/v1/chat/completions", r.URL.Path)
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	var body map[string]any
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
	f.bodies = append(f.bodies, body)
	if len(f.replies) == 0 {
		f.t.Fatalf("unexpected request %d", len(f.bodies))
	}
	reply := f.replies[0]
	f.replies = f.replies[1:]
	_, _ = io.WriteString(w, reply)
}

func completion(message string) string {
	return `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"qwen2.5:7b",` +
		`"choices":[{"index":0,"finish_reason":"stop","message":` + message + `}]}`
}

func toolCall(id, name, args string) string {
	quoted, _ := json.Marshal(args)
	return `{"role":"assistant","content":"","tool_calls":[{"id":"` + id + `","type":"function",` +
		`"function":{"name":"` + name + `","arguments":` + string(quoted) + `}}]}`
}

func reply(text string) string {
	quoted, _ := json.Marshal(text)
	return `{"role":"assistant","content":` + string(quoted) + `}`
}

func withFake(t *testing.T, replies ...string) (*localadapter.Adapter, *fakeServer) {
	t.Helper()
	fake := &fakeServer{t: t, replies: replies}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	a := localadapter.New()
	cfg, _ := json.Marshal(map[string]any{"base_url": server.URL + "/v1", "model": "qwen2.5:7b"})
	require.NoError(t, a.Configure(context.Background(), cfg))
	return a, fake
}

// TestR259_TheLocalAdapterPerformsEveryFunction asserts R-259 for a local
// model, and that its health check finds the model on the server.
func TestR259_TheLocalAdapterPerformsEveryFunction(t *testing.T) {
	a, _ := withFake(t)
	caps, err := a.Capabilities(context.Background())
	require.NoError(t, err)
	require.True(t, caps.ChoosesModel)
	require.Equal(t, "qwen2.5:7b", caps.Model)
	for _, fn := range api.AIFunctions() {
		require.True(t, caps.Does(fn), "%s", fn)
	}
	require.NoError(t, a.HealthCheck(context.Background()))
}

func TestTheLocalAdapterNeedsAModelAndSaysWhichIsMissing(t *testing.T) {
	err := localadapter.New().Configure(context.Background(), json.RawMessage(`{}`))
	require.ErrorContains(t, err, "no model is set")

	fake := &fakeServer{t: t}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	a := localadapter.New()
	require.NoError(t, a.Configure(context.Background(), json.RawMessage(`{"base_url":"`+server.URL+`/v1","model":"llama3:70b"}`)))
	err = a.HealthCheck(context.Background())
	require.ErrorContains(t, err, "does not serve llama3:70b")
	require.ErrorContains(t, err, "qwen2.5:7b", "and names what it does serve")
}

// TestR190_ALocalServerIsNeverSentTheOpenAIKey asserts R-190: a key meant
// for OpenAI in Pando's environment does not go to somebody's local server.
func TestR190_ALocalServerIsNeverSentTheOpenAIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-real-openai-key")
	a, fake := withFake(t, completion(toolCall("c1", "submit", `{"summary":"Nothing matched."}`)))
	_, err := a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{Question: "x"})
	require.NoError(t, err)
	require.NotContains(t, strings.Join(fake.auth, " "), "sk-real-openai-key")
}

// TestR336_ALocalModelRepairsThroughTheBudgetedReader asserts R-336 and
// R-337 for a local model: it reads through tools and submits findings.
func TestR336_ALocalModelRepairsThroughTheBudgetedReader(t *testing.T) {
	findings := `{"amendments":[{"kind":"set_env","key":"HOST","value":"0.0.0.0",` +
		`"reason":"server.js binds 127.0.0.1.","evidence":["server.js"]}]}`
	a, fake := withFake(t,
		completion(toolCall("c1", "read_file", `{"path":"server.js"}`)),
		completion(toolCall("c2", "submit_findings", findings)))

	got, err := a.RepairPlan(context.Background(), aitest.Request())
	require.NoError(t, err)
	require.Len(t, got.Amendments, 1)
	require.Equal(t, []string{"server.js"}, got.FilesRead)
	require.Equal(t, "qwen2.5:7b", got.Model)

	msgs := fake.bodies[1]["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	require.Equal(t, "tool", last["role"])
	require.Equal(t, "c1", last["tool_call_id"])
	require.Contains(t, last["content"], "127.0.0.1")
}

// A small model that answers with JSON in its reply, rather than a tool call,
// is read — in a code fence, and as a call written out as text.
func TestALocalModelsJSONReplyIsReadAsItsAnswer(t *testing.T) {
	a, _ := withFake(t, completion(reply("```json\n{\"answer\":\"Use POST /api/v1/groups.\",\"cites\":[],\"covered\":true}\n```")))
	got, err := a.AnswerReference(context.Background(), api.ReferenceRequest{Question: "groups?"})
	require.NoError(t, err)
	require.Equal(t, "Use POST /api/v1/groups.", got.Answer)

	a, _ = withFake(t, completion(reply(`{"name":"submit","arguments":{"summary":"Two apps were created."}}`)))
	sum, err := a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{Question: "apps?"})
	require.NoError(t, err)
	require.Equal(t, "Two apps were created.", sum.Summary)
}

// An answer that does not decode is handed back to be fixed, not fatal.
func TestALocalModelsUnreadableAnswerIsSentBackToFix(t *testing.T) {
	a, fake := withFake(t,
		completion(toolCall("c1", "submit", `{"summary": 5}`)),
		completion(toolCall("c2", "submit", `{"summary":"Nothing matched."}`)))
	got, err := a.SummarizeAudit(context.Background(), api.AuditSummaryRequest{Question: "x"})
	require.NoError(t, err)
	require.Equal(t, "Nothing matched.", got.Summary)
	msgs := fake.bodies[1]["messages"].([]any)
	require.Contains(t, msgs[len(msgs)-1].(map[string]any)["content"], "could not be read")
}

// TestLiveLocalModel runs one of each administrative function against a real
// server, when PANDO_TEST_LOCAL_AI names one (http://localhost:11434/v1) and
// PANDO_TEST_LOCAL_MODEL a model it serves. Skipped otherwise, and in CI.
func TestLiveLocalModel(t *testing.T) {
	url := os.Getenv("PANDO_TEST_LOCAL_AI")
	model := os.Getenv("PANDO_TEST_LOCAL_MODEL")
	if url == "" || model == "" {
		t.Skip("set PANDO_TEST_LOCAL_AI and PANDO_TEST_LOCAL_MODEL to run against a local server")
	}
	a := localadapter.New()
	cfg, _ := json.Marshal(map[string]any{"base_url": url, "model": model})
	require.NoError(t, a.Configure(context.Background(), cfg))
	require.NoError(t, a.HealthCheck(context.Background()))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	search, err := a.SearchAudit(ctx, api.AuditSearchRequest{
		Question: "Which apps did admin create in the last month?",
		Now:      time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC),
		People:   []api.PersonInfo{{ID: "usr_01ADMIN", Username: "admin"}},
		Actions:  []string{"app.create", "app.delete", "session.create"},
	})
	require.NoError(t, err)
	t.Logf("audit filter: %+v note=%q", search.Filter, search.Note)

	policy, err := a.DraftPolicy(ctx, api.PolicyRequest{
		Description: "Turn off terminal access",
		Current:     json.RawMessage(`{}`),
		Fields:      []api.PolicyField{{Key: "disabled_verbs", Type: "list", Meaning: "Permissions nobody may use. app.exec is terminal access."}},
		Verbs:       []api.VerbInfo{{Name: "app.exec", Scope: "app"}, {Name: "app.delete", Scope: "app"}},
	})
	require.NoError(t, err)
	t.Logf("policy: changes=%s reply=%q", policy.Changes, policy.Reply)

	access, err := a.DraftAccess(ctx, api.AccessRequest{
		Description: "Release managers can deploy and restart apps",
		Verbs:       []api.VerbInfo{{Name: "app.deploy", Scope: "app"}, {Name: "app.restart", Scope: "app"}, {Name: "app.view", Scope: "app"}},
	})
	require.NoError(t, err)
	t.Logf("access: role=%+v group=%+v reply=%q", access.Role, access.Group, access.Reply)

	answer, err := a.AnswerReference(ctx, api.ReferenceRequest{
		Question:  "How can I create a group?",
		Reference: "`POST /api/v1/groups` creates a group. Body: name, members.",
	})
	require.NoError(t, err)
	t.Logf("reference: %q cites=%v", answer.Answer, answer.Cites)

	repair, err := a.RepairPlan(ctx, aitest.Request())
	require.NoError(t, err)
	t.Logf("repair: %d amendments, read %v", len(repair.Amendments), repair.FilesRead)
}
