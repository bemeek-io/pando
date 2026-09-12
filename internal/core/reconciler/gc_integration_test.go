//go:build integration

package reconciler_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/core/reconciler"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/id"
)

// R-152: history is trimmed, and never a revision anyone could roll back to.
//
// The exemption is the whole test. Rollback is repointing at a revision that
// provably existed, and spec_pins is that proof — pruning a revision someone
// had pinned would make a rollback target vanish from under a person looking
// at it. Getting this wrong costs history that cannot be recovered, which is
// why the saving is never worth a doubt.
func TestR152_PruningNeverRemovesARevisionThatWasEverPinned(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	apps := state.NewApps(db)
	owner := seedOwner(t, db)

	app, err := apps.Create(ctx, "gc-"+id.New(id.App), id.New(id.App), owner, owner,
		spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	// Twenty-five revisions against a retention of ten. Two of them get pinned
	// early, so they are well outside the window the trim keeps.
	var pinnedEarly []string
	for i := range 25 {
		s := &spec.AppSpec{
			SchemaVersion: spec.SchemaVersion,
			AppID:         app.ID,
			Source:        spec.Source{Type: spec.SourceGit, URL: "https://example.test/app", Ref: "main"},
			Build:         spec.Build{Strategy: spec.BuildPrebuilt},
			Workloads:     []spec.Workload{{Name: "web", Image: fmt.Sprintf("example/app:%d", i), Primary: true, Exposed: true}},
			Routing:       spec.Routing{AdapterRef: "rte_fake", Mode: spec.RoutingPort, Port: 9000},
			Runtime:       spec.RuntimeRef{AdapterRef: "rt_fake", IsolationFloor: spec.IsolationContainer},
			Deploy:        spec.Deploy{Strategy: spec.DeployRecreate},
			Retention:     spec.Retention{SpecRevisions: 10},
		}
		rev, err := apps.CreateRevision(ctx, app.ID, s, spec.OriginEdited, owner)
		require.NoError(t, err)

		if i == 1 || i == 3 {
			require.NoError(t, apps.Pin(ctx, app.ID, rev.ID, state.StateRunning, owner))
			pinnedEarly = append(pinnedEarly, rev.ID)
		}
	}

	// And the current pin, which is also the retention setting in force.
	all, err := apps.ListRevisions(ctx, app.ID)
	require.NoError(t, err)
	require.NoError(t, apps.Pin(ctx, app.ID, all[0].ID, state.StateRunning, owner))

	gc := &reconciler.GC{Apps: apps, Logger: zap.NewNop()}
	gc.Collect(ctx)

	for _, specID := range pinnedEarly {
		_, found, err := apps.RevisionByID(ctx, specID)
		require.NoError(t, err)
		require.True(t, found,
			"revision %s was pinned once and must stay: it is a rollback target", specID)
	}

	remaining, err := apps.ListRevisions(ctx, app.ID)
	require.NoError(t, err)
	require.Less(t, len(remaining), 25, "something was pruned")
	require.GreaterOrEqual(t, len(remaining), 10, "the retention window is kept")
}

// Pruning is idempotent and safe to run on an install with nothing to prune.
func TestPruningAnAppWithLittleHistoryDoesNothing(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	apps := state.NewApps(db)
	owner := seedOwner(t, db)

	app, err := apps.Create(ctx, "gc-small-"+id.New(id.App), id.New(id.App), owner, owner,
		spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	s := &spec.AppSpec{
		SchemaVersion: spec.SchemaVersion, AppID: app.ID,
		Source:    spec.Source{Type: spec.SourceGit, URL: "https://example.test/app", Ref: "main"},
		Build:     spec.Build{Strategy: spec.BuildPrebuilt},
		Workloads: []spec.Workload{{Name: "web", Image: "example/app:1", Primary: true, Exposed: true}},
		Routing:   spec.Routing{AdapterRef: "rte_fake", Mode: spec.RoutingPort, Port: 9000},
		Runtime:   spec.RuntimeRef{AdapterRef: "rt_fake", IsolationFloor: spec.IsolationContainer},
		Deploy:    spec.Deploy{Strategy: spec.DeployRecreate},
	}
	rev, err := apps.CreateRevision(ctx, app.ID, s, spec.OriginDetected, owner)
	require.NoError(t, err)
	require.NoError(t, apps.Pin(ctx, app.ID, rev.ID, state.StateRunning, owner))

	gc := &reconciler.GC{Apps: apps, Logger: zap.NewNop()}
	gc.Collect(ctx)
	gc.Collect(ctx)

	remaining, err := apps.ListRevisions(ctx, app.ID)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
}
