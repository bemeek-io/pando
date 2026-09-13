package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/httpapi"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func newServer(dbErr error) http.Handler {
	return (&httpapi.Server{Logger: zap.NewNop(), DB: fakeDB{err: dbErr}}).Routes()
}

func TestHealthzRespondsWithoutTouchingTheDatabase(t *testing.T) {
	// Liveness must not depend on Postgres: a check that fails when the database
	// blips gets the process killed just as it was about to recover.
	rec := httptest.NewRecorder()
	newServer(errors.New("database is down")).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}

func TestReadyzReflectsTheDatabase(t *testing.T) {
	rec := httptest.NewRecorder()
	newServer(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	newServer(errors.New("connection refused")).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	// The reason is a category, not the driver's error text.
	require.NotContains(t, rec.Body.String(), "connection refused")
}

func TestEveryResponseCarriesARequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	newServer(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	rid := rec.Header().Get(httpapi.RequestIDHeader)
	require.NotEmpty(t, rid)
	require.Regexp(t, `^req_[0-9A-HJKMNP-TV-Z]{26}$`, rid)
}

func TestErrorEnvelopeCarriesCodeStatusAndRequestID(t *testing.T) {
	handler := httpapi.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpapi.Error(w, r, errs.New(errs.PlanSlotUnfilled, "This app needs a Redis, and one hasn't been chosen yet.").
			WithRemedy("Choose how to fill the REDIS_URL slot.").
			WithDetail("slots", []string{"REDIS_URL"}))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusConflict, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "PLAN_SLOT_UNFILLED", body["code"])
	require.NotEmpty(t, body["remedy"])
	require.Equal(t, rec.Header().Get(httpapi.RequestIDHeader), body["request_id"])
}

// An error with no envelope must become INTERNAL, and its detail must not reach
// the response body.
func TestUnenvelopedErrorDoesNotLeakDetail(t *testing.T) {
	handler := httpapi.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpapi.Error(w, r, errors.New("pq: relation \"users\" does not exist"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "relation")

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "INTERNAL", body["code"])
}

// appHosts answers yes for one hostname, the way the real resolver does for an
// app whose spec carries it.
type appHosts struct{ host string }

func (a appHosts) IsAppHostname(_ context.Context, host string) (bool, error) {
	return host == a.host, nil
}

// TestR172_SigningInWorksOnAnAppsOwnHostname asserts R-172.
//
// "Users bookmark URLs and carry a session. Pando is invisible except at
// login" — and at login it was invisible in the wrong sense. An unauthenticated
// visit to a subdomain app redirected to "/login" on the app's own hostname,
// where every path belongs to the app, so the router handed it to the proxy,
// which redirected to "/login" again. An infinite loop with the query string
// growing on each hop, and subdomain routing — the mode Traefik makes the
// default — unusable for any app that was not public.
func TestR172_SigningInWorksOnAnAppsOwnHostname(t *testing.T) {
	var reached string
	console := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = "console" })
	appProxy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = "proxy"
		// What the real proxy does for an unauthenticated caller.
		http.Redirect(w, r, httpapi.LoginPath+"?next="+r.URL.RequestURI(), http.StatusFound)
	})

	handler := (&httpapi.Server{
		Logger: zap.NewNop(), DB: fakeDB{},
		Console: console, AppProxy: appProxy, AppHosts: appHosts{host: "notes.example.com"},
	}).Routes()

	// 1. The app itself, unauthenticated: the proxy answers and sends them to
	//    sign in.
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req.Host = "notes.example.com"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, "proxy", reached)
	require.Equal(t, http.StatusFound, rec.Code)
	location := rec.Header().Get("Location")
	require.Equal(t, "/.pando/login?next=/dashboard", location)

	// 2. Following it lands on Pando's own sign-in page, on the app's hostname
	//    — not back at the proxy, which is the loop.
	req = httptest.NewRequest(http.MethodGet, location, nil)
	req.Host = "notes.example.com"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, "console", reached,
		"the sign-in page is Pando's on every hostname, or there is nowhere to sign in")
	require.NotEqual(t, http.StatusFound, rec.Code, "and it does not redirect anywhere")

	// 3. And so are the things that page loads. A sign-in page whose script
	//    404s, or is redirected to the sign-in page, is a blank screen — which
	//    is exactly what happened when "/assets/*" was dropped from the
	//    console's routes.
	for _, path := range []string{"/.pando/assets/index-abc123.js", "/.pando/api/v1/sessions"} {
		reached = ""
		req = httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "notes.example.com"
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		require.NotEqual(t, "proxy", reached, "%s must not fall through to the app", path)
	}
}

// TestR171_TheAppKeepsItsOwnLoginPath asserts R-171.
//
// An app that presents its own login page is that app working correctly, and
// Pando does not remediate it. Reserving "/login" for Pando's sign-in page on
// every hostname would have shadowed it — which is why the reserved path is
// "/.pando", something a slug cannot contain.
func TestR171_TheAppKeepsItsOwnLoginPath(t *testing.T) {
	var reached string
	console := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = "console" })
	appProxy := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = "proxy" })

	handler := (&httpapi.Server{
		Logger: zap.NewNop(), DB: fakeDB{},
		Console: console, AppProxy: appProxy, AppHosts: appHosts{host: "notes.example.com"},
	}).Routes()

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Host = "notes.example.com"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.Equal(t, "proxy", reached, "/login on an app's hostname is the app's own")
}

// TestR172_SigningInWorksOnAPortModeAppsOwnListener asserts R-172 on the
// listener a port-mode app gets to itself.
//
// A port-mode app has a socket of its own and the router is not in front of it,
// so the reserved path has to be carried there too — otherwise the sign-in
// redirect lands back on the proxy, and the loop R-172 removes comes back one
// listener over.
func TestR172_SigningInWorksOnAPortModeAppsOwnListener(t *testing.T) {
	var reached string
	console := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = "console" })
	app := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = "app" })

	handler := httpapi.ReservedOrApp(console, app)

	for _, tc := range []struct{ path, want string }{
		{httpapi.LoginPath, "console"},
		{"/.pando/assets/index.js", "console"},
		{"/", "app"},
		{"/login", "app"},
		// Not a prefix match on the string: an app's own path may begin with
		// those characters.
		{"/.pandowhatever", "app"},
	} {
		reached = ""
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, tc.path, nil))
		require.Equal(t, tc.want, reached, "%s", tc.path)
	}
}
