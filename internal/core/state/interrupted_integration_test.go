//go:build integration

package state_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/id"
)

// Work a stopped process had under way is recorded as interrupted at startup.
// A detection or deploy that was running when Pando restarted stayed running
// forever, and an app with a deploy "in flight" refused the next one (issue
// #55).
func TestWorkInterruptedByARestartIsRecordedAsFailed(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	owner := seedUser(t, db, "interrupted-owner")
	apps := state.NewApps(db)
	detections := state.NewDetections(db)
	deployments := state.NewDeployments(db)

	app, err := apps.Create(ctx, "interrupted", id.New(id.App), owner.ID, owner.ID,
		spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"})
	require.NoError(t, err)

	require.NoError(t, detections.Start(ctx, app.ID))
	n, err := detections.AbandonRunning(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, int64(1))

	got, err := detections.Get(ctx, app.ID)
	require.NoError(t, err)
	require.Equal(t, state.DetectionFailed, got.Status)
	require.Contains(t, string(got.Body), "restarted")

	rev, err := apps.CreateRevision(ctx, app.ID, &spec.AppSpec{
		SchemaVersion: spec.SchemaVersion, AppID: app.ID,
		Source: spec.Source{Type: spec.SourceGit, URL: "https://example.test/app"},
	}, spec.OriginManual, owner.ID)
	require.NoError(t, err)

	dep, err := deployments.Create(ctx, app.ID, rev.ID, "manual", owner.ID)
	require.NoError(t, err)
	require.NoError(t, deployments.SetStatus(ctx, dep.ID, state.DeployBuilding))

	inFlight, err := deployments.InFlight(ctx, app.ID)
	require.NoError(t, err)
	require.True(t, inFlight)

	_, err = deployments.AbandonInFlight(ctx)
	require.NoError(t, err)

	after, found, err := deployments.ByID(ctx, dep.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, state.DeployFailed, after.Status)

	inFlight, err = deployments.InFlight(ctx, app.ID)
	require.NoError(t, err)
	require.False(t, inFlight, "the next deploy is not refused on account of one that will never finish")
}
