package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/id"
)

// Favorites (R-341): apps a person pins to the top of their launcher.
//
// Self only and verb-free, like changing your own password. A favorite grants
// nothing — the launcher list is still scoped by data-plane grants — so there
// is nothing here for authorization to decide beyond "can you open this app",
// which is asked only so that pinning one cannot be used to learn it exists.

func (s *Server) handleFavoriteApp(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.favoritesOwner(w, r)
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

	if err := s.Apps.SetFavorite(r.Context(), userID, appID, true); err != nil {
		Error(w, r, err)
		return
	}
	s.auditApp(r, appID, "app.favorite")
	JSON(w, http.StatusNoContent, nil)
}

// handleUnfavoriteApp checks nothing about the app: removing your own
// favorite is always allowed, including for an app you can no longer open —
// otherwise it would be stuck in your favorites forever.
func (s *Server) handleUnfavoriteApp(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.favoritesOwner(w, r)
	if !ok {
		return
	}
	appID := chi.URLParam(r, "appID")
	if !id.Is(id.App, appID) {
		Error(w, r, errs.New(errs.NotFound, "There is no app with that ID."))
		return
	}
	if err := s.Apps.SetFavorite(r.Context(), userID, appID, false); err != nil {
		Error(w, r, err)
		return
	}
	s.auditApp(r, appID, "app.unfavorite")
	JSON(w, http.StatusNoContent, nil)
}

// favoritesOwner is the user whose favorites these are. A delegated token acts
// as its owner (R-059) and so pins to their launcher; a service token and the
// anonymous principal have no launcher to pin to.
func (s *Server) favoritesOwner(w http.ResponseWriter, r *http.Request) (string, bool) {
	p := PrincipalFrom(r.Context())
	if p.UserID == "" {
		Error(w, r, errs.New(errs.AuthRequired, "Favorites belong to a person, and this request is not signed in as one.").
			WithRemedy("Sign in, or use a token you minted for yourself rather than a service token."))
		return "", false
	}
	return p.UserID, true
}
