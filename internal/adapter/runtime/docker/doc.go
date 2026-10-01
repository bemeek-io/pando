// Package docker implements the runtime adapter for local containers.
//
// Observe reports facts and never remediates — an adapter that silently restarts things makes
// drift undetectable and breaks the reconciler's report path. Capacity is adapter-reported;
// core does not read /proc and has no concept of a host (R-243).
//
// Each app runs on its own bridge network with Pando's container attached and nothing published
// (R-023, R-025, R-026). An app whose egress rules restrict anything runs instead on an internal
// network with no route out, behind a per-app egress gateway that its workloads reach through
// HTTP_PROXY and HTTPS_PROXY (R-187); one with no restriction runs exactly as it would with no egress
// controls (R-186). See egress.go.
package docker
