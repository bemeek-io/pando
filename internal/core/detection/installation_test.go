package detection_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/detection"
	"github.com/bemeek-io/pando/internal/core/spec"
)

// routingStub declares a default mode, which is the whole question R-162 puts
// to a routing adapter.
type routingStub struct {
	mode spec.RoutingMode
	err  error
}

func (routingStub) Kind() string                                     { return "stub" }
func (routingStub) Category() api.Category                           { return api.CategoryRouting }
func (routingStub) Configure(context.Context, json.RawMessage) error { return nil }
func (routingStub) HealthCheck(context.Context) error                { return nil }
func (r routingStub) Capabilities(context.Context) (api.RoutingCapabilities, error) {
	if r.err != nil {
		return api.RoutingCapabilities{}, r.err
	}
	return api.RoutingCapabilities{Modes: []api.RoutingMode{r.mode}, DefaultMode: r.mode}, nil
}
func (routingStub) Ensure(context.Context, api.RouteRequest) (api.RouteHandle, error) {
	return api.RouteHandle{}, nil
}
func (routingStub) Remove(context.Context, api.RouteHandle) error { return nil }
func (routingStub) Observe(context.Context, api.RouteHandle) (api.RouteState, error) {
	return api.RouteState{}, nil
}

// runtimeStub and builderStub only have to exist and be registrable: the
// embedded interface supplies the methods nothing here calls, and the four
// declared below are the ones registration checks.
type runtimeStub struct{ api.RuntimeAdapter }

func (runtimeStub) Kind() string                                     { return "stub" }
func (runtimeStub) Category() api.Category                           { return api.CategoryRuntime }
func (runtimeStub) Configure(context.Context, json.RawMessage) error { return nil }
func (runtimeStub) HealthCheck(context.Context) error                { return nil }

type builderStub struct{ api.BuilderAdapter }

func (builderStub) Kind() string                                     { return "stub" }
func (builderStub) Category() api.Category                           { return api.CategoryBuilder }
func (builderStub) Configure(context.Context, json.RawMessage) error { return nil }
func (builderStub) HealthCheck(context.Context) error                { return nil }

func TestDefaultsWithNoRegistryFallsBackToTheStandardValues(t *testing.T) {
	i := detection.NewInstallation(nil, "apps.example")

	d := i.Defaults(context.Background())
	require.Equal(t, "apps.example", d.BaseDomain)
	require.Equal(t, spec.StandardDefaults().RoutingMode, d.RoutingMode)
	require.Empty(t, d.RuntimeAdapter, "nothing configured means nothing is claimed")
	require.Empty(t, d.RoutingAdapter)
	require.Empty(t, d.BuilderAdapter)
}

func TestDefaultsTakeEachCategorysConfiguredDefault(t *testing.T) {
	r := api.NewRegistry()
	require.NoError(t, r.Register("rt_docker_local",
		runtimeStub{}))
	require.NoError(t, r.Register("bld_buildkit",
		builderStub{}))
	require.NoError(t, r.Register("rte_loopback", routingStub{mode: spec.RoutingPort}))

	require.NoError(t, r.SetDefault(api.CategoryRuntime, "rt_docker_local"))
	require.NoError(t, r.SetDefault(api.CategoryBuilder, "bld_buildkit"))
	require.NoError(t, r.SetDefault(api.CategoryRouting, "rte_loopback"))

	d := detection.NewInstallation(r, "apps.example").Defaults(context.Background())

	require.Equal(t, "rt_docker_local", d.RuntimeAdapter)
	require.Equal(t, "bld_buildkit", d.BuilderAdapter)
	require.Equal(t, "rte_loopback", d.RoutingAdapter)
}

// R-162: each adapter declares a default mode, and adding an app uses it
// without asking. That is what makes an install feel like proxy mode or
// per-hostname without either being a global setting.
func TestR162_TheRoutingModeComesFromTheAdapterNotFromConfiguration(t *testing.T) {
	for _, mode := range []spec.RoutingMode{spec.RoutingPort, spec.RoutingSubdomain, spec.RoutingPath} {
		r := api.NewRegistry()
		require.NoError(t, r.Register("rte_x", routingStub{mode: mode}))
		require.NoError(t, r.SetDefault(api.CategoryRouting, "rte_x"))

		d := detection.NewInstallation(r, "apps.example").Defaults(context.Background())
		require.Equal(t, mode, d.RoutingMode)
	}
}

// A mode the adapter does not advertise would fail the planner's capability
// check (R-254) with an error about something nobody chose — so an adapter that
// cannot be reached leaves the fallback in place rather than guessing.
func TestAnUnreachableRoutingAdapterLeavesTheFallbackMode(t *testing.T) {
	r := api.NewRegistry()
	require.NoError(t, r.Register("rte_down", routingStub{err: errors.New("connection refused")}))
	require.NoError(t, r.SetDefault(api.CategoryRouting, "rte_down"))

	d := detection.NewInstallation(r, "apps.example").Defaults(context.Background())
	require.Equal(t, "rte_down", d.RoutingAdapter, "the ref is still the install's")
	require.Equal(t, spec.StandardDefaults().RoutingMode, d.RoutingMode)
}

func TestAnAdapterThatDeclaresNoDefaultModeLeavesTheFallback(t *testing.T) {
	r := api.NewRegistry()
	require.NoError(t, r.Register("rte_quiet", routingStub{mode: ""}))
	require.NoError(t, r.SetDefault(api.CategoryRouting, "rte_quiet"))

	d := detection.NewInstallation(r, "apps.example").Defaults(context.Background())
	require.Equal(t, spec.StandardDefaults().RoutingMode, d.RoutingMode)
}

func TestARegistryWithNoDefaultsClaimsNoAdapters(t *testing.T) {
	r := api.NewRegistry()
	require.NoError(t, r.Register("rte_loopback", routingStub{mode: spec.RoutingPort}))

	// Registered but not defaulted: an install has to say which one it means.
	d := detection.NewInstallation(r, "apps.example").Defaults(context.Background())
	require.Empty(t, d.RoutingAdapter)
	require.Equal(t, spec.StandardDefaults().RoutingMode, d.RoutingMode)
}

func TestAnOverriddenFallbackIsUsedForWhatAdaptersDoNotAnswer(t *testing.T) {
	i := detection.NewInstallation(nil, "apps.example")
	i.Fallback.Resources.CPUMillis = 2000
	i.Fallback.BuildTimeoutSeconds = 60

	d := i.Defaults(context.Background())
	require.Equal(t, 2000, d.Resources.CPUMillis)
	require.Equal(t, 60, d.BuildTimeoutSeconds)
	require.Equal(t, "apps.example", d.BaseDomain)
}

func TestTheInstallationSatisfiesTheInterfaceDetectionAsksFor(t *testing.T) {
	var _ detection.Installation = detection.NewInstallation(nil, "")
}
