package openai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/ai/aikit/aitest"
	openaiadapter "github.com/bemeek-io/pando/internal/adapter/ai/openai"
	"github.com/bemeek-io/pando/internal/adapter/api"
)

// fakeAPI stands in for the Responses API, replaying one scripted reply per
// request and recording what Pando sent. The conversation loop is Pando's own
// code, so it is tested here rather than trusted to a network call nobody
// runs in CI.
type fakeAPI struct {
	t       *testing.T
	mu      sync.Mutex
	replies []string
	bodies  []map[string]any
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/models/") {
		_, _ = io.WriteString(w, `{"id":"gpt-5.5","object":"model","created":1,"owned_by":"openai"}`)
		return
	}
	require.Equal(f.t, "/responses", r.URL.Path)
	require.Equal(f.t, "Bearer sk-test", r.Header.Get("Authorization"), "the credential reaches the API")
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

func response(id string, items ...string) string {
	return `{"id":"` + id + `","object":"response","created_at":1,"status":"completed","model":"gpt-5.5",` +
		`"output":[` + strings.Join(items, ",") + `],"parallel_tool_calls":true,"tool_choice":"auto","tools":[]}`
}

func call(id, name, args string) string {
	quoted, _ := json.Marshal(args)
	return `{"type":"function_call","id":"fc_` + id + `","call_id":"` + id + `","name":"` + name +
		`","arguments":` + string(quoted) + `,"status":"completed"}`
}

func text(s string) string {
	return `{"type":"message","id":"msg_1","role":"assistant","status":"completed",` +
		`"content":[{"type":"output_text","text":"` + s + `","annotations":[]}]}`
}

func withFake(t *testing.T, replies ...string) (*openaiadapter.Adapter, *fakeAPI) {
	t.Helper()
	fake := &fakeAPI{t: t, replies: replies}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	a := openaiadapter.New()
	cfg, err := json.Marshal(map[string]any{
		"credentials": map[string]string{"api_key": "sk-test"},
		"base_url":    server.URL,
	})
	require.NoError(t, err)
	require.NoError(t, a.Configure(context.Background(), cfg))
	return a, fake
}

// TestR259_TheOpenAIAdapterPerformsEveryFunctionAndChoosesModels asserts
// R-259 for OpenAI: every function, as data, with model choice.
func TestR259_TheOpenAIAdapterPerformsEveryFunctionAndChoosesModels(t *testing.T) {
	a, _ := withFake(t)
	caps, err := a.Capabilities(context.Background())
	require.NoError(t, err)
	require.True(t, caps.ChoosesModel)
	require.Equal(t, openaiadapter.DefaultModel, caps.Model)
	for _, fn := range api.AIFunctions() {
		require.True(t, caps.Does(fn), "%s", fn)
	}
	require.NoError(t, a.HealthCheck(context.Background()))
}

// TestAnOpenAIAdapterWithNoKeyRefusesToConfigure asserts design 10 §7: an AI
// adapter without a credential does not register.
func TestAnOpenAIAdapterWithNoKeyRefusesToConfigure(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	err := openaiadapter.New().Configure(context.Background(), json.RawMessage(`{}`))
	require.ErrorContains(t, err, "no API key")

	err = openaiadapter.New().Configure(context.Background(), json.RawMessage(`{"api_key":"sk-plain"}`))
	require.ErrorContains(t, err, "unencrypted", "a key in plain configuration is refused, not used")
}

// TestR336_OpenAIRepairsAPlanThroughTheBudgetedReader asserts R-336 and
// R-337 for OpenAI: the model reads through the reader, each turn continues
// the last, and what it read is recorded.
func TestR336_OpenAIRepairsAPlanThroughTheBudgetedReader(t *testing.T) {
	findings := `{"amendments":[{"kind":"set_env","key":"HOST","value":"0.0.0.0",` +
		`"reason":"server.js binds 127.0.0.1, which is unreachable behind the proxy.","evidence":["server.js"]}]}`
	a, fake := withFake(t,
		response("resp_1", call("c1", "read_file", `{"path":"server.js"}`)),
		response("resp_2", call("c2", "submit_findings", findings)))

	req := aitest.Request()
	req.Model = "gpt-5.4-mini"
	got, err := a.RepairPlan(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, got.Amendments, 1)
	require.Equal(t, "HOST", got.Amendments[0].Key)
	require.Equal(t, []string{"server.js"}, got.FilesRead)
	require.Equal(t, "gpt-5.4-mini", got.Model, "the assignment's model ran")

	require.Len(t, fake.bodies, 2)
	require.Equal(t, "gpt-5.4-mini", fake.bodies[0]["model"])
	require.Contains(t, fake.bodies[0]["instructions"], "repairing a deployment plan")
	require.Equal(t, "resp_1", fake.bodies[1]["previous_response_id"], "the conversation continues")
	input := fake.bodies[1]["input"].([]any)[0].(map[string]any)
	require.Equal(t, "function_call_output", input["type"])
	require.Equal(t, "c1", input["call_id"])
	require.Contains(t, input["output"], "127.0.0.1", "the file went back to the model")
}

// TestR346_OpenAIAnswersThroughTheSubmitTool asserts R-346 for OpenAI, and
// that a prose answer is asked for again rather than lost.
func TestR346_OpenAIAnswersThroughTheSubmitTool(t *testing.T) {
	a, fake := withFake(t,
		response("resp_1", text("Use POST /api/v1/groups.")),
		response("resp_2", call("c1", "submit", `{"answer":"Use POST /api/v1/groups.","cites":["POST /api/v1/groups"],"covered":true}`)))

	got, err := a.AnswerReference(context.Background(), api.ReferenceRequest{
		Question: "How do I make a group?", Reference: "POST /api/v1/groups creates a group.",
	})
	require.NoError(t, err)
	require.True(t, got.Covered)
	require.Equal(t, []string{"POST /api/v1/groups"}, got.Cites)
	require.Equal(t, openaiadapter.DefaultModel, got.Model)

	require.Len(t, fake.bodies, 2)
	tools := fake.bodies[0]["tools"].([]any)
	require.Len(t, tools, 1)
	require.Equal(t, "submit", tools[0].(map[string]any)["name"])
	require.Nil(t, fake.bodies[0]["tool_choice"], "asked, not forced")
	require.Equal(t, "resp_1", fake.bodies[1]["previous_response_id"])
}

func TestOpenAIGivesUpWhenTheModelNeverUsesTheTool(t *testing.T) {
	a, _ := withFake(t, response("resp_1", text("Sure.")), response("resp_2", text("Sure.")))
	_, err := a.DraftPolicy(context.Background(), api.PolicyRequest{Description: "no exec"})
	require.ErrorContains(t, err, "without submitting")
}
