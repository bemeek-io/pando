package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/authz"
)

// TestR155_AllowsInstallAnswersWithoutAuditingADenial asserts the check deploy
// approval makes first: install.deploys.approve is one of two verbs that
// approve, so not holding it is no denial when the other allows — and must not
// appear in the audit log as one.
func TestR155_AllowsInstallAnswersWithoutAuditingADenial(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[alice] = "active"
	s.userStatus[bob] = "active"
	s.install = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: bob, RoleID: authz.RoleAdministrator}}
	rec := &recorder{}
	a := authz.New(s, nil, rec)

	aliceP := authz.Principal{Kind: authz.KindUser, ID: alice, UserID: alice, Status: "active"}
	bobP := authz.Principal{Kind: authz.KindUser, ID: bob, UserID: bob, Status: "active"}

	ok, err := a.AllowsInstall(ctx, aliceP, authz.InstallDeploysApprove)
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = a.AllowsInstall(ctx, bobP, authz.InstallDeploysApprove)
	require.NoError(t, err)
	require.True(t, ok, "the Administrator role holds install.deploys.approve")
	require.Equal(t, 0, rec.denials, "looking is not trying")

	// Policy still runs first, as it does in CheckInstall (R-272).
	agent := authz.Principal{Kind: authz.KindToken, ID: "tok_1", UserID: bob, Status: "active"}
	a = authz.New(s, denyingPolicy{verb: authz.InstallDeploysApprove, agentsOnly: true}, rec)
	ok, err = a.AllowsInstall(ctx, agent, authz.InstallDeploysApprove)
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, 0, rec.denials)

	_, err = a.AllowsInstall(ctx, bobP, authz.AppDeployApprove)
	require.Error(t, err, "an app verb is refused install-wide (R-080)")
}

// TestR155_ManagingEveryAppIsNotApprovingItsDeploys asserts that App manager,
// which holds every other app verb's install-wide counterpart, does not
// approve deploys: install.deploys.approve is that verb's, and the role
// leaves it out as Owner leaves out app.deploy.approve.
func TestR155_ManagingEveryAppIsNotApprovingItsDeploys(t *testing.T) {
	ctx := context.Background()
	s := newStore()
	s.userStatus[bob] = "active"
	s.install = []authz.Grant{{Plane: "control", PrincipalKind: "user", PrincipalID: bob, RoleID: authz.RoleAppManager}}
	a := authz.New(s, nil, nil)
	bobP := authz.Principal{Kind: authz.KindUser, ID: bob, UserID: bob, Status: "active"}

	require.NoError(t, a.CheckControl(ctx, bobP, app, authz.AppDeploy))
	ok, err := a.Allows(ctx, bobP, app, authz.AppDeployApprove)
	require.NoError(t, err)
	require.False(t, ok)
}
