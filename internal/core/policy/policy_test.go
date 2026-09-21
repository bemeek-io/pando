package policy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/authz"
	"github.com/bemeek-io/pando/internal/core/policy"
	"github.com/bemeek-io/pando/internal/core/spec"
	"github.com/bemeek-io/pando/internal/errs"
)

var (
	person = authz.Principal{Kind: authz.KindUser, ID: "usr_01HQ8"}
	agent  = authz.Principal{Kind: authz.KindToken, ID: "tok_01HQ8"}
)

func ctx() context.Context { return context.Background() }

// R-270: Pando ships permissive defaults.
func TestR270_TheDefaultDocumentIsPermissive(t *testing.T) {
	d := policy.Default()

	require.Empty(t, d.SourceAllowlist, "apps may be created from anywhere")
	require.Empty(t, d.DisabledVerbs, "nothing is turned off install-wide")
	require.NotNil(t, d.AllowAnonymousGrants)
	require.True(t, *d.AllowAnonymousGrants)
	require.Equal(t, spec.IsolationContainer, d.MinBuildIsolation)
	require.Equal(t, spec.IsolationContainer, d.MinRuntimeIsolation)
	require.Zero(t, d.MaxLogDiskBytes)
	require.Zero(t, d.MaxTokenLifetimeDays)
	require.False(t, d.RequireBackupBeforeDestroy)
}

// O-12's resolution: the exclusions are policy, not a list of tools the MCP
// server declines to expose. An agent holding a token can call the REST API
// directly, so an MCP-layer exclusion is a speed bump rather than a boundary.
func TestO12_AgentExclusionsShipAsPolicyAndApplyToEverySurface(t *testing.T) {
	e := policy.Static(policy.Default())

	for _, verb := range []authz.Verb{
		authz.AppExec, authz.AppSecretsRead, authz.AppGrantsManage,
		authz.InstallPolicyManage, authz.InstallUsersManage, authz.InstallBackupManage,
	} {
		require.Error(t, e.Allows(ctx(), agent, verb, "app_01HQ8"), "%s", verb)
		require.NoError(t, e.Allows(ctx(), person, verb, "app_01HQ8"),
			"%s is denied to agents only, not to the person", verb)
	}
}

// A person told "this is turned off for the installation" when it is only
// turned off for their agent would go looking in the wrong place.
func TestTheAgentRuleProducesTheMoreSpecificMessage(t *testing.T) {
	e := policy.Static(policy.Document{
		AgentDisabledVerbs: []string{string(authz.AppExec)},
		DisabledVerbs:      []string{string(authz.AppExec)},
	})

	err := e.Allows(ctx(), agent, authz.AppExec, "app_01HQ8")
	require.Equal(t, errs.PolicyExecDisabled, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Message, "Tokens and agents")
	require.Contains(t, errs.As(err).Remedy, "signed in")
	require.Equal(t, string(authz.AppExec), errs.As(err).Details["verb"])
}

// R-085: host policy may disable exec install-wide. It denies the owner too —
// policy is a floor evaluated before grants, not something a grant outranks.
func TestR085_ExecDisabledInstallWideDeniesEveryone(t *testing.T) {
	e := policy.Static(policy.Document{DisabledVerbs: []string{string(authz.AppExec)}})

	for _, p := range []authz.Principal{person, agent} {
		err := e.Allows(ctx(), p, authz.AppExec, "app_01HQ8")
		require.Equal(t, errs.PolicyExecDisabled, errs.CodeOf(err))
		require.Contains(t, errs.As(err).Message, "Running commands inside apps is turned off")
		require.NotEmpty(t, errs.As(err).Remedy)
	}
}

// exec has its own message because the console explains that one specifically:
// it is a deliberate posture, not a permissions mistake to escalate.
func TestAnyOtherDisabledVerbGetsTheGeneralMessage(t *testing.T) {
	e := policy.Static(policy.Document{DisabledVerbs: []string{string(authz.AppSecretsRead)}})

	err := e.Allows(ctx(), person, authz.AppSecretsRead, "app_01HQ8")
	require.Equal(t, errs.PolicyExecDisabled, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Message, "This action is turned off")
	require.Equal(t, string(authz.AppSecretsRead), errs.As(err).Details["verb"])
}

// R-272: policy can only deny. Nothing here grants anything.
func TestR272_AVerbNoRuleMentionsIsUntouched(t *testing.T) {
	e := policy.Static(policy.Document{DisabledVerbs: []string{string(authz.AppExec)}})
	require.NoError(t, e.Allows(ctx(), person, authz.AppDeploy, "app_01HQ8"))
	require.NoError(t, policy.Static(policy.Document{}).Allows(ctx(), agent, authz.AppExec, "app_01HQ8"),
		"an empty document denies nothing, including to agents")
}

// R-274: policy applies to a running install, so the document is loaded per
// evaluation. A cached one would keep denying, or keep allowing, for however
// long the cache lived.
func TestR274_TheDocumentIsReloadedForEveryEvaluation(t *testing.T) {
	loads := 0
	disabled := false
	e := policy.New(func(context.Context) (policy.Document, error) {
		loads++
		if disabled {
			return policy.Document{DisabledVerbs: []string{string(authz.AppExec)}}, nil
		}
		return policy.Document{}, nil
	})

	require.NoError(t, e.Allows(ctx(), person, authz.AppExec, "app_01HQ8"))
	disabled = true
	require.Error(t, e.Allows(ctx(), person, authz.AppExec, "app_01HQ8"),
		"the next call sees the new document, with no cache to wait out")
	require.Equal(t, 2, loads)
}

func TestAPolicyThatCannotBeLoadedFailsClosedRatherThanSilently(t *testing.T) {
	boom := errors.New("the host_policy row is unreadable")
	e := policy.New(func(context.Context) (policy.Document, error) { return policy.Document{}, boom })

	require.ErrorIs(t, e.Allows(ctx(), person, authz.AppExec, "app_01HQ8"), boom)
	require.ErrorIs(t, e.AllowsSource(ctx(), "https://github.com/ben/notes"), boom)
	require.ErrorIs(t, e.AllowsAnonymousGrant(ctx()), boom)

	_, _, err := e.IsolationFloors(ctx())
	require.ErrorIs(t, err, boom)

	_, err = e.Document(ctx())
	require.ErrorIs(t, err, boom)
}

// R-092: the allowlist is checked before any clone, so a blocked source
// produces zero disk writes.
func TestR092_TheSourceAllowlistAcceptsOnlyApprovedHosts(t *testing.T) {
	e := policy.Static(policy.Document{SourceAllowlist: []string{"github.com", ".corp.example"}})

	for _, allowed := range []string{
		"https://github.com/ben/notes.git",
		"https://GitHub.com/ben/notes",
		"git@github.com:ben/notes.git",
		"https://git.corp.example/team/app",
		"https://corp.example/team/app",
		"ssh://git@deep.git.corp.example/x",
	} {
		require.NoError(t, e.AllowsSource(ctx(), allowed), allowed)
	}

	for _, blocked := range []string{
		"https://gitlab.com/ben/notes",
		"https://notgithub.com/ben/notes",
		"https://evil.example/x",
		"git@gitlab.com:ben/notes.git",
		// A suffix match must not be a substring match: this is not corp.example.
		"https://corp.example.evil.com/x",
	} {
		err := e.AllowsSource(ctx(), blocked)
		require.Equal(t, errs.PolicySourceNotAllowed, errs.CodeOf(err), blocked)
	}
}

func TestABlockedSourceSaysWhichHostAndWhatIsAllowed(t *testing.T) {
	e := policy.Static(policy.Document{SourceAllowlist: []string{"github.com"}})

	err := e.AllowsSource(ctx(), "https://gitlab.com/ben/notes")
	require.Contains(t, errs.As(err).Message, "gitlab.com")
	require.Equal(t, "https://gitlab.com/ben/notes", errs.As(err).Details["source"])
	require.Equal(t, []string{"github.com"}, errs.As(err).Details["allowed"])
	require.NotEmpty(t, errs.As(err).Remedy)
}

// A source with no parseable host still has to be refused, and the message
// cannot name a host it does not have.
func TestASourceWithNoHostIsRefusedReadably(t *testing.T) {
	e := policy.Static(policy.Document{SourceAllowlist: []string{"github.com"}})

	err := e.AllowsSource(ctx(), "/srv/local/repo")
	require.Equal(t, errs.PolicySourceNotAllowed, errs.CodeOf(err))
	require.Contains(t, errs.As(err).Message, "that address")
}

func TestAnEmptyAllowlistRestrictsNothing(t *testing.T) {
	e := policy.Static(policy.Document{})
	require.NoError(t, e.AllowsSource(ctx(), "https://anywhere.example/x"))

	// An empty URL is not the allowlist's business — there is nothing to check
	// yet, and an upload has no URL at all.
	require.NoError(t, policy.Static(policy.Document{SourceAllowlist: []string{"github.com"}}).
		AllowsSource(ctx(), ""))
}

// R-076: an app may be shared with everyone unless policy says otherwise.
func TestR076_AnonymousGrantsAreAllowedUnlessExplicitlyForbidden(t *testing.T) {
	require.NoError(t, policy.Static(policy.Default()).AllowsAnonymousGrant(ctx()))

	// Unset is not the same as false: a partially written document must not
	// start denying something nobody turned off.
	require.NoError(t, policy.Static(policy.Document{}).AllowsAnonymousGrant(ctx()))

	forbidden := false
	err := policy.Static(policy.Document{AllowAnonymousGrants: &forbidden}).AllowsAnonymousGrant(ctx())
	require.Equal(t, errs.PolicyAnonymousGrantForbidden, errs.CodeOf(err))
	require.NotEmpty(t, errs.As(err).Remedy)

	allowed := true
	require.NoError(t, policy.Static(policy.Document{AllowAnonymousGrants: &allowed}).AllowsAnonymousGrant(ctx()))
}

// R-024 and R-114: two floors, because how isolated a build must be is a
// different question from how isolated the running app must be.
func TestR114_BuildAndRuntimeFloorsAreReportedSeparately(t *testing.T) {
	build, runtime, err := policy.Static(policy.Document{
		MinBuildIsolation:   spec.IsolationVM,
		MinRuntimeIsolation: spec.IsolationContainer,
	}).IsolationFloors(ctx())

	require.NoError(t, err)
	require.Equal(t, spec.IsolationVM, build)
	require.Equal(t, spec.IsolationContainer, runtime)
	require.NotEqual(t, build, runtime)
}

func TestDocumentReturnsWhatWasLoaded(t *testing.T) {
	want := policy.Document{MaxLogDiskBytes: 1 << 30, MaxTokenLifetimeDays: 90}
	got, err := policy.Static(want).Document(ctx())
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// The evaluator is the authz.Policy the authorizer calls. If that stops being
// true, the wiring breaks somewhere less obvious than here.
func TestTheEvaluatorIsTheAuthorizersPolicy(t *testing.T) {
	var _ authz.Policy = policy.Static(policy.Default())
}

// R-336: host policy may forbid AI screening install-wide. It ships allowed
// (R-270), an unreadable document denies — the direction that sends nothing
// anywhere — and a reason, not an error, is what comes back.
func TestR336_HostPolicyCanForbidAIScreening(t *testing.T) {
	allowed := policy.New(func(context.Context) (policy.Document, error) { return policy.Default(), nil })
	require.Empty(t, allowed.AllowsScreening(ctx()))

	off := policy.New(func(context.Context) (policy.Document, error) {
		return policy.Document{DisableAIScreening: true}, nil
	})
	require.Contains(t, off.AllowsScreening(ctx()), "administrator")

	broken := policy.New(func(context.Context) (policy.Document, error) {
		return policy.Document{}, errors.New("db down")
	})
	require.NotEmpty(t, broken.AllowsScreening(ctx()))
}
