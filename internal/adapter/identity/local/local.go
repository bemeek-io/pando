package local

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/hash"
)

// Kind is the adapter's kind string.
const Kind = "local"

// Users is the adapter's view of stored local users.
//
// Deliberately minimal: the adapter authenticates and nothing more (R-044). It
// cannot read grants, cannot write audit events, and does not know what a verb
// is — the R-027 boundary as an interface rather than a convention.
type Users interface {
	// ByUsername returns the stored record, or ok=false. Implementations must
	// not distinguish "no such user" from "wrong password" to the caller; that
	// is Authenticate's job below.
	ByUsername(ctx context.Context, username string) (Record, bool, error)
}

// Record is a stored local user.
type Record struct {
	ExternalID   string
	Email        string
	DisplayName  string
	PasswordHash string
	Status       string
}

// Adapter is the local username-and-password identity adapter (R-041).
//
// It is the v1 adapter and the default on a fresh install. R-042 is a
// documentation requirement rather than a code one: local users must be secure,
// but are not claimed to be the most secure option, and installs with real
// security requirements are expected to configure an external provider.
type Adapter struct {
	users Users

	// sessionMaxLifetime is this adapter's declared session lifetime.
	sessionMaxLifetime time.Duration
}

// New builds the adapter.
func New(users Users) *Adapter {
	return &Adapter{users: users, sessionMaxLifetime: 12 * time.Hour}
}

func (a *Adapter) Kind() string           { return Kind }
func (a *Adapter) Category() api.Category { return api.CategoryIdentity }

func (a *Adapter) Configure(_ context.Context, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var cfg struct {
		SessionMaxLifetime string `json:"session_max_lifetime"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return errs.Wrap(errs.ValidInvalid, "The local identity adapter's configuration could not be read.", err)
	}
	if cfg.SessionMaxLifetime != "" {
		d, err := time.ParseDuration(cfg.SessionMaxLifetime)
		if err != nil {
			return errs.Wrap(errs.ValidInvalid, "session_max_lifetime is not a valid duration, for example \"12h\".", err)
		}
		a.sessionMaxLifetime = d
	}
	return nil
}

// HealthCheck always succeeds: the local adapter has no external dependency,
// and the store it reads is Pando's own.
func (a *Adapter) HealthCheck(context.Context) error { return nil }

// Begin returns nil: this adapter authenticates inline.
func (a *Adapter) Begin(context.Context, string) (*api.Redirect, error) { return nil, nil }

// Authenticate verifies a username and password.
//
// Every failure returns the same error regardless of cause. Distinguishing "no
// such user" from "wrong password" turns the login form into an account
// enumeration oracle, and the work is done either way so the timing does not
// separate them: a missing user is compared against a decoy hash.
func (a *Adapter) Authenticate(ctx context.Context, c api.Credential) (api.Subject, error) {
	record, found, err := a.users.ByUsername(ctx, c.Username)
	if err != nil {
		return api.Subject{}, err
	}

	storedHash := record.PasswordHash
	if !found || storedHash == "" {
		storedHash = decoyHash
	}

	ok, verifyErr := hash.Verify(c.Password, storedHash)
	if !found || verifyErr != nil || !ok {
		return api.Subject{}, errInvalidCredentials()
	}

	// Status is checked by the authorizer too (evaluation order step 2). It is
	// checked here as well so a suspended user cannot mint a fresh session in
	// the first place.
	if record.Status != "active" {
		return api.Subject{}, errInvalidCredentials()
	}

	return api.Subject{
		ExternalID:  record.ExternalID,
		Email:       record.Email,
		DisplayName: record.DisplayName,
	}, nil
}

// SessionPolicy declares this adapter's own session behavior (R-047).
//
// The local adapter owns its user records, so it can revoke immediately — there
// is no external provider to hear from.
func (a *Adapter) SessionPolicy() api.SessionPolicy {
	return api.SessionPolicy{
		MaxLifetime:    a.sessionMaxLifetime,
		RevocationMode: api.RevocationPush,
	}
}

// SupportsPush is true in the trivial sense: changes originate here.
func (a *Adapter) SupportsPush() bool { return true }

func errInvalidCredentials() error {
	return errs.New(errs.AuthInvalid, "That username and password do not match.")
}

// decoyHash is a real argon2id hash of an unguessable value, verified against
// when no user matches so that a missing account and a wrong password take the
// same amount of work.
const decoyHash = "$argon2id$v=19$m=65536,t=3,p=2$Y2Fubm90Z3Vlc3N0aGlz$" +
	"3RQ0dJ1H5HLQZ0kFQnYyq0jLQ0xJ0x7VQ8kFQnYyq0g"

var _ api.IdentityAdapter = (*Adapter)(nil)
