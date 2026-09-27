package traefik_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/adapter/routing/traefik"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// A Traefik somebody else runs, and the edges of the settings form.

func TestTheKindOffersTheFiveProvidersAndOther(t *testing.T) {
	a := traefik.New()
	require.Equal(t, traefik.Kind, a.Kind())
	require.Equal(t, api.CategoryRouting, a.Category())

	fields := map[string]api.Field{}
	for _, f := range traefik.Info().Fields {
		fields[f.Key] = f
	}
	provider := fields["dns_provider"]
	require.Equal(t, "select", provider.Type)
	require.True(t, provider.Other, "any provider Traefik knows, typed in")
	require.Len(t, provider.Options, 5)
	require.Equal(t, []string{"dns"}, provider.ShownWhen.Values)

	creds := fields["dns_credentials"]
	require.True(t, creds.Credential && creds.Multiline)
	for _, v := range []string{"CF_DNS_API_TOKEN", "AWS_SECRET_ACCESS_KEY", "DO_AUTH_TOKEN", "PORKBUN_SECRET_API_KEY", "NAMECHEAP_API_KEY"} {
		require.Contains(t, creds.Help, v)
	}

	require.Len(t, fields["certificates"].Options, 3, "three choices, shown as radio buttons")
	require.Equal(t, "true", fields["managed"].Default, "Pando runs Traefik unless told otherwise (R-174)")
}

// TestR190_DNSCredentialsStoredInTheClearAreRefused asserts R-190.
func TestR190_DNSCredentialsStoredInTheClearAreRefused(t *testing.T) {
	err := traefik.New().Configure(context.Background(), json.RawMessage(`{"dir":"/tmp/x","dns_credentials":"CF_DNS_API_TOKEN=abc"}`))
	var e *errs.Error
	require.ErrorAs(t, err, &e)
	require.Contains(t, e.Message, "not encrypted")
	require.NotContains(t, e.Message, "abc")
}

func TestBadSettingsAreRefusedWithTheReason(t *testing.T) {
	for cfg, want := range map[string]string{
		`{`:                               "could not be read",
		`{"dir":""}`:                      "directory",
		`{"dir":"DIR","http_port":70000}`: "port number",
		`{"dir":"DIR","certificates":"dns","acme_email":"a@b.c","base_domain":"x.test","dns_provider":"Bad Name"}`: "DNS provider",
	} {
		dir := t.TempDir()
		err := traefik.New().Configure(context.Background(), json.RawMessage(strings.Replace(cfg, "DIR", dir, 1)))
		require.ErrorContains(t, err, want, cfg)
	}
}

func TestSomebodyElsesTraefikUsesItsOwnEntrypointAndResolver(t *testing.T) {
	a, dir := adapter(t, `{"dir":"DIR","managed":false,"entrypoint":"https","cert_resolver":"le","base_domain":"apps.test"}`)

	caps, err := a.Capabilities(context.Background())
	require.NoError(t, err)
	require.True(t, caps.SupportsTLS)
	require.Equal(t, "apps.test", caps.BaseDomain)

	_, err = a.Ensure(context.Background(), api.RouteRequest{
		AppID: "app_01", Mode: spec.RoutingSubdomain, Hostname: "notes.apps.test",
		ProxyUpstream: "http://pando:8080", TLS: api.TLSRequest{Enabled: true},
	})
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(dir, "pando-app_01.yml"))
	require.NoError(t, err)
	require.Contains(t, string(body), "- https")
	require.Contains(t, string(body), "certResolver: le")
	require.NotContains(t, string(body), "domains:", "a wildcard is only asked of the Traefik Pando runs")
}

func TestWithoutCertificatesRoutesAreOnPlainHTTP(t *testing.T) {
	a, dir := adapter(t, "")
	_, err := a.Ensure(context.Background(), api.RouteRequest{
		AppID: "app_01", Mode: spec.RoutingPath, PathPrefix: "notes", Hostname: "pando.test",
		ProxyUpstream: "http://pando:8080", TLS: api.TLSRequest{Enabled: true},
	})
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join(dir, "pando-app_01.yml"))
	require.NoError(t, err)
	require.Contains(t, string(body), "- web")
	require.NotContains(t, string(body), "tls:", "no certificate was configured, so none is asked for")
	require.Contains(t, string(body), "Host(`pando.test`) && PathPrefix(`/notes`)")
}

func TestTheConsoleRouteIsWrittenOnlyWhenItChanges(t *testing.T) {
	a, dir := adapter(t, "")
	req := api.EdgeRequest{Ref: "rte_traefik", ProxyUpstream: "http://pando:8080"}
	_, _, err := a.Edge(context.Background(), req)
	require.NoError(t, err)

	path := filepath.Join(dir, "pando-console.yml")
	past := mustStat(t, path).ModTime().Add(-3600e9)
	require.NoError(t, os.Chtimes(path, past, past))

	_, _, err = a.Edge(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, past, mustStat(t, path).ModTime(), "Traefik reloads on every write, so an unchanged route is not rewritten")

	_, _, err = a.Edge(context.Background(), api.EdgeRequest{Ref: "rte_traefik"})
	require.ErrorContains(t, err, "where Traefik should send")
}

func TestAConfigurationDirectoryThatCannotBeWrittenIsNamed(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	a := traefik.New()
	require.NoError(t, a.Configure(context.Background(), json.RawMessage(`{"dir":"`+filepath.Join(blocker, "sub")+`"}`)))

	_, _, err := a.Edge(context.Background(), api.EdgeRequest{Ref: "rte_traefik", ProxyUpstream: "http://pando:8080"})
	require.ErrorContains(t, err, "could not write Traefik's configuration")
	_, err = a.Ensure(context.Background(), api.RouteRequest{AppID: "app_01", Mode: spec.RoutingSubdomain,
		Hostname: "n.test", ProxyUpstream: "http://pando:8080"})
	require.Error(t, err)
	require.Error(t, a.HealthCheck(context.Background()))
}

func TestObserveAndRemoveFollowTheFile(t *testing.T) {
	a, _ := adapter(t, "")
	ctx := context.Background()
	h, err := a.Ensure(ctx, api.RouteRequest{AppID: "app 01/../x", Mode: spec.RoutingSubdomain,
		Hostname: "n.test", ProxyUpstream: "http://pando:8080"})
	require.NoError(t, err)
	require.NotContains(t, filepath.Base(h.Handle), "/", "an ID cannot climb out of the directory")

	state, err := a.Observe(ctx, api.RouteHandle{AppID: "app 01/../x"})
	require.NoError(t, err)
	require.True(t, state.Present)
	require.Equal(t, "http://pando:8080", state.Address)

	require.NoError(t, a.Remove(ctx, api.RouteHandle{AppID: "app 01/../x"}))
	require.NoError(t, a.Remove(ctx, h), "removing twice is not an error")
	state, err = a.Observe(ctx, h)
	require.NoError(t, err)
	require.False(t, state.Present)
}

func TestAHostlessOrPathlessRouteIsRefused(t *testing.T) {
	a, _ := adapter(t, "")
	_, err := a.Ensure(context.Background(), api.RouteRequest{AppID: "a", Mode: spec.RoutingSubdomain, ProxyUpstream: "http://p"})
	require.ErrorContains(t, err, "no hostname")
	_, err = a.Ensure(context.Background(), api.RouteRequest{AppID: "a", Mode: spec.RoutingPath, ProxyUpstream: "http://p"})
	require.ErrorContains(t, err, "no path")
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	return info
}
