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
	"github.com/bemeek-io/pando/internal/hash"
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

	//nolint:gosec // G124: Secure is decided by secureCookie, not left unset.
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    sess.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookie(r),
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

// secureCookie decides whether the session cookie is marked Secure (O-19).
//
// The request alone cannot answer this. Behind a TLS-terminating reverse proxy
// — the topology SECURITY.md describes — r.TLS is nil on every request even
// though the browser's connection is encrypted, so trusting it alone ships the
// session cookie without Secure and one plaintext request to the hostname puts
// it on the wire.
//
// So the operator states the scheme, in PANDO_SERVER_EXTERNAL_URL, and Pando
// believes the operator rather than the request. Not X-Forwarded-Proto: any
// client that can reach Pando directly can set it, which is the same reason
// R-053 strips inbound X-Pando-* headers unconditionally.
//
// Unset falls back to the request, which is correct in both topologies where
// the request does tell the truth — Pando terminating its own TLS, and the
// plain-HTTP localhost install the README documents, where an unconditional
// Secure would stop sign-in working.
func (s *Server) secureCookie(r *http.Request) bool {
	if s.ExternalURL != nil {
		return s.ExternalURL.Scheme == "https"
	}
	return r.TLS != nil
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

	// The attributes have to match the cookie being cleared, or a browser may
	// treat this as a different cookie and leave the original one in place.
	// Same set as handleLogin, with MaxAge: -1 and an empty value — which is
	// why Secure comes from the same place rather than being repeated.
	//nolint:gosec // G124: Secure is decided by secureCookie, not left unset.
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secureCookie(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	JSON(w, http.StatusNoContent, nil)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p.Kind == authz.KindAnonymous {
		Error(w, r, errs.New(errs.AuthRequired, "You need to sign in."))
		return
	}

	// The installation-wide verbs the caller holds (R-265). The console reads
	// this to decide whether to show the administrative entry and what to put
	// in it — the server's answer, not the client's inference. An empty list is
	// the normal case: most accounts hold nothing install-wide, and their
	// console is the launcher.
	// Never nil: a missing key and an empty list are the same thing to a client
	// reading `verbs ?? []`, and only one of them is an answer.
	verbs := []string{}
	if s.Verbs != nil {
		held, err := s.Verbs.InstallVerbsFor(r.Context(), p)
		if err != nil {
			Error(w, r, err)
			return
		}
		if held != nil {
			verbs = held
		}
	}

	body := map[string]any{
		"principal_kind": string(p.Kind),
		"id":             p.ID,
		"groups":         p.Groups,
		"verbs":          verbs,
	}
	if p.UserID != "" {
		user, found, err := s.Users.ByID(r.Context(), p.UserID)
		if err != nil {
			Error(w, r, err)
			return
		}
		if found {
			body["user_id"] = user.ID

			// The sign-in name. The console shows it on the first-run password
			// screen so a password manager has something to file the new
			// credential under, and so the person can see which account they
			// are changing.
			body["username"] = user.ExternalID
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

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleChangePassword changes the caller's own password (R-046).
//
// Self only, and deliberately not a verb. Changing your own password is not
// administration, and changing somebody else's is a *reset* — a different
// action with different consequences, which does not exist yet and should not
// arrive by relaxing this route.
//
// The current password is required even though the caller is already
// authenticated. A session cookie is a bearer credential: someone holding a
// borrowed one could otherwise lock the owner out of their own account, which
// is the same escalation shape as O-17 on a smaller scale. Re-authentication
// goes through the identity adapter rather than comparing hashes here, so the
// adapter's own rules — including its failure timing — still apply (R-044).
//
// Without this endpoint R-046's "must be changed on first login" is a flag that
// nothing can clear, which is what it was until now.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p.Kind == authz.KindAnonymous {
		Error(w, r, errs.New(errs.AuthRequired, "You need to sign in."))
		return
	}
	if p.UserID == "" {
		// An account token is its own principal and has no account to hold a
		// password (R-060).
		Error(w, r, errs.New(errs.ValidInvalid, "A token has no password to change."))
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, r, errs.New(errs.ValidInvalid, "The request body could not be read."))
		return
	}
	if req.NewPassword == "" {
		Error(w, r, errs.New(errs.ValidInvalid, "A password cannot be empty."))
		return
	}
	if len(req.NewPassword) < hash.MinPasswordLength {
		Error(w, r, errs.Newf(errs.ValidInvalid,
			"A password needs at least %d characters.", hash.MinPasswordLength).
			WithRemedy("A short phrase you will remember is a good password."))
		return
	}

	user, found, err := s.Users.ByID(r.Context(), p.UserID)
	if err != nil {
		Error(w, r, err)
		return
	}
	if !found {
		Error(w, r, errs.New(errs.AuthInvalid, "This account no longer exists."))
		return
	}

	if _, err := s.Identity.Authenticate(r.Context(), api.Credential{
		Username: user.ExternalID,
		Password: secret.New(req.CurrentPassword),
	}); err != nil {
		s.audit(r, audit.Event{
			PrincipalKind: audit.PrincipalKind(p.Kind),
			PrincipalID:   p.ID,
			OnBehalfOf:    p.UserID,
			Action:        "user.password.denied",
			TargetKind:    "user",
			TargetID:      p.UserID,
		})
		Error(w, r, errs.New(errs.AuthInvalid, "That current password is not right."))
		return
	}

	digest, err := hash.New(secret.New(req.NewPassword))
	if err != nil {
		Error(w, r, errs.Wrap(errs.Internal, "Could not secure the password.", err))
		return
	}
	if err := s.Users.SetPassword(r.Context(), p.UserID, digest); err != nil {
		Error(w, r, err)
		return
	}

	// Every other session ends. A password change is what someone does when
	// they think a credential has leaked, and leaving the leaked session alive
	// would make the act cosmetic. The current one survives so that changing a
	// password does not sign you out of the page you changed it on.
	if cookie, err := r.Cookie(SessionCookie); err == nil && cookie.Value != "" {
		if err := s.Sessions.RevokeOthersForUser(r.Context(), p.UserID, cookie.Value); err != nil {
			Error(w, r, err)
			return
		}
	} else if err := s.Sessions.RevokeAllForUser(r.Context(), p.UserID); err != nil {
		Error(w, r, err)
		return
	}

	s.audit(r, audit.Event{
		PrincipalKind: audit.PrincipalKind(p.Kind),
		PrincipalID:   p.ID,
		OnBehalfOf:    p.UserID,
		Action:        "user.password.change",
		TargetKind:    "user",
		TargetID:      p.UserID,
	})
	JSON(w, http.StatusNoContent, nil)
}
