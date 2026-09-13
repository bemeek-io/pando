package planner_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/planner"
	"github.com/bemeek-io/pando/internal/core/policy"
	"github.com/bemeek-io/pando/internal/core/spec"
)

type staticInventory []planner.InventoryApp

func (s staticInventory) LiveApps(context.Context) ([]planner.InventoryApp, error) {
	return s, nil
}

func appNamed(t *testing.T, name, sourceURL string, anonymous bool) planner.InventoryApp {
	t.Helper()
	s := plannableSpec()
	s.Source.URL = sourceURL
	return planner.InventoryApp{
		AppID: "app_" + name, Name: name, Spec: s, AnonymousGrant: anonymous,
	}
}

func previewer(t *testing.T, inv staticInventory) *planner.Planner {
	t.Helper()
	return planner.New(
		registry(t, capableRuntime(), capableRouting(), capableBuilder()),
		policy.Static(policy.Default()),
		fixedAllocations{},
	).WithInventory(inv)
}

// TestO10_APolicyNamesTheAppsItWouldBlockBeforeItIsSaved asserts design 05 §3.
//
// O-10 resolved policy application to "report now, block on next deploy". That
// is the right behaviour and an unusable experience on its own: an admin
// tightening a source allowlist is entitled to know it blocks four apps before
// they save it, not one deploy at a time.
func TestO10_APolicyNamesTheAppsItWouldBlockBeforeItIsSaved(t *testing.T) {
	inv := staticInventory{
		appNamed(t, "notes", "https://github.com/acme/notes", false),
		appNamed(t, "wiki", "https://gitlab.com/acme/wiki", false),
		appNamed(t, "blog", "https://github.com/acme/blog", false),
	}

	tightened := policy.Default()
	tightened.SourceAllowlist = []string{"github.com"}

	violations, err := previewer(t, inv).PreviewPolicy(context.Background(), tightened)
	require.NoError(t, err)

	require.Len(t, violations, 1, "only the GitLab app is blocked")
	require.Equal(t, "wiki", violations[0].AppName)
	require.NotEmpty(t, violations[0].Message, "R-105: the console shows what the deploy will say")
	require.NotEmpty(t, violations[0].Remedy)
}

// A policy that breaks nothing says so, and says it the same way.
//
// An empty list is the answer an admin most wants and must not be
// indistinguishable from a failure.
func TestAPolicyThatBlocksNothingReturnsAnEmptyList(t *testing.T) {
	inv := staticInventory{appNamed(t, "notes", "https://github.com/acme/notes", false)}

	violations, err := previewer(t, inv).PreviewPolicy(context.Background(), policy.Default())
	require.NoError(t, err)
	require.Empty(t, violations)
}

// TestR076_ForbiddingAnonymousGrantsNamesTheAppsAnyoneCanReach asserts R-076.
//
// There is no spec field to detect this in — the grant is the violation. An app
// that anyone on the internet can reach under a policy that says they may not
// is exactly the case "report now, block on next deploy" is least comfortable
// with, and the one an admin most needs named before they save.
func TestR076_ForbiddingAnonymousGrantsNamesTheAppsAnyoneCanReach(t *testing.T) {
	inv := staticInventory{
		appNamed(t, "status-page", "https://github.com/acme/status", true),
		appNamed(t, "admin", "https://github.com/acme/admin", false),
	}

	forbid := policy.Default()
	no := false
	forbid.AllowAnonymousGrants = &no

	violations, err := previewer(t, inv).PreviewPolicy(context.Background(), forbid)
	require.NoError(t, err)

	require.Len(t, violations, 1)
	require.Equal(t, "status-page", violations[0].AppName)
}

// TestR114_RaisingTheIsolationFloorNamesTheAppsThatCannotMeetIt asserts R-114.
func TestR114_RaisingTheIsolationFloorNamesTheAppsThatCannotMeetIt(t *testing.T) {
	inv := staticInventory{appNamed(t, "notes", "https://github.com/acme/notes", false)}

	raised := policy.Default()
	raised.MinRuntimeIsolation = spec.IsolationVM

	violations, err := previewer(t, inv).PreviewPolicy(context.Background(), raised)
	require.NoError(t, err)

	require.Len(t, violations, 1)
	require.Equal(t, "notes", violations[0].AppName)
}

// Previewing changes nothing — not the stored policy, and not any app.
//
// The whole value of the answer is that it is free to ask. A preview with a
// side effect is a save with a misleading name.
func TestPreviewSavesNothing(t *testing.T) {
	inv := staticInventory{appNamed(t, "notes", "https://gitlab.com/acme/notes", false)}
	p := previewer(t, inv)

	tightened := policy.Default()
	tightened.SourceAllowlist = []string{"github.com"}

	violations, err := p.PreviewPolicy(context.Background(), tightened)
	require.NoError(t, err)
	require.Len(t, violations, 1)

	// The planner still plans under the policy it was built with, not the one
	// it was just asked about.
	_, err = p.Check(context.Background(), inv[0].Spec)
	require.NoError(t, err, "the previewed policy was not adopted")
}

// A planner with no inventory refuses rather than reporting that nothing
// breaks. "Nothing breaks" and "nothing was checked" are opposite answers.
func TestPreviewWithoutAnInventoryRefuses(t *testing.T) {
	p := planner.New(
		registry(t, capableRuntime(), capableRouting(), capableBuilder()),
		policy.Static(policy.Default()),
		fixedAllocations{},
	)
	_, err := p.PreviewPolicy(context.Background(), policy.Default())
	require.Error(t, err)
}
