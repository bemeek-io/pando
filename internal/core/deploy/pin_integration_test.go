//go:build integration

package deploy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/audit"
	"github.com/trypando/pando/internal/core/bootstrap"
	"github.com/trypando/pando/internal/core/spec"
	"github.com/trypando/pando/internal/core/state"
	"github.com/trypando/pando/internal/core/state/statetest"
	"github.com/trypando/pando/internal/secret"
)

// TestR120_PreparingARevisionPinsTheCommitItWillBuild asserts R-120: a ref is
// what was asked for, a commit is what runs. Resolving the ref produces a new
// revision naming the commit, and a revision that already names one is left
// as it is, so redeploying it builds the same code after the branch moves.
func TestR120_PreparingARevisionPinsTheCommitItWillBuild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, head := gitRepo(t)

	db, _ := statetest.Connect(t)
	first, err := bootstrap.Run(ctx, state.NewUsers(db), state.NewGrants(db), db, audit.New(db.Pool),
		secret.New("a-first-password-123"))
	require.NoError(t, err)
	apps := state.NewApps(db)
	src := spec.Source{Type: spec.SourceGit, URL: repo, Ref: "master"}
	app, err := apps.Create(ctx, "notes", "notes-pin", first.User.ID, first.User.ID, src)
	require.NoError(t, err)

	s := specWithProvisionedSlot()
	s.AppID = app.ID
	s.Source = src
	unpinned, err := apps.CreateRevision(ctx, app.ID, s, spec.OriginManual, first.User.ID)
	require.NoError(t, err)

	r := &Runner{apps: apps}
	pinned, err := r.PrepareRevision(ctx, unpinned, first.User.ID)
	require.NoError(t, err)
	require.NotEqual(t, unpinned.ID, pinned.ID, "pinning is a new revision: revisions are append-only (R-152)")
	require.Equal(t, head, pinned.Body.Source.Commit)
	require.Equal(t, "master", pinned.Body.Source.Ref, "the ref asked for is kept beside the commit")

	again, err := r.PrepareRevision(ctx, pinned, first.User.ID)
	require.NoError(t, err)
	require.Equal(t, pinned.ID, again.ID, "a revision that names a commit is not resolved again")
}

// gitRepo is a repository with one commit, and that commit's SHA.
func gitRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	require.NoError(t, err)
	wt, err := r.Worktree()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))
	_, err = wt.Add("main.go")
	require.NoError(t, err)
	sha, err := wt.Commit("initial", &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "t@example", When: time.Unix(1700000000, 0)},
	})
	require.NoError(t, err)
	return dir, sha.String()
}
