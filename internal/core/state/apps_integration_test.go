//go:build integration

package state_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/id"
)

func minimalSpec() *spec.AppSpec {
	return &spec.AppSpec{
		SchemaVersion: spec.SchemaVersion,
		Source:        spec.Source{Type: spec.SourceGit, URL: "https://github.com/acme/notes", Ref: "main"},
		Build:         spec.Build{Strategy: spec.BuildDockerfile, AdapterRef: "bld_buildkit"},
		Workloads:     []spec.Workload{{Name: "web", Primary: true, Exposed: true}},
		Routing:       spec.Routing{AdapterRef: "rte_loopback", Mode: spec.RoutingPort, Port: 8080},
		Runtime:       spec.RuntimeRef{AdapterRef: "rt_docker", IsolationFloor: spec.IsolationContainer},
		Deploy:        spec.Deploy{Strategy: spec.DeployRecreate},
	}
}

// TestR073_AppCreationWritesTwoGrants asserts R-073: one row per plane,
// independently revocable, written atomically with the app.
func TestR073_AppCreationWritesTwoGrants(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")

	app, err := state.NewApps(db).Create(ctx, "team notes", "team-notes", alice.ID, alice.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)
	require.Equal(t, state.StateDraft, app.State)

	rows, err := db.Query(ctx,
		`SELECT plane, principal_kind, principal_id, coalesce(role_id, '') FROM grants WHERE app_id = $1 ORDER BY plane`, app.ID)
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var plane, kind, principal, role string
		require.NoError(t, rows.Scan(&plane, &kind, &principal, &role))
		require.Equal(t, "user", kind)
		require.Equal(t, alice.ID, principal)
		got = append(got, plane+":"+role)
	}
	require.ElementsMatch(t, []string{"control:" + authz.RoleOwner, "data:"}, got,
		"exactly two grants, and the data-plane one carries no role (R-070)")
}

// TestR152_SpecRevisionsCannotBeEdited asserts R-152 at the database.
//
// Rollback is repointing at a revision that provably existed, which is only
// true if a revision can never be edited after the fact.
func TestR152_SpecRevisionsCannotBeEdited(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	apps := state.NewApps(db)

	app, err := apps.Create(ctx, "notes", "notes", alice.ID, alice.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	rev, err := apps.CreateRevision(ctx, app.ID, minimalSpec(), spec.OriginManual, alice.ID)
	require.NoError(t, err)
	require.Equal(t, 1, rev.Revision)

	_, err = db.Exec(ctx, `UPDATE spec_revisions SET body = '{}'::jsonb WHERE id = $1`, rev.ID)
	require.Error(t, err, "editing a spec revision must be refused")

	_, err = db.Exec(ctx, `UPDATE spec_revisions SET origin = 'detected' WHERE id = $1`, rev.ID)
	require.Error(t, err, "no column of a revision may be edited")

	stored, found, err := apps.RevisionByID(ctx, rev.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "https://github.com/acme/notes", stored.Body.Source.URL, "the revision is untouched")
}

// TestR152_RevisionsAreNumberedMonotonically asserts that editing produces a new
// revision rather than modifying one.
func TestR152_RevisionsAreNumberedMonotonically(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	apps := state.NewApps(db)

	app, err := apps.Create(ctx, "notes", "notes", alice.ID, alice.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	for want := 1; want <= 3; want++ {
		s := minimalSpec()
		s.Resources.CPUMillis = want * 100
		rev, err := apps.CreateRevision(ctx, app.ID, s, spec.OriginEdited, alice.ID)
		require.NoError(t, err)
		require.Equal(t, want, rev.Revision)
	}

	revs, err := apps.ListRevisions(ctx, app.ID)
	require.NoError(t, err)
	require.Len(t, revs, 3)
	require.Equal(t, 3, revs[0].Revision, "newest first")
}

// TestR152_PinningMarksARevisionEverPinned asserts the fact retention pruning
// depends on: a revision that was ever live is kept, even after a rollback has
// moved the pointer on.
func TestR152_PinningMarksARevisionEverPinned(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	apps := state.NewApps(db)

	app, err := apps.Create(ctx, "notes", "notes", alice.ID, alice.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	first, err := apps.CreateRevision(ctx, app.ID, minimalSpec(), spec.OriginManual, alice.ID)
	require.NoError(t, err)
	second, err := apps.CreateRevision(ctx, app.ID, minimalSpec(), spec.OriginEdited, alice.ID)
	require.NoError(t, err)

	require.NoError(t, apps.Pin(ctx, app.ID, first.ID, state.StateProposed, alice.ID))
	require.NoError(t, apps.Pin(ctx, app.ID, second.ID, state.StateProposed, alice.ID))

	// Roll back. The pointer moves; the history does not.
	require.NoError(t, apps.Pin(ctx, app.ID, first.ID, state.StateProposed, alice.ID))

	for _, rev := range []state.Revision{first, second} {
		stored, found, err := apps.RevisionByID(ctx, rev.ID)
		require.NoError(t, err)
		require.True(t, found)
		require.True(t, stored.EverPinned, "revision %d was live and must be kept", rev.Revision)
	}

	reloaded, _, err := apps.ByID(ctx, app.ID)
	require.NoError(t, err)
	require.Equal(t, first.ID, reloaded.PinnedSpecID)
}

// TestR204_AnAppCannotBeDeletedOutFromUnderItsVolumes asserts R-204's
// ON DELETE RESTRICT: the delete flow must resolve volumes explicitly rather
// than cascading silently.
func TestR204_AnAppCannotBeDeletedOutFromUnderItsVolumes(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	apps := state.NewApps(db)
	volumes := state.NewVolumes(db)

	app, err := apps.Create(ctx, "notes", "notes", alice.ID, alice.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	_, err = volumes.Create(ctx, app.ID, "data", "rt_docker")
	require.NoError(t, err)

	// A hard delete is refused while volumes remain. This is the constraint
	// that forces the keep-or-discard decision to be made explicitly.
	_, err = db.Exec(ctx, `DELETE FROM apps WHERE id = $1`, app.ID)
	require.Error(t, err, "volumes must survive an app's deletion")

	count, err := apps.VolumeCount(ctx, app.ID)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Resolving them first is what lets the delete proceed.
	require.NoError(t, volumes.DeleteForApp(ctx, app.ID))
	_, err = db.Exec(ctx, `DELETE FROM apps WHERE id = $1`, app.ID)
	require.NoError(t, err)
}

// TestR210_AnAppWithNoVolumesDoesNotStopTheRollingBackupSweep asserts that one
// app's spec cannot take the installation's backups down with it.
//
// `AppSpec.Volumes` is tagged `json:"volumes"` with no omitempty, so an app
// that declares no storage — which is most of them — stores `"volumes": null`.
// The sweep read that with `->`, which answers with the JSON null rather than
// SQL NULL, so the coalesce meant to handle "no volumes" never fired and
// `jsonb_array_length` was handed a scalar. The error came back to the
// reconciler as "could not list apps for rolling backups", on every pass, and
// no app on the installation was backed up.
func TestR210_AnAppWithNoVolumesDoesNotStopTheRollingBackupSweep(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	apps := state.NewApps(db)

	app, err := apps.Create(ctx, "notes", "notes", alice.ID, alice.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	// minimalSpec declares no volumes, which is the whole point: this is the
	// ordinary shape of an app, not a corrupted row.
	rev, err := apps.CreateRevision(ctx, app.ID, minimalSpec(), spec.OriginDetected, alice.ID)
	require.NoError(t, err)
	require.NoError(t, apps.Pin(ctx, app.ID, rev.ID, state.StateRunning, alice.ID))

	var kind *string
	require.NoError(t, db.QueryRow(ctx,
		`SELECT jsonb_typeof(body->'volumes') FROM spec_revisions WHERE id = $1`, rev.ID).Scan(&kind))
	require.Equal(t, "null", *kind, "the stored shape this is about")

	found, err := apps.WithStorage(ctx)
	require.NoError(t, err, "one app's spec must not fail the sweep for every app")
	require.Empty(t, found, "an app with no volumes has nothing to back up")
}

// TestR264_LauncherListIsDataPlaneScoped asserts that the two list endpoints
// answer different questions (R-070, R-071, R-264).
func TestR264_LauncherListIsDataPlaneScoped(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	bob := seedUser(t, db, "bob")
	apps := state.NewApps(db)

	app, err := apps.Create(ctx, "notes", "notes", alice.ID, alice.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	// Bob is an operator: he can manage the app but was given no data grant.
	_, err = db.Exec(ctx, `
		INSERT INTO grants (id, app_id, plane, principal_kind, principal_id, role_id, created_by)
		VALUES ($1, $2, 'control', 'user', $3, $4, $5)`,
		id.New(id.Grant), app.ID, bob.ID, authz.RoleOperator, alice.ID)
	require.NoError(t, err)

	bobPrincipal := authz.Principal{Kind: authz.KindUser, ID: bob.ID, UserID: bob.ID, Status: "active"}

	manageable, err := apps.ListForPrincipal(ctx, bobPrincipal)
	require.NoError(t, err)
	require.Len(t, manageable, 1, "bob can manage the app")

	usable, err := apps.ListForUse(ctx, bobPrincipal)
	require.NoError(t, err)
	require.Empty(t, usable, "but it does not appear in his launcher — two planes, two lists")

	// The owner sees it in both.
	alicePrincipal := authz.Principal{Kind: authz.KindUser, ID: alice.ID, UserID: alice.ID, Status: "active"}
	usable, err = apps.ListForUse(ctx, alicePrincipal)
	require.NoError(t, err)
	require.Len(t, usable, 1)
}

// TestSpecRoundTripsThroughJSONB asserts nothing is lost in storage — the spec
// is the sole record of how an app runs, so a lossy round trip is a data loss
// bug rather than a formatting one.
func TestSpecRoundTripsThroughJSONB(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	apps := state.NewApps(db)

	app, err := apps.Create(ctx, "notes", "notes", alice.ID, alice.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	original := minimalSpec()
	value := "production"
	original.Workloads[0].Env = []spec.EnvEntry{
		{Key: "NODE_ENV", Value: &value},
		{Key: "REDIS_URL", SlotRef: strPtr("REDIS_URL")},
	}
	original.Slots = []spec.Slot{{
		Key: "REDIS_URL", Type: spec.SlotRedis, Required: true,
		Evidence:   []string{"referenced in docker-compose.yml"},
		Resolution: &spec.Resolution{Mode: spec.ResolutionProvisioned},
	}}
	original.Volumes = []spec.Volume{{ID: "vol_01HQ8", Name: "data", Declared: spec.VolumeFromCompose}}
	original.Workloads[0].Mounts = []spec.Mount{{VolumeID: "vol_01HQ8", Path: "/app/data"}}
	original.Warnings = []spec.Warning{{Code: spec.WarnNoPersistentVolume, Message: "Your app wrote to /app/data during setup."}}

	rev, err := apps.CreateRevision(ctx, app.ID, original, spec.OriginDetected, alice.ID)
	require.NoError(t, err)

	stored, found, err := apps.RevisionByID(ctx, rev.ID)
	require.NoError(t, err)
	require.True(t, found)

	require.Equal(t, original.Workloads[0].Env, stored.Body.Workloads[0].Env)
	require.Equal(t, original.Slots, stored.Body.Slots)
	require.Equal(t, original.Volumes, stored.Body.Volumes)
	require.Equal(t, original.Warnings, stored.Body.Warnings)
	require.Equal(t, spec.OriginDetected, stored.Body.Origin)
	require.Equal(t, app.ID, stored.Body.AppID)
}

// TestARevisionIDFromAnotherAppDoesNotResolve asserts that revision lookup is
// scoped, so a guessed ID cannot read another app's spec.
func TestARevisionIDFromAnotherAppDoesNotResolve(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	apps := state.NewApps(db)

	one, err := apps.Create(ctx, "one", "one", alice.ID, alice.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)
	two, err := apps.Create(ctx, "two", "two", alice.ID, alice.ID, spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	rev, err := apps.CreateRevision(ctx, one.ID, minimalSpec(), spec.OriginManual, alice.ID)
	require.NoError(t, err)

	_, found, err := apps.RevisionByNumber(ctx, two.ID, rev.Revision)
	require.NoError(t, err)
	require.False(t, found, "app two has no revision 1 of its own")
}

func strPtr(s string) *string { return &s }
