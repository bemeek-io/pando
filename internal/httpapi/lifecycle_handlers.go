package httpapi

import (
	"net/http"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
)

// App lifecycle (design 04 §2.1).
//
// Start and stop set *desired* state and return. They do not touch the runtime:
// the reconciler converges (design 05), which is what makes "stopped" survive a
// Pando restart and a container that comes back on its own. A handler that
// stopped the workload directly would be undone by the next reconcile pass, and
// the app would come back with nothing explaining why.
//
// Restart is not desired state — it is an act, and the reconciler has no way to
// express "the same state, again". So it goes through the runtime adapter, and
// it is the one of the three that does.

func (s *Server) handleStartApp(w http.ResponseWriter, r *http.Request) {
	s.setDesired(w, r, state.StateRunning, "app.start")
}

func (s *Server) handleStopApp(w http.ResponseWriter, r *http.Request) {
	s.setDesired(w, r, "stopped", "app.stop")
}

func (s *Server) setDesired(w http.ResponseWriter, r *http.Request, desired, action string) {
	// app.restart rather than app.spec.edit: starting and stopping an app is
	// operating it, not reconfiguring it, and R-080 separates those.
	app, ok := s.requireControl(w, r, authz.AppRestart)
	if !ok {
		return
	}

	if err := s.Apps.SetDesiredState(r.Context(), app.ID, desired); err != nil {
		Error(w, r, err)
		return
	}

	p := PrincipalFrom(r.Context())
	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: action, AppID: app.ID, TargetKind: "app", TargetID: app.ID,
	})

	// 202: the desired state is recorded and the reconciler will converge. Not
	// 200, because the app is not there yet and saying so would be a lie the
	// console would have to unpick.
	JSON(w, http.StatusAccepted, map[string]any{
		"app_id": app.ID, "desired_state": desired,
	})
}

// handleRestartApp restarts an app's workloads in place.
func (s *Server) handleRestartApp(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppRestart)
	if !ok {
		return
	}

	if app.PinnedSpecID == "" {
		Error(w, r, errs.New(errs.StateInvalid, "This app has nothing pinned to restart.").
			WithRemedy("Deploy it first."))
		return
	}
	revision, found, err := s.Apps.RevisionByID(r.Context(), app.PinnedSpecID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found || revision.Body == nil {
		Error(w, r, errs.New(errs.StateInvalid, "This app has nothing pinned to restart.").
			WithRemedy("Deploy it first."))
		return
	}

	runtime, configured := s.Registry.Runtime(revision.Body.Runtime.AdapterRef)
	if !configured {
		Error(w, r, errs.Newf(errs.PlanAdapterNotConfigured,
			"The runtime %q is not configured.", revision.Body.Runtime.AdapterRef))
		return
	}

	// Stop, and let the reconciler start it again.
	//
	// Rather than a restart call the interface does not have: RuntimeAdapter
	// has Stop and Apply, and "restart" as a primitive would be a third thing
	// every adapter had to implement to mean what these two already mean
	// together. The reconciler brings it back because desired state is still
	// running — which is also what makes this survive Pando dying mid-restart.
	if err := runtime.Stop(r.Context(), apiBundleRef(app.ID)); err != nil {
		Error(w, r, err)
		return
	}

	p := PrincipalFrom(r.Context())
	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "app.restart", AppID: app.ID, TargetKind: "app", TargetID: app.ID,
	})
	JSON(w, http.StatusAccepted, map[string]any{"app_id": app.ID, "restarting": true})
}
