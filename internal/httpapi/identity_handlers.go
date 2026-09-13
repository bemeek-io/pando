package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/errs"
)

// Groups, roles and the verb catalog (design 04 §2.7).
//
// All install-scoped. A group is a collection of people and a role is a set of
// powers; both decide what anyone can do here, so both are administration.

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireInstall(w, r, authz.InstallView); !ok {
		return
	}
	groups, err := s.Groups.List(r.Context())
	if err != nil {
		Error(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]any{"groups": groups})
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.InstallUsersManage)
	if !ok {
		return
	}

	var req struct {
		Name    string   `json:"name"`
		Members []string `json:"members"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}

	group, err := s.Groups.Create(r.Context(), req.Name)
	if err != nil {
		Error(w, r, err)
		return
	}
	if len(req.Members) > 0 {
		if err := s.Groups.SetMembers(r.Context(), group.ID, req.Members); err != nil {
			Error(w, r, err)
			return
		}
		group.Members = req.Members
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "group.create", TargetKind: "group", TargetID: group.ID,
		Detail: map[string]any{"name": req.Name, "members": len(req.Members)},
	})
	JSON(w, http.StatusCreated, group)
}

// handleSetGroupMembers replaces a group's membership.
//
// Replaces rather than merges, because a merge cannot express "remove this
// person" — and removing someone is the operation that has to work. Membership
// is read live at authorization time (R-079), so a removal takes effect on the
// next request rather than whenever a cache expires.
func (s *Server) handleSetGroupMembers(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.InstallUsersManage)
	if !ok {
		return
	}

	var req struct {
		Members []string `json:"members"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}

	groupID := chi.URLParam(r, "groupID")
	if _, found, err := s.Groups.ByID(r.Context(), groupID); err != nil {
		Error(w, r, err)
		return
	} else if !found {
		Error(w, r, errs.New(errs.NotFound, "There is no group with that ID."))
		return
	}

	if err := s.Groups.SetMembers(r.Context(), groupID, req.Members); err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "group.members.set", TargetKind: "group", TargetID: groupID,
		Detail: map[string]any{"members": len(req.Members)},
	})
	JSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.InstallUsersManage)
	if !ok {
		return
	}

	groupID := chi.URLParam(r, "groupID")
	if err := s.Groups.Delete(r.Context(), groupID); err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "group.delete", TargetKind: "group", TargetID: groupID,
	})
	JSON(w, http.StatusNoContent, nil)
}

// handleListVerbs returns the catalog, for building custom roles (R-082).
//
// Read from the Go catalog rather than the database: the catalog is what
// authorization actually consults, so anything else would be a second list that
// could disagree with it.
func (s *Server) handleListVerbs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireInstall(w, r, authz.InstallView); !ok {
		return
	}

	out := make([]map[string]any, 0, len(authz.Verbs))
	for _, v := range authz.Verbs {
		scope := "app"
		if authz.InstallScoped(v) {
			scope = "install"
		}
		out = append(out, map[string]any{"verb": string(v), "scope": scope})
	}
	JSON(w, http.StatusOK, map[string]any{"verbs": out})
}

// handleCreateRole composes a custom role from the verb list (R-082).
func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.InstallUsersManage)
	if !ok {
		return
	}

	var req struct {
		Name  string   `json:"name"`
		Scope string   `json:"scope"`
		Verbs []string `json:"verbs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	if req.Scope == "" {
		req.Scope = "app"
	}

	verbs := make([]authz.Verb, 0, len(req.Verbs))
	for _, v := range req.Verbs {
		verbs = append(verbs, authz.Verb(v))
	}

	role, err := s.Roles.CreateCustom(r.Context(), req.Name, req.Scope, verbs)
	if err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "role.create", TargetKind: "role", TargetID: role.ID,
		Detail: map[string]any{"name": req.Name, "scope": req.Scope, "verbs": req.Verbs},
	})
	JSON(w, http.StatusCreated, role)
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.InstallUsersManage)
	if !ok {
		return
	}

	roleID := chi.URLParam(r, "roleID")
	if err := s.Roles.DeleteCustom(r.Context(), roleID); err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "role.delete", TargetKind: "role", TargetID: roleID,
	})
	JSON(w, http.StatusNoContent, nil)
}

// handleDeleteUser deletes an account, which fires R-282's destruction rules.
//
// Separate from PATCH status: suspension is not deletion (R-049). Suspending
// stops someone signing in and is reversible; this is not, and the two are
// different routes precisely so neither can be reached by mistyping the other.
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	p, ok := s.requireInstall(w, r, authz.InstallUsersManage)
	if !ok {
		return
	}

	userID := chi.URLParam(r, "userID")
	if userID == p.UserID {
		// Deleting yourself is how an install loses its last administrator in
		// one request. The same reasoning as refusing to revoke the last
		// install grant, and cheaper to check.
		Error(w, r, errs.New(errs.ValidInvalid, "You cannot delete your own account.").
			WithRemedy("Ask another administrator to do it."))
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

	// Sessions end now, tokens are orphaned by the owner being gone (R-059),
	// and the install grant goes with the account so the schema does not hold a
	// grant naming a user who no longer exists.
	if err := s.Sessions.RevokeAllForUser(r.Context(), userID); err != nil {
		Error(w, r, err)
		return
	}
	if err := s.Grants.RevokeInstall(r.Context(), "user", userID); err != nil {
		// Refused when this is the last account that can manage accounts, which
		// is the right answer: the account is still there to be demoted first.
		Error(w, r, err)
		return
	}
	if err := s.Users.Delete(r.Context(), userID); err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind), PrincipalID: p.ID, OnBehalfOf: p.UserID,
		Action: "user.delete", TargetKind: "user", TargetID: userID,
		Detail: map[string]any{"username": user.ExternalID},
	})
	JSON(w, http.StatusNoContent, nil)
}
