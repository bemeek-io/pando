package authz_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/authz"
)

var errStore = errors.New("the database went away")

// failing is a store that fails one kind of lookup, to show that a failure to
// read grants or roles is a failure and never an answer — neither an allow
// nor a denial that gets audited as one.
type failing struct {
	*store
	control, install bool
	role             string
}

func (f failing) ControlGrantsFor(ctx context.Context, appID string, p authz.Principal) ([]authz.Grant, error) {
	if f.control {
		return nil, errStore
	}
	return f.store.ControlGrantsFor(ctx, appID, p)
}

func (f failing) InstallGrantsFor(ctx context.Context, p authz.Principal) ([]authz.Grant, error) {
	if f.install {
		return nil, errStore
	}
	return f.store.InstallGrantsFor(ctx, p)
}

func (f failing) Role(ctx context.Context, roleID string) (authz.Role, error) {
	if roleID == f.role {
		return authz.Role{}, errStore
	}
	return f.store.Role(ctx, roleID)
}

// TestAStoreFailureIsNeverAnAnswer asserts that every grant and role lookup
// CheckControl makes, the install-wide ones of step 6b included (issue #81),
// fails closed: the error comes back as itself, nothing is allowed, and
// nothing is audited as a denial or as access through an install grant.
func TestAStoreFailureIsNeverAnAnswer(t *testing.T) {
	ctx := context.Background()
	bobP := activeUser(bob)

	cases := map[string]func(s *store) failing{
		"app grants": func(s *store) failing { return failing{store: s, control: true} },
		"an app role": func(s *store) failing {
			s.control[app] = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: bob, RoleID: authz.RoleViewer}}
			return failing{store: s, role: authz.RoleViewer}
		},
		"install grants": func(s *store) failing { return failing{store: s, install: true} },
		"an install role": func(s *store) failing {
			s.install = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: bob, RoleID: authz.RoleAppManager}}
			return failing{store: s, role: authz.RoleAppManager}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			rec := &recorder{}
			a := authz.New(build(newStore()), nil, rec)

			require.ErrorIs(t, a.CheckControl(ctx, bobP, app, authz.AppDeploy), errStore)
			_, err := a.AppVerbs(ctx, bobP, app)
			require.ErrorIs(t, err, errStore)
			_, err = a.Allows(ctx, bobP, app, authz.AppDeploy)
			require.ErrorIs(t, err, errStore)
			require.ErrorIs(t, a.PreviewControl(ctx, bobP, app, authz.AppDeploy), errStore)

			require.Zero(t, rec.denials, "a failure is not a denial")
			require.Empty(t, rec.through)
		})
	}
}

// TestAnInactivePrincipalReachesNoAppThroughAnInstallGrant asserts that steps
// 1–4 come before step 6b: a suspended administrator holds every
// install.apps.* verb and still reaches no app, and the refusal is audited.
func TestAnInactivePrincipalReachesNoAppThroughAnInstallGrant(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.install = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: bob, RoleID: authz.RoleAdministrator}}
	rec := &recorder{}
	a := authz.New(s, nil, rec)

	suspended := authz.Principal{Kind: authz.KindUser, ID: bob, UserID: bob, Status: "suspended"}
	require.Error(t, a.CheckControl(ctx, suspended, app, authz.AppDeploy))
	require.Equal(t, 1, rec.denials)
	require.Empty(t, rec.through)
}

// TestOnlyAppVerbsHaveAnInstallCounterpart asserts the other half of the
// table: an install verb has no counterpart of its own, so nothing stands for
// an install verb on every app.
func TestOnlyAppVerbsHaveAnInstallCounterpart(t *testing.T) {
	for _, v := range authz.Verbs {
		if !authz.InstallScoped(v) {
			continue
		}
		_, ok := authz.InstallCounterpart(v)
		require.False(t, ok, v)
	}
}
