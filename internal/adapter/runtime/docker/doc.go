// Package docker implements the runtime adapter for local containers.
//
// Observe reports facts and never remediates — an adapter that silently restarts things makes
// drift undetectable and breaks the reconciler's report path. Capacity is adapter-reported;
// core does not read /proc and has no concept of a host (R-243).
package docker
