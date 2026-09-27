package address

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/adapter/api"
	"github.com/trypando/pando/internal/core/spec"
	"github.com/trypando/pando/internal/errs"
)

type silentRouting struct{ api.RoutingAdapter }

func (silentRouting) Kind() string                                     { return "stub" }
func (silentRouting) Category() api.Category                           { return api.CategoryRouting }
func (silentRouting) Configure(context.Context, json.RawMessage) error { return nil }
func (silentRouting) HealthCheck(context.Context) error                { return nil }
func (silentRouting) Capabilities(context.Context) (api.RoutingCapabilities, error) {
	return api.RoutingCapabilities{}, errors.New("unreachable")
}

type failingPorts struct{}

func (failingPorts) Allocate(context.Context, string, string, int, int) (int, error) {
	return 0, errs.New(errs.CapacityNoFreePort, "Every port in 9000-9019 is taken.")
}

// TestR261_TheChoicesAreEveryAdapterThatCanSay: what the console offers comes
// from the adapters, with the install's domain filling in for one that has
// none, and an adapter that cannot answer is left out rather than offered.
func TestR261_TheChoicesAreEveryAdapterThatCanSay(t *testing.T) {
	s, _ := service(t)
	require.NoError(t, s.Registry.Register("rte_quiet", silentRouting{}))

	got := s.Options(context.Background())
	require.Len(t, got, 2)
	byRef := map[string]Option{}
	for _, o := range got {
		byRef[o.AdapterRef] = o
	}
	require.True(t, byRef["rte_cf"].IsDefault)
	require.Equal(t, "bemeek.io", byRef["rte_cf"].BaseDomain)
	require.Equal(t, "localtest.me", byRef["rte_loopback"].BaseDomain, "the install's domain, where the adapter has none")

	require.Nil(t, (&Service{}).Options(context.Background()))
}

func TestAnAdapterThatCannotSayWhatItServesIsRefused(t *testing.T) {
	s, _ := service(t)
	require.NoError(t, s.Registry.Register("rte_quiet", silentRouting{}))
	_, err := s.Resolve(context.Background(), "app_1", "notes", onLoopback, Request{AdapterRef: "rte_quiet"})
	require.Equal(t, errs.AdapterUnavailable, errs.CodeOf(err))

	quiet := spec.Routing{AdapterRef: "rte_quiet", Mode: spec.RoutingPath}
	_, err = s.Overrides(context.Background(), nil, &quiet)
	require.Equal(t, errs.AdapterUnavailable, errs.CodeOf(err))

	unknown := spec.Routing{AdapterRef: "rte_nowhere", Mode: spec.RoutingPath}
	over, err := s.Overrides(context.Background(), nil, &unknown)
	require.NoError(t, err, "an unknown adapter is validation's and the planner's to name")
	require.False(t, over)
}

func TestWithNoAdapterNamedTheDefaultIsUsed(t *testing.T) {
	s, _ := service(t)
	d, err := s.Resolve(context.Background(), "app_1", "notes", spec.Routing{}, Request{})
	require.NoError(t, err)
	require.Equal(t, "rte_cf", d.Routing.AdapterRef)
	require.Equal(t, "notes.bemeek.io", d.Routing.Hostname)
}

func TestAHostnameWithNowhereToComeFromIsAskedFor(t *testing.T) {
	s, _ := service(t)
	s.BaseDomain = ""
	require.NoError(t, s.Registry.Register("rte_bare", routingStub{caps: api.RoutingCapabilities{
		Modes: []spec.RoutingMode{spec.RoutingSubdomain}, DefaultMode: spec.RoutingSubdomain}}))
	_, err := s.Resolve(context.Background(), "app_1", "notes", onLoopback, Request{AdapterRef: "rte_bare"})
	require.ErrorContains(t, err, "no base domain")
}

func TestAPortThatCannotBeHadSaysWhy(t *testing.T) {
	s, _ := service(t)
	cf := spec.Routing{AdapterRef: "rte_cf", Mode: spec.RoutingSubdomain, Hostname: "n.bemeek.io"}

	s.Ports = failingPorts{}
	_, err := s.Resolve(context.Background(), "app_1", "notes", cf, Request{AdapterRef: "rte_loopback"})
	require.Equal(t, errs.CapacityNoFreePort, errs.CodeOf(err))

	s.Ports = nil
	_, err = s.Resolve(context.Background(), "app_1", "notes", cf, Request{AdapterRef: "rte_loopback"})
	require.Equal(t, errs.Internal, errs.CodeOf(err))
}

func TestModesAreDescribedInWords(t *testing.T) {
	require.Equal(t, "at their own hostname", describeMode(spec.RoutingSubdomain))
	require.Equal(t, "under a path", describeMode(spec.RoutingPath))
	require.Equal(t, "on their own port", describeMode(spec.RoutingPort))
	require.Equal(t, `in "carrier-pigeon" mode`, describeMode("carrier-pigeon"))
}
