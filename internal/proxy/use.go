package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/log"
)

// Recording who used an app (R-227, O-22).
//
// After a leak or a misuse, "who used this app, and when" is the question, and
// the proxy is the one place every use passes through. So an allowed request
// writes app.use — once per visit, not per request: a page load is dozens of
// requests, and an audit log with a row for every image is a log nobody can
// read, and a table that grows by the request.
//
// A visit is a browser session or a token:
//
//   - A browser — signed in or not — carries VisitCookie, a random ID Pando
//     sets on the app's response the first time it records a visit and strips
//     before anything reaches the app, like every cookie in its namespace
//     (R-173). A session cookie, so a visit ends when the browser closes.
//   - A token is its own visit: agents and scripts keep no cookies.
//
// Each visit is recorded once and remembered for UseWindow. Memory, not the
// database: a restart forgets, and records a visit that continues across it
// again — a duplicate row, never a missing one.
//
// Anonymous visitors are recorded unless host policy turns it off
// (disable_anonymous_use_audit). A client that never keeps cookies would be a
// new visit on every request, so new anonymous visits are capped per app per
// minute; what is over the cap is counted onto the next one recorded rather
// than dropped silently.

// VisitCookie names the cookie that marks a browser's visit to an app:
// VisitCookie + the app's ID. In Pando's namespace, so an app never sees it.
const VisitCookie = CookiePrefix + "visit_"

// UseWindow is how long a visit is remembered once recorded.
const UseWindow = 12 * time.Hour

// AnonymousUsePerMinute caps new anonymous visits recorded per app per minute.
const AnonymousUsePerMinute = 120

// maxRemembered bounds the memory the visit log takes; past it, expired visits
// are dropped, and if that is not enough the log starts over.
const maxRemembered = 200_000

// AuditWriter is where the proxy records what happened. *audit.Writer is one.
type AuditWriter interface {
	Write(ctx context.Context, e audit.Event) error
}

// UsePolicy says whether anonymous use is recorded. The policy evaluator is
// one; nil records it.
type UsePolicy interface {
	RecordsAnonymousUse(ctx context.Context) bool
}

// visits is the memory of recorded visits, and the anonymous rate cap.
type visits struct {
	mu      sync.Mutex
	seen    map[string]time.Time // visit key → when it stops being remembered
	minute  map[string]int       // app → new anonymous visits this minute
	current time.Time            // the minute counted
	skipped map[string]int       // app → anonymous visits over the cap, not yet reported
}

func newVisits() *visits {
	return &visits{seen: map[string]time.Time{}, minute: map[string]int{}, skipped: map[string]int{}}
}

// remembered reports whether a visit is already recorded.
func (v *visits) remembered(key string, now time.Time) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	until, ok := v.seen[key]
	return ok && now.Before(until)
}

// remember marks a visit recorded.
func (v *visits) remember(key string, now time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.seen) >= maxRemembered {
		for k, until := range v.seen {
			if !now.Before(until) {
				delete(v.seen, k)
			}
		}
		if len(v.seen) >= maxRemembered {
			v.seen = map[string]time.Time{}
		}
	}
	v.seen[key] = now.Add(UseWindow)
}

// admitAnonymous takes one of this minute's anonymous records for an app, and
// says how many were skipped since the last one; false when the cap is spent.
func (v *visits) admitAnonymous(appID string, now time.Time) (int, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if m := now.Truncate(time.Minute); !m.Equal(v.current) {
		v.current = m
		v.minute = map[string]int{}
	}
	if v.minute[appID] >= AnonymousUsePerMinute {
		v.skipped[appID]++
		return 0, false
	}
	v.minute[appID]++
	skipped := v.skipped[appID]
	delete(v.skipped, appID)
	return skipped, true
}

// recordUse writes app.use for an allowed request, the first time its visit is
// seen, and returns the visit cookie to set on the response, if one is to be.
//
// Never fails the request: the app use has been authorized, and an audit row
// that could not be written is logged rather than turned into a refusal a
// person cannot act on. Denials are written the same way (auditDenial).
func (p *Proxy) recordUse(r *http.Request, principal authz.Principal, appID string) *http.Cookie {
	if p.Auditor == nil {
		return nil
	}
	now := time.Now()
	if p.clock != nil {
		now = p.clock()
	}

	var key, visitID string
	var setCookie *http.Cookie
	switch {
	case principal.Kind == authz.KindToken && principal.ID != "":
		key = "token:" + principal.ID + ":" + appID
	default:
		if c, err := r.Cookie(VisitCookie + appID); err == nil && c.Value != "" {
			visitID = c.Value
		} else {
			visitID = newVisitID()
			setCookie = p.visitCookie(r, appID, visitID)
		}
		key = string(principal.Kind) + ":" + principal.ID + ":" + appID + ":" + visitID
	}
	if p.visits.remembered(key, now) {
		return nil
	}

	detail := map[string]any{"path": r.URL.Path}
	if principal.Kind == authz.KindAnonymous {
		if p.UsePolicy != nil && !p.UsePolicy.RecordsAnonymousUse(r.Context()) {
			return nil
		}
		skipped, ok := p.visits.admitAnonymous(appID, now)
		if !ok {
			return nil
		}
		if skipped > 0 {
			detail["unrecorded_before"] = skipped
		}
		// The address Pando saw. Behind a routing adapter that is the
		// router's, which is still worth having beside the time.
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			detail["remote_addr"] = host
		}
	}
	if visitID != "" {
		detail["visit"] = visitID
	}

	if err := p.Auditor.Write(r.Context(), audit.Event{
		PrincipalKind: audit.PrincipalKind(principal.Kind),
		PrincipalID:   principal.ID,
		OnBehalfOf:    principal.UserID,
		Action:        "app.use",
		AppID:         appID,
		Detail:        detail,
	}); err != nil {
		log.From(r.Context()).Warn("could not record app use", zap.Error(err))
		return setCookie
	}
	p.visits.remember(key, now)
	return setCookie
}

// visitCookie is the cookie marking this browser's visit to an app. Scoped to
// the app's path prefix in path mode, so one app's visit is not another's.
func (p *Proxy) visitCookie(r *http.Request, appID, id string) *http.Cookie {
	path := "/"
	if prefix := prefixOf(r); prefix != "" {
		path = prefix
	}
	// Secure by the session cookie's rule (O-19): the operator's external URL
	// when there is one, since behind a TLS-terminating proxy every request
	// arrives as plain HTTP; the request itself otherwise.
	secure := r.TLS != nil
	if p.ExternalURL != nil {
		secure = p.ExternalURL.Scheme == "https"
	}
	return &http.Cookie{ //nolint:gosec // G124: Secure follows the session cookie's rule above; forcing it would drop the cookie on a plain-HTTP localhost install.
		Name:     VisitCookie + appID,
		Value:    id,
		Path:     path,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func newVisitID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return "vis_" + hex.EncodeToString(b)
}

type ctxKeyPrefix struct{}

func prefixOf(r *http.Request) string {
	prefix, _ := r.Context().Value(ctxKeyPrefix{}).(string)
	return prefix
}
