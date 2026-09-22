package loopback

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// Kind is the adapter's kind string.
const Kind = "loopback"

// Adapter serves apps on localhost ports: the laptop default.
//
// It does nothing to the network — there is nothing to configure, because the
// proxy already listens and a port-mode app is reached by asking the proxy for
// it. Ensure therefore records intent rather than performing an action, and
// Observe reports what was recorded.
//
// That is not a degenerate case of the interface; it is the interface working.
// Ensure/Remove/Observe describe intent, which is why they fit both an adapter
// that writes config files and one that has no config at all.
type Adapter struct {
	mu     sync.RWMutex
	routes map[string]api.RouteRequest
	config Config
}

// Config is the adapter's configuration.
type Config struct {
	// BaseURL is where the proxy can be reached, for display.
	BaseURL string `json:"base_url,omitempty"`
}

func New() *Adapter {
	return &Adapter{routes: map[string]api.RouteRequest{}}
}

func (a *Adapter) Kind() string           { return Kind }
func (a *Adapter) Category() api.Category { return api.CategoryRouting }

func (a *Adapter) Configure(_ context.Context, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, &a.config); err != nil {
		return errs.Wrap(errs.ValidInvalid, "The loopback routing configuration could not be read.", err)
	}
	return nil
}

// HealthCheck always succeeds: there is no external dependency to be unhealthy.
func (a *Adapter) HealthCheck(context.Context) error { return nil }

// Capabilities reports port mode only, and no TLS.
//
// Stated honestly so the planner refuses a subdomain app at plan time with
// PLAN_CAPABILITY_UNSUPPORTED, rather than accepting it and producing an app
// nobody can reach.
func (a *Adapter) Capabilities(context.Context) (api.RoutingCapabilities, error) {
	return api.RoutingCapabilities{
		Modes:       []api.RoutingMode{spec.RoutingPort},
		DefaultMode: spec.RoutingPort,

		SupportsTLS:         false,
		SupportsWildcardTLS: false,

		// Nothing needs to be publicly reachable — this is a laptop.
		RequiresPublicReachability: false,
	}, nil
}

// Ensure records that an app should be reachable on a port.
func (a *Adapter) Ensure(_ context.Context, r api.RouteRequest) (api.RouteHandle, error) {
	if r.Mode != spec.RoutingPort {
		return api.RouteHandle{}, errs.Newf(errs.PlanCapabilityUnsupported,
			"Loopback routing can only serve apps on a port.").
			WithDetail("requested", string(r.Mode)).
			WithRemedy("Use port mode for this app, or configure a routing option that supports hostnames.")
	}
	if r.ProxyUpstream == "" {
		// The contract is that an adapter is told where to point (R-023). An
		// empty upstream means the caller built the request wrongly, and
		// guessing would be how a route ends up bypassing the proxy.
		return api.RouteHandle{}, errs.New(errs.AdapterFailed,
			"No proxy address was given for this app's traffic.")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.routes[r.AppID] = r

	return api.RouteHandle{AppID: r.AppID, Handle: fmt.Sprintf("port:%d", r.Port)}, nil
}

func (a *Adapter) Remove(_ context.Context, h api.RouteHandle) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.routes, h.AppID)
	return nil
}

func (a *Adapter) Observe(_ context.Context, h api.RouteHandle) (api.RouteState, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	r, ok := a.routes[h.AppID]
	if !ok {
		return api.RouteState{Present: false}, nil
	}
	base := a.config.BaseURL
	if base == "" {
		base = "http://localhost"
	}
	return api.RouteState{Present: true, Address: fmt.Sprintf("%s:%d", base, r.Port)}, nil
}

var _ api.RoutingAdapter = (*Adapter)(nil)

// Info describes this kind of adapter for the forms that configure one
// (api.KindInfo, R-261).
func Info() api.KindInfo {
	return api.KindInfo{
		Category:    api.CategoryRouting,
		Kind:        Kind,
		Name:        "Built in",
		Description: "Serves apps from Pando itself, on a path or a port.",
		IDPrefix:    "rte_",
		Fields: []api.Field{
			{Key: "base_url", Label: "Base URL", Type: "string", Help: "The address apps are reached at.", Default: "The address each request arrives on"},
		},
	}
}
