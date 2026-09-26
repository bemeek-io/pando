//go:build integration

package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	adapterapi "github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/errs"
)

// failingAI is an AI adapter that is reached and fails at every
// administrative function.
type failingAI struct{ *fakeAI }

var errProvider = errors.New("the provider answered 529 overloaded")

func (failingAI) DraftAccess(context.Context, adapterapi.AccessRequest) (adapterapi.AccessDraft, error) {
	return adapterapi.AccessDraft{}, errProvider
}

func (failingAI) DraftPolicy(context.Context, adapterapi.PolicyRequest) (adapterapi.PolicyDraft, error) {
	return adapterapi.PolicyDraft{}, errProvider
}

func (failingAI) SearchAudit(context.Context, adapterapi.AuditSearchRequest) (adapterapi.AuditSearch, error) {
	return adapterapi.AuditSearch{}, errProvider
}

func (failingAI) AnswerReference(context.Context, adapterapi.ReferenceRequest) (adapterapi.ReferenceAnswer, error) {
	return adapterapi.ReferenceAnswer{}, errProvider
}

// aiPosts are the administrative AI endpoints, each with a body it accepts.
var aiPosts = []struct {
	path string
	body map[string]any
}{
	{"/ai/access/draft", map[string]any{"description": "Release managers deploy any app"}},
	{"/ai/policy/draft", map[string]any{"description": "Nobody may open a shell"}},
	{"/ai/audit/search", map[string]any{"question": "Who deleted an app?"}},
	{"/ai/reference/answer", map[string]any{"question": "How can I make a group?"}},
}

// A body that is not JSON is refused as unreadable, with an example of one
// that is, on every AI endpoint that takes a body.
func TestAIEndpointsRefuseAnUnreadableBody(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	withAI(t, i, "ai_anthropic", everything("anthropic"))

	paths := []struct{ method, path string }{{http.MethodPut, "/ai/functions/search_audit"}}
	for _, p := range aiPosts {
		paths = append(paths, struct{ method, path string }{http.MethodPost, p.path})
	}
	for _, p := range paths {
		got := i.raw(admin, p.method, p.path, "application/json", []byte(`{"adapter_id":`))
		require.Equal(t, http.StatusBadRequest, got.Code, "%s %s: %s", p.method, p.path, got.String())
		require.Equal(t, errs.ValidInvalid, errs.Code(got.ErrorCode()), got.String())
		require.Contains(t, got.String(), "could not be read", got.String())
		var env struct {
			Remedy string `json:"remedy"`
		}
		got.JSON(t, &env)
		require.Contains(t, env.Remedy, "Send ", "the remedy shows a body that works")
	}
}

// TestR106_AIEndpointsWithoutAssistanceSayItIsNotSetUp asserts R-106: an
// install with no AI service answers each function with a clear adapter
// error, not a crash.
func TestR106_AIEndpointsWithoutAssistanceSayItIsNotSetUp(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	i.Server.Assist = nil

	for _, p := range aiPosts {
		got := i.do(admin, http.MethodPost, p.path, p.body)
		require.Equal(t, http.StatusBadGateway, got.Code, "%s: %s", p.path, got.String())
		require.Equal(t, errs.AdapterUnavailable, errs.Code(got.ErrorCode()), got.String())
		require.Contains(t, got.String(), "AI assistance is not set up")
	}
}

// Without the assignment service, listing and changing assignments is an
// internal error rather than a nil dereference.
func TestAIFunctionEndpointsWithoutAssignmentsAreAnInternalError(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	i.Server.AIFunctions = nil

	for _, got := range []reply{
		i.do(admin, http.MethodGet, "/ai/functions", nil),
		i.do(admin, http.MethodPut, "/ai/functions/search_audit", map[string]any{"adapter_id": "ai_anthropic"}),
		i.do(admin, http.MethodDelete, "/ai/functions/search_audit", nil),
	} {
		require.Equal(t, http.StatusInternalServerError, got.Code, got.String())
		require.Contains(t, got.String(), "AI function assignment is not set up")
	}
}

// Every AI endpoint needs a signed-in caller.
func TestAIEndpointsRefuseAnonymous(t *testing.T) {
	i := newInstall(t)
	withAI(t, i, "ai_anthropic", everything("anthropic"))

	for _, got := range []reply{
		i.anon(http.MethodGet, "/ai/functions", nil),
		i.anon(http.MethodPut, "/ai/functions/search_audit", map[string]any{"adapter_id": "ai_anthropic"}),
		i.anon(http.MethodDelete, "/ai/functions/search_audit", nil),
	} {
		require.Equal(t, http.StatusUnauthorized, got.Code, got.String())
	}
	for _, p := range aiPosts {
		got := i.anon(http.MethodPost, p.path, p.body)
		require.Equal(t, http.StatusUnauthorized, got.Code, "%s: %s", p.path, got.String())
	}
}

// An adapter that is reached and fails surfaces as ADAPTER_FAILED with its
// reason, and nothing is recorded as sent.
func TestAIAdapterFailureSurfacesAsAdapterFailed(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	require.NoError(t, i.Server.Registry.Register("ai_anthropic", failingAI{everything("anthropic")}))
	assignAll(t, i, admin, "ai_anthropic")

	for _, p := range aiPosts {
		got := i.do(admin, http.MethodPost, p.path, p.body)
		require.Equal(t, http.StatusBadGateway, got.Code, "%s: %s", p.path, got.String())
		require.Equal(t, errs.AdapterFailed, errs.Code(got.ErrorCode()), got.String())
		require.Contains(t, got.String(), "529 overloaded")
	}
	for _, action := range []string{"ai.draft_access", "ai.draft_policy", "ai.search_audit", "ai.answer_reference"} {
		require.Empty(t, auditActions(t, i, admin, action), action)
	}
}

// An empty question is refused before anything is sent.
func TestAIQuestionsMustSaySomething(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	withAI(t, i, "ai_anthropic", everything("anthropic"))
	assignAll(t, i, admin, "ai_anthropic")

	for _, path := range []string{"/ai/audit/search", "/ai/reference/answer"} {
		got := i.do(admin, http.MethodPost, path, map[string]any{"question": "  "})
		require.Equal(t, http.StatusBadRequest, got.Code, "%s: %s", path, got.String())
		require.Equal(t, errs.ValidInvalid, errs.Code(got.ErrorCode()), got.String())
	}
}

// An audit search is recorded with how many records were sent to the
// adapter, beside the adapter and model; assigning and unassigning a
// function are recorded too.
func TestAIAuditSearchRecordsWhatWasSent(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	ai := withAI(t, i, "ai_anthropic", everything("anthropic"))
	assignAll(t, i, admin, "ai_anthropic")
	i.createApp(admin, "notes")

	ai.search = adapterapi.AuditSearch{Filter: adapterapi.AuditFilter{Actions: []string{"app.create"}}}
	ai.summary = adapterapi.AuditSummary{Summary: "One app was created."}
	got := i.do(admin, http.MethodPost, "/ai/audit/search", map[string]any{"question": "What was created?"})
	require.Equal(t, http.StatusOK, got.Code, got.String())

	events := auditActions(t, i, admin, "ai.search_audit")
	require.Len(t, events, 1)
	detail, _ := events[0]["detail"].(map[string]any)
	require.Equal(t, float64(1), detail["records_sent"], events[0])
	require.Equal(t, "ai_anthropic", detail["adapter"])
	require.Equal(t, "anthropic-default", detail["model"])
	require.NotContains(t, detail, "question", "the content is never recorded")

	require.Equal(t, http.StatusOK, i.do(admin, http.MethodDelete, "/ai/functions/search_audit", nil).Code)
	require.NotEmpty(t, auditActions(t, i, admin, "ai.function.assign"))
	unassigned := auditActions(t, i, admin, "ai.function.unassign")
	require.Len(t, unassigned, 1)
	require.Equal(t, "search_audit", unassigned[0]["target_id"], unassigned[0])
}
