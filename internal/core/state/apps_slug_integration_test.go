//go:build integration

package state_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
)

// TestR204_ADeletedAppDoesNotHoldItsName asserts that deleting an app and
// adding it again works.
//
// The archive is kept deliberately: it is what a backup taken on delete refers
// to, what an audit event's target resolves against, and what the reconciler
// tears the bundle down from (R-204, R-227). None of that requires holding the
// name — and holding it produced "an app named crewmate already exists",
// pointing at a row the person had just deleted and can no longer see.
func TestR204_ADeletedAppDoesNotHoldItsName(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	apps := state.NewApps(db)

	src := spec.Source{Type: spec.SourceGit, URL: "https://example.test/crewmate"}

	first, err := apps.Create(ctx, "crewmate", "crewmate", alice.ID, alice.ID, src)
	require.NoError(t, err)

	// Two live apps still cannot share a slug: a slug is an address, and two
	// apps at one address is what the constraint exists to prevent.
	_, err = apps.Create(ctx, "crewmate", "crewmate", alice.ID, alice.ID, src)
	require.Error(t, err, "a live app still holds its name")

	require.NoError(t, apps.Archive(ctx, first.ID))

	second, err := apps.Create(ctx, "crewmate", "crewmate", alice.ID, alice.ID, src)
	require.NoError(t, err, "the name is free once the app is deleted")
	require.NotEqual(t, first.ID, second.ID, "a new app, not the old one back")

	// And the archive is still there, which is the reason it was kept.
	var archived int
	require.NoError(t, db.QueryRow(ctx,
		`SELECT count(*) FROM apps WHERE id = $1 AND deleted_at IS NOT NULL`, first.ID).Scan(&archived))
	require.Equal(t, 1, archived)
}
