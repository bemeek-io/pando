package anthropic_test

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

	anthropicadapter "github.com/bemeek-io/pando/internal/adapter/ai/anthropic"
	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
)

// fakeAPI stands in for the Messages API, replaying one scripted reply per
// request and recording what Pando sent. The screening loop is Pando's own
// code — budget, tool results, stopping on findings — so it is tested here
// rather than trusted to a network call nobody runs in CI.
type fakeAPI struct {
	t       *testing.T
	mu      sync.Mutex
	replies []string
	bodies  []map[string]any
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/models/") {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"claude-opus-5","type":"model","display_name":"Claude Opus 5","created_at":"2026-01-01T00:00:00Z"}`)
		return
	}

	require.Equal(f.t, "/v1/messages", r.URL.Path)
	require.Equal(f.t, "sk-ant-test", r.Header.Get("X-Api-Key"), "the credential reaches the API")

	var body map[string]any
	require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
	f.bodies = append(f.bodies, body)

	if len(f.replies) == 0 {
		f.t.Fatalf("unexpected request %d", len(f.bodies))
	}
	reply := f.replies[0]
	f.replies = f.replies[1:]
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, reply)
}

func message(stop string, content ...string) string {
	return `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5",` +
		`"content":[` + strings.Join(content, ",") + `],"stop_reason":"` + stop + `",` +
		`"usage":{"input_tokens":1,"output_tokens":1}}`
}

func toolUse(id, name, input string) string {
	return `{"type":"tool_use","id":"` + id + `","name":"` + name + `","input":` + input + `}`
}

func withFake(t *testing.T, replies ...string) (*anthropicadapter.Adapter, *fakeAPI) {
	t.Helper()
	fake := &fakeAPI{t: t, replies: replies}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	a := anthropicadapter.New()
	cfg, err := json.Marshal(map[string]any{
		"credentials": map[string]string{"api_key": "sk-ant-test"},
		"base_url":    server.URL,
		"max_files":   2,
	})
	require.NoError(t, err)
	require.NoError(t, a.Configure(context.Background(), cfg))
	return a, fake
}

func request() api.ScreenRequest {
	port := "3000"
	return api.ScreenRequest{
		Source: memSource{
			"package.json": `{"scripts":{"start":"node server.js"}}`,
			"server.js":    "app.listen(3000, '127.0.0.1')",
			"README.md":    "Set HOST=0.0.0.0",
		},
		Spec: spec.AppSpec{
			Build: spec.Build{Strategy: spec.BuildBuildpack},
			Workloads: []spec.Workload{{
				Name: "web", Primary: true,
				Env:   []spec.EnvEntry{{Key: "PORT", Value: &port}},
				Ports: []spec.Port{{Number: 3000, Protocol: "http", Source: spec.PortObserved}},
			}},
			Routing: spec.Routing{Mode: spec.RoutingPath},
		},
		Evidence:  []string{"package.json declares a start script"},
		Questions: []api.Question{{Key: "start_command", Kind: api.QuestionText, Prompt: "Which command starts it?", Options: []string{"npm start"}}},
		Trial: api.TrialSummary{
			Ran: true, Crashed: true, ObservedPorts: []int{3000},
			ObservedWrites: []string{"/app/uploads"}, Log: "Error: listen EADDRINUSE",
		},
		Budget: api.ScreenBudget{MaxFiles: 10, MaxBytes: 1 << 20},
	}
}

// TestR330_AScreeningReadsTheRepositoryThenSubmitsFindings drives the whole
// loop: the model lists files, reads two, and submits one amendment.
func TestR330_AScreeningReadsTheRepositoryThenSubmitsFindings(t *testing.T) {
	a, fake := withFake(t,
		message("tool_use",
			toolUse("t1", "list_files", `{"pattern":"*"}`),
			toolUse("t2", "read_file", `{"path":"server.js"}`)),
		message("tool_use",
			toolUse("t3", "read_file", `{"path":"README.md"}`),
			toolUse("t4", "read_file", `{"path":"package.json"}`)),
		message("tool_use", toolUse("t5", "submit_findings", `{
			"amendments":[{"kind":"set_env","key":"HOST","value":"0.0.0.0",
			  "reason":"The app binds 127.0.0.1.","evidence":["server.js","README.md"]}],
			"notes":["The README documents HOST."]}`)),
	)

	result, err := a.RepairPlan(context.Background(), request())
	require.NoError(t, err)

	require.Len(t, result.Amendments, 1)
	require.Equal(t, api.AmendSetEnv, result.Amendments[0].Kind)
	require.Equal(t, []string{"The README documents HOST."}, result.Notes)
	require.Equal(t, anthropicadapter.DefaultModel, result.Model)
	require.Equal(t, []string{"server.js", "README.md"}, result.FilesRead,
		"the adapter's own limit of two files held; the third read was refused, not performed")

	require.Len(t, fake.bodies, 3)
	first := fake.bodies[0]
	require.Equal(t, anthropicadapter.DefaultModel, first["model"])
	prompt, _ := json.Marshal(first["messages"])
	require.Contains(t, string(prompt), "EADDRINUSE", "the trial log is in the prompt")
	require.Contains(t, string(prompt), "start_command", "so are Pando's questions")
	require.Contains(t, string(prompt), "observed binding", "and what was observed")

	// The refused read came back as an ordinary result telling the model to
	// finish, so the screening was bounded rather than lost.
	third, _ := json.Marshal(fake.bodies[2]["messages"])
	require.Contains(t, string(third), "Submit your findings now")

	tools, _ := json.Marshal(first["tools"])
	require.Contains(t, string(tools), `"strict":true`, "findings are schema-checked by the API")
}

// TestR332_TheToolSchemaIsTheClosedSet asserts R-332 one layer before core:
// the enum the model is given contains exactly the amendment kinds Pando has.
func TestR332_TheToolSchemaIsTheClosedSet(t *testing.T) {
	a, fake := withFake(t, message("tool_use", toolUse("t1", "submit_findings", `{"amendments":[]}`)))

	result, err := a.RepairPlan(context.Background(), request())
	require.NoError(t, err)
	require.Empty(t, result.Amendments, "an empty result is a good outcome")

	tools, _ := json.Marshal(fake.bodies[0]["tools"])
	for _, kind := range []api.AmendmentKind{
		api.AmendSetCommand, api.AmendSetEnv, api.AmendSetPort, api.AmendSetHealth, api.AmendAddSlot,
		api.AmendSetBuildContext, api.AmendSetDockerfile, api.AmendSetStaticDir, api.AmendAddVolume,
		api.AmendAnswerQuestion, api.AmendAddWarning,
	} {
		require.Contains(t, string(tools), `"`+string(kind)+`"`)
	}
	require.NotContains(t, string(tools), "isolation")
	require.NotContains(t, string(tools), "egress")
}

// TestR106_ARepairIsToldThePlanFailed asserts R-106's repair function: the
// model is told this plan did not work, is handed the log, and is told that
// changing nothing is right when the failure is real (R-107).
func TestR106_ARepairIsToldThePlanFailed(t *testing.T) {
	a, fake := withFake(t, message("tool_use", toolUse("t1", "submit_findings", `{"amendments":[]}`)))

	_, err := a.RepairPlan(context.Background(), request())
	require.NoError(t, err)

	system, _ := json.Marshal(fake.bodies[0]["system"])
	require.Contains(t, string(system), "repairing a deployment plan")
	require.Contains(t, string(system), "the failure is real")
	prompt, _ := json.Marshal(fake.bodies[0]["messages"])
	require.Contains(t, string(prompt), "which did not work")
	require.Contains(t, string(prompt), "EADDRINUSE")
}

// TestR338_AnsweringQuestionsOffersOnlyAnswers asserts R-338 and R-336 at the
// adapter: asked to answer questions, the model is given one amendment kind,
// so it cannot spend its budget proposing changes core would refuse.
func TestR338_AnsweringQuestionsOffersOnlyAnswers(t *testing.T) {
	a, fake := withFake(t, message("tool_use", toolUse("t1", "submit_findings", `{
		"amendments":[{"kind":"answer_question","key":"start_command","value":"npm start",
		  "reason":"package.json defines a start script.","evidence":["package.json"]}]}`)))

	result, err := a.AnswerQuestions(context.Background(), request())
	require.NoError(t, err)
	require.Len(t, result.Amendments, 1)
	require.Equal(t, api.AmendAnswerQuestion, result.Amendments[0].Kind)

	system, _ := json.Marshal(fake.bodies[0]["system"])
	require.Contains(t, string(system), "answering questions")

	tools, _ := json.Marshal(fake.bodies[0]["tools"])
	require.Contains(t, string(tools), `"enum":["answer_question"]`)
	require.NotContains(t, string(tools), `"set_command"`)

	// The key is required and can only name a question that was asked. A
	// model once answered build_strategy correctly with no key, and the answer
	// was refused because nothing said which question it was for.
	item := fake.bodies[0]["tools"].([]any)[2].(map[string]any)["input_schema"].(map[string]any)["properties"].(map[string]any)["amendments"].(map[string]any)["items"].(map[string]any)
	require.ElementsMatch(t, []any{"kind", "key", "value", "reason", "evidence"}, item["required"])
	key := item["properties"].(map[string]any)["key"].(map[string]any)
	require.Equal(t, []any{"start_command"}, key["enum"])
}

// TestR336_ARevisionHearsThePersonAndReplies asserts R-336's third trigger at
// the adapter: the person's words and what was said before reach the model,
// quoted as theirs, and the tool requires a reply so they are never met with
// silence.
func TestR336_ARevisionHearsThePersonAndReplies(t *testing.T) {
	a, fake := withFake(t, message("tool_use", toolUse("t1", "submit_findings", `{
		"amendments":[],
		"reply":"server.js calls listen(3000), which is what the plan has."}`)))

	req := request()
	req.Instruction = "It serves on 8080."
	req.Conversation = []api.Turn{{From: "person", Text: "Is the port right?"}, {From: "ai", Text: "It is 3000."}}
	result, err := a.RevisePlan(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "server.js calls listen(3000), which is what the plan has.", result.Reply)

	system, _ := json.Marshal(fake.bodies[0]["system"])
	require.Contains(t, string(system), "helping a person review")
	prompt, _ := json.Marshal(fake.bodies[0]["messages"])
	require.Contains(t, string(prompt), "What the person reviewing the plan says now")
	// JSON escapes ">" as >; the prompt quotes the person's words.
	quoted := string(rune('\\')) + "u003e It serves on 8080."
	require.Contains(t, string(prompt), quoted, "quoted as theirs")
	require.Contains(t, string(prompt), "Is the port right?")

	tools, _ := json.Marshal(fake.bodies[0]["tools"])
	require.Contains(t, string(tools), `"reply"`)
	require.Contains(t, string(tools), `"required":["amendments","reply"]`)
}

// TestR335_AModelThatStopsWithoutFindingsIsAFailureNotACleanBill asserts R-335:
// "found nothing" and "did not finish" mean opposite things in the review.
func TestR335_AModelThatStopsWithoutFindingsIsAFailureNotACleanBill(t *testing.T) {
	a, _ := withFake(t, message("end_turn", `{"type":"text","text":"Looks fine."}`))
	_, err := a.RepairPlan(context.Background(), request())
	require.Error(t, err)
	require.Contains(t, err.Error(), "without submitting")
}

// TestR335_ARefusalIsReportedAsAFailure asserts R-335 for a declined request.
func TestR335_ARefusalIsReportedAsAFailure(t *testing.T) {
	a, _ := withFake(t, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5",`+
		`"content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber"},`+
		`"usage":{"input_tokens":1,"output_tokens":1}}`)
	_, err := a.RepairPlan(context.Background(), request())
	require.Error(t, err)
	require.Contains(t, err.Error(), "declined")
}

// TestR020_ToolCallsThatLeaveTheCheckoutAreRefused asserts R-020 through the
// loop: a bad path or an unknown tool is an error result, and the screening
// carries on to its findings.
func TestR020_ToolCallsThatLeaveTheCheckoutAreRefused(t *testing.T) {
	a, fake := withFake(t,
		message("tool_use",
			toolUse("t1", "read_file", `{"path":"../../etc/passwd"}`),
			toolUse("t2", "list_files", `{"pattern":"/etc/*"}`),
			toolUse("t3", "list_files", `{"pattern":"nothing-matches-*"}`),
			toolUse("t4", "write_file", `{"path":"x"}`),
			toolUse("t5", "read_file", `{"path":"missing.txt"}`)),
		message("tool_use", toolUse("t6", "submit_findings", `{"amendments":[]}`)),
	)

	result, err := a.RepairPlan(context.Background(), request())
	require.NoError(t, err)
	require.Empty(t, result.FilesRead)

	second, _ := json.Marshal(fake.bodies[1]["messages"])
	require.Contains(t, string(second), "outside the repository")
	require.Contains(t, string(second), "absolute path")
	require.Contains(t, string(second), "Nothing in this repository matches")
	require.Contains(t, string(second), "no tool by that name")
}

// TestHealthCheckAsksTheAPIAboutTheConfiguredModel asserts design 10 §6: a
// credential that does not work or a model nobody serves is caught before a
// screening, at no token cost.
func TestHealthCheckAsksTheAPIAboutTheConfiguredModel(t *testing.T) {
	a, _ := withFake(t)
	require.NoError(t, a.HealthCheck(context.Background()))
	require.Error(t, anthropicadapter.New().HealthCheck(context.Background()), "unconfigured")
}

// TestR336_AnAdapterTurnedOffForScreeningDoesNotScreen asserts R-336.
func TestR336_AnAdapterTurnedOffForScreeningDoesNotScreen(t *testing.T) {
	a := anthropicadapter.New()
	require.NoError(t, a.Configure(context.Background(),
		json.RawMessage(`{"credentials":{"api_key":"sk-ant-test"},"screen_plans":false}`)))
	_, err := a.RepairPlan(context.Background(), request())
	require.Error(t, err)
	require.Equal(t, anthropicadapter.Kind, a.Kind())
	require.Equal(t, api.CategoryAI, a.Category())
}
