//go:build integration

package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	adapterapi "github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/config"
	"github.com/bemeek-io/pando/internal/errs"
)

// fakeAI is an AI adapter whose answers a test sets, and which records what it
// was asked.
type fakeAI struct {
	kind string
	caps adapterapi.AICapabilities

	access    adapterapi.AccessDraft
	policy    adapterapi.PolicyDraft
	search    adapterapi.AuditSearch
	summary   adapterapi.AuditSummary
	reference adapterapi.ReferenceAnswer

	gotAccess    adapterapi.AccessRequest
	gotPolicy    adapterapi.PolicyRequest
	gotSearch    adapterapi.AuditSearchRequest
	gotSummary   adapterapi.AuditSummaryRequest
	gotReference adapterapi.ReferenceRequest
}

func (f *fakeAI) Kind() string                                                    { return f.kind }
func (f *fakeAI) Category() adapterapi.Category                                   { return adapterapi.CategoryAI }
func (f *fakeAI) Configure(context.Context, json.RawMessage) error                { return nil }
func (f *fakeAI) HealthCheck(context.Context) error                               { return nil }
func (f *fakeAI) Capabilities(context.Context) (adapterapi.AICapabilities, error) { return f.caps, nil }

func (f *fakeAI) RepairPlan(context.Context, adapterapi.ScreenRequest) (adapterapi.ScreenResult, error) {
	return adapterapi.ScreenResult{}, nil
}

func (f *fakeAI) AnswerQuestions(context.Context, adapterapi.ScreenRequest) (adapterapi.ScreenResult, error) {
	return adapterapi.ScreenResult{}, nil
}

func (f *fakeAI) RevisePlan(context.Context, adapterapi.ScreenRequest) (adapterapi.ScreenResult, error) {
	return adapterapi.ScreenResult{}, nil
}

func (f *fakeAI) DraftAccess(_ context.Context, req adapterapi.AccessRequest) (adapterapi.AccessDraft, error) {
	f.gotAccess = req
	return f.access, nil
}

func (f *fakeAI) DraftPolicy(_ context.Context, req adapterapi.PolicyRequest) (adapterapi.PolicyDraft, error) {
	f.gotPolicy = req
	return f.policy, nil
}

func (f *fakeAI) SearchAudit(_ context.Context, req adapterapi.AuditSearchRequest) (adapterapi.AuditSearch, error) {
	f.gotSearch = req
	return f.search, nil
}

func (f *fakeAI) SummarizeAudit(_ context.Context, req adapterapi.AuditSummaryRequest) (adapterapi.AuditSummary, error) {
	f.gotSummary = req
	return f.summary, nil
}

func (f *fakeAI) AnswerReference(_ context.Context, req adapterapi.ReferenceRequest) (adapterapi.ReferenceAnswer, error) {
	f.gotReference = req
	return f.reference, nil
}

// everything is an adapter that performs every function and chooses models.
func everything(kind string) *fakeAI {
	return &fakeAI{kind: kind, caps: adapterapi.AICapabilities{
		Functions: adapterapi.AIFunctions(), Model: kind + "-default", ChoosesModel: true,
	}}
}

func withAI(t *testing.T, i *install, ref string, ai *fakeAI) *fakeAI {
	t.Helper()
	require.NoError(t, i.Server.Registry.Register(ref, ai))
	return ai
}

type aiFunction struct {
	Function       string         `json:"function"`
	AdapterID      string         `json:"adapter_id"`
	Model          string         `json:"model"`
	EffectiveModel string         `json:"effective_model"`
	On             bool           `json:"on"`
	Off            string         `json:"off"`
	Source         *config.Source `json:"source"`
}

func functionsOf(t *testing.T, i *install, s *session) map[string]aiFunction {
	t.Helper()
	got := i.do(s, http.MethodGet, "/ai/functions", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	var body struct {
		Functions []aiFunction `json:"functions"`
	}
	got.JSON(t, &body)
	out := map[string]aiFunction{}
	for _, f := range body.Functions {
		out[f.Function] = f
	}
	return out
}

// TestR259_FunctionAssignedToOneAdapter asserts R-259: one adapter handles any
// number of functions, each function has one adapter, and assigning a
// function another adapter handles is refused with a message that says how to
// move it.
func TestR259_FunctionAssignedToOneAdapter(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	withAI(t, i, "ai_anthropic", everything("anthropic"))
	withAI(t, i, "ai_openai", everything("openai"))

	// Off until assigned: configuring an adapter assigns it nothing.
	fns := functionsOf(t, i, admin)
	require.Len(t, fns, len(adapterapi.AIFunctions()))
	require.False(t, fns["search_audit"].On)
	require.Contains(t, fns["search_audit"].Off, "not assigned")

	for _, fn := range []string{"repair_plan", "answer_questions", "revise_plan"} {
		got := i.do(admin, http.MethodPut, "/ai/functions/"+fn, map[string]any{"adapter_id": "ai_anthropic"})
		require.Equal(t, http.StatusOK, got.Code, got.String())
	}
	got := i.do(admin, http.MethodPut, "/ai/functions/search_audit", map[string]any{"adapter_id": "ai_openai"})
	require.Equal(t, http.StatusOK, got.Code, got.String())

	fns = functionsOf(t, i, admin)
	require.True(t, fns["repair_plan"].On)
	require.Equal(t, "ai_anthropic", fns["repair_plan"].AdapterID)
	require.Equal(t, "anthropic-default", fns["repair_plan"].EffectiveModel)
	require.Equal(t, "ai_openai", fns["search_audit"].AdapterID)

	// The registry is what callers ask, and it changed at once.
	_, a, ok := i.Server.Registry.AIFor(adapterapi.AIFunctionSearchAudit)
	require.True(t, ok)
	require.Equal(t, "ai_openai", a.AdapterRef)

	// A second adapter for a function one already handles is refused.
	taken := i.do(admin, http.MethodPut, "/ai/functions/search_audit", map[string]any{"adapter_id": "ai_anthropic"})
	require.Equal(t, http.StatusConflict, taken.Code, taken.String())
	require.Equal(t, errs.StateAIFunctionAssigned, errs.Code(taken.ErrorCode()))
	require.Contains(t, taken.String(), "Audit search is already handled by the adapter ai_openai")
	require.Contains(t, taken.String(), "remove it from the adapter ai_openai first")

	// …by the database, not only by the handler.
	_, err := i.db.Exec(context.Background(),
		`INSERT INTO ai_assignments (function, adapter_id, updated_by) VALUES ('search_audit', 'ai_anthropic', 'test')`)
	require.Error(t, err)

	// Moving it is remove, then assign.
	require.Equal(t, http.StatusOK, i.do(admin, http.MethodDelete, "/ai/functions/search_audit", nil).Code)
	moved := i.do(admin, http.MethodPut, "/ai/functions/search_audit", map[string]any{"adapter_id": "ai_anthropic"})
	require.Equal(t, http.StatusOK, moved.Code, moved.String())

	// Changing the model of an adapter's own assignment is not a move.
	again := i.do(admin, http.MethodPut, "/ai/functions/search_audit", map[string]any{"adapter_id": "ai_anthropic", "model": "small"})
	require.Equal(t, http.StatusOK, again.Code, again.String())

	// Reading is install.view; changing is install.adapters.manage.
	someone := i.user("someone")
	require.Equal(t, http.StatusForbidden,
		i.do(someone, http.MethodPut, "/ai/functions/draft_access", map[string]any{"adapter_id": "ai_anthropic"}).Code)
}

// TestR259_AssignmentModelOverride asserts R-259: an assignment names its
// own model only on an adapter that advertises model choice, from its list
// when it has one.
func TestR259_AssignmentModelOverride(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	withAI(t, i, "ai_anthropic", everything("anthropic"))
	fixed := everything("fixed")
	fixed.caps.ChoosesModel = false
	withAI(t, i, "ai_fixed", fixed)
	listed := everything("listed")
	listed.caps.Models = []string{"small", "large"}
	withAI(t, i, "ai_listed", listed)

	got := i.do(admin, http.MethodPut, "/ai/functions/answer_reference",
		map[string]any{"adapter_id": "ai_anthropic", "model": "claude-haiku-4-5"})
	require.Equal(t, http.StatusOK, got.Code, got.String())
	fns := functionsOf(t, i, admin)
	require.Equal(t, "claude-haiku-4-5", fns["answer_reference"].Model)
	require.Equal(t, "claude-haiku-4-5", fns["answer_reference"].EffectiveModel)

	refused := i.do(admin, http.MethodPut, "/ai/functions/search_audit",
		map[string]any{"adapter_id": "ai_fixed", "model": "anything"})
	require.Equal(t, http.StatusBadRequest, refused.Code, refused.String())
	require.Contains(t, refused.String(), "runs one model")

	notListed := i.do(admin, http.MethodPut, "/ai/functions/search_audit",
		map[string]any{"adapter_id": "ai_listed", "model": "medium"})
	require.Equal(t, http.StatusBadRequest, notListed.Code, notListed.String())
	require.Contains(t, notListed.String(), "small, large")
}

// TestR259_OnlyAdvertisedFunctionsCanBeAssigned asserts R-259 and R-254: a
// function the adapter's capabilities leave out is refused when assigned,
// rather than failing when called.
func TestR259_OnlyAdvertisedFunctionsCanBeAssigned(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()
	narrow := everything("narrow")
	narrow.caps.Functions = []adapterapi.AIFunction{adapterapi.AIFunctionRepairPlan}
	withAI(t, i, "ai_narrow", narrow)

	got := i.do(admin, http.MethodPut, "/ai/functions/search_audit", map[string]any{"adapter_id": "ai_narrow"})
	require.Equal(t, http.StatusBadRequest, got.Code, got.String())
	require.Contains(t, got.String(), "does not perform audit search")

	unknown := i.do(admin, http.MethodPut, "/ai/functions/read_minds", map[string]any{"adapter_id": "ai_narrow"})
	require.Equal(t, http.StatusBadRequest, unknown.Code, unknown.String())

	missing := i.do(admin, http.MethodPut, "/ai/functions/repair_plan", map[string]any{"adapter_id": "ai_nowhere"})
	require.Equal(t, http.StatusConflict, missing.Code, missing.String())
	require.Contains(t, missing.String(), "restart")
}

// TestR259_OneAIAdapterPerProvider asserts R-259: a second AI adapter of a
// provider the install already has is refused, by the database.
func TestR259_OneAIAdapterPerProvider(t *testing.T) {
	i := newInstall(t)
	admin := i.admin()

	first := i.do(admin, http.MethodPost, "/adapters", map[string]any{
		"id": "ai_anthropic", "category": "ai", "kind": "anthropic", "name": "Anthropic",
	})
	require.Equal(t, http.StatusCreated, first.Code, first.String())

	second := i.do(admin, http.MethodPost, "/adapters", map[string]any{
		"id": "ai_anthropic_2", "category": "ai", "kind": "anthropic", "name": "Anthropic again",
	})
	require.Equal(t, http.StatusBadRequest, second.Code, second.String())
	require.Contains(t, second.String(), "one AI adapter per provider")

	// Changing the one there is stays allowed.
	same := i.do(admin, http.MethodPost, "/adapters", map[string]any{
		"id": "ai_anthropic", "category": "ai", "kind": "anthropic", "name": "Claude",
	})
	require.Equal(t, http.StatusCreated, same.Code, same.String())
}

// TestR271_ConfigDeclaredAdaptersAndAssignmentsAreReadOnly asserts R-271:
// what the config file declares is reported with its source and cannot be
// changed through the API while it is declared.
func TestR271_ConfigDeclaredAdaptersAndAssignmentsAreReadOnly(t *testing.T) {
	src := func(key string) config.Source {
		return config.Source{Kind: "file", Name: "/etc/pando/pando.yaml", Key: key}
	}
	startup := &config.Config{File: "/etc/pando/pando.yaml", Adapters: []config.AdapterDecl{{
		ID: "ai_openai", Category: "ai", Kind: "openai", Name: "OpenAI", Enabled: true,
		Credentials: map[string]config.CredentialRef{"api_key": {Env: "OPENAI_API_KEY"}},
		Functions: []config.FunctionDecl{{Function: "search_audit", Model: "small",
			Source: src("adapters.ai_openai.functions.search_audit")}},
		Source: src("adapters.ai_openai"),
	}}}
	i := newInstallWith(t, nil, startup)
	admin := i.admin()
	withAI(t, i, "ai_openai", everything("openai"))
	withAI(t, i, "ai_anthropic", everything("anthropic"))
	require.NoError(t, i.Server.AIFunctions.Load(context.Background()))

	fns := functionsOf(t, i, admin)
	require.True(t, fns["search_audit"].On)
	require.Equal(t, "small", fns["search_audit"].Model)
	require.Equal(t, "adapters.ai_openai.functions.search_audit", fns["search_audit"].Source.Key)

	moved := i.do(admin, http.MethodPut, "/ai/functions/search_audit", map[string]any{"adapter_id": "ai_anthropic"})
	require.Equal(t, http.StatusConflict, moved.Code, moved.String())
	require.Equal(t, errs.StateSetAtStartup, errs.Code(moved.ErrorCode()))
	require.Contains(t, moved.String(), "/etc/pando/pando.yaml")
	require.Contains(t, moved.String(), "adapters.ai_openai.functions.search_audit")
	require.Equal(t, http.StatusConflict, i.do(admin, http.MethodDelete, "/ai/functions/search_audit", nil).Code)

	// Functions the file leaves alone are still managed here.
	free := i.do(admin, http.MethodPut, "/ai/functions/repair_plan", map[string]any{"adapter_id": "ai_openai"})
	require.Equal(t, http.StatusOK, free.Code, free.String())

	// The adapter itself is read-only, and so is its provider.
	changed := i.do(admin, http.MethodPost, "/adapters", map[string]any{"id": "ai_openai", "category": "ai", "kind": "anthropic"})
	require.Equal(t, errs.StateSetAtStartup, errs.Code(changed.ErrorCode()), changed.String())

	var list struct {
		Adapters []map[string]any `json:"adapters"`
	}
	got := i.do(admin, http.MethodGet, "/adapters", nil)
	require.Equal(t, http.StatusOK, got.Code, got.String())
	got.JSON(t, &list)
	var declared map[string]any
	for _, a := range list.Adapters {
		if a["id"] == "ai_openai" {
			declared = a
		}
	}
	require.NotNil(t, declared, got.String())
	require.Equal(t, true, declared["declared"])
	require.Equal(t, []any{"api_key"}, declared["credentials_set"])

	cfg := i.do(admin, http.MethodGet, "/config", nil)
	require.Equal(t, http.StatusOK, cfg.Code, cfg.String())
	require.Contains(t, cfg.String(), `"adapters.ai_openai.functions.search_audit"`)
	require.NotContains(t, cfg.String(), "OPENAI_API_KEY", "credentials by name only")
}
