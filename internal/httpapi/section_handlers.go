package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/id"
)

// Launcher sections (R-342): groupings a person makes in their own launcher.
//
// Self only and verb-free, like favorites. A section is only ever the caller's
// own — every store call is keyed on their user ID — so another person's
// section answers not-found, the same as one that does not exist.

var errNoSection = errs.New(errs.NotFound, "You have no section with that ID.")

func (s *Server) handleCreateSection(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.favoritesOwner(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	section, err := s.Apps.CreateSection(r.Context(), userID, req.Name)
	if err != nil {
		Error(w, r, err)
		return
	}
	s.auditSection(r, section.ID, "launcher.section.create")
	JSON(w, http.StatusCreated, section)
}

func (s *Server) handleRenameSection(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.favoritesOwner(w, r)
	if !ok {
		return
	}
	sectionID, ok := sectionParam(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	section, found, err := s.Apps.RenameSection(r.Context(), userID, sectionID, req.Name)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found {
		Error(w, r, errNoSection)
		return
	}
	s.auditSection(r, sectionID, "launcher.section.rename")
	JSON(w, http.StatusOK, section)
}

func (s *Server) handleDeleteSection(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.favoritesOwner(w, r)
	if !ok {
		return
	}
	sectionID, ok := sectionParam(w, r)
	if !ok {
		return
	}
	found, err := s.Apps.DeleteSection(r.Context(), userID, sectionID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found {
		Error(w, r, errNoSection)
		return
	}
	s.auditSection(r, sectionID, "launcher.section.delete")
	JSON(w, http.StatusNoContent, nil)
}

// handlePlaceApp files an app into a section, out of whichever it was in. The
// app has to be one the caller can open, asked for the same reason favorites
// ask: so that filing one cannot be used to learn an app exists.
func (s *Server) handlePlaceApp(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.favoritesOwner(w, r)
	if !ok {
		return
	}
	sectionID, ok := sectionParam(w, r)
	if !ok {
		return
	}
	appID := chi.URLParam(r, "appID")
	notFound := errs.New(errs.NotFound, "There is no app with that ID that you can open.")
	if !id.Is(id.App, appID) {
		Error(w, r, notFound)
		return
	}
	if _, found, err := s.Apps.ByID(r.Context(), appID); err != nil {
		Error(w, r, err)
		return
	} else if !found {
		Error(w, r, notFound)
		return
	}
	if err := s.Authz.CheckData(r.Context(), PrincipalFrom(r.Context()), appID); err != nil {
		if errs.CodeOf(err) == errs.Internal {
			Error(w, r, err)
			return
		}
		Error(w, r, notFound)
		return
	}

	placed, err := s.Apps.PlaceApp(r.Context(), userID, sectionID, appID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !placed {
		Error(w, r, errNoSection)
		return
	}
	s.auditApp(r, appID, "launcher.section.place")
	JSON(w, http.StatusNoContent, nil)
}

// handleUnplaceApp takes an app back out to "Your apps". Like unfavoriting, it
// checks nothing about the app, so one you have lost access to can still be
// taken out.
func (s *Server) handleUnplaceApp(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.favoritesOwner(w, r)
	if !ok {
		return
	}
	sectionID, ok := sectionParam(w, r)
	if !ok {
		return
	}
	appID := chi.URLParam(r, "appID")
	if !id.Is(id.App, appID) {
		Error(w, r, errs.New(errs.NotFound, "There is no app with that ID."))
		return
	}
	if err := s.Apps.UnplaceApp(r.Context(), userID, sectionID, appID); err != nil {
		Error(w, r, err)
		return
	}
	s.auditApp(r, appID, "launcher.section.unplace")
	JSON(w, http.StatusNoContent, nil)
}

func sectionParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	sectionID := chi.URLParam(r, "sectionID")
	if !id.Is(id.Section, sectionID) {
		Error(w, r, errNoSection)
		return "", false
	}
	return sectionID, true
}

func (s *Server) auditSection(r *http.Request, sectionID, action string) {
	p := PrincipalFrom(r.Context())
	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind),
		PrincipalID:   p.ID,
		OnBehalfOf:    p.UserID,
		Action:        action,
		TargetKind:    "launcher_section",
		TargetID:      sectionID,
	})
}
