package api_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
)

// incomplete claims to be a runtime adapter but implements only the base
// interface — the shape a package ends up in when a method is added to
// RuntimeAdapter and one implementation is missed.
type incomplete struct{}

func (incomplete) Kind() string                                     { return "incomplete" }
func (incomplete) Category() api.Category                           { return api.CategoryRuntime }
func (incomplete) Configure(context.Context, json.RawMessage) error { return nil }
func (incomplete) HealthCheck(context.Context) error                { return nil }

// An adapter that does not satisfy its category's interface must be rejected at
// registration, loudly.
//
// Without this the typed accessors return (nil, false) on a type mismatch,
// which is indistinguishable from "not configured" — so a missed method would
// surface much later as the planner telling an operator their runtime is not
// configured when in fact it is.
func TestRegisterRejectsAnAdapterThatDoesNotSatisfyItsCategory(t *testing.T) {
	r := api.NewRegistry()

	err := r.Register("rt_broken", incomplete{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not implement the runtime adapter interface")
	require.Contains(t, err.Error(), "rt_broken", "the message names which adapter")

	_, ok := r.Get("rt_broken")
	require.False(t, ok, "a rejected adapter must not be registered")
}

func TestRegisterRejectsDuplicates(t *testing.T) {
	r := api.NewRegistry()
	require.NoError(t, r.Register("rte_one", &stubRouting{}))
	require.Error(t, r.Register("rte_one", &stubRouting{}))
}

func TestDefaultMustBeRegisteredAndInCategory(t *testing.T) {
	r := api.NewRegistry()
	require.NoError(t, r.Register("rte_one", &stubRouting{}))

	require.Error(t, r.SetDefault(api.CategoryRouting, "rte_missing"),
		"cannot default to something unregistered")
	require.Error(t, r.SetDefault(api.CategoryRuntime, "rte_one"),
		"cannot default a routing adapter as the runtime")

	require.NoError(t, r.SetDefault(api.CategoryRouting, "rte_one"))
	ref, ok := r.Default(api.CategoryRouting)
	require.True(t, ok)
	require.Equal(t, "rte_one", ref)
}

type stubRouting struct{}

func (stubRouting) Kind() string                                     { return "stub" }
func (stubRouting) Category() api.Category                           { return api.CategoryRouting }
func (stubRouting) Configure(context.Context, json.RawMessage) error { return nil }
func (stubRouting) HealthCheck(context.Context) error                { return nil }
func (stubRouting) Capabilities(context.Context) (api.RoutingCapabilities, error) {
	return api.RoutingCapabilities{}, nil
}
func (stubRouting) Ensure(context.Context, api.RouteRequest) (api.RouteHandle, error) {
	return api.RouteHandle{}, nil
}
func (stubRouting) Remove(context.Context, api.RouteHandle) error { return nil }
func (stubRouting) Observe(context.Context, api.RouteHandle) (api.RouteState, error) {
	return api.RouteState{}, nil
}
