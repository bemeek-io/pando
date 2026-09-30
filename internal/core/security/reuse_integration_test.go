//go:build integration

package security_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/adapter/api"
	"github.com/trypando/pando/internal/core/security"
	"github.com/trypando/pando/internal/core/spec"
	"github.com/trypando/pando/internal/core/state"
	"github.com/trypando/pando/internal/core/state/statetest"
	"github.com/trypando/pando/internal/hash"
	"github.com/trypando/pando/internal/secret"
)

// idleScanner is a scanner that is configured and never asked to scan: these
// tests are about the scans that do not run.
type idleScanner struct{}

func (idleScanner) Kind() string                                     { return "idle" }
func (idleScanner) Category() api.Category                           { return api.CategoryScanner }
func (idleScanner) Configure(context.Context, json.RawMessage) error { return nil }
func (idleScanner) HealthCheck(context.Context) error                { return nil }
func (idleScanner) ScannerCapabilities() api.ScannerCapabilities {
	return api.ScannerCapabilities{ScansImages: true, ScansSource: true}
}
func (idleScanner) Scan(context.Context, api.ScanRequest) (api.ScanResult, error) {
	panic("a reused scan must not run the scanner")
}

func revisionSpec() *spec.AppSpec {
	return &spec.AppSpec{
		SchemaVersion: spec.SchemaVersion,
		Source:        spec.Source{Type: spec.SourceGit, URL: "https://example.test/rota", Commit: "abc123"},
		Build:         spec.Build{Strategy: spec.BuildDockerfile, AdapterRef: "bld_buildkit"},
		Workloads:     []spec.Workload{{Name: "web", Primary: true, Exposed: true}},
		Routing:       spec.Routing{AdapterRef: "rte_loopback", Mode: spec.RoutingPort, Port: 8080},
		Runtime:       spec.RuntimeRef{AdapterRef: "rt_docker", IsolationFloor: spec.IsolationContainer},
		Deploy:        spec.Deploy{Strategy: spec.DeployRecreate},
	}
}

// TestR312_ARedeployOfAnUnchangedSourceIsScoredByItsScan asserts R-312 as
// amended by issue #84, through the service a deploy calls: a new revision of
// a source that was scanned gets that scan without the scanner running, a
// second deploy of the same revision adds nothing, and a scan by a scanner
// that is no longer the configured one is not reused.
func TestR312_ARedeployOfAnUnchangedSourceIsScoredByItsScan(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db, _ := statetest.Connect(t)

	users := state.NewUsers(db)
	require.NoError(t, users.EnsureLocalAdapter(ctx))
	digest, err := hash.New(secret.New("correct-password"))
	require.NoError(t, err)
	alice, err := users.Create(ctx, state.LocalAdapterID, "alice", "alice@corp.com", "alice", digest, false)
	require.NoError(t, err)

	apps := state.NewApps(db)
	app, err := apps.Create(ctx, "rota", "rota", alice.ID, alice.ID,
		spec.Source{Type: spec.SourceGit, URL: "https://example.test/rota"})
	require.NoError(t, err)
	first, err := apps.CreateRevision(ctx, app.ID, revisionSpec(), spec.OriginDetected, alice.ID)
	require.NoError(t, err)
	second, err := apps.CreateRevision(ctx, app.ID, revisionSpec(), spec.OriginEdited, alice.ID)
	require.NoError(t, err)

	registry := api.NewRegistry()
	svc := &security.Service{Scans: state.NewScans(db), Registry: registry}

	_, found, err := svc.Reuse(ctx, app.ID, second.ID, "abc123")
	require.NoError(t, err)
	require.False(t, found, "with no scanner configured there is nothing to reuse")

	require.NoError(t, registry.Register("scn_idle", idleScanner{}))
	score := 64
	ran, err := svc.Scans.Record(ctx, state.Scan{
		AppID: app.ID, SpecID: first.ID, Commit: "abc123", ScannerRef: "scn_idle", Score: &score,
		Findings: []api.Finding{{ID: "CVE-1", Severity: api.SeverityHigh}},
	})
	require.NoError(t, err)

	_, found, err = svc.Reuse(ctx, app.ID, second.ID, "def456")
	require.NoError(t, err)
	require.False(t, found, "a source nobody scanned is scanned")

	reused, found, err := svc.Reuse(ctx, app.ID, second.ID, "abc123")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, second.ID, reused.SpecID, "attached to the revision being deployed")
	require.Equal(t, ran.ID, reused.ReusedFrom)
	require.Len(t, reused.Findings, 1)

	again, found, err := svc.Reuse(ctx, app.ID, second.ID, "abc123")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, reused.ID, again.ID, "a second deploy of the same revision records nothing new")

	own, found, err := svc.Reuse(ctx, app.ID, "", "abc123")
	require.NoError(t, err)
	require.True(t, found, "with no revision the scan is returned as it is")
	require.Equal(t, ran.ID, own.Origin())

	// The installation changes scanner: what the old one found is not what
	// this one would report.
	require.NoError(t, registry.Register("scn_other", idleScanner{}))
	require.NoError(t, registry.SetDefault(api.CategoryScanner, "scn_other"))
	_, found, err = svc.Reuse(ctx, app.ID, second.ID, "abc123")
	require.NoError(t, err)
	require.False(t, found, "another scanner's scan is not reused")
}
