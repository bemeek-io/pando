package httpapi

import (
	"net/http"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/errs"
)

// handleRestart restarts Pando.
//
// Behind install.adapters.manage because applying a saved adapter is what a
// restart is for (R-253): whoever may change an adapter may put the change
// into effect, rather than having to ask someone with a shell on the host.
func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.InstallAdaptersManage)
	if !ok {
		return
	}
	if s.Restart == nil {
		Error(w, r, errs.New(errs.StateInvalid, "This Pando process cannot restart itself.").
			WithRemedy("Restart it where it runs, for example docker compose restart pando."))
		return
	}

	// Before the restart, not after: after, there is no process to write it.
	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "install.restart", TargetKind: "install", TargetID: "install",
	})

	JSON(w, http.StatusAccepted, map[string]any{
		"restarting": true,
		"note":       "Pando is restarting. It is back when started_at on GET /api/v1/adapters changes.",
	})
	s.Restart()
}
