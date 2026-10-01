package approval

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/trypando/pando/internal/core/authz"
	"github.com/trypando/pando/internal/core/policy"
	"github.com/trypando/pando/internal/core/spec"
	"github.com/trypando/pando/internal/errs"
)

// fakeAuthz answers each way of being allowed separately, and counts the
// audited checks: CheckControl is the only one that writes a denial.
type fakeAuthz struct {
	install, app bool
	audited      int
}

func (f *fakeAuthz) CheckControl(_ context.Context, _ authz.Principal, _ string, verb authz.Verb) error {
	f.audited++
	if f.app {
		return nil
	}
	return errs.Newf(errs.PermVerbRequired, "You do not have permission to do this. It requires %s on this app.", verb)
}

func (f *fakeAuthz) Allows(_ context.Context, _ authz.Principal, _ string, verb authz.Verb) (bool, error) {
	return verb == authz.AppDeployApprove && f.app, nil
}

func (f *fakeAuthz) AllowsInstall(_ context.Context, _ authz.Principal, verb authz.Verb) (bool, error) {
	return verb == authz.InstallDeploysApprove && f.install, nil
}

var person = authz.Principal{Kind: authz.KindUser, ID: "usr_1", UserID: "usr_1", Status: "active"}

// TestR155_EitherVerbDecidesAndOnlyARealDenialIsAudited asserts the verb rule,
// and that being allowed by the second verb leaves no denial of the first in
// the audit log.
func TestR155_EitherVerbDecidesAndOnlyARealDenialIsAudited(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name         string
		install, app bool
		allowed      bool
		audited      int
	}{
		{"install.deploys.approve", true, false, true, 0},
		{"app.deploy.approve", false, true, true, 0},
		{"neither", false, false, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeAuthz{install: tc.install, app: tc.app}
			s := &Service{Authz: f}

			err := s.mayDecide(ctx, person, "app_1")
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Equal(t, errs.PermVerbRequired, errs.CodeOf(err))
				require.Contains(t, err.Error(), "install.deploys.approve")
				require.Contains(t, err.Error(), "app.deploy.approve")
			}
			require.Equal(t, tc.audited, f.audited, "a denial is audited once, and only when it happened")
		})
	}

	ok, err := (&Service{Authz: &fakeAuthz{install: true}}).CanDecide(ctx, authz.Anonymous(), "app_1")
	require.NoError(t, err)
	require.False(t, ok, "nobody signed in decides anything")
}

// TestR156_OnlyAVerdictOnTheRevisionFailsARequest asserts which plan failures
// end a request and which leave it waiting.
func TestR156_OnlyAVerdictOnTheRevisionFailsARequest(t *testing.T) {
	for _, code := range []errs.Code{errs.PlanSlotUnfilled, errs.PolicySourceNotAllowed, errs.ValidInvalid,
		errs.CapacityWouldOversubscribe, errs.PlanEgressLooseningForbidden} {
		require.True(t, answersTheRevision(errs.New(code, "x")), code)
	}
	for _, code := range []errs.Code{errs.AdapterUnavailable, errs.Internal, errs.StateInvalid} {
		require.False(t, answersTheRevision(errs.New(code, "x")), code)
	}
}

// TestR158_ApprovalPausesAutoDeployOnlyForAnAppThatAutoDeploys asserts what
// the app's status reports.
func TestR158_ApprovalPausesAutoDeployOnlyForAnAppThatAutoDeploys(t *testing.T) {
	ctx := context.Background()
	s := &Service{Policy: staticPolicy{policy.Document{DeployApprovalApps: []string{"app_1"}}}}

	tracking := &spec.AppSpec{Deploy: spec.Deploy{AutoDeploy: spec.AutoDeploy{Enabled: true}}}
	paused, err := s.AutoDeployPaused(ctx, "app_1", tracking)
	require.NoError(t, err)
	require.True(t, paused)

	paused, err = s.AutoDeployPaused(ctx, "app_2", tracking)
	require.NoError(t, err)
	require.False(t, paused)

	paused, err = s.AutoDeployPaused(ctx, "app_1", &spec.AppSpec{})
	require.NoError(t, err)
	require.False(t, paused, "nothing to pause")
}

type staticPolicy struct{ doc policy.Document }

func (p staticPolicy) Load(context.Context) (policy.Document, error) { return p.doc, nil }
