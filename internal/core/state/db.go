package state

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/log"
	"github.com/bemeek-io/pando/internal/secret"
	"github.com/bemeek-io/pando/migrations"
)

// AppRole is the Postgres role Pando serves traffic as.
//
// It is deliberately not the role that owns the schema, and the reason is more
// specific than it first appears. REVOKE does work against a table's owner —
// after REVOKE UPDATE, has_table_privilege reports false even for the owner. But
// an owner holds grant option implicitly, so it can hand the privilege straight
// back to itself:
//
//	GRANT UPDATE ON audit_events TO pando_app;   -- succeeds, run as pando_app
//	UPDATE audit_events SET action = 'something else';
//
// Against a role that owns the table, the REVOKE is therefore a speed bump and
// not a boundary: one statement undoes it, and that statement is available to
// exactly the process an attacker would be running inside. Ownership is the
// property that has to be denied, not the privilege.
//
// So the owner creates and migrates; this role reads and writes, owns nothing,
// and cannot grant itself anything (R-027).
const AppRole = "pando_app"

// DB is a connection to the state store, held as the application role.
type DB struct {
	*pgxpool.Pool

	// schemaVersion is the migration the database is at, recorded when
	// migrations run. A DR bundle carries it so a restore can refuse a bundle
	// from a newer schema rather than half-applying it (R-215).
	schemaVersion uint
}

// SchemaVersion is the migration version this database is at.
func (db *DB) SchemaVersion() uint { return db.schemaVersion }

// ConnectOptions configures the bootstrap sequence.
type ConnectOptions struct {
	// OwnerURL is the connection string Pando is given. It must be able to run
	// migrations and manage AppRole.
	OwnerURL string

	// ConnectTimeout bounds the retry loop. Compose starts Pando and Postgres
	// together, so Postgres will not be accepting connections when Pando first
	// dials — this is the standard Compose race and is handled rather than
	// assumed away.
	ConnectTimeout time.Duration

	// SkipMigrate is for tests that manage schema themselves.
	SkipMigrate bool
}

func (o *ConnectOptions) setDefaults() {
	if o.ConnectTimeout == 0 {
		o.ConnectTimeout = 60 * time.Second
	}
}

// Connect brings the state store up and returns a pool held as AppRole.
//
// The sequence is: wait for Postgres, migrate as owner, provision the
// application role, apply the grant policy, verify it, then reconnect as the
// application role. Every step after the wait is idempotent and re-runs on each
// start, so a grant that drifts is corrected rather than discovered later.
func Connect(ctx context.Context, opts ConnectOptions) (*DB, error) {
	opts.setDefaults()
	l := log.From(ctx)

	owner, err := waitForPostgres(ctx, opts.OwnerURL, opts.ConnectTimeout)
	if err != nil {
		return nil, err
	}
	defer owner.Close()

	if !opts.SkipMigrate {
		if err := Migrate(ctx, opts.OwnerURL); err != nil {
			return nil, err
		}
	}

	appPassword, err := provisionAppRole(ctx, owner)
	if err != nil {
		return nil, err
	}
	if err := applyGrants(ctx, owner); err != nil {
		return nil, err
	}
	if err := verifyAuditImmutability(ctx, owner); err != nil {
		return nil, err
	}

	appURL, err := withCredentials(opts.OwnerURL, AppRole, appPassword)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		return nil, errs.Wrap(errs.Internal, "Could not connect to the state database as the application role.", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errs.Wrap(errs.Internal, "Could not connect to the state database as the application role.", err)
	}

	l.Info("state store ready", zap.String("role", AppRole))
	return &DB{Pool: pool, schemaVersion: appliedSchemaVersion}, nil
}

// waitForPostgres dials until Postgres answers or the timeout expires.
func waitForPostgres(ctx context.Context, dsn string, timeout time.Duration) (*pgxpool.Pool, error) {
	l := log.From(ctx)
	deadline := time.Now().Add(timeout)

	var lastErr error
	for attempt := 1; ; attempt++ {
		pool, err := pgxpool.New(ctx, dsn)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				return pool, nil
			}
			pool.Close()
		}
		lastErr = err

		if time.Now().After(deadline) {
			break
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		wait := min(time.Duration(attempt)*500*time.Millisecond, 5*time.Second)
		l.Debug("waiting for postgres", zap.Int("attempt", attempt), zap.Duration("retry_in", wait), zap.Error(err))
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}

	return nil, errs.Wrap(errs.Internal,
		"Pando could not reach its state database. Check that Postgres is running and that the connection URL is correct.",
		lastErr).WithRemedy("If you are running the bundled Compose file, check `docker compose logs postgres`. If you set PANDO_DATABASE_URL, verify the host, port, and credentials.")
}

// Migrate applies pending migrations as the owning role.
func Migrate(ctx context.Context, ownerURL string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not read the embedded migrations.", err)
	}

	cfg, err := pgx.ParseConfig(ownerURL)
	if err != nil {
		return errs.Wrap(errs.Internal, "The database connection URL is malformed.", err)
	}
	db := stdlib.OpenDB(*cfg)
	defer db.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not prepare the database for migration.", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not prepare the database for migration.", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return errs.Wrap(errs.Internal, "Database migration failed.", err)
	}

	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return errs.Wrap(errs.Internal, "Could not read the database schema version.", err)
	}
	if dirty {
		return errs.New(errs.Internal,
			fmt.Sprintf("The database schema is marked dirty at version %d, which means a previous migration failed partway.", version)).
			WithRemedy("Restore from a backup, or resolve the failed migration manually before starting Pando again.")
	}

	log.From(ctx).Info("migrations applied", zap.Uint("version", version))
	appliedSchemaVersion = version
	return nil
}

// appliedSchemaVersion is set by migrate and read when the DB is built. A
// package variable rather than a return value because migrate runs on the owner
// pool, before the DB the rest of the process uses exists.
var appliedSchemaVersion uint

// provisionAppRole creates or updates AppRole and returns its password.
//
// The password is regenerated on every start and never persisted. Pando is the
// only thing that connects as this role, and it holds the value only for the
// life of the process — so there is one less secret at rest, and a leaked
// password expires at the next restart.
func provisionAppRole(ctx context.Context, owner *pgxpool.Pool) (secret.Value, error) {
	password, err := randomPassword()
	if err != nil {
		return secret.Value{}, err
	}

	// The role name is a compile-time constant and the password is passed as a
	// literal only because Postgres does not accept parameters in CREATE ROLE.
	// quoteLiteral escapes it; nothing here is caller-controlled.
	stmt := fmt.Sprintf(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '%s') THEN
				CREATE ROLE %s LOGIN PASSWORD %s;
			ELSE
				ALTER ROLE %s LOGIN PASSWORD %s;
			END IF;
		END
		$$;`, AppRole, AppRole, quoteLiteral(password.Reveal()), AppRole, quoteLiteral(password.Reveal()))

	if _, err := owner.Exec(ctx, stmt); err != nil {
		return secret.Value{}, errs.Wrap(errs.Internal,
			"Pando could not create the restricted database role it serves traffic as.",
			err).
			WithDetail("role", AppRole).
			WithRemedy("Pando needs a database account that can CREATE ROLE and GRANT. If you set PANDO_DATABASE_URL to an existing database, grant those privileges or point Pando at a database it owns. This is required: without a separate role, the audit log cannot be made tamper-proof.")
	}
	return password, nil
}

// applyGrants applies the grant policy to every table.
//
// It runs on every start, after migrations, rather than being written into each
// migration. That is deliberate: a future migration that adds a table gets the
// right grants without anyone remembering to write them, and — more
// importantly — cannot accidentally hand out UPDATE on audit_events. The
// policy lives in exactly one place.
func applyGrants(ctx context.Context, owner *pgxpool.Pool) error {
	stmts := []string{
		fmt.Sprintf(`GRANT USAGE ON SCHEMA public TO %s`, AppRole),
		fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %s`, AppRole),
		fmt.Sprintf(`GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %s`, AppRole),

		// R-027. The audit log is append-only, and this is the line that makes
		// it true of core as well as of adapters — which is stronger, and costs
		// nothing.
		fmt.Sprintf(`REVOKE UPDATE, DELETE, TRUNCATE ON audit_events FROM %s`, AppRole),

		// Deny by default for tables added later: a new table is unreachable
		// until the next start re-runs the GRANT above, rather than arriving
		// with whatever PUBLIC happens to have.
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s`, AppRole),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO %s`, AppRole),
	}

	for _, stmt := range stmts {
		if _, err := owner.Exec(ctx, stmt); err != nil {
			return errs.Wrap(errs.Internal, "Pando could not apply database permissions.", err).
				WithDetail("statement", stmt).
				WithRemedy("Pando's database account needs GRANT privileges on the schema it owns.")
		}
	}
	return nil
}

// verifyAuditImmutability refuses to start if the audit log is rewritable.
//
// This is the preflight the external-database path needs. A degraded mode that
// runs anyway is not acceptable: the entire value of enforcing R-027 at the
// database is that it holds without anyone checking, so an install where it
// silently does not hold is worse than one that refuses to start and says why.
func verifyAuditImmutability(ctx context.Context, owner *pgxpool.Pool) error {
	var canUpdate, canDelete bool
	var tableOwner string
	err := owner.QueryRow(ctx, `
		SELECT
			has_table_privilege($1, 'audit_events', 'UPDATE'),
			has_table_privilege($1, 'audit_events', 'DELETE'),
			(SELECT tableowner FROM pg_tables WHERE tablename = 'audit_events')`,
		AppRole).Scan(&canUpdate, &canDelete, &tableOwner)
	if err != nil {
		return errs.Wrap(errs.Internal, "Pando could not verify that its audit log is tamper-proof.", err)
	}

	// Ownership is checked separately from privilege, and it is the check that
	// matters. A revoked privilege reads as absent right up until the owner
	// grants it back to itself, which takes one statement and no extra access.
	if tableOwner == AppRole {
		return errs.New(errs.Internal,
			"Pando's audit log is not tamper-proof: the account it serves traffic as owns the audit table, and an owner can grant itself permission to rewrite it at any time.").
			WithDetail("role", AppRole).
			WithDetail("table_owner", tableOwner).
			WithRemedy("Pando must connect as an account that does not own its schema. Give it a database it owns, or an account separate from the schema owner. Refer to the external-database setup notes.")
	}

	if canUpdate || canDelete {
		return errs.New(errs.Internal,
			"Pando's audit log is not tamper-proof: the account it serves traffic as can modify audit records.").
			WithDetail("role", AppRole).
			WithDetail("can_update", canUpdate).
			WithDetail("can_delete", canDelete).
			WithRemedy("This usually means the application role owns the audit_events table. An owner can grant itself UPDATE at any time, so revoking it is not enough — Pando must connect as an account that does not own its schema. Refer to the external-database setup notes.")
	}
	return nil
}

// Close releases the pool.
func (db *DB) Close() {
	if db != nil && db.Pool != nil {
		db.Pool.Close()
	}
}

func randomPassword() (secret.Value, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return secret.Value{}, errs.Wrap(errs.Internal, "Could not generate a database password.", err)
	}
	return secret.New(base64.RawURLEncoding.EncodeToString(b)), nil
}

// quoteLiteral renders a Postgres string literal, doubling embedded quotes.
func quoteLiteral(s string) string {
	out := make([]rune, 0, len(s)+2)
	out = append(out, '\'')
	for _, r := range s {
		if r == '\'' {
			out = append(out, '\'')
		}
		out = append(out, r)
	}
	out = append(out, '\'')
	return string(out)
}

// withCredentials rewrites a connection URL's user and password.
func withCredentials(dsn, user string, password secret.Value) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", errs.Wrap(errs.Internal, "The database connection URL is malformed.", err)
	}
	u.User = url.UserPassword(user, password.Reveal())
	return u.String(), nil
}
