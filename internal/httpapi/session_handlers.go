package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/secret"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}

	subject, err := s.Identity.Authenticate(r.Context(), api.Credential{
		Username: req.Username,
		Password: secret.New(req.Password),
	})
	if err != nil {
		// Audited as a denial: a pattern of failed logins is the signal that
		// matters, and it is the thing most commonly left out.
		s.audit(r, audit.Event{
			PrincipalKind: audit.KindAnonymous,
			Action:        "session.denied",
			TargetKind:    "user",
			Detail:        map[string]any{"username": req.Username},
		})
		Error(w, r, err)
		return
	}

	user, found, err := s.Users.ByExternalID(r.Context(), state.LocalAdapterID, subject.ExternalID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found {
		Error(w, r, errs.New(errs.AuthInvalid, "That username and password do not match."))
		return
	}

	policy := s.Identity.SessionPolicy()
	sess, err := s.Sessions.Create(r.Context(), user.ID, user.AdapterID, policy.MaxLifetime,
		r.UserAgent(), clientIP(r))
	if err != nil {
		Error(w, r, err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		Expires:  sess.ExpiresAt,
	})

	s.audit(r, audit.Event{
		PrincipalKind: audit.KindUser,
		PrincipalID:   user.ID,
		Action:        "session.create",
		TargetKind:    "session",
		TargetID:      sess.ID,
	})

	JSON(w, http.StatusOK, map[string]any{
		"user_id":              user.ID,
		"must_change_password": user.MustChangePassword,
		"expires_at":           sess.ExpiresAt.Format(time.RFC3339),

		// The revocation window is stated rather than implied (design 06 §3.1).
		// Implying revocation is instant is the failure mode here.
		"revocation_window_seconds": 120,
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookie); err == nil && cookie.Value != "" {
		if err := s.Sessions.Revoke(r.Context(), cookie.Value); err != nil {
			Error(w, r, err)
			return
		}
		s.audit(r, audit.Event{
			PrincipalKind: audit.KindUser,
			PrincipalID:   PrincipalFrom(r.Context()).UserID,
			Action:        "session.revoke",
			TargetKind:    "session",
			TargetID:      cookie.Value,
		})
	}

	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1,
	})
	JSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p.Kind == authz.KindAnonymous {
		Error(w, r, errs.New(errs.AuthRequired, "You need to sign in."))
		return
	}

	body := map[string]any{
		"principal_kind": string(p.Kind),
		"id":             p.ID,
		"groups":         p.Groups,
	}
	if p.UserID != "" {
		user, found, err := s.Users.ByID(r.Context(), p.UserID)
		if err != nil {
			Error(w, r, err)
			return
		}
		if found {
			body["user_id"] = user.ID
			body["email"] = user.Email
			body["display_name"] = user.DisplayName
			body["must_change_password"] = user.MustChangePassword
		}
	}
	JSON(w, http.StatusOK, body)
}

func clientIP(r *http.Request) string {
	// Behind Pando's own proxy this is the immediate peer. Trusting a forwarded
	// header here would let a client choose what gets recorded in the audit log.
	host, _, found := cut(r.RemoteAddr, ':')
	if !found {
		return r.RemoteAddr
	}
	return host
}

func cut(s string, sep byte) (string, string, bool) {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
