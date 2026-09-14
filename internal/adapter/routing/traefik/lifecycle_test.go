package traefik_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/adapter/routing/traefik"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

func configuredIn(t *testing.T, dir string) *traefik.Adapter {
	t.Helper()
	a := traefik.New()
	raw, err := json.Marshal(map[string]string{"dir": dir, "base_domain": "apps.example"})
	require.NoError(t, err)
	require.NoError(t, a.Configure(context.Background(), raw))
	return a
}

func TestIdentity(t *testing.T) {
	a := traefik.New()
	require.Equal(t, traefik.Kind, a.Kind())
	require.Equal(t, api.CategoryRouting, a.Category())
}

func TestConfigureDefaultsToTheStandardDirectory(t *testing.T) {
	a := traefik.New()
	require.NoError(t, a.Configure(context.Background(), nil))

	// The default is /etc/traefik/dynamic, which a test cannot write to — what
	// matters is that it resolved to one rather than staying unconfigured.
	err := a.HealthCheck(context.Background())
	if err != nil {
		require.Contains(t, err.Error(), "/etc/traefik/dynamic")
	}
}

func TestConfigureRefusesAMalformedDocumentOrAnEmptyDirectory(t *testing.T) {
	err := traefik.New().Configure(context.Background(), json.RawMessage(`{`))
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))

	err = traefik.New().Configure(context.Background(), json.RawMessage(`{"dir":""}`))
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Remedy, "file provider",
		"R-105: the remedy says which directory it means")
}

// Not whether Traefik is running: Pando does not manage Traefik and cannot see
// it. What it can check is that a route it writes will land somewhere Traefik
// reads — a directory that has become read-only produces routes that silently
// never appear.
func TestHealthCheckVerifiesTheConfigurationDirectoryIsWritable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dynamic")
	a := configuredIn(t, dir)

	require.NoError(t, a.HealthCheck(context.Background()))
	require.DirExists(t, dir, "the directory is created rather than demanded")

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "the probe is removed again")

	require.Error(t, traefik.New().HealthCheck(context.Background()),
		"an unconfigured adapter is not healthy")
}

func TestHealthCheckFailsWhenTheDirectoryCannotExist(t *testing.T) {
	file := filepath.Join(t.TempDir(), "occupied")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))

	a := configuredIn(t, filepath.Join(file, "dynamic"))
	err := a.HealthCheck(context.Background())
	require.Equal(t, errs.AdapterFailed, errs.CodeOf(err))
}

// R-023: a route makes traffic arrive at Pando's proxy, never at the workload.
func TestR023_ARouteIsWrittenRemovedAndObserved(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dynamic")
	a := configuredIn(t, dir)
	ctx := context.Background()

	handle, err := a.Ensure(ctx, api.RouteRequest{
		AppID: "app_01HQ8", Mode: spec.RoutingSubdomain,
		Hostname: "notes.apps.example", ProxyUpstream: "http://pando:8080",
	})
	require.NoError(t, err)
	require.NotEmpty(t, handle.Handle)
	require.FileExists(t, handle.Handle)

	body, err := os.ReadFile(handle.Handle)
	require.NoError(t, err)
	require.Contains(t, string(body), "http://pando:8080",
		"the route points at Pando's proxy and not at a container")

	state, err := a.Observe(ctx, handle)
	require.NoError(t, err)
	require.True(t, state.Present)

	require.NoError(t, a.Remove(ctx, handle))
	require.NoFileExists(t, handle.Handle)

	state, err = a.Observe(ctx, handle)
	require.NoError(t, err)
	require.False(t, state.Present)
}

// Removing a route that is not there is not an error: the outcome the caller
// asked for is already true, and the janitor retries teardown.
func TestRemovingARouteThatIsNotThereIsNotAnError(t *testing.T) {
	a := configuredIn(t, filepath.Join(t.TempDir(), "dynamic"))
	ctx := context.Background()

	require.NoError(t, a.Remove(ctx, api.RouteHandle{AppID: "app_never_routed"}))
	require.NoError(t, a.Remove(ctx, api.RouteHandle{
		AppID: "app_01HQ8", Handle: filepath.Join(t.TempDir(), "gone.yml"),
	}))
}

// A handle the caller does not have is derived from the app ID, so teardown
// works for a route whose handle was never recorded.
func TestARouteCanBeRemovedByAppIDAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dynamic")
	a := configuredIn(t, dir)
	ctx := context.Background()

	handle, err := a.Ensure(ctx, api.RouteRequest{
		AppID: "app_01HQ8", Mode: spec.RoutingSubdomain,
		Hostname: "notes.apps.example", ProxyUpstream: "http://pando:8080",
	})
	require.NoError(t, err)

	require.NoError(t, a.Remove(ctx, api.RouteHandle{AppID: "app_01HQ8"}))
	require.NoFileExists(t, handle.Handle)
}

// Observe reports what exists and never remediates — an adapter that quietly
// rewrote a missing file would make drift undetectable (design 05).
func TestObserveNeverRewritesWhatIsMissing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dynamic")
	a := configuredIn(t, dir)
	ctx := context.Background()

	handle, err := a.Ensure(ctx, api.RouteRequest{
		AppID: "app_01HQ8", Mode: spec.RoutingSubdomain,
		Hostname: "notes.apps.example", ProxyUpstream: "http://pando:8080",
	})
	require.NoError(t, err)
	require.NoError(t, os.Remove(handle.Handle))

	state, err := a.Observe(ctx, handle)
	require.NoError(t, err)
	require.False(t, state.Present)
	require.NoFileExists(t, handle.Handle, "observing put it back, which hides the drift")
}
