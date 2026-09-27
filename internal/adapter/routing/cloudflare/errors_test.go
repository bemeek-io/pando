package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/secret"
)

// The paths where Cloudflare, or the configuration, says no — each one an
// error an operator reads, so each is checked for saying something usable.

func TestTheKindDescribesItself(t *testing.T) {
	a := New()
	require.Equal(t, Kind, a.Kind())
	require.Equal(t, api.CategoryRouting, a.Category())

	info := Info()
	require.Equal(t, Kind, info.Kind)
	keys := map[string]api.Field{}
	for _, f := range info.Fields {
		keys[f.Key] = f
	}
	require.True(t, keys["api_token"].Credential, "the token is stored encrypted (R-190)")
	require.Contains(t, keys["api_token"].Help, tokenPermissions)
	require.NotContains(t, keys, "base_domain", "apps are <app>.<zone>; there is no deeper option")
	require.NotContains(t, keys, "api_base", "a test hook, not a setting")
}

func TestConfigurationThatCannotBeReadIsRefused(t *testing.T) {
	err := New().Configure(context.Background(), json.RawMessage(`{`))
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))

	err = New().Configure(context.Background(), json.RawMessage(`{"account_id":1}`))
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))

	err = New().Configure(context.Background(), json.RawMessage(
		`{"account_id":"`+account+`","zone":"localhost","credentials":{"api_token":"t"}}`))
	require.ErrorContains(t, err, "zone")

	err = New().Configure(context.Background(), nil)
	require.ErrorContains(t, err, "API token", "nothing configured says what is missing first")
}

func TestTheDefaultImageAndClientAreFilledIn(t *testing.T) {
	a := New()
	require.NoError(t, a.Configure(context.Background(), json.RawMessage(
		`{"account_id":"`+account+`","zone":"Example.COM.","credentials":{"api_token":"t"}}`)))
	require.Equal(t, DefaultImage, a.config.Image)
	require.Equal(t, "example.com", a.config.Zone)
	require.Equal(t, "example.com", a.config.ConsoleHostname)
	require.Equal(t, DefaultAPIBase, a.api.base)
}

func TestAnAPIThatIsNotCloudflaresIsSaidSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	}))
	defer srv.Close()
	err := newClient(srv.URL, secret.New("t")).do(context.Background(), http.MethodGet, "/zones", nil, nil, nil)
	require.ErrorContains(t, err, "not its API's format")
	require.ErrorContains(t, err, "502")
}

func TestAnUnreachableAPISaysWhatToCheck(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	err := newClient(srv.URL, secret.New("t")).do(context.Background(), http.MethodGet, "/zones", nil, nil, nil)
	var e *errs.Error
	require.ErrorAs(t, err, &e)
	require.Equal(t, errs.AdapterUnavailable, e.Code)
	require.Contains(t, e.Remedy, "api.cloudflare.com")
}

func TestARefusalNamesCloudflaresReasons(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1003,"message":"Invalid zone."},{"code":7,"message":"No route"}]}`))
	}))
	defer srv.Close()
	err := newClient(srv.URL, secret.New("t")).do(context.Background(), http.MethodPost, "/zones", nil, map[string]string{"a": "b"}, nil)
	require.ErrorContains(t, err, "Invalid zone (code 1003); No route (code 7).")
	require.Equal(t, "no reason given.", joinErrors(nil))
}

func TestAResultThatDoesNotFitIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":"not a list"}`))
	}))
	defer srv.Close()
	var out []zone
	err := newClient(srv.URL, secret.New("t")).do(context.Background(), http.MethodGet, "/zones", nil, nil, &out)
	require.ErrorContains(t, err, "could not read Cloudflare's answer")
}

func TestAZoneTheTokenCannotSeeIsNamed(t *testing.T) {
	_, srv := newFake(t)
	a := New()
	require.NoError(t, a.Configure(context.Background(), json.RawMessage(
		`{"account_id":"`+account+`","zone":"other.org","api_base":"`+srv.URL+`","credentials":{"api_token":"tok-cf"}}`)))
	err := a.HealthCheck(context.Background())
	var e *errs.Error
	require.ErrorAs(t, err, &e)
	require.Contains(t, e.Message, "other.org")
	require.Contains(t, e.Remedy, "Zone: Read")
}

func TestAnAttachedTunnelThatIsGoneIsNamed(t *testing.T) {
	_, srv := newFake(t)
	gone := "11111111-2222-4333-8444-555555555555"
	a := configured(t, srv, `,"tunnel_id":"`+gone+`"`)
	_, err := a.Ensure(context.Background(), subdomain("app_01", "notes.example.com"))
	require.ErrorContains(t, err, gone)
}

func TestEnsureRefusesWhatItCannotRoute(t *testing.T) {
	_, srv := newFake(t)
	a := configured(t, srv, "")
	ctx := context.Background()

	_, err := a.Ensure(ctx, api.RouteRequest{AppID: "app_01", Mode: spec.RoutingSubdomain, Hostname: "n.example.com"})
	require.ErrorContains(t, err, "where to send")

	_, err = a.Ensure(ctx, api.RouteRequest{AppID: "app_01", Mode: spec.RoutingSubdomain, ProxyUpstream: upstream})
	require.ErrorContains(t, err, "no hostname")

	_, err = a.Ensure(ctx, api.RouteRequest{AppID: "app_01", Mode: spec.RoutingPath, ProxyUpstream: upstream})
	require.ErrorContains(t, err, "no path")

	_, err = a.Ensure(ctx, api.RouteRequest{AppID: "app_01", Mode: spec.RoutingPort, Port: 9001, ProxyUpstream: upstream})
	require.ErrorContains(t, err, "does not handle")
}

func TestAPathWithoutItsSlashIsAnchoredAnyway(t *testing.T) {
	f, srv := newFake(t)
	a := configured(t, srv, "")
	_, err := a.Ensure(context.Background(), api.RouteRequest{
		AppID: "app_01", Mode: spec.RoutingPath, PathPrefix: "team/notes/", Hostname: "pando.example.com", ProxyUpstream: upstream,
	})
	require.NoError(t, err)
	rules := f.ingress(t, f.onlyTunnel(t))
	require.Equal(t, `^/team/notes(/.*)?$`, rules[0].Path)
}

func TestRemovingKeepsANameAnotherRuleStillUses(t *testing.T) {
	f, srv := newFake(t)
	a := configured(t, srv, "")
	ctx := context.Background()

	host, err := a.Ensure(ctx, subdomain("app_01", "shared.example.com"))
	require.NoError(t, err)
	_, err = a.Ensure(ctx, api.RouteRequest{AppID: "app_02", Mode: spec.RoutingPath, PathPrefix: "/docs",
		Hostname: "shared.example.com", ProxyUpstream: upstream})
	require.NoError(t, err)

	require.NoError(t, a.Remove(ctx, host))
	_, ok := f.record("shared.example.com")
	require.True(t, ok, "the path rule still needs the name")

	require.NoError(t, a.Remove(ctx, api.RouteHandle{}), "a handle with nothing in it removes nothing")
}

func TestRemovingNeverTouchesARecordPandoDidNotMake(t *testing.T) {
	f, srv := newFake(t)
	a := configured(t, srv, "")
	ctx := context.Background()
	h, err := a.Ensure(ctx, subdomain("app_01", "notes.example.com"))
	require.NoError(t, err)

	// Somebody adds a TXT record at the same name.
	f.mu.Lock()
	f.records["txt"] = dnsRecord{ID: "txt", Type: "TXT", Name: "notes.example.com", Content: "v=spf1"}
	f.mu.Unlock()

	require.NoError(t, a.Remove(ctx, h))
	f.mu.Lock()
	defer f.mu.Unlock()
	_, kept := f.records["txt"]
	require.True(t, kept)
	for _, r := range f.records {
		require.NotEqual(t, ManagedComment, r.Comment, "Pando's own record is gone")
	}
}

func TestObservingBeforeAnyTunnelFindsNothing(t *testing.T) {
	_, srv := newFake(t)
	a := configured(t, srv, "")
	state, err := a.Observe(context.Background(), api.RouteHandle{Handle: handleOf(ingressRule{Hostname: "n.example.com"})})
	require.NoError(t, err)
	require.False(t, state.Present)

	_, err = a.Ensure(context.Background(), subdomain("app_01", "a.example.com"))
	require.NoError(t, err)
	state, err = a.Observe(context.Background(), api.RouteHandle{Handle: handleOf(ingressRule{Hostname: "gone.example.com"})})
	require.NoError(t, err)
	require.False(t, state.Present, "a rule that is not there is reported as not there")
}

func TestAnEdgeNeedsToKnowWherePandoIs(t *testing.T) {
	_, srv := newFake(t)
	_, _, err := configured(t, srv, "").Edge(context.Background(), api.EdgeRequest{Ref: "rte_cf"})
	require.ErrorContains(t, err, "where the tunnel should send")
}

func TestARefusedTokenStopsTheEdgeWithTheReason(t *testing.T) {
	f, srv := newFake(t)
	a := configured(t, srv, "")
	f.refuse = true
	_, _, err := a.Edge(context.Background(), api.EdgeRequest{Ref: "rte_cf", ProxyUpstream: upstream})
	require.ErrorContains(t, err, "refused the API token")
	_, err = a.Ensure(context.Background(), subdomain("app_01", "n.example.com"))
	require.Error(t, err)
	require.Error(t, a.Remove(context.Background(), api.RouteHandle{Handle: handleOf(ingressRule{Hostname: "n.example.com"})}))
	_, err = a.Observe(context.Background(), api.RouteHandle{Handle: handleOf(ingressRule{Hostname: "n.example.com"})})
	require.Error(t, err)
}

func TestAnEmptyTunnelConfigurationIsAnEmptyIngress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/configurations") && strings.Contains(r.URL.Path, "bad"):
			_, _ = w.Write([]byte(`{"success":true,"result":{"config":{"ingress":"nope"}}}`))
		case strings.HasSuffix(r.URL.Path, "/configurations"):
			_, _ = w.Write([]byte(`{"success":true,"result":{"config":null}}`))
		default:
			_, _ = w.Write([]byte(`{"success":true,"result":[]}`))
		}
	}))
	defer srv.Close()
	c := newClient(srv.URL, secret.New("t"))
	cfg, err := c.tunnelConfig(context.Background(), account, "t1")
	require.NoError(t, err)
	require.Empty(t, cfg.Ingress)

	_, err = c.tunnelConfig(context.Background(), account, "bad")
	require.ErrorContains(t, err, "ingress rules")

	found, err := c.tunnelByName(context.Background(), account, "pando-example.com")
	require.NoError(t, err)
	require.Nil(t, found)
}

func TestHostOfAnUnreadableUpstreamIsEmpty(t *testing.T) {
	require.Equal(t, "", hostOf("://bad"))
	require.Equal(t, "pando", hostOf("http://pando:8080"))
}
