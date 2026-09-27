//go:build integration

package state_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/state"
	"github.com/trypando/pando/internal/core/state/statetest"
	"github.com/trypando/pando/internal/errs"
	"github.com/trypando/pando/internal/secret"
)

// Every start migrates, so migrating a database that is already current is the
// ordinary case rather than an edge: it must change nothing and say so quietly.
// `pando migrate` is the same call.
func TestMigratingACurrentDatabaseChangesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ownerURL, password := statetest.Database(t)

	require.NoError(t, state.Migrate(ctx, ownerURL))
	require.NoError(t, state.Migrate(ctx, ownerURL), "a second run finds nothing to do")

	// The version is read back from the database rather than remembered by
	// the process, which is how a copy that was never migrated here knows it.
	db, err := state.ConnectCopy(ctx, ownerURL, password)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NotZero(t, db.SchemaVersion())
}

// A copy is served only as the application role, with the role's real
// password. Anything else is refused at connect rather than at the first query.
func TestACopyIsNotServedWithTheWrongPassword(t *testing.T) {
	t.Parallel()
	ownerURL, _ := statetest.Database(t)

	_, err := state.ConnectCopy(context.Background(), ownerURL, secret.New("not-the-password"))
	require.Error(t, err)
	require.Equal(t, errs.Internal, errs.CodeOf(err))
}

// A database that was never migrated has no version to read, and is refused
// rather than served as version zero. In a Postgres of its own, because this is
// state.Connect, which a shared cluster must never see (statetest).
func TestAnUnmigratedDatabaseIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, err := state.Connect(ctx, state.ConnectOptions{OwnerURL: startPostgres(t), SkipMigrate: true})
	require.Error(t, err)
	require.Contains(t, errs.As(err).Message, "schema version")
}
