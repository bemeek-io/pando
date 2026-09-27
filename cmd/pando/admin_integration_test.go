//go:build integration

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/audit"
	"github.com/trypando/pando/internal/core/bootstrap"
	"github.com/trypando/pando/internal/core/state"
	"github.com/trypando/pando/internal/hash"
	"github.com/trypando/pando/internal/secret"
)

// withAdmin is an install whose administrator was made from a password, and
// the environment `pando admin` reads its database from.
func withAdmin(t *testing.T) (*state.DB, *state.Users) {
	t.Helper()
	db, dsn := connectedURL(t)
	users := state.NewUsers(db)
	_, err := bootstrap.Run(context.Background(), users, state.NewGrants(db), db, audit.New(db.Pool),
		secret.New("the-first-password-1234"))
	require.NoError(t, err)
	t.Setenv("PANDO_DATABASE_URL", dsn)
	return db, users
}

func runAdmin(t *testing.T, args ...string) (string, error) {
	t.Helper()
	configPath := ""
	cmd := adminCmd(&configPath)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("")) // not a terminal, so nothing prompts
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

// TestR048_ResettingAPasswordFromTheHostEndsItsSessions asserts what
// `pando admin reset-password` promises: the new password works, the account
// must change it, every session it had is gone (R-048), and it is audited
// (R-227) — because it runs outside every check the API makes.
func TestR048_ResettingAPasswordFromTheHostEndsItsSessions(t *testing.T) {
	ctx := context.Background()
	db, users := withAdmin(t)

	var userID string
	require.NoError(t, db.QueryRow(ctx, `SELECT id FROM users WHERE external_id = $1`, bootstrap.AdminUsername).Scan(&userID))
	_, err := db.Exec(ctx, `INSERT INTO sessions (id, user_id, adapter_id, expires_at)
		SELECT 'ses_live', id, adapter_id, now() + interval '1 day' FROM users WHERE id = $1`, userID)
	require.NoError(t, err)

	out, err := runAdmin(t, "reset-password", "--password", "a-brand-new-password-9")
	require.NoError(t, err)
	require.Contains(t, out, "Password reset for admin.")
	require.NotContains(t, out, "New password:", "a supplied password is not echoed back")

	record, _, err := users.ByUsername(ctx, bootstrap.AdminUsername)
	require.NoError(t, err)
	ok, err := hash.Verify(secret.New("a-brand-new-password-9"), record.PasswordHash)
	require.NoError(t, err)
	require.True(t, ok)

	var live int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, userID).Scan(&live))
	require.Zero(t, live, "a reset that left a session alive has not reset anything")

	var action string
	require.NoError(t, db.QueryRow(ctx,
		`SELECT action FROM audit_events WHERE action = 'user.password.reset' AND target_id = $1`, userID).Scan(&action))
}

func TestWithoutAPasswordOneIsGeneratedAndShownOnce(t *testing.T) {
	withAdmin(t)
	out, err := runAdmin(t, "reset-password", "admin")
	require.NoError(t, err)
	require.Contains(t, out, "New password: ")
}

func TestAResetIsRefusedForAShortPasswordOrAnUnknownAccount(t *testing.T) {
	withAdmin(t)
	_, err := runAdmin(t, "reset-password", "--password", "short")
	require.ErrorContains(t, err, "at least")

	_, err = runAdmin(t, "reset-password", "nobody", "--password", "a-brand-new-password-9")
	require.Error(t, err)
}
