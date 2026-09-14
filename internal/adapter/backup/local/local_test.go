package local_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/adapter/backup/local"
	"github.com/bemeek-io/pando/internal/errs"
)

func configured(t *testing.T) (*local.Adapter, string) {
	t.Helper()
	dir := t.TempDir()
	a := local.New()
	require.NoError(t, a.Configure(context.Background(), pathConfig(t, dir)))
	return a, dir
}

// pathConfig is the adapter's configuration document for one directory.
func pathConfig(t *testing.T, dir string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"path": dir})
	require.NoError(t, err)
	return raw
}

func write(t *testing.T, a *local.Adapter, name, body string) {
	t.Helper()
	w, err := a.Writer(context.Background(), name)
	require.NoError(t, err)
	_, err = io.WriteString(w, body)
	require.NoError(t, err)
	require.NoError(t, w.Close())
}

func TestIdentity(t *testing.T) {
	a := local.New()
	require.Equal(t, local.Kind, a.Kind())
	require.Equal(t, api.CategoryBackup, a.Category())
}

func TestConfigureDefaultsToTheStandardPath(t *testing.T) {
	// An adapter registered with no configuration still knows where bundles go,
	// so an install that never touched backup settings is not silently broken.
	a := local.New()
	require.NoError(t, a.Configure(context.Background(), nil))

	// The test cannot write to /var/lib/pando, and does not need to: what it
	// asserts is that the adapter resolved a path at all rather than staying
	// unconfigured. The error names the directory it tried.
	err := a.HealthCheck(context.Background())
	require.ErrorContains(t, err, "/var/lib/pando/backups")
}

func TestConfigureRejectsMalformedAndEmptyPaths(t *testing.T) {
	a := local.New()
	err := a.Configure(context.Background(), json.RawMessage(`{`))
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))

	err = a.Configure(context.Background(), json.RawMessage(`{"path":""}`))
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))
	require.NotEmpty(t, errs.As(err).Remedy, "R-105: the message says what a valid answer looks like")
}

// HealthCheck must catch a directory that exists and cannot be written to.
// A stat would report healthy right up until the backup was needed.
func TestHealthCheckProbesWritability(t *testing.T) {
	a, dir := configured(t)
	require.NoError(t, a.HealthCheck(context.Background()))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "the probe file is removed again")

	require.Error(t, local.New().HealthCheck(context.Background()), "unconfigured is not healthy")

	// A path that cannot be a directory, because a file is already there.
	file := filepath.Join(t.TempDir(), "occupied")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	blocked := local.New()
	require.NoError(t, blocked.Configure(context.Background(), pathConfig(t, file)))
	require.Error(t, blocked.HealthCheck(context.Background()))
}

// Retention is Pando's for a filesystem: it expires nothing on its own (R-211).
func TestR211_CapabilitiesSayPandoOwnsRetention(t *testing.T) {
	caps := local.New().Capabilities()
	require.False(t, caps.OwnsRetention)
	require.False(t, caps.Immutable)
	require.Zero(t, caps.MaxBundleBytes, "the adapter does not guess the size of the disk it sits on")
}

// R-215: a half-written bundle must never look whole.
func TestR215_AWriteIsOnlyVisibleOnceItIsComplete(t *testing.T) {
	a, dir := configured(t)

	w, err := a.Writer(context.Background(), "bk_01HQ8")
	require.NoError(t, err)
	_, err = io.WriteString(w, "half a bundle")
	require.NoError(t, err)

	listed, err := a.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, listed, "an unfinished write is not a bundle")
	require.FileExists(t, filepath.Join(dir, "bk_01HQ8.partial"))

	require.NoError(t, w.Close())
	listed, err = a.List(context.Background())
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "bk_01HQ8", listed[0].Name)
	require.EqualValues(t, len("half a bundle"), listed[0].SizeBytes)
	require.False(t, listed[0].StoredAt.IsZero())
}

func TestCloseIsIdempotent(t *testing.T) {
	a, _ := configured(t)
	w, err := a.Writer(context.Background(), "bk_01HQ8")
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, w.Close(), "a second Close does not undo the rename")

	_, err = a.Reader(context.Background(), "bk_01HQ8")
	require.NoError(t, err)
}

func TestRoundTrip(t *testing.T) {
	a, _ := configured(t)
	write(t, a, "bk_01HQ8", "bundle bytes")

	r, err := a.Reader(context.Background(), "bk_01HQ8")
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()

	got, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "bundle bytes", string(got))
}

func TestReaderSaysWhichDestinationIsMissingTheBundle(t *testing.T) {
	a, _ := configured(t)
	_, err := a.Reader(context.Background(), "bk_nothere")
	require.Equal(t, errs.NotFound, errs.CodeOf(err))
	require.NotEmpty(t, errs.As(err).Remedy)
}

func TestListIgnoresDotFilesAndDirectories(t *testing.T) {
	a, dir := configured(t)
	write(t, a, "bk_01HQ8", "x")
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0o700))

	listed, err := a.List(context.Background())
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, "bk_01HQ8", listed[0].Name)
}

func TestListOfADirectoryThatDoesNotExistYetIsEmptyNotAnError(t *testing.T) {
	a := local.New()
	require.NoError(t, a.Configure(context.Background(),
		pathConfig(t, filepath.Join(t.TempDir(), "not-created-yet"))))

	listed, err := a.List(context.Background())
	require.NoError(t, err, "no backups taken yet is not a failure")
	require.Empty(t, listed)
}

func TestDeleteIsIdempotent(t *testing.T) {
	a, _ := configured(t)
	write(t, a, "bk_01HQ8", "x")

	require.NoError(t, a.Delete(context.Background(), "bk_01HQ8"))
	require.NoError(t, a.Delete(context.Background(), "bk_01HQ8"), "deleting what is gone is not an error")

	listed, err := a.List(context.Background())
	require.NoError(t, err)
	require.Empty(t, listed)
}

// Bundle names are Pando's own IDs today. The guard is here so that stops being
// load-bearing: a name that would escape the directory is refused by the
// adapter, not by the caller's good manners.
func TestANameCannotEscapeTheDestination(t *testing.T) {
	a, _ := configured(t)

	for _, name := range []string{"", ".", "..", "../outside", "sub/bundle", "/etc/passwd"} {
		t.Run(name, func(t *testing.T) {
			_, err := a.Writer(context.Background(), name)
			require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))

			_, err = a.Reader(context.Background(), name)
			require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))

			require.Equal(t, errs.ValidInvalid, errs.CodeOf(a.Delete(context.Background(), name)))
		})
	}
}

func TestAnUnconfiguredAdapterRefusesEveryOperation(t *testing.T) {
	a := local.New()
	_, err := a.Writer(context.Background(), "bk_01HQ8")
	require.Equal(t, errs.Internal, errs.CodeOf(err))

	_, err = a.Reader(context.Background(), "bk_01HQ8")
	require.Equal(t, errs.Internal, errs.CodeOf(err))

	require.Equal(t, errs.Internal, errs.CodeOf(a.Delete(context.Background(), "bk_01HQ8")))
}
