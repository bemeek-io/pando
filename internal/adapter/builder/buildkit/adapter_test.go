package buildkit

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

// view is a source on disk, which is what Bid and Plan read.
type view struct{ root string }

func (v view) Root() string { return v.root }

func (v view) Open(name string) (io.ReadCloser, error) { return os.Open(filepath.Join(v.root, name)) }

func (v view) Stat(name string) (api.FileInfo, error) {
	info, err := os.Stat(filepath.Join(v.root, name))
	if err != nil {
		return api.FileInfo{}, err
	}
	return api.FileInfo{Name: info.Name(), Size: info.Size(), IsDir: info.IsDir()}, nil
}

func (v view) Glob(pattern string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(v.root, pattern))
	for i, m := range matches {
		matches[i], _ = filepath.Rel(v.root, m)
	}
	return matches, err
}

func TestIdentity(t *testing.T) {
	a := New()
	require.Equal(t, Kind, a.Kind())
	require.Equal(t, api.CategoryBuilder, a.Category())
}

// Dialing is deferred: BuildKit may still be starting when Pando does, and an
// adapter that refuses to configure would be dropped from the registry for the
// life of the process.
func TestConfigureSucceedsEvenWhenBuildKitIsNotUpYet(t *testing.T) {
	a := New()
	require.NoError(t, a.Configure(context.Background(),
		json.RawMessage(`{"address":"tcp://127.0.0.1:1"}`)))
	require.Equal(t, "tcp://127.0.0.1:1", a.address)

	// HealthCheck reports the truth instead, and the planner turns that into a
	// readable refusal.
	err := a.HealthCheck(context.Background())
	require.Equal(t, errs.AdapterUnavailable, errs.CodeOf(err))
}

func TestTheAddressComesFromConfigThenTheEnvironmentThenADefault(t *testing.T) {
	a := New()
	require.NoError(t, a.Configure(context.Background(), nil))
	require.Equal(t, "tcp://buildkit:1234", a.address)

	t.Setenv("PANDO_BUILDKIT_ADDRESS", "tcp://elsewhere:1234")
	b := New()
	require.NoError(t, b.Configure(context.Background(), nil))
	require.Equal(t, "tcp://elsewhere:1234", b.address)

	c := New()
	require.NoError(t, c.Configure(context.Background(), json.RawMessage(`{"address":"tcp://explicit:1234"}`)))
	require.Equal(t, "tcp://explicit:1234", c.address, "configuration wins over the environment")
}

func TestConfigureRejectsAMalformedDocument(t *testing.T) {
	err := New().Configure(context.Background(), json.RawMessage(`{`))
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))
}

// R-254: capabilities are data, so the planner can refuse at plan time.
func TestR254_CapabilitiesAreReportedHonestly(t *testing.T) {
	caps, err := New().Capabilities(context.Background())
	require.NoError(t, err)

	// Rootless BuildKit in a container: a shared kernel, like the runtime.
	// Reported honestly so a policy floor above this excludes it (R-114).
	require.Equal(t, spec.IsolationContainer, caps.IsolationClass)

	require.True(t, caps.Supports(spec.BuildDockerfile))
	require.True(t, caps.Supports(spec.BuildStatic))
	require.True(t, caps.Supports(spec.BuildBuildpack))
	require.True(t, caps.Supports(spec.BuildCompose),
		"a compose app's pieces are Dockerfile builds, one per service that builds")
	require.False(t, caps.Supports(spec.BuildPrebuilt), "a prebuilt image is not built")

	require.True(t, caps.SupportsCache)

	// Claiming egress restriction would turn R-118 into a promise nothing
	// keeps, so the planner refuses a spec that needs it instead.
	require.False(t, caps.SupportsEgressRestriction)
}

func TestBidIsConfidentOnlyWhenThereIsADockerfile(t *testing.T) {
	root := t.TempDir()

	bid, err := New().Bid(context.Background(), view{root: root})
	require.NoError(t, err)
	require.Zero(t, bid.Confidence)
	require.Equal(t, spec.BuildDockerfile, bid.Strategy)

	require.NoError(t, os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM scratch"), 0o644))
	bid, err = New().Bid(context.Background(), view{root: root})
	require.NoError(t, err)
	require.InDelta(t, 0.92, bid.Confidence, 0.001)
	require.Equal(t, spec.BuildDockerfile, bid.Strategy)
	require.NotEmpty(t, bid.Evidence, "R-102: the user sees the reasoning, not a verdict")
}

// R-112: nothing here runs `docker compose build`, which would want a runtime
// socket and is forbidden outright.
func TestBuildRefusesWhenTheBuildServiceIsNotConnected(t *testing.T) {
	_, err := New().Build(context.Background(), api.BuildRequest{Strategy: spec.BuildDockerfile})
	require.Equal(t, errs.AdapterUnavailable, errs.CodeOf(err))
}

// The build wraps the caller's sink so BuildKit cannot close it: closing it
// here would end the pipe before the caller had finished reading the other side.
func TestTheLogSinkIsNotClosedOnTheCallersBehalf(t *testing.T) {
	var sink strings.Builder
	w := nopWriteCloser{Writer: &sink}

	_, err := w.Write([]byte("step 1\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	_, err = w.Write([]byte("step 2\n"))
	require.NoError(t, err, "the underlying writer is still open")
	require.Equal(t, "step 1\nstep 2\n", sink.String())
}

// R-117 keeps a per-app cache namespace, and the path is resolved on Pando's
// side of the connection: a local cache export is written by the client through
// filesync, so it has to be a directory Pando itself can write to.
func TestTheCachePathIsPerAppAndOnPandosSide(t *testing.T) {
	require.Equal(t, filepath.Join("/var/lib/pando/buildcache", "app_01HQ8"), cachePath("app_01HQ8"))
	require.NotEqual(t, cachePath("app_01HQ8"), cachePath("app_01HQ9"), "R-117: one namespace per app")

	t.Setenv("PANDO_BUILD_CACHE_DIR", "/srv/cache")
	require.Equal(t, filepath.Join("/srv/cache", "app_01HQ8"), cachePath("app_01HQ8"))
}

func TestImageNamesAreValidReferences(t *testing.T) {
	// Lowercased and underscores replaced: neither is legal in an OCI
	// repository name, and an app ID carries both.
	require.Equal(t, "pando/app-01hq8:latest", imageName("APP_01HQ8"))
	require.Equal(t, "pando/my-app:latest", imageName("my_app"))
	require.True(t, strings.HasPrefix(imageName("x"), "pando/"))
}

// One runaway generator must not put a megabyte of text into an error envelope.
func TestSubprocessOutputIsBoundedKeepingTheEnd(t *testing.T) {
	require.Equal(t, "short output", trim([]byte("  short output\n")))

	long := strings.Repeat("a", 3000) + "the actual error"
	got := trim([]byte(long))
	require.Len(t, got, 2000)
	require.True(t, strings.HasSuffix(got, "the actual error"),
		"the end is what says why it failed")
}

// The same generator the build path uses, so what somebody reviews is what runs
// (R-102).
func TestPlanNeedsTheSourceOnDisk(t *testing.T) {
	_, _, _, err := New().Plan(context.Background(), viewWithoutRoot{})
	require.Equal(t, errs.BuildFailed, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Message, "on disk")
}

// viewWithoutRoot is a source view that cannot hand over a path, which is the
// case Plan has to refuse rather than assume.
type viewWithoutRoot struct{}

func (viewWithoutRoot) Open(string) (io.ReadCloser, error) { return nil, os.ErrNotExist }
func (viewWithoutRoot) Stat(string) (api.FileInfo, error)  { return api.FileInfo{}, os.ErrNotExist }
func (viewWithoutRoot) Glob(string) ([]string, error)      { return nil, nil }
