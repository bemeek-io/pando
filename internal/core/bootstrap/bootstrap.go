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

	// Password is set only when Created is true. It is displayed once and never
	// stored in the clear (R-046), so a caller that discards it cannot recover
	// it — the operator resets rather than retrieves.
	Password secret.Value
}

// Run seeds the local identity adapter and, if no user exists, the first admin.
//
// Idempotent: on every start after the first it finds a user and does nothing.
// The check is "any user at all" rather than "the admin user" so that deleting
// the seeded admin after creating a real one does not make it reappear on the
// next restart.
func Run(ctx context.Context, users *state.Users, db *state.DB, auditor *audit.Writer) (Result, error) {
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
	digest, err := hash.New(password)
	if err != nil {
		return Result{}, errs.Wrap(errs.Internal, "Could not secure the initial password.", err)
	}

	user, err := users.Create(ctx, state.LocalAdapterID, AdminUsername, "", "Administrator", digest, true)
	if err != nil {
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
	return Result{Created: true, User: user, Password: password}, nil
}

func generatePassword() (secret.Value, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return secret.Value{}, errs.Wrap(errs.Internal, "Could not generate a password.", err)
	}
	return secret.New(base64.RawURLEncoding.EncodeToString(b)), nil
}
