package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/hash"
	"github.com/bemeek-io/pando/internal/secret"
)

// --- grants ----------------------------------------------------------------

type grantRequest struct {
	Plane         string `json:"plane"`
	PrincipalKind string `json:"principal_kind"`
	PrincipalID   string `json:"principal_id"`
	RoleID        string `json:"role_id"`
}

// handleCreateGrant shares an app.
//
// The anonymous grant comes through this same endpoint rather than a separate
// toggle (R-075). It is a real row with `principal_kind: "anonymous"`, which is
// why it can be listed, revoked and audited like any other.
func (s *Server) handleCreateGrant(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppGrantsManage)
	if !ok {
		return
	}

	var req grantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}

	// Host policy may forbid sharing with everyone (R-076). Checked before the
	// write, and it returns its own code so the console can explain who to ask
	// rather than showing a generic refusal.
	if req.PrincipalKind == "anonymous" && s.HostPolicy != nil {
		if err := s.HostPolicy.AllowsAnonymousGrant(r.Context()); err != nil {
			Error(w, r, err)
			return
		}
	}

	grant, err := s.Grants.Create(r.Context(), app.ID, req.Plane, req.PrincipalKind, req.PrincipalID,
		req.RoleID, PrincipalFrom(r.Context()).ID)
	if err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(PrincipalFrom(r.Context()).Kind),
		PrincipalID:   PrincipalFrom(r.Context()).ID,
		OnBehalfOf:    PrincipalFrom(r.Context()).UserID,
		Action:        "grant.create",
		AppID:         app.ID,
		TargetKind:    "grant",
		TargetID:      grant.ID,
		Detail: map[string]any{
			"plane":          req.Plane,
			"principal_kind": req.PrincipalKind,
		},
	})
	JSON(w, http.StatusCreated, grant)
}

func (s *Server) handleListGrants(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppView)
	if !ok {
		return
	}
	grants, err := s.Grants.ListForApp(r.Context(), app.ID)
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]any{"grants": grants})
}

func (s *Server) handleDeleteGrant(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppGrantsManage)
	if !ok {
		return
	}
	grantID := chi.URLParam(r, "grantID")
	if err := s.Grants.Delete(r.Context(), app.ID, grantID); err != nil {
		Error(w, r, err)
		return
	}
	s.auditApp(r, app.ID, "grant.delete")
	JSON(w, http.StatusNoContent, nil)
}

// --- users -----------------------------------------------------------------

type createUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

// handleCreateUser adds a local account.
//
// Local adapter only — an external identity provider's users arrive by
// authenticating, not by being created here (R-044).
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.InstallUsersManage)
	if !ok {
		return
	}

	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	if req.Username == "" || req.Password == "" {
		Error(w, r, errs.New(errs.ValidInvalid, "An account needs a username and a password."))
		return
	}

	digest, err := hash.New(secret.New(req.Password))
	if err != nil {
		Error(w, r, errs.Wrap(errs.Internal, "Could not secure the password.", err))
		return
	}

	user, err := s.Users.Create(r.Context(), state.LocalAdapterID, req.Username, req.Email,
		req.DisplayName, digest, false)
	if err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind),
		PrincipalID:   p.ID,
		OnBehalfOf:    p.UserID,
		Action:        "user.create",
		TargetKind:    "user",
		TargetID:      user.ID,
	})
	JSON(w, http.StatusCreated, user)
}

// handleGetUser returns one account.
//
// Your own without any administrative power; anyone else's with install.view.
// The directory is not public: an account carries an email address and a
// display name, and "every signed-in user can enumerate every user" is a
// disclosure nobody asked for.
func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	if _, ok := s.requireSelfOrInstall(w, r, userID, authz.InstallView); !ok {
		return
	}
	user, found, err := s.Users.ByID(r.Context(), userID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found {
		Error(w, r, errs.New(errs.NotFound, "There is no account with that ID."))
		return
	}
	JSON(w, http.StatusOK, user)
}

// handlePatchUser changes a user's status.
//
// Suspension is not deletion (R-049). This endpoint suspends and reinstates; it
// must never trigger the destruction rules that DELETE does (R-282), which is
// why they are separate routes rather than one with a flag.
func (s *Server) handlePatchUser(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	p, ok := s.requireSelfOrInstall(w, r, userID, authz.InstallUsersManage)
	if !ok {
		return
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}

	if err := s.Users.SetStatus(r.Context(), userID, req.Status); err != nil {
		Error(w, r, err)
		return
	}

	// A suspended account's sessions end now rather than at their own expiry —
	// otherwise suspension would take up to a session lifetime to mean
	// anything (R-048).
	if req.Status == "suspended" {
		if err := s.Sessions.RevokeAllForUser(r.Context(), userID); err != nil {
			Error(w, r, err)
			return
		}
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind),
		PrincipalID:   p.ID,
		OnBehalfOf:    p.UserID,
		Action:        "user.update",
		TargetKind:    "user",
		TargetID:      userID,
		Detail:        map[string]any{"status": req.Status},
	})
	JSON(w, http.StatusNoContent, nil)
}
