package spec

import "strings"

// Defaults are the install's answers to everything a repository cannot say
// about itself.
//
// R-104 draws the line this implements: "Questions are blockers; everything
// else is configuration. Anything with a reasonable default gets the default
// and is changeable later in settings." A repository can say it builds from a
// Dockerfile and listens on 3000. It cannot say which runtime adapter this
// install uses, how apps here are addressed, or what a memory limit should be —
// and none of those is a question worth putting to someone R-005 says may not
// know what a port is.
//
// This lives in spec rather than in detect because detection is not the only
// producer of an incomplete spec. An imported one (R-152, OriginImported) has
// exactly the same gap, and so does a hand-written one that omits a section.
//
// Plain values, no adapter types: api imports spec, so spec cannot import api.
// Whoever holds the registry resolves the refs and passes them in.
type Defaults struct {
	// Adapters. Empty means the install has none of that category configured,
	// and the planner's PLAN_ADAPTER_NOT_CONFIGURED says so far better than
	// anything this could invent.
	RoutingAdapter string
	RuntimeAdapter string
	BuilderAdapter string

	// RoutingMode is the routing adapter's declared default (R-162): adding an
	// app uses it without asking.
	RoutingMode RoutingMode

	// BaseDomain is what a subdomain is carved out of. Only consulted in
	// subdomain mode.
	BaseDomain string

	// Isolation floors from host policy (R-024, R-114).
	BuildIsolation   IsolationClass
	RuntimeIsolation IsolationClass

	BuildTimeoutSeconds int

	// Resources are the host defaults every new app inherits (R-240).
	Resources Resources

	Retention Retention
}

// Apply fills in every field the spec leaves empty.
//
// It only ever fills gaps. A spec that already says how it is routed keeps
// saying it — this must be safe to run over a spec someone has edited, because
// it runs on every revision and not only the first.
//
// slug names the app in an address. It is passed rather than read off the spec
// because a spec has an AppID and an AppID is not something to put in a URL.
func (d Defaults) Apply(s *AppSpec, slug string) {
	if s == nil {
		return
	}

	d.applyRouting(s, slug)
	d.applyRuntime(s)
	d.applyBuild(s)
	d.applyDeploy(s)
	d.applyLimits(s)
}

func (d Defaults) applyRouting(s *AppSpec, slug string) {
	if s.Routing.AdapterRef == "" {
		s.Routing.AdapterRef = d.RoutingAdapter
	}

	if s.Routing.Mode == "" {
		s.Routing.Mode = d.RoutingMode

		// R-163: the console shows whether a mode was inherited or chosen,
		// which matters when host policy later restricts who may override.
		// Anything filled in here was inherited by definition.
		s.Routing.ModeSource = ModeFromAdapterDefault
	}

	switch s.Routing.Mode {
	case RoutingSubdomain:
		if s.Routing.Hostname == "" && d.BaseDomain != "" {
			s.Routing.Hostname = slug + "." + d.BaseDomain
		}
	case RoutingPath:
		if s.Routing.PathPrefix == "" && slug != "" {
			s.Routing.PathPrefix = "/" + strings.TrimPrefix(slug, "/")
		}
	case RoutingPort:
		// Deliberately not defaulted. A port is allocated from the install's
		// range rather than derived from a name, and inventing one here would
		// collide with an app that already holds it.
	}
}

func (d Defaults) applyRuntime(s *AppSpec) {
	if s.Runtime.AdapterRef == "" {
		s.Runtime.AdapterRef = d.RuntimeAdapter
	}
	if s.Runtime.IsolationFloor == 0 {
		s.Runtime.IsolationFloor = d.RuntimeIsolation
	}
}

func (d Defaults) applyBuild(s *AppSpec) {
	// A prebuilt image is not built, so it needs no builder. Defaulting one in
	// would make the planner demand a builder this install may not have, for a
	// deploy that never touches it.
	if s.Build.Strategy != BuildPrebuilt && s.Build.AdapterRef == "" {
		s.Build.AdapterRef = d.BuilderAdapter
	}
	if s.Build.IsolationFloor == 0 {
		s.Build.IsolationFloor = d.BuildIsolation
	}
	if s.Build.TimeoutSeconds == 0 {
		s.Build.TimeoutSeconds = d.BuildTimeoutSeconds
	}
	if s.Build.EgressMode == "" {
		// A build fetches dependencies. R-118 lets an install restrict this,
		// and R-270 says Pando ships permissive and is narrowed deliberately.
		s.Build.EgressMode = EgressAllowAll
	}
}

func (d Defaults) applyDeploy(s *AppSpec) {
	if s.Deploy.Strategy == "" {
		// R-144. Start-then-swap is opt-in (R-145) and needs a capability the
		// runtime may not have, so it is never the default.
		s.Deploy.Strategy = DeployRecreate
	}
	// AutoDeploy stays off (R-141) and AutoRollback stays off (R-147). Both are
	// false already; naming them here so the absence reads as a decision.
}

func (d Defaults) applyLimits(s *AppSpec) {
	if s.Resources.CPUMillis == 0 {
		s.Resources.CPUMillis = d.Resources.CPUMillis
	}
	if s.Resources.MemoryBytes == 0 {
		s.Resources.MemoryBytes = d.Resources.MemoryBytes
	}
	if s.Resources.DiskBytes == 0 {
		s.Resources.DiskBytes = d.Resources.DiskBytes
	}

	if s.Egress.Mode == "" {
		// R-182: inherit means the install-wide list applies. An app-level
		// allowlist replaces it rather than narrowing it, which is why nothing
		// here ever writes one.
		s.Egress.Mode = EgressInherit
	}

	if s.Retention.SpecRevisions == 0 {
		s.Retention.SpecRevisions = d.Retention.SpecRevisions
	}
	if s.Retention.BackupDailyCount == 0 {
		s.Retention.BackupDailyCount = d.Retention.BackupDailyCount
	}
	if s.Retention.LogBytes == 0 {
		s.Retention.LogBytes = d.Retention.LogBytes
	}
}

// StandardDefaults are the [P] values from the requirements, for the parts of
// Defaults that do not come from adapters or host policy.
//
// Each is a proposed default and may be overridden with a reason:
//
//	R-152  10 pinned spec revisions retained for rollback
//	R-211  daily backups, 7 retained
//	R-223  100 MB of logs per app, oldest discarded first
//	R-240  CPU, memory and disk are set at the host; these are the fallbacks
//	       for an install that has not set them
func StandardDefaults() Defaults {
	return Defaults{
		RoutingMode:         RoutingPath,
		BuildTimeoutSeconds: 1800,
		Resources: Resources{
			CPUMillis:   1000,
			MemoryBytes: 512 << 20,
			DiskBytes:   10 << 30,
		},
		Retention: Retention{
			SpecRevisions:    10,
			BackupDailyCount: 7,
			LogBytes:         100 << 20,
		},
	}
}
