package spec_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bemeek-io/pando/internal/core/spec"
)

// installDefaults is a filled-in Defaults, so a test can assert that a field
// was inherited rather than that it happened to be zero on both sides.
func installDefaults() spec.Defaults {
	d := spec.StandardDefaults()
	d.RoutingAdapter = "rte_loopback"
	d.RuntimeAdapter = "rt_docker_local"
	d.BuilderAdapter = "bld_buildkit"
	d.RoutingMode = spec.RoutingSubdomain
	d.BaseDomain = "apps.example"
	d.BuildIsolation = spec.IsolationVM
	d.RuntimeIsolation = spec.IsolationContainer
	return d
}

// R-152 / R-211 / R-223 / R-240: the [P] values from the requirements.
func TestStandardDefaultsAreTheProposedValues(t *testing.T) {
	d := spec.StandardDefaults()

	require.Equal(t, spec.RoutingPath, d.RoutingMode)
	require.Equal(t, 1800, d.BuildTimeoutSeconds)

	require.Equal(t, 1000, d.Resources.CPUMillis, "R-240")
	require.Equal(t, int64(512<<20), d.Resources.MemoryBytes)
	require.Equal(t, int64(10<<30), d.Resources.DiskBytes)

	require.Equal(t, 10, d.Retention.SpecRevisions, "R-152: ten revisions to roll back through")
	require.Equal(t, 7, d.Retention.BackupDailyCount, "R-211")
	require.Equal(t, int64(100<<20), d.Retention.LogBytes, "R-223")

	// Adapters are not invented. PLAN_ADAPTER_NOT_CONFIGURED says it better
	// than anything a default could guess.
	require.Empty(t, d.RoutingAdapter)
	require.Empty(t, d.RuntimeAdapter)
	require.Empty(t, d.BuilderAdapter)
}

func TestApplyFillsAnEmptySpecCompletely(t *testing.T) {
	var s spec.AppSpec
	installDefaults().Apply(&s, "notes")

	require.Equal(t, "rte_loopback", s.Routing.AdapterRef)
	require.Equal(t, "rt_docker_local", s.Runtime.AdapterRef)
	require.Equal(t, "bld_buildkit", s.Build.AdapterRef)
	require.Equal(t, spec.RoutingSubdomain, s.Routing.Mode)
	require.Equal(t, "notes.apps.example", s.Routing.Hostname)
	require.Equal(t, spec.IsolationVM, s.Build.IsolationFloor)
	require.Equal(t, spec.IsolationContainer, s.Runtime.IsolationFloor)
	require.Equal(t, 1800, s.Build.TimeoutSeconds)
	require.Equal(t, 1000, s.Resources.CPUMillis)
	require.Equal(t, 10, s.Retention.SpecRevisions)
}

// Apply only ever fills gaps: it runs on every revision, not only the first, so
// it must be safe over a spec someone has edited.
func TestApplyNeverOverwritesWhatTheSpecAlreadySays(t *testing.T) {
	s := spec.AppSpec{
		Routing: spec.Routing{
			AdapterRef: "rte_traefik", Mode: spec.RoutingPath,
			PathPrefix: "/chosen", ModeSource: spec.ModeFromUserOverride,
		},
		Runtime: spec.RuntimeRef{AdapterRef: "rt_other", IsolationFloor: spec.IsolationVM},
		Build: spec.Build{
			AdapterRef: "bld_other", IsolationFloor: spec.IsolationVM,
			TimeoutSeconds: 60, EgressMode: spec.EgressAllowlist,
		},
		Deploy:    spec.Deploy{Strategy: spec.DeployStartThenSwap},
		Resources: spec.Resources{CPUMillis: 250, MemoryBytes: 64 << 20, DiskBytes: 1 << 30},
		Egress:    spec.Egress{Mode: spec.EgressAllowlist},
		Retention: spec.Retention{SpecRevisions: 3, BackupDailyCount: 2, LogBytes: 1 << 20},
	}
	before := s

	installDefaults().Apply(&s, "notes")
	require.Equal(t, before, s, "a fully specified spec comes back unchanged")
}

// R-163: the console shows whether a mode was inherited or chosen, which
// matters when host policy later restricts who may override.
func TestR163_AnInheritedRoutingModeIsMarkedAsInherited(t *testing.T) {
	var inherited spec.AppSpec
	installDefaults().Apply(&inherited, "notes")
	require.Equal(t, spec.ModeFromAdapterDefault, inherited.Routing.ModeSource)

	chosen := spec.AppSpec{Routing: spec.Routing{Mode: spec.RoutingPath, ModeSource: spec.ModeFromUserOverride}}
	installDefaults().Apply(&chosen, "notes")
	require.Equal(t, spec.ModeFromUserOverride, chosen.Routing.ModeSource, "a chosen mode stays chosen")
}

func TestPathModeDerivesThePrefixFromTheSlug(t *testing.T) {
	d := installDefaults()
	d.RoutingMode = spec.RoutingPath

	var s spec.AppSpec
	d.Apply(&s, "notes")
	require.Equal(t, "/notes", s.Routing.PathPrefix)

	// A slug that already leads with a slash must not become "//notes".
	var slashed spec.AppSpec
	d.Apply(&slashed, "/notes")
	require.Equal(t, "/notes", slashed.Routing.PathPrefix)

	// Nothing to derive one from.
	var unnamed spec.AppSpec
	d.Apply(&unnamed, "")
	require.Empty(t, unnamed.Routing.PathPrefix)
}

func TestSubdomainModeNeedsABaseDomainToDeriveAHostname(t *testing.T) {
	d := installDefaults()
	d.BaseDomain = ""

	var s spec.AppSpec
	d.Apply(&s, "notes")
	require.Empty(t, s.Routing.Hostname, "no base domain means no hostname is invented")
}

// A port is allocated from the install's range rather than derived from a name:
// two apps called different things still collide if both are handed 9000.
func TestPortModeLeavesThePortToBeAllocated(t *testing.T) {
	d := installDefaults()
	d.RoutingMode = spec.RoutingPort

	var s spec.AppSpec
	d.Apply(&s, "notes")

	require.Equal(t, spec.RoutingPort, s.Routing.Mode)
	require.Zero(t, s.Routing.Port)
	require.Empty(t, s.Routing.Hostname)
	require.Empty(t, s.Routing.PathPrefix)
}

// A prebuilt image is not built, so it needs no builder. Defaulting one in
// would make the planner demand a builder this install may not have, for a
// deploy that never touches it.
func TestAPrebuiltImageIsNotGivenABuilder(t *testing.T) {
	s := spec.AppSpec{Build: spec.Build{Strategy: spec.BuildPrebuilt}}
	installDefaults().Apply(&s, "notes")

	require.Empty(t, s.Build.AdapterRef)
	require.Equal(t, spec.IsolationVM, s.Build.IsolationFloor, "the floor still applies")

	built := spec.AppSpec{Build: spec.Build{Strategy: spec.BuildDockerfile}}
	installDefaults().Apply(&built, "notes")
	require.Equal(t, "bld_buildkit", built.Build.AdapterRef)
}

// R-144: recreate. Start-then-swap is opt-in (R-145) and needs a capability the
// runtime may not have, so it is never the default.
func TestR144_TheDefaultDeployStrategyIsRecreate(t *testing.T) {
	var s spec.AppSpec
	installDefaults().Apply(&s, "notes")

	require.Equal(t, spec.DeployRecreate, s.Deploy.Strategy)
	require.False(t, s.Deploy.AutoDeploy.Enabled, "R-141: off until asked for")
	require.False(t, s.Deploy.AutoRollback, "R-147: off until asked for")
}

// R-270: Pando ships permissive and is narrowed deliberately. A build fetches
// dependencies, and R-118 is what lets an install restrict that.
func TestR270_BuildEgressStartsPermissiveAndAppEgressInherits(t *testing.T) {
	var s spec.AppSpec
	installDefaults().Apply(&s, "notes")

	require.Equal(t, spec.EgressAllowAll, s.Build.EgressMode)

	// R-182: inherit means the install-wide list applies. An app-level
	// allowlist replaces it rather than narrowing it, so nothing here writes one.
	require.Equal(t, spec.EgressInherit, s.Egress.Mode)
	require.Empty(t, s.Egress.Allowlist)
}

func TestApplyOfANilSpecDoesNothingRatherThanPanicking(t *testing.T) {
	require.NotPanics(t, func() { installDefaults().Apply(nil, "notes") })
}

// An install with nothing configured fills in nothing, and the planner's
// message about a missing adapter is what the user sees.
func TestEmptyDefaultsLeaveTheSpecSayingWhatItSaid(t *testing.T) {
	var s spec.AppSpec
	spec.Defaults{}.Apply(&s, "notes")

	require.Empty(t, s.Routing.AdapterRef)
	require.Empty(t, s.Runtime.AdapterRef)
	require.Empty(t, s.Build.AdapterRef)
	require.Zero(t, s.Resources.CPUMillis)

	// The unconditional ones still apply: these are decisions, not inheritance.
	require.Equal(t, spec.DeployRecreate, s.Deploy.Strategy)
	require.Equal(t, spec.EgressAllowAll, s.Build.EgressMode)
	require.Equal(t, spec.EgressInherit, s.Egress.Mode)
}
