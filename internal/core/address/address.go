// Package address changes where a configured app is reached (R-162, R-163,
// R-165).
//
// An app's address is its spec's routing block, and changing it is a spec
// revision like any other edit (R-152). What this package adds is the part
// that is not a plain field edit: which modes the chosen routing adapter
// serves, what the hostname, path or port defaults to under it, and whether
// the result deviates from the adapter's default mode — which R-163 gates
// behind app.routing.override. The HTTP handler asks the authorizer; the
// decision of what needs asking is made here, once, for every surface.
package address

import (
	"context"
	"fmt"
	"strings"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// Ports allocates a host port for port-mode routing (design 03 §4.2).
// Idempotent: an app that holds a port on an adapter gets the same one back.
type Ports interface {
	Allocate(ctx context.Context, adapterRef, appID string, from, to int) (int, error)
}

// Service resolves routing changes against the configured routing adapters.
type Service struct {
	Registry *api.Registry
	Ports    Ports

	// PortRangeStart and PortRangeEnd are the install's port range.
	PortRangeStart, PortRangeEnd int

	// BaseDomain is the install's server.base_domain, used when the routing
	// adapter has no domain of its own.
	BaseDomain string
}

// Request is the address somebody asked for. Empty fields take the adapter's
// default: its default mode, and the hostname or port that mode implies. A
// hostname is the only part of an address a person names; a path is the app's
// slug and a port is allocated.
type Request struct {
	AdapterRef string           `json:"adapter_ref,omitempty"`
	Mode       spec.RoutingMode `json:"mode,omitempty"`
	Hostname   string           `json:"hostname,omitempty"`
}

// Decision is a resolved routing change.
type Decision struct {
	Routing spec.Routing

	// Changed is whether it differs from the current routing. A change moves
	// the app: the old address stops working, and bookmarks to it break
	// (R-165), which is why the caller asks for confirmation.
	Changed bool

	// Override is whether the mode deviates from the adapter's default, which
	// needs app.routing.override (R-163). Keeping a deviation an app already
	// has is not a new one and is not asked about again.
	Override bool
}

// Resolve works out the routing an app would have under req.
func (s *Service) Resolve(ctx context.Context, appID, slug string, current spec.Routing, req Request) (Decision, error) {
	ref := strings.TrimSpace(req.AdapterRef)
	if ref == "" {
		ref = current.AdapterRef
	}
	if ref == "" && s.Registry != nil {
		ref, _ = s.Registry.Default(api.CategoryRouting)
	}
	routing, ok := s.routing(ref)
	if !ok {
		return Decision{}, errs.Newf(errs.ValidInvalid, "There is no routing adapter %q on this installation.", ref).
			WithRemedy("Choose one of the routing adapters listed under Installation, Adapters.")
	}
	caps, err := routing.Capabilities(ctx)
	if err != nil {
		return Decision{}, errs.Wrap(errs.AdapterUnavailable,
			fmt.Sprintf("Pando could not ask the %s routing adapter what it can do.", ref), err)
	}

	mode := req.Mode
	if mode == "" {
		// Staying on the same adapter keeps the mode; moving to another takes
		// its default, since the old mode may be one it does not serve.
		if ref == current.AdapterRef && current.Mode != "" {
			mode = current.Mode
		} else {
			mode = caps.DefaultMode
		}
	}
	if !caps.Supports(mode) {
		return Decision{}, errs.Newf(errs.PlanCapabilityUnsupported,
			"The %s routing adapter does not serve apps %s.", ref, describeMode(mode)).
			WithRemedy("Choose " + describeModes(caps.Modes) + ", or another routing adapter.")
	}

	next := spec.Routing{AdapterRef: ref, Mode: mode, ModeSource: spec.ModeFromAdapterDefault}
	if mode != caps.DefaultMode {
		next.ModeSource = spec.ModeFromUserOverride
	}

	// The address the mode implies: what was asked for, or what the app
	// already has in that mode, or the default for its name.
	same := ref == current.AdapterRef && mode == current.Mode
	switch mode {
	case spec.RoutingSubdomain:
		host := req.Hostname
		if host == "" && same {
			host = current.Hostname
		}
		if host == "" {
			base := caps.BaseDomain
			if base == "" {
				base = s.BaseDomain
			}
			if base == "" {
				return Decision{}, errs.New(errs.ValidInvalid, "This app needs a hostname, and there is no base domain to make one from.").
					WithRemedy("Enter the hostname, such as notes.example.com.")
			}
			host = slug + "." + base
		}
		probe := spec.AppSpec{Routing: next}
		if err := spec.SetHostname(&probe, host); err != nil {
			return Decision{}, err
		}
		next.Hostname = probe.Routing.Hostname

	case spec.RoutingPath:
		// Always the app's slug. The proxy finds a path-mode app by the first
		// segment of the path, matched against slugs (proxy.resolve), so a
		// prefix chosen here would be stored and never served.
		next.PathPrefix = "/" + slug

	case spec.RoutingPort:
		// Allocated, never typed: two apps must not be handed one port, and
		// the allocation is what guarantees it (design 03 §4.2).
		if same && current.Port > 0 {
			next.Port = current.Port
			break
		}
		if s.Ports == nil {
			return Decision{}, errs.New(errs.Internal, "Pando cannot assign ports here.")
		}
		port, err := s.Ports.Allocate(ctx, ref, appID, s.PortRangeStart, s.PortRangeEnd)
		if err != nil {
			return Decision{}, err
		}
		next.Port = port
	}

	return Decision{
		Routing: next,
		Changed: !equal(current, next),
		// Asked only when the change introduces the deviation: an app already
		// on a non-default mode that only changes its path is not deviating
		// anew.
		Override: next.ModeSource == spec.ModeFromUserOverride &&
			(current.AdapterRef != next.AdapterRef || current.Mode != next.Mode),
	}, nil
}

// Overrides reports whether next deviates from its adapter's default mode
// where current did not — for a spec written whole, through POST /specs,
// rather than through Resolve. It also corrects next's ModeSource, which a
// hand-written spec may have left saying anything.
func (s *Service) Overrides(ctx context.Context, current *spec.Routing, next *spec.Routing) (bool, error) {
	routing, ok := s.routing(next.AdapterRef)
	if !ok {
		// Not ours to refuse: validation and the planner name an unknown
		// adapter in their own words.
		return false, nil
	}
	caps, err := routing.Capabilities(ctx)
	if err != nil {
		return false, errs.Wrap(errs.AdapterUnavailable,
			fmt.Sprintf("Pando could not ask the %s routing adapter what it can do.", next.AdapterRef), err)
	}
	if next.Mode == caps.DefaultMode || next.Mode == "" {
		next.ModeSource = spec.ModeFromAdapterDefault
		return false, nil
	}
	next.ModeSource = spec.ModeFromUserOverride
	if current != nil && current.AdapterRef == next.AdapterRef && current.Mode == next.Mode {
		return false, nil
	}
	return true, nil
}

// Option is a routing adapter an app can be moved to, and what it offers.
type Option struct {
	AdapterRef  string             `json:"adapter_ref"`
	Modes       []spec.RoutingMode `json:"modes"`
	DefaultMode spec.RoutingMode   `json:"default_mode"`
	BaseDomain  string             `json:"base_domain,omitempty"`
	IsDefault   bool               `json:"is_default"`
}

// Options lists the routing adapters an app's address can use. One that
// cannot say what it serves is left out rather than offered and refused.
func (s *Service) Options(ctx context.Context) []Option {
	if s.Registry == nil {
		return nil
	}
	def, _ := s.Registry.Default(api.CategoryRouting)
	var out []Option
	for _, ref := range s.Registry.ByCategory(api.CategoryRouting) {
		routing, ok := s.Registry.Routing(ref)
		if !ok {
			continue
		}
		caps, err := routing.Capabilities(ctx)
		if err != nil {
			continue
		}
		base := caps.BaseDomain
		if base == "" {
			base = s.BaseDomain
		}
		out = append(out, Option{AdapterRef: ref, Modes: caps.Modes, DefaultMode: caps.DefaultMode,
			BaseDomain: base, IsDefault: ref == def})
	}
	return out
}

func (s *Service) routing(ref string) (api.RoutingAdapter, bool) {
	if s.Registry == nil || ref == "" {
		return nil, false
	}
	return s.Registry.Routing(ref)
}

func equal(a, b spec.Routing) bool {
	return a.AdapterRef == b.AdapterRef && a.Mode == b.Mode &&
		a.Hostname == b.Hostname && a.PathPrefix == b.PathPrefix && a.Port == b.Port
}

func describeMode(m spec.RoutingMode) string {
	switch m {
	case spec.RoutingSubdomain:
		return "at their own hostname"
	case spec.RoutingPath:
		return "under a path"
	case spec.RoutingPort:
		return "on their own port"
	}
	return fmt.Sprintf("in %q mode", m)
}

func describeModes(modes []spec.RoutingMode) string {
	parts := make([]string, 0, len(modes))
	for _, m := range modes {
		parts = append(parts, describeMode(m))
	}
	return strings.Join(parts, " or ")
}
