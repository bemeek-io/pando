//go:build integration

package state_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
)

func score(n int) *int { return &n }

// TestR312_AcceptingAProposalDoesNotHideTheScanTakenAtDiscovery asserts the
// fallback that makes "scanned at discovery" mean anything.
//
// Detection scans the checkout before a spec revision exists, so that scan
// belongs to no revision. Accepting the proposal pins one — and a lookup that
// insisted on the pinned revision answered "this app has not been scanned yet"
// a minute after scanning the very source the revision was written from.
func TestR312_AcceptingAProposalDoesNotHideTheScanTakenAtDiscovery(t *testing.T) {
	ctx := context.Background()
	db := connected(t)
	alice := seedUser(t, db, "alice")
	apps := state.NewApps(db)
	scans := state.NewScans(db)

	app, err := apps.Create(ctx, "crewmate", "crewmate", alice.ID, alice.ID,
		spec.Source{Type: spec.SourceGit, URL: "https://example.test/crewmate"})
	require.NoError(t, err)

	// What detection records: a score, and no revision to attach it to.
	atDiscovery, err := scans.Record(ctx, state.Scan{
		AppID: app.ID, ScannerRef: "scn_trivy", Score: score(64),
		Findings: []api.Finding{{ID: "CVE-1", Severity: api.SeverityHigh}},
	})
	require.NoError(t, err)
	require.Empty(t, atDiscovery.SpecID)

	rev, err := apps.CreateRevision(ctx, app.ID, minimalSpec(), spec.OriginDetected, alice.ID)
	require.NoError(t, err)
	require.NoError(t, apps.Pin(ctx, app.ID, rev.ID, state.StateProposed, alice.ID))

	found, ok, err := scans.Latest(ctx, app.ID, rev.ID)
	require.NoError(t, err)
	require.True(t, ok, "the scan taken at discovery still describes this app")
	require.Equal(t, atDiscovery.ID, found.ID)
	require.Empty(t, found.SpecID, "and says it belongs to no revision")

	// A scan of the revision itself wins over it, because it describes what is
	// actually deployed.
	ofRevision, err := scans.Record(ctx, state.Scan{
		AppID: app.ID, SpecID: rev.ID, ScannerRef: "scn_trivy", Score: score(80),
	})
	require.NoError(t, err)

	found, ok, err = scans.Latest(ctx, app.ID, rev.ID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, ofRevision.ID, found.ID, "a scan of this revision beats one of none")

	// And another revision's scan is not this revision's, however new it is.
	other, err := apps.CreateRevision(ctx, app.ID, minimalSpec(), spec.OriginManual, alice.ID)
	require.NoError(t, err)
	_, err = scans.Record(ctx, state.Scan{
		AppID: app.ID, SpecID: other.ID, ScannerRef: "scn_trivy", Score: score(10),
	})
	require.NoError(t, err)

	found, ok, err = scans.Latest(ctx, app.ID, rev.ID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, ofRevision.ID, found.ID, "a score for a spec this app is not running is not its score")
}
