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

// handleSetGrantRole changes the role a control-plane grant carries: one
// update, rather than a revoke and a new grant that could leave the person
// with nothing between the two.
func (s *Server) handleSetGrantRole(w http.ResponseWriter, r *http.Request) {
	app, ok := s.requireControl(w, r, authz.AppGrantsManage)
	if !ok {
		return
	}
	var req struct {
		RoleID string `json:"role_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	grantID := chi.URLParam(r, "grantID")
	if err := s.Grants.SetRole(r.Context(), app.ID, grantID, req.RoleID); err != nil {
		Error(w, r, err)
		return
	}
	p := PrincipalFrom(r.Context())
	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "grant.update", AppID: app.ID, TargetKind: "grant", TargetID: grantID,
		Detail: map[string]any{"role_id": req.RoleID},
	})
	JSON(w, http.StatusNoContent, nil)
}

// handleUserApps returns the apps an account has something on, and what: its
// role for managing each (direct or through a group), whether it can use each,
// and whether the caller may change that.
//
// Your own without anything administrative; anyone else's with install.view.
// Only the apps the caller could see anyway are listed — a list of somebody
// else's apps is not a way to learn that an app exists.
func (s *Server) handleUserApps(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "userID")
	p, ok := s.requireSelfOrInstall(w, r, userID, authz.InstallView)
	if !ok {
		return
	}
	if _, found, err := s.Users.ByID(r.Context(), userID); err != nil {
		Error(w, r, err)
		return
	} else if !found {
		Error(w, r, errs.New(errs.NotFound, "There is no account with that ID."))
		return
	}

	grants, err := s.Grants.ForUser(r.Context(), userID)
	if err != nil {
		Error(w, r, err)
		return
	}

	type access struct {
		AppID     string               `json:"app_id"`
		AppName   string               `json:"app_name"`
		Owner     bool                 `json:"owner"`
		CanManage bool                 `json:"can_manage"`
		Control   []state.UserAppGrant `json:"control"`
		Data      []state.UserAppGrant `json:"data"`
	}
	out := []*access{}
	byApp := map[string]*access{}
	self := userID == p.UserID
	for _, g := range grants {
		a, seen := byApp[g.AppID]
		if !seen {
			visible := self
			if !visible {
				if visible, err = s.Authz.Allows(r.Context(), p, g.AppID, authz.AppView); err != nil {
					Error(w, r, err)
					return
				}
			}
			if !visible {
				byApp[g.AppID] = nil
				continue
			}
			manage, err := s.Authz.Allows(r.Context(), p, g.AppID, authz.AppGrantsManage)
			if err != nil {
				Error(w, r, err)
				return
			}
			a = &access{AppID: g.AppID, AppName: g.AppName, Owner: g.AppOwner == userID,
				CanManage: manage, Control: []state.UserAppGrant{}, Data: []state.UserAppGrant{}}
			byApp[g.AppID] = a
			out = append(out, a)
		}
		if a == nil {
			continue
		}
		if g.Plane == "control" {
			a.Control = append(a.Control, g)
		} else {
			a.Data = append(a.Data, g)
		}
	}
	JSON(w, http.StatusOK, map[string]any{"apps": out})
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

	// MustChangePassword defaults to true: whoever set this password is not
	// the person who will use it, and handed it over some other way (R-046).
	MustChangePassword *bool `json:"must_change_password"`
}

// orTrue reads an optional flag whose absence means yes.
func orTrue(b *bool) bool { return b == nil || *b }

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
		req.DisplayName, digest, orTrue(req.MustChangePassword))
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

// handleGeneratePassword returns a strong random password (R-046): what the
// console offers when an administrator creates an account or resets one, with
// a way to draw another. Nothing is stored — it becomes a password only when it
// is sent back in one of those requests.
//
// Behind install.users.manage because those are its only uses, not because a
// random string is a secret.
func (s *Server) handleGeneratePassword(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireInstall(w, r, authz.InstallUsersManage); !ok {
		return
	}
	password, err := hash.Generate()
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"password": password.Reveal()})
}

// handleResetPassword sets another account's password (R-046). The
// administrator hands it over out of band, so by default its holder must
// change it at the next sign-in; either way every session the account holds
// ends now, because a reset that leaves a stolen session alive has reset
// nothing.
//
// Not for your own password: that is POST /me/password, which asks for the
// current one. An administrator resetting their own here would skip that.
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.InstallUsersManage)
	if !ok {
		return
	}
	userID := chi.URLParam(r, "userID")
	if userID == p.UserID {
		Error(w, r, errs.New(errs.ValidInvalid, "Change your own password from your settings, with your current one.").
			WithRemedy("Use POST /api/v1/me/password, or Settings in the console."))
		return
	}

	var req struct {
		Password           string `json:"password"`
		MustChangePassword *bool  `json:"must_change_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	if len(req.Password) < hash.MinPasswordLength {
		Error(w, r, errs.Newf(errs.ValidInvalid, "A password needs at least %d characters.", hash.MinPasswordLength))
		return
	}
	digest, err := hash.New(secret.New(req.Password))
	if err != nil {
		Error(w, r, errs.Wrap(errs.Internal, "Could not secure the password.", err))
		return
	}

	must := orTrue(req.MustChangePassword)
	found, err := s.Users.SetPasswordFor(r.Context(), userID, digest, must)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found {
		Error(w, r, errs.New(errs.NotFound, "There is no local account with that ID.").
			WithRemedy("An account from an external identity provider changes its password where it lives."))
		return
	}
	if err := s.Sessions.RevokeAllForUser(r.Context(), userID); err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "user.password.reset", TargetKind: "user", TargetID: userID,
		Detail: map[string]any{"must_change_password": must},
	})
	JSON(w, http.StatusNoContent, nil)
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
	roles, err := s.Grants.InstallRolesByPrincipal(r.Context())
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, accountView(user, roles[user.ID]))
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
