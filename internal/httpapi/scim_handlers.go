package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/trypando/pando/internal/core/idp"
	"github.com/trypando/pando/internal/errs"
)

// SCIM 2.0 (R-048, RFC 7644), for a provider to push accounts and groups.
//
// Authenticated by the provider's own bearer token rather than a Pando token,
// so the Authenticate middleware lets this prefix through untouched
// (scimPrefix) and each handler resolves the token to its provider. A SCIM
// token is not a principal: it can reach these endpoints and nothing else.

// scimPrefix is where SCIM is served, under /api/v1.
const scimPrefix = "/api/v1/scim/v2"

const scimContentType = "application/scim+json"

func scimJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", scimContentType)
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func scimFail(w http.ResponseWriter, r *http.Request, err error) {
	var se *idp.SCIMError
	if !errors.As(err, &se) {
		e := errs.As(err)
		status, detail := http.StatusInternalServerError, "Pando could not complete the request."
		if e != nil {
			status, detail = e.Status(), e.Message
		}
		se = &idp.SCIMError{Status: status, Detail: detail}
	}
	if se.Status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="pando-scim"`)
	}
	scimJSON(w, se.Status, se.Body())
}

// scim wraps a handler with token authentication.
func (s *Server) scim(h func(http.ResponseWriter, *http.Request, idp.SCIMContext)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.IDP == nil {
			scimFail(w, r, &idp.SCIMError{Status: http.StatusNotFound, Detail: "SCIM is not set up on this installation."})
			return
		}
		bearer, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		c, err := s.IDP.SCIMAuthenticate(r.Context(), strings.TrimSpace(bearer), s.origin(r))
		if err != nil {
			scimFail(w, r, err)
			return
		}
		h(w, r, c)
	}
}

func scimBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		scimFail(w, r, &idp.SCIMError{Status: http.StatusRequestEntityTooLarge, Detail: "The request body is too large."})
		return nil, false
	}
	return body, true
}

func pageParams(r *http.Request) (int, int) {
	start, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	return start, count
}

func excludesMembers(r *http.Request) bool {
	for _, a := range strings.Split(r.URL.Query().Get("excludedAttributes"), ",") {
		if strings.EqualFold(strings.TrimSpace(a), "members") {
			return true
		}
	}
	return false
}

func (s *Server) scimRoutes(r chi.Router) {
	r.Get("/ServiceProviderConfig", s.scim(func(w http.ResponseWriter, _ *http.Request, c idp.SCIMContext) {
		scimJSON(w, http.StatusOK, s.IDP.SCIMServiceProviderConfig(c))
	}))
	r.Get("/ResourceTypes", s.scim(func(w http.ResponseWriter, _ *http.Request, c idp.SCIMContext) {
		scimJSON(w, http.StatusOK, s.IDP.SCIMResourceTypes(c))
	}))
	r.Get("/Schemas", s.scim(func(w http.ResponseWriter, _ *http.Request, c idp.SCIMContext) {
		scimJSON(w, http.StatusOK, s.IDP.SCIMSchemas(c))
	}))

	r.Get("/Users", s.scim(func(w http.ResponseWriter, r *http.Request, c idp.SCIMContext) {
		start, count := pageParams(r)
		out, err := s.IDP.SCIMListUsers(r.Context(), c, r.URL.Query().Get("filter"), start, count)
		if err != nil {
			scimFail(w, r, err)
			return
		}
		scimJSON(w, http.StatusOK, out)
	}))
	r.Post("/Users", s.scim(func(w http.ResponseWriter, r *http.Request, c idp.SCIMContext) {
		body, ok := scimBody(w, r)
		if !ok {
			return
		}
		out, err := s.IDP.SCIMCreateUser(r.Context(), c, body)
		if err != nil {
			scimFail(w, r, err)
			return
		}
		scimJSON(w, http.StatusCreated, out)
	}))
	r.Get("/Users/{id}", s.scim(func(w http.ResponseWriter, r *http.Request, c idp.SCIMContext) {
		out, err := s.IDP.SCIMGetUser(r.Context(), c, chi.URLParam(r, "id"))
		if err != nil {
			scimFail(w, r, err)
			return
		}
		scimJSON(w, http.StatusOK, out)
	}))
	r.Put("/Users/{id}", s.scim(func(w http.ResponseWriter, r *http.Request, c idp.SCIMContext) {
		body, ok := scimBody(w, r)
		if !ok {
			return
		}
		out, err := s.IDP.SCIMReplaceUser(r.Context(), c, chi.URLParam(r, "id"), body)
		if err != nil {
			scimFail(w, r, err)
			return
		}
		scimJSON(w, http.StatusOK, out)
	}))
	r.Patch("/Users/{id}", s.scim(func(w http.ResponseWriter, r *http.Request, c idp.SCIMContext) {
		body, ok := scimBody(w, r)
		if !ok {
			return
		}
		out, err := s.IDP.SCIMPatchUser(r.Context(), c, chi.URLParam(r, "id"), body)
		if err != nil {
			scimFail(w, r, err)
			return
		}
		scimJSON(w, http.StatusOK, out)
	}))
	r.Delete("/Users/{id}", s.scim(func(w http.ResponseWriter, r *http.Request, c idp.SCIMContext) {
		if err := s.IDP.SCIMDeleteUser(r.Context(), c, chi.URLParam(r, "id")); err != nil {
			scimFail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	r.Get("/Groups", s.scim(func(w http.ResponseWriter, r *http.Request, c idp.SCIMContext) {
		start, count := pageParams(r)
		out, err := s.IDP.SCIMListGroups(r.Context(), c, r.URL.Query().Get("filter"), start, count, excludesMembers(r))
		if err != nil {
			scimFail(w, r, err)
			return
		}
		scimJSON(w, http.StatusOK, out)
	}))
	r.Post("/Groups", s.scim(func(w http.ResponseWriter, r *http.Request, c idp.SCIMContext) {
		body, ok := scimBody(w, r)
		if !ok {
			return
		}
		out, err := s.IDP.SCIMCreateGroup(r.Context(), c, body)
		if err != nil {
			scimFail(w, r, err)
			return
		}
		scimJSON(w, http.StatusCreated, out)
	}))
	r.Get("/Groups/{id}", s.scim(func(w http.ResponseWriter, r *http.Request, c idp.SCIMContext) {
		out, err := s.IDP.SCIMGetGroup(r.Context(), c, chi.URLParam(r, "id"), excludesMembers(r))
		if err != nil {
			scimFail(w, r, err)
			return
		}
		scimJSON(w, http.StatusOK, out)
	}))
	r.Put("/Groups/{id}", s.scim(func(w http.ResponseWriter, r *http.Request, c idp.SCIMContext) {
		body, ok := scimBody(w, r)
		if !ok {
			return
		}
		out, err := s.IDP.SCIMReplaceGroup(r.Context(), c, chi.URLParam(r, "id"), body)
		if err != nil {
			scimFail(w, r, err)
			return
		}
		scimJSON(w, http.StatusOK, out)
	}))
	r.Patch("/Groups/{id}", s.scim(func(w http.ResponseWriter, r *http.Request, c idp.SCIMContext) {
		body, ok := scimBody(w, r)
		if !ok {
			return
		}
		if err := s.IDP.SCIMPatchGroup(r.Context(), c, chi.URLParam(r, "id"), body); err != nil {
			scimFail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	r.Delete("/Groups/{id}", s.scim(func(w http.ResponseWriter, r *http.Request, c idp.SCIMContext) {
		if err := s.IDP.SCIMDeleteGroup(r.Context(), c, chi.URLParam(r, "id")); err != nil {
			scimFail(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}
