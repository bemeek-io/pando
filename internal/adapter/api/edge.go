package api

import "github.com/trypando/pando/internal/secret"

// --- edge ------------------------------------------------------------------
//
// An edge is an install-scoped workload a routing adapter needs running in
// front of Pando: Traefik terminating :80 and :443, or cloudflared holding a
// tunnel open (R-174, design 03 §4.4). The routing adapter says what runs; the
// runtime adapter says how; core joins them. Neither adapter reaches into the
// other.
//
// It is deliberately not a BundlePlan. An edge publishes host ports, which
// R-026 forbids for a workload, and it is in no app's network. A flag on the
// app path would be a loophole every app plan could reach.

// EdgeRequest is what core tells a routing adapter when asking for its edge.
type EdgeRequest struct {
	// Ref is the adapter configuration's ID. An edge is named for it, so two
	// configured Traefiks would be two edges rather than one fought over.
	Ref string

	// ProxyUpstream is Pando's proxy, as the edge must reach it (R-023) — the
	// same value every RouteRequest carries.
	ProxyUpstream string
}

// EdgePlan is the workload a routing adapter needs running.
type EdgePlan struct {
	// Name is unique within the install. The runtime names what it creates
	// from it and finds it again by it.
	Name string

	Image string
	Args  []string

	// Env reaches the edge's configuration and nowhere else. Credentials —
	// a DNS provider's key, a tunnel token — are secret.Value so they cannot
	// reach a log line on the way (R-194).
	Env map[string]secret.Value

	// Ports are host ports bound to the edge. They are the only ports Pando
	// ever publishes: an app's workloads publish none (R-026).
	Ports []EdgePort

	Mounts []EdgeMount

	// ProxyAlias is the host name the edge dials to reach Pando's proxy — the
	// host part of ProxyUpstream. The runtime makes Pando answer to it on the
	// network it shares with the edge, and joins the edge to nothing else.
	ProxyAlias string
}

// EdgePort publishes one port of the edge on the host.
type EdgePort struct {
	Host      int
	Container int
	Protocol  string // "tcp" when empty
}

// EdgeMount is storage the edge sees at Path. Exactly one of SharedWithPando
// and Volume is set.
type EdgeMount struct {
	Path string

	// SharedWithPando is a path in Pando's own filesystem whose storage the
	// edge mounts too — the directory the Traefik adapter writes routes into.
	// The runtime resolves what that storage is (R-251); core never learns.
	SharedWithPando string

	// Volume is storage the edge owns, kept when the edge is recreated — an
	// ACME certificate store, so a restart does not re-issue every certificate.
	Volume string

	ReadOnly bool
}

// EdgeState is an edge as found.
type EdgeState struct {
	Present bool
	Running bool

	// Detail says what is wrong in words an operator can act on, when Present
	// or Running is false for a reason the runtime can see.
	Detail string
}
