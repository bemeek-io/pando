package aikit_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/ai/aikit"
	"github.com/bemeek-io/pando/internal/adapter/ai/aikit/aitest"
	"github.com/bemeek-io/pando/internal/adapter/api"
)

// TestSystemPromptDiffersByFunction asserts each screening function gets its
// own job description, and that an unknown one falls back to repair.
func TestSystemPromptDiffersByFunction(t *testing.T) {
	repair := aikit.SystemPrompt(api.AIFunctionRepairPlan)
	answer := aikit.SystemPrompt(api.AIFunctionAnswerQuestions)
	revise := aikit.SystemPrompt(api.AIFunctionRevisePlan)

	require.NotEqual(t, repair, answer)
	require.NotEqual(t, repair, revise)
	require.NotEqual(t, answer, revise)
	require.Contains(t, repair, "repairing a deployment plan")
	require.Contains(t, answer, "answering questions")
	require.Contains(t, revise, "review a deployment plan")
	for _, p := range []string{repair, answer, revise} {
		require.Contains(t, p, "submit_findings", "every screening answers through the submit tool")
	}
	require.Equal(t, repair, aikit.SystemPrompt(api.AIFunctionReadReadme), "anything else is a repair")
}

// TestUserPromptCarriesQuestionsAndValues asserts the questions and the values
// the deploy waits on reach the model, with a question's valid answers.
func TestUserPromptCarriesQuestionsAndValues(t *testing.T) {
	req := aitest.Request()
	req.Questions = []api.Question{
		{Key: "port", Kind: api.QuestionPort, Prompt: "Which port does it serve on?"},
		{Key: "build_strategy", Kind: api.QuestionChoice, Prompt: "How is it built?",
			Options: []string{"dockerfile", "buildpack"}},
	}
	req.Values = []string{"APP_URL", "CONTACT_EMAIL"}

	p := aikit.UserPrompt(api.AIFunctionAnswerQuestions, req)
	require.Contains(t, p, "the questions it could not answer")
	require.Contains(t, p, "**port** (port): Which port does it serve on?")
	require.Contains(t, p, "Valid answers: dockerfile, buildpack")
	require.Contains(t, p, "## Values the deploy waits on")
	require.Contains(t, p, "- APP_URL")
	require.Contains(t, p, "- CONTACT_EMAIL")
	require.Contains(t, p, "package.json declares a start script", "the detector's evidence")
	require.Contains(t, p, "You may read up to 10 files")
	require.NotContains(t, p, "What the person reviewing the plan says now",
		"only a revision quotes a person")
}

// TestUserPromptForARevisionQuotesThePerson asserts a revision carries the
// earlier conversation and the person's instruction, quoted as theirs.
func TestUserPromptForARevisionQuotesThePerson(t *testing.T) {
	req := aitest.Request()
	req.Evidence = nil
	req.Conversation = []api.Turn{
		{From: "person", Text: "It needs Redis."},
		{From: "ai", Text: "Nothing in the repository mentions Redis."},
	}
	req.Instruction = "Look at config/cache.js.\nIt reads REDIS_URL."

	p := aikit.UserPrompt(api.AIFunctionRevisePlan, req)
	require.Contains(t, p, "which a person is reviewing")
	require.Contains(t, p, "(no evidence recorded)")
	require.Contains(t, p, "## What was said before")
	require.Contains(t, p, "The person: It needs Redis.")
	require.Contains(t, p, "You: Nothing in the repository mentions Redis.")
	require.Contains(t, p, "> Look at config/cache.js.\n> It reads REDIS_URL.")
	require.Less(t, strings.Index(p, "What was said before"), strings.Index(p, "says now"),
		"the instruction comes last")
}

// TestUserPromptDescribesTheTrialRun asserts what the trial saw is told
// plainly in each of its shapes, and that a long log keeps its end.
func TestUserPromptDescribesTheTrialRun(t *testing.T) {
	req := aitest.Request()

	req.Trial = api.TrialSummary{}
	require.Contains(t, aikit.UserPrompt(api.AIFunctionRepairPlan, req), "Pando did not run this application")

	req.Trial = api.TrialSummary{Ran: true, Crashed: true}
	p := aikit.UserPrompt(api.AIFunctionRepairPlan, req)
	require.Contains(t, p, "which did not work")
	require.Contains(t, p, "exited with an error")
	require.Contains(t, p, "not observed binding any port")
	require.NotContains(t, p, "Its output")

	long := "first line\n" + strings.Repeat("x", 20<<10) + "\nthe real reason"
	req.Trial = api.TrialSummary{
		Ran: true, ObservedPorts: []int{8080}, ObservedWrites: []string{"/tmp/cache", "/var/data"}, Log: long,
	}
	p = aikit.UserPrompt(api.AIFunctionRepairPlan, req)
	require.Contains(t, p, "it stayed up")
	require.Contains(t, p, "observed binding [8080]")
	require.Contains(t, p, "It wrote to /tmp/cache, /var/data")
	require.Contains(t, p, "[earlier output omitted]")
	require.Contains(t, p, "the real reason", "the end of a log is where the reason is")
	require.NotContains(t, p, "first line")
}

func submitOf(t *testing.T, tools []aikit.Tool) aikit.Tool {
	t.Helper()
	require.Len(t, tools, 3)
	require.Equal(t, aikit.ToolListFiles, tools[0].Name)
	require.Equal(t, aikit.ToolReadFile, tools[1].Name)
	require.Equal(t, aikit.ToolSubmitFindings, tools[2].Name)
	return tools[2]
}

// TestScreenToolsRevisionRequiresAReply asserts a person who asked is never
// met with silence: a revision's submit requires reply, a repair's does not.
func TestScreenToolsRevisionRequiresAReply(t *testing.T) {
	revise := submitOf(t, aikit.ScreenTools(api.AIFunctionRevisePlan, nil, nil))
	require.Equal(t, []string{"amendments", "reply"}, revise.Required)
	require.Contains(t, revise.Properties, "reply")

	repair := submitOf(t, aikit.ScreenTools(api.AIFunctionRepairPlan, nil, nil))
	require.Equal(t, []string{"amendments"}, repair.Required)
	require.NotContains(t, repair.Properties, "reply")

	items := repair.Properties["amendments"].(map[string]any)["items"].(map[string]any)
	kinds := items["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]string)
	require.Contains(t, kinds, "set_command", "a repair gets the whole closed set")
	require.Contains(t, kinds, "answer_question")
}

// TestR336_AnswerSchemaNamesOnlyTheKeysAsked asserts R-336.
//
// An answer's key must be one of the questions asked, and a value's one of the
// variables the deploy waits on; with both, the item is either shape.
func TestR336_AnswerSchemaNamesOnlyTheKeysAsked(t *testing.T) {
	questions := []api.Question{{Key: "port"}, {Key: "build_strategy"}}

	items := func(tool aikit.Tool) map[string]any {
		return tool.Properties["amendments"].(map[string]any)["items"].(map[string]any)
	}
	keyEnum := func(shape map[string]any) []string {
		return shape["properties"].(map[string]any)["key"].(map[string]any)["enum"].([]string)
	}
	kindEnum := func(shape map[string]any) []string {
		return shape["properties"].(map[string]any)["kind"].(map[string]any)["enum"].([]string)
	}

	only := items(submitOf(t, aikit.ScreenTools(api.AIFunctionAnswerQuestions, questions, nil)))
	require.Equal(t, []string{"port", "build_strategy"}, keyEnum(only))
	require.Equal(t, []string{string(api.AmendAnswerQuestion)}, kindEnum(only))
	require.Equal(t, false, only["additionalProperties"])
	require.Contains(t, only["required"], "key", "an answer without a key cannot be matched to its question")

	values := items(submitOf(t, aikit.ScreenTools(api.AIFunctionAnswerQuestions, nil, []string{"APP_URL"})))
	require.Equal(t, []string{"APP_URL"}, keyEnum(values))
	require.Equal(t, []string{string(api.AmendSetEnv)}, kindEnum(values))

	both := items(submitOf(t, aikit.ScreenTools(api.AIFunctionAnswerQuestions, questions, []string{"APP_URL"})))
	shapes := both["anyOf"].([]any)
	require.Len(t, shapes, 2)
	require.Equal(t, []string{"port", "build_strategy"}, keyEnum(shapes[0].(map[string]any)))
	require.Equal(t, []string{"APP_URL"}, keyEnum(shapes[1].(map[string]any)))
}

// TestToolSchemaStrictClosesTheObject asserts a strict tool refuses fields it
// did not declare, and a plain one says nothing either way.
func TestToolSchemaStrictClosesTheObject(t *testing.T) {
	plain := aikit.Tool{Properties: map[string]any{"a": map[string]any{"type": "string"}}, Required: []string{"a"}}
	s := plain.Schema()
	require.Equal(t, "object", s["type"])
	require.Equal(t, []string{"a"}, s["required"])
	require.NotContains(t, s, "additionalProperties")

	plain.Strict = true
	require.Equal(t, false, plain.Schema()["additionalProperties"])
}

func reader(budget int) *aikit.Reader {
	return aikit.NewReader(aitest.Source{
		"package.json": `{"name":"x"}`,
		"server.js":    "listen(3000)",
		"src/a.js":     "a",
	}, budget, 1<<20)
}

// TestCallListFiles asserts list_files lists matches, says so when there are
// none, and reports an unreadable request as an error.
func TestCallListFiles(t *testing.T) {
	r := reader(10)

	out, isErr := aikit.Call(aikit.ToolListFiles, `{"pattern":"*.js*"}`, r)
	require.False(t, isErr)
	var names []string
	require.NoError(t, json.Unmarshal([]byte(out), &names))
	require.Equal(t, []string{"package.json", "server.js"}, names)

	out, isErr = aikit.Call(aikit.ToolListFiles, `{"pattern":"*.py"}`, r)
	require.False(t, isErr, "no match is an answer, not a failure")
	require.Contains(t, out, "Nothing in this repository matches")

	out, isErr = aikit.Call(aikit.ToolListFiles, `{"pattern":`, r)
	require.True(t, isErr)
	require.Contains(t, out, "could not be read")

	out, isErr = aikit.Call(aikit.ToolListFiles, `{"pattern":"/etc/*"}`, r)
	require.True(t, isErr)
	require.Contains(t, out, "absolute path")

	require.Empty(t, r.Files(), "listing is not reading")
}

// TestR339_ABudgetRefusalAsksForFindings asserts R-339.
//
// A read past the budget comes back as an ordinary result telling the model to
// submit, so the screening ends bounded rather than failed.
func TestR339_ABudgetRefusalAsksForFindings(t *testing.T) {
	r := reader(1)

	out, isErr := aikit.Call(aikit.ToolReadFile, `{"path":"server.js"}`, r)
	require.False(t, isErr)
	require.Equal(t, "listen(3000)", out)

	out, isErr = aikit.Call(aikit.ToolReadFile, `{"path":"package.json"}`, r)
	require.False(t, isErr, "a spent budget is not an error")
	require.Contains(t, out, "Submit your findings")
}

// TestCallReadFileErrors asserts an unreadable request, a missing file and an
// unknown tool each come back as errors the model can see.
func TestCallReadFileErrors(t *testing.T) {
	r := reader(10)

	out, isErr := aikit.Call(aikit.ToolReadFile, `not json`, r)
	require.True(t, isErr)
	require.Contains(t, out, "could not be read")

	out, isErr = aikit.Call(aikit.ToolReadFile, `{"path":"missing.txt"}`, r)
	require.True(t, isErr)
	require.Contains(t, out, "missing.txt could not be read")

	out, isErr = aikit.Call("delete_everything", `{}`, r)
	require.True(t, isErr)
	require.Contains(t, out, "no tool by that name")
}

// TestFindingsParsesTheSubmission asserts a submission becomes a result that
// carries the files read and the model, and a malformed one is an error.
func TestFindingsParsesTheSubmission(t *testing.T) {
	r := reader(10)
	_, err := r.Open("server.js")
	require.NoError(t, err)

	res, err := aikit.Findings(`{
		"amendments":[{"kind":"set_port","port":3000,"reason":"server.js listens on 3000.","evidence":["server.js"]}],
		"notes":["It binds loopback."],
		"reply":"Set the port."}`, r, "model-x")
	require.NoError(t, err)
	require.Len(t, res.Amendments, 1)
	require.Equal(t, api.AmendSetPort, res.Amendments[0].Kind)
	require.Equal(t, []string{"It binds loopback."}, res.Notes)
	require.Equal(t, "Set the port.", res.Reply)
	require.Equal(t, []string{"server.js"}, res.FilesRead)
	require.Equal(t, "model-x", res.Model)

	_, err = aikit.Findings(`{"amendments":`, r, "model-x")
	require.Error(t, err)
	require.Contains(t, err.Error(), "could not be read")
}

// TestR337_PreloadedFilesAreReadThroughTheBudget asserts R-337.
//
// Files handed over from an earlier call are read like any other, so they are
// recorded as sent; one that is gone is left out, and none at all is nothing.
func TestR337_PreloadedFilesAreReadThroughTheBudget(t *testing.T) {
	require.Empty(t, aikit.Preloaded(reader(10), nil))

	r := reader(10)
	out := aikit.Preloaded(r, []string{"server.js", "gone.txt"})
	require.Contains(t, out, "Files you read about this plan before")
	require.Contains(t, out, "### server.js")
	require.Contains(t, out, "listen(3000)")
	require.NotContains(t, out, "gone.txt")
	require.Equal(t, []string{"server.js"}, r.Files())

	require.Empty(t, aikit.Preloaded(reader(10), []string{"gone.txt"}), "nothing readable is nothing")
}

// TestLimitTakesTheLower asserts neither core nor the adapter raises the
// other's limit.
func TestLimitTakesTheLower(t *testing.T) {
	require.Equal(t, 5, aikit.Limit(5, 10))
	require.Equal(t, 10, aikit.Limit(20, 10))
	require.Equal(t, 10, aikit.Limit(0, 10), "nothing asked is the adapter's own")
	require.Equal(t, int64(10), aikit.Limit(int64(-1), int64(10)))
}

// TestR343_AccessTaskCarriesTheCatalog asserts R-343.
//
// The draft is asked for over the verbs given, by exact name, and a refinement
// carries the draft so far.
func TestR343_AccessTaskCarriesTheCatalog(t *testing.T) {
	req := api.AccessRequest{
		Description: "Release managers can deploy any app",
		Verbs:       []api.VerbInfo{{Name: "app.deploy", Scope: "app"}, {Name: "app.view", Scope: "app"}},
		Roles:       []api.RoleInfo{{ID: "role_1", Name: "Viewer", Builtin: true}},
		People:      []api.PersonInfo{{ID: "usr_1", Username: "ben"}},
	}
	task := aikit.AccessTask(req)
	require.Contains(t, task.System, "draft access")
	require.Contains(t, task.System, aikit.Voice)
	require.Contains(t, task.User, "Release managers can deploy any app")
	require.Contains(t, task.User, `"app.deploy"`)
	require.Contains(t, task.User, `"usr_1"`)
	require.Contains(t, task.User, "## Existing groups\n\n(none)")
	require.NotContains(t, task.User, "The draft so far")
	require.Equal(t, aikit.ToolSubmit, task.Tool.Name)
	require.Equal(t, []string{"reply"}, task.Tool.Required)

	role := task.Tool.Properties["role"].(map[string]any)["properties"].(map[string]any)
	items := role["verbs"].(map[string]any)["items"].(map[string]any)
	require.Equal(t, []string{"app.deploy", "app.view"}, items["enum"])

	req.Current = &api.AccessDraft{Role: &api.RoleDraft{Name: "Release manager", Scope: "app"}}
	req.Verbs = nil
	task = aikit.AccessTask(req)
	require.True(t, strings.HasPrefix(task.User, "## The draft so far"))
	require.Contains(t, task.User, "Release manager")
	role = task.Tool.Properties["role"].(map[string]any)["properties"].(map[string]any)
	require.NotContains(t, role["verbs"].(map[string]any)["items"], "enum", "an empty enum never reaches the API")
}

// TestR344_PolicyTaskCarriesFieldsAndDraft asserts R-344.
func TestR344_PolicyTaskCarriesFieldsAndDraft(t *testing.T) {
	req := api.PolicyRequest{
		Description: "Turn off terminal access",
		Current:     json.RawMessage(`{"terminal":true}`),
		Fields:      []api.PolicyField{{Key: "terminal", Type: "boolean", Meaning: "Terminal access"}},
	}
	task := aikit.PolicyTask(req)
	require.Contains(t, task.System, "host policy")
	require.Contains(t, task.User, "Turn off terminal access")
	require.Contains(t, task.User, `{"terminal":true}`)
	require.Contains(t, task.User, `"Terminal access"`)
	require.NotContains(t, task.User, "The draft so far")
	require.Equal(t, aikit.ToolSubmit, task.Tool.Name)
	require.Equal(t, []string{"changes", "reply"}, task.Tool.Required)

	req.Draft = json.RawMessage(`{"terminal":false,"max_apps":3}`)
	task = aikit.PolicyTask(req)
	require.Contains(t, task.User, "## The draft so far")
	require.Contains(t, task.User, `"max_apps":3`)
}

// TestR345_AuditSearchTaskResolvesAgainstNow asserts R-345.
//
// The current time is given in UTC and RFC 3339, so "last week" means
// something.
func TestR345_AuditSearchTaskResolvesAgainstNow(t *testing.T) {
	now := time.Date(2026, 9, 26, 14, 30, 0, 0, time.FixedZone("MDT", -6*3600))
	task := aikit.AuditSearchTask(api.AuditSearchRequest{
		Question: "Who deployed billing last week?",
		Now:      now,
		People:   []api.PersonInfo{{ID: "usr_1", Username: "ben"}},
		Apps:     []api.AppInfo{{ID: "app_1", Name: "billing"}},
		Actions:  []string{"app.deploy"},
	})
	require.Contains(t, task.System, "audit log")
	require.Contains(t, task.User, "Who deployed billing last week?")
	require.Contains(t, task.User, "2026-09-26T20:30:00Z")
	require.Contains(t, task.User, `"app_1"`)
	require.Contains(t, task.User, `"app.deploy"`)
	require.Equal(t, aikit.ToolSubmit, task.Tool.Name)
	require.Equal(t, []string{"filter"}, task.Tool.Required)
}

// TestR345_AuditSummaryTaskUsesOnlyTheRecords asserts R-345.
func TestR345_AuditSummaryTaskUsesOnlyTheRecords(t *testing.T) {
	task := aikit.AuditSummaryTask(api.AuditSummaryRequest{
		Question:  "Who deployed billing?",
		Filter:    api.AuditFilter{AppID: "app_1"},
		Records:   []api.AuditRecordView{{Action: "app.deploy", PrincipalID: "usr_1"}},
		Truncated: true,
	})
	require.Contains(t, task.System, "Use only the records given")
	require.Contains(t, task.User, "Who deployed billing?")
	require.Contains(t, task.User, `"app_id": "app_1"`)
	require.Contains(t, task.User, `"app.deploy"`)
	require.Contains(t, task.User, "Truncated: true")
	require.Contains(t, task.User, "## Accounts\n\n(none)")
	require.Equal(t, aikit.ToolSubmit, task.Tool.Name)
	require.Equal(t, []string{"summary"}, task.Tool.Required)
}

// TestR346_ReferenceTaskAnswersFromTheReference asserts R-346.
func TestR346_ReferenceTaskAnswersFromTheReference(t *testing.T) {
	task := aikit.ReferenceTask(api.ReferenceRequest{
		Question:  "How can I create a group?",
		Reference: "POST /api/v1/groups creates a group.",
	})
	require.Contains(t, task.System, "Answer only from the reference given")
	require.Contains(t, task.User, "How can I create a group?")
	require.Contains(t, task.User, "POST /api/v1/groups creates a group.")
	require.Equal(t, aikit.ToolSubmit, task.Tool.Name)
	require.Equal(t, []string{"answer", "covered"}, task.Tool.Required)
}

// TestJSONBlock asserts a value renders fenced, and nothing, or something that
// cannot be rendered, reads as "(none)".
func TestJSONBlock(t *testing.T) {
	require.Equal(t, "(none)", aikit.JSONBlock(nil))
	require.Equal(t, "(none)", aikit.JSONBlock([]api.AppInfo(nil)))
	require.Equal(t, "(none)", aikit.JSONBlock(make(chan int)))
	require.Equal(t, "```json\n[\n  \"a\"\n]\n```", aikit.JSONBlock([]string{"a"}))
}
