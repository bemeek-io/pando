package detection

import (
	"context"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
)

// RegistryInstallation reads an install's defaults from its configured adapters.
type RegistryInstallation struct {
	Registry *api.Registry

	// BaseDomain is what a subdomain is carved out of, in subdomain mode.
	BaseDomain string

	// Fallback supplies the values that come from neither an adapter nor host
	// policy — the [P] defaults in the requirements.
	Fallback spec.Defaults
}

// NewInstallation returns an installation reading from registry.
func NewInstallation(registry *api.Registry, baseDomain string) *RegistryInstallation {
	return &RegistryInstallation{
		Registry:   registry,
		BaseDomain: baseDomain,
		Fallback:   spec.StandardDefaults(),
	}
}

// Defaults resolves the install's adapters and routing mode.
//
// The routing mode is asked of the routing adapter rather than configured
// centrally. R-162: each adapter declares a default mode, and adding an app
// uses it without asking — which is what makes an install feel like proxy mode
// or per-hostname without either being a global setting (design 03 §4.1).
func (i *RegistryInstallation) Defaults(ctx context.Context) spec.Defaults {
	d := i.Fallback
	d.BaseDomain = i.BaseDomain

	if i.Registry == nil {
		return d
	}

	if ref, ok := i.Registry.Default(api.CategoryRuntime); ok {
		d.RuntimeAdapter = ref
	}
	if ref, ok := i.Registry.Default(api.CategoryBuilder); ok {
		d.BuilderAdapter = ref
	}
	if ref, ok := i.Registry.Default(api.CategoryRouting); ok {
		d.RoutingAdapter = ref

		// Ask the adapter, and only fall back if it cannot be reached. A mode
		// the adapter does not advertise would fail the planner's capability
		// check (R-254) with an error about something nobody chose.
		if routing, found := i.Registry.Routing(ref); found {
			if caps, err := routing.Capabilities(ctx); err == nil && caps.DefaultMode != "" {
				d.RoutingMode = caps.DefaultMode
			}
		}
	}

	return d
}

var _ Installation = (*RegistryInstallation)(nil)
