package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/bemeek-io/pando/internal/secret"
)

// Category names an adapter category.
type Category string

const (
	CategoryIdentity Category = "identity"
	CategoryRouting  Category = "routing"
	CategoryBuilder  Category = "builder"
	CategoryRuntime  Category = "runtime"
	CategorySecrets  Category = "secrets"
	CategoryServices Category = "services"
	CategoryNotify   Category = "notify"

	// CategoryBackup is the eighth (R-252), added in phase 9. It reverses a
	// decision recorded in design 03 §8.1 through phase 8; that section now
	// carries the reversal and the reasoning, rather than the change being
	// visible only here.
	CategoryBackup Category = "backup"
)

// Adapter is implemented by every adapter in every category.
type Adapter interface {
	Kind() string
	Category() Category
	Configure(ctx context.Context, raw json.RawMessage) error

	// HealthCheck lets the planner refuse to plan against an unhealthy adapter
	// and return ADAPTER_UNAVAILABLE, rather than failing mid-deploy (R-254).
	HealthCheck(ctx context.Context) error
}

// IdentityAdapter authenticates. It does not authorize (R-044).
type IdentityAdapter interface {
	Adapter

	// Begin returns where to send the user, or nil for adapters that
	// authenticate inline, such as local username and password.
	Begin(ctx context.Context, redirect string) (*Redirect, error)

	// Authenticate resolves an inbound callback or credential to a subject.
	Authenticate(ctx context.Context, c Credential) (Subject, error)

	// SessionPolicy is how R-047 is honored mechanically: each adapter declares
	// its own lifetime and revocation mode, so the console can show the real
	// window per adapter instead of implying a global guarantee.
	SessionPolicy() SessionPolicy

	// SupportsPush reports whether the adapter can push changes (SCIM or
	// webhook), which is what makes immediate revocation possible (R-048).
	SupportsPush() bool
}

// Redirect sends the user to an external identity provider.
type Redirect struct {
	URL   string
	State string
}

// Credential is inbound authentication material.
type Credential struct {
	// Username and Password for inline adapters.
	Username string
	Password secret.Value

	// Code and State for redirect-based adapters.
	Code  string
	State string
}

// Subject is who the adapter says this is.
//
// It carries no roles, no verbs, and no permissions. Group *names* cross this
// boundary; what a group can *do* is Pando's (R-078).
type Subject struct {
	// ExternalID is stable within this adapter. It is not what the rest of
	// Pando uses — users.id is (R-054).
	ExternalID  string
	Email       string
	DisplayName string
	Groups      []string
}

// RevocationMode describes how quickly an adapter can revoke.
type RevocationMode string

const (
	// RevocationPush means the provider notifies Pando, so revocation is
	// immediate (R-048).
	RevocationPush RevocationMode = "push"
	// RevocationRefresh means Pando learns at the next refresh.
	RevocationRefresh RevocationMode = "refresh"
	// RevocationExpiryOnly means access ends when the session expires, and
	// nothing sooner. The console must say so rather than implying otherwise.
	RevocationExpiryOnly RevocationMode = "expiry_only"
)

// SessionPolicy is an adapter's own session behavior (R-047).
type SessionPolicy struct {
	MaxLifetime     time.Duration
	RevocationMode  RevocationMode
	RefreshInterval time.Duration
}
