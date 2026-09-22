package authz

import (
	"context"

	"github.com/bemeek-io/pando/internal/errs"
)

// PrincipalKind identifies what is making a request.
type PrincipalKind string

const (
	KindUser      PrincipalKind = "user"
	KindToken     PrincipalKind = "token"
	KindAnonymous PrincipalKind = "anonymous"
	KindSystem    PrincipalKind = "system"
)

// Principal is the resolved identity of a request (design 06 §1).
type Principal struct {
	Kind PrincipalKind
	ID   string

	// UserID is the user authorization runs against. For a user it equals ID.
	// For a delegated token it is the owner (R-058, R-059) — authorization then
	// proceeds exactly as if the owner made the request, and no grant is ever
	// written for the token itself.
	//
	// For an account token it is empty: an account token is its own principal
	// and is looked up in grants under its own ID (R-060).
	UserID string

	TokenID string

	// Groups are resolved live per request (R-079), never denormalized.
	Groups []string

	// Email and DisplayName travel into the assertion's claims. They are
	// display detail, never used for authorization — an email is not an
	// identity here, users.id is (R-054).
	Email       string
	DisplayName string

	AdapterID string

	// Status of the underlying user, checked at step 2 of the evaluation order.
	Status string

	// Passcodes are the unlock tokens the request carried, by app ID (R-075a):
	// proof that whoever is visiting entered a passcode-protected app's
	// passcode. Unverified as carried — CheckData asks the store whether each
	// is live, on every request, as it asks about everything else.
	Passcodes map[string]string
}

// Anonymous is the principal for an unauthenticated request. It is a real
// principal with a real check path, not an absence — R-075's anonymous grant is
// a grant like any other, and R-056's assertion carries sub: "anonymous".
func Anonymous() Principal { return Principal{Kind: KindAnonymous} }

// System is the principal for the reconciler and background jobs. It bypasses
// grant checks but still writes audit events attributed to "system": anything a
// background job does must be as visible as anything a person does.
func System() Principal { return Principal{Kind: KindSystem, ID: "system"} }

// Store is the authorizer's view of persisted state.
//
// Narrow on purpose: the authorizer must not be able to reach anything else, and
// a small interface makes the evaluation order testable without a database.
type Store interface {
	// UserStatus returns the status of a user: active, suspended, or deleted.
	UserStatus(ctx context.Context, userID string) (string, error)

	// ControlGrantsFor returns control-plane grants matching the principal —
	// direct, by group, or by account token.
	ControlGrantsFor(ctx context.Context, appID string, p Principal) ([]Grant, error)

	// InstallGrantsFor returns the principal's installation-wide grants: the
	// ones with no app (O-17). A separate query from ControlGrantsFor rather
	// than a nullable argument, so an app-scoped lookup can never return an
	// install grant by passing the wrong value.
	InstallGrantsFor(ctx context.Context, p Principal) ([]Grant, error)

	// IsOwner reports whether userID owns the app (R-031).
	IsOwner(ctx context.Context, appID, userID string) (bool, error)

	// HasDataGrant reports whether the principal holds a data-plane grant,
	// directly or through a group.
	HasDataGrant(ctx context.Context, appID string, p Principal) (bool, error)

	// AnonymousAccess reports whether the app is shared with everyone (R-075),
	// and whether that sharing asks for a passcode (R-075a).
	AnonymousAccess(ctx context.Context, appID string) (granted, passcode bool, err error)

	// PasscodeUnlocked reports whether token is a live unlock of this app's
	// passcode: entered, not expired, and made under the grant that stands now.
	PasscodeUnlocked(ctx context.Context, appID, token string) (bool, error)

	// Role returns a role by ID.
	Role(ctx context.Context, roleID string) (Role, error)
}

// Grant binds a principal to an app on one plane.
type Grant struct {
	ID            string
	AppID         string
	Plane         string
	PrincipalKind string
	PrincipalID   string
	RoleID        string
}

// Policy is host policy, evaluated before grants.
type Policy interface {
	// Allows reports whether host policy permits the verb at all. Policy is a
	// floor, not an override (R-272): a policy disabling exec install-wide
	// denies the owner too.
	//
	// The principal is passed because O-12's resolution needs it. "Agents may
	// not exec here" is expressed as a policy scoped to token principals rather
	// than as an MCP-layer exclusion list, because an agent holding a token can
	// call the REST API directly — an MCP-specific block is a speed bump, not a
	// boundary. Enforcement has to live where every surface passes through it,
	// and this is that place.
	Allows(ctx context.Context, p Principal, verb Verb, appID string) error
}

// Auditor records authorization outcomes.
type Auditor interface {
	Denied(ctx context.Context, p Principal, appID string, verb Verb, reason errs.Code)
}

// Authorizer evaluates both planes.
type Authorizer struct {
	store   Store
	policy  Policy
	auditor Auditor
}

func New(store Store, policy Policy, auditor Auditor) *Authorizer {
	return &Authorizer{store: store, policy: policy, auditor: auditor}
}

// CheckControl authorizes a control-plane action: managing an app.
//
// The order is fixed and each step can only deny; none can restore access denied
// by an earlier step (design 06 §2).
func (a *Authorizer) CheckControl(ctx context.Context, p Principal, appID string, verb Verb) error {
	// An install verb has no app to be held on, and evaluating one here would
	// search for a grant that cannot exist and deny — safe, but it would hide a
	// call site that meant CheckInstall. Refused loudly instead.
	if InstallScoped(verb) {
		return errs.Newf(errs.Internal,
			"%s is an installation-wide permission and cannot be checked against an app.", verb)
	}

	if p.Kind == KindSystem {
		// The reconciler and background jobs. Grant checks are bypassed; audit
		// is not.
		return nil
	}

	denial, err := a.control(ctx, p, appID, verb)
	if err != nil {
		return err
	}
	if denial != nil {
		return a.deny(ctx, p, appID, verb, denial)
	}
	return nil
}

// AppVerbs returns the app verbs the principal may use on an app, by asking
// the same question CheckControl asks for each — so what the console shows as
// editable is what the API will allow, and cannot drift from it. Denials are
// not audited: this is somebody looking at a screen, not trying anything.
func (a *Authorizer) AppVerbs(ctx context.Context, p Principal, appID string) ([]Verb, error) {
	out := []Verb{}
	for _, verb := range AppVerbs() {
		if p.Kind == KindSystem {
			out = append(out, verb)
			continue
		}
		denial, err := a.control(ctx, p, appID, verb)
		if err != nil {
			return nil, err
		}
		if denial == nil {
			out = append(out, verb)
		}
	}
	return out, nil
}

// Allows reports whether CheckControl would allow the verb, without auditing a
// denial — for deciding what to show, as AppVerbs does, one verb at a time.
func (a *Authorizer) Allows(ctx context.Context, p Principal, appID string, verb Verb) (bool, error) {
	if InstallScoped(verb) {
		return false, errs.Newf(errs.Internal,
			"%s is an installation-wide permission and cannot be checked against an app.", verb)
	}
	if p.Kind == KindSystem {
		return true, nil
	}
	denial, err := a.control(ctx, p, appID, verb)
	if err != nil {
		return false, err
	}
	return denial == nil, nil
}

// control evaluates steps 1–7 for one app verb. It returns the reason for a
// denial, or a failure to evaluate at all, and audits neither.
func (a *Authorizer) control(ctx context.Context, p Principal, appID string, verb Verb) (denial, failure error) {
	// Steps 1–4: the principal itself.
	if err := a.checkPrincipal(ctx, p); err != nil {
		return err, nil
	}

	// Step 5: host policy, before grants. A policy that disables a verb
	// install-wide denies the owner too (R-272) — and an administrator.
	if a.policy != nil {
		if err := a.policy.Allows(ctx, p, verb, appID); err != nil {
			return err, nil
		}
	}

	// Steps 6–7: grants, then the verb.
	grants, err := a.store.ControlGrantsFor(ctx, appID, p)
	if err != nil {
		return nil, err
	}
	for _, g := range grants {
		role, err := a.store.Role(ctx, g.RoleID)
		if err != nil {
			return nil, err
		}
		if role.Has(verb) {
			return nil, nil
		}
	}

	// Step 6b: an install grant that reaches every app (R-081). The only
	// install verbs that bear on an app are install.apps.view and
	// install.apps.manage, and what each stands for is the table in everyApp —
	// nothing else in an install role is read here.
	install, err := a.store.InstallGrantsFor(ctx, p)
	if err != nil {
		return nil, err
	}
	for _, g := range install {
		role, err := a.store.Role(ctx, g.RoleID)
		if err != nil {
			return nil, err
		}
		for _, held := range role.Verbs {
			if everyApp(held, verb) {
				return nil, nil
			}
		}
	}

	return errs.Newf(errs.PermVerbRequired, "You do not have permission to do this. It requires %s on this app.", verb), nil
}

// CheckInstall authorizes an installation-wide action (O-17, R-265).
//
// A separate function from CheckControl, taking different arguments, for the
// same reason CheckData is separate: the scopes are different questions and a
// single function with an optional appID is one missed argument away from
// authorizing an app verb install-wide. That mistake has already been made once
// in this package's history, in the other direction (design 06 §2).
//
// Host policy still runs first (R-272) and still denies the holder: an install
// that has disabled a verb has disabled it for administrators too.
func (a *Authorizer) CheckInstall(ctx context.Context, p Principal, verb Verb) error {
	// The mirror of the guard in CheckControl, and the more important half. An
	// app verb evaluated install-wide would look for a grant that *can* exist
	// and could allow.
	if !InstallScoped(verb) {
		return errs.Newf(errs.Internal,
			"%s is a per-app permission and cannot be checked installation-wide.", verb)
	}

	if p.Kind == KindSystem {
		// The reconciler and background jobs, as in CheckControl. Audit is not
		// bypassed.
		return nil
	}

	if err := a.checkPrincipal(ctx, p); err != nil {
		return a.deny(ctx, p, "", verb, err)
	}

	if a.policy != nil {
		if err := a.policy.Allows(ctx, p, verb, ""); err != nil {
			return a.deny(ctx, p, "", verb, err)
		}
	}

	grants, err := a.store.InstallGrantsFor(ctx, p)
	if err != nil {
		return err
	}
	for _, g := range grants {
		role, err := a.store.Role(ctx, g.RoleID)
		if err != nil {
			return err
		}
		if role.Has(verb) {
			return nil
		}
	}

	return a.deny(ctx, p, "", verb, errs.Newf(errs.PermVerbRequired,
		"You do not have permission to do this. It requires %s for this installation.", verb))
}

// CheckData authorizes data-plane access: using an app through the proxy.
//
// This function contains exactly ONE cross-plane implication — ownership
// (R-072). No other control-plane role appears in it, and being a Pando admin
// does not appear in it at all (R-087: an admin has root and can reach a
// container outside Pando, but the supported path requires a grant).
//
// This was reversed once during design. If a change adds a control-plane check
// here, reject it — that is R-029, and the test asserting an operator on someone
// else's app is denied *use* exists to catch exactly that change.
func (a *Authorizer) CheckData(ctx context.Context, p Principal, appID string) error {
	if err := a.checkPrincipal(ctx, p); err != nil {
		return a.deny(ctx, p, appID, "app.use", err)
	}

	if p.UserID != "" {
		owner, err := a.store.IsOwner(ctx, appID, p.UserID)
		if err != nil {
			return err
		}
		if owner {
			return nil // R-072: the sole implication between planes.
		}
	}

	granted, err := a.store.HasDataGrant(ctx, appID, p)
	if err != nil {
		return err
	}
	if granted {
		return nil
	}

	anon, passcode, err := a.store.AnonymousAccess(ctx, appID)
	if err != nil {
		return err
	}
	if anon && !passcode {
		return nil // R-075.
	}
	if anon && passcode {
		// Shared with everyone who knows the passcode (R-075a). Still the
		// data plane alone — a passcode is a key to using the app, and says
		// nothing about managing it.
		if token := p.Passcodes[appID]; token != "" {
			unlocked, err := a.store.PasscodeUnlocked(ctx, appID, token)
			if err != nil {
				return err
			}
			if unlocked {
				return nil
			}
		}
		// Not audited as a denial: every first visit to a passcode app lands
		// here, and the proxy answers it with the passcode page.
		return errs.New(errs.PermPasscodeRequired, "This app asks for a passcode.")
	}

	return a.deny(ctx, p, appID, "app.use",
		errs.New(errs.PermDenied, "You do not have access to this app."))
}

// checkPrincipal runs steps 1–4: status, token validity, and token derivation.
func (a *Authorizer) checkPrincipal(ctx context.Context, p Principal) error {
	switch p.Kind {
	case KindAnonymous:
		// An anonymous principal is always "valid"; whether it may proceed is
		// entirely a question of grants.
		return nil

	case KindSystem:
		return nil

	case KindUser:
		return statusError(p.Status)

	case KindToken:
		// An account token is its own principal (R-060) and has no owner to
		// derive from. Its own validity was established at authentication.
		if p.UserID == "" {
			return nil
		}

		// Step 4 — token derivation. A delegated token is only as alive as its
		// owner (R-059). This is a live lookup on every request rather than a
		// cascade run at revocation time: slower, and correct, because a missed
		// cascade is a permanent security hole and a live lookup cannot be
		// missed.
		status, err := a.store.UserStatus(ctx, p.UserID)
		if err != nil {
			return err
		}
		if err := statusError(status); err != nil {
			return errs.New(errs.AuthTokenOrphaned,
				"This token no longer works because the account that created it is no longer active.")
		}
		return nil

	default:
		return errs.New(errs.AuthInvalid, "Unrecognized credential.")
	}
}

func statusError(status string) error {
	switch status {
	case "active":
		return nil
	case "suspended":
		// Suspended is not deleted (R-049). The distinction matters because
		// destruction rules fire on deletion and must not fire here.
		return errs.New(errs.AuthInvalid, "This account is suspended.")
	case "deleted":
		return errs.New(errs.AuthInvalid, "This account no longer exists.")
	default:
		return errs.New(errs.AuthInvalid, "This account is not active.")
	}
}

// deny records the denial and returns the error.
//
// Every denial is audited, not only successes. A denial pattern is the signal
// that matters for detecting misuse, and it is the thing most commonly left out.
func (a *Authorizer) deny(ctx context.Context, p Principal, appID string, verb Verb, err error) error {
	if a.auditor != nil {
		a.auditor.Denied(ctx, p, appID, verb, errs.CodeOf(err))
	}
	return err
}
