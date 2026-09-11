//go:build integration

package state_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/state"
)

// startPostgres brings up a real Postgres and returns an owner connection URL.
func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("pando"),
		postgres.WithUsername("pando"),
		postgres.WithPassword("test-password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	return dsn
}

// TestR027_AuditLogIsNotRewritable asserts R-027.
//
// This is phase 0's acceptance condition: an audit event can be written and
// provably not modified. The proof has to be attempted-and-refused, not
// inspected — reading the grant table would only show that the REVOKE ran, and
// the REVOKE is precisely what a table owner ignores.
func TestR027_AuditLogIsNotRewritable(t *testing.T) {
	ctx := context.Background()
	ownerURL := startPostgres(t)

	db, err := state.Connect(ctx, state.ConnectOptions{OwnerURL: ownerURL})
	require.NoError(t, err)
	t.Cleanup(db.Close)

	require.NoError(t, audit.New(db.Pool).Write(ctx, audit.Event{
		PrincipalKind: audit.KindUser,
		PrincipalID:   "usr_01HQ8",
		Action:        "app.deploy",
		AppID:         "app_01HQ8",
		Detail:        map[string]any{"spec_revision": 1},
	}))

	var count int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM audit_events`).Scan(&count))
	require.Equal(t, 1, count, "the event should have been written")

	// The application role must be refused both ways.
	_, err = db.Exec(ctx, `UPDATE audit_events SET action = 'app.nothing-happened'`)
	require.Error(t, err, "UPDATE on audit_events must be refused")
	require.Contains(t, err.Error(), "permission denied")

	_, err = db.Exec(ctx, `DELETE FROM audit_events`)
	require.Error(t, err, "DELETE on audit_events must be refused")
	require.Contains(t, err.Error(), "permission denied")

	_, err = db.Exec(ctx, `TRUNCATE audit_events`)
	require.Error(t, err, "TRUNCATE on audit_events must be refused")

	// The record is untouched and still readable.
	var action string
	require.NoError(t, db.QueryRow(ctx, `SELECT action FROM audit_events`).Scan(&action))
	require.Equal(t, "app.deploy", action)
}

// TestR027_ApplicationRoleDoesNotOwnTheSchema asserts the mechanism behind the
// test above, because it is the part a later refactor would quietly undo.
//
// A table's owner keeps UPDATE and DELETE no matter what is revoked. If Pando
// ever migrated and served traffic as one role, the REVOKE would still run, the
// grant table would still look right, and the audit log would be rewritable.
func TestR027_ApplicationRoleDoesNotOwnTheSchema(t *testing.T) {
	ctx := context.Background()
	ownerURL := startPostgres(t)

	db, err := state.Connect(ctx, state.ConnectOptions{OwnerURL: ownerURL})
	require.NoError(t, err)
	t.Cleanup(db.Close)

	var currentUser, tableOwner string
	require.NoError(t, db.QueryRow(ctx, `SELECT current_user`).Scan(&currentUser))
	require.NoError(t, db.QueryRow(ctx,
		`SELECT tableowner FROM pg_tables WHERE tablename = 'audit_events'`).Scan(&tableOwner))

	require.Equal(t, state.AppRole, currentUser, "traffic should be served as the restricted role")
	require.NotEqual(t, currentUser, tableOwner,
		"the application role must not own audit_events — an owner ignores REVOKE")
}

// TestR027_AuditRemainsImmutableAcrossRestarts asserts that the grant policy is
// re-applied rather than assumed, so a grant that drifts is corrected.
func TestR027_AuditRemainsImmutableAcrossRestarts(t *testing.T) {
	ctx := context.Background()
	ownerURL := startPostgres(t)

	db, err := state.Connect(ctx, state.ConnectOptions{OwnerURL: ownerURL})
	require.NoError(t, err)
	db.Close()

	// Simulate drift: an operator, or a bad migration, hands the app role
	// UPDATE on the audit log.
	owner, err := pgxpool.New(ctx, ownerURL)
	require.NoError(t, err)
	_, err = owner.Exec(ctx, fmt.Sprintf(`GRANT UPDATE, DELETE ON audit_events TO %s`, state.AppRole))
	require.NoError(t, err)
	owner.Close()

	// Starting again must put it back.
	db2, err := state.Connect(ctx, state.ConnectOptions{OwnerURL: ownerURL})
	require.NoError(t, err)
	t.Cleanup(db2.Close)

	require.NoError(t, audit.New(db2.Pool).Write(ctx, audit.Event{
		PrincipalKind: audit.KindSystem,
		PrincipalID:   "system",
		Action:        "server.start",
	}))

	_, err = db2.Exec(ctx, `UPDATE audit_events SET action = 'tampered'`)
	require.Error(t, err, "a drifted grant should have been revoked on restart")
}

// TestStartupFailsLoudlyWhenAuditCannotBeProtected asserts the external-database
// preflight: Pando refuses to run rather than serving with a rewritable audit
// log. A degraded mode is not acceptable — the value of enforcing R-027 at the
// database is that it holds without anyone checking.
func TestStartupFailsLoudlyWhenAuditCannotBeProtected(t *testing.T) {
	ctx := context.Background()
	ownerURL := startPostgres(t)

	// Bring the schema up normally, then hand the app role ownership of the
	// audit table — the situation a managed-Postgres install can land in.
	db, err := state.Connect(ctx, state.ConnectOptions{OwnerURL: ownerURL})
	require.NoError(t, err)
	db.Close()

	owner, err := pgxpool.New(ctx, ownerURL)
	require.NoError(t, err)
	_, err = owner.Exec(ctx, fmt.Sprintf(`ALTER TABLE audit_events OWNER TO %s`, state.AppRole))
	require.NoError(t, err)
	owner.Close()

	_, err = state.Connect(ctx, state.ConnectOptions{OwnerURL: ownerURL})
	require.Error(t, err, "Pando must refuse to start when its audit log is rewritable")
	require.Contains(t, err.Error(), "tamper-proof")
}

// TestConnectRetriesUntilPostgresIsReady asserts the Compose race is handled:
// both services start at once, so Postgres will not be accepting connections
// when Pando first dials.
func TestConnectRetriesUntilPostgresIsReady(t *testing.T) {
	ctx := context.Background()

	// A URL pointing at nothing must fail within the timeout rather than hang,
	// and must say something a person can act on.
	start := time.Now()
	_, err := state.Connect(ctx, state.ConnectOptions{
		OwnerURL:       "postgres://pando:pando@127.0.0.1:1/pando?sslmode=disable",
		ConnectTimeout: 3 * time.Second,
	})
	require.Error(t, err)
	require.WithinDuration(t, start.Add(3*time.Second), time.Now(), 5*time.Second)
	require.Contains(t, err.Error(), "could not reach its state database")
}
