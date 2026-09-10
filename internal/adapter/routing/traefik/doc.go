// Package traefik implements subdomain and path routing with TLS.
//
// Built last, deliberately: it is the second implementation of the routing interface, and
// building it is the test of whether that abstraction holds. If Traefik requires changing the
// interface, the interface was wrong.
//
// The generated config points at Pando's proxy, never at the workload — the workload address
// is right there and would appear to work, which is exactly the trap.
package traefik
