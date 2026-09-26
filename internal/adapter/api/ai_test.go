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

// TestR258_AIIsAnAdapterCategory asserts R-258.
//
// Registered and looked up like the other eight, and rejected at registration
// when it does not implement its category's interface — rather than surfacing
// later as "not configured" when in fact it is.
func TestR258_AIIsAnAdapterCategory(t *testing.T) {
	r := api.NewRegistry()
	require.NoError(t, r.Register("ai_anthropic", stubAI{base{kind: "anthropic", cat: api.CategoryAI}}))
	require.NoError(t, r.SetDefault(api.CategoryAI, "ai_anthropic"))

	ai, ref, found := r.DefaultAI()
	require.True(t, found)
	require.Equal(t, "ai_anthropic", ref)
	require.NotNil(t, ai)

	// A notify adapter claiming to be an AI one is refused loudly.
	err := r.Register("ai_bogus", stubNotify{base{kind: "console", cat: api.CategoryAI}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "ai adapter interface")
}

// TestR258_NoAIAdapterIsAnOrdinaryOutcome asserts R-258 and R-335.
//
// Every caller of DefaultAI has to be written so that "none" is the normal
// case: an install with no AI adapter is not a degraded install.
func TestR258_NoAIAdapterIsAnOrdinaryOutcome(t *testing.T) {
	_, _, found := api.NewRegistry().DefaultAI()
	require.False(t, found)
}

// TestR258_AnAIAdapterConfiguredWithoutBeingMarkedDefaultIsStillFound asserts
// R-258: an install that configured one should not have to know about a
// default flag for it to be used.
func TestR258_AnAIAdapterConfiguredWithoutBeingMarkedDefaultIsStillFound(t *testing.T) {
	r := api.NewRegistry()
	require.NoError(t, r.Register("ai_anthropic", stubAI{base{kind: "anthropic", cat: api.CategoryAI}}))

	_, ref, found := r.DefaultAI()
	require.True(t, found)
	require.Equal(t, "ai_anthropic", ref)
}
