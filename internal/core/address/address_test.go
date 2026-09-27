package address

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

type routingStub struct {
	api.RoutingAdapter
	caps api.RoutingCapabilities
}

func (routingStub) Kind() string                                     { return "stub" }
func (routingStub) Category() api.Category                           { return api.CategoryRouting }
func (routingStub) Configure(context.Context, json.RawMessage) error { return nil }
func (routingStub) HealthCheck(context.Context) error                { return nil }
func (r routingStub) Capabilities(context.Context) (api.RoutingCapabilities, error) {
	return r.caps, nil
}

type portsStub struct{ calls int }

func (p *portsStub) Allocate(context.Context, string, string, int, int) (int, error) {
	p.calls++
	return 9004, nil
}

// An install that started on loopback and now has Cloudflare as well.
func service(t *testing.T) (*Service, *portsStub) {
	t.Helper()
	reg := api.NewRegistry()
	require.NoError(t, reg.Register("rte_loopback", routingStub{caps: api.RoutingCapabilities{
		Modes: []spec.RoutingMode{spec.RoutingPort, spec.RoutingPath}, DefaultMode: spec.RoutingPort}}))
	require.NoError(t, reg.Register("rte_cf", routingStub{caps: api.RoutingCapabilities{
		Modes: []spec.RoutingMode{spec.RoutingSubdomain, spec.RoutingPath}, DefaultMode: spec.RoutingSubdomain,
		BaseDomain: "bemeek.io"}}))
	require.NoError(t, reg.SetDefault(api.CategoryRouting, "rte_cf"))
	ports := &portsStub{}
	return &Service{Registry: reg, Ports: ports, PortRangeStart: 9000, PortRangeEnd: 9019, BaseDomain: "localtest.me"}, ports
}

var onLoopback = spec.Routing{AdapterRef: "rte_loopback", Mode: spec.RoutingPort,
	ModeSource: spec.ModeFromAdapterDefault, Port: 9001}

// TestR162_MovingAnAppToAnotherAdapterTakesItsDefaults asserts R-162: moving
// an app from loopback to Cloudflare gives it Cloudflare's default mode and a
// hostname in its zone, and needs no override — it is the default.
func TestR162_MovingAnAppToAnotherAdapterTakesItsDefaults(t *testing.T) {
	s, _ := service(t)
	d, err := s.Resolve(context.Background(), "app_1", "notes", onLoopback, Request{AdapterRef: "rte_cf"})
	require.NoError(t, err)
	require.Equal(t, spec.Routing{AdapterRef: "rte_cf", Mode: spec.RoutingSubdomain,
		ModeSource: spec.ModeFromAdapterDefault, Hostname: "notes.bemeek.io"}, d.Routing)
	require.True(t, d.Changed)
	require.False(t, d.Override)
}

// TestR163_ANonDefaultModeIsAnOverride asserts R-163: choosing a mode the
// adapter does not default to is the deviation app.routing.override gates,
// and it is recorded as chosen.
func TestR163_ANonDefaultModeIsAnOverride(t *testing.T) {
	s, _ := service(t)
	d, err := s.Resolve(context.Background(), "app_1", "notes", onLoopback,
		Request{AdapterRef: "rte_cf", Mode: spec.RoutingPath})
	require.NoError(t, err)
	require.True(t, d.Override)
	require.Equal(t, spec.ModeFromUserOverride, d.Routing.ModeSource)
	require.Equal(t, "/notes", d.Routing.PathPrefix)

	// Staying on a mode the app already deviates to is not a new deviation.
	d, err = s.Resolve(context.Background(), "app_1", "notes", d.Routing, Request{Mode: spec.RoutingPath})
	require.NoError(t, err)
	require.False(t, d.Override)
	require.False(t, d.Changed)
}

func TestAHostnameCanBeChosenAndIsChecked(t *testing.T) {
	s, _ := service(t)
	current := spec.Routing{AdapterRef: "rte_cf", Mode: spec.RoutingSubdomain,
		ModeSource: spec.ModeFromAdapterDefault, Hostname: "notes.bemeek.io"}

	d, err := s.Resolve(context.Background(), "app_1", "notes", current, Request{Hostname: " Crew.bemeek.io. "})
	require.NoError(t, err)
	require.Equal(t, "crew.bemeek.io", d.Routing.Hostname)

	_, err = s.Resolve(context.Background(), "app_1", "notes", current, Request{Hostname: "not a host"})
	require.Error(t, err)

	d, err = s.Resolve(context.Background(), "app_1", "notes", current, Request{})
	require.NoError(t, err)
	require.False(t, d.Changed, "asking for nothing new changes nothing")
}

// TestAPortIsAllocatedNeverTyped: design 03 §4.2.
func TestAPortIsAllocatedNeverTyped(t *testing.T) {
	s, ports := service(t)
	cf := spec.Routing{AdapterRef: "rte_cf", Mode: spec.RoutingSubdomain, Hostname: "notes.bemeek.io"}
	d, err := s.Resolve(context.Background(), "app_1", "notes", cf, Request{AdapterRef: "rte_loopback"})
	require.NoError(t, err)
	require.Equal(t, 9004, d.Routing.Port)
	require.Equal(t, 1, ports.calls)

	d, err = s.Resolve(context.Background(), "app_1", "notes", onLoopback, Request{Mode: spec.RoutingPort})
	require.NoError(t, err)
	require.Equal(t, 9001, d.Routing.Port, "an app keeps the port it has")
	require.Equal(t, 1, ports.calls)
}

// TestR254_AModeTheAdapterDoesNotServeIsRefusedWithTheChoices asserts R-254.
func TestR254_AModeTheAdapterDoesNotServeIsRefusedWithTheChoices(t *testing.T) {
	s, _ := service(t)
	_, err := s.Resolve(context.Background(), "app_1", "notes", onLoopback,
		Request{AdapterRef: "rte_cf", Mode: spec.RoutingPort})
	var e *errs.Error
	require.ErrorAs(t, err, &e)
	require.Equal(t, errs.PlanCapabilityUnsupported, e.Code)
	require.Contains(t, e.Remedy, "at their own hostname or under a path")

	_, err = s.Resolve(context.Background(), "app_1", "notes", onLoopback, Request{AdapterRef: "rte_gone"})
	require.ErrorAs(t, err, &e)
	require.Contains(t, e.Message, "rte_gone")
}

// TestR163_AHandWrittenSpecIsCheckedTheSameWay: POST /specs is not a way
// around the override, and its ModeSource is set from the adapter rather than
// taken on the author's word.
func TestR163_AHandWrittenSpecIsCheckedTheSameWay(t *testing.T) {
	s, _ := service(t)
	next := spec.Routing{AdapterRef: "rte_cf", Mode: spec.RoutingPath, ModeSource: spec.ModeFromAdapterDefault, PathPrefix: "/notes"}
	over, err := s.Overrides(context.Background(), &onLoopback, &next)
	require.NoError(t, err)
	require.True(t, over)
	require.Equal(t, spec.ModeFromUserOverride, next.ModeSource)

	kept := next
	over, err = s.Overrides(context.Background(), &next, &kept)
	require.NoError(t, err)
	require.False(t, over, "an override the app already has is not asked about again")

	def := spec.Routing{AdapterRef: "rte_cf", Mode: spec.RoutingSubdomain, ModeSource: spec.ModeFromUserOverride, Hostname: "n.bemeek.io"}
	over, err = s.Overrides(context.Background(), &onLoopback, &def)
	require.NoError(t, err)
	require.False(t, over)
	require.Equal(t, spec.ModeFromAdapterDefault, def.ModeSource)
}
