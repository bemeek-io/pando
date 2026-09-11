package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/core/assertion"
	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/log"
)

// HeaderPrefix is Pando's header namespace.
//
// Every inbound header starting with this is stripped unconditionally before
// anything is set (R-053). See stripInbound — that loop is the single most
// security-critical piece of code in the system.
const HeaderPrefix = "X-Pando-"

// Convenience headers, sent alongside the assertion and documented as
// UNVERIFIED (R-053). An app that trusts them is trusting the network boundary,
// which is a legitimate choice a developer must know they are making.
const (
	HeaderUser   = "X-Pando-User"
	HeaderEmail  = "X-Pando-Email"
	HeaderGroups = "X-Pando-Groups"
)

// Resolver finds which app a request is for.
type Resolver interface {
	// ByHostname resolves an app from the Host header.
	ByHostname(ctx context.Context, hostname string) (state.App, *spec.AppSpec, bool, error)
	// BySlug resolves an app from the first path segment, for proxy mode.
	BySlug(ctx context.Context, slug string) (state.App, *spec.AppSpec, bool, error)
}

// Authenticator resolves a request's credentials to a principal.
type Authenticator interface {
	Authenticate(r *http.Request) (authz.Principal, error)
}

// Upstreams gives the address of an app's primary workload.
type Upstreams interface {
	PrimaryAddress(ctx context.Context, app state.App, s *spec.AppSpec) (string, error)
}

// Metrics counts what passed through, so "there is no bypass" is observable
// rather than merely intended.
type Metrics interface {
	Request(appID string, kind authz.PrincipalKind, allowed bool)
}

// Proxy is the single enforcement point for every request to every app (R-023).
//
// There is no bypass — not for public apps, not for performance, not for
// websockets. If you are adding a fast path, you are adding a security hole.
type Proxy struct {
	Resolver      Resolver
	Authenticator Authenticator
	Authz         *authz.Authorizer
	Minter        *assertion.Minter
	Upstreams     Upstreams
	Auditor       *audit.Writer
	Metrics       Metrics
	Logger        *zap.Logger

	// LoginPath is where an unauthenticated caller is sent.
	LoginPath string

	// Mode is how apps are addressed: subdomain or path (design 03 §4.1).
	Mode spec.RoutingMode
}

// ServeHTTP runs the request path from design 06 §4.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. Resolve the app.
	app, appSpec, prefix, found, err := p.resolve(r)
	if err != nil {
		p.fail(w, r, http.StatusInternalServerError, "Something went wrong.")
		return
	}
	if !found {
		p.fail(w, r, http.StatusNotFound, "There is no app at this address.")
		return
	}

	ctx = log.With(ctx, zap.String("app_id", app.ID))
	ctx = context.WithValue(ctx, ctxKeyAppID{}, app.ID)
	r = r.WithContext(ctx)

	// 2. Is it running?
	if app.State != state.StateRunning && app.State != state.StateDegraded {
		p.fail(w, r, http.StatusServiceUnavailable,
			"This app isn't running right now. Try again in a moment.")
		return
	}

	// 3-4. Authenticate, or fall through as anonymous.
	//
	// A bad credential is not fatal here: it resolves to anonymous and the data
	// check decides. That keeps one code path for every caller rather than a
	// separate one for "authentication failed".
	principal, err := p.Authenticator.Authenticate(r)
	if err != nil {
		principal = authz.Anonymous()
	}

	// 5. CheckData. The only authorization decision on this path.
	allowed := p.Authz.CheckData(ctx, principal, app.ID) == nil

	// Counted before the branch, so the anonymous path is provably not a bypass:
	// every request to every app increments this, whatever the outcome.
	if p.Metrics != nil {
		p.Metrics.Request(app.ID, principal.Kind, allowed)
	}

	if !allowed {
		if principal.Kind == authz.KindAnonymous {
			// Send them to sign in, with somewhere to come back to.
			p.redirectToLogin(w, r)
			return
		}
		p.auditDenial(r, principal, app.ID)
		p.fail(w, r, http.StatusForbidden,
			"You don't have access to this app. Ask whoever set it up to share it with you.")
		return
	}

	// 6. Mint the assertion.
	token, err := p.Minter.Mint(assertion.Claims{
		Sub:    subjectOf(principal),
		Email:  principal.Email,
		Name:   principal.DisplayName,
		Groups: principal.Groups,
		Aud:    app.ID,
	})
	if err != nil {
		p.fail(w, r, http.StatusInternalServerError, "Something went wrong.")
		return
	}

	upstream, err := p.Upstreams.PrimaryAddress(ctx, app, appSpec)
	if err != nil || upstream == "" {
		p.fail(w, r, http.StatusServiceUnavailable,
			"This app isn't reachable right now. Try again in a moment.")
		return
	}

	target, err := url.Parse(upstream)
	if err != nil {
		p.fail(w, r, http.StatusServiceUnavailable, "This app isn't reachable right now.")
		return
	}

	p.forward(w, r, target, token, principal, prefix)
}

// forward sets the headers and proxies the request.
func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, target *url.URL, token string, principal authz.Principal, prefix string) {
	rp := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host

			// 7. STRIP every inbound header in Pando's namespace.
			//
			// This is a security requirement, not hygiene. Without it a client
			// sets X-Pando-User: admin@corp.com and any app trusting the
			// convenience headers is trivially spoofed.
			//
			// Done on the OUTBOUND request, unconditionally, before anything is
			// set — so there is no ordering in which a forged value survives.
			stripInbound(pr.Out.Header)

			// 8. Set the assertion and the convenience headers.
			pr.Out.Header.Set(assertion.Header, token)
			pr.Out.Header.Set(HeaderUser, subjectOf(principal))
			if principal.Email != "" {
				pr.Out.Header.Set(HeaderEmail, principal.Email)
			}
			if len(principal.Groups) > 0 {
				pr.Out.Header.Set(HeaderGroups, strings.Join(principal.Groups, ","))
			}

			// 9. Path mode: strip the prefix and say what was stripped (R-167).
			if prefix != "" {
				pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, prefix)
				if !strings.HasPrefix(pr.Out.URL.Path, "/") {
					pr.Out.URL.Path = "/" + pr.Out.URL.Path
				}
				pr.Out.Header.Set("X-Forwarded-Prefix", prefix)
			}

			pr.SetXForwarded()
		},

		// R-170: streaming must work. FlushInterval -1 disables response
		// buffering entirely, so SSE arrives as it is produced rather than in
		// one lump at the end. Websocket upgrades are hijacked by
		// ReverseProxy itself and are unaffected by this.
		FlushInterval: -1,

		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.From(r.Context()).Warn("upstream failed", zap.Error(err))
			p.fail(w, r, http.StatusBadGateway,
				"This app didn't respond. It may still be starting up.")
		},

		// No response body limit, deliberately (R-170): large uploads and
		// downloads must pass through.
		Transport: transport(),
	}

	// Long-lived connections are re-authorized for as long as they stay open
	// (O-13). Wrapping here rather than inside ReverseProxy keeps the decision
	// with the rest of the authorization logic.
	rp.ServeHTTP(&reauthorizing{
		ResponseWriter: w,
		proxy:          p,
		principal:      principal,
		appID:          appIDOf(r),
		ctx:            r.Context(),
	}, r)
}

type ctxKeyAppID struct{}

func appIDOf(r *http.Request) string {
	id, _ := r.Context().Value(ctxKeyAppID{}).(string)
	return id
}

// stripInbound removes every header in Pando's namespace.
//
// Iterating the map and deleting by prefix — rather than deleting a known list —
// is deliberate: a header added to the namespace later is stripped without
// anyone remembering to update this. Header keys are canonicalized by net/http,
// so a lowercase or mixed-case forgery is caught by the same comparison.
func stripInbound(h http.Header) {
	for name := range h {
		if strings.HasPrefix(http.CanonicalHeaderKey(name), HeaderPrefix) {
			h.Del(name)
		}
	}
	// X-Forwarded-Prefix is Pando's to set, so an inbound one is also a forgery.
	h.Del("X-Forwarded-Prefix")
}

// resolve finds the app and, in path mode, the prefix to strip.
func (p *Proxy) resolve(r *http.Request) (state.App, *spec.AppSpec, string, bool, error) {
	ctx := r.Context()

	if p.Mode == spec.RoutingPath {
		segment := firstSegment(r.URL.Path)
		if segment == "" {
			return state.App{}, nil, "", false, nil
		}
		app, s, found, err := p.Resolver.BySlug(ctx, segment)
		return app, s, "/" + segment, found, err
	}

	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	app, s, found, err := p.Resolver.ByHostname(ctx, host)
	return app, s, "", found, err
}

// redirectToLogin sends an anonymous caller to sign in.
func (p *Proxy) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	loginPath := p.LoginPath
	if loginPath == "" {
		loginPath = "/login"
	}
	target := loginPath + "?next=" + url.QueryEscape(r.URL.RequestURI())
	http.Redirect(w, r, target, http.StatusFound)
}

// auditDenial records a refused request.
//
// Only for authenticated callers. An anonymous caller reaching a private app is
// the ordinary case — every crawler and stray link does it — and auditing those
// would bury the denials that mean something.
func (p *Proxy) auditDenial(r *http.Request, principal authz.Principal, appID string) {
	if p.Auditor == nil {
		return
	}
	_ = p.Auditor.Write(r.Context(), audit.Event{
		PrincipalKind: audit.PrincipalKind(principal.Kind),
		PrincipalID:   principal.ID,
		OnBehalfOf:    principal.UserID,
		Action:        "app.use.denied",
		AppID:         appID,
		Detail:        map[string]any{"path": r.URL.Path},
	})
}

// fail writes an error page in the product's voice.
//
// Plain text rather than the API's JSON envelope: the caller here is a browser
// showing a page to a person, not a client parsing a response.
func (p *Proxy) fail(w http.ResponseWriter, r *http.Request, status int, message string) {
	log.From(r.Context()).Info("proxy refused", zap.Int("status", status))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintln(w, message)
}

// subjectOf returns the assertion subject for a principal.
//
// An anonymous request still gets an assertion, with the constant
// "anonymous" (R-056). The consequence, which the app-developer documentation
// must state: absence of the assertion header means the request did not come
// through Pando at all, and an app may reject on that basis.
func subjectOf(p authz.Principal) string {
	if p.Kind == authz.KindAnonymous || p.UserID == "" {
		if p.Kind == authz.KindToken && p.ID != "" {
			return p.ID
		}
		return assertion.AnonymousSubject
	}
	return p.UserID
}

func firstSegment(path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	if i := strings.IndexByte(trimmed, '/'); i >= 0 {
		return trimmed[:i]
	}
	return trimmed
}

func transport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,

		// Streaming depends on this: with compression enabled the transport
		// buffers to decompress, which defeats FlushInterval for SSE.
		DisableCompression: true,
	}
}
