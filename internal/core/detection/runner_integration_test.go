//go:build integration

package detection_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	adapterapi "github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/core/detection"
	corepolicy "github.com/bemeek-io/pando/internal/core/policy"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/detect"
	"github.com/bemeek-io/pando/internal/errs"
)

// Sequence A's seam: the parts that need an app row, the source allowlist and
// somewhere to write the answer. The detectors themselves are tested in
// internal/detect; what is asserted here is what happens around them.

func connected(t *testing.T) *state.DB {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("pando"),
		postgres.WithUsername("pando"),
		postgres.WithPassword("test-password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := state.Connect(ctx, state.ConnectOptions{OwnerURL: dsn})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return db
}

// repoWith builds a real repository the runner can clone.
func repoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	r, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	wt, err := r.Worktree()
	require.NoError(t, err)

	for name, body := range files {
		full := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
		_, err := wt.Add(name)
		require.NoError(t, err)
	}
	_, err = wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "t@example", When: time.Unix(1700000000, 0)},
	})
	require.NoError(t, err)
	return dir
}

// runnerOver builds a detection runner wired the way main wires it, minus the
// runtime: with no trial runner a deferred question becomes a real one, which
// is R-097 working rather than a gap.
func runnerOver(t *testing.T, db *state.DB, policy *corepolicy.Evaluator) *detection.Runner {
	t.Helper()

	registry := adapterapi.NewRegistry()
	return &detection.Runner{
		Apps:       state.NewApps(db),
		Detections: state.NewDetections(db),
		Policy:     policy,
		Install:    detection.NewInstallation(registry, "apps.test"),
		Ports:      state.NewPorts(db),
		Job: &detect.Job{
			Auction: detect.NewAuction(
				detect.DockerfileDetector{},
				detect.ComposeDetector{},
				detect.StaticDetector{},
				detect.MonorepoDetector{},
			),
		},
	}
}

// owner seeds an account for apps to belong to: apps.owner_user_id is a real
// foreign key, which is what keeps an app from outliving whoever owns it.
func owner(t *testing.T, db *state.DB) string {
	t.Helper()
	users := state.NewUsers(db)
	require.NoError(t, users.EnsureLocalAdapter(context.Background()))

	user, err := users.Create(context.Background(),
		state.LocalAdapterID, "owner", "", "Owner", "", false)
	require.NoError(t, err)
	return user.ID
}

func appFrom(t *testing.T, db *state.DB, src spec.Source) string {
	t.Helper()
	ownerID := owner(t, db)

	app, err := state.NewApps(db).Create(context.Background(),
		"notes", "notes", ownerID, ownerID, src)
	require.NoError(t, err)
	return app.ID
}

// Sequence A: detection reads the repository, produces a proposal, and records
// it where GET /detection reads.
func TestDetectionRecordsAProposalAgainstTheApp(t *testing.T) {
	db := connected(t)
	ctx := context.Background()

	repo := repoWith(t, map[string]string{
		"Dockerfile": "FROM nginx:alpine\nEXPOSE 8080\n",
		"index.html": "<h1>notes</h1>",
	})
	appID := appFrom(t, db, spec.Source{Type: spec.SourceGit, URL: repo})

	got, err := runnerOver(t, db, corepolicy.Static(corepolicy.Default())).Detect(ctx, appID)
	require.NoError(t, err)
	require.NotEmpty(t, got.Status)

	// R-120: Ref is what the user asked for, Commit is what runs — recorded so
	// accepting pins a revision against a commit rather than a moving branch.
	require.NotEmpty(t, got.Commit, "the resolved commit is recorded on the proposal")

	stored, err := state.NewDetections(db).Get(ctx, appID)
	require.NoError(t, err)
	require.Equal(t, got.Status, stored.Status)
}

// R-092: the allowlist is checked before anything touches disk, so a blocked
// source produces zero disk writes — git clone is never invoked.
func TestR092_ABlockedSourceIsRefusedBeforeAnythingIsCloned(t *testing.T) {
	db := connected(t)
	ctx := context.Background()

	repo := repoWith(t, map[string]string{"Dockerfile": "FROM nginx:alpine"})
	appID := appFrom(t, db, spec.Source{Type: spec.SourceGit, URL: repo})

	runner := runnerOver(t, db, corepolicy.Static(corepolicy.Document{
		SourceAllowlist: []string{"github.com"},
	}))

	_, err := runner.Detect(ctx, appID)
	require.Equal(t, errs.PolicySourceNotAllowed, errs.CodeOf(err))

	// Not even a failed run was recorded: Detections.Start is reached after the
	// allowlist, so there is no row at all rather than one saying it failed.
	_, err = state.NewDetections(db).Get(ctx, appID)
	require.Equal(t, errs.NotFound, errs.CodeOf(err))
}

// The allowlist is checked here as well as at creation, because it can change
// between the two and a re-detection (R-022) of an app whose source is no
// longer allowed must not clone it.
func TestTheAllowlistIsRecheckedOnEveryDetection(t *testing.T) {
	db := connected(t)
	ctx := context.Background()

	repo := repoWith(t, map[string]string{"Dockerfile": "FROM nginx:alpine"})
	appID := appFrom(t, db, spec.Source{Type: spec.SourceGit, URL: repo})

	permissive := corepolicy.Static(corepolicy.Default())
	_, err := runnerOver(t, db, permissive).Detect(ctx, appID)
	require.NoError(t, err)

	narrowed := corepolicy.Static(corepolicy.Document{SourceAllowlist: []string{"github.com"}})
	_, err = runnerOver(t, db, narrowed).Detect(ctx, appID)
	require.Equal(t, errs.PolicySourceNotAllowed, errs.CodeOf(err))
}

func TestDetectingAnAppThatDoesNotExist(t *testing.T) {
	db := connected(t)

	_, err := runnerOver(t, db, corepolicy.Static(corepolicy.Default())).
		Detect(context.Background(), "app_01HQ8ZZZZZZZZZZZZZZZZZZZZZ")
	require.Equal(t, errs.NotFound, errs.CodeOf(err))
}

// An app with no source has nothing to detect from, and says so rather than
// failing somewhere further in.
func TestAnAppWithNoSourceSaysWhatToDoAboutIt(t *testing.T) {
	db := connected(t)
	appID := appFrom(t, db, spec.Source{})

	_, err := runnerOver(t, db, corepolicy.Static(corepolicy.Default())).
		Detect(context.Background(), appID)
	require.Equal(t, errs.StateInvalid, errs.CodeOf(err))
	require.NotEmpty(t, errs.As(err).Remedy, "R-105: it says what a valid answer looks like")
}

// Detection runs in the background after app creation, so a user who comes back
// to the console later needs to find out what happened — the failure is
// recorded rather than only returned.
func TestAFailedDetectionIsRecordedRatherThanOnlyReturned(t *testing.T) {
	db := connected(t)
	ctx := context.Background()

	appID := appFrom(t, db, spec.Source{
		Type: spec.SourceGit, URL: filepath.Join(t.TempDir(), "not-a-repo"),
	})

	_, err := runnerOver(t, db, corepolicy.Static(corepolicy.Default())).Detect(ctx, appID)
	require.Error(t, err)

	stored, storeErr := state.NewDetections(db).Get(ctx, appID)
	require.NoError(t, storeErr)
	require.Equal(t, state.DetectionFailed, stored.Status)
	require.NotEmpty(t, stored.Body, "the reason is there for the console to show")
}

// R-104: everything a repository cannot say about itself is configuration, not
// a question. Without the install's defaults a detected spec describes the app
// and says nothing about where it runs, which is a spec the planner refuses.
func TestR104_TheProposalCarriesTheInstallsOwnAnswers(t *testing.T) {
	db := connected(t)
	ctx := context.Background()

	repo := repoWith(t, map[string]string{
		"Dockerfile": "FROM nginx:alpine\nEXPOSE 8080\n",
	})
	appID := appFrom(t, db, spec.Source{Type: spec.SourceGit, URL: repo})

	got, err := runnerOver(t, db, corepolicy.Static(corepolicy.Default())).Detect(ctx, appID)
	require.NoError(t, err)
	require.NotEmpty(t, got.Body,
		"the review is where someone sees how their app will run, so the blanks are filled first")
}

// portModeInstall is an install whose routing adapter is port-mode, the way a
// loopback install is. Its defaults are otherwise the standard ones.
type portModeInstall struct{}

func (portModeInstall) Defaults(context.Context) spec.Defaults {
	d := spec.StandardDefaults()
	d.RoutingMode = spec.RoutingPort
	d.RoutingAdapter = "rte_loopback"
	return d
}

// TestO15_AFullPortRangeBlocksTheProposalRatherThanLeavingNoPort asserts that
// running out of ports is reported as running out of ports.
//
// It used to be swallowed: the spec was stored with port 0, accepted without
// complaint, and refused at deploy with "0 is not a usable port number" —
// which says nothing about the range being full or about deleting an app.
func TestO15_AFullPortRangeBlocksTheProposalRatherThanLeavingNoPort(t *testing.T) {
	db := connected(t)
	ctx := context.Background()

	repo := repoWith(t, map[string]string{"Dockerfile": "FROM nginx:alpine\nEXPOSE 8080\n"})
	appID := appFrom(t, db, spec.Source{Type: spec.SourceGit, URL: repo})

	runner := runnerOver(t, db, corepolicy.Static(corepolicy.Default()))
	runner.Install = portModeInstall{}
	// A range of one port, already held by another app. The same owner: a
	// second seeded account collides on the identity adapter's external ID.
	var ownerID string
	require.NoError(t, db.QueryRow(ctx, `SELECT owner_user_id FROM apps WHERE id = $1`, appID).Scan(&ownerID))
	taken, err := state.NewApps(db).Create(ctx, "taken", "taken", ownerID, ownerID,
		spec.Source{Type: spec.SourceGit, URL: repo})
	require.NoError(t, err)
	_, err = state.NewPorts(db).Allocate(ctx, "rte_loopback", taken.ID, 9900, 9900)
	require.NoError(t, err)
	runner.PortRangeStart, runner.PortRangeEnd = 9900, 9900

	got, err := runner.Detect(ctx, appID)
	require.NoError(t, err, "detection itself ran; the app just cannot be given a port")

	var proposal detect.Proposal
	require.NoError(t, json.Unmarshal(got.Body, &proposal))
	require.NotNil(t, proposal.Blocked, "the reason reaches the review")
	require.Equal(t, errs.CapacityNoFreePort, proposal.Blocked.Code)
	require.Contains(t, proposal.Blocked.Remedy, "Delete an app")
	require.Zero(t, proposal.DraftSpec.Routing.Port)
}
