package httpapi

import (
	"net/http"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/errs"
)

// handlePlan is the dry run (design 04 §2.3).
//
// It exists as its own endpoint because every plan-time failure is more useful
// before a user commits than during a deploy, and because it is side-effect-free
// the console can call it on every spec edit.
func (s *Server) handlePlan(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}
	if app.PinnedSpecID == "" {
		Error(w, r, errs.New(errs.StateInvalid, "This app has no spec to plan yet.").
			WithRemedy("Write a spec for the app and pin it first."))
		return
	}

	rev, found, err := s.Apps.RevisionByID(r.Context(), app.PinnedSpecID)
	if err != nil || !found {
		Error(w, r, orNotFound(err))
		return
	}

	plan, err := s.Planner.Check(r.Context(), rev.Body)
	if err != nil {
		// A plan-time refusal is the point of this endpoint, not a failure of
		// it. It is returned with its own status and left unaudited: planning
		// creates nothing, and auditing every keystroke of a console spec editor
		// would bury the events that matter.
		Error(w, r, err)
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"app_id":    plan.AppID,
		"revision":  rev.Revision,
		"checks":    plan.Checks,
		"workloads": len(plan.Bundle.Workloads),
		"volumes":   len(plan.Bundle.Volumes),
	})
}

// handleListAdapters returns configured adapters with their LIVE capabilities.
//
// Live rather than stored (design 04 §2.8), so the console can grey out routing
// modes an adapter does not support instead of offering choices that fail at
// plan time. Stored capabilities would drift the first time an adapter was
// upgraded.
// install.view rather than something app-scoped: this is the install's
// inventory, including which adapters are configured and whether each is
// reachable, which is operational detail about the host rather than about any
// app.
func (s *Server) handleListAdapters(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireInstall(w, r, authz.InstallView); !ok {
		return
	}

	configured, err := s.Adapters.List(r.Context())
	if err != nil {
		Error(w, r, err)
		return
	}

	health := s.Registry.HealthCheckAll(r.Context())

	out := make([]map[string]any, 0, len(configured))
	for _, c := range configured {
		entry := map[string]any{
			"id":         c.ID,
			"category":   c.Category,
			"kind":       c.Kind,
			"name":       c.Name,
			"is_default": c.IsDefault,
			"enabled":    c.Enabled,
			"healthy":    health[c.ID] == nil,
		}

		// An unhealthy adapter's error message is not returned. It can carry a
		// host, a port, or a path from the operator's infrastructure, and this
		// endpoint is readable by anyone signed in.
		if err := health[c.ID]; err != nil {
			entry["status"] = "unreachable"
		}

		switch api.Category(c.Category) {
		case api.CategoryRuntime:
			if rt, ok := s.Registry.Runtime(c.ID); ok {
				if caps, err := rt.Capabilities(r.Context()); err == nil {
					entry["capabilities"] = caps
				}
			}
		case api.CategoryRouting:
			if rte, ok := s.Registry.Routing(c.ID); ok {
				if caps, err := rte.Capabilities(r.Context()); err == nil {
					entry["capabilities"] = caps
				}
			}
		case api.CategoryBuilder:
			if b, ok := s.Registry.Builder(c.ID); ok {
				if caps, err := b.Capabilities(r.Context()); err == nil {
					entry["capabilities"] = caps
				}
			}
		}
		out = append(out, entry)
	}

	JSON(w, http.StatusOK, map[string]any{"adapters": out})
}

// handleCapacity aggregates what the runtime adapters report (R-243).
func (s *Server) handleCapacity(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireInstall(w, r, authz.InstallView); !ok {
		return
	}

	out := make([]map[string]any, 0)
	for _, ref := range s.Registry.ByCategory(api.CategoryRuntime) {
		rt, ok := s.Registry.Runtime(ref)
		if !ok {
			continue
		}
		capacity, err := rt.Capacity(r.Context())
		if err != nil {
			out = append(out, map[string]any{"adapter_ref": ref, "status": "unreachable"})
			continue
		}
		allocated, err := s.Allocations.AllocatedOn(r.Context(), ref, "")
		if err != nil {
			Error(w, r, err)
			return
		}
		out = append(out, map[string]any{
			"adapter_ref":            ref,
			"total_cpu_millis":       capacity.TotalCPUMillis,
			"total_memory_bytes":     capacity.TotalMemoryBytes,
			"total_disk_bytes":       capacity.TotalDiskBytes,
			"allocated_cpu_millis":   allocated.CPUMillis,
			"allocated_memory_bytes": allocated.MemoryBytes,
			"allocated_disk_bytes":   allocated.DiskBytes,
			"reported":               capacity.Reported,
		})
	}
	JSON(w, http.StatusOK, map[string]any{"runtimes": out})
}
