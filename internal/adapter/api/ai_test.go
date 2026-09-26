package api_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

type stubAI struct{ base }

func (stubAI) Capabilities(context.Context) (api.AICapabilities, error) {
	return api.AICapabilities{Functions: []api.AIFunction{api.AIFunctionRepairPlan}}, nil
}

func (stubAI) RepairPlan(context.Context, api.ScreenRequest) (api.ScreenResult, error) {
	return api.ScreenResult{}, nil
}

func (stubAI) AnswerQuestions(context.Context, api.ScreenRequest) (api.ScreenResult, error) {
	return api.ScreenResult{}, nil
}

func (stubAI) RevisePlan(context.Context, api.ScreenRequest) (api.ScreenResult, error) {
	return api.ScreenResult{}, nil
}

func (stubAI) DraftAccess(context.Context, api.AccessRequest) (api.AccessDraft, error) {
	return api.AccessDraft{}, nil
}

func (stubAI) DraftPolicy(context.Context, api.PolicyRequest) (api.PolicyDraft, error) {
	return api.PolicyDraft{}, nil
}

func (stubAI) SearchAudit(context.Context, api.AuditSearchRequest) (api.AuditSearch, error) {
	return api.AuditSearch{}, nil
}

func (stubAI) SummarizeAudit(context.Context, api.AuditSummaryRequest) (api.AuditSummary, error) {
	return api.AuditSummary{}, nil
}

func (stubAI) AnswerReference(context.Context, api.ReferenceRequest) (api.ReferenceAnswer, error) {
	return api.ReferenceAnswer{}, nil
}

// TestR258_AIIsAnAdapterCategory asserts R-258.
//
// Registered and looked up like the other eight, and rejected at registration
// when it does not implement its category's interface — rather than surfacing
// later as "not configured" when in fact it is.
func TestR258_AIIsAnAdapterCategory(t *testing.T) {
	r := api.NewRegistry()
	require.NoError(t, r.Register("ai_anthropic", stubAI{base{kind: "anthropic", cat: api.CategoryAI}}))

	ai, ok := r.AI("ai_anthropic")
	require.True(t, ok)
	require.NotNil(t, ai)

	// A notify adapter claiming to be an AI one is refused loudly.
	err := r.Register("ai_bogus", stubNotify{base{kind: "console", cat: api.CategoryAI}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ai adapter interface")
}

// TestR259_UnassignedFunctionIsOff asserts R-259 and R-335.
//
// A function nobody is assigned is off, which is an ordinary outcome: an
// install with no AI adapter is not a degraded install. Configuring an adapter
// does not assign it anything by itself.
func TestR259_UnassignedFunctionIsOff(t *testing.T) {
	_, _, found := api.NewRegistry().AIFor(api.AIFunctionRepairPlan)
	require.False(t, found)

	r := api.NewRegistry()
	require.NoError(t, r.Register("ai_anthropic", stubAI{base{kind: "anthropic", cat: api.CategoryAI}}))
	_, _, found = r.AIFor(api.AIFunctionRepairPlan)
	require.False(t, found)
}

// TestR259_EachFunctionGoesToItsAssignedAdapter asserts R-259: two adapters,
// each handling different functions, each on its own model.
func TestR259_EachFunctionGoesToItsAssignedAdapter(t *testing.T) {
	r := api.NewRegistry()
	require.NoError(t, r.Register("ai_anthropic", stubAI{base{kind: "anthropic", cat: api.CategoryAI}}))
	require.NoError(t, r.Register("ai_openai", stubAI{base{kind: "openai", cat: api.CategoryAI}}))
	r.SetAIAssignments([]api.AIAssignment{
		{Function: api.AIFunctionRepairPlan, AdapterRef: "ai_anthropic"},
		{Function: api.AIFunctionSearchAudit, AdapterRef: "ai_openai", Model: "small"},
	})

	_, a, found := r.AIFor(api.AIFunctionRepairPlan)
	require.True(t, found)
	require.Equal(t, "ai_anthropic", a.AdapterRef)
	require.Empty(t, a.Model)

	_, a, found = r.AIFor(api.AIFunctionSearchAudit)
	require.True(t, found)
	require.Equal(t, "ai_openai", a.AdapterRef)
	require.Equal(t, "small", a.Model)

	_, _, found = r.AIFor(api.AIFunctionDraftPolicy)
	require.False(t, found)
}

// TestR259_AssignmentToAnAdapterNotRunningIsOff asserts R-259 and R-335: an
// assignment whose adapter failed to configure leaves the function off rather
// than failing its callers.
func TestR259_AssignmentToAnAdapterNotRunningIsOff(t *testing.T) {
	r := api.NewRegistry()
	r.SetAIAssignments([]api.AIAssignment{{Function: api.AIFunctionRepairPlan, AdapterRef: "ai_gone"}})
	_, a, found := r.AIFor(api.AIFunctionRepairPlan)
	require.False(t, found)
	require.Equal(t, "ai_gone", a.AdapterRef)
}
