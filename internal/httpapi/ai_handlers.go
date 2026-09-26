package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/errs"
)

// handleListAIFunctions reports every AI function, which adapter handles it
// and on which model, whether it is on, and where it was assigned (R-259,
// R-271). install.view, like the adapter list it sits beside.
func (s *Server) handleListAIFunctions(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireInstall(w, r, authz.InstallView); !ok {
		return
	}
	if s.AIFunctions == nil {
		Error(w, r, errs.New(errs.Internal, "AI function assignment is not set up on this installation."))
		return
	}
	functions, err := s.AIFunctions.List(r.Context())
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]any{"functions": functions})
}

// handleAssignAIFunction gives one AI function to one adapter, optionally on
// a model of its own (R-259). Takes effect at once: adapters load at startup
// (R-253), but which running adapter a function goes to is a lookup, not a
// load.
//
// Refused when another adapter handles the function — each has one at a
// time, and the database says so — and when the startup configuration
// assigns it (R-271).
func (s *Server) handleAssignAIFunction(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.InstallAdaptersManage)
	if !ok {
		return
	}
	if s.AIFunctions == nil {
		Error(w, r, errs.New(errs.Internal, "AI function assignment is not set up on this installation."))
		return
	}

	var req struct {
		AdapterID string `json:"adapter_id"`
		Model     string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read.").
			WithRemedy(`Send the adapter and, optionally, a model: {"adapter_id": "ai_anthropic", "model": "claude-haiku-4-5"}.`))
		return
	}

	fn := api.AIFunction(chi.URLParam(r, "function"))
	f, err := s.AIFunctions.Assign(r.Context(), fn, req.AdapterID, req.Model, p.ID)
	if err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "ai.function.assign", TargetKind: "ai_function", TargetID: string(fn),
		Detail: map[string]any{"adapter": f.AdapterID, "model": f.Model},
	})
	JSON(w, http.StatusOK, f)
}

// handleUnassignAIFunction turns an AI function off. A function nobody
// handles degrades the way R-106 describes — to a question, or to Pando
// without AI — and is never an error.
func (s *Server) handleUnassignAIFunction(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.InstallAdaptersManage)
	if !ok {
		return
	}
	if s.AIFunctions == nil {
		Error(w, r, errs.New(errs.Internal, "AI function assignment is not set up on this installation."))
		return
	}

	fn := api.AIFunction(chi.URLParam(r, "function"))
	f, err := s.AIFunctions.Unassign(r.Context(), fn)
	if err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "ai.function.unassign", TargetKind: "ai_function", TargetID: string(fn),
	})
	JSON(w, http.StatusOK, f)
}
