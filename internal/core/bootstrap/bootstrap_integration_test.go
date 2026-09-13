//go:build integration

package bootstrap_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/bootstrap"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/hash"
	"github.com/bemeek-io/pando/internal/secret"
)

func newInstall(t *testing.T) (*state.DB, *state.Users, *state.Grants, *audit.Writer) {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("pando"),
		postgres.WithUsername("pando"),
		postgres.WithPassword("test-password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := state.Connect(ctx, state.ConnectOptions{OwnerURL: dsn, ConnectTimeout: 60 * time.Second})
	require.NoError(t, err)
	t.Cleanup(db.Close)

	return db, state.NewUsers(db), state.NewGrants(db), audit.New(db.Pool)
}

// TestR046_FirstRunGeneratesAPasswordAndShowsItOnce asserts R-046's default.
func TestR046_FirstRunGeneratesAPasswordAndShowsItOnce(t *testing.T) {
	ctx := context.Background()
	db, users, grants, auditor := newInstall(t)

	first, err := bootstrap.Run(ctx, users, grants, db, auditor, secret.Value{})
	require.NoError(t, err)
	require.True(t, first.Created)
	require.False(t, first.Supplied)
	require.NotEmpty(t, first.Password.Reveal(), "there is nothing to show otherwise")

	// It signs in, and it is the only account.
	rec, found, err := users.ByUsername(ctx, bootstrap.AdminUsername)
	require.NoError(t, err)
	require.True(t, found)

	ok, err := hash.Verify(first.Password, rec.PasswordHash)
	require.NoError(t, err)
	require.True(t, ok, "the password shown is the password stored")

	// Running again creates nothing. A second administrator on every restart
	// would be a new way in on every restart.
	again, err := bootstrap.Run(ctx, users, grants, db, auditor, secret.Value{})
	require.NoError(t, err)
	require.False(t, again.Created)
}

// TestR046_AnOperatorCanSupplyTheFirstPassword asserts the [P] override.
//
// The generated one is shown once, in a log line. An install whose server
// container is recreated before anybody reads it has an administrator nobody
// can sign in as — which is what `docker compose down && up` does.
func TestR046_AnOperatorCanSupplyTheFirstPassword(t *testing.T) {
	ctx := context.Background()
	db, users, grants, auditor := newInstall(t)

	chosen := secret.New("correct-horse-battery-staple")
	first, err := bootstrap.Run(ctx, users, grants, db, auditor, chosen)
	require.NoError(t, err)
	require.True(t, first.Created)
	require.True(t, first.Supplied)

	// Nothing to display. Handing it back invites the caller to print a
	// credential the operator already has, into a log (R-194).
	require.Empty(t, first.Password.Reveal())

	rec, found, err := users.ByUsername(ctx, bootstrap.AdminUsername)
	require.NoError(t, err)
	require.True(t, found)

	ok, err := hash.Verify(chosen, rec.PasswordHash)
	require.NoError(t, err)
	require.True(t, ok, "the supplied password is the one that signs in")
}

// R-046's forced change survives the override.
//
// An environment variable is not a safer place than a log line: it is in the
// Compose file, in `docker inspect`, and inherited by every child process.
// Supplying one buys a way in, not a credential.
func TestR046_ASuppliedPasswordStillMustBeChanged(t *testing.T) {
	ctx := context.Background()
	db, users, grants, auditor := newInstall(t)

	_, err := bootstrap.Run(ctx, users, grants, db, auditor, secret.New("correct-horse-battery-staple"))
	require.NoError(t, err)

	var mustChange bool
	require.NoError(t, db.QueryRow(ctx,
		`SELECT must_change_password FROM users WHERE external_id = $1`,
		bootstrap.AdminUsername).Scan(&mustChange))
	require.True(t, mustChange)
}

// A supplied password held to the same minimum as every other one.
//
// Refused at startup rather than accepted, because the alternative is an
// install whose only administrator has a four-character password and an
// operator who was never told.
func TestASuppliedPasswordTooShortIsRefused(t *testing.T) {
	ctx := context.Background()
	db, users, grants, auditor := newInstall(t)

	_, err := bootstrap.Run(ctx, users, grants, db, auditor, secret.New("short"))
	require.Error(t, err)

	// And it created nothing on the way out.
	_, found, err := users.ByUsername(ctx, bootstrap.AdminUsername)
	require.NoError(t, err)
	require.False(t, found, "a refused bootstrap leaves no half-made administrator")
}
