package backup

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
	backuplocal "github.com/bemeek-io/pando/internal/adapter/backup/local"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/secret"
)

// In-package: destination, pgEnv and the small readers are unexported, and they
// are where a backup goes wrong in ways that are quiet — a password in argv, a
// bundle written nowhere, a pg_dump error replaced by something generic.

func withDestination(t *testing.T) (*Service, *api.Registry, string) {
	t.Helper()
	dir := t.TempDir()

	adapter := backuplocal.New()
	raw, err := json.Marshal(map[string]string{"path": dir})
	require.NoError(t, err)
	require.NoError(t, adapter.Configure(context.Background(), raw))

	registry := api.NewRegistry()
	require.NoError(t, registry.Register("bk_local", adapter))
	require.NoError(t, registry.SetDefault(api.CategoryBackup, "bk_local"))

	return &Service{Registry: registry, WorkDir: t.TempDir()}, registry, dir
}

func TestTheDefaultDestinationIsUsedWhenNoneIsNamed(t *testing.T) {
	s, _, _ := withDestination(t)

	dest, ref, err := s.destination("")
	require.NoError(t, err)
	require.NotNil(t, dest)
	require.Equal(t, "bk_local", ref)

	named, ref, err := s.destination("bk_local")
	require.NoError(t, err)
	require.NotNil(t, named)
	require.Equal(t, "bk_local", ref)
}

// An install with nowhere to put a backup is told so before anything is dumped,
// rather than after.
func TestAnInstallWithNoDestinationSaysSoBeforeDoingAnyWork(t *testing.T) {
	s := &Service{Registry: api.NewRegistry()}

	_, _, err := s.destination("")
	require.Equal(t, errs.Internal, errs.CodeOf(err))
	require.NotEmpty(t, errs.As(err).Remedy)
}

func TestADestinationThatIsNotConfiguredIsNamedInTheRefusal(t *testing.T) {
	s, _, _ := withDestination(t)

	_, _, err := s.destination("bk_s3")
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Message, "bk_s3")
}

// The password must not appear in argv. Splitting the URL here also means a
// malformed one is reported as a configuration problem now, rather than as a
// pg_dump error later that names nothing useful.
func TestR194_TheDatabasePasswordGoesInTheEnvironmentAndNeverInArgv(t *testing.T) {
	env, dbname, err := pgEnv(secret.New("postgres://pando:hunter2@db.internal:5433/pando?sslmode=require"))
	require.NoError(t, err)
	require.Equal(t, "pando", dbname)

	require.Contains(t, env, "PGHOST=db.internal")
	require.Contains(t, env, "PGPORT=5433")
	require.Contains(t, env, "PGDATABASE=pando")
	require.Contains(t, env, "PGUSER=pando")
	require.Contains(t, env, "PGPASSWORD=hunter2")
	require.Contains(t, env, "PGSSLMODE=require")
}

func TestThePostgresDefaultPortIsUsedWhenTheURLOmitsOne(t *testing.T) {
	env, _, err := pgEnv(secret.New("postgres://pando@db/pando"))
	require.NoError(t, err)
	require.Contains(t, env, "PGPORT=5432")

	// Nothing is invented for what the URL does not carry.
	for _, unset := range env {
		require.NotEqual(t, "PGPASSWORD=", unset)
		require.NotEqual(t, "PGSSLMODE=", unset)
	}
}

func TestADatabaseAddressThatCannotBeReadIsAConfigurationProblem(t *testing.T) {
	for _, dsn := range []string{"", "://nonsense", "not a url at all", "postgres://host-with-no-db"} {
		_, _, err := pgEnv(secret.New(dsn))
		require.Error(t, err, dsn)
		require.Equal(t, errs.Internal, errs.CodeOf(err), dsn)
	}

	// The address is not echoed back, because it carries the password.
	_, _, err := pgEnv(secret.New("postgres://pando:hunter2@host-with-no-db"))
	require.Error(t, err)
	require.NotContains(t, errs.As(err).Message, "hunter2")
}

func TestTheWorkingDirectoryIsCreatedOrFallsBackToTheSystemOne(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "work")
	s := &Service{WorkDir: dir}
	require.NoError(t, s.ensureWorkDir())
	require.DirExists(t, dir)

	// Empty means os.CreateTemp's own fallback, which is not an error.
	require.NoError(t, (&Service{}).ensureWorkDir())

	// A path that cannot be a directory says which path.
	file := filepath.Join(t.TempDir(), "occupied")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	err := (&Service{WorkDir: filepath.Join(file, "work")}).ensureWorkDir()
	require.Equal(t, errs.Internal, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Message, file)
}

// pg_dump's own message names the table or permission at fault. Replacing it
// with something generic would delete the only useful detail an operator has —
// but one runaway dump must not put a megabyte into an error envelope either.
func TestSubprocessOutputIsBoundedForAnErrorEnvelope(t *testing.T) {
	require.Equal(t, "short", trimForMessage([]byte("short")))
	require.Empty(t, trimForMessage(nil))

	long := strings.Repeat("x", 1000)
	got := trimForMessage([]byte(long))
	require.Len(t, got, 400+len("…"))
	require.True(t, strings.HasSuffix(got, "…"), "and says it was cut")
}

func TestTheCountingWriterReportsWhatItPassedThrough(t *testing.T) {
	var sink strings.Builder
	c := &countingWriter{w: &sink}

	n, err := c.Write([]byte("twelve bytes"))
	require.NoError(t, err)
	require.Equal(t, 12, n)

	_, err = c.Write([]byte(" more"))
	require.NoError(t, err)

	require.EqualValues(t, len("twelve bytes more"), c.n)
	require.Equal(t, "twelve bytes more", sink.String())
}

func TestTheSliceReaderReadsToEOFInWhateverChunksItIsAsked(t *testing.T) {
	r := bytesReader([]byte("manifest bytes"))

	got, err := io.ReadAll(r)
	require.NoError(t, err)
	require.Equal(t, "manifest bytes", string(got))

	n, err := r.Read(make([]byte, 4))
	require.Zero(t, n)
	require.ErrorIs(t, err, io.EOF, "a drained reader keeps saying EOF")

	// And in small bites.
	small := bytesReader([]byte("abcdef"))
	buf := make([]byte, 2)
	for _, want := range []string{"ab", "cd", "ef"} {
		n, err := small.Read(buf)
		require.NoError(t, err)
		require.Equal(t, want, string(buf[:n]))
	}
	_, err = small.Read(buf)
	require.ErrorIs(t, err, io.EOF)

	require.NotPanics(t, func() { _, _ = bytesReader(nil).Read(buf) })
}

func TestAddFileSaysWhichFileItCouldNotRead(t *testing.T) {
	var sink strings.Builder
	w := NewWriter(&sink, "install", "test", 1)

	err := addFile(w, "database.dump", filepath.Join(t.TempDir(), "never-written.dump"))
	require.Error(t, err)
	require.Contains(t, errs.As(err).Message, "never-written.dump",
		"an operator needs to know which part of the bundle was missing")
}

// R-216: verifying touches nothing. That is why it is a separate call from
// Restore rather than a flag on it — a flag is a thing someone passes wrongly,
// and the wrong value here overwrites an install.
func TestR216_VerifyingABundleThatIsNotThereTouchesNothing(t *testing.T) {
	s, _, dir := withDestination(t)

	_, err := s.Verify(context.Background(), "bk_local", "bk_missing",
		secret.New("correct horse battery staple"))
	require.Error(t, err)
	require.Equal(t, errs.NotFound, errs.CodeOf(err))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "verifying wrote nothing")
}

func TestVerifyingAgainstADestinationThatIsNotConfigured(t *testing.T) {
	s, _, _ := withDestination(t)

	_, err := s.Verify(context.Background(), "bk_elsewhere", "bk_01HQ8", secret.New("x"))
	require.Equal(t, errs.ValidInvalid, errs.CodeOf(err))
}

// A bundle that is not a bundle is refused rather than half-applied: R-215 says
// a half-written backup that looks whole is the failure to catch, and catching
// it at restore time is catching it too late.
func TestR215_SomethingThatIsNotABundleFailsVerificationRatherThanRestore(t *testing.T) {
	s, _, dir := withDestination(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bk_01HQ8"), []byte("not a bundle"), 0o600))

	_, err := s.Verify(context.Background(), "bk_local", "bk_01HQ8",
		secret.New("correct horse battery staple"))
	require.Error(t, err)
	require.NotEqual(t, errs.NotFound, errs.CodeOf(err), "it was found; it is just not a bundle")
}

// R-204: a backup outlives the thing it records, so discarding one is its own
// deliberate act.
func TestR204_DiscardingABackupThatIsAlreadyGoneIsNotAnError(t *testing.T) {
	s, _, dir := withDestination(t)

	require.NoError(t, s.Discard(context.Background(), "bk_local", "bk_never_taken"))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "bk_01HQ8"), []byte("bundle"), 0o600))
	require.NoError(t, s.Discard(context.Background(), "bk_local", "bk_01HQ8"))
	require.NoFileExists(t, filepath.Join(dir, "bk_01HQ8"))
}

func TestDiscardingFromADestinationThatIsNotConfigured(t *testing.T) {
	s, _, _ := withDestination(t)
	require.Error(t, s.Discard(context.Background(), "bk_elsewhere", "bk_01HQ8"))
}
