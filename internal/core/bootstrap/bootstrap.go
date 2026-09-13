// Package bootstrap creates the first administrative account on a fresh install.
//
// R-046: first run creates a single administrative local user, the initial
// credential is generated and displayed once, and it must be changed on first
// login.
package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/base64"

	"go.uber.org/zap"

	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/hash"
	"github.com/bemeek-io/pando/internal/log"
	"github.com/bemeek-io/pando/internal/secret"
)

// AdminUsername is the account created on first run.
const AdminUsername = "admin"

// Result reports what first run produced.
type Result struct {
	Created bool
	User    state.User

	// Password is set only when Created is true and Pando generated it. It is
	// displayed once and never stored in the clear (R-046), so a caller that
	// discards it cannot recover it — the operator resets rather than
	// retrieves, with `pando admin reset-password`.
	Password secret.Value

	// Supplied says the operator provided the password, so there is nothing for
	// the caller to display. The distinction matters at the log line: printing
	// a password the operator already has copies it somewhere they did not
	// choose (R-194).
	Supplied bool
}

// Run seeds the local identity adapter and, if no user exists, the first admin.
//
// Idempotent: on every start after the first it finds a user and does nothing.
// The check is "any user at all" rather than "the admin user" so that deleting
// the seeded admin after creating a real one does not make it reappear on the
// next restart.
// Run creates the first administrative account if this install has none.
//
// supplied is an operator-chosen initial password, empty to have Pando generate
// one. It is a [P] override of R-046's "the initial credential is generated":
// the generated one is shown once, in a log line, and an install whose server
// container is recreated before anyone reads it has an account nobody can sign
// in to. That is not a hypothetical — it is what a `docker compose down && up`
// does, and there was no way back from it until the reset command existed.
//
// It changes nothing else. The account still must change its password at first
// sign-in, because an environment variable is not a safer place than a log
// line — it is in the Compose file, in `docker inspect`, and inherited by every
// child process. Supplying it buys a way in, not a credential.
func Run(ctx context.Context, users *state.Users, grants *state.Grants, db *state.DB, auditor *audit.Writer, supplied secret.Value) (Result, error) {
	if err := users.EnsureLocalAdapter(ctx); err != nil {
		return Result{}, err
	}

	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM users WHERE deleted_at IS NULL`).Scan(&count); err != nil {
		return Result{}, errs.Wrap(errs.Internal, "Could not check for existing accounts.", err)
	}
	if count > 0 {
		return Result{}, nil
	}

	password, err := generatePassword()
	if err != nil {
		return Result{}, err
	}

	fromOperator := !supplied.IsZero()
	if fromOperator {
		if supplied.Len() < hash.MinPasswordLength {
			return Result{}, errs.Newf(errs.ValidInvalid,
				"The administrator password supplied for this installation is shorter than %d characters.",
				hash.MinPasswordLength).
				WithRemedy("Set PANDO_ADMIN_PASSWORD to something longer, or unset it and Pando will generate one and print it once.")
		}
		password = supplied
	}
	digest, err := hash.New(password)
	if err != nil {
		return Result{}, errs.Wrap(errs.Internal, "Could not secure the initial password.", err)
	}

	user, err := users.Create(ctx, state.LocalAdapterID, AdminUsername, "", "Administrator", digest, true)
	if err != nil {
		return Result{}, err
	}

	// The account is administrative because of a grant, not because of a column
	// on the user (O-17). There is no `is_admin` anywhere: administration is a
	// role with verbs, revocable like any other, and this row is the only thing
	// that separates the first account from every later one. Without it a fresh
	// install has a user who can sign in and do nothing — which is how the
	// endpoints were gated before this existed, and why any logged-in account
	// could suspend this one.
	grant, err := grants.GrantInstall(ctx, "user", user.ID, authz.RoleAdministrator, "system")
	if err != nil {
		return Result{}, err
	}

	if err := auditor.Write(ctx, audit.Event{
		PrincipalKind: audit.KindSystem,
		PrincipalID:   "system",
		Action:        "grant.create",
		TargetKind:    "grant",
		TargetID:      grant.ID,
		Detail: map[string]any{
			"reason": "first run",
			"scope":  "install",
			"role":   authz.RoleAdministrator,
			"user":   user.ID,
		},
	}); err != nil {
		return Result{}, err
	}

	if err := auditor.Write(ctx, audit.Event{
		PrincipalKind: audit.KindSystem,
		PrincipalID:   "system",
		Action:        "user.create",
		TargetKind:    "user",
		TargetID:      user.ID,
		Detail:        map[string]any{"reason": "first run", "username": AdminUsername},
	}); err != nil {
		return Result{}, err
	}

	log.From(ctx).Info("created the first administrator", zap.String("user_id", user.ID))
	// The password is returned only when Pando chose it. Handing back one the
	// operator already supplied invites the caller to print it, which copies a
	// credential into a log for no one's benefit (R-194).
	out := Result{Created: true, User: user, Supplied: fromOperator}
	if !fromOperator {
		out.Password = password
	}
	return out, nil
}

// GeneratePassword returns a credential nobody chose.
//
// Exported because the reset command needs the same one first run produces:
// two generators would eventually disagree about length or alphabet, and the
// weaker one would be the one nobody looked at.
func GeneratePassword() (secret.Value, error) { return generatePassword() }

func generatePassword() (secret.Value, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return secret.Value{}, errs.Wrap(errs.Internal, "Could not generate a password.", err)
	}
	return secret.New(base64.RawURLEncoding.EncodeToString(b)), nil
}
