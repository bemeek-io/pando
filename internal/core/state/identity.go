package state

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bemeek-io/pando/internal/adapter/identity/local"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/id"
)

// LocalAdapterID is the identity adapter seeded on a fresh install (R-041).
const LocalAdapterID = "idp_local"

// Users reads and writes user records.
type Users struct{ db *DB }

func NewUsers(db *DB) *Users { return &Users{db: db} }

// ByUsername implements local.Users. Username is the local adapter's
// external_id.
func (u *Users) ByUsername(ctx context.Context, username string) (local.Record, bool, error) {
	var r local.Record
	var email, display *string
	err := u.db.QueryRow(ctx, `
		SELECT external_id, email, display_name, coalesce(password_hash, ''), status
		FROM users
		WHERE adapter_id = $1 AND external_id = $2 AND deleted_at IS NULL`,
		LocalAdapterID, username).
		Scan(&r.ExternalID, &email, &display, &r.PasswordHash, &r.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return local.Record{}, false, nil
	}
	if err != nil {
		return local.Record{}, false, errs.Wrap(errs.Internal, "Could not look up the account.", err)
	}
	if email != nil {
		r.Email = *email
	}
	if display != nil {
		r.DisplayName = *display
	}
	return r, true, nil
}

// User is a stored principal.
//
// Tagged because this type is serialized directly by the API, and every other
// type on the wire is lower_snake_case. An untagged struct would put Go field
// names in the API surface — where they would then be a compatibility promise.
type User struct {
	ID          string `json:"id"`
	AdapterID   string `json:"adapter_id"`
	ExternalID  string `json:"external_id"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Status      string `json:"status"`

	MustChangePassword bool `json:"must_change_password"`
}

// Create inserts a user and returns it.
func (u *Users) Create(ctx context.Context, adapterID, externalID, email, displayName, passwordHash string, mustChange bool) (User, error) {
	user := User{
		ID:                 id.New(id.User),
		AdapterID:          adapterID,
		ExternalID:         externalID,
		Email:              email,
		DisplayName:        displayName,
		Status:             "active",
		MustChangePassword: mustChange,
	}
	_, err := u.db.Exec(ctx, `
		INSERT INTO users (id, adapter_id, external_id, email, display_name, password_hash, must_change_password, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'active')`,
		user.ID, adapterID, externalID, nullable(email), nullable(displayName), nullable(passwordHash), mustChange)
	if err != nil {
		return User{}, errs.Wrap(errs.Internal, "Could not create the account.", err)
	}
	return user, nil
}

// ByExternalID resolves an adapter's subject to a Pando user.
func (u *Users) ByExternalID(ctx context.Context, adapterID, externalID string) (User, bool, error) {
	var user User
	var email, display *string
	err := u.db.QueryRow(ctx, `
		SELECT id, adapter_id, external_id, email, display_name, status, must_change_password
		FROM users
		WHERE adapter_id = $1 AND external_id = $2 AND deleted_at IS NULL`,
		adapterID, externalID).
		Scan(&user.ID, &user.AdapterID, &user.ExternalID, &email, &display, &user.Status, &user.MustChangePassword)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, errs.Wrap(errs.Internal, "Could not look up the account.", err)
	}
	if email != nil {
		user.Email = *email
	}
	if display != nil {
		user.DisplayName = *display
	}
	return user, true, nil
}

// ByID returns a user by Pando ID.
func (u *Users) ByID(ctx context.Context, userID string) (User, bool, error) {
	var user User
	var email, display *string
	err := u.db.QueryRow(ctx, `
		SELECT id, adapter_id, external_id, email, display_name, status, must_change_password
		FROM users WHERE id = $1`, userID).
		Scan(&user.ID, &user.AdapterID, &user.ExternalID, &email, &display, &user.Status, &user.MustChangePassword)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, false, nil
	}
	if err != nil {
		return User{}, false, errs.Wrap(errs.Internal, "Could not look up the account.", err)
	}
	if email != nil {
		user.Email = *email
	}
	if display != nil {
		user.DisplayName = *display
	}
	return user, true, nil
}

// SetStatus changes a user's status.
//
// Suspension is not deletion (R-049): this is the endpoint behind PATCH, and it
// must never trigger the destruction rules that DELETE does (R-282).
func (u *Users) SetStatus(ctx context.Context, userID, status string) error {
	switch status {
	case "active", "suspended":
	default:
		return errs.New(errs.ValidInvalid, "An account can be set to active or suspended.")
	}
	_, err := u.db.Exec(ctx,
		`UPDATE users SET status = $2, updated_at = now() WHERE id = $1`, userID, status)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not update the account.", err)
	}
	return nil
}

// EnsureLocalAdapter seeds the local identity adapter on a fresh install.
func (u *Users) EnsureLocalAdapter(ctx context.Context) error {
	_, err := u.db.Exec(ctx, `
		INSERT INTO identity_adapters (id, kind, name)
		VALUES ($1, $2, 'Local users')
		ON CONFLICT (id) DO NOTHING`, LocalAdapterID, local.Kind)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not set up local accounts.", err)
	}
	return nil
}

// Sessions are server-side rows; the cookie carries only ses_… (design 02 §2.7).
type Sessions struct{ db *DB }

func NewSessions(db *DB) *Sessions { return &Sessions{db: db} }

// Session is an active login.
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	AdapterID string    `json:"adapter_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Create issues a session.
func (s *Sessions) Create(ctx context.Context, userID, adapterID string, lifetime time.Duration, userAgent, ip string) (Session, error) {
	sess := Session{
		ID:        id.New(id.Session),
		UserID:    userID,
		AdapterID: adapterID,
		ExpiresAt: time.Now().UTC().Add(lifetime),
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO sessions (id, user_id, adapter_id, expires_at, user_agent, ip)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		sess.ID, userID, adapterID, sess.ExpiresAt, nullable(userAgent), nullableInet(ip))
	if err != nil {
		return Session{}, errs.Wrap(errs.Internal, "Could not start a session.", err)
	}
	return sess, nil
}

// Active returns a session if it exists, is unrevoked, and has not expired.
//
// Checked on every request rather than cached. At single-host scale this is one
// indexed lookup; caching it would add a delay to the revocation window (design
// 06 §3.1), and that window is a stated number rather than an accident.
func (s *Sessions) Active(ctx context.Context, sessionID string) (Session, bool, error) {
	var sess Session
	err := s.db.QueryRow(ctx, `
		SELECT id, user_id, adapter_id, expires_at
		FROM sessions
		WHERE id = $1 AND revoked_at IS NULL AND expires_at > now()`,
		sessionID).Scan(&sess.ID, &sess.UserID, &sess.AdapterID, &sess.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, errs.Wrap(errs.Internal, "Could not read the session.", err)
	}
	return sess, true, nil
}

// Revoke ends one session.
func (s *Sessions) Revoke(ctx context.Context, sessionID string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, sessionID)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not end the session.", err)
	}
	return nil
}

// RevokeAllForUser ends every session a user holds. Used when an adapter pushes
// a revocation (R-048), which is what makes immediate revocation possible.
func (s *Sessions) RevokeAllForUser(ctx context.Context, userID string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not end the account's sessions.", err)
	}
	return nil
}

func nullableInet(s string) any {
	if s == "" {
		return nil
	}
	return s
}
