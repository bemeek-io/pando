//go:build integration

package state_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/state"
	"github.com/trypando/pando/internal/core/state/statetest"
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
