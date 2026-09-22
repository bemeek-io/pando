//go:build integration

package state_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/id"
)

// O-15: ports are allocated, not derived.
//
// The first implementation computed "the lowest port no pinned spec is using".
// It raced, and it raced in the ordinary case rather than an exotic one: adding
// three apps at once runs three background detections, and two of them computed
// the same answer before either had written anything down. Two apps ended up
// holding port 9001 and nothing anywhere noticed.
func TestO15_ConcurrentAllocationNeverHandsOutTheSamePort(t *testing.T) {
	db := connected(t)
	owner := seedUser(t, db, "port-owner")
	apps := state.NewApps(db)
	ports := state.NewPorts(db)

	const n = 12
	ids := make([]string, n)
	for i := range ids {
		app, err := apps.Create(context.Background(),
			"port-race", id.New(id.App), owner.ID, owner.ID,
			spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
		require.NoError(t, err)
		ids[i] = app.ID
	}

	var wg sync.WaitGroup
	got := make([]int, n)
	errsOut := make([]error, n)

	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i], errsOut[i] = ports.Allocate(context.Background(), "rte_loopback", ids[i], 9000, 9100)
		}(i)
	}
	wg.Wait()

	seen := map[int]string{}
	for i, port := range got {
		require.NoError(t, errsOut[i])
		require.NotZero(t, port)
		if other, clash := seen[port]; clash {
			t.Fatalf("apps %s and %s were both given port %d", other, ids[i], port)
		}
		seen[port] = ids[i]
	}
	require.Len(t, seen, n, "every app got a port of its own")
}

// Re-detection must not consume a second port and leave bookmarks pointing at
// nothing (R-022 makes re-detection explicit, not free).
func TestO15_AllocatingTwiceForOneAppReturnsTheSamePort(t *testing.T) {
	db := connected(t)
	owner := seedUser(t, db, "port-owner")
	apps := state.NewApps(db)
	ports := state.NewPorts(db)

	app, err := apps.Create(context.Background(), "port-stable", id.New(id.App), owner.ID, owner.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	first, err := ports.Allocate(context.Background(), "rte_loopback", app.ID, 9000, 9100)
	require.NoError(t, err)

	again, err := ports.Allocate(context.Background(), "rte_loopback", app.ID, 9000, 9100)
	require.NoError(t, err)
	require.Equal(t, first, again)
}

// A full range is a capacity error naming the setting to change, not a crash.
func TestO15_AFullRangeSaysWhatToDoAboutIt(t *testing.T) {
	db := connected(t)
	owner := seedUser(t, db, "port-owner")
	apps := state.NewApps(db)
	ports := state.NewPorts(db)

	first, err := apps.Create(context.Background(), "port-full-a", id.New(id.App), owner.ID, owner.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)
	second, err := apps.Create(context.Background(), "port-full-b", id.New(id.App), owner.ID, owner.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	// A range of exactly one.
	_, err = ports.Allocate(context.Background(), "rte_tiny", first.ID, 9500, 9500)
	require.NoError(t, err)

	_, err = ports.Allocate(context.Background(), "rte_tiny", second.ID, 9500, 9500)
	require.Error(t, err)
	require.Contains(t, err.Error(), "9500")
}

// A port belongs to an app, and goes when the app does.
func TestO15_DeletingAnAppReleasesItsPort(t *testing.T) {
	db := connected(t)
	owner := seedUser(t, db, "port-owner")
	apps := state.NewApps(db)
	ports := state.NewPorts(db)

	app, err := apps.Create(context.Background(), "port-release", id.New(id.App), owner.ID, owner.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	port, err := ports.Allocate(context.Background(), "rte_release", app.ID, 9600, 9610)
	require.NoError(t, err)

	_, err = db.Exec(context.Background(), `DELETE FROM apps WHERE id = $1`, app.ID)
	require.NoError(t, err)

	next, err := apps.Create(context.Background(), "port-release-2", id.New(id.App), owner.ID, owner.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	reused, err := ports.Allocate(context.Background(), "rte_release", next.ID, 9600, 9610)
	require.NoError(t, err)
	require.Equal(t, port, reused, "a deleted app's port comes back rather than the range filling up")
}

// TestO15_ArchivingAnAppReleasesItsPort asserts the same through the path the
// product actually takes.
//
// The test above deletes the row, which nothing outside a test does: deleting
// an app archives it, leaving the row with deleted_at set, so the table's ON
// DELETE CASCADE never fired. An install reached twenty apps, deleted most of
// them, and was told every port was assigned to an app — by which the ports
// were held by apps that no longer existed.
func TestO15_ArchivingAnAppReleasesItsPort(t *testing.T) {
	db := connected(t)
	owner := seedUser(t, db, "port-owner")
	apps := state.NewApps(db)
	ports := state.NewPorts(db)
	ctx := context.Background()

	app, err := apps.Create(ctx, "port-archive", id.New(id.App), owner.ID, owner.ID,
		spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	port, err := ports.Allocate(ctx, "rte_archive", app.ID, 9700, 9701)
	require.NoError(t, err)

	require.NoError(t, apps.Archive(ctx, app.ID))

	next, err := apps.Create(ctx, "port-archive-2", id.New(id.App), owner.ID, owner.ID,
		spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	reused, err := ports.Allocate(ctx, "rte_archive", next.ID, 9700, 9701)
	require.NoError(t, err)
	require.Equal(t, port, reused, "a deleted app's port comes back")
}

// TestO15_AllocationReclaimsPortsLeftByDeletedApps asserts the recovery of an
// install that already filled its range this way, without a migration: the
// rows are stale the moment the app is gone, and the next allocation clears
// them. InUse has always ignored them, so nothing was listening on those
// ports either.
func TestO15_AllocationReclaimsPortsLeftByDeletedApps(t *testing.T) {
	db := connected(t)
	owner := seedUser(t, db, "port-owner")
	apps := state.NewApps(db)
	ports := state.NewPorts(db)
	ctx := context.Background()

	gone, err := apps.Create(ctx, "port-stale", id.New(id.App), owner.ID, owner.ID,
		spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)
	port, err := ports.Allocate(ctx, "rte_stale", gone.ID, 9800, 9800)
	require.NoError(t, err)

	// Archived the way an older build left it: the app is deleted, the
	// allocation stayed behind.
	_, err = db.Exec(ctx, `UPDATE apps SET state = 'archived', deleted_at = now() WHERE id = $1`, gone.ID)
	require.NoError(t, err)

	next, err := apps.Create(ctx, "port-stale-2", id.New(id.App), owner.ID, owner.ID,
		spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	reused, err := ports.Allocate(ctx, "rte_stale", next.ID, 9800, 9800)
	require.NoError(t, err, "the one port in the range is held by an app that no longer exists")
	require.Equal(t, port, reused)
}
