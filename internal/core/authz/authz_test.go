package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/errs"
)

// --- test double -----------------------------------------------------------

type store struct {
	userStatus map[string]string
	owner      map[string]string // appID -> userID
	control    map[string][]authz.Grant
	data       map[string][]string // appID -> principal IDs with a data grant
	anonymous  map[string]bool
	roles      map[string]authz.Role

	// install is a flat list, not keyed by app: an install grant has no app.
	// Held separately for the same reason the schema holds them in rows with a
	// null app_id — so an app lookup can never find one.
	install []authz.Grant
}

func newStore() *store {
	return &store{
		userStatus: map[string]string{},
		owner:      map[string]string{},
		control:    map[string][]authz.Grant{},
		data:       map[string][]string{},
		anonymous:  map[string]bool{},
		roles: map[string]authz.Role{
			authz.RoleViewer:        {ID: authz.RoleViewer, Name: "viewer", Builtin: true, Verbs: []authz.Verb{authz.AppView, authz.AppLogsRead}},
			authz.RoleOperator:      {ID: authz.RoleOperator, Name: "operator", Builtin: true, Verbs: []authz.Verb{authz.AppView, authz.AppLogsRead, authz.AppDeploy, authz.AppRestart, authz.AppSpecEdit, authz.AppSecretsWrite}},
			authz.RoleOwner:         {ID: authz.RoleOwner, Name: "owner", Builtin: true, Verbs: appVerbs()},
			authz.RoleAdministrator: {ID: authz.RoleAdministrator, Name: "administrator", Builtin: true, Verbs: installVerbs()},
		},
	}
}

// appVerbs is the owner's set: the catalog minus the install-scoped verbs.
// Owner is an app role, and an owner of one app administers nothing (R-031).
func appVerbs() []authz.Verb {
	var out []authz.Verb
	for _, v := range authz.Verbs {
		if !authz.InstallScoped(v) {
			out = append(out, v)
		}
	}
	return out
}

func installVerbs() []authz.Verb {
	var out []authz.Verb
	for _, v := range authz.Verbs {
		if authz.InstallScoped(v) {
			out = append(out, v)
		}
	}
	return out
}

func (s *store) UserStatus(_ context.Context, userID string) (string, error) {
	if st, ok := s.userStatus[userID]; ok {
		return st, nil
	}
	return "deleted", nil
}

func (s *store) ControlGrantsFor(_ context.Context, appID string, p authz.Principal) ([]authz.Grant, error) {
	var out []authz.Grant
	for _, g := range s.control[appID] {
		if matches(g.PrincipalKind, g.PrincipalID, p) {
			out = append(out, g)
		}
	}
	return out, nil
}

func (s *store) InstallGrantsFor(_ context.Context, p authz.Principal) ([]authz.Grant, error) {
	var out []authz.Grant
	for _, g := range s.install {
		if matches(g.PrincipalKind, g.PrincipalID, p) {
			out = append(out, g)
		}
	}
	return out, nil
}

func (s *store) IsOwner(_ context.Context, appID, userID string) (bool, error) {
	return userID != "" && s.owner[appID] == userID, nil
}

func (s *store) HasDataGrant(_ context.Context, appID string, p authz.Principal) (bool, error) {
	for _, id := range s.data[appID] {
		if id == p.UserID || id == p.ID {
			return true, nil
		}
		for _, g := range p.Groups {
			if id == g {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *store) HasAnonymousGrant(_ context.Context, appID string) (bool, error) {
	return s.anonymous[appID], nil
}

func (s *store) Role(_ context.Context, roleID string) (authz.Role, error) {
	return s.roles[roleID], nil
}

func matches(kind, id string, p authz.Principal) bool {
	switch kind {
	case "user":
		return id == p.UserID
	case "token":
		return id == p.ID && p.UserID == ""
	case "group":
		for _, g := range p.Groups {
			if g == id {
				return true
			}
		}
	}
	return false
}

type denyingPolicy struct{ verb authz.Verb }

func (d denyingPolicy) Allows(_ context.Context, v authz.Verb, _ string) error {
	if v == d.verb {
		return errs.New(errs.PolicyExecDisabled, "Running commands in apps is turned off for this installation.")
	}
	return nil
}

type recorder struct{ denials int }

func (r *recorder) Denied(context.Context, authz.Principal, string, authz.Verb, errs.Code) {
	r.denials++
}

// --- fixtures --------------------------------------------------------------

const (
	app   = "app_01HQ8"
	alice = "usr_alice"
	bob   = "usr_bob"
)

func activeUser(id string) authz.Principal {
	return authz.Principal{Kind: authz.KindUser, ID: id, UserID: id, Status: "active"}
}

// --- the evaluation order ---------------------------------------------------

// TestR029_ControlPlaneRoleDoesNotGrantDataPlaneUse asserts R-029 and R-070.
//
// This is the test guarding the mistake that was made once already during
// design: an operator can deploy someone else's app but may not *use* it.
// CheckData contains exactly one cross-plane implication — ownership — and if a
// change adds another, this fails.
func TestR029_ControlPlaneRoleDoesNotGrantDataPlaneUse(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[bob] = "active"
	s.owner[app] = alice
	s.control[app] = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: bob, RoleID: authz.RoleOperator}}

	a := authz.New(s, nil, nil)
	operator := activeUser(bob)

	require.NoError(t, a.CheckControl(ctx, operator, app, authz.AppDeploy),
		"an operator should be able to deploy")

	err := a.CheckData(ctx, operator, app)
	require.Error(t, err, "an operator on someone else's app must be denied USE of it")
	require.Equal(t, errs.PermDenied, errs.CodeOf(err))
}

// TestR029_OwnerRoleAloneDoesNotGrantUse asserts that even the owner *role* is
// not the owner *of record*. Only R-031 ownership crosses the planes.
func TestR029_OwnerRoleAloneDoesNotGrantUse(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[bob] = "active"
	s.owner[app] = alice
	s.control[app] = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: bob, RoleID: authz.RoleOwner}}

	a := authz.New(s, nil, nil)
	require.Error(t, a.CheckData(ctx, activeUser(bob), app),
		"holding the owner ROLE is not the same as being the app's owner of record")
}

// TestR072_OwnershipGrantsUse asserts the one permitted implication.
func TestR072_OwnershipGrantsUse(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[alice] = "active"
	s.owner[app] = alice

	require.NoError(t, authz.New(s, nil, nil).CheckData(ctx, activeUser(alice), app))
}

// TestR059_DelegatedTokenIsOrphanedByItsOwnersDeletion asserts R-059, and is
// named in phase 1's Done when.
//
// The check is a live lookup on every request rather than a cascade run at
// revocation time. A cascade means a missed cascade is a permanent security
// hole; a live lookup cannot be missed.
func TestR059_DelegatedTokenIsOrphanedByItsOwnersDeletion(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[alice] = "active"
	s.owner[app] = alice

	token := authz.Principal{Kind: authz.KindToken, ID: "tok_01HQ8", UserID: alice, TokenID: "tok_01HQ8"}
	a := authz.New(s, nil, nil)

	require.NoError(t, a.CheckData(ctx, token, app), "the token acts as its owner while the owner is active")

	// No cascade runs, no grant is rewritten — only the owner's status changes.
	s.userStatus[alice] = "deleted"

	err := a.CheckData(ctx, token, app)
	require.Error(t, err)
	require.Equal(t, errs.AuthTokenOrphaned, errs.CodeOf(err))
}

// TestR049_SuspendedIsNotDeletedButBothDeny asserts R-049: suspension denies
// access without being deletion, because destruction rules fire on one and not
// the other.
func TestR049_SuspendedIsNotDeletedButBothDeny(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.owner[app] = alice
	a := authz.New(s, nil, nil)

	for _, status := range []string{"suspended", "deleted"} {
		s.userStatus[alice] = status
		p := authz.Principal{Kind: authz.KindUser, ID: alice, UserID: alice, Status: status}
		require.Error(t, a.CheckData(ctx, p, app), "a %s user must be denied", status)
	}

	s.userStatus[alice] = "active"
	require.NoError(t, a.CheckData(ctx, activeUser(alice), app))
}

// TestR272_PolicyIsAFloorAndDeniesTheOwnerToo asserts R-272: policy is
// evaluated before grants and cannot be overridden by one.
func TestR272_PolicyIsAFloorAndDeniesTheOwnerToo(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[alice] = "active"
	s.owner[app] = alice
	s.control[app] = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: alice, RoleID: authz.RoleOwner}}

	a := authz.New(s, denyingPolicy{verb: authz.AppExec}, nil)

	err := a.CheckControl(ctx, activeUser(alice), app, authz.AppExec)
	require.Error(t, err, "policy disabling exec install-wide must deny the owner too")
	require.Equal(t, errs.PolicyExecDisabled, errs.CodeOf(err))

	require.NoError(t, a.CheckControl(ctx, activeUser(alice), app, authz.AppDeploy),
		"other verbs are unaffected")
}

// TestR075_AnonymousGrantAllowsUnauthenticatedUse asserts R-075: the anonymous
// grant is a grant, evaluated on the same path as any other.
func TestR075_AnonymousGrantAllowsUnauthenticatedUse(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	a := authz.New(s, nil, nil)

	require.Error(t, a.CheckData(ctx, authz.Anonymous(), app))

	s.anonymous[app] = true
	require.NoError(t, a.CheckData(ctx, authz.Anonymous(), app))
}

// TestR079_GroupMembershipIsResolvedFromThePrincipal asserts that a group grant
// works and that membership arrives on the principal, resolved live per request
// rather than denormalized into the grant.
func TestR079_GroupMembershipIsResolvedFromThePrincipal(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[bob] = "active"
	s.owner[app] = alice
	s.data[app] = []string{"grp_engineering"}
	s.control[app] = []authz.Grant{{Plane: "control", PrincipalKind: "group", PrincipalID: "grp_engineering", RoleID: authz.RoleViewer}}

	a := authz.New(s, nil, nil)

	member := activeUser(bob)
	member.Groups = []string{"grp_engineering"}
	require.NoError(t, a.CheckData(ctx, member, app))
	require.NoError(t, a.CheckControl(ctx, member, app, authz.AppView))

	// Removing the group from the principal is all it takes — no grant changes.
	require.Error(t, a.CheckData(ctx, activeUser(bob), app))
	require.Error(t, a.CheckControl(ctx, activeUser(bob), app, authz.AppView))
}

// TestR060_AccountTokenIsItsOwnPrincipal asserts R-060.
func TestR060_AccountTokenIsItsOwnPrincipal(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.owner[app] = alice
	s.control[app] = []authz.Grant{{Plane: "control", PrincipalKind: "token", PrincipalID: "tok_ci", RoleID: authz.RoleOperator}}
	s.data[app] = []string{"tok_ci"}

	account := authz.Principal{Kind: authz.KindToken, ID: "tok_ci", TokenID: "tok_ci"} // no UserID
	a := authz.New(s, nil, nil)

	require.NoError(t, a.CheckControl(ctx, account, app, authz.AppDeploy))
	require.NoError(t, a.CheckData(ctx, account, app))
	require.Error(t, a.CheckControl(ctx, account, app, authz.AppExec), "operator does not hold exec")
}

// TestR081_BuiltInRoleVerbSets asserts the verb sets from R-080/R-081, including
// that the three *.override verbs are Owner-only.
func TestR081_BuiltInRoleVerbSets(t *testing.T) {
	s := newStore()

	viewer := s.roles[authz.RoleViewer]
	require.True(t, viewer.Has(authz.AppView))
	require.False(t, viewer.Has(authz.AppDeploy))

	operator := s.roles[authz.RoleOperator]
	require.True(t, operator.Has(authz.AppDeploy))
	require.True(t, operator.Has(authz.AppSecretsWrite))
	require.False(t, operator.Has(authz.AppSecretsRead), "R-083: write is separable from read")
	require.False(t, operator.Has(authz.AppExec), "R-084: exec is its own verb")

	for _, v := range []authz.Verb{authz.AppRoutingOverride, authz.AppResourceOverride, authz.AppEgressOverride} {
		require.False(t, operator.Has(v), "%s deviates from a host default and is Owner-only", v)
		require.True(t, s.roles[authz.RoleOwner].Has(v))
	}
}

// TestR082_NoVerbImplicationGraph asserts that holding one verb implies nothing.
// Implication graphs are where authorization bugs live.
func TestR082_NoVerbImplicationGraph(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[bob] = "active"
	s.roles["role_custom"] = authz.Role{ID: "role_custom", Name: "deleter", Verbs: []authz.Verb{authz.AppDelete}}
	s.control[app] = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: bob, RoleID: "role_custom"}}

	a := authz.New(s, nil, nil)
	require.NoError(t, a.CheckControl(ctx, activeUser(bob), app, authz.AppDelete))
	require.Error(t, a.CheckControl(ctx, activeUser(bob), app, authz.AppView),
		"app.delete must not imply app.view")
}

// TestDenialsAreAudited asserts that every denial is recorded, not only
// successes. A denial pattern is the signal that matters for detecting misuse.
func TestDenialsAreAudited(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[bob] = "active"
	s.owner[app] = alice

	rec := &recorder{}
	a := authz.New(s, nil, rec)

	require.Error(t, a.CheckData(ctx, activeUser(bob), app))
	require.Error(t, a.CheckControl(ctx, activeUser(bob), app, authz.AppDeploy))
	require.Equal(t, 2, rec.denials, "both denials should have been audited")

	s.owner[app] = bob
	require.NoError(t, a.CheckData(ctx, activeUser(bob), app))
	require.Equal(t, 2, rec.denials, "a success is not a denial")
}

// TestSystemPrincipalBypassesGrantsButIsStillAPrincipal asserts the reconciler's
// path: grant checks are bypassed, and the audit trail is not.
func TestSystemPrincipalBypassesGrantsButIsStillAPrincipal(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	rec := &recorder{}
	a := authz.New(s, nil, rec)

	require.NoError(t, a.CheckControl(ctx, authz.System(), app, authz.AppRestart))
	require.Equal(t, 0, rec.denials)
}

func TestVerbCatalogIsClosed(t *testing.T) {
	// 13 app verbs plus the six install-scoped ones (O-17). The count is here
	// deliberately: R-080 says the catalog is fixed, so adding a verb should
	// require editing a test rather than only a constant.
	require.Len(t, authz.Verbs, 19)
	require.True(t, authz.IsVerb(authz.AppEgressOverride), "R-184's verb must exist")
	require.False(t, authz.IsVerb(authz.Verb("app.do.anything")))

	var install, app int
	for _, v := range authz.Verbs {
		if authz.InstallScoped(v) {
			install++
		} else {
			app++
		}
	}
	require.Equal(t, 6, install)
	require.Equal(t, 13, app)
}

// TestR080_InstallVerbRequiresAnInstallGrant asserts install-level
// authorization: an ordinary account holds nothing install-wide, and the power
// arrives as a grant rather than as a property of the account.
func TestR080_InstallVerbRequiresAnInstallGrant(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[bob] = "active"
	bobP := authz.Principal{Kind: authz.KindUser, ID: bob, UserID: bob, Status: "active"}

	a := authz.New(s, nil, nil)

	// Signed in, and that is all. This is the state every account was in when
	// six endpoints were gated by "are you signed in" and nothing else.
	require.Error(t, a.CheckInstall(ctx, bobP, authz.InstallUsersManage))
	require.Error(t, a.CheckInstall(ctx, bobP, authz.AppCreate))

	// An owner grant on an app is not administration. Owning every app in the
	// install would still not be.
	s.control[app] = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: bob, RoleID: authz.RoleOwner}}
	require.NoError(t, a.CheckControl(ctx, bobP, app, authz.AppDelete))
	require.Error(t, a.CheckInstall(ctx, bobP, authz.InstallUsersManage),
		"an app role must never satisfy an install verb")

	// The grant with no app is what makes an administrator.
	s.install = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: bob, RoleID: authz.RoleAdministrator}}
	require.NoError(t, a.CheckInstall(ctx, bobP, authz.InstallUsersManage))
	require.NoError(t, a.CheckInstall(ctx, bobP, authz.AppCreate))

	// And it does not reach into any app. R-031's owner of record survives the
	// arrival of an administrator: to manage an app, hold a grant on it.
	require.Error(t, a.CheckControl(ctx, bobP, "app_other", authz.AppView),
		"an administrator is not an owner of every app")
}

// TestR080_ScopesCannotBeCheckedAgainstEachOther asserts the guard at the
// boundary between the two functions.
//
// The asymmetry is the point. An install verb evaluated against an app searches
// for a grant that cannot exist and denies — wrong but safe. An app verb
// evaluated install-wide searches for a grant that *can* exist and could allow.
// Both are refused so that neither call site can be written by accident.
func TestR080_ScopesCannotBeCheckedAgainstEachOther(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[bob] = "active"
	s.install = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: bob, RoleID: authz.RoleAdministrator}}
	bobP := authz.Principal{Kind: authz.KindUser, ID: bob, UserID: bob, Status: "active"}

	a := authz.New(s, nil, nil)

	err := a.CheckControl(ctx, bobP, app, authz.InstallUsersManage)
	require.Error(t, err)
	require.Equal(t, errs.Internal, errs.CodeOf(err))

	err = a.CheckInstall(ctx, bobP, authz.AppExec)
	require.Error(t, err)
	require.Equal(t, errs.Internal, errs.CodeOf(err))
}

// TestR080_AnonymousHoldsNothingInstallWide asserts that the install plane has no
// anonymous path at all — unlike the data plane, where R-075's anonymous grant
// is a real row.
func TestR080_AnonymousHoldsNothingInstallWide(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	a := authz.New(s, nil, nil)

	for _, v := range authz.Verbs {
		if !authz.InstallScoped(v) {
			continue
		}
		require.Error(t, a.CheckInstall(ctx, authz.Anonymous(), v))
	}
}

// TestR049_SuspendedAdministratorHoldsNothing asserts the evaluation order:
// principal status is checked before grants, so suspension takes effect without
// touching the grant.
func TestR049_SuspendedAdministratorHoldsNothing(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.install = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: bob, RoleID: authz.RoleAdministrator}}

	a := authz.New(s, nil, nil)
	suspended := authz.Principal{Kind: authz.KindUser, ID: bob, UserID: bob, Status: "suspended"}

	err := a.CheckInstall(ctx, suspended, authz.InstallUsersManage)
	require.Error(t, err)
	require.Equal(t, errs.AuthInvalid, errs.CodeOf(err), "status is step 2, before grants")
}

// TestR272_HostPolicyIsAFloorForAdministratorsToo asserts R-272 on the install
// plane: policy is evaluated before grants and denies the holder.
func TestR272_HostPolicyIsAFloorForAdministratorsToo(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[bob] = "active"
	s.install = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: bob, RoleID: authz.RoleAdministrator}}
	bobP := authz.Principal{Kind: authz.KindUser, ID: bob, UserID: bob, Status: "active"}

	a := authz.New(s, denyingPolicy{verb: authz.InstallPolicyManage}, nil)

	require.NoError(t, a.CheckInstall(ctx, bobP, authz.InstallView))
	require.Error(t, a.CheckInstall(ctx, bobP, authz.InstallPolicyManage),
		"host policy is a floor, not an override — it denies the administrator too")
}

// TestR080_DeniedInstallChecksAreAudited asserts that denial on the install plane
// is recorded like denial on any other. A denial pattern is the signal that
// matters for detecting misuse.
func TestR080_DeniedInstallChecksAreAudited(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[bob] = "active"
	rec := &recorder{}
	a := authz.New(s, nil, rec)

	require.Error(t, a.CheckInstall(ctx,
		authz.Principal{Kind: authz.KindUser, ID: bob, UserID: bob, Status: "active"},
		authz.InstallUsersManage))
	require.Equal(t, 1, rec.denials)
}

// TestR060_AccountTokenHoldsItsOwnInstallGrant asserts R-060: an account token is
// its own principal and is looked up under its own ID, install-wide as per app.
func TestR060_AccountTokenHoldsItsOwnInstallGrant(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.install = []authz.Grant{{Plane: "control", PrincipalKind: "token", PrincipalID: "tok_1", RoleID: authz.RoleAdministrator}}

	a := authz.New(s, nil, nil)
	account := authz.Principal{Kind: authz.KindToken, ID: "tok_1", TokenID: "tok_1"}
	require.NoError(t, a.CheckInstall(ctx, account, authz.InstallView))

	other := authz.Principal{Kind: authz.KindToken, ID: "tok_2", TokenID: "tok_2"}
	require.Error(t, a.CheckInstall(ctx, other, authz.InstallView))
}
